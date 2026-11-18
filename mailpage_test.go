package main

import (
	"strings"
	"testing"
)

func TestPageHTML(t *testing.T) {
	got := pageHTML(`<table><tr><td>Hi</td></tr></table><script>alert(1)</script>`)
	csp := strings.Index(got, "Content-Security-Policy")
	if csp < 0 || csp > strings.Index(got, "<table>") || strings.Contains(got[:csp], "<script") {
		t.Fatalf("the policy must come before the message: %q", got)
	}
	if strings.Contains(got[csp:strings.Index(got, "<style>")], "script-src") || !strings.Contains(got, "default-src 'none'") {
		t.Errorf("scripts must fall under default-src 'none': %q", got)
	}
}

func TestExternal(t *testing.T) {
	for u, want := range map[string]bool{
		"https://a.com/x": true, "HTTP://a.com": true, "mailto:a@b.c": true,
		"javascript:alert(1)": false, "file:///etc/passwd": false, "data:text/html,x": false, "about:blank#x": false,
	} {
		if external(u) != want {
			t.Errorf("external(%q) = %v", u, !want)
		}
	}
}

// A mail sized by the viewport asks for viewport + k forever: the second
// equal growth is refused and the height holds, at minHolds from the start.
func TestPageHeightTracking(t *testing.T) {
	p := &mailPage{}
	p.h.Store(120)
	p.report(520)  // k = 400
	p.report(920)  // viewport 520: asks +400 again — held at minHolds
	p.report(1040) // viewport now 640: still +400 — ignored
	if got := p.h.Load(); got != minHolds {
		t.Fatalf("tracking page kept growing to %d", got)
	}
	p.report(300) // a real shrink still applies
	if got := p.h.Load(); got != 300 {
		t.Fatalf("shrink ignored: %d", got)
	}
	// A normal page grows with its content as its pictures come in.
	p = &mailPage{}
	p.h.Store(120)
	for _, n := range []int{700, 900, 900, 1100} {
		p.report(n)
	}
	if got := p.h.Load(); got != 1100 {
		t.Fatalf("legit growth clipped: %d", got)
	}
}
