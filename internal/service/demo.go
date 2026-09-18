package service

import (
	"astra-mwe/internal/scene"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func DemoVolume(b scene.Bounds) *scene.Volume {
	v := &scene.Volume{Bounds: b, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
	put := func(x, y, z int, name string) {
		if inside(b, scene.Vec3{float64(x), float64(y), float64(z)}) {
			v.Blocks[[3]int{x, y, z}] = scene.Block{Name: "minecraft:" + name}
		}
	}
	for x := b.MinX; x <= b.MaxX; x++ {
		for z := b.MinZ; z <= b.MaxZ; z++ {
			height := int(63 + 3*math.Sin(float64(x)*.12) + 2*math.Cos(float64(z)*.16))
			lake := x > 8 && z > 2 && z < 22
			if lake {
				height = 57
			}
			low := b.MinY
			if low < 50 {
				low = 50
			}
			for y := low; y <= height && y <= b.MaxY; y++ {
				name := "stone"
				if y > height-3 {
					name = "dirt"
				}
				if y == height {
					name = "grass_block"
					if lake {
						name = "sand"
					}
				}
				put(x, y, z, name)
			}
			if lake {
				for y := height + 1; y <= 61; y++ {
					put(x, y, z, "water")
				}
			}
			if x%12 == 0 && z%13 == 0 && !lake {
				for y := height + 1; y <= height+5; y++ {
					put(x, y, z, "birch_log")
				}
				for dx := -2; dx <= 2; dx++ {
					for dz := -2; dz <= 2; dz++ {
						for dy := 4; dy <= 6; dy++ {
							if abs(dx)+abs(dz) < 4 {
								put(x+dx, height+dy, z+dz, "birch_leaves")
							}
						}
					}
				}
			}
			bx, bz := int(math.Floor(float64(x)/4))*4, int(math.Floor(float64(z)/4))*4
			for y := low; y <= b.MaxY; y += 4 {
				by := int(math.Floor(float64(y)/4)) * 4
				biome := "minecraft:birch_forest"
				if x > 8 {
					biome = "minecraft:swamp"
				}
				v.Biomes[[3]int{bx, by, bz}] = biome
			}
		}
	}
	return v
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func DemoAssets(root string) (string, error) {
	colors := map[string]color.NRGBA{"stone": {120, 126, 130, 255}, "dirt": {124, 87, 62, 255}, "grass_block": {167, 184, 133, 255}, "sand": {214, 202, 155, 255}, "water": {200, 219, 232, 155}, "birch_log": {213, 211, 193, 255}, "birch_leaves": {159, 182, 136, 255}}
	write := func(path string, data []byte) error {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0600)
	}
	parent := map[string]any{"textures": map[string]string{"all": "minecraft:block/stone"}, "elements": []any{map[string]any{"from": []int{0, 0, 0}, "to": []int{16, 16, 16}, "faces": map[string]any{}}}}
	faces := parent["elements"].([]any)[0].(map[string]any)["faces"].(map[string]any)
	for _, side := range []string{"north", "south", "east", "west", "up", "down"} {
		faces[side] = map[string]any{"texture": "#all", "cullface": side}
	}
	data, _ := json.Marshal(parent)
	if err := write("assets/minecraft/models/block/cube_all.json", data); err != nil {
		return "", err
	}
	for name, c := range colors {
		state, _ := json.Marshal(map[string]any{"variants": map[string]any{"": map[string]string{"model": "minecraft:block/" + name}}})
		if err := write("assets/minecraft/blockstates/"+name+".json", state); err != nil {
			return "", err
		}
		model := map[string]any{"parent": "minecraft:block/cube_all", "textures": map[string]string{"all": "minecraft:block/" + name}}
		if name == "grass_block" || name == "birch_leaves" || name == "water" {
			var clone map[string]any
			json.Unmarshal(data, &clone)
			clone["textures"] = model["textures"]
			f := clone["elements"].([]any)[0].(map[string]any)["faces"].(map[string]any)
			for _, face := range f {
				face.(map[string]any)["tintindex"] = 0
			}
			model = clone
		}
		raw, _ := json.Marshal(model)
		if err := write("assets/minecraft/models/block/"+name+".json", raw); err != nil {
			return "", err
		}
		im := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				q := c
				delta := int((x*17+y*31+x*y*7)%29) - 14
				q.R = uint8(max(0, min(255, int(c.R)+delta)))
				q.G = uint8(max(0, min(255, int(c.G)+delta)))
				q.B = uint8(max(0, min(255, int(c.B)+delta)))
				if name == "birch_leaves" && (x*3+y*7)%7 == 0 {
					q.A = 0
				}
				if name == "birch_log" && ((y%5 == 0 && x%7 < 4) || (y%7 == 0 && x > 10)) {
					q = color.NRGBA{54, 51, 48, 255}
				}
				im.SetNRGBA(x, y, q)
			}
		}
		path := filepath.Join(root, "assets/minecraft/textures/block/"+name+".png")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		f, err := os.Create(path)
		if err != nil {
			return "", err
		}
		err = png.Encode(f, im)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	waterPath := filepath.Join(root, "assets/minecraft/textures/block/water.png")
	water, err := os.ReadFile(waterPath)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"water_still", "water_flow"} {
		if err := write("assets/minecraft/textures/block/"+name+".png", water); err != nil {
			return "", err
		}
	}
	for _, name := range []string{"grass", "foliage"} {
		im := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				im.SetNRGBA(x, y, color.NRGBA{uint8(80 + x/3), uint8(135 + (255-y)/4), uint8(55 + y/5), 255})
			}
		}
		p := filepath.Join(root, "assets/minecraft/textures/colormap/"+name+".png")
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return "", err
		}
		f, err := os.Create(p)
		if err != nil {
			return "", err
		}
		err = png.Encode(f, im)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return root, nil
}
