package skins

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
)

// FacePNG uses the front of the head and its hat layer, including legacy skins.
func FacePNG(data []byte) ([]byte, error) {
	normalized, err := normalize(data)
	if err != nil {
		return nil, err
	}
	im, err := png.Decode(bytes.NewReader(normalized))
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(out, out.Bounds(), im, image.Pt(8, 8), draw.Src)
	draw.Draw(out, out.Bounds(), im, image.Pt(40, 8), draw.Over)
	var buf bytes.Buffer
	err = png.Encode(&buf, out)
	return buf.Bytes(), err
}

// CachedFace avoids a second network request for map icons while terrain loads.
func (c *Client) CachedFace(uuid string) []byte {
	data, err := os.ReadFile(c.cachePath(uuid) + ".png")
	if err != nil {
		return nil
	}
	face, err := FacePNG(data)
	if err != nil {
		return nil
	}
	return face
}
