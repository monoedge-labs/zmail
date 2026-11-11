package main

import (
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
// ground, as a mail is drawn for.
func pageHTML(markup string) string {
	return `<!doctype html><meta charset=utf-8>` +
		`<meta http-equiv=Content-Security-Policy content="default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline' https: http:; font-src https: http: data:">` +
		`<style>html{background:#fff;color:#202124;color-scheme:light;font:14px/1.5 -apple-system,"PingFang SC",sans-serif}body{margin:0}img{max-width:100%;height:auto}img[src^="cid:" i]{display:none}` + chipCSS() + `</style>` +
		markup
}

// external reports whether a link goes to the browser or the mail app.
func external(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:")
}

// syncPages gives each message of the thread with HTML a web view, and
// closes those of messages no longer shown. Made here, once per message,
// not by the view, which only places them.
func (a *app) syncPages() {
	if a.win == nil { // under test: no window
		return
	}
	keep := map[int64]bool{}
	for _, m := range a.thread {
		if m.HTML == "" {
			continue
		}
		keep[m.ID] = true
		if a.pages[m.ID] == nil {
			if p := a.newPage(m); p != nil {
				a.pages[m.ID] = p
			}
		}
	}
	for id, p := range a.pages {
		if !keep[id] {
			p.view.Destroy()
			delete(a.pages, id)
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
		return nil // the message shows as Markdown
	}
	p := &mailPage{view: v}
	p.h.Store(120)
	pg := v.Page()
	pg.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
		e.PreventDefault()
		if n, ok := strings.CutPrefix(e.Title, heightMark); ok {
			if h, err := strconv.Atoi(n); err == nil {
				p.h.Store(int32(min(max(h, 40), 30000)))
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
