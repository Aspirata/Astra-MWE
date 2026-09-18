package exporter

import (
	"astra-mwe/internal/scene"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

func triangle() *scene.Scene {
	return &scene.Scene{Materials: []scene.Material{{Name: "test:stone", Alpha: "OPAQUE"}}, Meshes: []scene.Mesh{{Name: "chunk", Primitives: []scene.Primitive{{Material: 0, Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Normals: []float32{0, 0, 1, 0, 0, 1, 0, 0, 1}, UVs: []float32{0, 0, 1, 0, 0, 1}, Colors: []float32{1, 0, 0, 1, 0, 1, 0, 1, 0, 0, 1, 1}, Indices: []uint32{0, 1, 2}}}}}}
}
func TestGLBStructureAndRGBA(t *testing.T) {
	b, e := GLB(triangle())
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:4]) != "glTF" || binary.LittleEndian.Uint32(b[4:8]) != 2 || int(binary.LittleEndian.Uint32(b[8:12])) != len(b) {
		t.Fatal("invalid GLB header")
	}
	n := int(binary.LittleEndian.Uint32(b[12:16]))
	if n%4 != 0 || string(b[16:20]) != "JSON" {
		t.Fatal("invalid JSON chunk")
	}
	var doc map[string]any
	if e = json.Unmarshal(b[20:20+n], &doc); e != nil {
		t.Fatal(e)
	}
	accessors := doc["accessors"].([]any)
	p := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)[0].(map[string]any)
	ci := int(p["attributes"].(map[string]any)["COLOR_0"].(float64))
	if accessors[ci].(map[string]any)["type"] != "VEC4" {
		t.Fatal("RGBA requires VEC4")
	}
	if string(b[24+n:28+n]) != "BIN\x00" {
		t.Fatal("missing binary chunk")
	}
}
func TestRejectMalformedGeometry(t *testing.T) {
	for _, mutate := range []func(*scene.Scene){func(s *scene.Scene) { s.Meshes[0].Primitives[0].Indices[2] = 99 }, func(s *scene.Scene) { s.Meshes[0].Primitives[0].Positions[0] = float32(math.NaN()) }, func(s *scene.Scene) { s.Meshes[0].Primitives[0].Colors = []float32{1} }, func(s *scene.Scene) { s.Meshes[0].Primitives[0].Material = 8 }} {
		s := triangle()
		mutate(s)
		if _, e := GLB(s); e == nil {
			t.Fatal("invalid mesh accepted")
		}
	}
}
