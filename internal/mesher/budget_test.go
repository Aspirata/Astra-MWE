package mesher

import (
	"astra-mwe/internal/scene"
	"context"
	"strings"
	"testing"
)

func TestFaceBudgetCountsOnlyEmittedFaces(t *testing.T) {
	a := fixture(t, nil)
	v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}, {1, 0, 0}: {Name: "test:stone"}})
	if _, err := buildWithLimit(context.Background(), v, a, scene.MeshOptions{}, 10); err != nil {
		t.Fatal(err)
	}
	if s, err := buildWithLimit(context.Background(), v, a, scene.MeshOptions{}, 9); err == nil || !strings.Contains(err.Error(), "face budget") || s != nil {
		t.Fatalf("expected bounded failure, got %v %v", s, err)
	}
}
