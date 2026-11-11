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

// A mail of plain paragraphs gets room around its text; the rule that takes
// it away is for a mail laid out in its own table.
func TestPagePadding(t *testing.T) {
	got := pageHTML(`<h2>Notice</h2><p>Hi</p>`)
	if !strings.Contains(got, "body{margin:0;padding:"+pagePad+"}") || !strings.Contains(got, ownLayout+"{padding:0}"+dataTable) {
		t.Errorf("no room around plain text: %q", got)
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
