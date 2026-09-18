package world

import (
	"astra-mwe/internal/nbt"
	"astra-mwe/internal/scene"
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sort"
)

// unpack handles both Anvil palette encodings: older entries may cross a
// 64-bit boundary; padded entries never do and leave unused high bits per word.
// Validate the exact word count before indexing data from a world file.
func unpack(words []int64, palette, count, minBits int, padded bool) ([]int, error) {
	if palette < 1 || palette > 65536 {
		return nil, fmt.Errorf("invalid palette size %d", palette)
	}
	out := make([]int, count)
	if palette == 1 && len(words) == 0 {
		return out, nil
	}
	width := bits.Len(uint(palette - 1))
	if width < minBits {
		width = minBits
	}
	per := 64 / width
	required := (count*width + 63) / 64
	if padded {
		required = (count + per - 1) / per
	}
	if len(words) != required {
		return nil, fmt.Errorf("packed palette has %d longs, expected %d", len(words), required)
	}
	mask := uint64(1<<width) - 1
	for i := range out {
		var value uint64
		if padded {
			value = uint64(words[i/per]) >> uint(i%per*width)
		} else {
			bit := i * width
			word, shift := bit/64, bit%64
			value = uint64(words[word]) >> uint(shift)
			if shift+width > 64 {
				value |= uint64(words[word+1]) << uint(64-shift)
			}
		}
		out[i] = int(value & mask)
		if out[i] >= palette {
			return nil, fmt.Errorf("palette index %d exceeds size %d", out[i], palette)
		}
	}
	return out, nil
}
func decodeChunk(ctx context.Context, root nbt.Compound, cx, cz, version int, v *scene.Volume) error {
	return decodeChunkMode(ctx, root, cx, cz, version, v, false)
}

func decodeChunkMode(ctx context.Context, root nbt.Compound, cx, cz, version int, v *scene.Volume, surface bool) error {
	step := 0
	if surface {
		step = 1
	}
	return decodeChunkSampled(ctx, root, cx, cz, version, v, step, nil)
}

func decodeChunkSampled(ctx context.Context, root nbt.Compound, cx, cz, version int, v *scene.Volume, step int, biomes *surfaceBiomeStore) error {
	surface := step > 0
	if n := integer(root["DataVersion"]); n != 0 {
		version = n
	}
	c := root
	if level := compound(root["Level"]); level != nil {
		c = level
	}
	for key, want := range map[string]int{"xPos": cx, "zPos": cz} {
		if value, ok := c[key]; ok && integer(value) != want {
			return fmt.Errorf("stored %s %d disagrees with region coordinates", key, integer(value))
		}
	}
	sections, ok := c["sections"]
	modern := ok
	if !ok {
		sections, ok = c["Sections"]
	}
	if !ok {
		return errors.New("unsupported chunk layout: no sections/Sections list")
	}
	ss, ok := sections.([]any)
	if !ok {
		return errors.New("chunk sections is not an NBT list")
	}
	if len(ss) > 256 {
		return errors.New("chunk exceeds 256 section limit")
	}
	if surface {
		ss = append([]any(nil), ss...)
		sort.SliceStable(ss, func(i, j int) bool {
			return integer(compound(ss[i])["Y"]) > integer(compound(ss[j])["Y"])
		})
	}
	var retained [256]uint8
	seen := map[int]bool{}
	b := v.Bounds
	for _, value := range ss {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := compound(value)
		if s == nil {
			return errors.New("section is not a compound")
		}
		yv, ok := s["Y"]
		if !ok {
			return errors.New("section has no Y coordinate")
		}
		sy := integer(yv)
		if seen[sy] {
			return fmt.Errorf("duplicate section Y %d", sy)
		}
		seen[sy] = true
		if sy*16 > b.MaxY || sy*16+15 < b.MinY {
			continue
		}
		states := s
		pk, dk := "Palette", "BlockStates"
		if modern {
			states = compound(s["block_states"])
			pk, dk = "palette", "data"
			if s["block_states"] != nil && (states == nil || states[pk] == nil) {
				return errors.New("block_states has no palette compound")
			}
		}
		if !modern && states[dk] != nil && states[pk] == nil {
			return errors.New("BlockStates has no Palette")
		}
		if states != nil && states[pk] != nil {
			entries := list(states[pk])
			palette := make([]scene.Block, len(entries))
			for i, entry := range entries {
				p := compound(entry)
				name := stringValue(p["Name"])
				if name == "" {
					return errors.New("block palette entry has no Name")
				}
				block := scene.Block{Name: name}
				if props := compound(p["Properties"]); props != nil {
					block.Properties = map[string]string{}
					for k, value := range props {
						str, ok := value.(string)
						if !ok {
							return fmt.Errorf("block property %s is not a string", k)
						}
						block.Properties[k] = str
					}
				}
				palette[i] = block
			}
			words, valid := states[dk].([]int64)
			if states[dk] != nil && !valid {
				return errors.New("block state data is not a long array")
			}
			if surface {
				packed, e := surfacePacked(words, len(palette), version >= 2529 || modern)
				if e != nil {
					return e
				}
				for z := firstSurfaceSample(max(b.MinZ, cz*16), step); z <= min(b.MaxZ, cz*16+15); z += step {
					for x := firstSurfaceSample(max(b.MinX, cx*16), step); x <= min(b.MaxX, cx*16+15); x += step {
						column := (z&15)*16 + (x & 15)
						for y := min(b.MaxY, sy*16+15); y >= max(b.MinY, sy*16) && retained[column] < 8; y-- {
							block := palette[packed.at((y&15)*256+column)]
							if isAir(block) {
								continue
							}
							v.Blocks[[3]int{x, y, z}] = block
							retained[column]++
						}
					}
				}
			} else {
				indices, e := unpack(words, len(palette), 4096, 4, version >= 2529 || modern)
				if e != nil {
					return e
				}
				for index, p := range indices {
					block := palette[p]
					if isAir(block) {
						continue
					}
					x, y, z := cx*16+(index&15), sy*16+(index>>8), cz*16+((index>>4)&15)
					if x >= b.MinX && x <= b.MaxX && y >= b.MinY && y <= b.MaxY && z >= b.MinZ && z <= b.MaxZ {
						v.Blocks[[3]int{x, y, z}] = block
					}
				}
			}
		} else if !modern && s["Blocks"] != nil {
			return errors.New("pre-flattening numeric blocks are unsupported; Java 1.13+ required")
		}
		if modern {
			if biome := compound(s["biomes"]); biome != nil {
				entries := list(biome["palette"])
				names := make([]string, len(entries))
				for i, value := range entries {
					names[i] = stringValue(value)
					if names[i] == "" {
						return errors.New("biome palette entry is not a name")
					}
				}
				words, valid := biome["data"].([]int64)
				if biome["data"] != nil && !valid {
					return errors.New("biome data is not a long array")
				}
				indices, e := unpack(words, len(names), 64, 1, true)
				if e != nil {
					return fmt.Errorf("biomes: %w", e)
				}
				if biomes != nil {
					biomes.addSection(cx, sy, cz, names, indices)
				} else {
					for i, p := range indices {
						putBiome(v, cx*16+(i&3)*4, sy*16+(i>>4)*4, cz*16+((i>>2)&3)*4, names[p])
					}
				}
			}
		}
	}
	if !modern {
		var e error
		if biomes != nil {
			e = biomes.addLegacy(c["Biomes"], cx, cz)
		} else {
			e = oldBiomes(c["Biomes"], cx, cz, v)
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func putBiome(v *scene.Volume, x, y, z int, name string) {
	b := v.Bounds
	if x+3 >= b.MinX && x <= b.MaxX && y+3 >= b.MinY && y <= b.MaxY && z+3 >= b.MinZ && z <= b.MaxZ {
		v.Biomes[[3]int{x, y, z}] = name
	}
}
func oldBiomes(raw any, cx, cz int, v *scene.Volume) error {
	if raw == nil {
		return nil
	}
	var ids []int32
	switch a := raw.(type) {
	case []int32:
		ids = a
	case []byte:
		ids = make([]int32, len(a))
		for i, b := range a {
			ids[i] = int32(b)
		}
	default:
		return errors.New("legacy Biomes is not a byte/int array")
	}
	if len(ids) == 256 {
		for y := (v.Bounds.MinY >> 2) * 4; y <= v.Bounds.MaxY; y += 4 {
			for z := 0; z < 4; z++ {
				for x := 0; x < 4; x++ {
					putBiome(v, cx*16+x*4, y, cz*16+z*4, biomeName(ids[z*4*16+x*4]))
				}
			}
		}
		return nil
	}
	if len(ids) == 1024 {
		for i, id := range ids {
			putBiome(v, cx*16+(i&3)*4, (i>>4)*4, cz*16+((i>>2)&3)*4, biomeName(id))
		}
		return nil
	}
	return fmt.Errorf("legacy Biomes length %d is unsupported", len(ids))
}
func biomeName(id int32) string {
	if s, ok := legacyBiomes[id]; ok {
		return "minecraft:" + s
	}
	return fmt.Sprintf("legacy:biome_%d", id)
}

var legacyBiomes = map[int32]string{0: "ocean", 1: "plains", 2: "desert", 3: "mountains", 4: "forest", 5: "taiga", 6: "swamp", 7: "river", 8: "nether_wastes", 9: "the_end", 10: "frozen_ocean", 11: "frozen_river", 12: "snowy_tundra", 13: "snowy_mountains", 14: "mushroom_fields", 15: "mushroom_field_shore", 16: "beach", 17: "desert_hills", 18: "wooded_hills", 19: "taiga_hills", 20: "mountain_edge", 21: "jungle", 22: "jungle_hills", 23: "jungle_edge", 24: "deep_ocean", 25: "stone_shore", 26: "snowy_beach", 27: "birch_forest", 28: "birch_forest_hills", 29: "dark_forest", 30: "snowy_taiga", 31: "snowy_taiga_hills", 32: "giant_tree_taiga", 33: "giant_tree_taiga_hills", 34: "wooded_mountains", 35: "savanna", 36: "savanna_plateau", 37: "badlands", 38: "wooded_badlands_plateau", 39: "badlands_plateau", 40: "small_end_islands", 41: "end_midlands", 42: "end_highlands", 43: "end_barrens", 44: "warm_ocean", 45: "lukewarm_ocean", 46: "cold_ocean", 47: "deep_warm_ocean", 48: "deep_lukewarm_ocean", 49: "deep_cold_ocean", 50: "deep_frozen_ocean", 127: "the_void", 129: "sunflower_plains", 130: "desert_lakes", 131: "gravelly_mountains", 132: "flower_forest", 133: "taiga_mountains", 134: "swamp_hills", 140: "ice_spikes", 149: "modified_jungle", 151: "modified_jungle_edge", 155: "tall_birch_forest", 156: "tall_birch_hills", 157: "dark_forest_hills", 158: "snowy_taiga_mountains", 160: "giant_spruce_taiga", 161: "giant_spruce_taiga_hills", 162: "modified_gravelly_mountains", 163: "shattered_savanna", 164: "shattered_savanna_plateau", 165: "eroded_badlands", 166: "modified_wooded_badlands_plateau", 167: "modified_badlands_plateau", 168: "bamboo_jungle", 169: "bamboo_jungle_hills", 170: "soul_sand_valley", 171: "crimson_forest", 172: "warped_forest", 173: "basalt_deltas", 174: "dripstone_caves", 175: "lush_caves"}
