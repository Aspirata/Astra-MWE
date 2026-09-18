package service

import (
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDemoBuildProducesGLB(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/demo", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	data, _ := json.Marshal(BuildRequest{Bounds: scene.Bounds{MinX: -8, MaxX: 8, MinY: 0, MaxY: 100, MinZ: -8, MaxZ: 8}, Dimension: "minecraft:overworld", AutoMin: true, Padding: 4, BiomeColors: true, BiomeBlend: 2})
	req := httptest.NewRequest("POST", "/api/build", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	deadline := time.Now().Add(20 * time.Second)
	for s.Status().Busy && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	st := s.Status()
	if st.Busy || st.Error != "" {
		t.Fatalf("build failed: %+v", st)
	}
	if len(st.Preview.Warnings) != 0 {
		t.Fatalf("biome blending should load its border samples: %v", st.Preview.Warnings)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/preview.glb", nil))
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("glTF")) {
		t.Fatal("preview must be an actual binary GLB")
	}
	if st.Preview.Bounds.MinY >= 60 {
		t.Fatal("auto minimum should follow low terrain plus four blocks")
	}
	if st.Preview.Bounds.MaxY >= 100 {
		t.Fatal("automatic height must trim empty space above the terrain")
	}
}
func TestRejectOutOfWorldSelection(t *testing.T) {
	b := scene.Bounds{MinX: -30000001, MaxX: 100000, MinY: -64, MaxY: 319, MinZ: 0, MaxZ: 200000}
	if ValidateBounds(b) == nil {
		t.Fatal("out-of-world request must be rejected")
	}
}
func TestAtomicExportPreservesExistingAndCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.glb")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if WriteFile(context.Background(), path, []byte("replacement")) == nil {
		t.Fatal("must not overwrite an existing output")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Fatal("original export changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := filepath.Join(filepath.Dir(path), "cancelled.glb")
	if WriteFile(ctx, dest, []byte("data")) == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cancel created output")
	}
}
func TestCrossOriginRequestsRejected(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/demo", nil)
	req.Header.Set("Origin", "https://malicious.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin request must be rejected")
	}
}
