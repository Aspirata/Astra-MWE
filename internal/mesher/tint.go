package mesher

import (
	"astra-mwe/internal/scene"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"strings"
)

// Minecraft changed biome color fields from decimal numbers to #RRGGBB strings.
func (v *biome) UnmarshalJSON(data []byte) error {
	type plain biome
	var raw struct {
		Temperature float64                    `json:"temperature"`
		Downfall    float64                    `json:"downfall"`
		Effects     map[string]json.RawMessage `json:"effects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v.Temperature, v.Downfall = raw.Temperature, raw.Downfall
	for key, dest := range map[string]**int{"grass_color": &v.Effects.Grass, "foliage_color": &v.Effects.Foliage, "dry_foliage_color": &v.Effects.DryFoliage, "water_color": &v.Effects.Water} {
		value, ok := raw.Effects[key]
		if !ok || string(value) == "null" {
			continue
		}
		var n int
		if err := json.Unmarshal(value, &n); err != nil {
			var hex string
			if err := json.Unmarshal(value, &hex); err != nil {
				return err
			}
			parsed, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 24)
			if err != nil {
				return fmt.Errorf("invalid biome color %s: %w", key, err)
			}
			n = int(parsed)
		}
		*dest = &n
	}
	if value := raw.Effects["grass_color_modifier"]; value != nil {
		return json.Unmarshal(value, &v.Effects.Modifier)
	}
	return nil
}

func linearComponent(s float32) float32 {
	if s <= .04045 {
		return s / 12.92
	}
	return float32(math.Pow(float64((s+.055)/1.055), 2.4))
}
func srgbComponent(v float32) float32 {
	if v <= .0031308 {
		return v * 12.92
	}
	return float32(1.055*math.Pow(float64(v), 1/2.4) - .055)
}

type biome struct {
	Temperature float64 `json:"temperature"`
	Downfall    float64 `json:"downfall"`
	Effects     struct {
		Grass      *int   `json:"grass_color"`
		Foliage    *int   `json:"foliage_color"`
		DryFoliage *int   `json:"dry_foliage_color"`
		Water      *int   `json:"water_color"`
		Modifier   string `json:"grass_color_modifier"`
	} `json:"effects"`
}
type climate struct {
	temp, rain float64
	water      int
}

var climates = map[string]climate{
	"plains": {.8, .4, 0x3f76e4}, "sunflower_plains": {.8, .4, 0x3f76e4}, "forest": {.7, .8, 0x3f76e4}, "flower_forest": {.7, .8, 0x3f76e4}, "birch_forest": {.6, .6, 0x3f76e4}, "old_growth_birch_forest": {.6, .6, 0x3f76e4}, "dark_forest": {.7, .8, 0x3f76e4},
	"taiga": {.25, .8, 0x3f76e4}, "old_growth_pine_taiga": {.3, .8, 0x3f76e4}, "old_growth_spruce_taiga": {.25, .8, 0x3f76e4}, "snowy_taiga": {-.5, .4, 0x3d57d6}, "snowy_plains": {0, .5, 0x3f76e4}, "snowy_beach": {.05, .3, 0x3d57d6}, "ice_spikes": {0, .5, 0x3f76e4},
	"swamp": {.8, .9, 0x617b64}, "mangrove_swamp": {.8, .9, 0x3a7a6a}, "jungle": {.95, .9, 0x3f76e4}, "sparse_jungle": {.95, .8, 0x3f76e4}, "bamboo_jungle": {.95, .9, 0x3f76e4}, "desert": {2, 0, 0x3f76e4}, "savanna": {2, 0, 0x3f76e4}, "savanna_plateau": {2, 0, 0x3f76e4}, "windswept_savanna": {2, 0, 0x3f76e4}, "badlands": {2, 0, 0x3f76e4}, "wooded_badlands": {2, 0, 0x3f76e4}, "eroded_badlands": {2, 0, 0x3f76e4},
	"ocean": {.5, .5, 0x3f76e4}, "deep_ocean": {.5, .5, 0x3f76e4}, "warm_ocean": {.5, .5, 0x43d5ee}, "lukewarm_ocean": {.5, .5, 0x45adf2}, "deep_lukewarm_ocean": {.5, .5, 0x45adf2}, "cold_ocean": {.5, .5, 0x3d57d6}, "deep_cold_ocean": {.5, .5, 0x3d57d6}, "frozen_ocean": {0, .5, 0x3938c9}, "deep_frozen_ocean": {.5, .5, 0x3938c9}, "river": {.5, .5, 0x3f76e4}, "frozen_river": {0, .5, 0x3938c9}, "beach": {.8, .4, 0x3f76e4}, "stony_shore": {.2, .3, 0x3f76e4},
	"meadow": {.5, .8, 0x0e4ecf}, "cherry_grove": {.5, .8, 0x5db7ef}, "grove": {-.2, .8, 0x3f76e4}, "snowy_slopes": {-.3, .9, 0x3f76e4}, "frozen_peaks": {-.7, .9, 0x3f76e4}, "jagged_peaks": {-.7, .9, 0x3f76e4}, "stony_peaks": {1, .3, 0x3f76e4}, "windswept_hills": {.2, .3, 0x3f76e4}, "windswept_forest": {.2, .3, 0x3f76e4}, "windswept_gravelly_hills": {.2, .3, 0x3f76e4},
	"mushroom_fields": {.9, 1, 0x3f76e4}, "dripstone_caves": {.8, .4, 0x3f76e4}, "lush_caves": {.5, .5, 0x3f76e4}, "deep_dark": {.8, .4, 0x3f76e4}, "pale_garden": {.7, .8, 0x76889d}, "nether_wastes": {2, 0, 0x3f76e4}, "soul_sand_valley": {2, 0, 0x3f76e4}, "crimson_forest": {2, 0, 0x3f76e4}, "warped_forest": {2, 0, 0x3f76e4}, "basalt_deltas": {2, 0, 0x3f76e4}, "the_end": {.5, .5, 0x3f76e4}, "end_highlands": {.5, .5, 0x3f76e4}, "end_midlands": {.5, .5, 0x3f76e4}, "small_end_islands": {.5, .5, 0x3f76e4}, "end_barrens": {.5, .5, 0x3f76e4},
}

func colorRGB(rgb int) [3]float32 {
	return [3]float32{float32((rgb>>16)&255) / 255, float32((rgb>>8)&255) / 255, float32(rgb&255) / 255}
}
func (b *builder) biome(name string) biome {
	if v, ok := b.biomes[name]; ok {
		return v
	}
	ns, n := splitID(name)
	v := biome{Temperature: .8, Downfall: .4}
	raw, e := b.assets.Read("data/" + ns + "/worldgen/biome/" + n + ".json")
	if e == nil && json.Unmarshal(raw, &v) == nil {
		b.biomes[name] = v
		return v
	}
	if c, ok := climates[n]; ok && ns == "minecraft" {
		v.Temperature, v.Downfall = c.temp, c.rain
		water := c.water
		v.Effects.Water = &water
		switch n {
		case "swamp", "mangrove_swamp":
			dry := 0x7b5334
			v.Effects.DryFoliage = &dry
			v.Effects.Modifier = "swamp"
			fol := 0x6a7039
			if n == "mangrove_swamp" {
				fol = 0x8db127
			}
			v.Effects.Foliage = &fol
		case "dark_forest":
			dry := 0x7b5334
			v.Effects.DryFoliage = &dry
			v.Effects.Modifier = "dark_forest"
		case "badlands", "wooded_badlands", "eroded_badlands":
			g, f := 0x90814d, 0x9e814d
			v.Effects.Grass = &g
			v.Effects.Foliage = &f
		case "cherry_grove":
			g, f := 0xb6db61, 0xb6db61
			v.Effects.Grass = &g
			v.Effects.Foliage = &f
		case "pale_garden":
			dry := 0xa0a69c
			v.Effects.DryFoliage = &dry
			g, f := 0x778272, 0x878d76
			v.Effects.Grass = &g
			v.Effects.Foliage = &f
		}
	} else {
		b.warn("Unknown biome " + name + ": using plains tint")
	}
	b.biomes[name] = v
	return v
}
func (b *builder) colormap(kind string) image.Image {
	if im, ok := b.colormaps[kind]; ok {
		return im
	}
	var im image.Image
	if data, e := b.assets.Read("assets/minecraft/textures/colormap/" + kind + ".png"); e == nil {
		im, _ = png.Decode(bytes.NewReader(data))
	}
	b.colormaps[kind] = im
	return im
}
func (b *builder) biomeColor(name, kind string) [3]float32 {
	v := b.biome(name)
	if kind == "water" {
		if v.Effects.Water != nil {
			return colorRGB(*v.Effects.Water)
		}
		return colorRGB(0x3f76e4)
	}
	explicit := v.Effects.Grass
	if kind == "foliage" {
		explicit = v.Effects.Foliage
	} else if kind == "dry_foliage" {
		explicit = v.Effects.DryFoliage
	}
	var rgb [3]float32
	if explicit != nil {
		rgb = colorRGB(*explicit)
	} else if im := b.colormap(kind); im != nil {
		temp := math.Max(0, math.Min(1, v.Temperature))
		rain := math.Max(0, math.Min(1, v.Downfall)) * temp
		rect := im.Bounds()
		x := rect.Min.X + int((1-temp)*float64(rect.Dx()-1))
		y := rect.Min.Y + int((1-rain)*float64(rect.Dy()-1))
		r, g, bb, _ := im.At(x, y).RGBA()
		rgb = [3]float32{float32(r) / 65535, float32(g) / 65535, float32(bb) / 65535}
	} else {
		b.warn("Missing " + kind + " colormap: using fallback biome colors")
		if kind == "dry_foliage" {
			rgb = colorRGB(0xa38c4a)
		} else if kind == "foliage" {
			rgb = colorRGB(0x48b518)
		} else {
			rgb = colorRGB(0x91bd59)
		}
	}
	if kind == "grass" {
		switch v.Effects.Modifier {
		case "swamp":
			rgb = colorRGB(0x6a7039)
		case "dark_forest":
			base := colorRGB(0x28340a)
			for i := range rgb {
				rgb[i] = (rgb[i] + base[i]) * .5
			}
		}
	}
	return rgb
}

// A model can mark only selected surfaces for coloring. Flowerbeds additionally
// reserve layer zero for unchanged petals and layer one for biome-colored stems.
func tintedFace(block scene.Block, index *int) bool {
	if index == nil || *index < 0 {
		return false
	}
	ns, name := splitID(block.Name)
	if ns == "minecraft" && (name == "pink_petals" || name == "wildflowers") {
		return *index == 1
	}
	return true
}

func (b *builder) tint(block scene.Block, pos [3]int, tinted bool) (result [4]float32) {
	// Minecraft blends its biome lookup in sRGB. glTF COLOR_0 is linear.
	defer func() {
		for i := 0; i < 3; i++ {
			result[i] = linearComponent(result[i])
		}
	}()
	out := [4]float32{1, 1, 1, 1}
	if !tinted {
		return out
	}
	ns, name := splitID(block.Name)
	// Cherry artwork is already pink; Java does not register a biome color
	// provider for this block, even when a pack supplies a model tint index.
	if ns == "minecraft" && name == "cherry_leaves" {
		return out
	}
	if ns == "minecraft" && name == "redstone_wire" {
		power, _ := strconv.Atoi(block.Properties["power"])
		f := float64(max(0, min(15, power))) / 15
		r := f*.6 + .4
		if power == 0 {
			r = .3
		}
		return [4]float32{float32(r), float32(math.Max(0, f*f*.7-.5)), float32(math.Max(0, f*f*.6-.7)), 1}
	}
	if ns == "minecraft" && name == "spruce_leaves" {
		v := colorRGB(0x619961)
		return [4]float32{v[0], v[1], v[2], 1}
	}
	if ns == "minecraft" && name == "birch_leaves" {
		v := colorRGB(0x80a755)
		return [4]float32{v[0], v[1], v[2], 1}
	}
	if ns == "minecraft" {
		fixed := -1
		switch name {
		case "lily_pad":
			fixed = 0x208030
		case "attached_melon_stem", "attached_pumpkin_stem":
			fixed = 0xe0c71c
		case "melon_stem", "pumpkin_stem":
			age, _ := strconv.Atoi(block.Properties["age"])
			age = max(0, min(7, age))
			fixed = (age*32)<<16 | (255-age*8)<<8 | age*4
		}
		if fixed >= 0 {
			v := colorRGB(fixed)
			return [4]float32{v[0], v[1], v[2], 1}
		}
	}
	if !b.opts.BiomeColors {
		return out
	}
	// Unrecognized model-marked surfaces use grass as a useful fallback for
	// new or modded vegetation. Unmarked, already-colored textures return above.
	kind := "grass"
	if ns == "minecraft" && (name == "water" || name == "bubble_column" || name == "water_cauldron") {
		kind = "water"
	} else if ns == "minecraft" && name == "leaf_litter" {
		kind = "dry_foliage"
	} else if b.leaf(block.Name) || name == "vine" {
		kind = "foliage"
	}
	if ns == "minecraft" && (name == "tall_grass" || name == "large_fern") && block.Properties["half"] == "upper" {
		pos[1]--
	}
	key := tintKey{pos, kind}
	if rgb, ok := b.tints[key]; ok {
		return [4]float32{rgb[0], rgb[1], rgb[2], 1}
	}
	radius := max(0, min(16, b.opts.BiomeBlend))
	sum := [3]float32{}
	n := float32(0)
	colors := map[string][3]float32{}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			cell := [3]int{floorDiv(pos[0]+dx, 4) * 4, floorDiv(pos[1], 4) * 4, floorDiv(pos[2]+dz, 4) * 4}
			biomeName := b.volume.Biomes[cell]
			if biomeName == "" {
				biomeName = "minecraft:plains"
				b.warn("Missing biome cells: using plains tint")
			}
			c, ok := colors[biomeName]
			if !ok {
				c = b.biomeColor(biomeName, kind)
				colors[biomeName] = c
			}
			for i := range sum {
				sum[i] += c[i]
			}
			n++
		}
	}
	for i := range sum {
		sum[i] /= n
	}
	b.tints[key] = sum
	return [4]float32{sum[0], sum[1], sum[2], 1}
}
