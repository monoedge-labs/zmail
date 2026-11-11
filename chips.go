package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/redact"
)

// A hidden value shows as a chip: its kind's name and number on a tint of the
// kind's color, as magpie shows what it masked. The palette is magpie's, in
// its light and dark forms; a kind keeps one slot of it everywhere.

var chipSlot = map[redact.Kind]int{
	redact.Secret: 0, redact.Password: 0, redact.Email: 1, redact.Phone: 2,
	redact.ID: 6, redact.Card: 6, redact.Account: 6, redact.Salary: 3, redact.Amount: 3,
	redact.Person: 4, redact.Address: 5, redact.Code: 5,
}

var (
	chipLight = []string{"#4f46e5", "#0ea5e9", "#10b981", "#f59e0b", "#ec4899", "#8b5cf6", "#64748b"}
	chipDark  = []string{"#a5b4fc", "#7dd3fc", "#6ee7b7", "#fcd34d", "#f9a8d4", "#c4b5fd", "#94a3b8"}
)

func chipColor(p pal, k redact.Kind) ui.Color {
	slot, ok := chipSlot[k]
	if !ok {
		slot = 6
	}
	if p.dark {
		return ui.Hex(chipDark[slot])
	}
	return ui.Hex(chipLight[slot])
}

// chipCSS styles the <mark class="zm"> elements redact puts in a page, which
// is always on a light ground.
func chipCSS() string {
	var b strings.Builder
	b.WriteString(`mark.zm{--k:` + chipLight[6] + `;padding:1px 4px;border-radius:4px;color:inherit;font:inherit;` +
		`background:color-mix(in srgb,var(--k) 16%,transparent);box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--k) 30%,transparent)}`)
	for k, slot := range chipSlot {
		fmt.Fprintf(&b, `mark.zm[data-k=%s]{--k:%s}`, k, chipLight[slot])
	}
	return b.String()
}

// maskedText shows s, its placeholders as chips. size 0 keeps the default.
func maskedText(c *ui.Context, p pal, s string, size float32, weight int, ink ui.Color) ui.Element {
	chunks := redact.Chunks(s)
	var e ui.Element
	if len(chunks) == 1 && chunks[0].Kind == "" {
		e = ui.Text(c, s)
	} else {
		spans := make([]ui.Span, 0, len(chunks))
		for _, ch := range chunks {
			if ch.Kind == "" {
				spans = append(spans, ui.Span{Text: ch.Text})
				continue
			}
			spans = append(spans, ui.Span{Text: redact.Chip(ch.Kind, ch.N), Background: chipColor(p, ch.Kind).Alpha(.16),
				Radius: 4, PaddingX: 4, PaddingY: 1})
		}
		e = ui.RichText(c, spans...)
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
