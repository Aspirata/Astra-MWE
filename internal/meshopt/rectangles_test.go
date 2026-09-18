package meshopt

import (
	"astra-mwe/internal/scene"
	"context"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func addRect(p *scene.Primitive, x, y, w, h float32) {
	q := quad{}
	for i, c := range [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		q[i] = vertex{p: [3]float32{x + c[0]*w, y + c[1]*h, 0}, n: [3]float32{0, 0, 1}, uv: [2]float32{x + c[0]*w, y + c[1]*h}, c: [4]float32{1, 1, 1, 1}}
	}
	appendQuad(p, q)
}

func rectangleScene(rects ...[4]float32) *scene.Scene {
	sc := wall(0, 0)
	for _, r := range rects {
		addRect(&sc.Meshes[0].Primitives[0], r[0], r[1], r[2], r[3])
	}
	return sc
}

// Sample triangle interiors independently of optimizer rectangle classification.
// The multiset checks coverage (including overlaps), winding and texture phase.
func surfaceSamples(p scene.Primitive) map[string]int {
	out := map[string]int{}
	for i := 0; i < len(p.Indices); i += 3 {
		a, b, c := read(&p, p.Indices[i]), read(&p, p.Indices[i+1]), read(&p, p.Indices[i+2])
		det := (b.p[0]-a.p[0])*(c.p[1]-a.p[1]) - (b.p[1]-a.p[1])*(c.p[0]-a.p[0])
		if det == 0 {
			continue
		}
		x0, x1 := int(math.Floor(float64(min(a.p[0], b.p[0], c.p[0])*16))), int(math.Ceil(float64(max(a.p[0], b.p[0], c.p[0])*16)))
		y0, y1 := int(math.Floor(float64(min(a.p[1], b.p[1], c.p[1])*16))), int(math.Ceil(float64(max(a.p[1], b.p[1], c.p[1])*16)))
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				px, py := (float32(x)+.31)/16-a.p[0], (float32(y)+.37)/16-a.p[1]
				v := (px*(c.p[1]-a.p[1]) - py*(c.p[0]-a.p[0])) / det
				w := ((b.p[0]-a.p[0])*py - (b.p[1]-a.p[1])*px) / det
				if v < 0 || w < 0 || v+w > 1 {
					continue
				}
				var uv [2]int
				for j := range uv {
					f := float64(a.uv[j] + v*(b.uv[j]-a.uv[j]) + w*(c.uv[j]-a.uv[j]))
					uv[j] = int(math.Round((f-math.Floor(f))*10000)) % 10000
				}
				sign := 1
				if det < 0 {
					sign = -1
				}
				out[fmt.Sprint(x, ",", y, ",", sign, ",", uv, ",", a.c, ",", a.n, ",", p.Material)]++
			}
		}
	}
	return out
}

func TestMergeDifferentTileSizesAndRepeatedUVs(t *testing.T) {
	for _, rects := range [][][4]float32{
		{{0, 0, .5, 1}, {.5, 0, .5, .5}, {.5, .5, .5, .5}},
		{{0, 0, 3, 2}, {3, 0, 2, 2}},
		{{-3, -2, 2, 2}, {-1, -2, 3, 2}},
	} {
		sc := rectangleScene(rects...)
		before := surfaceSamples(sc.Meshes[0].Primitives[0])
		if err := Optimize(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		p := sc.Meshes[0].Primitives[0]
		if len(p.Indices) != 6 {
			t.Errorf("%v: want one rectangle, got %d triangles", rects, len(p.Indices)/3)
		}
		if !reflect.DeepEqual(before, surfaceSamples(p)) {
			t.Fatal("surface coverage, winding or texture phase changed")
		}
	}
}

func TestGreedyChoosesBetterOrientation(t *testing.T) {
	sc := wall(3, 2)
	p := &sc.Meshes[0].Primitives[0]
	p.Indices = append(p.Indices[6:12:12], p.Indices[18:]...)
	before := surfaceSamples(*p)
	if err := Optimize(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	if len(p.Indices) != 12 {
		t.Fatalf("T-shaped wall should use 2 rectangles, got %d", len(p.Indices)/6)
	}
	if !reflect.DeepEqual(before, surfaceSamples(*p)) {
		t.Fatal("T-shaped surface changed")
	}
}

func TestCoincidentAndOppositeWindingFacesRemain(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		sc := rectangleScene([4]float32{0, 0, 1, 1}, [4]float32{0, 0, 1, 1}, [4]float32{1, 0, 1, 1})
		p := &sc.Meshes[0].Primitives[0]
		if reverse {
			p.Indices[6], p.Indices[7], p.Indices[8], p.Indices[9], p.Indices[10], p.Indices[11] = 4, 7, 6, 4, 6, 5
		}
		before := surfaceSamples(*p)
		if err := Optimize(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, surfaceSamples(*p)) {
			t.Fatalf("reverse=%v: lost coincident face or changed winding", reverse)
		}
	}
}

func TestOppositeWindingNeighborsDoNotMerge(t *testing.T) {
	sc := wall(2, 1)
	p := &sc.Meshes[0].Primitives[0]
	p.Indices[6], p.Indices[7], p.Indices[8], p.Indices[9], p.Indices[10], p.Indices[11] = 4, 7, 6, 4, 6, 5
	before := surfaceSamples(*p)
	if err := Optimize(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, surfaceSamples(*p)) {
		t.Fatal("merged neighbors with opposite triangle winding")
	}
}

func TestIrregularSurfacesPreserveHolesOverlapsAndTexturePhase(t *testing.T) {
	rng := rand.New(rand.NewSource(41))
	for fixture := 0; fixture < 24; fixture++ {
		sc := wall(0, 0)
		p := &sc.Meshes[0].Primitives[0]
		p.Material = 7
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				if rng.Intn(3) == 0 {
					continue
				}
				addRect(p, float32(x)/2-1, float32(y)/2-1, .5, .5)
				if rng.Intn(8) == 0 {
					addRect(p, float32(x)/2-1, float32(y)/2-1, .5, .5)
				}
			}
		}
		for i := 0; i < len(p.Positions)/3; i++ {
			p.UVs[i*2], p.UVs[i*2+1] = 1-p.UVs[i*2+1], p.UVs[i*2]
		}
		before := surfaceSamples(*p)
		if err := Optimize(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, surfaceSamples(*p)) {
			t.Fatalf("fixture %d: surface multiset changed", fixture)
		}
		if err := Optimize(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, surfaceSamples(*p)) {
			t.Fatalf("fixture %d: repeated optimization changed surface", fixture)
		}
	}
}

func TestUnsupportedTrianglesPreserveEveryAttribute(t *testing.T) {
	sc := wall(2, 1)
	p := &sc.Meshes[0].Primitives[0]
	p.Material = 4
	p.Positions[2] = .123
	p.Colors[16] = .35
	p.UVs[1] = .12345
	before := make([]vertex, len(p.Indices))
	for i, idx := range p.Indices {
		before[i] = read(p, idx)
	}
	if err := Optimize(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	after := make([]vertex, len(p.Indices))
	for i, idx := range p.Indices {
		after[i] = read(p, idx)
	}
	if !reflect.DeepEqual(before, after) || p.Material != 4 {
		t.Fatal("unsupported triangles or attributes changed")
	}
}

type cancelOnCheck struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *cancelOnCheck) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCancellationDuringLargePrimitivePreservesInput(t *testing.T) {
	for _, optimize := range []func(context.Context, *scene.Scene) error{Optimize, DeduplicateContext} {
		for _, check := range []int{2, 20} {
			sc := wall(128, 128)
			before := sc.Meshes[0].Primitives[0]
			ctx, cancel := context.WithCancel(context.Background())
			err := optimize(&cancelOnCheck{ctx, cancel, check}, sc)
			cancel()
			if err != context.Canceled {
				t.Fatalf("check %d: cancellation returned %v", check, err)
			}
			if !reflect.DeepEqual(before, sc.Meshes[0].Primitives[0]) {
				t.Fatal("canceled operation committed a partial primitive")
			}
		}
	}
}

func BenchmarkOptimize(b *testing.B) {
	for _, fixture := range []string{"wall", "checkerboard", "mixed_sizes"} {
		b.Run(fixture, func(b *testing.B) {
			base := wall(128, 128)
			if fixture == "checkerboard" {
				p := &base.Meshes[0].Primitives[0]
				for i := range p.Colors {
					if (i/16%128+i/16/128)%2 == 0 {
						p.Colors[i] = .5
					}
				}
			}
			if fixture == "mixed_sizes" {
				base = wall(0, 0)
				p := &base.Meshes[0].Primitives[0]
				for y := 0; y < 128; y++ {
					for x := 0; x < 128; x++ {
						w := float32(1)
						if x%2 == 0 {
							w = .5
						}
						addRect(p, float32(x), float32(y), w, 1)
						if w == .5 {
							addRect(p, float32(x)+.5, float32(y), .5, 1)
						}
					}
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Optimize replaces primitive buffers; a shallow primitive copy suffices.
				sc := &scene.Scene{Materials: base.Materials, Meshes: []scene.Mesh{{Name: "Minecraft World", Primitives: append([]scene.Primitive(nil), base.Meshes[0].Primitives...)}}}
				if err := Optimize(context.Background(), sc); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(sc.Meshes[0].Primitives[0].Indices)/3), "triangles")
			}
		})
	}
}
