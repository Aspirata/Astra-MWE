package world

import (
	"astra-mwe/internal/scene"
	"context"
	"errors"
	"fmt"
	"math/bits"
)

func firstSurfaceSample(minimum, step int) int {
	remainder := minimum % step
	if remainder < 0 {
		remainder += step
	}
	if remainder != 0 {
		minimum += step - remainder
	}
	return minimum
}

// Keep compact palette indices until all neighboring sampled column heights
// are known. Expanding every quart biome into Volume.Biomes would overwhelm
// the memory savings from sampling a distant map tile.
type surfaceBiomeSection struct {
	names   []string
	indices [64]uint16
}

type surfaceBiomeStore struct {
	sections map[[3]int]surfaceBiomeSection
	legacy   map[[2]int][]int32
}

func newSurfaceBiomeStore() *surfaceBiomeStore {
	return &surfaceBiomeStore{sections: map[[3]int]surfaceBiomeSection{}, legacy: map[[2]int][]int32{}}
}

func (s *surfaceBiomeStore) addSection(cx, sy, cz int, names []string, indices []int) {
	section := surfaceBiomeSection{names: names}
	for i, index := range indices {
		section.indices[i] = uint16(index)
	}
	s.sections[[3]int{cx, sy, cz}] = section
}

func (s *surfaceBiomeStore) addLegacy(raw any, cx, cz int) error {
	if raw == nil {
		return nil
	}
	var ids []int32
	switch a := raw.(type) {
	case []int32:
		ids = a
	case []byte:
		ids = make([]int32, len(a))
		for i, id := range a {
			ids[i] = int32(id)
		}
	default:
		return errors.New("legacy Biomes is not a byte/int array")
	}
	if len(ids) != 256 && len(ids) != 1024 {
		return fmt.Errorf("legacy Biomes length %d is unsupported", len(ids))
	}
	s.legacy[[2]int{cx, cz}] = ids
	return nil
}

func (s *surfaceBiomeStore) at(x, y, z int) string {
	if section, ok := s.sections[[3]int{x >> 4, y >> 4, z >> 4}]; ok {
		i := ((y&15)>>2)*16 + ((z&15)>>2)*4 + ((x & 15) >> 2)
		return section.names[section.indices[i]]
	}
	if ids := s.legacy[[2]int{x >> 4, z >> 4}]; len(ids) == 256 {
		return biomeName(ids[(z&12)*16+(x&12)])
	} else if len(ids) == 1024 && y >= 0 && y < 256 {
		return biomeName(ids[(y>>2)*16+((z&15)>>2)*4+((x&15)>>2)])
	}
	return ""
}

func (s *surfaceBiomeStore) materialize(ctx context.Context, v *scene.Volume) error {
	for p := range v.Blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		y := (p[1] >> 2) * 4
		for z := ((p[2] - 7) >> 2) * 4; z <= p[2]+7; z += 4 {
			for x := ((p[0] - 7) >> 2) * 4; x <= p[0]+7; x += 4 {
				if x+3 < v.Bounds.MinX || x > v.Bounds.MaxX || z+3 < v.Bounds.MinZ || z > v.Bounds.MaxZ {
					continue
				}
				key := [3]int{x, y, z}
				if _, exists := v.Biomes[key]; exists {
					continue
				}
				if name := s.at(x, y, z); name != "" {
					v.Biomes[key] = name
				}
			}
		}
	}
	return nil
}

func isAir(b scene.Block) bool {
	return b.Name == "minecraft:air" || b.Name == "minecraft:cave_air" || b.Name == "minecraft:void_air"
}

// packedSurface accesses requested column entries without allocating a
// 4096-element index array per section, including discarded underground layers.
type packedSurface struct {
	words      []int64
	width, per int
	mask       uint64
	padded     bool
}

func surfacePacked(words []int64, palette int, padded bool) (packedSurface, error) {
	p := packedSurface{words: words, padded: padded}
	if palette < 1 || palette > 65536 {
		return p, fmt.Errorf("invalid palette size %d", palette)
	}
	if palette == 1 && len(words) == 0 {
		return p, nil
	}
	p.width = max(4, bits.Len(uint(palette-1)))
	p.per = 64 / p.width
	p.mask = uint64(1<<p.width) - 1
	required := (4096*p.width + 63) / 64
	if padded {
		required = (4096 + p.per - 1) / p.per
	}
	if len(words) != required {
		return p, fmt.Errorf("packed palette has %d longs, expected %d", len(words), required)
	}
	// Validate even unretained blocks so corrupt underground data is never
	// silently accepted. A full-width palette makes every packed value valid.
	if palette != 1<<p.width {
		for i := 0; i < 4096; i++ {
			if index := p.at(i); index >= palette {
				return p, fmt.Errorf("palette index %d exceeds size %d", index, palette)
			}
		}
	}
	return p, nil
}

func (p packedSurface) at(i int) int {
	if len(p.words) == 0 {
		return 0
	}
	var value uint64
	if p.padded {
		value = uint64(p.words[i/p.per]) >> uint(i%p.per*p.width)
	} else {
		bit := i * p.width
		word, shift := bit/64, bit%64
		value = uint64(p.words[word]) >> uint(shift)
		if shift+p.width > 64 {
			value |= uint64(p.words[word+1]) << uint(64-shift)
		}
	}
	return int(value & p.mask)
}
