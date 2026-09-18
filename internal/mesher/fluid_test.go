package mesher

import (
	"astra-mwe/internal/scene"
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
)

type fluidQuad struct {
	pos     [4][3]float32
	uv      [4][2]float32
	n       [3]float32
	texture string
}

func fluidQuads(s *scene.Scene) []fluidQuad {
	var out []fluidQuad
	for _, m := range s.Meshes {
		for _, p := range m.Primitives {
			for i := 0; i < len(p.Positions)/3; i += 4 {
				q := fluidQuad{texture: s.Materials[p.Material].TextureName}
				copy(q.n[:], p.Normals[i*3:i*3+3])
				for j := 0; j < 4; j++ {
					for axis := 0; axis < 3; axis++ {
						q.pos[j][axis] = p.Positions[(i+j)*3+axis] + float32(s.Origin[axis])
					}
					copy(q.uv[j][:], p.UVs[(i+j)*2:(i+j)*2+2])
				}
				out = append(out, q)
			}
		}
	}
	return out
}

func fluidBlock(kind string, level int) scene.Block {
	return scene.Block{Name: "minecraft:" + kind, Properties: map[string]string{"level": strconv.Itoa(level)}}
}

func TestFluidLevelsShareSlopedCornersAndCullInternalSides(t *testing.T) {
	for _, kind := range []string{"water", "lava"} {
		t.Run(kind, func(t *testing.T) {
			v := vol(map[[3]int]scene.Block{{0, 0, 0}: fluidBlock(kind, 0), {1, 0, 0}: fluidBlock(kind, 4)})
			s, err := Build(context.Background(), v, fixture(t, nil), scene.MeshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			qs := fluidQuads(s)
			if len(qs) != 10 {
				t.Errorf("shared fluid sides must both be hidden: %d quads", len(qs))
			}
			tops := map[float32]fluidQuad{}
			for _, q := range qs {
				if q.n[1] > 0 {
					tops[q.pos[0][0]] = q
				}
			}
			left, right := tops[0], tops[1]
			if left.pos[3][1] != right.pos[0][1] || left.pos[2][1] != right.pos[1][1] {
				t.Errorf("fluid seam is disconnected: left=%v right=%v", left.pos, right.pos)
			}
			if right.pos[0][1] <= right.pos[3][1] {
				t.Errorf("flow must slope downhill: %v", right.pos)
			}
			for _, q := range qs {
				if q.n[1] != 0 {
					continue
				}
				if q.texture != kind+"_flow.png" {
					t.Errorf("side uses %s", q.texture)
				}
				for j := 2; j < 4; j++ {
					found := false
					for _, top := range tops {
						for _, p := range top.pos {
							if p == q.pos[j] {
								found = true
							}
						}
					}
					if !found {
						t.Errorf("side detaches from top at %v", q.pos[j])
					}
					if math.Abs(float64(q.uv[j][1]-(1-q.pos[j][1])*.5)) > 1e-6 {
						t.Errorf("side UV stretches at %v: %v", q.pos[j], q.uv[j])
					}
				}
			}
			if right.texture != kind+"_flow.png" {
				t.Errorf("flowing top uses %s", right.texture)
			}
			wantUV := [4][2]float32{{.75, .25}, {.25, .25}, {.25, .75}, {.75, .75}}
			for i := range wantUV {
				for j := range wantUV[i] {
					if math.Abs(float64(right.uv[i][j]-wantUV[i][j])) > 1e-6 {
						t.Errorf("east-flow UV %v, want %v", right.uv, wantUV)
					}
				}
			}
		})
	}
}

func TestFluidSourceFallingAndCoveredHeights(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level int
		above bool
		want  float32
	}{
		{"source", 0, false, 8.0 / 9}, {"falling", 8, false, 8.0 / 9}, {"falling_variant", 15, false, 8.0 / 9}, {"covered", 4, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocks := map[[3]int]scene.Block{}
			for x := -1; x <= 1; x++ {
				for z := -1; z <= 1; z++ {
					blocks[[3]int{x, 0, z}] = fluidBlock("water", tc.level)
				}
			}
			if tc.above {
				blocks[[3]int{0, 1, 0}] = fluidBlock("water", 0)
			}
			v := vol(blocks)
			v.Bounds = scene.Bounds{MinX: 0, MaxX: 0, MinY: 0, MaxY: 0, MinZ: 0, MaxZ: 0}
			s, err := Build(context.Background(), v, fixture(t, nil), scene.MeshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var top *fluidQuad
			for _, q := range fluidQuads(s) {
				if q.n[1] > 0 {
					c := q
					top = &c
				}
			}
			if top == nil {
				t.Fatal("missing selection-boundary top")
			}
			for _, p := range top.pos {
				if math.Abs(float64(p[1]-tc.want)) > 1e-6 {
					t.Errorf("height=%g, want %g", p[1], tc.want)
				}
			}
		})
	}
}

func TestCherryLeavesRemainUntintedInMeshAndMap(t *testing.T) {
	tintedCube := strings.ReplaceAll(cube, `"texture":"#all"`, `"texture":"#all","tintindex":0`)
	a := fixture(t, map[string]string{
		"assets/test/models/block/tinted.json":            tintedCube,
		"assets/minecraft/blockstates/cherry_leaves.json": `{"variants":{"":{"model":"test:block/tinted"}}}`,
		"assets/minecraft/blockstates/oak_leaves.json":    `{"variants":{"":{"model":"test:block/tinted"}}}`,
	})
	painter := NewSurfacePainter(a)
	for _, name := range []string{"cherry_leaves", "oak_leaves"} {
		block := scene.Block{Name: "minecraft:" + name}
		v := vol(map[[3]int]scene.Block{{0, 0, 0}: block})
		s, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
		if err != nil {
			t.Fatal(err)
		}
		color := s.Meshes[0].Primitives[0].Colors[:3]
		white := color[0] == 1 && color[1] == 1 && color[2] == 1
		if white != (name == "cherry_leaves") {
			t.Errorf("%s mesh tint=%v", name, color)
		}
		painter.b.volume = v
		mapColor := painter.blockColor(block, [3]int{})
		neutral := math.Abs(float64(mapColor[0]-mapColor[1])) < 1e-6 && math.Abs(float64(mapColor[1]-mapColor[2])) < 1e-6
		if neutral != (name == "cherry_leaves") {
			t.Errorf("%s map color=%v", name, mapColor)
		}
	}
}

func TestFluidCornerSamplingAndTileHalo(t *testing.T) {
	a := fixture(t, nil)
	for _, tc := range []struct {
		name      string
		neighbors map[[3]int]scene.Block
		want      [4]float32
	}{
		{"isolated_source", nil, [4]float32{20.0 / 27, 20.0 / 27, 20.0 / 27, 20.0 / 27}},
		{"solid_bank", map[[3]int]scene.Block{{-1, 0, 0}: {Name: "test:stone"}, {0, 0, -1}: {Name: "test:stone"}}, [4]float32{8.0 / 9, 80.0 / 99, 20.0 / 27, 80.0 / 99}},
		{"neighbor_column", map[[3]int]scene.Block{{1, 0, 0}: fluidBlock("water", 5), {1, 1, 0}: fluidBlock("water", 0)}, [4]float32{20.0 / 27, 20.0 / 27, 1, 1}},
		{"disconnected_diagonal", map[[3]int]scene.Block{{1, 0, 1}: fluidBlock("water", 0), {1, 1, 1}: fluidBlock("water", 0)}, [4]float32{20.0 / 27, 20.0 / 27, 20.0 / 27, 20.0 / 27}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocks := map[[3]int]scene.Block{{0, 0, 0}: fluidBlock("water", 0)}
			for p, block := range tc.neighbors {
				blocks[p] = block
			}
			v := vol(blocks)
			v.Bounds = scene.Bounds{MaxY: 1}
			s, err := Build(context.Background(), v, a, scene.MeshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, q := range fluidQuads(s) {
				if q.n[1] <= 0 {
					continue
				}
				found = true
				for i, p := range q.pos {
					if math.Abs(float64(p[1]-tc.want[i])) > 1e-6 {
						t.Errorf("corner %d=%g want %g", i, p[1], tc.want[i])
					}
				}
			}
			if !found {
				t.Fatal("missing fluid top")
			}
		})
	}
}

func TestFluidWaterfallFlowAndVerticalInternalCulling(t *testing.T) {
	v := vol(map[[3]int]scene.Block{{0, 1, 0}: fluidBlock("water", 0), {1, 0, 0}: fluidBlock("water", 8)})
	s, err := Build(context.Background(), v, fixture(t, nil), scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range fluidQuads(s) {
		if q.n[1] > 0 && q.pos[0][0] == 0 {
			found = true
			if q.texture != "water_flow.png" {
				t.Errorf("falling edge must use flow texture: %s", q.texture)
			}
		}
	}
	if !found {
		t.Fatal("missing waterfall lip")
	}
	v.Blocks = map[[3]int]scene.Block{{0, 0, 0}: fluidBlock("water", 8), {0, 1, 0}: fluidBlock("water", 0)}
	s, err = Build(context.Background(), v, fixture(t, nil), scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	qs := fluidQuads(s)
	if len(qs) != 10 {
		t.Fatalf("stacked fluid: %d faces, want 10", len(qs))
	}
	for _, q := range qs {
		if q.n[1] != 0 && q.pos[0][1] == 1 {
			t.Error("fluid column retained an internal horizontal face")
		}
	}
}
