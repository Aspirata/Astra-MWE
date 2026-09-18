package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const cube = `{"textures":{"all":"test:block/stone"},"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"down":{"texture":"#all","cullface":"down"},"up":{"texture":"#all","cullface":"up"},"north":{"texture":"#all","cullface":"north"},"south":{"texture":"#all","cullface":"south"},"west":{"texture":"#all","cullface":"west"},"east":{"texture":"#all","cullface":"east"}}}]}`

func fixture(t *testing.T, more map[string]string) *assets.Stack {
	t.Helper()
	d := t.TempDir()
	files := map[string]string{
		"assets/test/models/block/cube.json":           cube,
		"assets/test/models/block/stone.json":          `{"parent":"test:block/cube"}`,
		"assets/test/blockstates/stone.json":           `{"variants":{"":{"model":"test:block/stone"}}}`,
		"assets/test/blockstates/other.json":           `{"variants":{"":{"model":"test:block/stone"}}}`,
		"assets/minecraft/blockstates/oak_leaves.json": `{"variants":{"":{"model":"test:block/stone"}}}`,
	}
	for k, v := range more {
		files[k] = v
	}
	for k, v := range files {
		p := filepath.Join(d, k)
		os.MkdirAll(filepath.Dir(p), 0755)
		if e := os.WriteFile(p, []byte(v), 0644); e != nil {
			t.Fatal(e)
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.NRGBA{180, 180, 180, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	p := filepath.Join(d, "assets/test/textures/block/stone.png")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, b.Bytes(), 0644)
	a, e := assets.Open([]string{d})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func vol(blocks map[[3]int]scene.Block) *scene.Volume {
	return &scene.Volume{Bounds: scene.Bounds{MinX: -8, MaxX: 8, MinY: 0, MaxY: 16, MinZ: -8, MaxZ: 8}, Blocks: blocks}
}
func counts(s *scene.Scene) (int, int) {
	vs, is := 0, 0
	for _, m := range s.Meshes {
		for _, p := range m.Primitives {
			vs += len(p.Positions) / 3
			is += len(p.Indices)
		}
	}
	return vs, is
}
func TestCubesCullSharedFacesAndSeparateTypes(t *testing.T) {
	a := fixture(t, nil)
	v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}, {1, 0, 0}: {Name: "test:other"}})
	s, e := Build(context.Background(), v, a, scene.MeshOptions{})
	if e != nil {
		t.Fatal(e)
	}
	vs, is := counts(s)
	if vs != 40 || is != 60 {
		t.Fatalf("two neighboring cubes: vertices=%d indices=%d", vs, is)
	}
	if len(s.Materials) != 2 {
		t.Fatalf("different block types must remain distinct: %d", len(s.Materials))
	}
	if len(s.Warnings) != 0 {
		t.Fatal(s.Warnings)
	}
	s2, e := Build(context.Background(), v, a, scene.MeshOptions{})
	if e != nil || !reflect.DeepEqual(s, s2) {
		t.Fatal("meshing is not deterministic", e)
	}
	for _, m := range s.Meshes {
		for _, p := range m.Primitives {
			for i := 0; i < len(p.Indices); i += 3 {
				ia, ib, ic := int(p.Indices[i])*3, int(p.Indices[i+1])*3, int(p.Indices[i+2])*3
				ax, ay, az := p.Positions[ib]-p.Positions[ia], p.Positions[ib+1]-p.Positions[ia+1], p.Positions[ib+2]-p.Positions[ia+2]
				bx, by, bz := p.Positions[ic]-p.Positions[ia], p.Positions[ic+1]-p.Positions[ia+1], p.Positions[ic+2]-p.Positions[ia+2]
				if (ay*bz-az*by)*p.Normals[ia]+(az*bx-ax*bz)*p.Normals[ia+1]+(ax*by-ay*bx)*p.Normals[ia+2] <= 0 {
					t.Fatal("triangle winding opposes normal")
				}
			}
		}
	}
}
func TestMultipartInheritanceRotationAndTint(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/test/models/block/half.json":              `{"parent":"test:block/cube","textures":{"all":"test:block/stone"},"elements":[{"from":[0,0,0],"to":[16,8,16],"faces":{"up":{"texture":"#all","tintindex":0,"uv":[0,0,16,8],"rotation":90}}}]}`,
		"assets/test/blockstates/part.json":               `{"multipart":[{"apply":{"model":"test:block/half"}},{"when":{"OR":[{"axis":"x"},{"axis":"z"}]},"apply":{"model":"test:block/half","x":90,"y":90}}]}`,
		"assets/minecraft/blockstates/redstone_wire.json": `{"variants":{"":{"model":"test:block/half"}}}`,
	})
	s, e := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:part", Properties: map[string]string{"axis": "x"}}}), a, scene.MeshOptions{BiomeColors: true})
	if e != nil {
		t.Fatal(e)
	}
	vs, _ := counts(s)
	if vs != 8 {
		t.Fatalf("multipart count: %d", vs)
	}
	for _, power := range []string{"0", "15"} {
		out, e := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:redstone_wire", Properties: map[string]string{"power": power}}}), a, scene.MeshOptions{})
		if e != nil {
			t.Fatal(e)
		}
		p := out.Meshes[0].Primitives[0]
		if len(p.Colors) != 16 || p.Colors[3] != 1 {
			t.Fatal("expected RGBA vertex tint")
		}
		if power == "0" && p.Colors[0] > .4 {
			t.Fatal("unpowered dust too bright")
		}
		if power == "15" && p.Colors[0] < .9 {
			t.Fatal("powered dust too dark")
		}
	}
}
func TestLeavesDefaultSolidAndOptionalHollow(t *testing.T) {
	a := fixture(t, nil)
	blocks := map[[3]int]scene.Block{}
	for x := 0; x < 3; x++ {
		for y := 0; y < 3; y++ {
			for z := 0; z < 3; z++ {
				blocks[[3]int{x, y, z}] = scene.Block{Name: "minecraft:oak_leaves"}
			}
		}
	}
	s, e := Build(context.Background(), vol(blocks), a, scene.MeshOptions{})
	if e != nil {
		t.Fatal(e)
	}
	full, _ := counts(s)
	s, e = Build(context.Background(), vol(blocks), a, scene.MeshOptions{HollowLeaves: true})
	if e != nil {
		t.Fatal(e)
	}
	hollow, _ := counts(s)
	if full != 648 || hollow >= full {
		t.Fatalf("leaves faces default=%d hollow=%d", full, hollow)
	}
}
func TestMissingCycleAndFluidFallback(t *testing.T) {
	a := fixture(t, map[string]string{"assets/test/blockstates/cycle.json": `{"variants":{"":{"model":"test:block/cycle"}}}`, "assets/test/models/block/cycle.json": `{"parent":"test:block/cycle"}`})
	s, e := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:missing"}, {2, 0, 0}: {Name: "test:cycle"}, {4, 0, 0}: {Name: "minecraft:water"}}), a, scene.MeshOptions{BiomeColors: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Warnings) < 2 {
		t.Fatalf("missing model warnings: %v", s.Warnings)
	}
	found := false
	for _, m := range s.Materials {
		if strings.HasPrefix(m.Name, "minecraft:water") {
			found = true
			if m.Alpha != "BLEND" {
				t.Fatal("water must blend")
			}
			im, e := png.Decode(bytes.NewReader(m.PNG))
			if e != nil {
				t.Fatal(e)
			}
			r, g, b, _ := im.At(0, 0).RGBA()
			if r > g && b > g {
				t.Fatal("water incorrectly uses magenta fallback")
			}
		}
	}
	if !found {
		t.Fatal("water geometry missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Build(ctx, vol(nil), a, scene.MeshOptions{}); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestWeightedVariantIsStable(t *testing.T) {
	a := fixture(t, map[string]string{"assets/test/blockstates/random.json": `{"variants":{"":{"model":"test:block/stone"},"kind=a":[{"model":"test:block/stone","weight":1},{"model":"test:block/stone","y":90,"weight":3}]}}`})
	v := vol(map[[3]int]scene.Block{{-1, 0, -1}: {Name: "test:random", Properties: map[string]string{"kind": "a"}}})
	s, _ := Build(context.Background(), v, a, scene.MeshOptions{})
	b, _ := json.Marshal(s)
	s, _ = Build(context.Background(), v, a, scene.MeshOptions{})
	c, _ := json.Marshal(s)
	if !bytes.Equal(b, c) {
		t.Fatal("unstable variant")
	}
}
