package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/exporter"
	"astra-mwe/internal/scene"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorldSingleMeshAcrossChunks(t *testing.T) {
	v := &scene.Volume{Bounds: scene.Bounds{MinX: -16, MaxX: 17, MinY: 0, MaxY: 1, MinZ: 0, MaxZ: 0}, Blocks: map[[3]int]scene.Block{
		{-16, 0, 0}: {Name: "test:stone"}, {0, 0, 0}: {Name: "test:stone"}, {17, 0, 0}: {Name: "test:other"},
	}}
	s, err := Build(context.Background(), v, fixture(t, nil), scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meshes) != 1 {
		t.Fatalf("world split into %d objects", len(s.Meshes))
	}
	if len(s.Meshes[0].Primitives) != 2 {
		t.Fatalf("material slots lost: %+v", s.Meshes[0])
	}
	vs, indices := counts(s)
	if vs != 72 || indices != 108 {
		t.Fatalf("geometry lost: %d vertices, %d indices", vs, indices)
	}
	for _, p := range s.Meshes[0].Primitives {
		for i, idx := range p.Indices {
			if idx != uint32(i/6*4)+[]uint32{0, 1, 2, 0, 2, 3}[i%6] {
				t.Fatalf("face index was not rebased: %d", idx)
			}
		}
		if len(p.Normals) != len(p.Positions) || len(p.UVs)*3 != len(p.Positions)*2 || len(p.Colors)*3 != len(p.Positions)*4 {
			t.Fatal("merged attributes misaligned")
		}
	}
	b, err := exporter.GLB(s)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes  []json.RawMessage
		Meshes []json.RawMessage
	}
	if err := json.Unmarshal(b[20:20+binary.LittleEndian.Uint32(b[12:16])], &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Nodes) != 1 || len(doc.Meshes) != 1 {
		t.Fatal("GLB has multiple world objects")
	}
}

func TestOverlayExcludedBeforeMaterialCreation(t *testing.T) {
	for _, tid := range []string{"minecraft:block/glass_block_side_overlay", "other:custom/glass_block_side_overlay.png", "glass_block_side_overlay", "glass_block_side_overlay.png"} {
		t.Run(tid, func(t *testing.T) {
			m := fmt.Sprintf(`{"textures":{"all":%q},"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"north":{"texture":"#all"}}}]}`, tid)
			a := fixture(t, map[string]string{"assets/test/models/block/stone.json": m})
			s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}}), a, scene.MeshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Meshes) != 0 || len(s.Materials) != 0 || len(s.Warnings) != 0 {
				t.Fatalf("overlay left geometry, materials or fallback warning: %+v", s)
			}
		})
	}
}

func TestShortMaterialNamesKeepSeparateSources(t *testing.T) {
	a := fixture(t, map[string]string{"assets/test/models/block/stone.json": `{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"north":{"texture":"test:block/stone"},"south":{"texture":"test:block/other"}}}]}`})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}}), a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Materials) != 2 || len(s.Meshes[0].Primitives) != 2 {
		t.Fatal("distinct source materials combined")
	}
	for _, m := range s.Materials {
		if m.Name != "test:stone" {
			t.Fatalf("material name retains source path: %q", m.Name)
		}
	}
}

func TestFlatOppositeFacesCollapseWithTwoSidedMaterial(t *testing.T) {
	for _, axis := range []struct{ from, to, first, second string }{{"0,0,8", "16,16,8", "north", "south"}, {"8,0,0", "8,16,16", "west", "east"}, {"0,8,0", "16,8,16", "down", "up"}} {
		t.Run(axis.first, func(t *testing.T) {
			m := fmt.Sprintf(`{"elements":[{"from":[%s],"to":[%s],"rotation":{"origin":[8,8,8],"axis":"y","angle":45,"rescale":true},"faces":{%q:{"texture":"test:block/stone","uv":[0,0,16,16]},%q:{"texture":"test:block/stone","uv":[0,0,16,16]}}}]}`, axis.from, axis.to, axis.first, axis.second)
			s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}}), fixture(t, map[string]string{"assets/test/models/block/stone.json": m}), scene.MeshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			vs, is := counts(s)
			if vs != 4 || is != 6 {
				t.Fatalf("coincident reverse faces remain: %d vertices %d indices", vs, is)
			}
			if !s.Materials[0].DoubleSided {
				t.Fatal("back face disappeared")
			}
		})
	}
}

func TestClientJARCrossSingleSurfaces(t *testing.T) {
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
			grass := "minecraft:grass"
			if version == "26.1.2" {
				grass = "minecraft:short_grass"
			}
			for _, name := range []string{grass, "minecraft:poppy"} {
				v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: name}})
				v.Biomes = map[[3]int]string{{0, 0, 0}: "minecraft:plains"}
				s, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
				if err != nil {
					t.Fatal(err)
				}
				vs, is := counts(s)
				if vs != 8 || is != 12 {
					t.Fatalf("%s has duplicate cross surfaces: %d vertices %d indices", name, vs, is)
				}
				if len(s.Materials) != 1 || !s.Materials[0].DoubleSided || s.Materials[0].Alpha != "MASK" {
					t.Fatalf("%s lost two-sided cutout", name)
				}
				if len(s.Warnings) != 0 {
					t.Fatal(s.Warnings)
				}
				if strings.Contains(s.Materials[0].Name, "/") {
					t.Fatal("source remains in material name")
				}
			}
		})
	}
}
