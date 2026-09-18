package selection

import (
	"astra-mwe/internal/scene"
	"errors"
	"strings"
)

// AutoMin finds the lowest topmost terrain surface across populated columns.
// Bounds.MinY must be the loaded dimension floor for a complete automatic scan.
func AutoMin(v *scene.Volume, padding int) (int, error) {
	if v == nil {
		return 0, errors.New("no volume loaded")
	}
	if padding < 4 {
		padding = 4
	}
	tops := map[[2]int]int{}
	b := v.Bounds
	for p, block := range v.Blocks {
		if p[0] < b.MinX || p[0] > b.MaxX || p[1] < b.MinY || p[1] > b.MaxY || p[2] < b.MinZ || p[2] > b.MaxZ || !terrain(block.Name) {
			continue
		}
		k := [2]int{p[0], p[2]}
		if y, ok := tops[k]; !ok || p[1] > y {
			tops[k] = p[1]
		}
	}
	if len(tops) == 0 {
		return 0, errors.New("no terrain surface found; set minimum height manually")
	}
	min := b.MaxY
	for _, y := range tops {
		if y < min {
			min = y
		}
	}
	if padding >= min-b.MinY {
		return b.MinY, nil
	}
	return min - padding, nil
}
func terrain(name string) bool {
	n := name
	if i := strings.IndexByte(n, ':'); i >= 0 {
		n = n[i+1:]
	}
	switch n {
	case "", "air", "cave_air", "void_air", "water", "lava", "bubble_column", "grass", "short_grass", "tall_grass", "fern", "large_fern", "dead_bush", "vine", "glow_lichen", "hanging_roots", "moss_carpet", "snow", "seagrass", "tall_seagrass", "kelp", "kelp_plant", "sugar_cane", "bamboo", "bamboo_sapling", "cactus", "lily_pad", "red_mushroom", "brown_mushroom", "red_mushroom_block", "brown_mushroom_block", "mushroom_stem", "azalea", "flowering_azalea", "pink_petals", "wildflowers", "leaf_litter", "fire", "soul_fire", "crimson_fungus", "warped_fungus", "crimson_roots", "warped_roots", "nether_sprouts", "weeping_vines", "weeping_vines_plant", "twisting_vines", "twisting_vines_plant", "wheat", "carrots", "potatoes", "beetroots", "sweet_berry_bush", "torchflower_crop", "pitcher_crop":
		return false
	}
	for _, suffix := range []string{"_leaves", "_log", "_wood", "_stem", "_hyphae", "_sapling", "_tulip", "_orchid", "_bluet", "_daisy", "_rose", "_flower", "_bush"} {
		if strings.HasSuffix(n, suffix) {
			return false
		}
	}
	switch n {
	case "dandelion", "poppy", "allium", "cornflower", "lily_of_the_valley", "sunflower", "lilac", "rose_bush", "peony", "torchflower", "pitcher_plant":
		return false
	}
	return true
}
