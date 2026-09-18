package world

import (
	"astra-mwe/internal/nbt"
	"astra-mwe/internal/scene"
	fixture "astra-mwe/internal/world/testdata"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestLoadSurfaceMatchesFullLoadAcrossFormats(t *testing.T) {
	for _, version := range []int{1519, 1976, 2529, 2730, 3465, 4790} {
		for _, compression := range []byte{1, 2, 3} {
			t.Run(fmt.Sprintf("%d/%d", version, compression), func(t *testing.T) {
				dir := t.TempDir()
				if err := fixture.World(dir, version, compression); err != nil {
					t.Fatal(err)
				}
				r, err := Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				bounds := scene.Bounds{MinX: -19, MaxX: 2, MinY: -64, MaxY: 255, MinZ: -19, MaxZ: 2}
				assertSurfaceMatchesFull(t, r, "minecraft:overworld", bounds)
				assertSampledSurfaceMatchesFull(t, r, "minecraft:overworld", bounds, 2)
			})
		}
	}
}

func surfaceFixture(t testing.TB) (*Reader, scene.Bounds) {
	t.Helper()
	dir := t.TempDir()
	if err := fixture.World(dir, 3465, 2); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "dimensions", "test", "surface")
	sections := []any{}
	for _, sy := range []int8{0, -4, 2, 1, -3, -2, -1} {
		name := "minecraft:stone"
		if sy == 2 {
			name = "minecraft:air"
		}
		states := fixture.Compound{"palette": fixture.List{Type: 10, Values: []any{fixture.Compound{"Name": name}}}}
		if sy == 2 {
			states["palette"] = fixture.List{Type: 10, Values: []any{fixture.Compound{"Name": "minecraft:air"}, fixture.Compound{"Name": "minecraft:water", "Properties": fixture.Compound{"level": "0"}}, fixture.Compound{"Name": "minecraft:glass"}, fixture.Compound{"Name": "minecraft:short_grass"}}}
			words := make([]int64, 256)
			// One column has sparse transparent overlays, another has a tall plant.
			for _, item := range [][2]int{{0, 1}, {256, 2}, {15*256 + 1, 3}} {
				words[item[0]/16] |= int64(item[1]) << uint(item[0]%16*4)
			}
			states["data"] = words
		}
		sections = append(sections, fixture.Compound{"Y": sy, "block_states": states, "biomes": fixture.Compound{"palette": fixture.List{Type: 8, Values: []any{"minecraft:swamp"}}}})
	}
	chunk := fixture.Compound{"DataVersion": int32(3465), "xPos": int32(-1), "zPos": int32(-1), "sections": fixture.List{Type: 10, Values: sections}}
	if err := fixture.Chunk(custom, -1, -1, fixture.Encode(chunk), 2, true); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, scene.Bounds{MinX: -16, MaxX: -1, MinY: -64, MaxY: 63, MinZ: -16, MaxZ: -1}
}

func TestLoadSurfaceRetainsHighestEightIncludingOverlays(t *testing.T) {
	r, b := surfaceFixture(t)
	assertSurfaceMatchesFull(t, r, "test:surface", b)
	v, err := r.LoadSurface(context.Background(), "test:surface", b)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Blocks) != 2048 {
		t.Fatalf("got %d blocks, want eight per column", len(v.Blocks))
	}
	for _, item := range []struct {
		p    [3]int
		name string
	}{{[3]int{-16, 33, -16}, "minecraft:glass"}, {[3]int{-16, 32, -16}, "minecraft:water"}, {[3]int{-16, 26, -16}, "minecraft:stone"}, {[3]int{-15, 47, -16}, "minecraft:short_grass"}} {
		if got := v.Blocks[item.p].Name; got != item.name {
			t.Fatalf("%v: %s, want %s", item.p, got, item.name)
		}
	}
	if _, ok := v.Blocks[[3]int{-16, 25, -16}]; ok {
		t.Fatal("retained ninth block")
	}
	b.MinX, b.MaxX, b.MinZ, b.MaxZ, b.MaxY = -15, -15, -16, -16, 30
	assertSurfaceMatchesFull(t, r, "test:surface", b)
}

func TestLoadSurfaceSampledUsesGlobalGridAndRetainsBiomeHalo(t *testing.T) {
	r, b := surfaceFixture(t)
	b.MinX, b.MinZ = -15, -15
	v, err := r.LoadSurfaceSampled(context.Background(), "test:surface", b, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Blocks) != 72 {
		t.Fatalf("got %d blocks, want 9 sampled columns of 8", len(v.Blocks))
	}
	for p := range v.Blocks {
		if p[0]%4 != 0 || p[2]%4 != 0 {
			t.Fatalf("sample not on global grid: %v", p)
		}
	}
	if v.Blocks[[3]int{-12, 31, -12}].Name != "minecraft:stone" {
		t.Fatal("sampled surface missing")
	}
	if v.Biomes[[3]int{-16, 28, -16}] != "minecraft:swamp" {
		t.Fatal("negative biome halo sample missing")
	}
	if _, ok := v.Biomes[[3]int{-16, -64, -16}]; ok {
		t.Fatal("unneeded underground biome sample retained")
	}
	for _, step := range []int{0, 17} {
		if _, err := r.LoadSurfaceSampled(context.Background(), "test:surface", b, step); err == nil {
			t.Fatalf("invalid step %d accepted", step)
		}
	}
}

func assertSurfaceMatchesFull(t *testing.T, r *Reader, dimension string, b scene.Bounds) {
	t.Helper()
	full, err := r.Load(context.Background(), dimension, b)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := r.LoadSurface(context.Background(), dimension, b)
	if err != nil {
		t.Fatal(err)
	}
	columns := map[[2]int][]int{}
	for p := range full.Blocks {
		key := [2]int{p[0], p[2]}
		columns[key] = append(columns[key], p[1])
	}
	want := map[[3]int]scene.Block{}
	for xz, ys := range columns {
		sort.Sort(sort.Reverse(sort.IntSlice(ys)))
		if len(ys) > 8 {
			ys = ys[:8]
		}
		for _, y := range ys {
			p := [3]int{xz[0], y, xz[1]}
			want[p] = full.Blocks[p]
		}
	}
	if !reflect.DeepEqual(surface.Blocks, want) {
		t.Fatalf("surface does not match highest eight full-load blocks: got %d want %d", len(surface.Blocks), len(want))
	}
	if !reflect.DeepEqual(surface.Biomes, full.Biomes) {
		t.Fatal("biome samples differ")
	}
}

func assertSampledSurfaceMatchesFull(t *testing.T, r *Reader, dimension string, b scene.Bounds, step int) {
	t.Helper()
	full, err := r.LoadSurface(context.Background(), dimension, b)
	if err != nil {
		t.Fatal(err)
	}
	sampled, err := r.LoadSurfaceSampled(context.Background(), dimension, b, step)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[3]int]scene.Block{}
	for p, block := range full.Blocks {
		if p[0]%step == 0 && p[2]%step == 0 {
			want[p] = block
		}
	}
	if !reflect.DeepEqual(sampled.Blocks, want) {
		t.Fatalf("sampled blocks differ: got %d want %d", len(sampled.Blocks), len(want))
	}
	for p := range sampled.Blocks {
		for z := p[2] - 7; z <= p[2]+7; z++ {
			for x := p[0] - 7; x <= p[0]+7; x++ {
				if x < b.MinX || x > b.MaxX || z < b.MinZ || z > b.MaxZ {
					continue
				}
				key := [3]int{(x >> 2) * 4, (p[1] >> 2) * 4, (z >> 2) * 4}
				if sampled.Biomes[key] != full.Biomes[key] {
					t.Fatalf("sampled biome %v: got %q want %q", key, sampled.Biomes[key], full.Biomes[key])
				}
			}
		}
	}
}

func TestLoadSurfaceMissingCancelledMalformedAndWideBounds(t *testing.T) {
	r, b := surfaceFixture(t)
	b.MinX, b.MaxX, b.MinZ, b.MaxZ, b.MinY, b.MaxY = 1024, 1295, 1024, 1295, -64, 319
	v, err := r.LoadSurface(context.Background(), "test:surface", b)
	if err != nil || len(v.Blocks) != 0 {
		t.Fatalf("absent wide surface: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.LoadSurface(ctx, "test:surface", b); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	b.MinX, b.MaxX, b.MinZ, b.MaxZ = -16, -1, -16, -1
	path := filepath.Join(r.dimensions["test:surface"], "region", "c.-1.-1.mcc")
	if err := os.WriteFile(path, []byte("invalid compressed NBT"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadSurface(context.Background(), "test:surface", b); err == nil || !strings.Contains(err.Error(), "(-1,-1)") {
		t.Fatalf("malformed chunk: %v", err)
	}
	if _, err := r.LoadSurface(context.Background(), "unknown", b); err == nil {
		t.Fatal("unknown dimension accepted")
	}
}

func TestSurfaceRejectsMalformedUndergroundSections(t *testing.T) {
	stone := nbt.Compound{"palette": []any{nbt.Compound{"Name": "minecraft:stone"}}}
	badIndices := make([]int64, 256)
	badIndices[0] = 15
	for _, tc := range []struct {
		name    string
		section nbt.Compound
	}{
		{"missing palette", nbt.Compound{"Y": int8(0), "block_states": nbt.Compound{"data": []int64{1}}}},
		{"wrong data type", nbt.Compound{"Y": int8(0), "block_states": nbt.Compound{"palette": stone["palette"], "data": "bad"}}},
		{"truncated data", nbt.Compound{"Y": int8(0), "block_states": nbt.Compound{"palette": stone["palette"], "data": []int64{0}}}},
		{"out of range index", nbt.Compound{"Y": int8(0), "block_states": nbt.Compound{"palette": stone["palette"], "data": badIndices}}},
		{"duplicate section", nbt.Compound{"Y": int8(1), "block_states": stone}},
		{"missing y", nbt.Compound{"block_states": stone}},
		{"invalid biome", nbt.Compound{"Y": int8(0), "biomes": nbt.Compound{"palette": []any{int32(1)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := nbt.Compound{"sections": []any{nbt.Compound{"Y": int8(1), "block_states": stone}, tc.section}}
			v := &scene.Volume{Bounds: scene.Bounds{MaxX: 15, MaxZ: 15, MaxY: 31}, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
			if err := decodeChunkMode(context.Background(), root, 0, 0, 3465, v, true); err == nil {
				t.Fatal("malformed section accepted after top eight layers were retained")
			}
		})
	}
}

func TestUserSuppliedWorldSurfaceMatchesFullLoad(t *testing.T) {
	path := os.Getenv("ASTRA_TEST_WORLD")
	if path == "" {
		t.Skip("ASTRA_TEST_WORLD is not set")
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	p, dim := r.Info.Spawn, r.Info.SpawnDimension
	if len(r.Info.Players) > 0 {
		p, dim = r.Info.Players[0].Position, r.Info.Players[0].Dimension
	}
	x, z := int(math.Floor(p[0])), int(math.Floor(p[2]))
	lo, hi := r.DimensionBounds(dim)
	assertSurfaceMatchesFull(t, r, dim, scene.Bounds{MinX: x - 15, MaxX: x + 16, MinZ: z - 15, MaxZ: z + 16, MinY: lo, MaxY: hi})
	for _, step := range []int{2, 4, 8, 16} {
		assertSampledSurfaceMatchesFull(t, r, dim, scene.Bounds{MinX: x - 31, MaxX: x + 32, MinZ: z - 31, MaxZ: z + 32, MinY: lo, MaxY: hi}, step)
	}
}

func BenchmarkLoadSurfaceSynthetic(b *testing.B) {
	r, bounds := surfaceFixture(b)
	benchmarkSurfaceReaders(b, r, "test:surface", bounds)
}

func BenchmarkLoadSurfaceUserWorld(b *testing.B) {
	path := os.Getenv("ASTRA_TEST_WORLD")
	if path == "" {
		b.Skip("ASTRA_TEST_WORLD is not set")
	}
	r, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	p, dim := r.Info.Spawn, r.Info.SpawnDimension
	if len(r.Info.Players) > 0 {
		p, dim = r.Info.Players[0].Position, r.Info.Players[0].Dimension
	}
	x, z := int(math.Floor(p[0])), int(math.Floor(p[2]))
	lo, hi := r.DimensionBounds(dim)
	benchmarkSurfaceReaders(b, r, dim, scene.Bounds{MinX: x - 31, MaxX: x + 32, MinZ: z - 31, MaxZ: z + 32, MinY: lo, MaxY: hi})
}

func BenchmarkLoadSurfaceSampledUserWorld(b *testing.B) {
	path := os.Getenv("ASTRA_TEST_WORLD")
	if path == "" {
		b.Skip("ASTRA_TEST_WORLD is not set")
	}
	r, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	p, dim := r.Info.Spawn, r.Info.SpawnDimension
	if len(r.Info.Players) > 0 {
		p, dim = r.Info.Players[0].Position, r.Info.Players[0].Dimension
	}
	x, z := int(math.Floor(p[0])), int(math.Floor(p[2]))
	lo, hi := r.DimensionBounds(dim)
	for _, step := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("step%d", step), func(b *testing.B) {
			bounds := scene.Bounds{MinX: x - 32*step - 7, MaxX: x + 32*step + 6, MinZ: z - 32*step - 7, MaxZ: z + 32*step + 6, MinY: lo, MaxY: hi}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				v, err := r.LoadSurfaceSampled(context.Background(), dim, bounds, step)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(v.Blocks)), "blocks")
				b.ReportMetric(float64(len(v.Biomes)), "biomes")
			}
		})
	}
}

func benchmarkSurfaceReaders(b *testing.B, r *Reader, dim string, bounds scene.Bounds) {
	for _, tc := range []struct {
		name string
		load func(context.Context, string, scene.Bounds) (*scene.Volume, error)
	}{{"Full", r.Load}, {"Surface", r.LoadSurface}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				v, err := tc.load(context.Background(), dim, bounds)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(v.Blocks)), "blocks")
			}
		})
	}
}
