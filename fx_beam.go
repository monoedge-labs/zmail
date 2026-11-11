package main

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The border beam of Libraries.dev (packages/border-beam, MIT, Copyright (c)
// 2026 Jakub Antalik), Rotate family, Large, Colorful, in its dark and light
// renditions, as one fragment shader. Its three layers, a 1px colored
// stroke, an inner glow and a white bloom, all show through a window that
// turns around the box once every 1.96 s while the colors drift 30 degrees
// out and back every 12 s. The numbers are those of the library's
// spec/beam-spec.json; the shader follows its rotateShader.ts.
const (
	beamTurn  = 1.96 // seconds a lap takes
	beamDrift = 12.0 // seconds the hue takes to go out and back
	beamHue   = 30.0 // degrees the hue drifts either way
	beamBrite = 1.3  // CSS brightness() of the stroke and the inner glow
	// beamStill is the time of the frame shown under reduced motion: the
	// beam at the top right, the hue at its middle.
	beamStill = 3.0
)

// beamShader is compiled once. Its tables hold stops as (position in turns,
// alpha in dark, alpha in light); a table shorter than its array ends in
// zeros, which ramp skips.
var beamShader = shader.MustCompile(`package main

const Tau = 6.28318530718

var Angle float   // where the beam is, in turns
var Radius float  // corner radius of the box
var Dark float    // 1 for the dark rendition, 0 for the light
var Filter mat3   // hue-rotate, brightness and saturate, by columns
var Opacity vec3  // stroke, inner glow and bloom, with the strength

// ramp is a CSS conic gradient's stops: alpha linear between them.
func ramp(st [13]vec3, t float) vec2 {
	r := st[0].yz
	for i := 1; i < 13; i++ {
		if st[i].x > st[i-1].x && t > st[i-1].x {
			r = mix(st[i-1].yz, st[i].yz, clamp((t-st[i-1].x)/(st[i].x-st[i-1].x), 0, 1))
		}
	}
	return r
}

// blobs are the nine radial gradients behind the beam, the first on top:
// where they sit in the box, their radii in DIPs, their colors.
func blobs(p, size vec2, grow, alpha float) vec4 {
	at := [9]vec4{
		vec4(0.33, -0.074, 70, 40), vec4(0.12, -0.05, 60, 35), vec4(0.021, 0.683, 40, 70),
		vec4(0.021, 0.683, 20, 35), vec4(0.744, 1, 180, 32), vec4(0.55, 1, 85, 26),
		vec4(0.939, 0, 74, 32), vec4(1, 0.271, 26, 42), vec4(1, 0.271, 52, 48),
	}
	rgb := [9]vec3{
		vec3(255, 50, 100), vec3(40, 140, 255), vec3(50, 200, 80),
		vec3(30, 185, 170), vec3(100, 70, 255), vec3(40, 140, 255),
		vec3(255, 120, 40), vec3(240, 50, 180), vec3(180, 40, 240),
	}
	var acc vec4
	for i := 0; i < 9; i++ {
		d := length((p - at[i].xy*size) / (at[i].zw * grow))
		a := alpha * clamp(1-d, 0, 1)
		acc += vec4(rgb[i]/255*a, a) * (1 - acc.a)
	}
	return acc
}

func rrect(p, half vec2, r float) float {
	q := abs(p) - half + vec2(r)
	return length(max(q, vec2(0))) + min(max(q.x, q.y), 0) - r
}

// cover is how much of a pixel a half-plane x DIPs deep covers, px wide.
func cover(x, px float) float {
	return clamp(x/px+0.5, 0, 1)
}

func filter(c vec4) vec4 {
	if c.a < 0.0001 {
		return vec4(0)
	}
	return vec4(clamp(Filter*(c.rgb/c.a), vec3(0), vec3(1))*c.a, c.a)
}

func over(top, under vec4) vec4 {
	return top + under*(1-top.a)
}

func Fragment(dst vec4, pos vec2) vec4 {
	size := rectSize()
	half := size / 2
	px := 1 / pixelScale()
	depth := -rrect(pos-half, half, min(Radius, min(half.x, half.y)))
	inside := cover(depth, px)

	// The inner glow fades out 28 DIPs from the edges; beyond, nothing is drawn.
	ev := max(max(1-pos.y/28, 1-(size.y-pos.y)/28), 0)
	eh := max(max(1-pos.x/28, 1-(size.x-pos.x)/28), 0)
	fade := ev + eh - ev*eh
	if fade <= 0 || inside <= 0 {
		return vec4(0)
	}

	d := pos - half
	t := fract(fract(atan2(d.x, -d.y)/Tau+1) - Angle)

	// The window the beam shows through, shared by every layer.
	window := ramp([13]vec3{
		vec3(0, 0, 0), vec3(0.30, 0, 0), vec3(0.36, 0.1, 0.1), vec3(0.44, 0.35, 0.35), vec3(0.52, 1, 1),
		vec3(0.80, 1, 1), vec3(0.86, 0.35, 0.35), vec3(0.92, 0.1, 0.1), vec3(0.95, 0, 0), vec3(1, 0, 0),
	}, t).x
	if window <= 0 {
		return vec4(0)
	}
	ink := vec3(Dark) // white on dark, black on light

	// Inner glow: the blobs, thinner, with the inset shadow of
	// "inset 0 0 9px 1px" over them. The blur is a logistic, which is a
	// normal curve to within 1%.
	glow := blobs(pos, size, 0.9, 0.45)
	shade := mix(0.14, 0.27, Dark) / (1 + exp(1.702*(depth-1)/4.5))
	glow = over(vec4(ink*shade, shade), glow)
	glow = filter(glow) * (inside * window * fade * Opacity.y)

	// A 1 DIP ring along the edge carries the stroke and the bloom.
	ring := cover(depth, px) - cover(depth-1, px)
	if ring <= 0 {
		return glow
	}

	// Stroke: the blobs under the white (or black) spot.
	spot := ramp([13]vec3{
		vec3(0, 0, 0), vec3(0.54, 0, 0), vec3(0.57, 0.1, 0.08), vec3(0.60, 0.3, 0.2), vec3(0.63, 0.6, 0.4),
		vec3(0.66, 0.75, 0.55), vec3(0.69, 0.6, 0.4), vec3(0.72, 0.3, 0.2), vec3(0.75, 0.1, 0.08),
		vec3(0.78, 0, 0), vec3(1, 0, 0),
	}, t)
	a := mix(spot.y, spot.x, Dark)
	line := over(vec4(ink*a, a), blobs(pos, size, 1, 1))
	line = filter(line) * (ring * window * Opacity.x)

	// Bloom: CSS blurs the gradient by 8 DIPs and masks the blur to the
	// ring afterwards, so the ring keeps what the blur leaves at the edge.
	halo := ramp([13]vec3{
		vec3(0, 0, 0), vec3(0.58, 0, 0), vec3(0.62, 0.03, 0.02), vec3(0.65, 0.08, 0.08), vec3(0.67, 0.2, 0.2),
		vec3(0.69, 0.45, 0.4), vec3(0.70, 0.85, 0.6), vec3(0.705, 0.85, 0.6), vec3(0.715, 0.45, 0.4),
		vec3(0.73, 0.2, 0.2), vec3(0.75, 0.08, 0.08), vec3(0.78, 0.03, 0.02), vec3(0.82, 0, 0),
	}, t)
	b := mix(halo.y, halo.x, Dark)
	bloom := vec4(ink*b, b) * (ring / (1 + exp(-1.702*depth/8)) * Opacity.z)

	return over(bloom, over(line, glow))
}
`)

// beam paints the border beam around r, the box of the element being drawn,
// from that element's Draw callback. radius is the box's corner radius in
// DIPs, t the seconds since the beam started, strength 0..1 fades all of it
// (0 paints nothing and asks for no frame), dark picks the rendition.
// While strength > 0 it asks for the next animation frame.
//
// It adds light over what is already there and fills nothing opaque, so a
// Draw callback (under the children) and a DrawOver one (over them, as the
// library's layers lie over its content) both work. Everything stays inside
// r, as in the library, whose box clips it.
func beam(p *ui.Painter, r ui.Rect, radius float32, t float64, strength float32, dark bool) {
	if strength <= 0 {
		return
	}
	if p.Preferences().ReduceMotion {
		t = beamStill
	} else {
		// A lap of 900 DIPs a second looks as smooth at 60 frames as at 120.
		p.AnimationFrameRate(60)
	}
	strength = min(strength, 1)

	// Opacities and saturation of the library's "md" presets.
	stroke, inner, bloom, sat, isDark := float32(0.12), float32(0.26), float32(0.34), 1.5, float32(0)
	if dark {
		stroke, inner, bloom, sat, isDark = 0.26, 0.42, 0.24, 1.2, 1
	}
	beamShader.Draw(p, r, shader.Options{Uniforms: map[string]any{
		"Angle":   t/beamTurn - math.Floor(t/beamTurn),
		"Radius":  radius,
		"Dark":    isDark,
		"Filter":  beamFilter(-beamHue*math.Cos(2*math.Pi*t/beamDrift), beamBrite, sat),
		"Opacity": []float32{stroke * strength, inner * strength, bloom * strength},
	}})
}

// beamFilter is CSS's hue-rotate(hue degrees) brightness(b) saturate(s) as a
// matrix by columns, with the W3C's feColorMatrix numbers that browsers use.
func beamFilter(hue, b, s float64) []float32 {
	c, n := math.Cos(hue*math.Pi/180), math.Sin(hue*math.Pi/180)
	h := [3][3]float64{
		{0.213 + c*0.787 - n*0.213, 0.715 - c*0.715 - n*0.715, 0.072 - c*0.072 + n*0.928},
		{0.213 - c*0.213 + n*0.143, 0.715 + c*0.285 + n*0.140, 0.072 - c*0.072 - n*0.283},
		{0.213 - c*0.213 - n*0.787, 0.715 - c*0.715 + n*0.715, 0.072 + c*0.928 + n*0.072},
	}
	g := [3][3]float64{
		{0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s},
		{0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s},
		{0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s},
	}
	m := make([]float32, 9)
	for row := range 3 {
		for col := range 3 {
			var sum float64
			for k := range 3 {
				sum += g[row][k] * h[k][col]
			}
			m[col*3+row] = float32(sum * b)
		}
	}
	return m
}
