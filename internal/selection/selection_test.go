package selection

import (
	"astra-mwe/internal/scene"
	"testing"
)

func TestAutoMinUsesLowestSurfaceNotWorldFloor(t *testing.T) {
	v := &scene.Volume{Bounds: scene.Bounds{MinX: 0, MaxX: 3, MinY: -64, MaxY: 100, MinZ: 0, MaxZ: 0}, Blocks: map[[3]int]scene.Block{}}
	for _, b := range []struct {
		x, y int
		name string
	}{{0, -64, "bedrock"}, {0, 63, "grass_block"}, {0, 70, "oak_log"}, {0, 75, "oak_leaves"}, {1, 60, "sand"}, {1, 63, "water"}, {2, 65, "stone"}, {2, 66, "short_grass"}} {
		v.Blocks[[3]int{b.x, b.y, 0}] = scene.Block{Name: "minecraft:" + b.name}
	}
	got, err := AutoMin(v, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 56 {
		t.Fatalf("got %d, want lowest terrain 60 minus minimum padding 4", got)
	}
	got, err = AutoMin(v, 8)
	if err != nil || got != 52 {
		t.Fatalf("padding got %d, %v", got, err)
	}
}
func TestAutoMinClampsAndRejectsEmpty(t *testing.T) {
	v := &scene.Volume{Bounds: scene.Bounds{MinY: -64, MaxY: 0}, Blocks: map[[3]int]scene.Block{{0, -63, 0}: {Name: "minecraft:stone"}}}
	if got, err := AutoMin(v, 4); err != nil || got != -64 {
		t.Fatalf("got %d, %v", got, err)
	}
	v.Blocks = map[[3]int]scene.Block{{0, -50, 0}: {Name: "minecraft:water"}}
	if _, err := AutoMin(v, 4); err == nil {
		t.Fatal("empty terrain must request manual bounds")
	}
}
