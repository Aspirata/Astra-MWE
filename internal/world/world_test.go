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
	"strings"
	"testing"
)

func TestReadsAnvilVersionsCompressionsAndNegativeCoordinates(t *testing.T) {
	for _, version := range []int{1519, 1976, 2529, 2730, 3465, 4790} {
		for _, compression := range []byte{1, 2, 3} {
			t.Run(fmt.Sprintf("version_%d_compression_%d", version, compression), func(t *testing.T) {
				dir := t.TempDir()
				if err := fixture.World(dir, version, compression); err != nil {
					t.Fatal(err)
				}
				r, err := Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				y := 0
				if version >= 2844 {
					y = -64
				}
				b := scene.Bounds{MinX: -16, MaxX: -1, MinY: y, MaxY: y + 15, MinZ: -16, MaxZ: -1}
				v, err := r.Load(context.Background(), "minecraft:overworld", b)
				if err != nil {
					t.Fatal(err)
				}
				if len(v.Blocks) != 2 || v.Blocks[[3]int{-1, y, -1}].Properties["axis"] != "x" || v.Blocks[[3]int{-16, y + 1, -16}].Name != "minecraft:stone" {
					t.Fatalf("wrong blocks: %#v", v.Blocks)
				}
				if v.Biomes[[3]int{-4, y, -4}] != "minecraft:desert" {
					t.Fatalf("wrong biome: %#v", v.Biomes)
				}
				if len(r.Info.Players) != 1 || r.Info.Players[0].Dimension != "minecraft:the_nether" || r.Info.Players[0].Position[0] != -1.5 || r.Info.Players[0].UUID != "12345678-1234-1234-1234-123412345678" {
					t.Fatalf("wrong player: %#v", r.Info.Players)
				}
			})
		}
	}
}

func TestWorld26SpawnAndPrimaryPlayer(t *testing.T) {
	dir := t.TempDir()
	if err := fixture.World(dir, 4790, 2); err != nil {
		t.Fatal(err)
	}
	other := fixture.Compound{"Pos": fixture.List{Type: 6, Values: []any{float64(100), float64(70), float64(100)}}, "Dimension": "minecraft:overworld"}
	if err := fixture.GzipNBT(filepath.Join(dir, "players", "data", "00000000-0000-0000-0000-000000000000.dat"), other); err != nil {
		t.Fatal(err)
	}
	r, err := Open(filepath.Join(dir, "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Info.Spawn != (scene.Vec3{-2, 64, 3}) {
		t.Fatalf("new spawn was lost: %v", r.Info.Spawn)
	}
	if r.Info.SpawnDimension != "minecraft:overworld" {
		t.Fatal(r.Info.SpawnDimension)
	}
	if len(r.Info.Players) != 2 || r.Info.Players[0].UUID != "12345678-1234-1234-1234-123412345678" {
		t.Fatalf("local player not prioritized: %v", r.Info.Players)
	}
}

func TestDimensionBuildBounds(t *testing.T) {
	dir := t.TempDir()
	data := fixture.Compound{"Data": fixture.Compound{"DataVersion": int32(3465), "WorldGenSettings": fixture.Compound{"dimensions": fixture.Compound{"mod:high": fixture.Compound{"type": fixture.Compound{"min_y": int32(-128), "height": int32(640)}}}}}}
	if err := fixture.GzipNBT(filepath.Join(dir, "level.dat"), data); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		d        string
		min, max int
	}{{"minecraft:overworld", -64, 319}, {"minecraft:the_nether", 0, 255}, {"minecraft:the_end", 0, 255}, {"mod:high", -128, 511}} {
		min, max := r.DimensionBounds(tc.d)
		if min != tc.min || max != tc.max {
			t.Fatalf("%s: %d %d", tc.d, min, max)
		}
	}
}

// Explicit opt-in only: the supplied save is opened read-only and never modified.
func TestUserSuppliedWorld(t *testing.T) {
	path := os.Getenv("ASTRA_TEST_WORLD")
	if path == "" {
		t.Skip("ASTRA_TEST_WORLD is not set")
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	p := r.Info.Spawn
	dim := r.Info.SpawnDimension
	if len(r.Info.Players) > 0 {
		p = r.Info.Players[0].Position
		dim = r.Info.Players[0].Dimension
	}
	x, y, z := int(math.Floor(p[0])), int(math.Floor(p[1])), int(math.Floor(p[2]))
	b := scene.Bounds{MinX: x - 8, MaxX: x + 8, MinY: y - 8, MaxY: y + 8, MinZ: z - 8, MaxZ: z + 8}
	v, err := r.Load(context.Background(), dim, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Opened %s (%s), %d dimensions, %d players; loaded %d blocks near %s position %v", r.Info.Name, r.Info.Version, len(r.Info.Dimensions), len(r.Info.Players), len(v.Blocks), dim, p)
}

func TestMalformedPresentStatesAreNotSilentlyAir(t *testing.T) {
	for _, states := range []fixture.Compound{{"data": []int64{1}}, {"palette": fixture.List{Type: 10, Values: []any{fixture.Compound{"Name": "minecraft:stone"}}}, "data": "bad"}} {
		root := nbt.Compound{"sections": []any{nbt.Compound{"Y": int8(0), "block_states": states}}}
		v := &scene.Volume{Bounds: scene.Bounds{}, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
		if err := decodeChunk(context.Background(), root, 0, 0, 3465, v); err == nil {
			t.Fatal("invalid present states silently accepted")
		}
	}
}
func TestMalformedRegionReportsCoordinatesAndCancellation(t *testing.T) {
	dir := t.TempDir()
	fixture.World(dir, 3465, 2)
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := scene.Bounds{MinX: -16, MaxX: -1, MinY: -64, MaxY: 0, MinZ: -16, MaxZ: -1}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.Load(ctx, "minecraft:overworld", b); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	path := filepath.Join(dir, "region", "r.-1.-1.mca")
	raw, _ := os.ReadFile(path)
	raw[8192] = 127
	os.WriteFile(path, raw, 0644)
	if _, err = r.Load(context.Background(), "minecraft:overworld", b); err == nil || !strings.Contains(err.Error(), "(-1,-1)") {
		t.Fatalf("missing corruption diagnostics: %v", err)
	}
	if _, err = r.Load(context.Background(), "../../escape", b); err == nil {
		t.Fatal("unknown dimension accepted")
	}
}
func TestCustomDimensionsExternalChunksAndSinglePalette(t *testing.T) {
	dir := t.TempDir()
	fixture.World(dir, 3465, 2)
	custom := filepath.Join(dir, "dimensions", "mod", "caves", "deep")
	os.MkdirAll(filepath.Join(custom, "region"), 0755)
	chunk := fixture.Compound{"DataVersion": int32(3465), "sections": fixture.List{Type: 10, Values: []any{fixture.Compound{"Y": int8(-4), "block_states": fixture.Compound{"palette": fixture.List{Type: 10, Values: []any{fixture.Compound{"Name": "minecraft:stone"}}}}}}}}
	if err := fixture.Chunk(custom, 0, 0, fixture.Encode(chunk), 2, true); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.Load(context.Background(), "mod:caves/deep", scene.Bounds{MinY: -64, MaxY: -64})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Blocks) != 1 || v.Blocks[[3]int{0, -64, 0}].Name != "minecraft:stone" {
		t.Fatal(v.Blocks)
	}
}
func TestRejectsTruncatedPaletteData(t *testing.T) {
	_, err := unpack([]int64{1}, 18, 4096, 4, true)
	if err == nil {
		t.Fatal("truncated packed palette accepted")
	}
	_, err = unpack([]int64{15}, 2, 1, 4, true)
	if err == nil {
		t.Fatal("invalid palette index accepted")
	}
}
