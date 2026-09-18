package service

import (
	"astra-mwe/internal/scene"
	"math"
)

func DemoSurface(b scene.Bounds, step int) *scene.Volume {
	v := &scene.Volume{Bounds: b, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
	height := func(x, z int) int { return int(63 + 3*math.Sin(float64(x)*.12) + 2*math.Cos(float64(z)*.16)) }
	put := func(x, y, z int, name string) {
		if y >= b.MinY && y <= b.MaxY {
			v.Blocks[[3]int{x, y, z}] = scene.Block{Name: "minecraft:" + name}
		}
	}
	start := func(n int) int { return int(math.Ceil(float64(n)/float64(step))) * step }
	for x := start(b.MinX); x <= b.MaxX; x += step {
		for z := start(b.MinZ); z <= b.MaxZ; z += step {
			h := height(x, z)
			lake := x > 8 && z > 2 && z < 22
			if lake {
				h = 57
			}
			for y := h - 7; y <= h; y++ {
				name := "stone"
				if y > h-3 {
					name = "dirt"
				}
				if y == h {
					name = "grass_block"
					if lake {
						name = "sand"
					}
				}
				put(x, y, z, name)
			}
			if lake {
				for y := h + 1; y <= 61; y++ {
					put(x, y, z, "water")
				}
			}
			for cx := x - 2; cx <= x+2; cx++ {
				for cz := z - 2; cz <= z+2; cz++ {
					if cx%12 != 0 || cz%13 != 0 || (cx > 8 && cz > 2 && cz < 22) || abs(x-cx)+abs(z-cz) >= 4 {
						continue
					}
					ch := height(cx, cz)
					if x == cx && z == cz {
						for y := ch + 1; y <= ch+5; y++ {
							put(x, y, z, "birch_log")
						}
					}
					for y := ch + 4; y <= ch+6; y++ {
						put(x, y, z, "birch_leaves")
					}
				}
			}
			for bx := (x - 7) >> 2; bx <= (x+7)>>2; bx++ {
				for bz := (z - 7) >> 2; bz <= (z+7)>>2; bz++ {
					for by := 12; by <= 20; by++ {
						biome := "minecraft:birch_forest"
						if bx*4 > 8 {
							biome = "minecraft:swamp"
						}
						v.Biomes[[3]int{bx * 4, by * 4, bz * 4}] = biome
					}
				}
			}
		}
	}
	return v
}
