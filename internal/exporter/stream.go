package exporter

import (
	"astra-mwe/internal/scene"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// Stream keeps geometry on disk. Its resident metadata grows with materials and
// tile primitives, never with the number of vertices in the exported world.
type Stream struct {
	file                        *os.File
	size                        int
	origin                      scene.Vec3
	views                       []bufferView
	accessors                   []accessor
	meshes                      []mesh
	materials, images, textures []map[string]any
	imageIDs, materialIDs       map[string]int
	warnings                    []string
}

func NewStream(dir string, origin scene.Vec3) (*Stream, error) {
	f, err := os.CreateTemp(dir, "astra-geometry-*.bin")
	if err != nil {
		return nil, err
	}
	return &Stream{file: f, origin: origin, imageIDs: map[string]int{}, materialIDs: map[string]int{}, meshes: []mesh{{Name: "World", Primitives: []primitive{}}}}, nil
}

func (s *Stream) Close() {
	if s.file != nil {
		name := s.file.Name()
		s.file.Close()
		os.Remove(name)
		s.file = nil
	}
}
func (s *Stream) MaterialCount() int     { return len(s.materials) }
func (s *Stream) SetWarnings(w []string) { s.warnings = append([]string(nil), w...) }

type streamDocument struct {
	BufferViews []bufferView     `json:"bufferViews"`
	Accessors   []accessor       `json:"accessors"`
	Meshes      []mesh           `json:"meshes"`
	Materials   []map[string]any `json:"materials"`
	Images      []struct {
		Name       string `json:"name"`
		BufferView int    `json:"bufferView"`
	} `json:"images"`
	Textures []struct {
		Source int `json:"source"`
	} `json:"textures"`
}

// Add serializes one bounded tile. World tiles share a single glTF mesh/node;
// player meshes retain their individual names. Positions use the global origin.
func (s *Stream) Add(ctx context.Context, sc *scene.Scene, world bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for mi := range sc.Meshes {
		for pi := range sc.Meshes[mi].Primitives {
			p := &sc.Meshes[mi].Primitives[pi]
			for i := range p.Positions {
				p.Positions[i] += float32(sc.Origin[i%3] - s.origin[i%3])
			}
		}
	}
	data, err := GLB(sc)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	jl := int(binary.LittleEndian.Uint32(data[12:]))
	var doc streamDocument
	if err = json.Unmarshal(data[20:20+jl], &doc); err != nil {
		return err
	}
	var bin []byte
	if len(data) > 20+jl {
		bin = data[28+jl:]
	}
	viewIDs := map[int]int{}
	copyView := func(i int) (int, error) {
		if id, ok := viewIDs[i]; ok {
			return id, nil
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		v := doc.BufferViews[i]
		pad := (4 - s.size%4) % 4
		if uint64(s.size)+uint64(pad)+uint64(v.ByteLength) > math.MaxUint32 {
			return 0, errors.New("scene exceeds GLB 4 GiB format limit")
		}
		if pad > 0 {
			if _, err := s.file.Write(make([]byte, pad)); err != nil {
				return 0, err
			}
			s.size += pad
		}
		if _, err := s.file.Write(bin[v.ByteOffset : v.ByteOffset+v.ByteLength]); err != nil {
			return 0, err
		}
		id := len(s.views)
		v.ByteOffset = s.size
		s.size += v.ByteLength
		s.views = append(s.views, v)
		viewIDs[i] = id
		return id, nil
	}
	textureIDs := make([]int, len(doc.Textures))
	for ti, t := range doc.Textures {
		im := doc.Images[t.Source]
		v := doc.BufferViews[im.BufferView]
		key := fmt.Sprintf("%s:%x", im.Name, sha256.Sum256(bin[v.ByteOffset:v.ByteOffset+v.ByteLength]))
		id, ok := s.imageIDs[key]
		if !ok {
			vi, err := copyView(im.BufferView)
			if err != nil {
				return err
			}
			id = len(s.textures)
			s.images = append(s.images, map[string]any{"name": im.Name, "bufferView": vi, "mimeType": "image/png"})
			s.textures = append(s.textures, map[string]any{"source": len(s.images) - 1, "sampler": 0})
			s.imageIDs[key] = id
		}
		textureIDs[ti] = id
	}
	materialIDs := make([]int, len(doc.Materials))
	for i, m := range doc.Materials {
		if pbr, ok := m["pbrMetallicRoughness"].(map[string]any); ok {
			if tx, ok := pbr["baseColorTexture"].(map[string]any); ok {
				tx["index"] = textureIDs[int(tx["index"].(float64))]
			}
		}
		if tx, ok := m["emissiveTexture"].(map[string]any); ok {
			tx["index"] = textureIDs[int(tx["index"].(float64))]
		}
		key, _ := json.Marshal(m)
		id, ok := s.materialIDs[string(key)]
		if !ok {
			id = len(s.materials)
			s.materials = append(s.materials, m)
			s.materialIDs[string(key)] = id
		}
		materialIDs[i] = id
	}
	accessorBase := len(s.accessors)
	for _, a := range doc.Accessors {
		vi, err := copyView(a.BufferView)
		if err != nil {
			return err
		}
		a.BufferView = vi
		s.accessors = append(s.accessors, a)
	}
	for _, m := range doc.Meshes {
		for i := range m.Primitives {
			p := &m.Primitives[i]
			p.Material = materialIDs[p.Material]
			p.Indices += accessorBase
			for k, v := range p.Attributes {
				p.Attributes[k] = v + accessorBase
			}
		}
		if world {
			s.meshes[0].Primitives = append(s.meshes[0].Primitives, m.Primitives...)
		} else {
			s.meshes = append(s.meshes, m)
		}
	}
	return nil
}

// Finish creates an immutable GLB snapshot without copying geometry into RAM.
// It may be called before and after adding players for independent exports.
func (s *Stream) Finish(ctx context.Context, dir string) (path string, err error) {
	if err = ctx.Err(); err != nil {
		return "", err
	}
	nodes := []map[string]any{}
	nodeIDs := []int{}
	meshes := []mesh{}
	for _, m := range s.meshes {
		if len(m.Primitives) > 0 {
			nodes = append(nodes, map[string]any{"name": m.Name, "mesh": len(meshes)})
			nodeIDs = append(nodeIDs, len(nodeIDs))
			meshes = append(meshes, m)
		}
	}
	binSize := (s.size + 3) &^ 3
	doc := map[string]any{"asset": map[string]any{"version": "2.0", "generator": "Astra MWE"}, "scene": 0, "scenes": []any{map[string]any{"nodes": nodeIDs}}, "extras": map[string]any{"minecraftOrigin": s.origin, "warnings": s.warnings}}
	if len(nodes) > 0 {
		doc["nodes"] = nodes
		doc["meshes"] = meshes
	}
	if len(s.materials) > 0 {
		doc["materials"] = s.materials
	}
	if binSize > 0 {
		doc["buffers"] = []any{map[string]any{"byteLength": binSize}}
		doc["bufferViews"] = s.views
	}
	if len(s.accessors) > 0 {
		doc["accessors"] = s.accessors
	}
	if len(s.images) > 0 {
		doc["images"] = s.images
		doc["textures"] = s.textures
		doc["samplers"] = []any{map[string]any{"magFilter": 9728, "minFilter": 9728, "wrapS": 10497, "wrapT": 10497}}
	}
	j, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	for len(j)%4 != 0 {
		j = append(j, ' ')
	}
	total := uint64(20 + len(j))
	if binSize > 0 {
		total += uint64(8 + binSize)
	}
	if total > math.MaxUint32 {
		return "", errors.New("scene exceeds GLB 4 GiB format limit")
	}
	f, err := os.CreateTemp(dir, "astra-build-*.glb")
	if err != nil {
		return "", err
	}
	path = f.Name()
	createdPath := path
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(createdPath)
		}
	}()
	header := make([]byte, 20)
	copy(header, "glTF")
	binary.LittleEndian.PutUint32(header[4:], 2)
	binary.LittleEndian.PutUint32(header[8:], uint32(total))
	binary.LittleEndian.PutUint32(header[12:], uint32(len(j)))
	copy(header[16:], "JSON")
	if _, err = f.Write(header); err != nil {
		return "", err
	}
	if _, err = f.Write(j); err != nil {
		return "", err
	}
	if binSize > 0 {
		header = make([]byte, 8)
		binary.LittleEndian.PutUint32(header, uint32(binSize))
		copy(header[4:], "BIN\x00")
		if _, err = f.Write(header); err != nil {
			return "", err
		}
		reader := io.NewSectionReader(s.file, 0, int64(s.size))
		buf := make([]byte, 1<<20)
		for {
			if err = ctx.Err(); err != nil {
				return "", err
			}
			var n int
			n, err = reader.Read(buf)
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				return "", err
			}
			if _, err = f.Write(buf[:n]); err != nil {
				return "", err
			}
		}
		if pad := binSize - s.size; pad > 0 {
			if _, err = f.Write(make([]byte, pad)); err != nil {
				return "", err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	return path, nil
}
