package mesher

import (
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestSurfaceMapUsesTopTextureAndBiomeWithoutMesh(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/test/blockstates/plant.json":  `{"variants":{"":{"model":"test:block/plant"}}}`,
		"assets/test/models/block/plant.json": `{"textures":{"all":"test:block/stone"},"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#all","tintindex":0}}}]}`,
		"data/test/worldgen/biome/green.json": `{"effects":{"grass_color":"#00ff00"}}`,
	})
	v := vol(map[[3]int]scene.Block{{0, 2, 0}: {Name: "test:plant"}, {0, 1, 0}: {Name: "test:stone"}})
	v.Biomes = map[[3]int]string{}
	for x := -8; x <= 8; x += 4 {
		for z := -8; z <= 8; z += 4 {
			v.Biomes[[3]int{x, 0, z}] = "test:green"
		}
	}
	painter := NewSurfacePainter(a)
	if _, err := painter.b.resolver.resolve(scene.Block{Name: "test:plant"}, [3]int{}); err != nil {
		t.Fatal(err)
	}
	img, err := painter.Paint(context.Background(), v, scene.Bounds{MaxX: 1, MaxZ: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	c := img.NRGBAAt(0, 0)
	if c.R != 0 || c.G < 178 || c.B != 0 || c.A != 255 {
		t.Fatal(c)
	}
	if img.NRGBAAt(1, 0).A != 0 {
		t.Fatal("missing column is not transparent")
	}
	if len(painter.b.scene.Meshes) != 0 || painter.b.volume != nil {
		t.Fatal("map created mesh or retained full volume")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = painter.Paint(ctx, v, scene.Bounds{}, 1); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestDetailedSurfaceTextureUVRotationAndTransparency(t *testing.T) {
	tex := image.NewNRGBA(image.Rect(0, 0, 16, 32))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 8 {
				c = color.NRGBA{B: 255, A: 255}
			}
			if x >= 8 && y >= 8 {
				c = color.NRGBA{}
			}
			tex.SetNRGBA(x, y, c)
			tex.SetNRGBA(x, y+16, color.NRGBA{G: 255, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, tex); err != nil {
		t.Fatal(err)
	}
	a := fixture(t, map[string]string{
		"assets/test/textures/block/check.png":  data.String(),
		"assets/test/blockstates/check.json":    `{"variants":{"":{"model":"test:block/check"}}}`,
		"assets/test/models/block/check.json":   `{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"test:block/check","uv":[0,0,16,16]}}}]}`,
		"assets/test/blockstates/rotated.json":  `{"variants":{"":{"model":"test:block/rotated"}}}`,
		"assets/test/models/block/rotated.json": `{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"test:block/check","uv":[0,0,16,16],"rotation":90}}}]}`,
	})
	p := NewSurfacePainter(a)
	v := vol(map[[3]int]scene.Block{{0, 1, 0}: {Name: "test:check"}, {0, 0, 0}: {Name: "test:stone"}})
	img, err := p.PaintDetailed(context.Background(), v, scene.Bounds{}, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 16 || img.NRGBAAt(1, 1) != (color.NRGBA{R: 255, A: 255}) || img.NRGBAAt(14, 1) != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("texture or first animation frame lost: %v %v", img.NRGBAAt(1, 1), img.NRGBAAt(14, 1))
	}
	if c := img.NRGBAAt(14, 14); c.R != 180 || c.G != 180 || c.B != 180 || c.A != 255 {
		t.Fatalf("transparent texel failed to reveal lower block: %v", c)
	}
	v.Blocks[[3]int{0, 1, 0}] = scene.Block{Name: "test:rotated"}
	rotated, err := p.PaintDetailed(context.Background(), v, scene.Bounds{}, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.NRGBAAt(14, 1).R != 255 || rotated.NRGBAAt(14, 14).B != 255 {
		t.Fatalf("face UV rotation lost: %v %v", rotated.NRGBAAt(14, 1), rotated.NRGBAAt(14, 14))
	}
}
func TestDetailedSurfaceHonorsElementFootprintAndLimits(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/test/blockstates/half.json":  `{"variants":{"":{"model":"test:block/half"}}}`,
		"assets/test/models/block/half.json": `{"elements":[{"from":[0,0,0],"to":[8,16,16],"faces":{"up":{"texture":"test:block/stone"}}}]}`,
	})
	p := NewSurfacePainter(a)
	v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:half"}})
	img, err := p.PaintDetailed(context.Background(), v, scene.Bounds{}, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if img.NRGBAAt(0, 0).A != 255 || img.NRGBAAt(3, 0).A != 0 {
		t.Fatal("element footprint was stretched over whole block")
	}
	for _, pair := range [][2]int{{1, 3}, {2, 4}, {1, 32}} {
		if _, err := p.PaintDetailed(context.Background(), v, scene.Bounds{}, pair[0], pair[1]); err == nil {
			t.Fatalf("accepted invalid detail %v", pair)
		}
	}
	if _, err := p.PaintDetailed(context.Background(), v, scene.Bounds{MaxX: 64, MaxZ: 64}, 1, 16); err == nil {
		t.Fatal("pixel budget ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.PaintDetailed(ctx, v, scene.Bounds{}, 1, 16); err == nil {
		t.Fatal("cancellation ignored")
	}
}
