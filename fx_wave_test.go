package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// waveSpan is the part of the original's clock that waveDuration covers, in
// its seconds.
const waveSpan = waveAnticipation + waveSettled*waveTravel

// waveAtSeconds is the wave at a time on the original's clock, where its
// tests have their numbers.
func waveAtSeconds(s float64) waveFront {
	return waveAt(time.Duration(s / waveSpan * float64(waveDuration)))
}

// The curve's parameter at 1/2 gives x = 0.3425, y = 0.875 by hand.
func TestWaveEasingMatchesCSS(t *testing.T) {
	if waveEase(0) != 0 || waveEase(1) != 1 {
		t.Errorf("the easing runs from %v to %v, want 0 to 1", waveEase(0), waveEase(1))
	}
	if got := waveEase(0.3425); math.Abs(got-0.875) > 1e-4 {
		t.Errorf("easing(0.3425) = %v, want 0.875", got)
	}
}

// The front covers its way on the easing, and is past the bottom exactly as
// the wave ends, by more than its bend: all of the element is then revealed.
func TestWaveFrontMovesOnTheEasing(t *testing.T) {
	end := waveAtSeconds(waveAnticipation + waveTravel).moved
	mid := waveAtSeconds(waveAnticipation + 0.3425*waveTravel).moved
	if math.Abs(mid/end-0.875) > 1e-4 {
		t.Errorf("at 0.3425 of the travel the front is %v of its way, want 0.875", mid/end)
	}
	if got := waveAt(waveDuration).moved; math.Abs(got-1) > 1e-6 {
		t.Errorf("at the end the front has moved %v, want 1", got)
	}
}

// Before the launch the ring of colour falls onto the line the front leaves
// from, gathering a sixth of the front's colour, and no content shows.
func TestWaveRingFallsInBeforeTheLaunch(t *testing.T) {
	last := math.Inf(1)
	for i := 0; i < 40; i++ {
		w := waveAtSeconds(waveAnticipation * float64(i) / 40)
		if w.rise > last || w.launched || w.moved != 0 || w.energy != 0 {
			t.Fatalf("step %d of the anticipation: %+v after a rise of %v", i, w, last)
		}
		last = w.rise
	}
	if w := waveAt(0); w.rise != 56 || w.glow != 0 {
		t.Errorf("the wave starts at %+v, want the ring 56 out and no colour", w)
	}
	if w := waveAtSeconds(waveAnticipation); w.rise > 1e-6 || math.Abs(w.glow-0.165) > 1e-6 || w.energy > 1e-6 {
		t.Errorf("the wave launches at %+v, want the ring on the top edge at 0.165, and no energy yet", w)
	}
}

// After the launch the front never retreats, and has not crossed a screen
// 0.2 s on: the original's is 874 pt tall.
func TestWaveFrontOnlyEverMovesOn(t *testing.T) {
	last := 0.0
	for i := 1; i <= 400; i++ {
		w := waveAtSeconds(waveAnticipation + waveSettled*waveTravel*float64(i)/400)
		if w.moved < last || w.rise != 0 || !w.launched {
			t.Fatalf("step %d: the front at %v after %v, risen %v", i, w.moved, last, w.rise)
		}
		last = w.moved
	}
	if w := waveAtSeconds(waveAnticipation + 0.2); w.moved*(874+82) > 874 || w.energy < 0.99 {
		t.Errorf("0.2 s after the launch: %+v, want the front on screen at full energy", w)
	}
}

// The front has crossed before the easing lets it stop, and what it carries
// must not hang there while it slows down: by the end nothing is left, so
// that switching the effect off cannot be seen.
func TestWaveDiesAsTheFrontSettles(t *testing.T) {
	for _, w := range []waveFront{waveAtSeconds(waveAnticipation + waveSettled*waveTravel), waveAt(waveDuration)} {
		if w.energy > 1e-6 || w.glow > 1e-6 || w.moved < 1-1e-6 {
			t.Errorf("at the end: %+v, want no energy, no colour and the front past the bottom", w)
		}
	}
}

// waveShot saves img in $ZMAIL_SHOTS, when it is set.
func waveShot(t *testing.T, img image.Image, name string) {
	t.Helper()
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

// waveGround is an appearance: the ground, and the text on it.
type waveGround struct {
	name        string
	dark        bool
	ground, ink ui.Color
}

var waveGrounds = []waveGround{
	{"light", false, ui.Hex("#F4F6FA"), ui.Hex("#232B38")},
	{"dark", true, ui.Hex("#22263A"), ui.Hex("#E4E8F0")},
}

const (
	wavePlain = -1 // stands for the view without the wave
	wavePast  = waveDuration + time.Millisecond
)

// waveMail is a mail's text, a column w×h on the ground it does not paint
// itself, as the reader's: through the wave at *elapsed, whose effect it
// leaves in *fx.
func waveMail(g waveGround, w, h float32, elapsed *time.Duration, fx *ui.Effect) func(c *ui.Context) {
	lines := []string{
		"Hi Lin, the review moved to Thursday 15:00.",
		"The agenda is in the doc; add what you want to cover by Wednesday.",
		"Card ending ••••, verification code ••••••: both stay on this Mac.",
		"Reply here if the new time does not work for you.",
	}
	return func(c *ui.Context) {
		ui.Box(c).Fill().Background(g.ground).Children(func() {
			col := ui.Column(c).Size(w, h).Padding(8, 16).Gap(10)
			col.Children(func() {
				for i := range int(h-16) / 28 {
					ui.Text(c, fmt.Sprintf("%d · %s", i+1, lines[i%len(lines)])).FontSize(15).SingleLine().TextColor(g.ink)
				}
			})
			if *fx = nil; *elapsed >= 0 {
				*fx = waveEffect(c, *elapsed)
			}
			col.Effect(*fx)
		})
	}
}

// waveFrames renders tt's view at each of the times at, which the view reads
// from *elapsed.
func waveFrames(tt *ui.Tester, elapsed *time.Duration, at ...time.Duration) []*image.RGBA {
	var out []*image.RGBA
	for _, e := range at {
		*elapsed = e
		tt.Frame()
		out = append(out, tt.Image())
	}
	return out
}

// waveRowsOff counts the rows from y0 up to y1 on which a and b differ by more
// than one level: an effect's layer rounds text's edges once more than the
// window does.
func waveRowsOff(a, b *image.RGBA, y0, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for i := a.PixOffset(0, y); i < a.PixOffset(0, y+1); i++ {
			if d := int(a.Pix[i]) - int(b.Pix[i]); d < -1 || d > 1 {
				n++
				break
			}
		}
	}
	return n
}

// waveBlank reports whether every pixel of rows y0 up to y1 of img is c.
func waveBlank(img *image.RGBA, y0, y1 int, c ui.Color) bool {
	for i := img.PixOffset(0, y0); i < img.PixOffset(0, y1); i += 4 {
		if [4]uint8(img.Pix[i:i+4]) != [4]uint8{c.R, c.G, c.B, 255} {
			return false
		}
	}
	return true
}

// The wave reveals text from the top: behind the front it is itself; on the
// front it is moved, split and blurred; ahead of it nothing shows; and once
// over the element draws with no effect at all.
func TestWaveEffect(t *testing.T) {
	// Tall enough that the wave's reach behind its front, about 600 DIPs,
	// leaves the top alone at half time, when the front is 904 DIPs down.
	const w, h = 40, 1400 // narrow: the headless Tester runs the shader on the CPU
	for _, g := range waveGrounds {
		elapsed := time.Duration(wavePlain)
		var fx ui.Effect
		tt := ui.NewTesterWith(waveMail(g, w, h, &elapsed, &fx), w, h, ui.TesterOptions{Dark: g.dark})
		f := waveFrames(tt, &elapsed, wavePlain, 0, waveDuration/5, waveDuration/2, waveDuration*4/5, wavePast)
		for i, name := range []string{"plain", "000", "020", "050", "080", "past"} {
			waveShot(t, f[i], "wave-text-"+g.name+"-"+name)
		}
		plain, start, fifth, half, late, past := f[0], f[1], f[2], f[3], f[4], f[5]
		if waveBlank(plain, 0, h, g.ground) {
			t.Fatalf("%s: the view shows no text", g.name)
		}
		if fx != nil {
			t.Errorf("%s: waveEffect past the end is %v, want nil", g.name, fx)
		}
		if string(past.Pix) != string(plain.Pix) {
			t.Errorf("%s: past the end the element is not drawn as it is", g.name)
		}
		// Before the launch no text shows: only the colour gathering at the top.
		if !waveBlank(start, 0, h, g.ground) || !waveBlank(fifth, 400, h, g.ground) || waveBlank(fifth, 0, 100, g.ground) {
			t.Errorf("%s: before the launch text shows, or no colour gathers at the top", g.name)
		}
		if n := waveRowsOff(half, plain, 0, 300); n > 0 {
			t.Errorf("%s: at half time %d of the top 300 rows are not yet themselves", g.name, n)
		}
		if n := waveRowsOff(half, plain, 760, 1000); n < 230 {
			t.Errorf("%s: at half time only %d of the 240 rows on the front differ", g.name, n)
		}
		if !waveBlank(half, 1080, h, g.ground) {
			t.Errorf("%s: at half time text shows ahead of the front", g.name)
		}
		// Late, the front is leaving the bottom, and what it carries is dying.
		if n := waveRowsOff(late, plain, 0, 700); n > 0 {
			t.Errorf("%s: late, %d of the top 700 rows are not themselves", g.name, n)
		}
		if waveRowsOff(late, plain, 1100, h) == 0 {
			t.Errorf("%s: late, the wave has already left", g.name)
		}
	}
}

// Over a box whose pixels cannot be moved, the wave is a veil of the ground's
// color that clears from the top, under the front's colour and hairline.
func TestWaveOverlay(t *testing.T) {
	const w, h = 40, 1400     // narrow: the headless Tester runs the shader on the CPU
	page := ui.Hex("#3366CC") // stands for a web view's page
	for _, g := range waveGrounds {
		elapsed := time.Duration(wavePlain)
		tt := ui.NewTesterWith(func(c *ui.Context) {
			ui.Box(c).Fill().Background(page).Draw(func(p *ui.Painter, r ui.Rect) {
				if elapsed >= 0 {
					// Translucent, as the reader's ground is: the veil is opaque all the same.
					waveOverlay(p, r, elapsed, ui.RGBA(g.ground.R, g.ground.G, g.ground.B, 0.5))
				}
			})
		}, w, h, ui.TesterOptions{Dark: g.dark})
		f := waveFrames(tt, &elapsed, wavePlain, 0, waveDuration/5, waveDuration/2, waveDuration*4/5, wavePast)
		for i, name := range []string{"plain", "000", "020", "050", "080", "past"} {
			waveShot(t, f[i], "wave-overlay-"+g.name+"-"+name)
		}
		plain, start, half, past := f[0], f[1], f[3], f[5]
		if !waveBlank(plain, 0, h, page) || string(past.Pix) != string(plain.Pix) {
			t.Errorf("%s: past the end the page is not bare", g.name)
		}
		if !waveBlank(start, 0, h, g.ground) {
			t.Errorf("%s: before the launch the page shows through the veil", g.name)
		}
		if !waveBlank(half, 0, 480, page) {
			t.Errorf("%s: at half time the top of the page is not clear", g.name)
		}
		if n := waveRowsOff(half, plain, 760, 1000); n < 240 {
			t.Errorf("%s: at half time only %d of the 240 rows on the front show the wave", g.name, n)
		}
		if !waveBlank(half, 1080, h, g.ground) {
			t.Errorf("%s: at half time the page shows ahead of the front", g.name)
		}
	}
}

// With less motion the element draws as it is from the start, and the
// overlay paints nothing.
func TestWaveReducedMotion(t *testing.T) {
	white := ui.Hex("#FFFFFF")
	var fx ui.Effect
	painted := false
	tt := ui.NewTesterWith(func(c *ui.Context) {
		fx = waveEffect(c, waveDuration/2)
		ui.Box(c).Fill().Background(white).Draw(func(p *ui.Painter, r ui.Rect) {
			waveOverlay(p, r, waveDuration/2, ui.Hex("#000000"))
			painted = true
		})
	}, 60, 60, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
	if fx != nil || !painted || !waveBlank(tt.Image(), 0, 60, white) {
		t.Errorf("with less motion: effect %v, painted %v, or the overlay shows", fx, painted)
	}
}

// TestWaveLook draws the wave as the reader shows it, a mail 680 DIPs wide
// at two pixels a DIP, short and long, for looking at: with $ZMAIL_SHOTS and $ZMAIL_WAVE_LOOK
// only, as the shaders run on the CPU here, seconds a frame.
func TestWaveLook(t *testing.T) {
	if os.Getenv("ZMAIL_SHOTS") == "" || os.Getenv("ZMAIL_WAVE_LOOK") == "" {
		t.Skip("set $ZMAIL_SHOTS to a directory and $ZMAIL_WAVE_LOOK to draw the frames (about three minutes)")
	}
	percents := func(tt *ui.Tester, elapsed *time.Duration, name string, at ...int) {
		for _, pc := range at {
			f := waveFrames(tt, elapsed, waveDuration*time.Duration(pc)/100)
			waveShot(t, f[0], fmt.Sprintf("%s-%03d", name, pc))
		}
	}
	for _, g := range waveGrounds {
		for _, h := range []int{120, 700} {
			var elapsed time.Duration
			var fx ui.Effect
			tt := ui.NewTesterWith(waveMail(g, 680, float32(h), &elapsed, &fx), 680, h, ui.TesterOptions{Dark: g.dark})
			tt.SetScale(2)
			percents(tt, &elapsed, fmt.Sprintf("look-text-%s-%d", g.name, h), 34, 46, 60, 75)
		}
		var elapsed time.Duration
		tt := ui.NewTesterWith(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.Hex("#FFFFFF")).Draw(func(p *ui.Painter, r ui.Rect) {
				p.Text(24, 24, "A page in a web view", 28, ui.Hex("#232B38"))
				waveOverlay(p, r, elapsed, g.ground)
			})
		}, 680, 700, ui.TesterOptions{Dark: g.dark})
		tt.SetScale(2)
		percents(tt, &elapsed, "look-overlay-"+g.name, 20, 46)
	}
}
