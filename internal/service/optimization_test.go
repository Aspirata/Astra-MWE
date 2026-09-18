package service

import (
	"astra-mwe/internal/scene"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestOptimizationChangesExportAndReportsWorldCounts(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	if _, err := s.Demo(); err != nil {
		t.Fatal(err)
	}
	req := BuildRequest{Dimension: "minecraft:overworld", Bounds: scene.Bounds{MinX: -8, MaxX: 8, MinY: 0, MaxY: 127, MinZ: -8, MaxZ: 8}, AutoMin: true, Padding: 4}
	var counts []int
	for _, opt := range []bool{false, true} {
		req.OptimizeMesh = opt
		if err := s.StartBuild(req); err != nil {
			t.Fatal(err)
		}
		<-s.done
		st := s.Status()
		if st.Error != "" {
			t.Fatal(st.Error)
		}
		if st.Preview.Optimized != opt || st.Preview.WorldTrianglesAfter != st.Preview.Triangles {
			t.Fatal("wrong reported world counts")
		}
		var doc struct {
			Meshes    []struct{ Primitives []struct{ Indices int } }
			Accessors []struct{ Count int }
		}
		data := s.data
		if err := json.Unmarshal(data[20:20+binary.LittleEndian.Uint32(data[12:16])], &doc); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, m := range doc.Meshes {
			for _, p := range m.Primitives {
				count += doc.Accessors[p.Indices].Count / 3
			}
		}
		if count != st.Preview.Triangles {
			t.Fatal("export and reported count differ")
		}
		counts = append(counts, count)
	}
	if counts[1] >= counts[0] {
		t.Fatalf("checkbox did not reduce export: %v", counts)
	}
}
