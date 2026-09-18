package service

import (
	"astra-mwe/internal/scene"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectionHasNoAreaLimit(t *testing.T) {
	for _, b := range []scene.Bounds{{MaxX: 1024, MaxY: 319, MaxZ: 1024}, {MinX: -30000000, MaxX: 30000000, MinY: -2048, MaxY: 2047, MinZ: -30000000, MaxZ: 30000000}} {
		if err := ValidateBounds(b); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWideTiledBuildPreservesFacesAndUsesDisk(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	bounds := scene.Bounds{MinX: -300, MaxX: 299, MinZ: 0, MaxZ: 0, MinY: 0, MaxY: 20}
	maxLoad := 0
	load := func(ctx context.Context, b scene.Bounds) (*scene.Volume, error) {
		if n := (b.MaxX - b.MinX + 1) * (b.MaxZ - b.MinZ + 1) * (b.MaxY - b.MinY + 1); n > maxLoad {
			maxLoad = n
		}
		v := &scene.Volume{Bounds: b, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
		if b.MinZ <= 0 && b.MaxZ >= 0 && b.MinY <= 2 && b.MaxY >= 2 {
			for x := max(-300, b.MinX); x <= min(299, b.MaxX); x++ {
				v.Blocks[[3]int{x, 2, 0}] = scene.Block{Name: "minecraft:stone"}
			}
		}
		return v, ctx.Err()
	}
	req := BuildRequest{Bounds: bounds, Dimension: "minecraft:overworld", AutoMin: true, Padding: 4, BiomeColors: true, BiomeBlend: 7}
	info := scene.WorldInfo{Path: "demo", MinY: 0, MaxY: 20, Players: []scene.Player{{Name: "Test", Dimension: req.Dimension, Position: scene.Vec3{0, 3, 0}}}}
	preview, out, err := s.buildTiledWithLoader(context.Background(), req, true, info, load, visitRectangle)
	if err != nil {
		t.Fatal(err)
	}
	defer out.cleanup()
	if len(out.export) != 0 || len(out.preview) != 0 || out.exportFile == "" || out.previewFile == out.exportFile {
		t.Fatal("large export not on disk or player toggle lost")
	}
	if preview.Bounds.MinY != 0 || preview.Bounds.MaxY != 4 || preview.Blocks != 600 || preview.WorldTrianglesAfter != 4804 {
		t.Fatalf("wrong geometry/height: %+v", preview)
	}
	if maxLoad > 20000 {
		t.Fatal("loaded whole selection", maxLoad)
	}
	data, err := os.ReadFile(out.exportFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Meshes    []struct{ Primitives []struct{ Indices int } }
		Materials []any
		Accessors []struct{ Count int }
		Images    []struct{ Name string }
	}
	if err = json.Unmarshal(data[20:20+binary.LittleEndian.Uint32(data[12:])], &doc); err != nil {
		t.Fatal(err)
	}
	tri := 0
	for _, m := range doc.Meshes {
		for _, p := range m.Primitives {
			tri += doc.Accessors[p.Indices].Count / 3
		}
	}
	if len(doc.Meshes) != 1 || len(doc.Materials) != 1 || tri != 4804 || len(doc.Images) != 1 || doc.Images[0].Name != "stone.png" {
		t.Fatalf("stream changed material/texture/geometry: %+v", doc)
	}
	if remains, _ := filepath.Glob(filepath.Join(s.cache, "astra-geometry-*.bin")); len(remains) != 0 {
		t.Fatal("geometry spool leaked", remains)
	}
}

func TestTiledBuildCancellationCleansTemporaryFiles(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	load := func(ctx context.Context, b scene.Bounds) (*scene.Volume, error) {
		calls++
		if calls == 2 {
			cancel()
			return nil, ctx.Err()
		}
		return DemoVolume(b), nil
	}
	_, _, err := s.buildTiledWithLoader(ctx, BuildRequest{Bounds: scene.Bounds{MaxX: 64, MaxZ: 1, MinY: 60, MaxY: 70}}, true, scene.WorldInfo{}, load, visitRectangle)
	if err != context.Canceled {
		t.Fatal(err)
	}
	for _, pattern := range []string{"astra-geometry-*.bin", "astra-build-*.glb"} {
		if files, _ := filepath.Glob(filepath.Join(s.cache, pattern)); len(files) != 0 {
			t.Fatal("cancelled build leaked files", files)
		}
	}
}
