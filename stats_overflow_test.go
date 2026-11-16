package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/store"
)

// 8 days of mostly-unhandled mail, like the real account.
func TestEightDays(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccount(store.Account{ID: "qq", Provider: "qq", Email: "q@qq.com"}); err != nil {
		t.Fatal(err)
	}
	counts := []struct {
		daysAgo, untouched, me, agent int
	}{
		{7, 9, 1, 0}, {6, 21, 0, 0}, {5, 20, 2, 1}, {4, 28, 7, 0},
		{3, 28, 5, 0}, {2, 8, 1, 0}, {1, 16, 2, 0}, {0, 15, 0, 0},
	}
	uid := uint32(1)
	for _, d := range counts {
		date := time.Now().AddDate(0, 0, -d.daysAgo)
		total := d.untouched + d.me + d.agent
		for i := 0; i < total; i++ {
			by := ""
			switch {
			case i < d.agent:
				by = store.ActorAgent
			case i < d.agent+d.me:
				by = store.ActorMe
			}
			if err := st.UpsertMessage(store.Message{
				AccountID: "qq", Folder: "INBOX", Role: store.RoleInbox,
				UID: uid, FromName: "X", FromAddr: "x@x.com",
				Subject:   "s", Date: date, Seen: true, HandledBy: by,
			}); err != nil {
				t.Fatal(err)
			}
			uid++
		}
	}
	a := newApp(st, demoService(st))
	a.reload()
	tt := ui.NewTester(a.view, 982, 1050)
	tt.SetScale(2)
	if err := tt.Click("统计"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	shot(t, tt, "eight")
}
