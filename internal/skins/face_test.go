package skins

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestFaceCropsFrontAndCompositesHat(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	base := color.NRGBA{160, 80, 30, 255}
	for y := 8; y < 16; y++ {
		for x := 8; x < 16; x++ {
			im.SetNRGBA(x, y, base)
		}
	}
	im.SetNRGBA(40, 8, color.NRGBA{20, 40, 240, 255})
	var raw bytes.Buffer
	png.Encode(&raw, im)
	data, err := FacePNG(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	face, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if face.Bounds().Dx() != 8 || face.Bounds().Dy() != 8 {
		t.Fatal("wrong face size")
	}
	if color.NRGBAModel.Convert(face.At(0, 0)) != (color.NRGBA{20, 40, 240, 255}) || color.NRGBAModel.Convert(face.At(7, 7)) != base {
		t.Fatal("wrong skin crop or overlay")
	}
	if _, err := FacePNG([]byte("invalid")); err == nil {
		t.Fatal("accepted invalid skin")
	}
}
