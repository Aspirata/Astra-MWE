package meshopt

import (
	"astra-mwe/internal/scene"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestUserGLBSurfaceAreaPreserved(t *testing.T) {
	if os.Getenv("MESHOPT_GLB_BENCH") == "" {
		t.Skip("opt-in local GLB regression")
	}
	areas := func(sc *scene.Scene) map[string]float64 {
		out := map[string]float64{}
		for _, mesh := range sc.Meshes {
			for _, p := range mesh.Primitives {
				for i := 0; i < len(p.Indices); i += 3 {
					a, b, c := read(&p, p.Indices[i]), read(&p, p.Indices[i+1]), read(&p, p.Indices[i+2])
					u := [3]float64{}
					v := [3]float64{}
					for j := 0; j < 3; j++ {
						u[j] = float64(b.p[j] - a.p[j])
						v[j] = float64(c.p[j] - a.p[j])
					}
					cross := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
					area := math.Sqrt(cross[0]*cross[0]+cross[1]*cross[1]+cross[2]*cross[2]) / 2
					normal := [3]int{int(math.Round(float64(a.n[0]) * 10000)), int(math.Round(float64(a.n[1]) * 10000)), int(math.Round(float64(a.n[2]) * 10000))}
					key := fmt.Sprintf("%s/%d/%v/%v", mesh.Name, p.Material, normal, a.c)
					out[key] += area
				}
			}
		}
		return out
	}
	for _, name := range []string{"asdwqe.glb", "asdwqe_optimized.glb"} {
		t.Run(name, func(t *testing.T) {
			sc := readBenchmarkGLB(t, filepath.Join("..", "..", name))
			before := areas(sc)
			if err := Optimize(context.Background(), sc); err != nil {
				t.Fatal(err)
			}
			after := areas(sc)
			if len(before) != len(after) {
				t.Fatal("surface groups changed")
			}
			for key, area := range before {
				if math.Abs(area-after[key]) > 1e-5 {
					t.Fatalf("surface changed %s: %f -> %f", key, area, after[key])
				}
			}
		})
	}
}

// Opt in with MESHOPT_GLB_BENCH=1; the user's files remain read-only and are
// intentionally not prerequisites for portable tests.
func BenchmarkOptimizeUserGLB(b *testing.B) {
	if os.Getenv("MESHOPT_GLB_BENCH") == "" {
		b.Skip("set MESHOPT_GLB_BENCH=1 to benchmark local exports")
	}
	for _, name := range []string{"asdwqe.glb", "asdwqe_optimized.glb"} {
		b.Run(name, func(b *testing.B) {
			base := readBenchmarkGLB(b, filepath.Join("..", "..", name))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sc := &scene.Scene{Materials: base.Materials, Meshes: make([]scene.Mesh, len(base.Meshes))}
				for j, m := range base.Meshes {
					sc.Meshes[j] = scene.Mesh{Name: m.Name, Primitives: append([]scene.Primitive(nil), m.Primitives...)}
				}
				if err := Optimize(context.Background(), sc); err != nil {
					b.Fatal(err)
				}
				triangles := 0
				for _, m := range sc.Meshes {
					for _, p := range m.Primitives {
						triangles += len(p.Indices) / 3
					}
				}
				b.ReportMetric(float64(triangles), "triangles")
			}
		})
	}
}

func readBenchmarkGLB(t testing.TB, path string) *scene.Scene {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 20 || binary.LittleEndian.Uint32(data[:4]) != 0x46546c67 {
		t.Fatal("invalid GLB")
	}
	endJSON := 20 + int(binary.LittleEndian.Uint32(data[12:16]))
	var doc struct {
		BufferViews []struct{ ByteOffset, ByteLength, ByteStride int }
		Accessors   []struct {
			BufferView, ByteOffset, ComponentType, Count int
			Type                                         string
		}
		Meshes []struct {
			Name       string
			Primitives []struct {
				Attributes        map[string]int
				Indices, Material int
			}
		}
	}
	if err := json.Unmarshal(data[20:endJSON], &doc); err != nil {
		t.Fatal(err)
	}
	bin := data[endJSON+8:]
	floats := func(ai int) []float32 {
		a := doc.Accessors[ai]
		v := doc.BufferViews[a.BufferView]
		if a.ComponentType != 5126 {
			t.Fatal("benchmark requires float attributes")
		}
		n := map[string]int{"VEC2": 2, "VEC3": 3, "VEC4": 4}[a.Type]
		stride := v.ByteStride
		if stride == 0 {
			stride = n * 4
		}
		out := make([]float32, a.Count*n)
		for i := 0; i < a.Count; i++ {
			for j := 0; j < n; j++ {
				off := v.ByteOffset + a.ByteOffset + i*stride + j*4
				out[i*n+j] = math.Float32frombits(binary.LittleEndian.Uint32(bin[off:]))
			}
		}
		return out
	}
	sc := &scene.Scene{}
	for _, m := range doc.Meshes {
		mesh := scene.Mesh{Name: m.Name}
		for _, src := range m.Primitives {
			p := scene.Primitive{Material: src.Material, Positions: floats(src.Attributes["POSITION"])}
			if a, ok := src.Attributes["NORMAL"]; ok {
				p.Normals = floats(a)
			}
			if a, ok := src.Attributes["TEXCOORD_0"]; ok {
				p.UVs = floats(a)
			}
			if a, ok := src.Attributes["COLOR_0"]; ok {
				p.Colors = floats(a)
			}
			a := doc.Accessors[src.Indices]
			v := doc.BufferViews[a.BufferView]
			for i := 0; i < a.Count; i++ {
				off := v.ByteOffset + a.ByteOffset
				var idx uint32
				switch a.ComponentType {
				case 5125:
					idx = binary.LittleEndian.Uint32(bin[off+i*4:])
				case 5123:
					idx = uint32(binary.LittleEndian.Uint16(bin[off+i*2:]))
				default:
					t.Fatal("unsupported index format")
				}
				p.Indices = append(p.Indices, idx)
			}
			mesh.Primitives = append(mesh.Primitives, p)
		}
		sc.Meshes = append(sc.Meshes, mesh)
	}
	return sc
}
