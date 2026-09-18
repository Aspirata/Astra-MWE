package mesher

import (
	"astra-mwe/internal/scene"
	"context"
	"testing"
)

func TestViewHaloOccludesBoundaryWithoutEmittingNeighbor(t *testing.T) {
	a := fixture(t, nil)
	v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}, {1, 0, 0}: {Name: "test:stone"}})
	v.Bounds = scene.Bounds{MinX: 0, MaxX: 0, MinY: 0, MaxY: 0, MinZ: 0, MaxZ: 0}
	sc, err := Build(context.Background(), v, a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, indices := counts(sc)
	if indices != 30 {
		t.Fatalf("tile seam: got %d indices, want five faces", indices)
	}
	for _, m := range sc.Meshes {
		for _, p := range m.Primitives {
			for i := 0; i < len(p.Positions); i += 3 {
				if p.Positions[i] > 1 {
					t.Fatal("neighbor leaked into tile")
				}
			}
		}
	}
}
