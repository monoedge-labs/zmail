package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monoedge-labs/zmail/internal/guard"
	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/redact"
	"github.com/monoedge-labs/zmail/internal/store"
)

// seedMail makes a mail database in dir: one account, a thread of two
// messages, a notification, an old message, and extra newer messages.
func seedMail(t *testing.T, dir string, extra int) {
	t.Helper()
	st, err := store.Open(dir + "/zmail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.UpsertAccount(store.Account{ID: "g", Provider: "google", Email: "me@gmail.com", Host: "h"})
	st.UpsertAccount(store.Account{ID: "q", Provider: "qq", Email: "me@qq.com", Host: "h"})
	for _, a := range []string{"g", "q"} {
		st.SetFolder(a, "INBOX", store.RoleInbox)
		st.SetFolder(a, "Archive", store.RoleArchive)
	}
	st.SetFolder("g", "Projects", store.RoleOther)
	st.SetFolder("g", "[Gmail]/Spam", store.RoleJunk)
	for _, f := range []string{"Trash", "Junk", "Drafts"} {
		st.SetFolder("g", f, mailbox.RoleSkip)
	}
	now := time.Now()
	long := strings.Repeat("长", 7000)
	msgs := []store.Message{
		{AccountID: "g", UID: 1, ThreadKey: "review", FromName: "Lin", FromAddr: "lin@x.com", Subject: "Design review", Body: "Thursday 3pm?", Category: store.CatPerson, Date: now.Add(-3 * time.Minute)},
		{AccountID: "g", UID: 2, ThreadKey: "review", FromName: "Lin", FromAddr: "lin@x.com", Subject: "Re: Design review", Body: "or Friday", Category: store.CatPerson, Date: now.Add(-2 * time.Minute)},
		{AccountID: "g", UID: 3, ThreadKey: "ci", FromAddr: "noreply@ci.dev", Subject: "Build passed", Body: long, Category: store.CatNotification, Date: now.Add(-time.Minute)},
		{AccountID: "g", UID: 4, ThreadKey: "old", FromAddr: "old@x.com", Subject: "Old news", Body: "ancient", Category: store.CatNewsletter, Date: now.AddDate(0, 0, -30)},
		{AccountID: "q", UID: 1, ThreadKey: "qq", FromAddr: "tx@qq.com", Subject: "From QQ", Body: "hello", Category: store.CatPerson, Date: now.Add(-4 * time.Minute)},
	}
	for i := 0; i < extra; i++ {
		msgs = append(msgs, store.Message{AccountID: "g", UID: uint32(100 + i), ThreadKey: fmt.Sprint("bulk", i), FromAddr: "bulk@x.com",
			Subject: fmt.Sprint("Bulk ", i), Body: "bulk", Category: store.CatNotification, Date: now.Add(-time.Duration(10+i) * time.Second)})
	}
	for _, m := range msgs {
		m.Folder, m.Role, m.Snippet = "INBOX", store.RoleInbox, m.Subject
		if err := st.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonRun(t *testing.T, dir string, v any, args ...string) error {
	t.Helper()
	out, err := run(t, dir, memSecrets{}, "", append(args, "--json")...)
	if err != nil {
		return err
	}
	if v != nil {
		if e := json.Unmarshal([]byte(out), v); e != nil {
			t.Fatalf("%v printed %q: %v", args, out, e)
		}
	}
	return nil
}

type found struct {
	Subject  string
	Messages []mailItem
}

func search(t *testing.T, dir string, args ...string) []mailItem {
	t.Helper()
	var f found
	if err := jsonRun(t, dir, &f, append([]string{"search"}, args...)...); err != nil {
		t.Fatalf("search %v: %v", args, err)
	}
	return f.Messages
}

func TestMailCLI(t *testing.T) {
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })
	dir := t.TempDir()
	seedMail(t, dir, 0)

	// search: filters
	if ms := search(t, dir, "--query", "friday"); len(ms) != 1 || ms[0].Account != "me@gmail.com" || !ms[0].Unread || ms[0].Body != "" {
		t.Fatalf("query: %+v", ms)
	}
	if ms := search(t, dir); len(ms) != 5 || ms[0].Subject != "Build passed" {
		t.Errorf("all, newest first: %+v", ms)
	}
	if ms := search(t, dir, "--account", "ME@QQ.COM"); len(ms) != 1 || ms[0].Subject != "From QQ" {
		t.Errorf("account: %+v", ms)
	}
	if ms := search(t, dir, "--category", "notification"); len(ms) != 1 {
		t.Errorf("category: %+v", ms)
	}
	if ms := search(t, dir, "--since-days", "7"); len(ms) != 4 {
		t.Errorf("since-days 7 must leave out the old message: %+v", ms)
	}
	if ms := search(t, dir, "--folder", "Archive"); len(ms) != 0 {
		t.Errorf("folder: %+v", ms)
	}
	if err := jsonRun(t, dir, nil, "search", "--account", "nobody@x.com"); err == nil {
		t.Error("an unknown account must be an error")
	}

	// read: the whole thread, oldest first; bodies cut at 6000 runes
	id := search(t, dir, "--query", "friday")[0].ID
	var th found
	if err := jsonRun(t, dir, &th, "read", fmt.Sprint(id)); err != nil {
		t.Fatal(err)
	}
	if th.Subject != "Design review" || len(th.Messages) != 2 || th.Messages[0].Body != "Thursday 3pm?" {
		t.Errorf("thread: %+v", th)
	}
	ci := search(t, dir, "--query", "Build")[0].ID
	if err := jsonRun(t, dir, &th, "read", fmt.Sprint(ci)); err != nil {
		t.Fatal(err)
	}
	if b := th.Messages[0].Body; !strings.HasSuffix(b, "\n[cut]") || len([]rune(b)) != 6000+len("\n[cut]") {
		t.Errorf("body of %d runes", len([]rune(b)))
	}
	for _, bad := range [][]string{{"read"}, {"read", "1", "2"}, {"read", "x"}, {"read", "0"}, {"read", "9999"}} {
		if err := jsonRun(t, dir, nil, bad...); err == nil {
			t.Errorf("%v must fail", bad)
		}
	}

	// mark-read, with flags among the IDs
	var done struct{ Affected int }
	if err := jsonRun(t, dir, &done, "mark-read", fmt.Sprint(id)); err != nil || done.Affected != 1 {
		t.Fatalf("mark-read: %v %+v", err, done)
	}
	if ms := search(t, dir, "--unread"); len(ms) != 4 {
		t.Errorf("unread after mark-read: %d, want 4", len(ms))
	}
	if err := jsonRun(t, dir, &done, "mark-read", fmt.Sprint(id), "--unread"); err != nil || done.Affected != 1 {
		t.Fatalf("mark-read --unread: %v %+v", err, done)
	}
	if ms := search(t, dir, "--unread"); len(ms) != 5 {
		t.Errorf("unread after --unread: %d, want 5", len(ms))
	}
	if err := jsonRun(t, dir, nil, "mark-read"); err == nil {
		t.Error("mark-read without IDs must fail")
	}

	// archive: the whole thread
	if err := jsonRun(t, dir, &done, "archive", fmt.Sprint(id)); err != nil || done.Affected != 2 {
		t.Fatalf("archive: %v %+v", err, done)
	}
	if ms := search(t, dir, "--folder", "Archive"); len(ms) != 2 {
		t.Errorf("archived: %+v", ms)
	}

	// move: a folder is needed, and Trash and Drafts are refused
	for _, f := range []string{"", "  ", "Trash", "Drafts"} {
		if err := jsonRun(t, dir, nil, "move", "--folder", f, fmt.Sprint(ci)); err == nil {
			t.Errorf("move to %q must fail", f)
		}
	}
	if err := jsonRun(t, dir, nil, "move", fmt.Sprint(ci)); err == nil {
		t.Error("move without --folder must fail")
	}
	if err := jsonRun(t, dir, &done, "move", fmt.Sprint(ci), "--folder", "Projects"); err != nil || done.Affected != 1 {
		t.Fatalf("move: %v %+v", err, done)
	}
	if ms := search(t, dir, "--folder", "Projects"); len(ms) != 1 || ms[0].ID != ci {
		t.Errorf("moved: %+v", ms)
	}

	// a name the account lacks is an error, not a new folder, unless --create
	if err := jsonRun(t, dir, nil, "move", "--folder", "Taxes", fmt.Sprint(ci)); err == nil || !strings.Contains(err.Error(), "--create") {
		t.Errorf("move to a missing folder: %v", err)
	}
	if err := jsonRun(t, dir, &done, "move", "--folder", "projects", fmt.Sprint(ci)); err != nil {
		t.Errorf("a folder's name in other case: %v", err)
	}
	if err := jsonRun(t, dir, &done, "move", "--create", "--folder", "Taxes", fmt.Sprint(ci)); err != nil || done.Affected != 1 {
		t.Fatalf("move --create: %v %+v", err, done)
	}
	if ms := search(t, dir, "--folder", "Taxes"); len(ms) != 1 || ms[0].ID != ci {
		t.Errorf("created: %+v", ms)
	}

	// junk: the account's own junk folder, found by role, and searched by name
	if err := jsonRun(t, dir, &done, "move", "--folder", "junk", fmt.Sprint(ci)); err != nil || done.Affected != 1 {
		t.Fatalf("move to junk: %v %+v", err, done)
	}
	if ms := search(t, dir, "--folder", "[Gmail]/Spam"); len(ms) != 1 || ms[0].ID != ci {
		t.Errorf("junked: %+v", ms)
	}
	if ms := search(t, dir, "--query", "Build passed"); len(ms) != 0 {
		t.Errorf("a plain search found junk: %+v", ms)
	}
	if err := jsonRun(t, dir, &done, "move", "--folder", "INBOX", fmt.Sprint(ci)); err != nil {
		t.Fatalf("move back from junk: %v", err)
	}

	// folders leave out Trash and Drafts, and tell the roles of the others
	var fo struct {
		Folders map[string][]string
		Roles   map[string]map[string]string
	}
	if err := jsonRun(t, dir, &fo, "folders"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fo.Folders["me@gmail.com"], ","); got != "Archive,INBOX,Projects,Taxes,[Gmail]/Spam" {
		t.Errorf("folders = %q", got)
	}
	if r := fo.Roles["me@gmail.com"]; r["[Gmail]/Spam"] != "junk" || r["Archive"] != "archive" || r["Projects"] != "other" {
		t.Errorf("roles = %v", r)
	}

	// stats: 7 days by default
	var st statsOut
	if err := jsonRun(t, dir, &st, "stats"); err != nil {
		t.Fatal(err)
	}
	if st.Days != 7 || st.Received != 4 || st.HandledByAgents != 3 {
		t.Errorf("stats = %+v", st)
	}
	if err := jsonRun(t, dir, &st, "stats", "--days", "90"); err != nil || st.Days != 90 || st.Received != 5 {
		t.Errorf("stats --days 90 = %+v, %v", st, err)
	}
	if err := jsonRun(t, dir, &st, "stats", "--days", "-3"); err != nil || st.Days != 7 {
		t.Errorf("stats --days -3 = %+v, %v", st, err)
	}

	// text output
	if out, err := run(t, dir, memSecrets{}, "", "search", "--query", "QQ"); err != nil || !strings.Contains(out, "From QQ") {
		t.Errorf("text search: %q %v", out, err)
	}

	// every call went through the Service or was logged here, as the cli agent
	s, _ := store.Open(dir + "/zmail.db")
	defer s.Close()
	ops, _ := s.RecentOps(500)
	seen := map[string]bool{}
	for _, o := range ops {
		if o.Client != "cli" || o.Actor != store.ActorAgent {
			t.Errorf("op %+v should be an agent call by cli", o)
		}
		seen[o.Tool] = true
	}
	for _, tool := range []string{"search_mail", "read_thread", "list_folders", "get_stats", "mark_read", "archive_thread", "move_to_folder"} {
		if !seen[tool] {
			t.Errorf("no log of %s", tool)
		}
	}
	var failed int
	for _, o := range ops {
		if o.Tool == "move_to_folder" && !o.OK {
			failed++
		}
	}
	if failed != 2 {
		t.Errorf("%d refused moves logged, want 2", failed)
	}
}

func TestSearchLimit(t *testing.T) {
	dir := t.TempDir()
	seedMail(t, dir, 130)
	for _, c := range []struct {
		args []string
		want int
	}{
		{nil, 20},
		{[]string{"--limit", "5"}, 5},
		{[]string{"--limit", "500"}, 100},
		{[]string{"--limit", "0"}, 20},
		{[]string{"--limit", "-1"}, 20},
	} {
		if got := len(search(t, dir, c.args...)); got != c.want {
			t.Errorf("search %v returned %d, want %d", c.args, got, c.want)
		}
	}
}

func TestConnectIsGone(t *testing.T) {
	if _, err := run(t, t.TempDir(), memSecrets{}, "", "connect"); err == nil {
		t.Error("zmail connect must no longer exist")
	}
}

// search, folders and stats take flags only: a stray word must be an error,
// not a query that quietly ignores it.
func TestFlagsOnlyCommandsRefuseWords(t *testing.T) {
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })
	dir := t.TempDir()
	seedMail(t, dir, 0)
	for _, c := range []struct {
		word string
		args []string
	}{
		{"accidental-positional", []string{"search", "accidental-positional", "--json"}},
		{"accidental-positional", []string{"search", "--json", "accidental-positional"}},
		{"extra", []string{"search", "--query", "friday", "extra", "--limit", "5"}},
		{"accidental-positional", []string{"folders", "accidental-positional", "--json"}},
		{"accidental-positional", []string{"folders", "--json", "accidental-positional"}},
		{"accidental-positional", []string{"stats", "accidental-positional", "--json"}},
		{"accidental-positional", []string{"stats", "--days", "7", "accidental-positional"}},
	} {
		args := c.args
		out, err := run(t, dir, memSecrets{}, "", args...)
		if err == nil || !strings.Contains(err.Error(), "unexpected positional argument "+`"`+c.word+`"`) ||
			!strings.Contains(err.Error(), "usage: zmail "+args[0]) {
			t.Errorf("%v: err = %v, want an unexpected positional argument error with usage", args, err)
		}
		if out != "" {
			t.Errorf("%v printed %q", args, out)
		}
	}
	// nothing ran: no operation was logged
	s, _ := store.Open(dir + "/zmail.db")
	defer s.Close()
	if ops, _ := s.RecentOps(10); len(ops) != 0 {
		t.Errorf("refused commands logged %+v", ops)
	}
	// IDs commands keep taking words, among flags
	id := search(t, dir, "--query", "friday")[0].ID
	if err := jsonRun(t, dir, nil, "read", "--json", fmt.Sprint(id)); err != nil {
		t.Errorf("read with a flag before the ID: %v", err)
	}
}

// With the user's choice kept in privacy.json, what the agent reads has its
// private values hidden, and says how many; without it the text is whole.
func TestAgentReadsHidden(t *testing.T) {
	dir := t.TempDir()
	seedMail(t, dir, 0)
	st, _ := store.Open(dir + "/zmail.db")
	if err := st.UpsertMessage(store.Message{AccountID: "g", Folder: "INBOX", Role: store.RoleInbox, UID: 50, ThreadKey: "otp",
		FromName: "Bank", FromAddr: "bank@x.com", Subject: "Your code 481516", Snippet: "call 13800138000",
		Body: "Mail lin@acme.com. Your verification code is 481516.", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })

	read := func() mailItem {
		var r struct{ Messages []mailItem }
		ms := search(t, dir, "--query", "481516")
		if len(ms) != 1 {
			t.Fatalf("search = %+v", ms)
		}
		if err := jsonRun(t, dir, &r, "read", fmt.Sprint(ms[0].ID)); err != nil {
			t.Fatal(err)
		}
		return r.Messages[0]
	}
	if m := read(); !strings.Contains(m.Body, "481516") || m.Redacted != 0 {
		t.Errorf("by default: %+v", m)
	}
	if err := (privacy{Agent: true, ModelOff: true}).save(dir); err != nil {
		t.Fatal(err)
	}
	m := read()
	if strings.Contains(m.Body+m.Subject+m.Snippet, "481516") || strings.Contains(m.Body, "lin@acme.com") || strings.Contains(m.Snippet, "13800138000") {
		t.Errorf("not hidden: %+v", m)
	}
	if m.Subject != "Your code 481516" && !strings.Contains(m.Subject, "‹") {
		t.Errorf("subject = %q", m.Subject)
	}
	if m.Redacted != 4 || !strings.Contains(m.Body, "‹EMAIL_1›") || !strings.Contains(m.Body, "‹CODE_1›") {
		t.Errorf("hidden count or placeholders: %+v", m)
	}
}

// linkModel makes the model fetched into $ZMAIL_MODEL_ROOT the one in a config directory,
// or skips the test that needs it.
func linkModel(t *testing.T, dir string) {
	t.Helper()
	root := os.Getenv("ZMAIL_MODEL_ROOT")
	if root == "" || !guard.Installed(guard.Dir(root)) {
		t.Skip("set ZMAIL_MODEL_ROOT to a folder holding the model (go test ./internal/guard fetches it)")
	}
	d := modelDir(dir)
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(guard.Dir(root), d); err != nil {
		t.Fatal(err)
	}
}

// Asked to find names and addresses with the model, the agent's commands
// either do so or fail: they never pass the text on with the patterns alone.
func TestAgentModelMissing(t *testing.T) {
	dir := t.TempDir()
	seedMail(t, dir, 0)
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })
	if err := (privacy{Agent: true}).save(dir); err != nil {
		t.Fatal(err)
	}
	err := jsonRun(t, dir, nil, "search", "--query", "Design")
	if err == nil || !strings.Contains(err.Error(), "not downloaded") {
		t.Errorf("search with the model missing: %v", err)
	}
	if err := (privacy{Agent: true, ModelOff: true}).save(dir); err != nil {
		t.Fatal(err)
	}
	if err := jsonRun(t, dir, nil, "search", "--query", "Design"); err != nil {
		t.Errorf("search with the model off: %v", err)
	}
}

func TestAgentReadsWithModel(t *testing.T) {
	dir := t.TempDir()
	linkModel(t, dir)
	seedMail(t, dir, 0)
	st, _ := store.Open(dir + "/zmail.db")
	if err := st.UpsertMessage(store.Message{AccountID: "g", Folder: "INBOX", Role: store.RoleInbox, UID: 60, ThreadKey: "addr",
		FromName: "Lin", FromAddr: "lin@x.com", Subject: "收货地址", Snippet: "我是王小明",
		Body: "你好，我是王小明，请寄到北京市海淀区中关村大街1号，电话 13800138000。", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })
	if err := (privacy{Agent: true}).save(dir); err != nil {
		t.Fatal(err)
	}
	ms := search(t, dir, "--query", "收货地址")
	if len(ms) != 1 || strings.Contains(ms[0].Snippet, "王小明") {
		t.Fatalf("snippet not hidden: %+v", ms)
	}
	var r struct{ Messages []mailItem }
	if err := jsonRun(t, dir, &r, "read", fmt.Sprint(ms[0].ID)); err != nil {
		t.Fatal(err)
	}
	b := r.Messages[0].Body
	for _, leak := range []string{"王小明", "海淀", "13800138000"} {
		if strings.Contains(b, leak) {
			t.Errorf("body keeps %q: %s", leak, b)
		}
	}
	for _, want := range []string{"‹PERSON_1›", "‹ADDRESS_1›", "‹PHONE_1›"} {
		if !strings.Contains(b, want) {
			t.Errorf("body lacks %s: %s", want, b)
		}
	}
}

// A text the model has read is not read again, an empty answer included; a
// model of another revision starts clean.
func TestStoredModel(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	calls := 0
	inner := func(text string) ([]redact.Found, bool) {
		calls++
		if text == "nothing" {
			return nil, true
		}
		return []redact.Found{{Start: 1, End: 3, Kind: redact.Person}}, true
	}
	m := storedModel(st, "r1", inner)
	first, _ := m("a name")
	if again, ok := m("a name"); !ok || !slices.Equal(first, again) || calls != 1 {
		t.Errorf("second read = %v %v after %d calls, want %v from the store", again, ok, calls, first)
	}
	m("nothing")
	if found, ok := m("nothing"); !ok || len(found) != 0 || calls != 2 {
		t.Errorf("empty answer = %v %v after %d calls, want it kept", found, ok, calls)
	}
	storedModel(st, "r2", inner)("a name")
	if calls != 3 {
		t.Errorf("another revision made %d calls, want 3", calls)
	}
	storedModel(nil, "r1", inner)("a name")
	if calls != 4 {
		t.Errorf("no store made %d calls, want 4", calls)
	}
}
