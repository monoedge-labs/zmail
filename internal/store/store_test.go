package store

import (
	"testing"
	"time"
)

func seed(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertAccount(Account{ID: "g", Provider: "google", Email: "me@gmail.com", Host: "imap.gmail.com:993"}))
	must(s.SetFolder("g", "INBOX", RoleInbox))
	must(s.SetFolder("g", "Archive", RoleArchive))
	now := time.Now()
	must(s.UpsertMessage(Message{AccountID: "g", Folder: "INBOX", Role: RoleInbox, UID: 1, ThreadKey: "review",
		FromName: "Lin", FromAddr: "lin@x.com", Subject: "Design review", Body: "Thursday 3pm?", Date: now, Category: CatPerson}))
	must(s.UpsertMessage(Message{AccountID: "g", Folder: "INBOX", Role: RoleInbox, UID: 2, ThreadKey: "review",
		FromName: "Lin", FromAddr: "lin@x.com", Subject: "Re: Design review", Body: "or Friday", Date: now.Add(time.Minute), Category: CatPerson}))
	must(s.UpsertMessage(Message{AccountID: "g", Folder: "INBOX", Role: RoleInbox, UID: 3, ThreadKey: "ci",
		FromName: "CI", FromAddr: "noreply@ci.dev", Subject: "Build passed", Body: "100% green", Date: now, Category: CatNotification}))
	return s
}

func TestSearchAndThread(t *testing.T) {
	s := seed(t)
	got, err := s.Search(Query{Text: "friday"})
	if err != nil || len(got) != 1 || got[0].UID != 2 {
		t.Fatalf("search body: %v %v", got, err)
	}
	if got[0].Body != "" {
		t.Error("search results must not carry bodies")
	}
	if got, _ := s.Search(Query{Text: "100%"}); len(got) != 1 {
		t.Errorf("a percent sign in the text must match literally, got %d", len(got))
	}
	if got, _ := s.Search(Query{Text: "%"}); len(got) != 1 {
		t.Errorf("a lone percent sign matches only the message containing one, got %d", len(got))
	}
	first, _ := s.Search(Query{Category: CatPerson})
	th, err := s.Thread(first[0].ID)
	if err != nil || len(th) != 2 || th[0].UID != 1 {
		t.Fatalf("thread: %v %v", th, err)
	}
}

func TestHandledByKeepsFirstActor(t *testing.T) {
	s := seed(t)
	all, _ := s.Search(Query{Unread: true})
	if len(all) != 3 {
		t.Fatalf("unread = %d, want 3", len(all))
	}
	id := all[0].ID
	if err := s.MarkSeen([]int64{id}, true, ActorAgent); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(id, "Archive", RoleArchive, 9, ActorMe); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Get(id)
	if m.HandledBy != ActorAgent || !m.Seen || m.Role != RoleArchive || m.UID != 9 {
		t.Errorf("message = %+v", m)
	}
	st, err := s.Stats(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if st.Received != 3 || st.Unread != 2 || st.AgentHandled != 1 || st.MeHandled != 0 {
		t.Errorf("stats = %+v", st)
	}
	if len(st.Days) != 1 || st.Days[0].Agent != 1 || st.Days[0].Untouched != 2 {
		t.Errorf("days = %+v", st.Days)
	}
}

func TestUpsertKeepsHandledBy(t *testing.T) {
	s := seed(t)
	all, _ := s.Search(Query{})
	s.MarkSeen([]int64{all[0].ID}, true, ActorMe)
	m := all[0]
	m.Seen = true
	if err := s.UpsertMessage(m); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(all[0].ID)
	if got.HandledBy != ActorMe {
		t.Errorf("a sync overwrote handled_by: %q", got.HandledBy)
	}
}

func TestOps(t *testing.T) {
	s := seed(t)
	s.LogOp(Op{Actor: ActorAgent, Client: "claude-code", Tool: "search_mail", OK: true})
	s.LogOp(Op{Actor: ActorAgent, Client: "claude-code", Tool: "mark_read", OK: true})
	s.LogOp(Op{Actor: ActorMe, Client: "ui", Tool: "archive", OK: true})
	ops, _ := s.RecentOps(10)
	if len(ops) != 3 || ops[0].Tool != "archive" {
		t.Errorf("ops = %+v", ops)
	}
	c, _ := s.Clients(time.Now().Add(-time.Hour))
	if c["claude-code"] != 2 || len(c) != 1 {
		t.Errorf("clients = %v", c)
	}
}

// A message keeps its markup for the window, which Get and Thread return and
// a list leaves out.
func TestHTMLKept(t *testing.T) {
	s := seed(t)
	if err := s.UpsertMessage(Message{AccountID: "g", Folder: "INBOX", Role: RoleInbox, UID: 4, ThreadKey: "news",
		FromName: "N", FromAddr: "n@x.com", Subject: "Digest", Body: "hi", HTML: "<b>hi</b>", Date: time.Now(), Category: CatNewsletter}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Search(Query{Category: CatNewsletter})
	if len(list) != 1 || list[0].HTML != "" {
		t.Fatalf("a list must leave the markup out: %+v", list)
	}
	m, err := s.Get(list[0].ID)
	if err != nil || m.HTML != "<b>hi</b>" {
		t.Fatalf("Get = %+v, %v", m, err)
	}
	if th, _ := s.Thread(m.ID); len(th) != 1 || th[0].HTML != "<b>hi</b>" {
		t.Fatalf("Thread = %+v", th)
	}
}

// Junk is stored and found by asking for it, and stays out of everything else.
func TestJunkApart(t *testing.T) {
	s := seed(t)
	if err := s.SetFolder("g", "Junk", RoleJunk); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(Message{AccountID: "g", Folder: "Junk", Role: RoleJunk, UID: 1, ThreadKey: "review",
		FromName: "Spam", FromAddr: "x@spam.biz", Subject: "Re: Design review", Body: "friday prize", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Search(Query{Text: "friday"})
	if len(got) != 1 || got[0].Role == RoleJunk {
		t.Errorf("default search = %+v, want the inbox mail only", got)
	}
	got, _ = s.Search(Query{Role: RoleJunk})
	if len(got) != 1 || got[0].FromAddr != "x@spam.biz" {
		t.Errorf("junk search = %+v", got)
	}
	inbox, _ := s.Search(Query{Role: RoleInbox, Text: "Design review"})
	th, _ := s.Thread(inbox[0].ID)
	for _, m := range th {
		if m.Role == RoleJunk {
			t.Errorf("junk in a real thread: %+v", m)
		}
	}
	th, _ = s.Thread(got[0].ID)
	if len(th) != 1 || th[0].Role != RoleJunk {
		t.Errorf("a junk thread = %+v, want the junk message alone", th)
	}
	if st, _ := s.Stats(time.Now().AddDate(0, 0, -1)); st.Received != 3 {
		t.Errorf("received = %d, junk must not count", st.Received)
	}
}
