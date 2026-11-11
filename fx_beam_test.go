package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// beamWindow is a window holding a card and a pill, each drawing the beam at
// the time and strength its variables hold. It counts how often the view
// runs and how often the beam paints.
type beamWindow struct {
	t                 float64
	strength          float32
	builds, paintings int
}

func (w *beamWindow) view(dark bool) func(c *ui.Context) {
	bg, card, ink := ui.RGB(244, 244, 246), ui.RGB(255, 255, 255), ui.RGB(30, 30, 36)
	if dark {
		bg, card, ink = ui.RGB(16, 17, 21), ui.RGB(24, 25, 31), ui.RGB(230, 230, 236)
	}
	paint := func(radius float32) func(p *ui.Painter, r ui.Rect) {
		return func(p *ui.Painter, r ui.Rect) {
			w.paintings++
			beam(p, r, radius, w.t, w.strength, dark)
		}
	}
	return func(c *ui.Context) {
		w.builds++
		ui.Column(c).Fill().Background(bg).Padding(20).Gap(20).Children(func() {
			ui.Column(c).Size(300, 120).Radius(16).Background(card).Padding(24).Draw(paint(16)).Children(func() {
				ui.Text(c, "Design review moved to Thursday").FontSize(18).FontWeight(600).TextColor(ink)
				ui.Text(c, "The agenda is in the invite.").FontSize(13).TextColor(ink)
			})
			ui.Row(c).Size(300, 40).Radius(12).Background(card).Padding(0, 16).AlignItems(ui.Center).Draw(paint(12)).Children(func() {
				ui.Text(c, "Search mail").FontSize(13).TextColor(ink)
			})
		})
	}
}

// diffs counts the pixels of area that differ between a and b.
func diffs(a, b *image.RGBA, area image.Rectangle) (n int) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// writeBeamShot saves img in $ZMAIL_SHOTS, when it is set.
func writeBeamShot(t *testing.T, img *image.RGBA, name string) {
	dir := os.Getenv("ZMAIL_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// The beam shows around the box on a dark and a light card, moves with the
// time, fades to nothing with the strength, and rests under reduced motion.
func TestBeam(t *testing.T) {
	if beamShader == nil {
		t.Fatal("the beam's shader did not compile")
	}
	const w, h = 340, 220 // small: the headless Tester runs the shader on the CPU
	card := image.Rect(20, 20, 320, 140)
	pill := image.Rect(20, 160, 320, 200)
	edge := func(r image.Rectangle) image.Rectangle { return r.Inset(-4) } // the beam hugs the border
	var offDark *image.RGBA

	for _, dark := range []bool{true, false} {
		name := map[bool]string{true: "dark", false: "light"}[dark]
		win := &beamWindow{}
		tt := ui.NewTester(win.view(dark), w, h)
		frame := func(at float64, strength float32) *image.RGBA {
			win.t, win.strength = at, strength
			tt.Frame()
			return tt.Image()
		}

		off := frame(0.2, 0)
		if dark {
			offDark = off
		}
		if tt.FramePending() {
			t.Errorf("%s: strength 0 asks for frames", name)
		}
		var shots []*image.RGBA
		for i, at := range []float64{0.2, 0.9, 1.5} {
			img := frame(at, 1)
			if !tt.FramePending() {
				t.Errorf("%s: strength 1 asks for no frame", name)
			}
			for what, r := range map[string]image.Rectangle{"card": card, "pill": pill} {
				if n := diffs(img, off, edge(r)); n < 40 {
					t.Errorf("%s %s at %.1fs: only %d pixels near the border differ from strength 0", name, what, at, n)
				}
			}
			shots = append(shots, img)
			writeBeamShot(t, img, fmt.Sprintf("beam-%s-%d", name, i+1))
		}
		for i := 1; i < len(shots); i++ {
			if n := diffs(shots[0], shots[i], card); n < 40 {
				t.Errorf("%s: frames 1 and %d differ by only %d pixels of the card", name, i+1, n)
			}
		}

		// Half the strength is a fainter frame, not another one.
		half := frame(0.9, 0.5)
		if n := diffs(half, shots[1], card); n == 0 {
			t.Errorf("%s: strength 0.5 paints what 1 does", name)
		}

		// The pixels inside, away from the edge, stay as the card painted them.
		if n := diffs(shots[0], off, card.Inset(40)); n != 0 {
			t.Errorf("%s: %d pixels deep inside the card changed", name, n)
		}
	}

	// Under reduced motion the beam stands still: the same frame at any
	// time, and no frame asked for.
	win := &beamWindow{strength: 1}
	tt := ui.NewTesterWith(win.view(true), w, h, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	win.t = 0.3
	tt.Frame()
	still := tt.Image()
	win.t = 1.1
	tt.Frame()
	if n := diffs(still, tt.Image(), card); n != 0 {
		t.Errorf("reduced motion: the beam moved (%d pixels)", n)
	}
	if tt.FramePending() {
		t.Error("reduced motion: the beam asks for frames")
	}
	if n := diffs(still, offDark, edge(card)); n < 40 {
		t.Errorf("reduced motion: no beam shows (%d pixels)", n)
	}
	writeBeamShot(t, still, "beam-dark-still")

	// A moving beam repaints without building the view again.
	win = &beamWindow{strength: 1}
	tt = ui.NewTester(win.view(true), w, h)
	builds, paintings := win.builds, win.paintings
	for range 10 {
		win.t += 0.016
		tt.Advance(16 * time.Millisecond)
	}
	t.Logf("10 frames of a moving beam: view built %d times, beam painted %d times", win.builds-builds, win.paintings-paintings)
}
