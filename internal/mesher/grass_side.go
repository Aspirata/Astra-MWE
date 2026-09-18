package mesher

import (
	"astra-mwe/internal/scene"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Fold the tinted overlay into the opaque side PNG. A second coincident
// surface causes z-fighting and unnecessary alpha rendering in Blender.
func (b *builder) grassSide(inst instance, pos [3]int, tid, dir string, q [4]point, uv [4][2]float64) (int, bool) {
	if textureName(tid) != "grass_block_side.png" {
		return 0, false
	}
	overlay := ""
	for _, r := range inst.models {
		for _, el := range r.model.Elements {
			f, ok := el.Faces[dir]
			if !ok {
				continue
			}
			id, _, err := textureID(r.model, f.Texture)
			if err != nil || textureName(id) != "grass_block_side_overlay.png" {
				continue
			}
			other := corners(el, dir)
			for i := range other {
				other[i] = transform(other[i], el, r.transform)
			}
			if other == q && faceUV(el, dir, f, r.transform) == uv {
				overlay = id
			}
		}
	}
	if overlay == "" {
		return 0, false
	}
	tint := b.tint(inst.block, pos, true)
	key := fmt.Sprintf("%s / %s / baked %s %08x %08x %08x", inst.block.Name, tid, overlay, math.Float32bits(tint[0]), math.Float32bits(tint[1]), math.Float32bits(tint[2]))
	if id, ok := b.materials[key]; ok {
		return id, true
	}
	baseTexture, overTexture := b.getTexture(tid, ""), b.getTexture(overlay, "")
	base, err := png.Decode(bytes.NewReader(baseTexture.png))
	if err != nil {
		return 0, false
	}
	over, err := png.Decode(bytes.NewReader(overTexture.png))
	if err != nil {
		return 0, false
	}
	w, h := max(base.Bounds().Dx(), over.Bounds().Dx()), max(base.Bounds().Dy(), over.Bounds().Dy())
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha := "OPAQUE"
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bc := color.NRGBAModel.Convert(base.At(x*base.Bounds().Dx()/w, y*base.Bounds().Dy()/h)).(color.NRGBA)
			oc := color.NRGBAModel.Convert(over.At(x*over.Bounds().Dx()/w, y*over.Bounds().Dy()/h)).(color.NRGBA)
			pixel := composeGrassPixel(bc, oc, tint)
			if pixel.A > 0 && pixel.A < 255 {
				alpha = "BLEND"
			} else if pixel.A == 0 && alpha != "BLEND" {
				alpha = "MASK"
			}
			out.SetNRGBA(x, y, pixel)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, out); err != nil {
		return 0, false
	}
	id := len(b.scene.Materials)
	b.scene.Materials = append(b.scene.Materials, scene.Material{Name: inst.block.Name, TextureName: textureName(tid), PNG: data.Bytes(), Alpha: alpha})
	b.materials[key] = id
	return id, true
}

func composeGrassPixel(bc, oc color.NRGBA, tint [4]float32) color.NRGBA {
	a, ba := float64(oc.A)/255, float64(bc.A)/255
	outAlpha := a + ba*(1-a)
	if outAlpha == 0 {
		return color.NRGBA{}
	}
	src, dst := [3]uint8{oc.R, oc.G, oc.B}, [3]uint8{bc.R, bc.G, bc.B}
	var c [3]uint8
	for i := range c {
		c[i] = uint8(math.Round((float64(src[i])*float64(srgbComponent(tint[i]))*a + float64(dst[i])*ba*(1-a)) / outAlpha))
	}
	return color.NRGBA{c[0], c[1], c[2], uint8(math.Round(outAlpha * 255))}
}
