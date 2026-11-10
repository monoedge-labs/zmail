package mailbox

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/monoedge-labs/zmail/internal/store"
)

func TestFolderRole(t *testing.T) {
	cases := []struct {
		name  string
		attrs []imap.MailboxAttr
		want  string
	}{
		{"INBOX", nil, "inbox"},
		{"[Gmail]/All Mail", []imap.MailboxAttr{imap.MailboxAttrAll}, "archive"},
		{"[Gmail]/Sent Mail", []imap.MailboxAttr{imap.MailboxAttrSent}, "sent"},
		{"[Gmail]/Trash", []imap.MailboxAttr{imap.MailboxAttrTrash}, RoleSkip},
		{"[Gmail]/Starred", []imap.MailboxAttr{imap.MailboxAttrFlagged}, RoleSkip},
		{"Sent Messages", nil, "sent"},      // QQ
		{"Deleted Messages", nil, RoleSkip}, // QQ
		{"草稿箱", nil, RoleSkip},
		{"归档", nil, "archive"},
		{"账单", nil, "other"},
	}
	for _, c := range cases {
		if got := folderRole(c.name, c.attrs); got != c.want {
			t.Errorf("folderRole(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestThreadKeyAndCategory(t *testing.T) {
	for _, s := range []string{"Re: Design review", "RE: re: design review", "回复：Design review", "Fwd: Design Review "} {
		if got := ThreadKey(s); got != "design review" {
			t.Errorf("ThreadKey(%q) = %q", s, got)
		}
	}
	for addr, want := range map[string]string{
		"lin@example.com": "person", "noreply@ci.dev": "notification", "no-reply@x.com": "notification",
		"billing@cloud.com": "notification", "reply@friend.com": "person", "newsletter@news.com": "person",
	} {
		if got := Categorize(addr, false); got != want {
			t.Errorf("Categorize(%q) = %q, want %q", addr, got, want)
		}
	}
	if Categorize("hello@news.com", true) != "newsletter" {
		t.Error("a List-Unsubscribe header makes a newsletter")
	}
}

func TestParseBodyGBKAndHTML(t *testing.T) {
	// "你好，世界" in GBK
	gbk := "From: a@b.c\r\nSubject: x\r\nContent-Type: text/plain; charset=gbk\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" +
		"\xc4\xe3\xba\xc3\xa3\xac\xca\xc0\xbd\xe7"
	if got, _ := parseBody([]byte(gbk)); got != "你好，世界" {
		t.Errorf("gbk body = %q", got)
	}
	multi := "From: a@b.c\r\nList-Unsubscribe: <mailto:u@b.c>\r\nContent-Type: multipart/alternative; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<style>p{}</style><p>Hello&nbsp;<b>there</b></p><p>Bye</p>\r\n--XX--\r\n"
	got, unsub := parseBody([]byte(multi))
	if !unsub || !strings.Contains(got, "Hello") || !strings.Contains(got, "Bye") || strings.Contains(got, "<") || strings.Contains(got, "p{}") {
		t.Errorf("html body = %q, unsub=%v", got, unsub)
	}
	// A cut message still yields what came before the cut.
	cut := multi[:strings.Index(multi, "Bye")+1]
	if got, _ := parseBody([]byte(cut)); !strings.Contains(got, "Hello") {
		t.Errorf("cut body = %q", got)
	}
}

// fakeRemote is an in-memory server.
type fakeRemote struct {
	folders map[string][]Header
	roles   map[string]string
	nextUID uint32
	noMap   bool // does not report the UIDs a move gave
	moves   []string
	fail    error
}

func newFake() *fakeRemote {
	return &fakeRemote{
		folders: map[string][]Header{"INBOX": nil, "Archive": nil, "Trash": nil},
		roles:   map[string]string{"INBOX": "inbox", "Archive": "archive", "Trash": RoleSkip},
		nextUID: 100,
	}
}

func (f *fakeRemote) Folders() (out []Folder, _ error) {
	if f.fail != nil {
		return nil, f.fail
	}
	for n, r := range f.roles {
		out = append(out, Folder{n, r})
	}
	return out, nil
}

func (f *fakeRemote) Fetch(folder string, after uint32, limit int) (out []Header, _ error) {
	for _, h := range f.folders[folder] {
		if h.UID > after {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *fakeRemote) Flags(folder string, limit int) (map[uint32]bool, error) {
	out := map[uint32]bool{}
	for _, h := range f.folders[folder] {
		out[h.UID] = h.Seen
	}
	return out, nil
}

func (f *fakeRemote) SetSeen(folder string, uids []uint32, seen bool) error {
	for _, u := range uids {
		for i := range f.folders[folder] {
			if f.folders[folder][i].UID == u {
				f.folders[folder][i].Seen = seen
			}
		}
	}
	return nil
}

func (f *fakeRemote) Move(folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	if _, ok := f.folders[dest]; !ok {
		f.folders[dest], f.roles[dest] = nil, "other"
	}
	mapped := map[uint32]uint32{}
	for _, u := range uids {
		for i, h := range f.folders[folder] {
			if h.UID == u {
				f.folders[folder] = append(f.folders[folder][:i:i], f.folders[folder][i+1:]...)
				f.nextUID++
				mapped[u], h.UID = f.nextUID, f.nextUID
				f.folders[dest] = append(f.folders[dest], h)
				break
			}
		}
	}
	f.moves = append(f.moves, fmt.Sprintf("%s->%s %v", folder, dest, uids))
	if f.noMap {
		return nil, nil
	}
	return mapped, nil
}

func (f *fakeRemote) Close() error { return nil }

type memSecrets map[string]string

func (m memSecrets) Get(id string) (string, error) {
	if v, ok := m[id]; ok {
		return v, nil
	}
	return "", errors.New("no secret")
}
func (m memSecrets) Set(id, s string) error { m[id] = s; return nil }
func (m memSecrets) Delete(id string) error { delete(m, id); return nil }

func setup(t *testing.T) (*Service, *fakeRemote, store.Account) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fake := newFake()
	now := time.Now()
	fake.folders["INBOX"] = []Header{
		{UID: 1, Subject: "Design review", FromName: "Lin", FromAddr: "lin@x.com", Date: now, Body: "Thursday 3pm?"},
		{UID: 2, Subject: "Re: Design review", FromName: "Lin", FromAddr: "lin@x.com", Date: now.Add(time.Minute), Body: "or Friday"},
		{UID: 3, Subject: "Build passed", FromAddr: "noreply@ci.dev", Date: now, Body: "green"},
	}
	svc := New(st, memSecrets{})
	svc.Dial = func(store.Account, string) Remote { return fake }
	a, err := svc.AddAccount("qq", "me@qq.com", "code")
	if err != nil {
		t.Fatal(err)
	}
	if a.Host != "imap.qq.com:993" {
		t.Errorf("host = %q", a.Host)
	}
	if err := svc.SyncAll(); err != nil {
		t.Fatal(err)
	}
	return svc, fake, a
}

func ids(t *testing.T, svc *Service, q store.Query) []int64 {
	t.Helper()
	ms, err := svc.Store.Search(q)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestSyncMarkReadArchive(t *testing.T) {
	svc, fake, _ := setup(t)
	all := ids(t, svc, store.Query{Role: store.RoleInbox})
	if len(all) != 3 {
		t.Fatalf("inbox has %d messages after sync, want 3", len(all))
	}
	if err := svc.MarkRead(Who{store.ActorAgent, "claude-code"}, all[:1], true); err != nil {
		t.Fatal(err)
	}
	if n := len(ids(t, svc, store.Query{Unread: true})); n != 2 {
		t.Errorf("unread = %d, want 2", n)
	}
	seenOnServer := 0
	for _, h := range fake.folders["INBOX"] {
		if h.Seen {
			seenOnServer++
		}
	}
	if seenOnServer != 1 {
		t.Errorf("server has %d read messages, want 1", seenOnServer)
	}

	if err := svc.Archive(Who{store.ActorAgent, "claude-code"}, all[1:]); err != nil {
		t.Fatal(err)
	}
	if n := len(ids(t, svc, store.Query{Role: store.RoleInbox})); n != 1 {
		t.Errorf("inbox = %d after archiving two, want 1", n)
	}
	if n := len(ids(t, svc, store.Query{Role: store.RoleArchive})); n != 2 {
		t.Errorf("archive = %d, want 2", n)
	}
	m, _ := svc.Store.Get(all[1])
	if m.UID <= 100 || m.HandledBy != store.ActorAgent {
		t.Errorf("archived message = %+v: UID should be the one the server gave", m)
	}
	// A second sync neither duplicates nor resurrects anything.
	if err := svc.SyncAll(); err != nil {
		t.Fatal(err)
	}
	if n := len(ids(t, svc, store.Query{})); n != 3 {
		t.Errorf("%d messages after resync, want 3", n)
	}
	ops, _ := svc.Store.RecentOps(10)
	if len(ops) != 2 || ops[0].Tool != "archive_thread" || ops[0].Client != "claude-code" || !ops[0].OK {
		t.Errorf("ops = %+v", ops)
	}
}

func TestMoveWithoutUIDMappingReadsTheMessageAgain(t *testing.T) {
	svc, fake, _ := setup(t)
	fake.noMap = true
	id := ids(t, svc, store.Query{Text: "Build"})[0]
	if err := svc.MoveToFolder(UI, []int64{id}, "账单"); err != nil {
		t.Fatal(err)
	}
	got := ids(t, svc, store.Query{Folder: "账单"})
	if len(got) != 1 {
		t.Fatalf("%d messages in the new folder, want 1", len(got))
	}
	if n := len(ids(t, svc, store.Query{})); n != 3 {
		t.Errorf("%d messages in all, want 3 (no duplicate)", n)
	}
}

func TestMoveRefusesTrash(t *testing.T) {
	svc, fake, _ := setup(t)
	if err := svc.MoveToFolder(Who{store.ActorAgent, "x"}, ids(t, svc, store.Query{}), "Trash"); err == nil {
		t.Fatal("moving to Trash must fail")
	}
	if len(fake.moves) != 0 {
		t.Errorf("the server was asked to move: %v", fake.moves)
	}
	ops, _ := svc.Store.RecentOps(1)
	if len(ops) != 1 || ops[0].OK {
		t.Errorf("the refusal should be logged as a failure: %+v", ops)
	}
}

func TestSyncPicksUpReadFlagsSetElsewhere(t *testing.T) {
	svc, fake, _ := setup(t)
	fake.folders["INBOX"][0].Seen = true
	if err := svc.SyncAll(); err != nil {
		t.Fatal(err)
	}
	if n := len(ids(t, svc, store.Query{Unread: true})); n != 2 {
		t.Errorf("unread = %d, want 2", n)
	}
	st, _ := svc.Store.Stats(time.Now().Add(-time.Hour))
	if st.AgentHandled+st.MeHandled != 0 {
		t.Errorf("a flag set elsewhere is nobody's handling: %+v", st)
	}
}

func TestAddAccountFailureKeepsNothing(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	sec := memSecrets{}
	svc := New(st, sec)
	bad := newFake()
	bad.fail = errors.New("login refused")
	svc.Dial = func(store.Account, string) Remote { return bad }
	if _, err := svc.AddAccount("google", "me@gmail.com", "wrong"); err == nil {
		t.Fatal("a refused login must fail")
	}
	if accs, _ := st.Accounts(); len(accs) != 0 || len(sec) != 0 {
		t.Errorf("kept %v and %v after a failed login", accs, sec)
	}
	if h, _ := HostFor("icloud", "me@icloud.com"); h != "imap.mail.me.com:993" {
		t.Errorf("icloud host = %q", h)
	}
	if h, _ := HostFor("aliyun", "me@aliyun.com"); h != "imap.aliyun.com:993" {
		t.Errorf("personal aliyun host = %q", h)
	}
	if h, _ := HostFor("aliyun", "me@corp.com"); h != "imap.qiye.aliyun.com:993" {
		t.Errorf("company aliyun host = %q", h)
	}
}

// iCloud names its folders "Sent Messages" and "Deleted Messages".
func TestICloudFolders(t *testing.T) {
	for name, want := range map[string]string{
		"INBOX": "inbox", "Sent Messages": "sent", "Archive": "archive",
		"Deleted Messages": RoleSkip, "Junk": RoleSkip, "Drafts": RoleSkip, "Notes": "other",
	} {
		if got := folderRole(name, nil); got != want {
			t.Errorf("folderRole(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestBodyMarkdown(t *testing.T) {
	md := htmlMarkdown(`<html><head><title>t</title></head><body>
<div style="display:none;max-height:0">Preview text ͏ ­</div><p>A͏­B</p>
<table><tr><td><img src="https://cdn.x.com/logo.png" alt="Logo"></td></tr>
<tr><td><p>Hi *Lin*,<br>price $5</p>
<p>See <a href="https://x.com/a b">the report</a> or <a href="https://x.com/r">https://x.com/r</a>.</p>
<a href="https://x.com/buy"><div>Buy now</div></a>
<a href="https://x.com/b"><img src="https://x.com/banner.png" alt="Banner"></a><a href="https://x.com/app">Open<img src="https://x.com/arrow.png"></a>
<img src="https://t.x.com/open.gif" width="1" height="1"><img src="https://x.com/avatar.png" width="40"><img src="cid:part1"></td></tr></table></body></html>`)
	for _, want := range []string{
		"![Logo](https://cdn.x.com/logo.png)", `Hi \*Lin\*,  ` + "\n" + `price \$5`,
		"[the report](https://x.com/a%20b)", "<https://x.com/r>", "[Buy now](https://x.com/buy)", "\n![Banner](https://x.com/banner.png)\n", "[Open](https://x.com/app)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Preview") || !strings.Contains(md, "AB") {
		t.Errorf("hidden preheader kept, or invisible fill left:\n%s", md)
	}
	if strings.Contains(md, "open.gif") || strings.Contains(md, "avatar") || strings.Contains(md, "cid:") || strings.Contains(md, "\n    ") {
		t.Errorf("tracking pixel, cid picture or code indent kept:\n%s", md)
	}
	if got := PlainText(md); !strings.Contains(got, "See the report or https://x.com/r.") || strings.Contains(got, "](") {
		t.Errorf("plain text = %q", got)
	}

	text := textMarkdown("Hello_world\n- not a list\nhttps://x.com/a_b?c=1\n\nBye")
	if !strings.Contains(text, `Hello\_world  `) || !strings.Contains(text, `\- not a list`) ||
		!strings.Contains(text, "<https://x.com/a_b?c=1>") || !strings.Contains(text, "\n\nBye") {
		t.Errorf("text markdown = %q", text)
	}
}
