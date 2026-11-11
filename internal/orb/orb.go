// Package orb is the geometry of the Thinking orbs of Libraries.dev
// (https://github.com/Jakubantalik/Libraries.dev, MIT): dots on a tilted,
// turning sphere that stand for work in progress. A frame is a final list
// of dots and lines in draw order; the painter only draws it.
//
// Two of the library's nine states are here: Working (orbits) and
// Connecting (web), at its three sizes. The geometry is a port of the
// library's `engine`, checked against its golden vectors (testdata).
package orb

import (
	"fmt"
	"math"
	"sort"
)

// State is what the orb shows work as.
type State string

const (
	Working    State = "working"    // particles on tilted orbits
	Connecting State = "connecting" // a constellation wiring itself
)

// Dot is one mark. White is its ink value, 0 darkest on paper.
type Dot struct{ X, Y, Z, R, White, A float64 }

// Line is an edge between two projected points, drawn under the dots.
type Line struct{ X1, Y1, X2, Y2, White, A, W float64 }

// Frame is one instant: lines first, then dots far to near.
type Frame struct {
	Dots  []Dot
	Lines []Line
}

type opts map[string]float64

func (o opts) get(k string, def float64) float64 {
	if v, ok := o[k]; ok {
		return v
	}
	return def
}

// Orb is a state at a size, ready to draw frames.
type Orb struct {
	Size  int     // 20, 32 or 64
	Speed float64 // the clock's multiplier: Frame takes seconds × Speed
	mode  func(size, t float64, o opts) Frame
	o     opts
}

var baseProfiles = map[State]opts{
	Working:    {"orbitN": 12, "ghostN": 40, "ghostR": 0.9, "ghostA": 0.5, "particles": 3, "partR": 1.2, "partRDepth": 1.6, "rsPow": 0.6, "rMin": 0.3},
	Connecting: {"nodeN": 30, "thr": 0.72, "signals": 5, "nodeR": 1.4, "nodeRDepth": 1.8, "lineW": 0.8, "rsPow": 0.6, "rMin": 0.3},
}

type preset struct{ speed, count, size float64 }

var presets = map[State]map[int]preset{
	Working:    {64: {1.885, 1, 1}, 32: {2.9072, 0.4251, 1.6849}, 20: {3.9, 0.238, 2.4}},
	Connecting: {64: {3.315, 1.35, 0.95}, 32: {5.0104, 0.4942, 1.2571}, 20: {6.63, 0.25, 1.52}},
}

var modes = map[State]func(size, t float64, o opts) Frame{Working: orbits, Connecting: web}

// New resolves a state at a size, 20, 32 or 64; others panic.
func New(s State, size int) *Orb {
	p, ok := presets[s][size]
	if !ok {
		panic(fmt.Sprintf("orb: no %q at size %d", s, size))
	}
	o := opts{}
	for k, v := range baseProfiles[s] {
		o[k] = v
	}
	if p.count != 1 {
		scaleCounts(o, p.count)
	}
	if p.size != 1 {
		scaleRadii(o, p.size)
	}
	return &Orb{Size: size, Speed: p.speed, mode: modes[s], o: o}
}

// Frame is the orb at t, in seconds × Speed.
func (b *Orb) Frame(t float64) Frame { return b.mode(float64(b.Size), t, b.o) }

// scaleCounts scales the dot counts, as the library does for a preset.
func scaleCounts(o opts, scale float64) {
	for _, k := range []string{"orbitN", "ghostN", "nodeN", "signals"} {
		if v, ok := o[k]; ok && v != 0 {
			o[k] = math.Max(1, math.Round(v*scale))
		}
	}
}

// scaleRadii scales every key that sets a dot's radius.
func scaleRadii(o opts, scale float64) {
	for _, k := range []string{"rBase", "rDepth", "rActive", "rDot", "ghostR", "partR", "partRDepth", "nodeR", "nodeRDepth"} {
		if v, ok := o[k]; ok {
			o[k] = v * scale
		}
	}
	o["rSizeMul"] = o.get("rSizeMul", 1) * scale
}

func frac(x float64) float64 { return x - math.Floor(x) }

func lerp(a, b, f float64) float64 { return a + (b-a)*f }

// hashD is a deterministic hash in [0, 1).
func hashD(a, b float64) float64 { return frac(math.Sin(a*12.9898+b*78.233) * 43758.5453) }

// vnoise is value noise on a 2D lattice.
func vnoise(x, y float64) float64 {
	xi, yi := math.Floor(x), math.Floor(y)
	fx, fy := x-xi, y-yi
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	a, b, c, d := hashD(xi, yi), hashD(xi+1, yi), hashD(xi, yi+1), hashD(xi+1, yi+1)
	return a + (b-a)*fx + (c-a)*fy + (a-b-c+d)*fx*fy
}

// fibDir is the i-th of n stable directions on a unit sphere.
func fibDir(i, n int) (x, y, z float64) {
	golden := math.Pi * (3 - math.Sqrt(5))
	y = 1 - 2*(float64(i)+0.5)/float64(n)
	rad := math.Sqrt(1 - y*y)
	a := float64(i) * golden
	return rad * math.Cos(a), y, rad * math.Sin(a)
}

// proj is a spin, a tilt and an orthographic projection.
func proj(yaw, tilt, cx, cy, scale float64) func(x, y, z float64) (px, py, pz float64) {
	st, ct, sy, cyw := math.Sin(tilt), math.Cos(tilt), math.Sin(yaw), math.Cos(yaw)
	return func(x, y, z float64) (float64, float64, float64) {
		x1 := x*cyw + z*sy
		z1 := -x*sy + z*cyw
		y1 := y*ct - z1*st
		z2 := y*st + z1*ct
		return cx + x1*scale, cy - y1*scale, z2
	}
}

// radiusScale: radii were tuned for 300, and shrink less than the size does.
func radiusScale(size, pow float64) float64 { return math.Pow(size/300, pow) }

// finalize drops the invisible, clamps radii and sorts dots far to near.
func finalize(dots []Dot, lines []Line, rMin float64) Frame {
	vis := dots[:0:0]
	for _, d := range dots {
		if d.A < 0.02 {
			continue
		}
		d.R = math.Max(rMin, d.R)
		vis = append(vis, d)
	}
	sort.SliceStable(vis, func(i, j int) bool { return vis[i].Z < vis[j].Z })
	kept := lines[:0:0]
	for _, l := range lines {
		if l.A >= 0.02 {
			kept = append(kept, l)
		}
	}
	return Frame{vis, kept}
}

// orbits: particles on tilted orbits, each with a ghost path.
func orbits(size, t float64, o opts) Frame {
	cx, cy := size/2, size/2
	R := size / 2 * 0.82
	pt := proj(t*0.12, 0.3, cx, cy, 1)
	rs := radiusScale(size, o.get("rsPow", 0.6))
	orbitN, ghostN, particles := int(o.get("orbitN", 12)), int(o.get("ghostN", 40)), int(o.get("particles", 3))

	var dots []Dot
	for orb := 0; orb < orbitN; orb++ {
		h1, h2, h3 := hashD(float64(orb), 1.7), hashD(float64(orb), 5.2), hashD(float64(orb), 8.9)
		ro := R * (0.45 + 0.52*h1)
		th := h1 * 2 * math.Pi
		phi := math.Acos(2*h2 - 1)
		// the orbit's plane: u and v are perpendicular to its normal n
		nx, ny, nz := math.Sin(phi)*math.Cos(th), math.Cos(phi), math.Sin(phi)*math.Sin(th)
		ux, uy, uz := -ny, nx, 0.0
		ul := math.Max(1e-6, math.Sqrt(ux*ux+uy*uy))
		ux, uy = ux/ul, uy/ul
		vx, vy, vz := ny*uz-nz*uy, nz*ux-nx*uz, nx*uy-ny*ux
		speed := 0.25 + 0.55*h3
		if h3 <= 0.5 {
			speed = -speed
		}
		at := func(a float64) (float64, float64, float64) {
			return pt((ux*math.Cos(a)+vx*math.Sin(a))*ro, (uy*math.Cos(a)+vy*math.Sin(a))*ro, (uz*math.Cos(a)+vz*math.Sin(a))*ro)
		}
		for k := 0; k < ghostN; k++ {
			px, py, z := at(float64(k) / float64(ghostN) * 2 * math.Pi)
			depth := (z/ro + 1) / 2
			dots = append(dots, Dot{px, py, z, o.get("ghostR", 0.9) * rs, 0.72, o.get("ghostA", 0.5) * (0.4 + 0.6*depth)})
		}
		for m := 0; m < particles; m++ {
			px, py, z := at(t*speed + float64(m)/float64(particles)*2*math.Pi + h2*6)
			depth := (z/ro + 1) / 2
			dots = append(dots, Dot{px, py, z, (o.get("partR", 1.2) + o.get("partRDepth", 1.6)*depth) * rs, 0.3 - 0.22*depth, 1})
		}
	}
	return finalize(dots, nil, o.get("rMin", 0.3))
}

// web: nodes drift on the sphere, close pairs grow an edge, and packets run
// along pairs picked again and again.
func web(size, t float64, o opts) Frame {
	cx, cy := size/2, size/2
	R := size / 2 * 0.8 * o.get("spread", 1)
	pt := proj(t*0.12, 0.32, cx, cy, R)
	rs := radiusScale(size, o.get("rsPow", 0.6))
	nodeN, thr := int(o.get("nodeN", 30)), o.get("thr", 0.72)
	nodeR, nodeRDepth := o.get("nodeR", 1.4), o.get("nodeRDepth", 1.8)

	nodes := make([][3]float64, nodeN)
	for i := range nodes {
		dx, dy, dz := fibDir(i, nodeN)
		x := dx + 0.3*(vnoise(float64(i)*0.31+9, t*0.24)-0.5)*2
		y := dy + 0.3*(vnoise(float64(i)*0.53+27, t*0.21)-0.5)*2
		z := dz + 0.3*(vnoise(float64(i)*0.77+55, t*0.27)-0.5)*2
		l := math.Sqrt(x*x + y*y + z*z)
		nodes[i] = [3]float64{x / l, y / l, z / l}
	}

	var lines []Line
	var dots []Dot
	for i := 0; i < nodeN; i++ {
		for j := i + 1; j < nodeN; j++ {
			dx, dy, dz := nodes[i][0]-nodes[j][0], nodes[i][1]-nodes[j][1], nodes[i][2]-nodes[j][2]
			dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
			if dist >= thr {
				continue
			}
			x1, y1, z1 := pt(nodes[i][0], nodes[i][1], nodes[i][2])
			x2, y2, z2 := pt(nodes[j][0], nodes[j][1], nodes[j][2])
			depth := ((z1+z2)/2 + 1) / 2
			lines = append(lines, Line{x1, y1, x2, y2, 0.42, (1 - dist/thr) * (0.3 + 0.55*depth), math.Max(0.6, o.get("lineW", 0.8)*rs)})
		}
	}
	for i, n := range nodes {
		px, py, z := pt(n[0], n[1], n[2])
		depth := (z + 1) / 2
		pulse := 1 + 0.25*math.Sin(t*1.4+float64(i)*2.7)
		dots = append(dots, Dot{px, py, z, (nodeR + nodeRDepth*depth) * pulse * rs, 0.55 - 0.45*depth, 1})
	}
	signals := int(o.get("signals", 5))
	for s := 0; s < signals; s++ {
		fs := float64(s)
		seg := math.Floor(t*0.55 + fs*7.31)
		a := int(math.Floor(hashD(seg, fs*3.1+1.7) * float64(nodeN)))
		b := int(math.Floor(hashD(seg, fs*5.7+4.2) * float64(nodeN)))
		if a == b {
			continue
		}
		f := frac(t*0.55 + fs*7.31)
		x, y, z := lerp(nodes[a][0], nodes[b][0], f), lerp(nodes[a][1], nodes[b][1], f), lerp(nodes[a][2], nodes[b][2], f)
		l := math.Max(1e-6, math.Sqrt(x*x+y*y+z*z))
		px, py, zr := pt(x/l, y/l, z/l)
		depth := (zr + 1) / 2
		dots = append(dots, Dot{px, py, zr, (nodeR*1.5 + nodeRDepth*depth) * rs, 0.05, 0.5 + 0.5*depth})
	}
	return finalize(dots, lines, o.get("rMin", 0.3))
}
