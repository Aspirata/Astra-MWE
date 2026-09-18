package meshopt

import (
	"astra-mwe/internal/scene"
	"context"
	"reflect"
	"testing"
)

func wall(w, h int) *scene.Scene {
	p := scene.Primitive{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := uint32(len(p.Positions) / 3)
			for _, c := range [][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
				p.Positions = append(p.Positions, float32(x)+c[0], float32(y)+c[1], 0)
				p.Normals = append(p.Normals, 0, 0, 1)
				p.UVs = append(p.UVs, c[0], c[1])
				p.Colors = append(p.Colors, 1, 1, 1, 1)
			}
			p.Indices = append(p.Indices, base, base+1, base+2, base, base+2, base+3)
		}
	}
	return &scene.Scene{Materials: []scene.Material{{Alpha: "OPAQUE"}}, Meshes: []scene.Mesh{{Name: "Minecraft World", Primitives: []scene.Primitive{p}}}}
}
func TestRectanglesRepeatTextureAndPreserveArea(t *testing.T) {
	sc := wall(8, 4)
	if err := Optimize(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	p := sc.Meshes[0].Primitives[0]
	if len(p.Indices) != 6 {
		t.Fatalf("expected 2 triangles, got %d", len(p.Indices)/3)
	}
	for i := 0; i < len(p.Positions)/3; i++ {
		if p.UVs[2*i] != p.Positions[3*i] || p.UVs[2*i+1] != p.Positions[3*i+1] {
			t.Fatal("UV tiles stretched")
		}
	}
	sc2 := wall(8, 4)
	Optimize(context.Background(), sc2)
	if !reflect.DeepEqual(sc, sc2) {
		t.Fatal("nondeterministic output")
	}
}
func TestTintBoundariesAndTranslucencyPreserved(t *testing.T) {
	sc := wall(2, 1)
	for i := 16; i < 32; i += 4 {
		sc.Meshes[0].Primitives[0].Colors[i] = .5
	}
	Optimize(context.Background(), sc)
	if len(sc.Meshes[0].Primitives[0].Indices) != 12 {
		t.Fatal("tint boundary merged")
	}
	sc = wall(2, 1)
	sc.Materials[0].Alpha = "BLEND"
	Optimize(context.Background(), sc)
	if len(sc.Meshes[0].Primitives[0].Indices) != 6 || sc.Materials[0].Alpha != "BLEND" {
		t.Fatal("coplanar translucent tiles should merge without losing alpha")
	}
}

func TestFractionalSnowPlanesAndQuarterTurnRoundoff(t *testing.T) {
	for _, side := range []bool{false, true} {
		sc := wall(8, 1)
		p := &sc.Meshes[0].Primitives[0]
		for i := 0; i < len(p.Positions)/3; i++ {
			p.Positions[i*3+2] = .125
			p.Normals[i*3] = 1e-16
			if side {
				p.Positions[i*3+1] *= .125
				p.UVs[i*2+1] *= .125
			}
		}
		if err := Optimize(context.Background(), sc); err != nil {
			t.Fatal(err)
		}
		p = &sc.Meshes[0].Primitives[0]
		if len(p.Indices) != 6 {
			t.Fatalf("side=%v: %d triangles", side, len(p.Indices)/3)
		}
		for i := 0; i < len(p.Positions)/3; i++ {
			if p.Positions[i*3+2] != .125 || p.UVs[i*2] != p.Positions[i*3] || p.UVs[i*2+1] != p.Positions[i*3+1] {
				t.Fatal("snow geometry or UV scale changed")
			}
		}
	}
}
func TestDefaultDedupRetainsEveryBlockFace(t *testing.T) {
	sc := wall(4, 4)
	Deduplicate(sc)
	if len(sc.Meshes[0].Primitives[0].Indices) != 96 {
		t.Fatal("default changed topology")
	}
}

func TestPartialTilesWithDifferentUVPhaseDoNotMerge(t *testing.T) {
	sc := wall(1, 2)
	p := &sc.Meshes[0].Primitives[0]
	for i := 0; i < len(p.Positions)/3; i++ {
		p.Positions[i*3+1] *= .5
		p.UVs[i*2+1] *= .5
	}
	// Each half-height face restarts the same half of the texture. Merging
	// them would incorrectly reveal the other half on the second face.
	if err := Optimize(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	if len(sc.Meshes[0].Primitives[0].Indices) != 12 {
		t.Fatal("different texture phases were merged")
	}
}
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Optimize(ctx, wall(1, 1)) == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestAllAxesMirroredUVsAndNegativePositions(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		for _, sign := range []float32{-1, 1} {
			sc := wall(5, 3)
			p := &sc.Meshes[0].Primitives[0]
			a, b := (axis+1)%3, (axis+2)%3
			for i := 0; i < len(p.Positions)/3; i++ {
				u, v := p.Positions[i*3], p.Positions[i*3+1]
				var pos, n [3]float32
				pos[axis] = -7
				pos[a] = u - 9
				pos[b] = v - 11
				n[axis] = sign
				copy(p.Positions[i*3:i*3+3], pos[:])
				copy(p.Normals[i*3:i*3+3], n[:])
				p.UVs[i*2], p.UVs[i*2+1] = 1-p.UVs[i*2+1], p.UVs[i*2]
			}
			if err := Optimize(context.Background(), sc); err != nil {
				t.Fatal(err)
			}
			p = &sc.Meshes[0].Primitives[0]
			if len(p.Indices) != 6 {
				t.Fatalf("axis %d sign %v did not merge", axis, sign)
			}
			for i := 0; i < len(p.Positions)/3; i++ {
				wantU, wantV := 1-(p.Positions[i*3+b]+11), p.Positions[i*3+a]+9
				if p.UVs[i*2] != wantU || p.UVs[i*2+1] != wantV {
					t.Fatal("rotated texture changed")
				}
			}
		}
	}
}

func TestPartialUVAndTintGradientCannotMerge(t *testing.T) {
	for _, mode := range []string{"uv", "color"} {
		sc := wall(2, 1)
		p := &sc.Meshes[0].Primitives[0]
		if mode == "uv" {
			for i := range p.UVs {
				p.UVs[i] *= .5
			}
		} else {
			p.Colors[0] = .5
		}
		Optimize(context.Background(), sc)
		if len(sc.Meshes[0].Primitives[0].Indices) != 12 {
			t.Fatalf("lost %s variation", mode)
		}
	}
}
