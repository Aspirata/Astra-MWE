package mesher

import (
	"archive/zip"
	"astra-mwe/internal/assets"
	"astra-mwe/internal/exporter"
	"astra-mwe/internal/scene"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptySpecialModelGetsVisibleDiagnostic(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/test/models/block/special.json": `{"textures":{"particle":"test:block/stone"}}`,
		"assets/test/blockstates/special.json":  `{"variants":{"":{"model":"test:block/special"}}}`,
	})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:special"}}), a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vertices, _ := counts(s)
	if vertices == 0 || len(s.Warnings) == 0 {
		t.Fatal("special-renderer block silently disappeared")
	}
}

func TestClientJARModelInventory(t *testing.T) {
	for _, version := range []string{"1.20.1", "26.1.2"} {
		t.Run(version, func(t *testing.T) {
			jar := filepath.Join("..", "..", ".local-testdata", "minecraft-"+version+".jar")
			if _, err := os.Stat(jar); os.IsNotExist(err) {
				t.Skip("optional local JAR unavailable")
			}
			a, err := assets.Open([]string{jar})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			z, err := zip.OpenReader(jar)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			r := resolver{assets: a, models: map[string]modelResult{}, states: map[string]stateResult{}}
			count, empty, builtin := 0, 0, 0
			for _, entry := range z.File {
				if !strings.HasPrefix(entry.Name, "assets/minecraft/models/block/") || !strings.HasSuffix(entry.Name, ".json") {
					continue
				}
				id := "minecraft:" + strings.TrimSuffix(strings.TrimPrefix(entry.Name, "assets/minecraft/models/"), ".json")
				m, err := r.loadModel(id, map[string]bool{})
				if err != nil {
					if strings.Contains(err.Error(), "unsupported built-in") {
						builtin++
						continue
					}
					t.Errorf("%s: %v", id, err)
					continue
				}
				count++
				if len(m.Elements) == 0 {
					empty++
				}
			}
			t.Logf("%s: %d block JSON models parsed, %d empty/template models, %d builtin parents requiring adapters", version, count, empty, builtin)
		})
	}
}

func TestPartialTransparentNeighborDoesNotEraseFullFace(t *testing.T) {
	for _, name := range []string{"test:custom_leaves", "test:glass"} {
		t.Run(name, func(t *testing.T) {
			_, short := splitID(name)
			a := fixture(t, map[string]string{
				"assets/test/blockstates/" + short + ".json": `{"variants":{"half=false":{"model":"test:block/stone"},"half=true":{"model":"test:block/half"}}}`,
				"assets/test/models/block/half.json":         strings.Replace(cube, `"to":[16,16,16]`, `"to":[16,8,16]`, 1),
			})
			s, err := Build(context.Background(), vol(map[[3]int]scene.Block{
				{0, 0, 0}: {Name: name, Properties: map[string]string{"half": "false"}},
				{1, 0, 0}: {Name: name, Properties: map[string]string{"half": "true"}},
			}), a, scene.MeshOptions{HollowLeaves: true})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range s.Meshes {
				for _, p := range m.Primitives {
					for i := 0; i < len(p.Positions); i += 3 {
						if p.Positions[i] == 9 && p.Positions[i+1] == 1 && p.Normals[i] > .9 {
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatal("partial transparent neighbor erased exposed upper half of full face")
			}
		})
	}
}

func TestStackedFluidHasContinuousSides(t *testing.T) {
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:water"}, {0, 1, 0}: {Name: "minecraft:water"}}), fixture(t, nil), scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.Meshes {
		for _, p := range m.Primitives {
			for i := 0; i < len(p.Positions); i += 3 {
				if p.Positions[i+1] == .875 && p.Normals[i] > .9 {
					found = true
				}
			}
		}
	}
	if found {
		t.Fatal("lower fluid side stops below the block above")
	}
}

func TestModLeafTagsPreserveInteriorByDefault(t *testing.T) {
	a := fixture(t, map[string]string{
		"data/minecraft/tags/block/leaves.json": `{"values":["#test:foliage"]}`,
		"data/test/tags/block/foliage.json":     `{"values":[{"id":"test:canopy","required":false}]}`,
		"assets/test/blockstates/canopy.json":   `{"variants":{"":{"model":"test:block/stone"}}}`,
	})
	v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:canopy"}, {1, 0, 0}: {Name: "test:canopy"}})
	s, err := Build(context.Background(), v, a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	full, _ := counts(s)
	if full != 48 {
		t.Fatalf("tagged leaves interior disappeared: %d vertices", full)
	}
	s, err = Build(context.Background(), v, a, scene.MeshOptions{HollowLeaves: true})
	if err != nil {
		t.Fatal(err)
	}
	hollow, _ := counts(s)
	if hollow != 40 {
		t.Fatalf("tagged leaves interior not removed: %d vertices", hollow)
	}
}

func TestUVLockUsesDestinationFaceOrientation(t *testing.T) {
	e := element{To: [3]float64{16, 16, 16}}
	v := variant{X: 90, UVLock: true}
	dir := "north"
	targetDir := rotatedDirection(dir, v)
	got := faceUV(e, dir, face{}, v)
	want := faceUV(e, targetDir, face{}, variant{})
	source, target := corners(e, dir), corners(e, targetDir)
	for i, p := range source {
		tp := transform(p, element{}, v)
		for j, q := range target {
			if distance(tp, q) < 1e-10 && got[i] != want[j] {
				t.Fatalf("UV lock differs from world face: got %v want %v", got[i], want[j])
			}
		}
	}
}
func distance(a, b point) float64 {
	d := 0.0
	for i := range a {
		d += (a[i] - b[i]) * (a[i] - b[i])
	}
	return d
}

func TestClientJARSmoke(t *testing.T) {
	for _, version := range []string{"1.20.1", "26.1.2"} {
		t.Run(version, func(t *testing.T) {
			jar := filepath.Join("..", "..", ".local-testdata", "minecraft-"+version+".jar")
			if _, err := os.Stat(jar); os.IsNotExist(err) {
				t.Skip("optional local client JAR unavailable")
			}
			a, err := assets.Open([]string{jar})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			cases := []scene.Block{
				{Name: "minecraft:stone"}, {Name: "minecraft:grass_block", Properties: map[string]string{"snowy": "false"}},
				{Name: "minecraft:oak_log", Properties: map[string]string{"axis": "x"}}, {Name: "minecraft:oak_leaves", Properties: map[string]string{"distance": "1", "persistent": "false", "waterlogged": "false"}},
				{Name: "minecraft:oak_stairs", Properties: map[string]string{"facing": "west", "half": "bottom", "shape": "inner_left", "waterlogged": "false"}},
				{Name: "minecraft:oak_fence", Properties: map[string]string{"north": "true", "south": "false", "east": "true", "west": "false", "waterlogged": "false"}},
				{Name: "minecraft:redstone_wire", Properties: map[string]string{"north": "side", "south": "up", "east": "none", "west": "side", "power": "15"}},
				{Name: "minecraft:water", Properties: map[string]string{"level": "0"}}, {Name: "minecraft:lava", Properties: map[string]string{"level": "0"}},
				{Name: "minecraft:glass"}, {Name: "minecraft:poppy"}, {Name: "minecraft:rail", Properties: map[string]string{"shape": "ascending_east", "waterlogged": "false"}},
			}
			v := &scene.Volume{Bounds: scene.Bounds{MaxX: len(cases) * 3, MaxY: 8, MaxZ: 8}, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
			for i, b := range cases {
				p := [3]int{i * 3, 0, 0}
				v.Blocks[p] = b
				v.Biomes[[3]int{p[0] / 4 * 4, 0, 0}] = "minecraft:swamp"
			}
			s, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Warnings) > 0 {
				t.Fatal(s.Warnings)
			}
			glb, err := exporter.GLB(s)
			if err != nil {
				t.Fatal(err)
			}
			vs, indices := counts(s)
			if vs < 12*4 || len(s.Materials) < len(cases) {
				t.Fatal("missing geometry/materials")
			}
			t.Logf("%s: %d blocks, %d vertices, %d triangles, %d materials, %d GLB bytes", version, len(cases), vs, indices/3, len(s.Materials), len(glb))
		})
	}
}
