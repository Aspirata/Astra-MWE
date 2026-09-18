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

func TestLeafLitterUsesDryFoliageOverride(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/minecraft/blockstates/leaf_litter.json": `{"variants":{"":{"model":"test:block/litter"}}}`,
		"assets/test/models/block/litter.json":          `{"elements":[{"from":[0,0,0],"to":[16,0,16],"faces":{"up":{"texture":"test:block/stone","tintindex":0}}}]}`,
		"data/minecraft/worldgen/biome/plains.json":     `{"temperature":0.8,"downfall":0.4,"effects":{"grass_color":65280,"dry_foliage_color":"#b76a37"}}`,
	})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:leaf_litter"}}), a, scene.MeshOptions{BiomeColors: true})
	if err != nil {
		t.Fatal(err)
	}
	c := s.Meshes[0].Primitives[0].Colors
	expected := colorRGB(0xb76a37)
	for i := 0; i < 3; i++ {
		if c[i] != linearComponent(expected[i]) {
			t.Fatalf("dry foliage color: %v", c[:4])
		}
	}
}

func TestEyeblossomEyeBakedOnSameFace(t *testing.T) {
	encode := func(c color.NRGBA) string {
		im := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		im.SetNRGBA(0, 0, c)
		var buf bytes.Buffer
		png.Encode(&buf, im)
		return buf.String()
	}
	a := fixture(t, map[string]string{
		"assets/minecraft/blockstates/open_eyeblossom.json":            `{"variants":{"":{"model":"test:block/eye"}}}`,
		"assets/test/models/block/eye.json":                            `{"elements":[{"from":[0,0,8],"to":[16,16,8],"faces":{"north":{"uv":[0,0,16,16],"texture":"minecraft:block/open_eyeblossom"},"south":{"uv":[0,0,16,16],"texture":"minecraft:block/open_eyeblossom"}}},{"from":[0,0,8],"to":[16,16,8],"faces":{"north":{"uv":[0,0,16,16],"texture":"minecraft:block/open_eyeblossom_emissive"},"south":{"uv":[0,0,16,16],"texture":"minecraft:block/open_eyeblossom_emissive"}}}]}`,
		"assets/minecraft/textures/block/open_eyeblossom.png":          encode(color.NRGBA{50, 70, 20, 255}),
		"assets/minecraft/textures/block/open_eyeblossom_emissive.png": encode(color.NRGBA{255, 100, 0, 255}),
	})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:open_eyeblossom"}}), a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vertices, indices := counts(s)
	if vertices != 4 || indices != 6 || len(s.Materials) != 1 {
		t.Fatalf("eye still has coincident faces: %d vertices, %d indices, %d materials", vertices, indices, len(s.Materials))
	}
	m := s.Materials[0]
	if !m.DoubleSided || m.TextureName != "open_eyeblossom.png" {
		t.Fatal("baked material lost source name or back face")
	}
	im, err := png.Decode(bytes.NewReader(m.PNG))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(im.At(0, 0)).(color.NRGBA); c != (color.NRGBA{255, 100, 0, 255}) {
		t.Fatalf("eye not composited: %v", c)
	}
}

func TestLeafLitterMirroredBottomHasOneUpwardFace(t *testing.T) {
	a := fixture(t, map[string]string{"assets/test/models/block/stone.json": `{"elements":[{"from":[0,0.25,0],"to":[16,0.25,16],"faces":{"up":{"uv":[0,0,16,16],"texture":"test:block/stone"},"down":{"uv":[0,16,16,0],"texture":"test:block/stone"}}}]}`})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "test:stone"}}), a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, is := counts(s)
	if is != 6 {
		t.Fatalf("coincident litter bottom remains: %d indices", is)
	}
	if s.Meshes[0].Primitives[0].Normals[1] != 1 {
		t.Fatal("litter should face upward")
	}
}

func TestSnowLayersCullOnlyFullyCoveredSides(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/minecraft/blockstates/snow.json": `{"variants":{"":{"model":"test:block/snow"}}}`,
		"assets/test/models/block/snow.json":     `{"textures":{"all":"test:block/stone"},"elements":[{"from":[0,0,0],"to":[16,2,16],"faces":{"up":{"texture":"#all"},"down":{"texture":"#all","cullface":"down"},"north":{"texture":"#all","cullface":"north"},"south":{"texture":"#all","cullface":"south"},"west":{"texture":"#all","cullface":"west"},"east":{"texture":"#all","cullface":"east"}}}]}`,
	})
	s, err := Build(context.Background(), vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:snow"}, {1, 0, 0}: {Name: "minecraft:snow"}}), a, scene.MeshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, indices := counts(s)
	if indices != 60 {
		t.Fatalf("buried snow sides remain: %d indices", indices)
	}
}
