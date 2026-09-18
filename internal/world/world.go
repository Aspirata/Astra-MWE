// Package world provides bounded, read-only Java Edition Anvil save access.
package world

import (
	"astra-mwe/internal/nbt"
	"astra-mwe/internal/scene"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

type Reader struct {
	Info       scene.WorldInfo
	dimensions map[string]string
	bounds     map[string][2]int
	closed     atomic.Bool
}

func Open(path string) (*Reader, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Base(path), "level.dat") {
		path = filepath.Dir(path)
	}
	root, err := readGzipNBT(filepath.Join(path, "level.dat"))
	if err != nil {
		return nil, fmt.Errorf("open level.dat: %w", err)
	}
	data := compound(root["Data"])
	if data == nil {
		return nil, errors.New("level.dat has no Data compound")
	}
	version := integer(data["DataVersion"])
	if version < 1519 {
		return nil, fmt.Errorf("world DataVersion %d is older than supported Java 1.13", version)
	}
	r := &Reader{Info: scene.WorldInfo{Path: path, Name: stringValue(data["LevelName"]), DataVersion: version, Version: stringValue(compound(data["Version"])["Name"]), MinY: 0, MaxY: 255}, dimensions: map[string]string{"minecraft:overworld": path}}
	if version >= 2825 {
		r.Info.MinY = -64
		r.Info.MaxY = 319
	}
	r.bounds = map[string][2]int{}
	for name, raw := range compound(compound(data["WorldGenSettings"])["dimensions"]) {
		typeData := compound(compound(raw)["type"])
		if typeData != nil {
			min, height := integer(typeData["min_y"]), integer(typeData["height"])
			if height > 0 && height <= 4096 && min >= -2048 && min+height <= 2048 {
				r.bounds[name] = [2]int{min, min + height - 1}
			}
		} else {
			switch stringValue(compound(raw)["type"]) {
			case "minecraft:the_nether", "minecraft:the_end":
				r.bounds[name] = [2]int{0, 255}
			}
		}
	}
	r.Info.Spawn = scene.Vec3{float64(integer(data["SpawnX"])), float64(integer(data["SpawnY"])), float64(integer(data["SpawnZ"]))}
	r.Info.SpawnDimension = "minecraft:overworld"
	if spawn := compound(data["spawn"]); spawn != nil {
		if pos, ok := spawn["pos"].([]int32); ok && len(pos) == 3 {
			for i := range pos {
				r.Info.Spawn[i] = float64(pos[i])
			}
		} else {
			return nil, errors.New("spawn.pos is not a three-element int array")
		}
		if d := stringValue(spawn["dimension"]); d != "" {
			r.Info.SpawnDimension = d
		}
	}
	for _, d := range [][2]string{{"minecraft:the_nether", "DIM-1"}, {"minecraft:the_end", "DIM1"}} {
		p := filepath.Join(path, d[1])
		if s, e := os.Stat(p); e == nil && s.IsDir() {
			r.dimensions[d[0]] = p
		}
	}
	custom := filepath.Join(path, "dimensions")
	if s, e := os.Stat(custom); e == nil && s.IsDir() {
		err = filepath.WalkDir(custom, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				return nil
			}
			if d.Name() != "region" {
				return nil
			}
			rel, e := filepath.Rel(custom, filepath.Dir(p))
			if e != nil {
				return e
			}
			parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
			if len(parts) == 2 {
				r.dimensions[parts[0]+":"+parts[1]] = filepath.Dir(p)
			}
			return filepath.SkipDir
		})
		if err != nil {
			return nil, fmt.Errorf("discover dimensions: %w", err)
		}
	}
	for name := range r.dimensions {
		r.Info.Dimensions = append(r.Info.Dimensions, name)
	}
	sort.Strings(r.Info.Dimensions)
	if err = r.readPlayers(data); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Reader) Close() error { r.closed.Store(true); return nil }

// DimensionBounds returns inclusive build bounds. Custom dimension types that
// are not embedded in level.dat fall back to the world's overworld bounds.
func (r *Reader) DimensionBounds(d string) (int, int) {
	if b, ok := r.bounds[d]; ok {
		return b[0], b[1]
	}
	if d == "minecraft:the_nether" || d == "minecraft:the_end" {
		return 0, 255
	}
	return r.Info.MinY, r.Info.MaxY
}

// Load reads only intersecting chunks and omits air. Every malformed present
// chunk aborts the load with coordinates; missing chunks are empty terrain.
func (r *Reader) Load(ctx context.Context, dimension string, b scene.Bounds) (*scene.Volume, error) {
	return r.loadWithLimit(ctx, dimension, b, 2_000_000)
}

func (r *Reader) loadWithLimit(ctx context.Context, dimension string, b scene.Bounds, maxBlocks int) (*scene.Volume, error) {
	return r.load(ctx, dimension, b, maxBlocks, 0)
}

// LoadSurface reads the eight highest non-air blocks in each requested column.
// Include the biome-blending halo in bounds. Missing chunks remain empty and
// intersecting biome samples retain their original heights.
func (r *Reader) LoadSurface(ctx context.Context, dimension string, b scene.Bounds) (*scene.Volume, error) {
	return r.LoadSurfaceSampled(ctx, dimension, b, 1)
}

// LoadSurfaceSampled retains only columns on the global x/z multiples-of-step
// grid, including at negative coordinates. Step is 1..16. Callers include a
// seven-block halo in bounds for biome blending. For step > 1 only biome cells
// needed at retained block heights and within that halo are materialized.
func (r *Reader) LoadSurfaceSampled(ctx context.Context, dimension string, b scene.Bounds, step int) (*scene.Volume, error) {
	if step < 1 || step > 16 {
		return nil, errors.New("map sample step must be between 1 and 16")
	}
	return r.load(ctx, dimension, b, 8*262144, step)
}

func (r *Reader) load(ctx context.Context, dimension string, b scene.Bounds, maxBlocks int, step int) (*scene.Volume, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.closed.Load() {
		return nil, errors.New("world is closed")
	}
	dir, ok := r.dimensions[dimension]
	if !ok {
		return nil, fmt.Errorf("unknown dimension %q", dimension)
	}
	validate := validateBounds
	if step > 0 {
		validate = func(b scene.Bounds) error { return validateSurfaceBounds(b, step) }
	}
	if err := validate(b); err != nil {
		return nil, err
	}
	v := &scene.Volume{Bounds: b, Blocks: map[[3]int]scene.Block{}, Biomes: map[[3]int]string{}}
	var biomes *surfaceBiomeStore
	if step > 1 {
		biomes = newSurfaceBiomeStore()
	}
	for _, p := range r.Info.Players {
		if p.Dimension == dimension {
			v.Players = append(v.Players, p)
		}
	}
	for cz := b.MinZ >> 4; cz <= b.MaxZ>>4; cz++ {
		for cx := b.MinX >> 4; cx <= b.MaxX>>4; cx++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			root, err := readChunk(dir, cx, cz)
			if err != nil {
				return nil, fmt.Errorf("chunk (%d,%d): %w", cx, cz, err)
			}
			if root == nil {
				continue
			}
			if err = decodeChunkSampled(ctx, root, cx, cz, r.Info.DataVersion, v, step, biomes); err != nil {
				return nil, fmt.Errorf("chunk (%d,%d): %w", cx, cz, err)
			}
			if len(v.Blocks) > maxBlocks {
				return nil, fmt.Errorf("selection exceeds %d occupied block memory budget; select a smaller region", maxBlocks)
			}
		}
	}
	if biomes != nil {
		if err := biomes.materialize(ctx, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}
func validateBounds(b scene.Bounds) error {
	if err := validateCoordinates(b); err != nil {
		return err
	}
	if int64(b.MaxX-b.MinX+1)*int64(b.MaxY-b.MinY+1)*int64(b.MaxZ-b.MinZ+1) > 16<<20 {
		return errors.New("selection exceeds 16 million block memory limit; select a smaller region")
	}
	return nil
}

func validateSurfaceBounds(b scene.Bounds, step int) error {
	if err := validateCoordinates(b); err != nil {
		return err
	}
	if b.MaxX-b.MinX+1 > 2048 || b.MaxZ-b.MinZ+1 > 2048 {
		return errors.New("map extent exceeds 2048 blocks; select a smaller region")
	}
	columnsX := (b.MaxX - firstSurfaceSample(b.MinX, step) + step) / step
	columnsZ := (b.MaxZ - firstSurfaceSample(b.MinZ, step) + step) / step
	if int64(columnsX)*int64(columnsZ) > 262144 {
		return errors.New("map exceeds 262144 column memory limit; select a smaller region")
	}
	return nil
}

func validateCoordinates(b scene.Bounds) error {
	if b.MinX > b.MaxX || b.MinY > b.MaxY || b.MinZ > b.MaxZ {
		return errors.New("selection minimum exceeds maximum")
	}
	if b.MinX < -30000000 || b.MaxX > 30000000 || b.MinZ < -30000000 || b.MaxZ > 30000000 || b.MinY < -2048 || b.MaxY > 2047 {
		return errors.New("selection exceeds supported world coordinates")
	}
	return nil
}
func readGzipNBT(path string) (nbt.Compound, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	z, e := gzip.NewReader(f)
	if e != nil {
		return nil, e
	}
	defer z.Close()
	return nbt.Decode(z)
}
func compound(v any) nbt.Compound { m, _ := v.(map[string]any); return m }
func list(v any) []any            { l, _ := v.([]any); return l }
func stringValue(v any) string    { s, _ := v.(string); return s }
func integer(v any) int {
	switch n := v.(type) {
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return float64(integer(v))
	}
}

func (r *Reader) readPlayers(data nbt.Compound) error {
	players := map[string]scene.Player{}
	var order []string
	add := func(c nbt.Compound, id string) error {
		p, ok := player(c, id)
		if !ok {
			return errors.New("saved player has invalid position")
		}
		key := p.UUID
		if key == "" {
			key = "local-player"
		}
		if _, exists := players[key]; exists {
			return nil
		}
		players[key] = p
		order = append(order, key)
		return nil
	}
	if c := compound(data["Player"]); c != nil {
		if err := add(c, ""); err != nil {
			return fmt.Errorf("level.dat Player: %w", err)
		}
	}
	var files []string
	for _, sub := range []string{"players/data", "playerdata"} {
		found, err := filepath.Glob(filepath.Join(r.Info.Path, sub, "*.dat"))
		if err != nil {
			return err
		}
		files = append(files, found...)
	}
	primary := uuidValue(data["singleplayer_uuid"])
	if len(files) > 10000 {
		return errors.New("playerdata exceeds 10000 player limit")
	}
	modified := make(map[string]time.Time, len(files))
	for _, f := range files {
		stat, err := os.Stat(f)
		if err != nil {
			return fmt.Errorf("player %s: %w", filepath.Base(f), err)
		}
		modified[f] = stat.ModTime()
	}
	sort.SliceStable(files, func(i, j int) bool {
		iPrimary := primary != "" && strings.EqualFold(filepath.Base(files[i]), primary+".dat")
		jPrimary := primary != "" && strings.EqualFold(filepath.Base(files[j]), primary+".dat")
		if iPrimary != jPrimary {
			return iPrimary
		}
		return modified[files[i]].After(modified[files[j]])
	})
	for _, f := range files {
		c, e := readGzipNBT(f)
		if e != nil {
			return fmt.Errorf("player %s: %w", filepath.Base(f), e)
		}
		if e = add(c, strings.TrimSuffix(filepath.Base(f), ".dat")); e != nil {
			return fmt.Errorf("player %s: %w", filepath.Base(f), e)
		}
	}
	names := map[string]string{}
	cachePaths := []string{filepath.Join(filepath.Dir(r.Info.Path), "usercache.json"), filepath.Join(r.Info.Path, "usercache.json")}
	if strings.EqualFold(filepath.Base(filepath.Dir(r.Info.Path)), "saves") {
		cachePaths = append([]string{filepath.Join(filepath.Dir(filepath.Dir(r.Info.Path)), "usercache.json")}, cachePaths...)
	}
	for _, p := range cachePaths {
		f, e := os.Open(p)
		if e != nil {
			continue
		}
		var entries []struct{ Name, UUID string }
		e = json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&entries)
		f.Close()
		if e == nil {
			for _, entry := range entries {
				names[strings.ToLower(entry.UUID)] = entry.Name
			}
		}
	}
	for _, id := range order {
		p := players[id]
		if name := names[strings.ToLower(p.UUID)]; name != "" {
			p.Name = name
		}
		if p.Name == "" {
			if p.UUID != "" {
				p.Name = p.UUID
			} else {
				p.Name = "Local player"
			}
		}
		r.Info.Players = append(r.Info.Players, p)
	}
	return nil
}
func player(c nbt.Compound, fallback string) (scene.Player, bool) {
	p := scene.Player{UUID: strings.ToLower(fallback), Dimension: "minecraft:overworld"}
	pos := list(c["Pos"])
	if len(pos) != 3 {
		return p, false
	}
	for i := range p.Position {
		switch pos[i].(type) {
		case float64, float32, int8, int16, int32, int64:
		default:
			return p, false
		}
		p.Position[i] = number(pos[i])
		if math.IsNaN(p.Position[i]) || math.IsInf(p.Position[i], 0) {
			return p, false
		}
	}
	rot := list(c["Rotation"])
	if len(rot) == 2 {
		for i := range p.Rotation {
			p.Rotation[i] = number(rot[i])
			if math.IsNaN(p.Rotation[i]) || math.IsInf(p.Rotation[i], 0) {
				p.Rotation[i] = 0
			}
		}
	}
	switch d := c["Dimension"].(type) {
	case string:
		p.Dimension = d
	case int32:
		switch d {
		case -1:
			p.Dimension = "minecraft:the_nether"
		case 1:
			p.Dimension = "minecraft:the_end"
		}
	}
	if a, ok := c["UUID"].([]int32); ok && len(a) == 4 {
		hex := fmt.Sprintf("%08x%08x%08x%08x", uint32(a[0]), uint32(a[1]), uint32(a[2]), uint32(a[3]))
		p.UUID = hex[:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:]
	} else if most, ok := c["UUIDMost"].(int64); ok {
		least, _ := c["UUIDLeast"].(int64)
		hex := fmt.Sprintf("%016x%016x", uint64(most), uint64(least))
		p.UUID = hex[:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:]
	}
	p.Name = stringValue(c["Name"])
	return p, true
}

func uuidValue(value any) string {
	if s, ok := value.(string); ok {
		return strings.ToLower(s)
	}
	if a, ok := value.([]int32); ok && len(a) == 4 {
		h := fmt.Sprintf("%08x%08x%08x%08x", uint32(a[0]), uint32(a[1]), uint32(a[2]), uint32(a[3]))
		return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	}
	return ""
}
