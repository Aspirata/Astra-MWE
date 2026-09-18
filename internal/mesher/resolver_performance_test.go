package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"astra-mwe/internal/world"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"testing"
)

// Opt-in read-only benchmark. The digest allows geometry, UVs, tints, textures,
// material ordering and warnings to be compared before/after an optimization.
func BenchmarkBuildUserWorld(b *testing.B) {
	path, jar := os.Getenv("ASTRA_BENCH_WORLD"), os.Getenv("ASTRA_BENCH_JAR")
	if path == "" || jar == "" {
		b.Skip("set ASTRA_BENCH_WORLD and ASTRA_BENCH_JAR")
	}
	r, err := world.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	a, err := assets.Open([]string{jar})
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	box := scene.Bounds{MinX: 98 * 32, MaxX: 98*32 + 31, MinZ: 40 * 32, MaxZ: 40*32 + 31, MinY: -64, MaxY: 319}
	halo := box
	halo.MinX -= 7
	halo.MaxX += 7
	halo.MinZ -= 7
	halo.MaxZ += 7
	v, err := r.Load(context.Background(), "minecraft:overworld", halo)
	if err != nil {
		b.Fatal(err)
	}
	v.Bounds = box
	b.ReportAllocs()
	b.ResetTimer()
	var sc *scene.Scene
	for i := 0; i < b.N; i++ {
		sc, err = Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true, BiomeBlend: 7})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	encoded, err := json.Marshal(sc)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("scene SHA256 %x", sha256.Sum256(encoded))
}

func TestPositionSeedPreservesSignedCoordinatesWithoutAllocating(t *testing.T) {
	for _, pos := range [][3]int{{0, 0, 0}, {-30000000, -64, 30000000}, {3456, 255, -8123}} {
		h := fnv.New64a()
		fmt.Fprintf(h, "%s/%d/%d/%d", "minecraft:stone", pos[0], pos[1], pos[2])
		if got := positionSeed("minecraft:stone", pos); got != h.Sum64() {
			t.Fatalf("seed at %v = %d, want %d", pos, got, h.Sum64())
		}
	}
	allocs := testing.AllocsPerRun(100, func() { positionSeed("minecraft:stone", [3]int{3456, 255, -8123}) })
	if allocs != 0 {
		t.Fatalf("position hashing allocated %.0f objects per block", allocs)
	}
}

// Repeated stone blocks must reuse prepared geometry instead of decoding their
// identical JSON blockstate again. Allocations here multiply by buried blocks.
func TestRepeatedStaticResolutionDoesNotAllocate(t *testing.T) {
	a := fixture(t, nil)
	r := resolver{assets: a, models: map[string]modelResult{}, states: map[string]stateResult{}}
	b := scene.Block{Name: "test:stone", Properties: map[string]string{"axis": "y"}}
	if _, err := r.resolve(b, [3]int{}); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := r.resolve(b, [3]int{17, -30, 24}); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("repeated static resolution allocated %.0f objects per block; want at most 1", allocs)
	}
}

func TestPreparedResolutionPreservesWeightedMultipart(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/test/blockstates/weighted.json": `{"variants":{"facing=north":[{"model":"test:block/stone","y":90,"weight":2},{"model":"test:block/stone","y":180}],"":{"model":"test:block/stone"}},"multipart":[{"when":{"OR":[{"powered":"true"},{"facing":"south"}]},"apply":[{"model":"test:block/stone","x":90},{"model":"test:block/stone","x":180,"weight":3}]}]}`,
	})
	r := resolver{assets: a, models: map[string]modelResult{}, states: map[string]stateResult{}}
	for _, props := range []map[string]string{{"facing": "north", "powered": "true"}, {"facing": "south"}, {"facing": "north", "powered": "false"}, {"facing": "east"}} {
		for x := -24; x < 24; x++ {
			pos := [3]int{x, -5, x * 3}
			got, err := r.resolve(scene.Block{Name: "test:weighted", Properties: props}, pos)
			if err != nil {
				t.Fatal(err)
			}
			seed := positionSeed("test:weighted", pos)
			wantY := 0
			if props["facing"] == "north" {
				wantY = 90
				if seed%3 == 2 {
					wantY = 180
				}
			}
			wantCount := 1
			if props["powered"] == "true" || props["facing"] == "south" {
				wantCount = 2
			}
			if len(got) != wantCount || got[0].transform.Y != wantY {
				t.Fatalf("%v at %v: unexpected variant: %+v", props, pos, got)
			}
			if wantCount == 2 {
				wantX := 180
				if seed%4 == 0 {
					wantX = 90
				}
				if got[1].transform.X != wantX {
					t.Fatalf("%v: multipart rotation=%d want %d", pos, got[1].transform.X, wantX)
				}
			}
		}
	}
}

func BenchmarkResolverRepeatedTerrain(b *testing.B) {
	m := cubeModel("test:block/stone", false)
	for _, weighted := range []bool{false, true} {
		name, raw := "static", `{"model":"test:block/stone"}`
		if weighted {
			name, raw = "weighted", `[{"model":"test:block/stone","y":0},{"model":"test:block/stone","y":90},{"model":"test:block/stone","y":180},{"model":"test:block/stone","y":270}]`
		}
		b.Run(name, func(b *testing.B) {
			r := resolver{models: map[string]modelResult{"test:block/stone": {value: m}}, states: map[string]stateResult{"test:stone": {value: &blockstate{Variants: map[string]json.RawMessage{"": json.RawMessage(raw)}}}}}
			block := scene.Block{Name: "test:stone"}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := r.resolve(block, [3]int{i % 32, 64, i / 32}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
