package mesher

import (
	"astra-mwe/internal/scene"
	"math"
	"strconv"
)

// Corner order matches the upward quad: northwest, southwest, southeast,
// northeast. Heights and flow follow Java's FluidRenderer and FlowingFluid.
type fluidSurface struct {
	heights [4]float64
	flow    [2]float64
}

func fluidKind(block scene.Block) string {
	ns, name := splitID(block.Name)
	if ns != "minecraft" {
		return ""
	}
	switch name {
	case "water", "bubble_column":
		return "water"
	case "lava":
		return "lava"
	}
	return ""
}

func fluidOwnHeight(block scene.Block) float64 {
	level, _ := strconv.Atoi(block.Properties["level"])
	// Levels 8..15 are falling fluid with amount 8. Only a matching fluid
	// above raises the rendered height to one full block.
	if level <= 0 || level >= 8 {
		return 8.0 / 9
	}
	return float64(8-level) / 9
}

func fluidModel(kind string, block scene.Block) *model {
	m := cubeModel("minecraft:block/"+kind+"_still", kind == "water")
	m.Elements[0].To[1] = fluidOwnHeight(block) * 16
	return m
}

func (b *builder) fluidHeight(p [3]int, kind string) float64 {
	block := b.volume.Blocks[p]
	if fluidKind(block) == kind {
		if fluidKind(b.volume.Blocks[[3]int{p[0], p[1] + 1, p[2]}]) == kind {
			return 1
		}
		return fluidOwnHeight(block)
	}
	// Model coverage also respects full solid cubes supplied by resource packs
	// and mods. Non-full models cannot be assumed to occupy a fluid corner.
	if b.instanceAt(p).fullCube {
		return -1
	}
	return 0
}

func (b *builder) fluidSurface(p [3]int, kind string) fluidSurface {
	s := fluidSurface{flow: b.fluidFlow(p, kind)}
	h := b.fluidHeight(p, kind)
	if h >= 1 {
		s.heights = [4]float64{1, 1, 1, 1}
		return s
	}
	for i, off := range [4][2]int{{-1, -1}, {-1, 1}, {1, 1}, {1, -1}} {
		a := b.fluidHeight([3]int{p[0] + off[0], p[1], p[2]}, kind)
		c := b.fluidHeight([3]int{p[0], p[1], p[2] + off[1]}, kind)
		if a >= 1 || c >= 1 {
			s.heights[i] = 1
			continue
		}
		sum, weight := 0.0, 0.0
		add := func(v float64) {
			if v >= .8 {
				sum += v * 10
				weight += 10
			} else if v >= 0 {
				sum += v
				weight++
			}
		}
		if a > 0 || c > 0 {
			d := b.fluidHeight([3]int{p[0] + off[0], p[1], p[2] + off[1]}, kind)
			if d >= 1 {
				s.heights[i] = 1
				continue
			}
			add(d)
		}
		add(h)
		add(a)
		add(c)
		s.heights[i] = sum / weight
	}
	return s
}

func (b *builder) fluidFlow(p [3]int, kind string) [2]float64 {
	var flow [2]float64
	h := fluidOwnHeight(b.volume.Blocks[p])
	for _, off := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		np := [3]int{p[0] + off[0], p[1], p[2] + off[1]}
		neighbor := b.volume.Blocks[np]
		nk := fluidKind(neighbor)
		if nk != "" && nk != kind {
			continue
		}
		delta := 0.0
		if nk == kind {
			delta = h - fluidOwnHeight(neighbor)
		} else if !b.instanceAt(np).fullCube {
			below := b.volume.Blocks[[3]int{np[0], np[1] - 1, np[2]}]
			if fluidKind(below) == kind {
				delta = h - (fluidOwnHeight(below) - 8.0/9)
			}
		}
		flow[0] += float64(off[0]) * delta
		flow[1] += float64(off[1]) * delta
	}
	return flow
}

func (s fluidSurface) corners(dir string) [4]point {
	q := corners(element{To: [3]float64{16, 16, 16}}, dir)
	for i, p := range q {
		if p[1] == 0 {
			continue
		}
		corner := 0
		if p[0] == 0 && p[2] == 1 {
			corner = 1
		} else if p[0] == 1 && p[2] == 1 {
			corner = 2
		} else if p[0] == 1 {
			corner = 3
		}
		q[i][1] = s.heights[corner]
	}
	return q
}

func (s fluidSurface) texture(kind, dir string) string {
	frame := "still"
	if (dir != "up" && dir != "down") || (dir == "up" && s.flow != ([2]float64{})) {
		frame = "flow"
	}
	return "minecraft:block/" + kind + "_" + frame
}

func (s fluidSurface) uv(dir string, q [4]point) [4][2]float64 {
	if dir == "up" && s.flow != ([2]float64{}) {
		angle := math.Atan2(s.flow[1], s.flow[0]) - math.Pi/2
		sn, cs := math.Sin(angle)*.25, math.Cos(angle)*.25
		return [4][2]float64{{.5 - cs - sn, .5 - cs + sn}, {.5 - cs + sn, .5 + cs + sn}, {.5 + cs + sn, .5 + cs - sn}, {.5 + cs - sn, .5 - cs - sn}}
	}
	if dir == "up" || dir == "down" {
		return faceUV(element{To: [3]float64{16, 16, 16}}, dir, face{}, variant{})
	}
	uv := [4][2]float64{{.5, .5}, {0, .5}, {0, 0}, {.5, 0}}
	for i := range uv {
		uv[i][1] = (1 - q[i][1]) * .5
	}
	return uv
}
