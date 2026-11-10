package mcpsrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// okRemote is a server that accepts everything.
type okRemote struct{ uid uint32 }

func (okRemote) Folders() ([]mailbox.Folder, error) { return nil, nil }
func (okRemote) Fetch(string, uint32, int) ([]mailbox.Header, error) {
	return nil, nil
}
func (okRemote) Flags(string, int) (map[uint32]bool, error) { return nil, nil }
func (okRemote) SetSeen(string, []uint32, bool) error       { return nil }
func (r okRemote) Move(_ string, uids []uint32, _ string) (map[uint32]uint32, error) {
	m := map[uint32]uint32{}
	for _, u := range uids {
		m[u] = u + 1000
	}
	return m, nil
}
func (okRemote) Close() error { return nil }

type secrets struct{}

func (secrets) Get(string) (string, error) { return "x", nil }
func (secrets) Set(string, string) error   { return nil }
func (secrets) Delete(string) error        { return nil }

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.UpsertAccount(store.Account{ID: "g", Provider: "google", Email: "me@gmail.com", Host: "h"})
	st.SetFolder("g", "INBOX", store.RoleInbox)
	st.SetFolder("g", "Archive", store.RoleArchive)
	st.SetFolder("g", "Trash", mailbox.RoleSkip)
	now := time.Now()
	for i, m := range []store.Message{
		{UID: 1, ThreadKey: "review", FromName: "Lin", FromAddr: "lin@x.com", Subject: "Design review", Body: "Thursday 3pm?", Category: store.CatPerson},
		{UID: 2, ThreadKey: "review", FromName: "Lin", FromAddr: "lin@x.com", Subject: "Re: Design review", Body: "or Friday", Category: store.CatPerson},
		{UID: 3, ThreadKey: "ci", FromAddr: "noreply@ci.dev", Subject: "Build passed", Body: "green", Category: store.CatNotification},
	} {
		m.AccountID, m.Folder, m.Role, m.Date = "g", "INBOX", store.RoleInbox, now.Add(time.Duration(i)*time.Minute)
		m.Snippet = m.Body
		if err := st.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	svc := mailbox.New(st, secrets{})
	svc.Dial = func(store.Account, string) mailbox.Remote { return okRemote{} }
	s := New(svc, "zm_secret", "127.0.0.1:0")
	return s, httptest.NewServer(s.Handler())
}

func connect(t *testing.T, url, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: url + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s result %s: %v", tool, b, err)
		}
	}
	return res
}

func TestTokenRequired(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()
	res, err := http.Post(ts.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status without a token = %d, want 401", res.StatusCode)
	}
}

func TestToolsEndToEnd(t *testing.T) {
	s, ts := newTestServer(t)
	defer ts.Close()
	cs := connect(t, ts.URL, "zm_secret")

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != len(Tools) {
		t.Fatalf("tools = %d, %v; want %d", len(tools.Tools), err, len(Tools))
	}
	for _, tl := range tools.Tools {
		if tl.Name == "send_mail" || tl.Name == "delete_thread" {
			t.Errorf("tool %s must not exist", tl.Name)
		}
	}

	var found searchOut
	call(t, cs, "search_mail", map[string]any{"query": "friday"}, &found)
	if len(found.Messages) != 1 || found.Messages[0].Account != "me@gmail.com" || !found.Messages[0].Unread {
		t.Fatalf("search = %+v", found)
	}
	id := found.Messages[0].ID

	var th threadOut
	call(t, cs, "read_thread", map[string]any{"id": id}, &th)
	if len(th.Messages) != 2 || th.Messages[0].Body != "Thursday 3pm?" {
		t.Errorf("thread = %+v", th)
	}

	var done doneOut
	call(t, cs, "mark_read", map[string]any{"ids": []int64{id}}, &done)
	var unread searchOut
	call(t, cs, "search_mail", map[string]any{"unread_only": true}, &unread)
	if done.Affected != 1 || len(unread.Messages) != 2 {
		t.Errorf("affected %d, unread %d, want 1 and 2", done.Affected, len(unread.Messages))
	}

	call(t, cs, "archive_thread", map[string]any{"id": id}, &done)
	var arch searchOut
	call(t, cs, "search_mail", map[string]any{"folder": "Archive"}, &arch)
	if done.Affected != 2 || len(arch.Messages) != 2 {
		t.Errorf("archived %d, in archive %d, want 2 and 2", done.Affected, len(arch.Messages))
	}

	if res := call(t, cs, "move_to_folder", map[string]any{"ids": []int64{id}, "folder": "Trash"}, nil); !res.IsError {
		t.Error("moving to Trash must be an error")
	}

	var st statsOut
	call(t, cs, "get_stats", map[string]any{"days": 1}, &st)
	if st.Received != 3 || st.HandledByAgents != 2 {
		t.Errorf("stats = %+v", st)
	}
	var fo foldersOut
	call(t, cs, "list_folders", nil, &fo)
	if len(fo.Folders["me@gmail.com"]) != 2 {
		t.Errorf("folders = %v: Trash must not be listed", fo.Folders)
	}

	s.SetEnabled("mark_read", false)
	if res := call(t, cs, "mark_read", map[string]any{"ids": []int64{id}}, nil); !res.IsError {
		t.Error("a turned-off tool must be an error")
	}

	ops, _ := s.svc.Store.RecentOps(50)
	seen := map[string]bool{}
	for _, o := range ops {
		if o.Client != "test-agent" || o.Actor != store.ActorAgent {
			t.Errorf("op %+v should be an agent call by test-agent", o)
		}
		seen[o.Tool] = true
	}
	for _, tool := range []string{"search_mail", "read_thread", "mark_read", "archive_thread", "move_to_folder", "get_stats"} {
		if !seen[tool] {
			t.Errorf("no log of %s", tool)
		}
	}
}

func TestWrongTokenCannotConnect(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()
	c := mcp.NewClient(&mcp.Implementation{Name: "x", Version: "1"}, nil)
	_, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{"wrong"}}, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err == nil {
		t.Fatal("a wrong token must not connect")
	}
}

func TestStartRefusesNonLoopback(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	s := New(mailbox.New(st, secrets{}), "t", "0.0.0.0:0")
	if err := s.Start(); err == nil {
		s.Stop()
		t.Fatal("serving beyond loopback must be refused")
	}
	s = New(mailbox.New(st, secrets{}), "t", "127.0.0.1:0")
	if err := s.Start(); err != nil || !s.Running() {
		t.Fatalf("start on loopback: %v", err)
	}
	s.Stop()
	if s.Running() {
		t.Error("still running after Stop")
	}
}
