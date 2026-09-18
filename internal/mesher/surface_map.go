package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"strings"
)

// SurfacePainter makes a map from surface columns, without constructing mesh
// vertices or export materials. Resource/model averages persist between tiles.
// Each preview worker owns a painter and serializes its Paint calls.
type SurfacePainter struct {
	b        *builder
	textures map[string][4]float32
	layers   map[string][]mapLayer
	detailed map[string][]mapDetailLayer
	images   map[string]image.Image
}
type mapLayer struct {
	rgba   [4]float32
	tinted bool
}

func NewSurfacePainter(a *assets.Stack) *SurfacePainter {
	b := &builder{assets: a, opts: scene.MeshOptions{BiomeColors: true, BiomeBlend: 7}, scene: &scene.Scene{}, warnings: map[string]bool{}, biomes: map[string]biome{}, colormaps: map[string]image.Image{}, tints: map[tintKey][3]float32{}}
	b.resolver = resolver{assets: a, models: map[string]modelResult{}, states: map[string]stateResult{}}
	b.leaves = b.loadLeaves()
	return &SurfacePainter{b: b, textures: map[string][4]float32{}, layers: map[string][]mapLayer{}, detailed: map[string][]mapDetailLayer{}, images: map[string]image.Image{}}
}

func (p *SurfacePainter) average(id string) [4]float32 {
	if c, ok := p.textures[id]; ok {
		return c
	}
	c := [4]float32{1, 0, 1, 1}
	data, err := p.b.assets.Read(resource(id, "textures", ".png"))
	if err == nil {
		if img, e := png.Decode(bytes.NewReader(data)); e == nil {
			rect := img.Bounds()
			rect.Max.Y = min(rect.Max.Y, rect.Min.Y+rect.Dx()) // first animation frame
			stride := max(1, int(math.Sqrt(float64(rect.Dx()*rect.Dy())/4096)))
			sum := [4]float32{}
			n := float32(0)
			for y := rect.Min.Y; y < rect.Max.Y; y += stride {
				for x := rect.Min.X; x < rect.Max.X; x += stride {
					q := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
					a := float32(q.A) / 255
					sum[0] += float32(q.R) / 255 * a
					sum[1] += float32(q.G) / 255 * a
					sum[2] += float32(q.B) / 255 * a
					sum[3] += a
					n++
				}
			}
			if sum[3] > 0 {
				c = [4]float32{sum[0] / sum[3], sum[1] / sum[3], sum[2] / sum[3], sum[3] / n}
			} else {
				c = [4]float32{}
			}
		}
	}
	p.textures[id] = c
	return c
}

func mapStateKey(block scene.Block) string {
	keys := make([]string, 0, len(block.Properties))
	for k := range block.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(block.Name)
	for _, k := range keys {
		fmt.Fprintf(&b, "|%s=%s", k, block.Properties[k])
	}
	return b.String()
}
func (p *SurfacePainter) blockLayers(block scene.Block) []mapLayer {
	key := mapStateKey(block)
	if layers, ok := p.layers[key]; ok {
		return layers
	}
	var layers []mapLayer
	if block.Name == "minecraft:water" || block.Name == "minecraft:bubble_column" || block.Name == "minecraft:lava" {
		kind := "water"
		if block.Name == "minecraft:lava" {
			kind = "lava"
		}
		rgba := p.average("minecraft:block/" + kind + "_still")
		if kind == "water" {
			rgba[3] = min(rgba[3], .72)
		}
		layers = []mapLayer{{rgba, kind == "water"}}
	} else if models, err := p.b.resolver.resolve(block, [3]int{}); err == nil {
		for _, r := range models {
			var up, other []mapLayer
			for _, el := range r.model.Elements {
				for _, dir := range directions {
					f, ok := el.Faces[dir]
					if !ok {
						continue
					}
					id, _, err := textureID(r.model, f.Texture)
					if err != nil || excludedTexture(id) || textureName(id) == "grass_block_side_overlay.png" {
						continue
					}
					layer := mapLayer{p.average(id), tintedFace(block, f.TintIndex)}
					if rotatedDirection(dir, r.transform) == "up" {
						up = append(up, layer)
					} else if dir != "down" {
						other = append(other, layer)
					}
				}
			}
			if len(up) > 0 {
				layers = append(layers, up...)
			} else {
				layers = append(layers, other...)
			}
		}
	}
	if len(layers) == 0 {
		layers = []mapLayer{{[4]float32{1, 0, 1, 1}, false}}
	}
	p.layers[key] = layers
	return layers
}

func (p *SurfacePainter) blockColor(block scene.Block, pos [3]int) [4]float32 {
	layers := p.blockLayers(block)
	out := [4]float32{}
	for _, layer := range layers {
		tint := p.b.tint(block, pos, layer.tinted)
		a := layer.rgba[3]
		for i := 0; i < 3; i++ {
			out[i] += layer.rgba[i] * srgbComponent(tint[i]) * a
		}
		out[3] += a
	}
	if out[3] > 0 {
		for i := 0; i < 3; i++ {
			out[i] /= out[3]
		}
	}
	out[3] /= float32(len(layers))
	return out
}

// Paint returns one pixel per step blocks inside bounds; v may contain a halo.
// At close zoom pixels are individual blocks. Zooming out samples the surface.
func (p *SurfacePainter) Paint(ctx context.Context, v *scene.Volume, bounds scene.Bounds, step int) (*image.NRGBA, error) {
	return p.PaintDetailed(ctx, v, bounds, step, 1)
}

// PaintDetailed preserves texture texels when a block occupies multiple screen pixels.
func (p *SurfacePainter) PaintDetailed(ctx context.Context, v *scene.Volume, bounds scene.Bounds, step, detail int) (*image.NRGBA, error) {
	if detail == 0 {
		detail = 1
	}
	if detail != 1 && detail != 2 && detail != 4 && detail != 8 && detail != 16 {
		return nil, fmt.Errorf("invalid map detail")
	}
	if step != 1 && detail != 1 {
		return nil, fmt.Errorf("map detail requires step 1")
	}
	if step < 1 || step > 16 || bounds.MinX > bounds.MaxX || bounds.MinZ > bounds.MaxZ {
		return nil, fmt.Errorf("invalid map bounds or step")
	}
	w, h := (bounds.MaxX-bounds.MinX)/step+1, (bounds.MaxZ-bounds.MinZ)/step+1
	if w > 512 || h > 512 || w*detail > 1024 || h*detail > 1024 {
		return nil, fmt.Errorf("map exceeds pixel budget")
	}
	p.b.volume = v
	p.b.tints = map[tintKey][3]float32{}
	p.b.warnings = map[string]bool{}
	p.b.scene.Warnings = nil
	defer func() { p.b.volume = nil; p.b.tints = nil }()
	columns := make(map[[2]int][]int)
	for pos, block := range v.Blocks {
		if air(block.Name) {
			continue
		}
		if pos[0] >= bounds.MinX && pos[0] <= bounds.MaxX && pos[2] >= bounds.MinZ && pos[2] <= bounds.MaxZ && (pos[0]-bounds.MinX)%step == 0 && (pos[2]-bounds.MinZ)%step == 0 {
			k := [2]int{pos[0], pos[2]}
			columns[k] = append(columns[k], pos[1])
		}
	}
	for k, ys := range columns {
		sort.Sort(sort.Reverse(sort.IntSlice(ys)))
		if len(ys) > 8 {
			ys = ys[:8]
		}
		columns[k] = ys
	}
	img := image.NewNRGBA(image.Rect(0, 0, w*detail, h*detail))
	// Reuse two small pixel buffers across the whole tile. Stop at opaque
	// coverage instead of shading eight invisible underground blocks.
	samples := make([][4]float32, detail*detail)
	blockSamples := make([][4]float32, detail*detail)
	for z := 0; z < h; z++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			wx, wz := bounds.MinX+x*step, bounds.MinZ+z*step
			ys := columns[[2]int{wx, wz}]
			if len(ys) == 0 {
				continue
			}
			if detail > 1 {
				clear(samples)
				for _, y := range ys {
					pos := [3]int{wx, y, wz}
					p.blockDetailInto(v.Blocks[pos], pos, detail, blockSamples)
					covered := true
					for i, c := range blockSamples {
						if samples[i][3] > .995 {
							continue
						}
						a := (1 - samples[i][3]) * c[3]
						for j := 0; j < 3; j++ {
							samples[i][j] += c[j] * a
						}
						samples[i][3] += a
						if samples[i][3] <= .995 {
							covered = false
						}
					}
					if covered {
						break
					}
				}
			}
			for dz := 0; dz < detail; dz++ {
				for dx := 0; dx < detail; dx++ {
					out := [4]float32{}
					if detail > 1 {
						out = samples[dz*detail+dx]
					} else {
						for _, y := range ys {
							pos := [3]int{wx, y, wz}
							c := p.blockColor(v.Blocks[pos], pos)
							a := (1 - out[3]) * c[3]
							for i := 0; i < 3; i++ {
								out[i] += c[i] * a
							}
							out[3] += a
							if out[3] > .995 {
								break
							}
						}
					}
					shade := float32(1)
					if other := columns[[2]int{wx, wz - step}]; len(other) > 0 {
						delta := max(-4, min(4, ys[0]-other[0]))
						shade += float32(delta) * .035
					}
					if out[3] > 0 {
						var c color.NRGBA
						c.A = uint8(out[3] * 255)
						rgb := []*uint8{&c.R, &c.G, &c.B}
						for i := range rgb {
							*rgb[i] = uint8(max(0, min(255, out[i]/out[3]*shade*255)))
						}
						img.SetNRGBA(x*detail+dx, z*detail+dz, c)
					}
				}
			}
		}
	}
	return img, nil
}

type mapDetailLayer struct {
	pixels [][4]float32
	tinted bool
}
type mapDetailFace struct {
	q       [4]point
	uv      [4][2]float64
	texture image.Image
	tinted  bool
	alpha   float32
	height  float64
}

func (p *SurfacePainter) mapTexture(id string) image.Image {
	if img, ok := p.images[id]; ok {
		return img
	}
	var img image.Image
	if data, err := p.b.assets.Read(resource(id, "textures", ".png")); err == nil {
		img, _ = png.Decode(bytes.NewReader(data))
	}
	p.images[id] = img
	return img
}
func mapSample(img image.Image, u, v float64) [4]float32 {
	if img == nil {
		return [4]float32{1, 0, 1, 1}
	}
	r := img.Bounds()
	h := min(r.Dy(), r.Dx())
	x := r.Min.X + max(0, min(r.Dx()-1, int(math.Floor(u*float64(r.Dx())))))
	y := r.Min.Y + max(0, min(h-1, int(math.Floor(v*float64(h)))))
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, float32(c.A) / 255}
}
func (p *SurfacePainter) detailLayers(block scene.Block, detail int) []mapDetailLayer {
	key := fmt.Sprintf("%s:%d", mapStateKey(block), detail)
	if layers, ok := p.detailed[key]; ok {
		return layers
	}
	var faces []mapDetailFace
	if block.Name == "minecraft:water" || block.Name == "minecraft:bubble_column" || block.Name == "minecraft:lava" {
		kind := "water"
		alpha := float32(.72)
		if block.Name == "minecraft:lava" {
			kind = "lava"
			alpha = 1
		}
		el := element{To: [3]float64{16, 16, 16}}
		faces = append(faces, mapDetailFace{q: corners(el, "up"), uv: faceUV(el, "up", face{}, variant{}), texture: p.mapTexture("minecraft:block/" + kind + "_still"), tinted: kind == "water", alpha: alpha})
	} else if models, err := p.b.resolver.resolve(block, [3]int{}); err == nil {
		for _, r := range models {
			for _, el := range r.model.Elements {
				for _, dir := range directions {
					f, ok := el.Faces[dir]
					if !ok {
						continue
					}
					q := corners(el, dir)
					for i := range q {
						q[i] = transform(q[i], el, r.transform)
					}
					if normal(q)[1] < .001 {
						continue
					}
					id, _, err := textureID(r.model, f.Texture)
					if err != nil || excludedTexture(id) {
						continue
					}
					height := 0.0
					for _, v := range q {
						height += v[1] / 4
					}
					faces = append(faces, mapDetailFace{q: q, uv: faceUV(el, dir, f, r.transform), texture: p.mapTexture(id), tinted: tintedFace(block, f.TintIndex), alpha: 1, height: height})
				}
			}
		}
	}
	sort.SliceStable(faces, func(i, j int) bool { return faces[i].height > faces[j].height })
	var layers []mapDetailLayer
	for _, f := range faces {
		layer := mapDetailLayer{pixels: make([][4]float32, detail*detail), tinted: f.tinted}
		// Project the transformed quad onto X/Z. Its two edges give an affine UV basis.
		ax, az := f.q[1][0]-f.q[0][0], f.q[1][2]-f.q[0][2]
		bx, bz := f.q[3][0]-f.q[0][0], f.q[3][2]-f.q[0][2]
		det := ax*bz - az*bx
		if math.Abs(det) < 1e-9 {
			continue
		}
		for z := 0; z < detail; z++ {
			for x := 0; x < detail; x++ {
				px, pz := (float64(x)+.5)/float64(detail)-f.q[0][0], (float64(z)+.5)/float64(detail)-f.q[0][2]
				a, b := (px*bz-pz*bx)/det, (ax*pz-az*px)/det
				if a < 0 || a > 1 || b < 0 || b > 1 {
					continue
				}
				u := f.uv[0][0] + a*(f.uv[1][0]-f.uv[0][0]) + b*(f.uv[3][0]-f.uv[0][0])
				v := f.uv[0][1] + a*(f.uv[1][1]-f.uv[0][1]) + b*(f.uv[3][1]-f.uv[0][1])
				c := mapSample(f.texture, u, v)
				c[3] = min(c[3], f.alpha)
				layer.pixels[z*detail+x] = c
			}
		}
		layers = append(layers, layer)
	}
	if len(layers) == 0 {
		for _, l := range p.blockLayers(block) {
			pixels := make([][4]float32, detail*detail)
			for i := range pixels {
				pixels[i] = l.rgba
			}
			layers = append(layers, mapDetailLayer{pixels, l.tinted})
		}
	}
	p.detailed[key] = layers
	return layers
}
func (p *SurfacePainter) blockDetail(block scene.Block, pos [3]int, detail int) [][4]float32 {
	out := make([][4]float32, detail*detail)
	p.blockDetailInto(block, pos, detail, out)
	return out
}

func (p *SurfacePainter) blockDetailInto(block scene.Block, pos [3]int, detail int, out [][4]float32) {
	clear(out)
	for _, layer := range p.detailLayers(block, detail) {
		tint := p.b.tint(block, pos, layer.tinted)
		for i := range tint {
			tint[i] = srgbComponent(tint[i])
		}
		for i, c := range layer.pixels {
			a := (1 - out[i][3]) * c[3]
			for j := 0; j < 3; j++ {
				out[i][j] += c[j] * tint[j] * a
			}
			out[i][3] += a
		}
	}
	for i := range out {
		if out[i][3] > 0 {
			for j := 0; j < 3; j++ {
				out[i][j] /= out[i][3]
			}
		}
	}
}
