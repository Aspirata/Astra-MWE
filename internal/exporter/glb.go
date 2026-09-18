// Package exporter serializes indexed, textured meshes as self-contained glTF 2.0 binary files.
package exporter

import (
	"astra-mwe/internal/scene"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}
type accessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float32 `json:"min,omitempty"`
	Max           []float32 `json:"max,omitempty"`
}
type primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   int            `json:"material"`
	Mode       int            `json:"mode"`
}
type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
}

func GLB(s *scene.Scene) ([]byte, error) {
	return glbWithLimit(s, 512<<20)
}

func glbWithLimit(s *scene.Scene, maxBytes uint64) ([]byte, error) {
	if s == nil {
		return nil, errors.New("nil scene")
	}
	// Count binary payloads before copying any textures or geometry. Include
	// alignment for each view; shared image bytes are embedded per material.
	var estimated uint64
	add := func(n uint64) bool {
		if n > maxBytes || estimated > maxBytes-n {
			return false
		}
		estimated += n
		return true
	}
	for _, m := range s.Materials {
		if len(m.EmissivePNG) > 0 && !add((uint64(len(m.EmissivePNG))+3)&^3) {
			return nil, errors.New("scene exceeds GLB memory budget")
		}
		if len(m.PNG) > 0 && !add((uint64(len(m.PNG))+3)&^3) {
			return nil, errors.New("scene exceeds GLB memory budget; reduce the selected region")
		}
	}
	for _, m := range s.Meshes {
		for _, p := range m.Primitives {
			for _, n := range []int{len(p.Positions), len(p.Normals), len(p.UVs), len(p.Colors), len(p.Indices)} {
				if !add(uint64(n) * 4) {
					return nil, errors.New("scene exceeds GLB memory budget; reduce the selected region")
				}
			}
		}
	}
	var bin []byte
	views := []bufferView{}
	accessors := []accessor{}
	meshes := []mesh{}
	nodes := []map[string]any{}
	nodeIDs := []int{}
	materials := []map[string]any{}
	images := []map[string]any{}
	textures := []map[string]any{}
	textureIDs := map[string]int{}
	appendView := func(data []byte, target int) int {
		for len(bin)%4 != 0 {
			bin = append(bin, 0)
		}
		i := len(views)
		views = append(views, bufferView{Buffer: 0, ByteOffset: len(bin), ByteLength: len(data), Target: target})
		bin = append(bin, data...)
		return i
	}
	floats := func(values []float32, components int, typ string, bounds bool) int {
		data := make([]byte, len(values)*4)
		for i, v := range values {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
		}
		a := accessor{BufferView: appendView(data, 34962), ComponentType: 5126, Count: len(values) / components, Type: typ}
		if bounds {
			a.Min = append([]float32(nil), values[:components]...)
			a.Max = append([]float32(nil), a.Min...)
			for i, v := range values {
				c := i % components
				if v < a.Min[c] {
					a.Min[c] = v
				}
				if v > a.Max[c] {
					a.Max[c] = v
				}
			}
		}
		i := len(accessors)
		accessors = append(accessors, a)
		return i
	}
	for _, m := range s.Materials {
		pbr := map[string]any{"metallicFactor": 0, "roughnessFactor": 1}
		alpha := m.Alpha
		if alpha == "" {
			alpha = "OPAQUE"
		}
		if alpha != "OPAQUE" && alpha != "MASK" && alpha != "BLEND" {
			return nil, fmt.Errorf("invalid alpha mode for %q", m.Name)
		}
		mat := map[string]any{"name": m.Name, "pbrMetallicRoughness": pbr, "alphaMode": alpha, "doubleSided": m.DoubleSided, "extras": map[string]any{"astraMWE": true, "surfaceRenderMethod": "DITHERED"}}
		if alpha == "MASK" {
			mat["alphaCutoff"] = 0.5
		}
		if len(m.PNG) > 0 {
			if len(m.PNG) < 8 || string(m.PNG[:8]) != "\x89PNG\r\n\x1a\n" {
				return nil, fmt.Errorf("invalid PNG for %q", m.Name)
			}
			name := m.TextureName
			if name == "" {
				name = m.Name + ".png"
			}
			key := fmt.Sprintf("%s:%x", name, sha256.Sum256(m.PNG))
			ti, exists := textureIDs[key]
			if !exists {
				iv := appendView(m.PNG, 0)
				ii := len(images)
				images = append(images, map[string]any{"name": name, "bufferView": iv, "mimeType": "image/png"})
				ti = len(textures)
				textures = append(textures, map[string]any{"sampler": 0, "source": ii})
				textureIDs[key] = ti
			}
			pbr["baseColorTexture"] = map[string]any{"index": ti}
		}
		if len(m.EmissivePNG) > 0 {
			if len(m.EmissivePNG) < 8 || string(m.EmissivePNG[:8]) != "\x89PNG\r\n\x1a\n" {
				return nil, fmt.Errorf("invalid emissive PNG for %q", m.Name)
			}
			name := m.EmissiveName
			if name == "" {
				name = m.Name + "_emissive.png"
			}
			key := fmt.Sprintf("%s:%x", name, sha256.Sum256(m.EmissivePNG))
			ti, ok := textureIDs[key]
			if !ok {
				iv := appendView(m.EmissivePNG, 0)
				ii := len(images)
				images = append(images, map[string]any{"name": name, "bufferView": iv, "mimeType": "image/png"})
				ti = len(textures)
				textures = append(textures, map[string]any{"sampler": 0, "source": ii})
				textureIDs[key] = ti
			}
			mat["emissiveTexture"] = map[string]any{"index": ti}
			mat["emissiveFactor"] = []float32{1, 1, 1}
		}
		materials = append(materials, mat)
	}
	for _, m := range s.Meshes {
		gm := mesh{Name: m.Name, Primitives: []primitive{}}
		for _, p := range m.Primitives {
			if len(p.Positions) == 0 && len(p.Indices) == 0 {
				continue
			}
			n := len(p.Positions) / 3
			if n == 0 || len(p.Positions)%3 != 0 || len(p.Indices) == 0 || len(p.Indices)%3 != 0 || p.Material < 0 || p.Material >= len(s.Materials) {
				return nil, fmt.Errorf("invalid geometry in %q", m.Name)
			}
			for _, a := range []struct {
				v    []float32
				size int
			}{{p.Positions, 3}, {p.Normals, 3}, {p.UVs, 2}, {p.Colors, 4}} {
				if len(a.v) > 0 && len(a.v) != n*a.size {
					return nil, fmt.Errorf("attribute length mismatch in %q", m.Name)
				}
				for _, v := range a.v {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						return nil, fmt.Errorf("non-finite attribute in %q", m.Name)
					}
				}
			}
			gp := primitive{Attributes: map[string]int{"POSITION": floats(p.Positions, 3, "VEC3", true)}, Material: p.Material, Mode: 4}
			if len(p.Normals) > 0 {
				gp.Attributes["NORMAL"] = floats(p.Normals, 3, "VEC3", false)
			}
			if len(p.UVs) > 0 {
				gp.Attributes["TEXCOORD_0"] = floats(p.UVs, 2, "VEC2", false)
			}
			if len(p.Colors) > 0 {
				gp.Attributes["COLOR_0"] = floats(p.Colors, 4, "VEC4", false)
			}
			data := make([]byte, len(p.Indices)*4)
			for i, v := range p.Indices {
				if uint64(v) >= uint64(n) {
					return nil, fmt.Errorf("index out of range in %q", m.Name)
				}
				binary.LittleEndian.PutUint32(data[i*4:], v)
			}
			gp.Indices = len(accessors)
			accessors = append(accessors, accessor{BufferView: appendView(data, 34963), ComponentType: 5125, Count: len(p.Indices), Type: "SCALAR"})
			gm.Primitives = append(gm.Primitives, gp)
		}
		if len(gm.Primitives) > 0 {
			mi := len(meshes)
			meshes = append(meshes, gm)
			nodeIDs = append(nodeIDs, len(nodes))
			nodes = append(nodes, map[string]any{"name": m.Name, "mesh": mi})
		}
	}
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	doc := map[string]any{"asset": map[string]any{"version": "2.0", "generator": "Astra MWE"}, "scene": 0, "scenes": []any{map[string]any{"nodes": nodeIDs}}, "extras": map[string]any{"minecraftOrigin": s.Origin, "warnings": s.Warnings}}
	if len(nodes) > 0 {
		doc["nodes"] = nodes
		doc["meshes"] = meshes
	}
	if len(materials) > 0 {
		doc["materials"] = materials
	}
	if len(bin) > 0 {
		doc["buffers"] = []any{map[string]any{"byteLength": len(bin)}}
		doc["bufferViews"] = views
	}
	if len(accessors) > 0 {
		doc["accessors"] = accessors
	}
	if len(images) > 0 {
		doc["images"] = images
		doc["textures"] = textures
		doc["samplers"] = []any{map[string]any{"magFilter": 9728, "minFilter": 9728, "wrapS": 10497, "wrapT": 10497}}
	}
	j, e := json.Marshal(doc)
	if e != nil {
		return nil, e
	}
	for len(j)%4 != 0 {
		j = append(j, ' ')
	}
	total := uint64(12 + 8 + len(j))
	if len(bin) > 0 {
		total += uint64(8 + len(bin))
	}
	if total > math.MaxUint32 {
		return nil, errors.New("scene exceeds GLB 4 GiB limit; reduce the selected region")
	}
	if total > maxBytes {
		return nil, errors.New("scene exceeds GLB memory budget; reduce the selected region")
	}
	out := make([]byte, int(total))
	copy(out, "glTF")
	binary.LittleEndian.PutUint32(out[4:], 2)
	binary.LittleEndian.PutUint32(out[8:], uint32(total))
	binary.LittleEndian.PutUint32(out[12:], uint32(len(j)))
	copy(out[16:], "JSON")
	copy(out[20:], j)
	if len(bin) > 0 {
		o := 20 + len(j)
		binary.LittleEndian.PutUint32(out[o:], uint32(len(bin)))
		copy(out[o+4:], "BIN\x00")
		copy(out[o+8:], bin)
	}
	return out, nil
}
