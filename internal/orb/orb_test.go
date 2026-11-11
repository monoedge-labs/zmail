package orb

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// The frames match the library's golden vectors: dots are stride 6 (x, y, z,
// r, white, a), lines stride 7 (x1, y1, x2, y2, white, a, w), t as the engine
// takes it.
func TestGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Tolerance float64
		Cases     []struct {
			Key   string
			State State
			Size  int
			T     float64
			Dots  []float64
			Lines []float64
		}
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range g.Cases {
		f := New(c.State, c.Size).Frame(c.T)
		if len(f.Dots)*6 != len(c.Dots) || len(f.Lines)*7 != len(c.Lines) {
			t.Errorf("%s: %d dots, %d lines; want %d, %d", c.Key, len(f.Dots), len(f.Lines), len(c.Dots)/6, len(c.Lines)/7)
			continue
		}
		for i, d := range f.Dots {
			for k, v := range []float64{d.X, d.Y, d.Z, d.R, d.White, d.A} {
				if math.Abs(v-c.Dots[i*6+k]) > g.Tolerance {
					t.Fatalf("%s: dot %d field %d = %v, want %v", c.Key, i, k, v, c.Dots[i*6+k])
				}
			}
		}
		for i, l := range f.Lines {
			for k, v := range []float64{l.X1, l.Y1, l.X2, l.Y2, l.White, l.A, l.W} {
				if math.Abs(v-c.Lines[i*7+k]) > g.Tolerance {
					t.Fatalf("%s: line %d field %d = %v, want %v", c.Key, i, k, v, c.Lines[i*7+k])
				}
			}
		}
	}
}
