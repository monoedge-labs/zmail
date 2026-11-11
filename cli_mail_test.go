package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monoedge-labs/zmail/internal/mailbox"
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

	// move: a folder is needed, and Trash, Junk and Drafts are refused
	for _, f := range []string{"", "  ", "Trash", "Junk", "Drafts"} {
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

	// folders leave out Trash, Junk and Drafts
	var fo struct{ Folders map[string][]string }
	if err := jsonRun(t, dir, &fo, "folders"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fo.Folders["me@gmail.com"], ","); got != "Archive,INBOX,Projects" {
		t.Errorf("folders = %q", got)
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
	if failed != 3 {
		t.Errorf("%d refused moves logged, want 3", failed)
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
