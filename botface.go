package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

func faceProps(state string) string {
	return fmt.Sprintf(`{type:"clover",size:%d,state:%q,jumpHeight:8,jumpLean:3}`, faceSize, state)
}

// facePage is the page of the face: the avatar centered on a solid ground,
// since a web view cuts a hole in the window and shows no card behind a
// clear page. The view rounds the page into a disc.
func facePage(state string) string {
	js, _ := assets.ReadFile("assets/botavatars.js")
	return `<!doctype html><meta charset=utf-8><style>html,body{margin:0;overflow:hidden;background:#E7EEFC}` +
		`@media(prefers-color-scheme:dark){html,body{background:#22304F}}` +
		`#r{width:100vw;height:100vh;display:flex;align-items:center;justify-content:center}</style><div id=r></div>` +
		`<script>` + strings.ReplaceAll(string(js), "</script", `<\/script`) + `</script>` +
		`<script>setAvatar(` + faceProps(state) + `)</script>`
}

// syncFace makes the face once the statistics page shows, and keeps its
// state with the statistics. Made here, not by the view, which places it.
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
		if a.router.Path() != "/stats" {
			return
		}
		v, err := a.win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{Untrusted: true}, BackgroundColor: "light-dark(#E7EEFC, #22304F)"})
		if err != nil {
			return // the page shows without a face
		}
		a.face = &agentFace{view: v, state: state}
		v.Page().LoadHTML(facePage(state), "")
		return
	}
	if a.face.state != state {
		a.face.state = state
		go a.face.view.Page().Eval("setAvatar(" + faceProps(state) + ")")
	}
}

// faceView places the face.
func (a *app) faceView(c *ui.Context) {
	if a.face != nil {
		ui.WebView(c, a.face.view).Size(faceBox, faceBox).Radius(faceBox / 2)
	}
}
