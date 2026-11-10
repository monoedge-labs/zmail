// Package mcpsrv serves the mail on a local MCP endpoint for agents. The
// tools read mail, flag it read, archive it and file it in folders. None of
// them sends mail or deletes it.
package mcpsrv

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// ToolInfo names a tool for the window's permission list.
type ToolInfo struct{ Name, Summary string }

// Tools are the tools served, in the order the window lists them.
var Tools = []ToolInfo{
	{"search_mail", "搜索与筛选邮件"},
	{"read_thread", "读取会话全文"},
	{"list_folders", "列出文件夹"},
	{"get_stats", "收发量与处理情况统计"},
	{"mark_read", "标记已读或未读"},
	{"archive_thread", "归档会话"},
	{"move_to_folder", "移动到文件夹"},
}

type Server struct {
	svc   *mailbox.Service
	token string
	Addr  string
	// OnCall is called after every tool call, from the server's goroutines.
	OnCall func()

	mu      sync.Mutex
	enabled map[string]bool
	http    *http.Server
}

// New returns a server that answers requests bearing token.
func New(svc *mailbox.Service, token, addr string) *Server {
	s := &Server{svc: svc, token: token, Addr: addr, enabled: map[string]bool{}}
	for _, t := range Tools {
		s.enabled[t.Name] = true
	}
	return s
}

func (s *Server) SetEnabled(tool string, on bool) {
	s.mu.Lock()
	s.enabled[tool] = on
	s.mu.Unlock()
}

func (s *Server) Enabled(tool string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[tool]
}

// Handler returns the HTTP handler: the MCP endpoint behind the token.
func (s *Server) Handler() http.Handler {
	srv := s.build()
	mcpH := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.auth(mcpH))
	return mux
}

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "missing or wrong token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start listens on Addr, which must be a loopback address, and serves in
// the background. It returns once listening.
func (s *Server) Start() error {
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s is not a loopback address: the mail is served to this machine only", s.Addr)
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	h := s.http
	s.mu.Unlock()
	go h.Serve(ln)
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	h := s.http
	s.http = nil
	s.mu.Unlock()
	if h == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.Shutdown(ctx)
}

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.http != nil
}

// ---- tools

type searchIn struct {
	Query    string `json:"query,omitempty" jsonschema:"text to find in subject, sender or body"`
	Account  string `json:"account,omitempty" jsonschema:"an account's email address"`
	Folder   string `json:"folder,omitempty" jsonschema:"a folder name, as list_folders gives"`
	Category string `json:"category,omitempty" jsonschema:"person, notification or newsletter"`
	Unread   bool   `json:"unread_only,omitempty"`
	SinceDay int    `json:"since_days,omitempty" jsonschema:"only mail from the last N days"`
	Limit    int    `json:"limit,omitempty" jsonschema:"at most this many, newest first; 20 by default, 100 at most"`
}

type mailItem struct {
	ID       int64  `json:"id"`
	Account  string `json:"account"`
	Folder   string `json:"folder"`
	From     string `json:"from"`
	Subject  string `json:"subject"`
	Snippet  string `json:"snippet"`
	Date     string `json:"date"`
	Unread   bool   `json:"unread"`
	Category string `json:"category"`
	Body     string `json:"body,omitempty"`
}

type searchOut struct {
	Messages []mailItem `json:"messages"`
}

type idIn struct {
	ID int64 `json:"id" jsonschema:"a message id from search_mail"`
}

type threadOut struct {
	Subject  string     `json:"subject"`
	Messages []mailItem `json:"messages"`
}

type foldersOut struct {
	Folders map[string][]string `json:"folders" jsonschema:"folder names by account email"`
}

type statsIn struct {
	Days int `json:"days,omitempty" jsonschema:"the last N days; 7 by default"`
}

type statsOut struct {
	Days            int            `json:"days"`
	Received        int            `json:"received"`
	Sent            int            `json:"sent"`
	Unread          int            `json:"unread_in_inbox"`
	ByAccount       map[string]int `json:"received_by_account"`
	TopSenders      []senderItem   `json:"top_senders"`
	HandledByAgents int            `json:"handled_by_agents"`
	HandledByUser   int            `json:"handled_by_user"`
}

type senderItem struct {
	Name  string `json:"name"`
	Addr  string `json:"addr"`
	Count int    `json:"count"`
}

type markIn struct {
	IDs  []int64 `json:"ids"`
	Read *bool   `json:"read,omitempty" jsonschema:"false marks unread; true by default"`
}

type moveIn struct {
	IDs    []int64 `json:"ids"`
	Folder string  `json:"folder" jsonschema:"the destination folder; created when missing"`
}

type doneOut struct {
	Affected int `json:"affected"`
}

func (s *Server) build() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "zmail", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Zmail is the user's local mail client. Read and search mail, mark it read, archive it, or file it in folders. " +
			"You cannot send, reply to or delete mail. Message ids come from search_mail.",
	})
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	idem := &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false)}

	add(s, srv, "search_mail", "Search mail, newest first. Returns snippets; read_thread gives full text.", ro,
		func(ctx context.Context, who mailbox.Who, in searchIn) (searchOut, error) {
			q := store.Query{Text: in.Query, Folder: in.Folder, Category: in.Category, Unread: in.Unread, Limit: in.Limit}
			if q.Limit <= 0 {
				q.Limit = 20
			}
			if in.SinceDay > 0 {
				q.Since = time.Now().AddDate(0, 0, -in.SinceDay)
			}
			accs, _ := s.svc.Store.Accounts()
			byID := map[string]string{}
			for _, a := range accs {
				byID[a.ID] = a.Email
				if in.Account != "" && strings.EqualFold(a.Email, in.Account) {
					q.AccountID = a.ID
				}
			}
			if in.Account != "" && q.AccountID == "" {
				return searchOut{}, fmt.Errorf("no account %q", in.Account)
			}
			ms, err := s.svc.Store.Search(q)
			return searchOut{Messages: items(ms, byID, false)}, err
		})

	add(s, srv, "read_thread", "Read the whole conversation a message belongs to, oldest first.", ro,
		func(ctx context.Context, who mailbox.Who, in idIn) (threadOut, error) {
			ms, err := s.svc.Store.Thread(in.ID)
			if err != nil {
				return threadOut{}, fmt.Errorf("message %d: %w", in.ID, err)
			}
			accs, _ := s.svc.Store.Accounts()
			byID := map[string]string{}
			for _, a := range accs {
				byID[a.ID] = a.Email
			}
			return threadOut{Subject: ms[0].Subject, Messages: items(ms, byID, true)}, nil
		})

	add(s, srv, "list_folders", "List the folders of every account.", ro,
		func(ctx context.Context, who mailbox.Who, _ struct{}) (foldersOut, error) {
			accs, err := s.svc.Store.Accounts()
			out := foldersOut{Folders: map[string][]string{}}
			for _, a := range accs {
				fs, _ := s.svc.Store.Folders(a.ID)
				for name, role := range fs {
					if role != mailbox.RoleSkip {
						out.Folders[a.Email] = append(out.Folders[a.Email], name)
					}
				}
			}
			return out, err
		})

	add(s, srv, "get_stats", "Counts of mail received and sent, by account and sender, and who handled it.", ro,
		func(ctx context.Context, who mailbox.Who, in statsIn) (statsOut, error) {
			days := in.Days
			if days <= 0 {
				days = 7
			}
			st, err := s.svc.Store.Stats(time.Now().AddDate(0, 0, -days))
			out := statsOut{Days: days, Received: st.Received, Sent: st.Sent, Unread: st.Unread, ByAccount: st.ByAccount,
				HandledByAgents: st.AgentHandled, HandledByUser: st.MeHandled}
			for _, c := range st.TopSenders {
				out.TopSenders = append(out.TopSenders, senderItem(c))
			}
			return out, err
		})

	add(s, srv, "mark_read", "Mark messages read, or unread with read=false.", idem,
		func(ctx context.Context, who mailbox.Who, in markIn) (doneOut, error) {
			read := in.Read == nil || *in.Read
			return doneOut{len(in.IDs)}, s.svc.MarkRead(who, in.IDs, read)
		})

	add(s, srv, "archive_thread", "Archive a conversation: every message of it still in an inbox or folder.", idem,
		func(ctx context.Context, who mailbox.Who, in idIn) (doneOut, error) {
			ms, err := s.svc.Store.Thread(in.ID)
			if err != nil {
				return doneOut{}, fmt.Errorf("message %d: %w", in.ID, err)
			}
			var ids []int64
			for _, m := range ms {
				if m.Role != store.RoleArchive && m.Role != store.RoleSent {
					ids = append(ids, m.ID)
				}
			}
			return doneOut{len(ids)}, s.svc.Archive(who, ids)
		})

	add(s, srv, "move_to_folder", "Move messages to a folder of their account. Trash, junk and drafts are refused.", idem,
		func(ctx context.Context, who mailbox.Who, in moveIn) (doneOut, error) {
			if strings.TrimSpace(in.Folder) == "" {
				return doneOut{}, errors.New("folder is empty")
			}
			return doneOut{len(in.IDs)}, s.svc.MoveToFolder(who, in.IDs, in.Folder)
		})
	return srv
}

// add registers a tool: its permission is checked, who called is worked
// out from the session, and every call is logged.
func add[In, Out any](s *Server, srv *mcp.Server, name, desc string, ann *mcp.ToolAnnotations,
	h func(context.Context, mailbox.Who, In) (Out, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var zero Out
			who := mailbox.Who{Actor: store.ActorAgent, Client: clientName(req)}
			if !s.Enabled(name) {
				s.svc.Store.LogOp(store.Op{Actor: who.Actor, Client: who.Client, Tool: name, Detail: "turned off", OK: false})
				return nil, zero, fmt.Errorf("%s is turned off in Zmail", name)
			}
			out, err := h(ctx, who, in)
			// The tools that change mail log themselves, with what they did.
			if ann != nil && ann.ReadOnlyHint {
				s.svc.Store.LogOp(store.Op{Actor: who.Actor, Client: who.Client, Tool: name, OK: err == nil})
			}
			if s.OnCall != nil {
				s.OnCall()
			}
			if err != nil {
				return nil, zero, err
			}
			return nil, out, nil
		})
}

func clientName(req *mcp.CallToolRequest) string {
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			return p.ClientInfo.Name
		}
	}
	return "unknown"
}

func items(ms []store.Message, accounts map[string]string, withBody bool) []mailItem {
	out := make([]mailItem, 0, len(ms))
	for _, m := range ms {
		from := m.FromAddr
		if m.FromName != "" {
			from = fmt.Sprintf("%s <%s>", m.FromName, m.FromAddr)
		}
		it := mailItem{ID: m.ID, Account: accounts[m.AccountID], Folder: m.Folder, From: from, Subject: m.Subject,
			Snippet: m.Snippet, Date: m.Date.Format(time.RFC3339), Unread: !m.Seen, Category: m.Category}
		if withBody {
			it.Body = truncate(m.Body, 6000)
		}
		out = append(out, it)
	}
	return out
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n[cut]"
	}
	return s
}

func ptr[T any](v T) *T { return &v }
