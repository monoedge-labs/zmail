package main

import (
	"testing"
	"time"

	"github.com/monoedge-labs/zmail/internal/store"
)

// The notice's words follow the window's: hidden when the window hides,
// as they are when it does not.
func TestNoticeText(t *testing.T) {
	a := testApp(t)
	m := store.Message{Subject: "验证码 481516", Snippet: "致电 13800138000", Date: time.Now()}
	subj, snip, ready := a.noticeText(m)
	if !ready || subj != m.Subject || snip != m.Snippet {
		t.Fatalf("nothing hidden, noticeText = %q %q %v", subj, snip, ready)
	}
	a.priv.View = true
	subj, snip, ready = a.noticeText(m)
	if !ready || subj != "验证码 ‹CODE_1›" || snip != "致电 ‹PHONE_1›" {
		t.Fatalf("hidden, noticeText = %q %q %v", subj, snip, ready)
	}
}

// fromTitle falls back to the address, then to a bare heading.
func TestFromTitle(t *testing.T) {
	m := store.Message{FromName: "Lin", FromAddr: "lin@x.com"}
	if fromTitle(m) != "Lin" {
		t.Error(fromTitle(m))
	}
	if fromTitle(store.Message{FromAddr: "lin@x.com"}) != "lin@x.com" {
		t.Fail()
	}
	if fromTitle(store.Message{}) != "新邮件" {
		t.Fail()
	}
}

// A sync worth noticing is its newest mail by UID, however it arrived.
func TestArrivedCap(t *testing.T) {
	var shown []store.Message
	defer func(prev func(store.Message, string, string)) { noticeSent = prev }(noticeSent)
	noticeSent = func(m store.Message, _, _ string) { shown = append(shown, m) }
	a := testApp(t)
	var ms []store.Message
	for uid := uint32(10); uid >= 1; uid-- { // newest first, as a fetch may come
		ms = append(ms, store.Message{ID: int64(uid), UID: uid, Subject: "s"})
	}
	a.arrived(ms)
	if len(shown) != noticeMax || shown[0].UID != 6 || shown[noticeMax-1].UID != 10 {
		t.Fatalf("notices = %+v, want UIDs 6..10 newest", shown)
	}
}

// With notices off, none is made or kept.
func TestNotifyOff(t *testing.T) {
	var n int
	defer func(prev func(store.Message, string, string)) { noticeSent = prev }(noticeSent)
	noticeSent = func(store.Message, string, string) { n++ }
	a := testApp(t)
	a.prefs.NotifyOff = true
	a.arrived([]store.Message{{ID: 1, UID: 1, Subject: "s"}})
	if n != 0 || len(a.notices) != 0 {
		t.Fatalf("notices off still saw %d shown, %d kept", n, len(a.notices))
	}
}
