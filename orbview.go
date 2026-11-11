package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/orb"
)

// The Thinking orbs of Libraries.dev stand for work in progress: syncing,
// and connecting an account.
var (
	syncOrb    = orb.New(orb.Working, 20)
	connectOrb = orb.New(orb.Connecting, 20)
	orbStart   = time.Now()
)

// orbSpinner draws an orb, which keeps the window drawing frames.
func orbSpinner(c *ui.Context, p pal, o *orb.Orb) {
	s := float32(o.Size)
	ui.Box(c).Size(s, s).Draw(func(pt *ui.Painter, r ui.Rect) {
		f := o.Frame(pt.Now().Sub(orbStart).Seconds() * o.Speed)
		at := func(x, y float64) [2]float32 { return [2]float32{r.X + float32(x), r.Y + float32(y)} }
		for _, l := range f.Lines {
			pt.StrokePolyline([][2]float32{at(l.X1, l.Y1), at(l.X2, l.Y2)}, float32(l.W), orbInk(l.White, l.A, p.dark))
		}
		for _, d := range f.Dots {
			rad := float32(d.R)
			pt.Fill(ui.Rect{X: r.X + float32(d.X) - rad, Y: r.Y + float32(d.Y) - rad, W: 2 * rad, H: 2 * rad}, orbInk(d.White, d.A, p.dark), rad)
		}
		pt.AnimationFrame()
	})
}

// orbInk is a dot's ink value as gray, mirrored on dark so near dots read bright.
func orbInk(white, a float64, dark bool) ui.Color {
	if dark {
		white = 1 - white
	}
	g := uint8(math.Round(min(max(white, 0), 1) * 255))
	return ui.RGBA(g, g, g, float32(a))
}
