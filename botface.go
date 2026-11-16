package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

// The agent has a face on the statistics page: a Bot avatar of Libraries.dev
// (MIT, assets/botavatars.js) in a web view of its own. It works while the
// agent called a tool a moment ago, idles while it did in the range shown,
// and sleeps otherwise.

const (
	faceSize = 40 // the avatar
	faceBox  = 64 // the disc it sits in, which also keeps its jumps inside
	faceBusy = 2 * time.Minute
)

// agentFace is the web view, and the state its page shows.
type agentFace struct {
	view  *mygo.WebView
	state string
	size  int
	css   string     // the ground its page last drew: the window's base, standing in for the card
	look  [2]float32 // the pointer last sent to it, client DIPs
}

// faceState is what the agent is doing: a state of the avatar.
func faceState(now, last time.Time, ops int) string {
	switch {
	case !last.IsZero() && now.Sub(last) < faceBusy:
		return "working"
	case ops > 0:
		return "default"
	}
	return "sleeping"
}

func faceProps(state string, size int) string {
	return fmt.Sprintf(`{type:"clover",size:%d,state:%q,jumpHeight:8,jumpLean:3}`, size, state)
}

// facePage is the page of the face: the avatar centered on the ground css
// gives it. The hole the web view paints clears everything under its box,
// card included, so the ground the face stands on is the page's own.
func facePage(state string, size int, css string) string {
	js, _ := assets.ReadFile("assets/botavatars.js")
	return `<!doctype html><meta charset=utf-8><style>html,body{margin:0;overflow:hidden}` +
		`#r{width:100vw;height:100vh;display:flex;align-items:center;justify-content:center;background:` + css + `}</style><div id=r></div>` +
		`<script>` + strings.ReplaceAll(string(js), "</script", `<\/script`) + `</script>` +
		`<script>setAvatar(` + faceProps(state, size) + `)</script>`
}

// faceCSS is the page's ground as CSS: the window's base color, which
// stands in for the card a web view's hole clears to reach it — the hole
// shows whatever is behind the window, not the card's tinted material.
func faceCSS(p pal) string {
	return fmt.Sprintf("#%02x%02x%02x", p.base.R, p.base.G, p.base.B)
}

// syncFace makes the face once a view asks for it, and keeps its state
// with the statistics. faceView calls it: reload can run with no window,
// where the face cannot be made.
func (a *app) syncFace() {
	if a.win == nil { // under test: no window
		return
	}
	ops := 0
	for _, n := range a.stats.AgentOps {
		ops += n
	}
	state := faceState(time.Now(), a.stats.AgentLast, ops)
	if a.face == nil {
		v, err := a.win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{Untrusted: true}, Transparent: true})
		if err != nil {
			return // the page shows without a face
		}
		a.face = &agentFace{view: v, state: state, size: faceSize}
		return // faceView draws its page with the ground it is placed on
	}
	if a.face.state != state {
		a.face.state = state
		faceEval(a.face.view, "setAvatar("+faceProps(state, a.face.size)+")")
	}
}

// faceView places the face at size avatar in a disc of size box, and keeps
// its page to match when either differs from what was drawn last. It returns
// the web view's element, for the caller to point the face at.
func (a *app) faceView(c *ui.Context, box, avatar float32) ui.Element {
	a.syncFace()
	if a.face == nil {
		return ui.Element{}
	}
	p := palFor(theme.TokensOf(c).Dark)
	css := faceCSS(p)
	if a.face.css == "" {
		// The page has yet to be drawn: it opens with the ground it is
		// placed on, so an Eval after it cannot race its load.
		a.face.css, a.face.size = css, int(avatar)
		a.face.view.Page().LoadHTML(facePage(a.face.state, a.face.size, css), "")
	} else if a.face.css != css {
		a.face.css = css
		faceEval(a.face.view, "document.getElementById('r').style.background='"+css+"'")
	}
	if a.face.size != int(avatar) {
		a.face.size = int(avatar)
		faceEval(a.face.view, "setAvatar("+faceProps(a.face.state, a.face.size)+")")
	}
	// coreWebView aligns itself Stretch, which a column would fill across;
	// the face keeps to its box. The page paints its own ground, so the
	// element needs only the hole's corners.
	wv := ui.WebView(c, a.face.view).Size(box, box).AlignSelf(ui.Center).Radius(box / 2)
	return wv
}

// faceEval updates the live page. A face made before the window has no
// page to update yet; faceView loads it with its recorded state.
var faceEval = func(v *mygo.WebView, js string) {
	go v.Page().Eval(js)
}

// faceLook feeds the avatar's page the pointer, in its client DIPs: the
// avatar in botavatars.js reads oo.x/oo.y, set by its own listener while the
// pointer is over the web view. Moving it elsewhere the page cannot hear, so
// an element that can, asks for the pointer and this passes it: its local
// position plus its bounds, and the web view's bounds, both in window DIPs.
func (a *app) faceLook(el, wv ui.Element) {
	if a.face == nil {
		return
	}
	x, y, _ := el.PointerPosition()
	b := el.Bounds()
	a.faceLookAt(b.X+x, b.Y+y, wv)
}

// faceLookAt maps window-local pointer coordinates into the face's page.
func (a *app) faceLookAt(x, y float32, wv ui.Element) {
	if a.face == nil {
		return
	}
	w := wv.Bounds()
	nx, ny := x-w.X, y-w.Y
	if a.face.look == [2]float32{nx, ny} {
		return
	}
	if a.face.view == nil { // headless: record the mapped point only
		a.face.look = [2]float32{nx, ny}
		return
	}
	a.face.look = [2]float32{nx, ny}
	faceEval(a.face.view, fmt.Sprintf("oo.x=%g;oo.y=%g", nx, ny))
}

// faceLookAway is faceLook for a pointer out of the window: the page loses
// sight of it, as its own blur and pointerleave listeners would do.
func (a *app) faceLookAway() {
	if a.face == nil {
		return
	}
	if a.face.view == nil { // headless: record only
		a.face.look = [2]float32{float32(math.NaN()), float32(math.NaN())}
		return
	}
	if math.IsNaN(float64(a.face.look[0])) {
		return
	}
	a.face.look = [2]float32{float32(math.NaN()), float32(math.NaN())}
	faceEval(a.face.view, "oo.x=NaN;oo.y=NaN")
}
