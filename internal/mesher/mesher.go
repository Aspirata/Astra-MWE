// Package mesher resolves Java blockstates and JSON block models into textured chunk meshes.
package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"path"
	"sort"
	"strings"
)

type texture struct {
	png   []byte
	alpha string
}
type tintKey struct {
	pos  [3]int
	kind string
}
type instance struct {
	block      scene.Block
	models     []resolved
	opaque     bool
	fluid      string
	fullCube   bool
	snowHeight float64
}
type builder struct {
	assets    *assets.Stack
	volume    *scene.Volume
	opts      scene.MeshOptions
	resolver  resolver
	scene     *scene.Scene
	warnings  map[string]bool
	textures  map[string]texture
	materials map[string]int
	instances map[[3]int]*instance
	biomes    map[string]biome
	colormaps map[string]image.Image
	tints     map[tintKey][3]float32
	leaves    map[string]bool
}

func Build(ctx context.Context, v *scene.Volume, a *assets.Stack, opts scene.MeshOptions) (*scene.Scene, error) {
	return buildWithLimit(ctx, v, a, opts, 1_000_000)
}

func buildWithLimit(ctx context.Context, v *scene.Volume, a *assets.Stack, opts scene.MeshOptions, maxFaces int) (*scene.Scene, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if v == nil {
		return nil, fmt.Errorf("nil block volume")
	}
	if v.Bounds.MinX > v.Bounds.MaxX || v.Bounds.MinY > v.Bounds.MaxY || v.Bounds.MinZ > v.Bounds.MaxZ {
		return nil, fmt.Errorf("invalid volume bounds")
	}
	b := &builder{assets: a, volume: v, opts: opts, scene: &scene.Scene{Origin: scene.Vec3{float64(v.Bounds.MinX), float64(v.Bounds.MinY), float64(v.Bounds.MinZ)}}, warnings: map[string]bool{}, textures: map[string]texture{}, materials: map[string]int{}, biomes: map[string]biome{}, colormaps: map[string]image.Image{}, tints: map[tintKey][3]float32{}}
	b.resolver = resolver{assets: a, models: map[string]modelResult{}, states: map[string]stateResult{}}
	b.leaves = b.loadLeaves()
	positions := make([][3]int, 0, len(v.Blocks))
	for p, block := range v.Blocks {
		// A one-block horizontal halo occludes seams between independently viewed tiles.
		if !air(block.Name) && p[0] >= v.Bounds.MinX-1 && p[0] <= v.Bounds.MaxX+1 && p[1] >= v.Bounds.MinY && p[1] <= v.Bounds.MaxY && p[2] >= v.Bounds.MinZ-1 && p[2] <= v.Bounds.MaxZ+1 {
			positions = append(positions, p)
		}
	}
	sort.Slice(positions, func(i, j int) bool {
		for _, axis := range []int{0, 2, 1} {
			if positions[i][axis] != positions[j][axis] {
				return positions[i][axis] < positions[j][axis]
			}
		}
		return false
	})
	// Most terrain blocks share one resolved model. Its coverage and texture
	// alpha are immutable for this build, including blocks hidden underground.
	type coverageKey struct {
		name  string
		model *model
		x     int
	}
	type coverage struct {
		full, opaque bool
		snow         float64
	}
	coverages := map[coverageKey]coverage{}
	// Most positions share one immutable blockstate/variant. Store a pointer
	// instead of duplicating its model list and block properties underground.
	type instanceKey struct {
		plan        *resolutionPlan
		model       *model
		transform   variant
		name, level string
	}
	shared := map[instanceKey]*instance{}
	b.instances = make(map[[3]int]*instance, len(positions))
	for i, p := range positions {
		if i%256 == 0 {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
		}
		block := v.Blocks[p]
		if !strings.Contains(block.Name, ":") {
			block.Name = "minecraft:" + block.Name
		}
		inst := instance{block: block}
		var key instanceKey
		share := false
		if block.Name == "minecraft:water" || block.Name == "minecraft:bubble_column" || block.Name == "minecraft:lava" {
			key = instanceKey{name: block.Name, level: block.Properties["level"]}
			share = true
			if cached := shared[key]; cached != nil {
				b.instances[p] = cached
				continue
			}
			inst.fluid = "water"
			if block.Name == "minecraft:lava" {
				inst.fluid = "lava"
			}
			inst.models = []resolved{{model: fluidModel(inst.fluid, block)}}
		} else {
			models, e := b.resolver.resolve(block, p)
			if e != nil {
				b.warn(block.Name + ": " + e.Error() + "; using missing-model cube")
				models = []resolved{{model: cubeModel("astra:missing", false)}}
			}
			inst.models = models
			if len(models) == 1 {
				if plan, err := b.resolver.plan(block); err == nil {
					key = instanceKey{plan: plan, model: models[0].model, transform: models[0].transform}
					share = true
					if cached := shared[key]; cached != nil {
						b.instances[p] = cached
						continue
					}
				}
				key := coverageKey{block.Name, models[0].model, models[0].transform.X}
				c, ok := coverages[key]
				if !ok {
					c = coverage{fullCube(inst), b.occluder(inst), b.snowHeight(inst)}
					coverages[key] = c
				}
				inst.fullCube, inst.opaque, inst.snowHeight = c.full, c.opaque, c.snow
			} else {
				inst.fullCube = fullCube(inst)
				inst.opaque = b.occluder(inst)
				inst.snowHeight = b.snowHeight(inst)
			}
		}
		saved := new(instance)
		*saved = inst
		b.instances[p] = saved
		if share {
			shared[key] = saved
		}
	}
	primitives := map[int]*scene.Primitive{}
	faces := 0
	for i, p := range positions {
		if p[0] < v.Bounds.MinX || p[0] > v.Bounds.MaxX || p[2] < v.Bounds.MinZ || p[2] > v.Bounds.MaxZ {
			continue
		}
		if i%128 == 0 {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
		}
		inst := b.instanceAt(p)
		var fluid fluidSurface
		if inst.fluid != "" {
			fluid = b.fluidSurface(p, inst.fluid)
		}
		for _, r := range inst.models {
			for _, el := range r.model.Elements {
				for _, dir := range directions {
					f, ok := el.Faces[dir]
					if !ok {
						continue
					}
					if b.cull(p, inst, rotatedDirection(f.Cullface, r.transform)) {
						continue
					}
					tid, translucent, e := textureID(r.model, f.Texture)
					if inst.fluid != "" {
						tid = fluid.texture(inst.fluid, dir)
					}
					if e != nil {
						b.warn(inst.block.Name + ": " + e.Error())
						tid = "astra:missing"
					}
					if excludedTexture(tid) {
						continue
					}
					q := corners(el, dir)
					if inst.fluid != "" {
						q = fluid.corners(dir)
					}
					for j := range q {
						q[j] = transform(q[j], el, r.transform)
					}
					n := normal(q)
					if n == (point{}) {
						continue
					}
					twoSided, skip := b.flatFace(p, inst, r, el, dir, f, tid, translucent)
					if skip {
						continue
					}
					uv := faceUV(el, dir, f, r.transform)
					if inst.fluid != "" {
						uv = fluid.uv(dir, q)
					}
					rgba := b.tint(inst.block, p, tintedFace(inst.block, f.TintIndex) || inst.fluid == "water")
					mat, baked := b.grassSide(inst, p, tid, dir, q, uv)
					if !baked {
						var skipEye bool
						mat, baked, skipEye = b.eyeblossom(inst, tid, dir, q, uv)
						if skipEye {
							continue
						}
					}
					if !baked {
						mat = b.material(inst.block.Name, tid, inst.fluid, translucent)
					} else {
						rgba = [4]float32{1, 1, 1, 1}
					}
					if twoSided {
						b.scene.Materials[mat].DoubleSided = true
					}
					prim := primitives[mat]
					if prim == nil {
						prim = &scene.Primitive{Material: mat}
						primitives[mat] = prim
					}
					if faces >= maxFaces {
						return nil, fmt.Errorf("scene exceeds %d face budget; select a smaller region", maxFaces)
					}
					faces++
					base := uint32(len(prim.Positions) / 3)
					for j, point := range q {
						for axis := 0; axis < 3; axis++ {
							prim.Positions = append(prim.Positions, float32(point[axis]+float64(p[axis])-b.scene.Origin[axis]))
							prim.Normals = append(prim.Normals, float32(n[axis]))
						}
						prim.UVs = append(prim.UVs, float32(uv[j][0]), float32(uv[j][1]))
						prim.Colors = append(prim.Colors, rgba[:]...)
					}
					prim.Indices = append(prim.Indices, base, base+1, base+2, base, base+2, base+3)
				}
			}
		}
	}
	ids := make([]int, 0, len(primitives))
	for id := range primitives {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	m := scene.Mesh{Name: "Minecraft World"}
	for _, id := range ids {
		if len(primitives[id].Indices) > 0 {
			m.Primitives = append(m.Primitives, *primitives[id])
		}
	}
	if len(m.Primitives) > 0 {
		b.scene.Meshes = append(b.scene.Meshes, m)
	}
	sort.Strings(b.scene.Warnings)
	return b.scene, nil
}
func floorDiv(v, d int) int {
	q := v / d
	if v%d < 0 {
		q--
	}
	return q
}
func air(name string) bool {
	_, n := splitID(name)
	return n == "" || n == "air" || n == "cave_air" || n == "void_air" || n == "structure_void" || n == "light" || n == "barrier"
}
func (b *builder) warn(s string) {
	if !b.warnings[s] {
		b.warnings[s] = true
		b.scene.Warnings = append(b.scene.Warnings, s)
	}
}
func leaf(name string) bool { return strings.HasSuffix(name, "_leaves") }
func transparent(name string) bool {
	return leaf(name) || strings.Contains(name, "glass") || strings.HasSuffix(name, ":ice") || strings.HasSuffix(name, ":frosted_ice") || strings.HasSuffix(name, ":slime_block") || strings.HasSuffix(name, ":honey_block")
}
func fullCube(inst instance) bool {
	for _, r := range inst.models {
		for _, e := range r.model.Elements {
			if e.Rotation == nil && e.From == [3]float64{0, 0, 0} && e.To == [3]float64{16, 16, 16} && len(e.Faces) == 6 {
				return true
			}
		}
	}
	return false
}
func (b *builder) occluder(inst instance) bool {
	if transparent(inst.block.Name) || b.leaf(inst.block.Name) {
		return false
	}
	for _, r := range inst.models {
		for _, e := range r.model.Elements {
			if e.Rotation != nil || e.From != [3]float64{0, 0, 0} || e.To != [3]float64{16, 16, 16} || len(e.Faces) != 6 {
				continue
			}
			opaque := true
			for _, f := range e.Faces {
				tid, translucent, err := textureID(r.model, f.Texture)
				if err != nil || excludedTexture(tid) || translucent || b.getTexture(tid, "").alpha != "OPAQUE" {
					opaque = false
					break
				}
			}
			if opaque {
				return true
			}
		}
	}
	return false
}
func (b *builder) cull(p [3]int, current instance, dir string) bool {
	off, ok := offsets[dir]
	if !ok {
		return false
	}
	entry, ok := b.instances[[3]int{p[0] + off[0], p[1] + off[1], p[2] + off[2]}]
	if !ok {
		return false
	}
	neighbor := *entry
	if neighbor.opaque {
		return true
	}
	if dir != "up" && dir != "down" && current.snowHeight > 0 && neighbor.snowHeight >= current.snowHeight {
		return true
	}
	if current.fluid != "" && neighbor.fluid == current.fluid {
		// Neighboring fluid surfaces meet at shared corner heights.
		return true
	}
	if b.leaf(current.block.Name) {
		return b.opts.HollowLeaves && b.leaf(neighbor.block.Name) && current.fullCube && neighbor.fullCube
	}
	return transparent(current.block.Name) && current.block.Name == neighbor.block.Name && current.fullCube && neighbor.fullCube
}

// Adjacent snow layers hide each other's fully covered side faces. Validate
// the actual model and texture so resource packs with holes stay intact.
func (b *builder) snowHeight(inst instance) float64 {
	if inst.block.Name != "minecraft:snow" || len(inst.models) != 1 {
		return 0
	}
	r := inst.models[0]
	if r.transform.X != 0 || len(r.model.Elements) != 1 {
		return 0
	}
	el := r.model.Elements[0]
	if el.Rotation != nil || el.From != ([3]float64{}) || el.To[0] != 16 || el.To[2] != 16 || el.To[1] <= 0 || el.To[1] > 16 || len(el.Faces) != 6 {
		return 0
	}
	for _, f := range el.Faces {
		id, tr, err := textureID(r.model, f.Texture)
		if err != nil || tr || b.getTexture(id, "").alpha != "OPAQUE" {
			return 0
		}
	}
	return el.To[1] / 16
}
func cubeModel(tid string, tinted bool) *model {
	faces := map[string]face{}
	for _, d := range directions {
		f := face{Texture: tid, Cullface: d}
		if tinted {
			zero := 0
			f.TintIndex = &zero
		}
		faces[d] = f
	}
	return &model{Elements: []element{{To: [3]float64{16, 16, 16}, Faces: faces}}}
}
func (b *builder) getTexture(id, fluid string) texture {
	key := id + "|" + fluid
	if t, ok := b.textures[key]; ok {
		return t
	}
	data, e := b.assets.Read(resource(id, "textures", ".png"))
	var im image.Image
	if e == nil {
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || uint64(cfg.Width)*uint64(cfg.Height) > 64<<20 {
			e = fmt.Errorf("invalid or oversized PNG")
		} else {
			im, e = png.Decode(bytes.NewReader(data))
		}
	}
	if e != nil {
		if id != "astra:missing" {
			b.warn("Missing or invalid texture " + id + ": using fallback")
		}
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				c := color.NRGBA{255, 0, 255, 255}
				if (x/8+y/8)%2 != 0 {
					c = color.NRGBA{25, 25, 25, 255}
				}
				if fluid == "water" {
					c = color.NRGBA{185, 210, 255, 180}
				}
				if fluid == "lava" {
					c = color.NRGBA{255, 100, 12, 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
		im = img
		data = nil
	}
	// Static export takes the first frame listed in the animation metadata.
	if im != nil {
		rect := im.Bounds()
		var meta struct {
			Animation *struct {
				Width  int               `json:"width"`
				Height int               `json:"height"`
				Frames []json.RawMessage `json:"frames"`
			} `json:"animation"`
		}
		raw, _ := b.assets.Read(resource(id, "textures", ".png.mcmeta"))
		if json.Unmarshal(raw, &meta) == nil && meta.Animation != nil {
			w, h := meta.Animation.Width, meta.Animation.Height
			if w == 0 && h == 0 {
				w = min(rect.Dx(), rect.Dy())
				h = w
			} else if w == 0 {
				w = rect.Dx()
			} else if h == 0 {
				h = rect.Dy()
			}
			idx := 0
			if len(meta.Animation.Frames) > 0 {
				if json.Unmarshal(meta.Animation.Frames[0], &idx) != nil {
					var frame struct {
						Index int `json:"index"`
					}
					json.Unmarshal(meta.Animation.Frames[0], &frame)
					idx = frame.Index
				}
			}
			if w > 0 && h > 0 && w <= rect.Dx() && h <= rect.Dy() {
				cols := rect.Dx() / w
				total := cols * (rect.Dy() / h)
				if idx < 0 || idx >= total {
					idx = 0
				}
				frame := image.NewNRGBA(image.Rect(0, 0, w, h))
				draw.Draw(frame, frame.Bounds(), im, image.Pt(rect.Min.X+(idx%cols)*w, rect.Min.Y+(idx/cols)*h), draw.Src)
				im = frame
				data = nil
			}
		}
	}
	alpha := "OPAQUE"
	rect := im.Bounds()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			_, _, _, a := im.At(x, y).RGBA()
			if a > 0 && a < 65535 {
				alpha = "BLEND"
				break
			}
			if a == 0 {
				alpha = "MASK"
			}
		}
		if alpha == "BLEND" {
			break
		}
	}
	if data == nil {
		var encoded bytes.Buffer
		png.Encode(&encoded, im)
		data = encoded.Bytes()
	}
	t := texture{data, alpha}
	b.textures[key] = t
	return t
}
func (b *builder) material(block, tid, fluid string, translucent bool) int {
	key := block + " / " + tid
	if translucent {
		key += " / translucent"
	}
	if id, ok := b.materials[key]; ok {
		return id
	}
	t := b.getTexture(tid, fluid)
	alpha := t.alpha
	if fluid == "water" {
		alpha = "BLEND"
	}
	if b.leaf(block) {
		alpha = "MASK"
	}
	if transparent(block) && !b.leaf(block) {
		alpha = "BLEND"
	}
	if translucent {
		alpha = "BLEND"
	}
	name, _, _ := strings.Cut(key, "/")
	m := scene.Material{Name: strings.TrimSpace(name), TextureName: textureName(tid), PNG: t.png, Alpha: alpha}
	id := len(b.scene.Materials)
	b.scene.Materials = append(b.scene.Materials, m)
	b.materials[key] = id
	return id
}

func excludedTexture(id string) bool {
	_, name := splitID(id)
	base := strings.TrimSuffix(path.Base(name), ".png")
	return base == "glass_block_side_overlay" || base == "grass_block_side_overlay"
}

func textureName(id string) string {
	_, name := splitID(id)
	return strings.TrimSuffix(path.Base(name), ".png") + ".png"
}

// Java cross models encode both sides of each zero-thickness element. Export
// one quad and use a two-sided material; leave distinct back-face artwork intact.
func (b *builder) flatFace(p [3]int, inst instance, r resolved, el element, dir string, f face, tid string, translucent bool) (twoSided, skip bool) {
	for _, pair := range []struct {
		axis          int
		first, second string
	}{{0, "west", "east"}, {1, "down", "up"}, {2, "north", "south"}} {
		if el.From[pair.axis] != el.To[pair.axis] || (dir != pair.first && dir != pair.second) {
			continue
		}
		otherDir := pair.first
		if dir == pair.first {
			otherDir = pair.second
		}
		other, ok := el.Faces[otherDir]
		if !ok || b.cull(p, inst, rotatedDirection(other.Cullface, r.transform)) {
			return false, false
		}
		otherID, otherTranslucent, err := textureID(r.model, other.Texture)
		if err != nil || otherID != tid || otherTranslucent != translucent || (f.TintIndex != nil) != (other.TintIndex != nil) {
			return false, false
		}
		uv, otherUV := faceUV(el, dir, f, r.transform), faceUV(el, otherDir, other, r.transform)
		if uv != otherUV {
			// Horizontal plants mirror the bottom UV rectangle to compensate
			// for opposite winding. Compare UVs at the same spatial corner.
			q, otherQ := corners(el, dir), corners(el, otherDir)
			for i, p := range q {
				match := false
				for j, op := range otherQ {
					if p == op && uv[i] == otherUV[j] {
						match = true
						break
					}
				}
				if !match {
					return false, false
				}
			}
		}
		if pair.axis == 1 {
			return true, dir == pair.first
		}
		return true, dir == pair.second
	}
	return false, false
}
