package service

import (
	"astra-mwe/internal/scene"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func meshNames(t *testing.T, data []byte) []string {
	t.Helper()
	var doc struct{ Meshes []struct{ Name string } }
	if len(data) < 20 {
		t.Fatal("missing GLB")
	}
	if err := json.Unmarshal(data[20:20+binary.LittleEndian.Uint32(data[12:16])], &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range doc.Meshes {
		out = append(out, m.Name)
	}
	return out
}
func TestPlayersStayVisibleWhenExcludedFromExport(t *testing.T) {
	for _, include := range []bool{false, true} {
		s := New(t.TempDir())
		defer s.Close()
		if _, err := s.Demo(); err != nil {
			t.Fatal(err)
		}
		req := BuildRequest{Dimension: "minecraft:overworld", Bounds: scene.Bounds{MinX: -4, MaxX: 4, MinY: 0, MaxY: 127, MinZ: -4, MaxZ: 4}, AutoMin: true, Padding: 4, Players: include}
		if err := s.StartBuild(req); err != nil {
			t.Fatal(err)
		}
		<-s.done
		if s.Status().Error != "" {
			t.Fatal(s.Status().Error)
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/preview.glb", nil))
		if got := meshNames(t, response.Body.Bytes()); len(got) != 2 {
			t.Fatalf("player absent from preview (export=%v): %v", include, got)
		}
		file := filepath.Join(t.TempDir(), "world.glb")
		if err := s.Export(file); err != nil {
			t.Fatal(err)
		}
		<-s.done
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		count := 1
		if include {
			count = 2
		}
		if got := meshNames(t, data); len(got) != count {
			t.Fatalf("wrong export objects: %v", got)
		}
	}
}
