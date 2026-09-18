package mesher

import (
	"astra-mwe/internal/scene"
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// The emissive Java cross occupies the exact same plane as the ordinary cross.
// Composite its color and attach its emission map to that single surface.
func (b *builder) eyeblossom(inst instance, tid, dir string, q [4]point, uv [4][2]float64) (int, bool, bool) {
	name := textureName(tid)
	isEye := name == "open_eyeblossom_emissive.png"
	if name != "open_eyeblossom.png" && !isEye {
		return 0, false, false
	}
	wanted := "open_eyeblossom_emissive.png"
	if isEye {
		wanted = "open_eyeblossom.png"
	}
	counterpart := ""
	for _, r := range inst.models {
		for _, el := range r.model.Elements {
			f, ok := el.Faces[dir]
			if !ok {
				continue
			}
			id, _, err := textureID(r.model, f.Texture)
			if err != nil || textureName(id) != wanted {
				continue
			}
			other := corners(el, dir)
			for i := range other {
				other[i] = transform(other[i], el, r.transform)
			}
			if other == q && faceUV(el, dir, f, r.transform) == uv {
				counterpart = id
			}
		}
	}
	if counterpart == "" {
		return 0, false, false
	}
	baseID, eyeID := tid, counterpart
	if isEye {
		baseID, eyeID = counterpart, tid
	}
	key := inst.block.Name + " / baked eye / " + baseID + " / " + eyeID
	if id, ok := b.materials[key]; ok {
		return id, true, isEye
	}
	base, err := png.Decode(bytes.NewReader(b.getTexture(baseID, "").png))
	if err != nil {
		return 0, false, false
	}
	eye, err := png.Decode(bytes.NewReader(b.getTexture(eyeID, "").png))
	if err != nil {
		return 0, false, false
	}
	w, h := max(base.Bounds().Dx(), eye.Bounds().Dx()), max(base.Bounds().Dy(), eye.Bounds().Dy())
	out, emission := image.NewNRGBA(image.Rect(0, 0, w, h)), image.NewNRGBA(image.Rect(0, 0, w, h))
	alpha := "OPAQUE"
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bc := color.NRGBAModel.Convert(base.At(x*base.Bounds().Dx()/w, y*base.Bounds().Dy()/h)).(color.NRGBA)
			ec := color.NRGBAModel.Convert(eye.At(x*eye.Bounds().Dx()/w, y*eye.Bounds().Dy()/h)).(color.NRGBA)
			c := composeGrassPixel(bc, ec, [4]float32{1, 1, 1, 1})
			out.SetNRGBA(x, y, c)
			// glTF emission ignores alpha; transparent texels must emit black.
			emission.SetNRGBA(x, y, color.NRGBA{uint8(uint16(ec.R) * uint16(ec.A) / 255), uint8(uint16(ec.G) * uint16(ec.A) / 255), uint8(uint16(ec.B) * uint16(ec.A) / 255), 255})
			if c.A > 0 && c.A < 255 {
				alpha = "BLEND"
			} else if c.A == 0 && alpha != "BLEND" {
				alpha = "MASK"
			}
		}
	}
	var pngColor, pngEmission bytes.Buffer
	if png.Encode(&pngColor, out) != nil || png.Encode(&pngEmission, emission) != nil {
		return 0, false, false
	}
	id := len(b.scene.Materials)
	b.scene.Materials = append(b.scene.Materials, scene.Material{Name: inst.block.Name, TextureName: textureName(baseID), PNG: pngColor.Bytes(), EmissiveName: textureName(eyeID), EmissivePNG: pngEmission.Bytes(), Alpha: alpha, DoubleSided: true})
	b.materials[key] = id
	return id, true, isEye
}
