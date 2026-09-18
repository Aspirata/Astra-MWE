package exporter

import (
	"astra-mwe/internal/scene"
	"strings"
	"testing"
)

func TestGLBRejectsOversizedBuffersBeforeSerialization(t *testing.T) {
	// Shared bytes still get embedded separately for each material.
	png := []byte("\x89PNG\r\n\x1a\n")
	s := &scene.Scene{Materials: []scene.Material{{PNG: png}, {PNG: png}}}
	if _, err := glbWithLimit(s, 12); err == nil || !strings.Contains(err.Error(), "memory budget") {
		t.Fatalf("expected preflight rejection, got %v", err)
	}
	if _, err := glbWithLimit(s, 4096); err != nil {
		t.Fatal(err)
	}
}
