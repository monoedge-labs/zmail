package main

import (
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/redact"
)

// A hidden value shows as blurred text of its own shape, as magpie blurs what
// it masks: a stand-in (redact.Fake) in clear glyphs made invisible, their
// shadow blurred. The value itself never reaches the window.
const blurSigma = 5

// blurCSS styles the <mark class="zm"> elements redact puts in a page.
const blurCSS = `mark.zm{display:inline-block;background:none;color:inherit;font:inherit;filter:blur(4px);user-select:none;cursor:default}`

// maskedText shows s, its placeholders as blurred stand-ins. size 0 keeps the
// default.
func maskedText(c *ui.Context, p pal, s string, size float32, weight int, ink ui.Color) ui.Element {
	chunks := redact.Chunks(s)
	var e ui.Element
	if len(chunks) == 1 && chunks[0].Kind == "" {
		e = ui.Text(c, s)
	} else {
		e = ui.RichText(c).Children(func() {
			for _, ch := range chunks {
				if ch.Kind == "" {
					ui.Text(c, ch.Text)
					continue
				}
				ui.Text(c, redact.Fake(ch.Kind, ch.N)).TextColor(ink.Alpha(1.0/255)).
					TextShadow(0, 0, blurSigma, ink.Alpha(.85)).Tooltip(redact.Chip(ch.Kind, ch.N))
			}
		})
	}
	if size > 0 {
		e = e.FontSize(size)
	}
	return e.FontWeight(weight).TextColor(ink)
}

var (
	reMDEscape = regexp.MustCompile("\\\\([\\\\`*_\\[\\]<>#~|$])")
	reAutoLink = regexp.MustCompile(`<(https?://[^>\s]+)>`)
)

// plainBody shows a plain-text body whose values are hidden: line by line, as
// it was written, with the chips in it. Its Markdown is only the escapes and
// the web addresses that body text got when it was kept.
func plainBody(c *ui.Context, p pal, body string) {
	text := reAutoLink.ReplaceAllString(reMDEscape.ReplaceAllString(body, "$1"), "$1")
	text = strings.TrimRight(strings.ReplaceAll(text, "  \n", "\n"), "\n ")
	ui.Column(c).MaxWidth(680).Children(func() {
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				ui.Box(c).Height(12)
				continue
			}
			maskedText(c, p, line, 15, 400, p.body).LineHeight(1.7)
		}
	})
}
