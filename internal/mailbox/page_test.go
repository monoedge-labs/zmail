package mailbox

import (
	"strings"
	"testing"
)

// A mail of plain paragraphs gets room around its text; the rule that takes
// it away is for a mail laid out in its own table.
func TestPagePadding(t *testing.T) {
	got := PageHTML(`<h2>Notice</h2><p>Hi</p>`, "")
	if !strings.Contains(got, "body{margin:0;padding:"+pagePad+"}") || !strings.Contains(got, ownLayout+"{padding:0}"+dataTable) {
		t.Errorf("no room around plain text: %q", got)
	}
}

// A mail with colors of its own keeps the white it is drawn for; one without
// gets the app's ground, in the system's dark appearance too.
func TestAuthored(t *testing.T) {
	for markup, want := range map[string]bool{
		`<p>Hi</p><table border="1"><tr><td>x</td></tr></table>`:          false,
		`<style>.red{color:red}</style><body><p>Hi <a href="x">y</a></p>`: false, // a class nothing uses
		`<p style="color:#333">Hi</p>`:                                    true,
		`<p class="a">Hi</p>`:                                             true,
		`<table width="600"><tr><td>x</td></tr></table>`:                  true,
		`<table bgcolor="#eee"><tr><td>x</td></tr></table>`:               true,
		`<font color=red>x</font>`:                                        true,
		`<style>p{color:#333}</style><p>Hi</p>`:                           true,
		`<style>body { background: #f4f4f4 }</style><body><p>Hi</p>`:      true,
		`<p>Hi</p><img src="https://a/b.png">`:                            true,
		`<body bgcolor="#fff"><p>Hi</p>`:                                  true,
	} {
		if authored(markup) != want {
			t.Errorf("authored(%q) = %v", markup, !want)
		}
	}
	plain, styled := PageHTML(`<p>Hi</p>`, ""), PageHTML(`<p style="color:red">Hi</p>`, "")
	if !strings.Contains(plain, "prefers-color-scheme:dark") || strings.Contains(styled, "prefers-color-scheme") {
		t.Error("only a plain mail follows the dark appearance")
	}
	if !strings.Contains(styled, "background:#fff") {
		t.Error("a mail with its own colors keeps white")
	}
}
