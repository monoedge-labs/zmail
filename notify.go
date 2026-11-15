package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo"

	"github.com/monoedge-labs/zmail/internal/store"
)

// noticeMax is how many notices one sync can raise: a mailbox left for a
// while gets its newest few, not a stream of banners.
const noticeMax = 5

// noticeSent, when set, is told of each notice that is ready to show, in the
// words it would carry. Only tests set it.
var noticeSent func(m store.Message, subject, snippet string)

// arrived is the service's call that mail is new: it runs on the sync's
// goroutine, and the notice is made on the main thread.
func (a *app) arrived(ms []store.Message) {
	// Fetch's order is its own: the newest few means the highest UIDs.
	slices.SortFunc(ms, func(x, y store.Message) int { return int(x.UID) - int(y.UID) })
	if len(ms) > noticeMax {
		ms = ms[len(ms)-noticeMax:]
	}
	if a.win == nil { // under test
		for _, m := range ms {
			a.notify(m)
		}
		return
	}
	mygo.RunOnMain(func() {
		for _, m := range ms {
			a.notify(m)
		}
	})
}

// noticeText is what the notice of m says, ready while nothing is hidden or
// the hidden copy is made. With sensitive values hidden (the 打码 switch) and
// the model still reading, ready is false and the mail waits in notices: a
// notice never shows what the window hides.
func (a *app) noticeText(m store.Message) (subject, snippet string, ready bool) {
	if !a.priv.View {
		return m.Subject, m.Snippet, true
	}
	e, pending := a.hide(m, false, a.variant())
	return e.subject, e.snippet, !pending
}

// notify raises the notice of a new mail, in the window's own words. The
// sender is a notice's heading and shows as it does in the mail list.
func (a *app) notify(m store.Message) {
	if a.prefs.NotifyOff {
		return
	}
	subject, snippet, ready := a.noticeText(m)
	if !ready {
		if a.notices == nil {
			a.notices = map[int64]store.Message{}
		}
		a.notices[m.ID] = m
		return
	}
	if noticeSent != nil { // set by tests
		noticeSent(m, subject, snippet)
	}
	if a.win == nil || !mygo.NotificationsSupported() { // under test
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       fmt.Sprintf("mail-%d", m.ID),
		Title:    fromTitle(m),
		Subtitle: subject,
		Body:     snippet,
		// no Group: it reaches macOS as threadIdentifier, and the thread key
		// is the subject in words — a leak the masking is there to stop.
	})
	id := m.ID
	n.OnClick(func() {
		mygo.RunOnMain(func() { a.openMail(id) })
	})
	go n.Show() // waits for the system's answer once; denied, it does nothing
}

// notices are the mail waiting on the model, flushed by each reload.
func (a *app) notifyFlush() {
	if len(a.notices) == 0 {
		return
	}
	ms := a.notices
	a.notices = nil
	for _, m := range ms {
		a.notify(m)
	}
}

// openMail shows the mail a notice was clicked for: its row when the list
// has it, else the inbox.
func (a *app) openMail(id int64) {
	a.win.Show()
	a.win.Focus()
	a.reload()
	for i, m := range a.rows {
		if m.ID == id {
			a.open(i)
			return
		}
	}
	a.navigate("mail:inbox")
	for i, m := range a.rows {
		if m.ID == id {
			a.open(i)
			return
		}
	}
}

// fromTitle is the notice's heading: the sender's name, or address.
func fromTitle(m store.Message) string {
	if m.FromName != "" {
		return m.FromName
	}
	if m.FromAddr != "" {
		return m.FromAddr
	}
	return "新邮件"
}
