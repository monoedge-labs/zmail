package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"

	"github.com/monoedge-labs/zmail/internal/store"
)

// A message with HTML shows as that HTML in a web view of its own, as any
// mail app does: the sender's layout, colors and pictures. One without keeps
// the Markdown of its body.

// mailPage is a message's web view.
type mailPage struct {
	view *mygo.WebView
	h    atomic.Int32 // the page's height, which it tells in its title
	laid atomic.Bool  // it has told its height: the page is laid out
}

// The page is someone else's and calls no Go, so it tells its height in its
// title. ponytail: a page sized by the viewport (100vh) keeps the height it
// had; measure its content when one shows up.
const (
	heightMark = "zmail-height:"
	watchPage  = `const f = () => { document.title = "` + heightMark + `" + Math.ceil(document.documentElement.getBoundingClientRect().height) };
const o = new ResizeObserver(f); o.observe(document.documentElement); o.observe(document.body); f()`
)

// pageHTML is a message's markup as a page: a policy that leaves it styles,
// pictures and fonts and no script, frame, form or plug-in, on a light
// ground, as a mail is drawn for. A mail of plain paragraphs gets room around
// its text (pagePad); one laid out in a table of its own, as newsletters and
// notifications are, keeps the whole page, to its edges.
func pageHTML(markup string) string {
	return `<!doctype html><meta charset=utf-8>` +
		`<meta http-equiv=Content-Security-Policy content="default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline' https: http:; font-src https: http: data:">` +
		`<style>` + look(markup) + `body{margin:0;padding:` + pagePad + `}` + ownLayout + `{padding:0}` + dataTable + `img{max-width:100%;height:auto}img[src^="cid:" i]{display:none}` + blurCSS + `</style>` +
		markup
}

// look is the page's ground and type. A mail with colors of its own is drawn
// for white, so it gets white. One without is Zmail's to dress: a soft ground
// and larger type, the app's own blue for links, and the dark ground of the
// system's dark appearance, as the window around it has.
func look(markup string) string {
	const font = `font:14px/1.5 -apple-system,"PingFang SC",sans-serif`
	if authored(markup) {
		return `html{background:#fff;color:#202124;color-scheme:light;` + font + `}`
	}
	return `html{background:#fcfcfd;color:#1f2430;color-scheme:light;font:15px/1.7 -apple-system,"PingFang SC",sans-serif}` +
		`a{color:#2558d9}table[border],table[border] :is(th,td){border-color:#d5dae3}th{background:#f3f5f9}` +
		`@media (prefers-color-scheme:dark){html{background:#20232f;color:#e4e8f0;color-scheme:dark}a{color:#7fa0ff}` +
		`table[border],table[border] :is(th,td){border-color:rgba(255,255,255,.2)}th{background:rgba(255,255,255,.07)}}`
}

// authoredRE finds what gives a mail colors, sizes or pictures of its own: a
// style or class, a font or color attribute, a background, a picture, or a
// table with a width. Where it finds none, the mail is plain text in markup.
var authoredRE = regexp.MustCompile(`(?i)\b(?:style|class|bgcolor|color|background)\s*=|<font\b|background|<img\b|<table\b[^>]*\bwidth\s*=`)

// styledRE finds a rule of a style block that colors a plain element, as
// `p { color: red }` does; a rule of a class cannot reach a body without classes.
var styledRE = regexp.MustCompile(`(?is)(?:^|[},\s])(?:html|body|p|a|td|th|table|div|span|li|h[1-6])\s*(?:,[^{]*)?\{[^}]*(?:color|background)`)

var styleBlockRE = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`)

// authored reports whether a mail brings colors of its own.
func authored(markup string) bool {
	body := markup
	if i := strings.Index(strings.ToLower(markup), "<body"); i >= 0 {
		body = markup[i:]
	}
	if authoredRE.MatchString(body) {
		return true
	}
	for _, m := range styleBlockRE.FindAllStringSubmatch(markup, -1) {
		if styledRE.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// pagePad is the room around the text of a mail that brings no layout.
const pagePad = "20px 24px"

// dataTable draws a table the sender gave a border attribute, which a browser
// shows as double lines with the text against them, as a plain grid.
const dataTable = `table[border]{border-collapse:collapse}table[border] :is(th,td){padding:4px 8px}`

// ownLayout selects the body of a mail laid out in a table of its own, which
// takes no room: its page is a table (or a centered one, or one in a div or
// two) and holds no paragraph, heading or list of its own beside it. A mail of
// paragraphs that ends in a table, as a report does, is plain text.
const ownLayout = `body:has(>table,>center,>div>table,>div>center,>div>div>table):not(:has(>p,>h1,>h2,>h3,>h4,>ul,>ol,>pre,>blockquote))`

// external reports whether a link goes to the browser or the mail app.
func external(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:")
}

// pageKey names a web view of a message: of its text as it is, or of one of
// the ways it is hidden. A thread keeps those it showed while it is open, so
// that switching between the text and its hidden copy loads nothing again.
// ponytail: that is two web views a message once the switch was used; they
// go when the thread closes.
type pageKey struct {
	id int64
	v  variant
}

// syncPages gives each message of the thread with HTML a web view, and
// closes those of messages no longer shown. Made here, once per message and
// way of showing it, not by the view, which only places them. A message
// still to be hidden has no HTML yet, and gets its web view when it has.
func (a *app) syncPages() {
	if a.win == nil || a.noPages { // under test: no window
		return
	}
	v := a.variant()
	keep := map[int64]bool{}
	for _, m := range a.thread {
		keep[m.ID] = true
		k := pageKey{m.ID, v}
		if m.HTML == "" || a.pages[k] != nil {
			continue
		}
		p := a.newPage(m)
		if p == nil {
			// No web view can be made: HTML mail shows as its Markdown from
			// now on, which its hidden copies so far left out.
			a.noPages = true
			clear(a.veils)
			a.veilBytes = 0
			a.win.Update(a.reload)
			break
		}
		a.pages[k] = p
	}
	for k, p := range a.pages {
		if !keep[k.id] || a.noPages {
			p.view.Destroy()
			delete(a.pages, k)
		}
	}
}

func (a *app) newPage(m store.Message) *mailPage {
	v, err := a.win.NewWebView(mygo.WebViewOptions{
		Page:              mygo.PageOptions{Untrusted: true},
		BackgroundColor:   "#ffffff",
		ScrollToContainer: true, // the wheel scrolls the thread, not the page
	})
	if err != nil {
		return nil
	}
	p := &mailPage{view: v}
	p.h.Store(120)
	pg := v.Page()
	pg.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
		e.PreventDefault()
		if n, ok := strings.CutPrefix(e.Title, heightMark); ok {
			if h, err := strconv.Atoi(n); err == nil {
				p.h.Store(int32(min(max(h, 40), 30000)))
				p.laid.Store(true)
				a.win.Invalidate()
			}
		}
	})
	// The page stays where it is; a link the user follows opens outside, once
	// the user has confirmed it (see openLink).
	pg.OnWillNavigate(func(e *mygo.NavigateEvent) {
		if e.URL == "about:blank" {
			return
		}
		e.PreventDefault()
		if e.UserInitiated {
			a.openLink(e.URL)
		}
	})
	pg.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		if req.UserInitiated {
			a.openLink(req.URL)
		}
		return nil
	})
	pg.OnDOMReady(func() { go pg.Eval(watchPage) })
	pg.LoadHTML(pageHTML(m.HTML), "")
	return p
}
