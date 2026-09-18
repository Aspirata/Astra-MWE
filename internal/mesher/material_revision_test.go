package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrassCompositePreservesDirtAndTransparentPackAlpha(t *testing.T) {
	tint := [4]float32{linearComponent(.5), 1, 1, 1}
	dirt := color.NRGBA{83, 40, 17, 255}
	if got := composeGrassPixel(dirt, color.NRGBA{255, 255, 255, 0}, tint); got != dirt {
		t.Fatal("untinted dirt changed", got)
	}
	got := composeGrassPixel(color.NRGBA{}, color.NRGBA{200, 100, 50, 128}, tint)
	if got != (color.NRGBA{100, 100, 50, 128}) {
		t.Fatal("resource-pack overlay alpha lost", got)
	}
}

func TestClientGrassAndIceMaterials(t *testing.T) {
	for _, version := range []string{"1.20.1", "26.1.2"} {
		t.Run(version, func(t *testing.T) {
			jar := filepath.Join("..", "..", ".local-testdata", "minecraft-"+version+".jar")
			if _, err := os.Stat(jar); err != nil {
				t.Skip(err)
			}
			a, err := assets.Open([]string{jar})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			for _, name := range []string{"grass_block", "ice", "packed_ice", "blue_ice"} {
				t.Run(name, func(t *testing.T) {
					v := vol(map[[3]int]scene.Block{{0, 0, 0}: {Name: "minecraft:" + name, Properties: map[string]string{"snowy": "false"}}})
					v.Biomes = map[[3]int]string{{0, 0, 0}: "minecraft:plains"}
					sc, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
					if err != nil {
						t.Fatal(err)
					}
					_, indices := counts(sc)
					if indices != 36 {
						t.Fatalf("cube should have six surfaces, got %d triangles", indices/3)
					}
					for _, m := range sc.Materials {
						if strings.Contains(m.TextureName, "missing") {
							t.Fatal("test used fallback geometry")
						}
						if m.DoubleSided {
							t.Error("closed block renders back faces")
						}
						if strings.Contains(m.TextureName, "overlay") {
							t.Errorf("overlay exported: %s", m.TextureName)
						}
						if !strings.HasSuffix(m.TextureName, ".png") {
							t.Errorf("source PNG name missing: %s", m.TextureName)
						}
						if name == "ice" {
							if m.Alpha != "BLEND" {
								t.Error("ice alpha not blended")
							}
							im, err := png.Decode(bytes.NewReader(m.PNG))
							if err != nil {
								t.Fatal(err)
							}
							partial := false
							for y := 0; y < im.Bounds().Dy(); y++ {
								for x := 0; x < im.Bounds().Dx(); x++ {
									_, _, _, alpha := im.At(x, y).RGBA()
									partial = partial || (alpha > 0 && alpha < 65535)
								}
							}
							if !partial {
								t.Error("ice lost fractional alpha")
							}
						} else if m.Alpha != "OPAQUE" {
							t.Errorf("%s unexpectedly translucent: %s", name, m.Alpha)
						}
					}
				})
			}
		})
	}
}

func TestTintHexAndLinearOutput(t *testing.T) {
	a := fixture(t, map[string]string{"data/test/worldgen/biome/custom.json": `{"temperature":0.8,"downfall":0.4,"effects":{"grass_color":"#808080","water_color":"#617b64"}}`})
	b := &builder{assets: a, opts: scene.MeshOptions{BiomeColors: true}, volume: &scene.Volume{Biomes: map[[3]int]string{{0, 0, 0}: "test:custom"}}, biomes: map[string]biome{}, tints: map[tintKey][3]float32{}, warnings: map[string]bool{}, scene: &scene.Scene{}}
	v := b.tint(scene.Block{Name: "minecraft:grass_block"}, [3]int{}, true)
	if v[0] < .215 || v[0] > .217 || v[1] != v[0] || v[3] != 1 {
		t.Fatalf("sRGB #808080 must become linear ~0.21586, got %v", v)
	}
	if len(b.scene.Warnings) != 0 {
		t.Fatal(b.scene.Warnings)
	}
}
