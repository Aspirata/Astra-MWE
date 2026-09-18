package world

import (
	"astra-mwe/internal/scene"
	fixture "astra-mwe/internal/world/testdata"
	"context"
	"strings"
	"testing"
)

func TestOccupiedBlockBudgetStopsReading(t *testing.T) {
	dir := t.TempDir()
	if err := fixture.World(dir, 3465, 2); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	bounds := scene.Bounds{MinX: -16, MaxX: -1, MinY: -64, MaxY: -49, MinZ: -16, MaxZ: -1}
	if _, err := r.loadWithLimit(context.Background(), "minecraft:overworld", bounds, 2); err != nil {
		t.Fatal(err)
	}
	if v, err := r.loadWithLimit(context.Background(), "minecraft:overworld", bounds, 1); err == nil || !strings.Contains(err.Error(), "occupied block memory budget") || v != nil {
		t.Fatalf("wanted bounded failure, got %v %v", v, err)
	}
}
