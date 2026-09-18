package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"context"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func vegetationTintBuilder(t *testing.T) *builder {
	t.Helper()
	a := fixture(t, map[string]string{
		"data/test/worldgen/biome/custom.json": `{"temperature":0.8,"downfall":0.4,"effects":{"grass_color":"#00ff00","foliage_color":"#ff0000","dry_foliage_color":"#0000ff","water_color":"#00ffff"}}`,
	})
	return &builder{assets: a, opts: scene.MeshOptions{BiomeColors: true}, volume: &scene.Volume{Biomes: map[[3]int]string{{}: "test:custom"}}, biomes: map[string]biome{}, colormaps: map[string]image.Image{}, tints: map[tintKey][3]float32{}, warnings: map[string]bool{}, leaves: map[string]bool{"test:canopy": true}, scene: &scene.Scene{}}
}

func TestVegetationTintUsesModelAndBiomeCategory(t *testing.T) {
	b := vegetationTintBuilder(t)
	for _, tc := range []struct {
		name string
		want [4]float32
	}{
		{"minecraft:pink_petals", [4]float32{0, 1, 0, 1}},
		{"minecraft:wildflowers", [4]float32{0, 1, 0, 1}},
		{"minecraft:bush", [4]float32{0, 1, 0, 1}},
		{"minecraft:potted_fern", [4]float32{0, 1, 0, 1}},
		{"test:meadow_flower", [4]float32{0, 1, 0, 1}},
		{"test:canopy", [4]float32{1, 0, 0, 1}},
		{"minecraft:oak_leaves", [4]float32{1, 0, 0, 1}},
		{"minecraft:vine", [4]float32{1, 0, 0, 1}},
		{"minecraft:leaf_litter", [4]float32{0, 0, 1, 1}},
		{"minecraft:water_cauldron", [4]float32{0, 1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.tint(scene.Block{Name: tc.name}, [3]int{}, true); got != tc.want {
				t.Fatalf("model-marked surface got %v, want %v", got, tc.want)
			}
			if got := b.tint(scene.Block{Name: tc.name}, [3]int{}, false); got != ([4]float32{1, 1, 1, 1}) {
				t.Fatalf("unmarked surface must keep its texture colors, got %v", got)
			}
		})
	}
}

func TestVegetationSpecialTintsIgnoreBiome(t *testing.T) {
	b := vegetationTintBuilder(t)
	for _, tc := range []struct {
		name, property, value string
		want                  [3]float64
	}{
		{"minecraft:melon_stem", "age", "0", [3]float64{0, 255, 0}},
		{"minecraft:pumpkin_stem", "age", "4", [3]float64{128, 223, 16}},
		{"minecraft:melon_stem", "age", "7", [3]float64{224, 199, 28}},
		{"minecraft:attached_melon_stem", "", "", [3]float64{224, 199, 28}},
		{"minecraft:attached_pumpkin_stem", "", "", [3]float64{224, 199, 28}},
		{"minecraft:lily_pad", "", "", [3]float64{32, 128, 48}},
		{"minecraft:birch_leaves", "", "", [3]float64{128, 167, 85}},
		{"minecraft:spruce_leaves", "", "", [3]float64{97, 153, 97}},
		{"minecraft:redstone_wire", "power", "0", [3]float64{76.5, 0, 0}},
		{"minecraft:redstone_wire", "power", "15", [3]float64{255, 51, 0}},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				b.opts.BiomeColors = enabled
				got := b.tint(scene.Block{Name: tc.name, Properties: map[string]string{tc.property: tc.value}}, [3]int{}, true)
				for i, want := range tc.want {
					if actual := float64(srgbComponent(got[i])) * 255; math.Abs(actual-want) > .001 {
						t.Fatalf("channel %d got %g, want %g (biomes=%v)", i, actual, want, enabled)
					}
				}
			}
		})
	}
}

func TestDoublePlantUpperHalfUsesLowerBiome(t *testing.T) {
	b := vegetationTintBuilder(t)
	b.volume.Biomes[[3]int{0, 4, 0}] = "minecraft:badlands"
	for _, name := range []string{"tall_grass", "large_fern"} {
		got := b.tint(scene.Block{Name: "minecraft:" + name, Properties: map[string]string{"half": "upper"}}, [3]int{0, 4, 0}, true)
		if got != ([4]float32{0, 1, 0, 1}) {
			t.Fatalf("%s upper half uses different biome from lower half: %v", name, got)
		}
	}
}

func TestVegetationBlendSevenKeepsSampleWeightsAndLinearColors(t *testing.T) {
	b := vegetationTintBuilder(t)
	b.opts.BiomeBlend = 7
	blue := 0x0000ff
	v := biome{}
	v.Effects.Grass = &blue
	b.biomes["test:blue"] = v
	for x := -8; x <= 4; x += 4 {
		for z := -8; z <= 4; z += 4 {
			name := "test:custom"
			if x < 0 {
				name = "test:blue"
			}
			b.volume.Biomes[[3]int{x, 0, z}] = name
		}
	}
	// The 15x15 window contains eight green columns and seven blue ones.
	// Repeated requests must return the same linear color from the tint cache.
	for attempt := 0; attempt < 2; attempt++ {
		got := b.tint(scene.Block{Name: "minecraft:wildflowers"}, [3]int{}, true)
		for i, want := range [3]float64{0, 8.0 / 15, 7.0 / 15} {
			if actual := float64(srgbComponent(got[i])); math.Abs(actual-want) > .000001 {
				t.Fatalf("blend channel %d: got %g sRGB, want %g", i, actual, want)
			}
		}
	}
}

func TestFlowerModelPreservesBlankAndNegativeTintLayers(t *testing.T) {
	a := fixture(t, map[string]string{
		"assets/minecraft/blockstates/pink_petals.json": `{"variants":{"":{"model":"test:block/layers"}}}`,
		"assets/test/models/block/layers.json":          `{"textures":{"all":"test:block/stone"},"elements":[{"from":[0,0,0],"to":[16,1,16],"faces":{"up":{"texture":"#all","tintindex":0},"down":{"texture":"#all","tintindex":1},"north":{"texture":"#all","tintindex":-1}}}]}`,
		"data/test/worldgen/biome/custom.json":          `{"effects":{"grass_color":"#00ff00"}}`,
	})
	v := vol(map[[3]int]scene.Block{{}: {Name: "minecraft:pink_petals"}})
	v.Biomes = map[[3]int]string{{}: "test:custom"}
	sc, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
	if err != nil {
		t.Fatal(err)
	}
	white, green := 0, 0
	for _, m := range sc.Meshes {
		for _, p := range m.Primitives {
			for i := 0; i < len(p.Colors); i += 4 {
				c := [4]float32{p.Colors[i], p.Colors[i+1], p.Colors[i+2], p.Colors[i+3]}
				switch c {
				case [4]float32{1, 1, 1, 1}:
					white++
				case [4]float32{0, 1, 0, 1}:
					green++
				default:
					t.Fatalf("unexpected layer color %v", c)
				}
			}
		}
	}
	if white != 8 || green != 4 {
		t.Fatalf("blank/negative layers must stay white; got %d white, %d green vertices", white, green)
	}
}

func TestClientVegetationTintedStemsAndUntintedPetals(t *testing.T) {
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
			names := []string{"pink_petals", "potted_fern"}
			if version == "26.1.2" {
				names = append(names, "wildflowers", "bush", "leaf_litter")
			}
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					v := vol(map[[3]int]scene.Block{{}: {Name: "minecraft:" + name, Properties: map[string]string{"facing": "north", "flower_amount": "4", "segment_amount": "4"}}})
					v.Biomes = map[[3]int]string{{}: "minecraft:plains"}
					sc, err := Build(context.Background(), v, a, scene.MeshOptions{BiomeColors: true})
					if err != nil {
						t.Fatal(err)
					}
					tinted, petals := 0, 0
					for _, m := range sc.Meshes {
						for _, p := range m.Primitives {
							material := sc.Materials[p.Material].TextureName
							if strings.Contains(material, "missing") {
								t.Fatal("fallback model: ", material)
							}
							for i := 0; i < len(p.Colors); i += 4 {
								white := p.Colors[i] == 1 && p.Colors[i+1] == 1 && p.Colors[i+2] == 1
								if !white {
									tinted++
								}
								if (name == "pink_petals" || name == "wildflowers") && !strings.Contains(material, "stem") {
									petals++
									if !white {
										t.Fatalf("petal texture %s was recolored", material)
									}
								}
							}
						}
					}
					if tinted == 0 {
						t.Fatal("all plant vertices are white: model tint was lost")
					}
					if (name == "pink_petals" || name == "wildflowers") && petals == 0 {
						t.Fatal("missing flower petals")
					}
				})
			}
		})
	}
}
