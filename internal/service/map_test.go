package service

import (
	"bytes"
	"context"
	"image/png"
	"testing"
)

func TestMapTileIsPNGIndependentOfExportAndCached(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	info, _ := s.Demo()
	req := MapRequest{World: info.Path, Dimension: "minecraft:overworld", Step: 1}
	tile, err := s.MapTile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(tile.Data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatal(img.Bounds())
	}
	if s.Status().Preview != nil || s.Status().Busy {
		t.Fatal("map generated an export mesh")
	}
	again, err := s.MapTile(context.Background(), req)
	if err != nil || again != tile {
		t.Fatal("unchanged map was regenerated", err)
	}
	s.Demo()
	fresh, err := s.MapTile(context.Background(), req)
	if err != nil || fresh == tile {
		t.Fatal("reopening world reused stale map", err)
	}
	req.Step = 3
	if _, err = s.MapTile(context.Background(), req); err == nil {
		t.Fatal("invalid scale accepted")
	}
}
