package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The wave that crosses a mail whose private values have just been hidden is
// a port of the Distortion waves of Libraries.dev (MIT, Copyright (c) 2026
// Jakub Antalik; DistortionWavesKit's WaveFront.swift and
// DistortionWaveShaders.metal), Type Vertical in the "Colorful" palette, with
// the lengths of its defaults.
//
// The original carries one screen into another, up from a button. Here there
// is one layer and no button, so the wave is a reveal: it leaves the top edge
// and travels down, a straight band with the original's slow bend along it.
// Behind the front the content shows as it is; on it the content swells, its
// colours part and it blurs, under the colour riding the front; ahead of it
// nothing shows yet. What needs a source is left out: the pull toward it, the
// dots falling into it, the lean toward a touch, and the colour the original
// leaves on the screen's edges.

// waveDuration is how long the wave takes to cross an element.
const waveDuration = 900 * time.Millisecond

// The original's clock, in its seconds: waveDuration is scaled onto the part
// of it that the front takes, so its drifts and easings keep their numbers.
const (
	waveAnticipation = 0.5  // the colour gathers at the top before the front leaves
	waveTravel       = 2.28 // from the front leaving to the end of its easing
	waveSettled      = 0.55 // the share of the travel by which the front carries nothing
)

// waveFront is where the wave is at a moment of its life, as the original's
// WaveFront: a pure function of the time, so any frame of it can be drawn.
type waveFront struct {
	time     float64 // seconds on the original's clock
	rise     float64 // before the launch: how far below the top the gathering colour is, DIPs
	moved    float64 // how far the front is on its way, 0 at the top, 1 past the bottom
	bend     float64 // how far the front bends along its length, DIPs
	energy   float64 // how strongly the swell and the blur act, 0–1
	glow     float64 // how strong the colour on the front is, 0–1
	launched bool    // the front has left the top: before that no content shows
}

func waveAt(elapsed time.Duration) waveFront {
	t := elapsed.Seconds() / waveDuration.Seconds() * (waveAnticipation + waveSettled*waveTravel)
	since := t - waveAnticipation
	x := min(max(since/waveTravel, 0), 1)
	// The front stops being drawn at waveSettled, short of the easing's end:
	// that is where it has just cleared the bottom, whatever the height.
	moved := waveEase(x) / waveEase(waveSettled)
	// The front has crossed most of its way by 0.2, and from there slows down
	// too much to carry anything off: what it carries dies away instead.
	settling := 1 - waveSmooth(0.2, waveSettled, x)
	// The original's ring of colour (Colour ring 56 pt, Ring opacity 33%):
	// it falls onto the line the front leaves from, at a sixth of the front's
	// full colour, so the colour does not pop at the launch.
	const ring, gatheredGlow = 56, 0.5 * 0.33
	gathered := waveSmooth(0, waveAnticipation, t)
	w := waveFront{
		time:     t,
		moved:    moved,
		bend:     22 * waveSmooth(0, 0.3, moved),
		energy:   waveSmooth(0, 0.05, x) * settling,
		glow:     (gatheredGlow + (1-gatheredGlow)*waveSmooth(0, 0.07, x)) * settling,
		launched: since > 0,
	}
	if !w.launched {
		w.rise, w.glow = ring*(1-gathered), gatheredGlow*gathered
	}
	return w
}

// uniforms are the wave as its shaders read it.
func (w waveFront) uniforms() map[string]any {
	return map[string]any{
		"Rise": w.rise, "Moved": w.moved, "Bend": w.bend, "Time": w.time,
		"Energy": w.energy, "Glow": w.glow, "Shown": w.launched, "Palette": wavePalette,
	}
}

// waveEase is the original's easing of the front, CSS's
// cubic-bezier(0.22, 1, 0.36, 1): off at full speed, settling softly.
func waveEase(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bezier := func(s, a, b float64) float64 { r := 1 - s; return 3*r*r*s*a + 3*r*s*s*b + s*s*s }
	// x grows with the curve's parameter, so halving always finds it.
	lo, hi := 0.0, 1.0
	for range 24 {
		if s := (lo + hi) / 2; bezier(s, 0.22, 0.36) < x {
			lo = s
		} else {
			hi = s
		}
	}
	return bezier((lo+hi)/2, 1, 1)
}

func waveSmooth(a, b, x float64) float64 {
	t := min(max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}

// wavePalette is the original's "Colorful" palette in OKLab, where its
// shader mixes colours so that neighbours never pass through grey: seven
// flowing along the front, then the pink fringe ahead of it and the blue one
// behind.
var wavePalette = waveOKLab(
	[3]float64{255, 201, 21}, [3]float64{126, 196, 255}, [3]float64{180, 40, 230}, [3]float64{235, 100, 160},
	[3]float64{255, 176, 122}, [3]float64{154, 160, 255}, [3]float64{127, 217, 238},
	[3]float64{255, 122, 182}, [3]float64{90, 190, 255},
)

// waveOKLab returns sRGB colors, 0–255 a channel, as OKLab triplets.
func waveOKLab(colors ...[3]float64) []float32 {
	linear := func(v float64) float64 {
		if c := v / 255; c > 0.04045 {
			return math.Pow((c+0.055)/1.055, 2.4)
		} else {
			return c / 12.92
		}
	}
	var out []float32
	for _, c := range colors {
		r, g, b := linear(c[0]), linear(c[1]), linear(c[2])
		l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
		m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
		s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
		out = append(out,
			float32(0.2104542553*l+0.7936177850*m-0.0040720468*s),
			float32(1.9779984951*l-2.4285922050*m+0.4505937099*s),
			float32(0.0259040371*l+0.7827717662*m-0.8086757660*s))
	}
	return out
}

// waveFieldSrc is what both shaders share: the front, and the colour on it.
// As in the original, everything reads one signed distance to the front, so
// the swell, the blur and the colour can never drift apart:
//
//	d > 0  the wave has not got here yet
//	d = 0  the front
//	d < 0  the wave has passed
const waveFieldSrc = `
var Rise float
var Moved float
var Bend float
var Time float
var Glow float
var Palette [9]vec3

const lead = 130.0    // how far the colour feathers out ahead of the front
const tail = 300.0    // how long it trails behind the front
const strength = 0.92 // its peak opacity
const ahead = 90.0    // how far ahead of the front the content comes in, and its blur gathers

// How far past the bottom the front ends: its bend, and the zone the original
// crossfades its two screens over.
const runOut = 82.0

// Where pos is along the front, from the middle of the box, in the
// original's bowed metric.
func alongFront(pos vec2) float {
	return (pos.x - 0.5*rectSize().x) * 1.4
}

func frontDistance(pos vec2) float {
	// A slow bend along the front, so it never looks drawn with a ruler. Low
	// frequencies only: anything quicker reads as a wobble at this speed.
	s := alongFront(pos) * 0.0058
	bend := 0.62*sin(s+Time*1.1) + 0.38*sin(s*2.1-Time*1.5+1.7)
	return pos.y - Rise - Moved*(rectSize().y+runOut) + Bend*bend
}

func sq(x float) float {
	return x * x
}

func oklabToSRGB(c vec3) vec3 {
	l := c.x + 0.3963377774*c.y + 0.2158037573*c.z
	m := c.x - 0.1055613458*c.y - 0.0638541728*c.z
	s := c.x - 0.0894841775*c.y - 1.2914855480*c.z
	l = l * l * l
	m = m * m * m
	s = s * s * s
	lin := clamp(vec3(
		4.0767416621*l-3.3077115913*m+0.2309699292*s,
		-1.2684380046*l+2.6097574011*m-0.3413193965*s,
		-0.0041960863*l-0.7034186147*m+1.7076147010*s), 0, 1)
	return mix(12.92*lin, 1.055*pow(lin, vec3(1/2.4))-0.055, step(vec3(0.0031308), lin))
}

// One of the palette's seven colours, mixed with the next.
func paletteAt(u float) vec3 {
	x := fract(u) * 7
	i := min(int(x), 6)
	j := i + 1
	if j == 7 {
		j = 0
	}
	m := fract(x)
	m = m * m * (3 - 2*m)
	return mix(Palette[i], Palette[j], m)
}

// The colour riding the front: the palette flowing along it, a warm fringe
// ahead, a cool one behind and a white glow between them.
func frontColor(pos vec2, d float) vec3 {
	flow := 0.62 + alongFront(pos)*0.0068*0.2 + Time*0.13 + d*0.0015 + 0.05*sin(pos.y*0.009+Time)
	lab := paletteAt(flow)
	lab = mix(lab, Palette[7], 0.5*exp(-sq((d-30)/50)))
	lab = mix(lab, Palette[8], 0.5*exp(-sq((d+84)/72)))
	lab = vec3(lab.x, lab.yz*1.22)
	lab = mix(lab, vec3(0.99, 0, 0), 0.7*exp(-sq((d+8)/26)))
	return oklabToSRGB(lab)
}

// How much of that colour covers pos. Every edge of it is wide and feathered,
// with no line anywhere; its tail swells and thins along the front, and it is
// lighter away from the box's edges.
func frontCover(pos vec2, d float) float {
	if Glow <= 0 || d >= lead {
		return 0
	}
	a := 0.0
	if d >= 0 {
		k := 1 - d/lead
		a = k * k * k * (k*(k*6-15) + 10)
	} else {
		along := alongFront(pos) * 0.0068
		lobes := 0.5 + 0.5*sin(along*1.3+Time*1.4)*cos(along*0.6-Time*0.7+1.2)
		a = pow(1-clamp(-d/(tail*mix(0.6, 1.3, lobes)), 0, 1), 2.3)
	}
	size := rectSize()
	inset := max(min(min(pos.x, size.x-pos.x), min(pos.y, size.y-pos.y)), 0)
	return a * Glow * strength * mix(0.6, 1, exp(-inset/140))
}

// Interleaved gradient noise, from -0.5 to 0.5.
func grain(pos vec2) float {
	return fract(52.9829189*fract(dot(pos*pixelScale(), vec2(0.06711056, 0.00583715)))) - 0.5
}

// The front's colour over pos, premultiplied, dithered enough to hide banding.
func frontLight(pos vec2, d float) vec4 {
	a := frontCover(pos, d)
	if a <= 0.0005 {
		return vec4(0)
	}
	n := grain(pos) * 2 / 255
	return vec4(clamp(frontColor(pos, d)*a+n, 0, 1), clamp(a+n, 0, 1))
}
`

// waveLens draws an element through the wave. A pixel reads the element's
// content once where the wave is not acting, 5 times where only its colours
// part, 25 to 55 times where it blurs (the 500 DIPs or so behind the front),
// and not at all further ahead of the front than the content comes in.
var waveLens = shader.MustCompile(`package main

var Energy float
var Shown float  // 1 once the front has left the top
var Ground float // 1 where the content stands on a light ground, 0 on a dark one
` + waveFieldSrc + `
const amplitude = 34.0   // how far the swell pushes content as it passes
const lensWidth = 150.0  // the swell's width
const aberration = 0.78  // how far red and blue part, as a share of the push
const blur = 18.0        // the blur on the front
const blurBehind = 240.0 // how far behind the front the blur clears

// The content at pos as the wave shows it. It is pushed once, the way the
// wave travels, and settles back: one smooth pulse a pixel, never a
// reversal, which at this speed would be seen as the page shaking. Its
// colours part along that push, in proportion to it, so they show exactly
// where the content is distorted. Blur gathers ahead of the front, peaks on
// it and clears slowly behind, so what the wave reveals comes into focus as
// the wave moves on.
func lens(pos vec2, d float) vec4 {
	// Steeper ahead, where the swell compresses what it is about to reach.
	w := lensWidth * 1.5
	b := blurBehind
	if d > 0 {
		w = lensWidth * 0.85
		b = ahead
	}
	push := Energy * amplitude * exp(-sq(d/w))
	sigma := Energy * blur * exp(-sq(d/b))
	if sigma < 0.25 {
		// Too little to see: and untouched content has to stay exactly itself.
		if push < 0.05 {
			return layerAt(pos)
		}
		sigma = 0
	}
	// Reads stay inside the box, as the content's edge stretched, so that
	// nothing pushed in from outside it shows as a gap.
	lo := vec2(0.5 / pixelScale())
	hi := rectSize() - lo
	from := pos - vec2(0, push)
	split := push * aberration
	// Under a quarter of a point the colours cannot be seen apart.
	if sigma == 0 && split < 0.25 {
		return layerSample(clamp(from, lo, hi))
	}

	// How much each read, from the red end to the blue end, gives to each
	// channel: a smooth split, not three ghost images.
	spectrum := [5]vec3{
		vec3(0.571, 0, 0), vec3(0.343, 0.206, 0), vec3(0.086, 0.588, 0.086),
		vec3(0, 0.206, 0.343), vec3(0, 0, 0.571),
	}
	// A blur is rings of five reads, one of each colour, on a spiral whose
	// radii follow a gaussian: more rings for a wider blur. A wide spiral is
	// turned from pixel to pixel and from frame to frame, so that reads
	// further apart than the text's strokes show as a grain the eye evens
	// out, and not as copies of the text.
	rings := 1
	if sigma > 0 {
		rings = 5 + int(sigma/3)
	}
	turn := 6.2832 * fract(grain(pos)+Time*61.8) * smoothstep(1, 3, sigma)
	// Under a wide blur the colours could hardly be seen apart, but reads
	// shared out between them would show as coloured grain: there every read
	// gives to every channel, and the colours come back together.
	even := smoothstep(2, 8, sigma)
	split *= 1 - even
	var rgb, cover vec3
	for i := 0; i < 5*rings; i++ {
		ring := i / 5
		k := i - 5*ring
		q := (float(ring) + (mod(float(k+2*ring), 5)+0.5)/5) / float(rings)
		r := sigma * sqrt(-2*log(1-q))
		a := turn + 1.2566*float(k) + 2.39996*float(ring)
		c := layerSample(clamp(from+r*vec2(cos(a), sin(a))+vec2(0, split*(0.5*float(k)-1)), lo, hi))
		w := mix(spectrum[k], vec3(0.2), even)
		rgb += c.rgb * w
		cover += c.a * w
	}
	rgb /= float(rings)
	cover /= float(rings)
	// The original splits the colours of an opaque screen. Text stands on a
	// ground it does not paint, and dark ink has no colour to part: where a
	// channel is covered less than the others, the ground's light shows in it.
	a := max(cover.x, max(cover.y, cover.z))
	return vec4(rgb+Ground*(a-cover), a)
}

func LayerEffect(pos vec2) vec4 {
	d := frontDistance(pos)
	c := frontLight(pos, d)
	shown := Shown * (1 - smoothstep(0, ahead, d))
	if shown <= 0 {
		return c
	}
	return c + (1-c.a)*shown*lens(pos, d)
}
`)

// waveLight draws what the wave shows over pixels it cannot move.
var waveLight = shader.MustCompile(`package main

var Shown float
var Base vec3
` + waveFieldSrc + `
func Fragment(dst vec4, pos vec2) vec4 {
	d := frontDistance(pos)
	c := frontLight(pos, d)
	// A hairline on the front, as the original's on the screen's edge: the
	// front's own colour, whitening where a light runs along it. It stands
	// for the swell, which here has nothing to move.
	hair := Glow * 0.9 * (1 - smoothstep(0.75, 1.5, abs(d)))
	if hair > 0 {
		shimmer := 0.5 + 0.5*sin(Time*3.4-12.566*pos.x/rectSize().x)
		c = c*(1-hair) + hair*vec4(mix(frontColor(pos, d), vec3(1), 0.95*(0.5+0.5*shimmer)), 1)
	}
	veil := 1 - Shown*(1-smoothstep(0, ahead, d))
	return c + (1-c.a)*veil*vec4(Base, 1)
}
`)

// waveEffect is the wave over an element drawn by the window itself (text),
// elapsed since it began: the element shows through it, revealed from the
// top. It is nil once the wave is over, or when the system asks for less
// motion: the element then draws as it is.
func waveEffect(c *ui.Context, elapsed time.Duration) ui.Effect {
	if elapsed >= waveDuration || c.Preferences().ReduceMotion {
		return nil
	}
	u := waveAt(elapsed).uniforms()
	u["Ground"] = !c.Theme().Dark
	return waveLens.Effect(shader.Options{Uniforms: u})
}

// waveOverlay paints the wave's light over r from a Draw callback, for a box
// whose pixels cannot be moved (a web view): the colour ring and the hairline
// travelling down, and a veil ahead of the front that clears as it passes,
// so that the page appears from the top as text does. base is the color of
// the ground the page stands on (the veil's color). It paints nothing once
// the wave is over or with less motion.
func waveOverlay(p *ui.Painter, r ui.Rect, elapsed time.Duration, base ui.Color) {
	if elapsed >= waveDuration || p.Preferences().ReduceMotion {
		return
	}
	u := waveAt(elapsed).uniforms()
	u["Base"] = base // as a vec3: the veil is opaque whatever the ground's alpha
	waveLight.Draw(p, r, shader.Options{Uniforms: u})
	p.AnimationFrame()
}
