package service

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/exporter"
	"astra-mwe/internal/mesher"
	"astra-mwe/internal/meshopt"
	"astra-mwe/internal/scene"
	"astra-mwe/internal/selection"
	"astra-mwe/internal/skins"
	"astra-mwe/internal/world"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func needsTiledBuild(b scene.Bounds) bool {
	// Include a halo when considering the loader's occupied-block budget. Keep
	// ordinary player neighborhoods on the faster single-volume path.
	return b.MaxX-b.MinX+1 > 128 || b.MaxZ-b.MinZ+1 > 128 || int64(b.MaxX-b.MinX+15)*int64(b.MaxY-b.MinY+1)*int64(b.MaxZ-b.MinZ+15) > 1_900_000
}

func removeBuildFiles(paths ...string) {
	seen := map[string]bool{}
	for _, p := range paths {
		if p != "" && !seen[p] {
			os.Remove(p)
			seen[p] = true
		}
	}
}

// clearBuildFilesLocked removes service-owned previews, never exported copies.
// The caller holds s.mu so publication and preview-file opening cannot interleave.
func (s *Service) clearBuildFilesLocked() {
	removeBuildFiles(s.previewFile, s.exportFile)
	s.previewFile, s.exportFile = "", ""
}

type tileLoader func(context.Context, scene.Bounds) (*scene.Volume, error)
type chunkVisitor func(context.Context, scene.Bounds, func(scene.Bounds) error) error

func visitRectangle(ctx context.Context, b scene.Bounds, visit func(scene.Bounds) error) error {
	for z := b.MinZ; z <= b.MaxZ; z += 16 {
		for x := b.MinX; x <= b.MaxX; x += 16 {
			if err := ctx.Err(); err != nil {
				return err
			}
			tile := b
			tile.MinX = x
			tile.MaxX = min(x+15, b.MaxX)
			tile.MinZ = z
			tile.MaxZ = min(z+15, b.MaxZ)
			if err := visit(tile); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) buildTiled(ctx context.Context, req BuildRequest, demo bool, r *world.Reader, info scene.WorldInfo) (*Preview, *buildOutput, error) {
	load := func(ctx context.Context, b scene.Bounds) (*scene.Volume, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if demo {
			return DemoVolume(b), nil
		}
		return r.Load(ctx, req.Dimension, b)
	}
	visit := func(ctx context.Context, b scene.Bounds, f func(scene.Bounds) error) error {
		if demo {
			return visitRectangle(ctx, b, f)
		}
		return r.VisitChunks(ctx, req.Dimension, b, f)
	}
	return s.buildTiledWithLoader(ctx, req, demo, info, load, visit)
}

func (s *Service) buildTiledWithLoader(ctx context.Context, req BuildRequest, demo bool, info scene.WorldInfo, load tileLoader, visit chunkVisitor) (result *Preview, output *buildOutput, err error) {
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err = os.MkdirAll(s.cache, 0700); err != nil {
		return nil, nil, err
	}
	bounds := req.Bounds
	// Keep full-height loads and model resolution bounded even in custom dimensions.
	size := 16
	for (size+14)*(size+14)*(bounds.MaxY-bounds.MinY+1) > 1_000_000 {
		size /= 2
	}
	each := func(b scene.Bounds, f func(scene.Bounds) error) error {
		return visit(ctx, b, func(cb scene.Bounds) error {
			for z := cb.MinZ; z <= cb.MaxZ; z += size {
				for x := cb.MinX; x <= cb.MaxX; x += size {
					if err := ctx.Err(); err != nil {
						return err
					}
					tb := cb
					tb.MinX = x
					tb.MaxX = min(x+size-1, cb.MaxX)
					tb.MinZ = z
					tb.MaxZ = min(z+size-1, cb.MaxZ)
					if err := f(tb); err != nil {
						return err
					}
				}
			}
			return nil
		})
	}
	if req.AutoMin {
		s.setProgress("Поиск поверхности по частям", .05)
		low, top, found := bounds.MaxY, bounds.MinY, false
		err = each(bounds, func(tb scene.Bounds) error {
			v, e := load(ctx, tb)
			if e != nil {
				return e
			}
			v.Bounds = tb
			if y, e := selection.AutoMin(v, req.Padding); e == nil {
				low = min(low, y)
				found = true
			}
			for p := range v.Blocks {
				if inside(tb, scene.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}) {
					top = max(top, p[1])
				}
			}
			return ctx.Err()
		})
		if err != nil {
			return nil, nil, err
		}
		if !found {
			return nil, nil, fmt.Errorf("no terrain surface found; set minimum height manually")
		}
		bounds.MinY = max(info.MinY, low)
		for _, p := range info.Players {
			if p.Dimension == req.Dimension && inside(req.Bounds, p.Position) {
				top = max(top, int(math.Floor(p.Position[1]+1.9)))
			}
		}
		bounds.MaxY = min(top, req.Bounds.MaxY)
	}
	paths := append([]string(nil), req.Resources...)
	if demo {
		p, e := DemoAssets(filepath.Join(s.cache, "demo-assets"))
		if e != nil {
			return nil, nil, e
		}
		paths = append([]string{p}, paths...)
	}
	a, err := assets.Open(paths)
	if err != nil {
		return nil, nil, err
	}
	defer a.Close()
	origin := scene.Vec3{float64(bounds.MinX), float64(bounds.MinY), float64(bounds.MinZ)}
	stream, err := exporter.NewStream(s.cache, origin)
	if err != nil {
		return nil, nil, err
	}
	defer stream.Close()
	out := &buildOutput{}
	defer func() {
		if err != nil {
			out.cleanup()
		}
	}()
	warnings := map[string]bool{}
	blocks, before, after, tiles := 0, 0, 0, 0
	err = each(bounds, func(tb scene.Bounds) error {
		halo := max(1, req.BiomeBlend)
		lb := tb
		lb.MinX = max(-30000000, tb.MinX-halo)
		lb.MaxX = min(30000000, tb.MaxX+halo)
		lb.MinZ = max(-30000000, tb.MinZ-halo)
		lb.MaxZ = min(30000000, tb.MaxZ+halo)
		v, e := load(ctx, lb)
		if e != nil {
			return e
		}
		v.Bounds = tb
		for p := range v.Blocks {
			// Preserve interior tile neighbors for culling, but expose selection edges.
			if p[0] < bounds.MinX || p[0] > bounds.MaxX || p[2] < bounds.MinZ || p[2] > bounds.MaxZ {
				delete(v.Blocks, p)
				continue
			}
			if p[0] >= tb.MinX && p[0] <= tb.MaxX && p[2] >= tb.MinZ && p[2] <= tb.MaxZ {
				blocks++
			}
		}
		sc, e := mesher.Build(ctx, v, a, scene.MeshOptions{HollowLeaves: req.HollowLeaves, BiomeColors: req.BiomeColors, BiomeBlend: req.BiomeBlend})
		if e != nil {
			return e
		}
		before += triangleCount(sc)
		if req.OptimizeMesh {
			e = meshopt.Optimize(ctx, sc)
		} else {
			e = meshopt.DeduplicateContext(ctx, sc)
		}
		if e != nil {
			return e
		}
		after += triangleCount(sc)
		for _, w := range sc.Warnings {
			warnings[w] = true
		}
		if e = stream.Add(ctx, sc, true); e != nil {
			return e
		}
		tiles++
		s.setProgress(fmt.Sprintf("Построение GLB · участков: %d", tiles), .3+.5*float64(tiles)/float64(tiles+64))
		return ctx.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	warningList := func() []string {
		list := []string{}
		for w := range warnings {
			list = append(list, w)
		}
		sort.Strings(list)
		return list
	}
	stream.SetWarnings(warningList())
	worldMaterials := stream.MaterialCount()
	if !req.Players {
		out.exportFile, err = stream.Finish(ctx, s.cache)
		if err != nil {
			return nil, nil, err
		}
	}
	players := &scene.Scene{Origin: origin}
	if err = s.addTiledPlayers(ctx, players, req, demo, info, bounds, a); err != nil {
		return nil, nil, err
	}
	for _, w := range players.Warnings {
		warnings[w] = true
	}
	stream.SetWarnings(warningList())
	playerTriangles := triangleCount(players)
	if err = stream.Add(ctx, players, false); err != nil {
		return nil, nil, err
	}
	s.setProgress("Сборка GLB на диске", .9)
	out.previewFile, err = stream.Finish(ctx, s.cache)
	if err != nil {
		return nil, nil, err
	}
	triangles, materials := after, worldMaterials
	if req.Players {
		out.exportFile = out.previewFile
		triangles += playerTriangles
		materials = stream.MaterialCount()
	}
	return &Preview{Bounds: bounds, Blocks: blocks, Triangles: triangles, WorldTrianglesBefore: before, WorldTrianglesAfter: after, Optimized: req.OptimizeMesh, Materials: materials, Warnings: warningList(), Origin: origin, Revision: time.Now().UnixMilli()}, out, nil
}

func (s *Service) addTiledPlayers(ctx context.Context, sc *scene.Scene, req BuildRequest, demo bool, info scene.WorldInfo, bounds scene.Bounds, a *assets.Stack) error {
	fallback, _ := a.Read("assets/minecraft/textures/entity/player/wide/steve.png")
	if len(fallback) == 0 {
		fallback, _ = a.Read("assets/minecraft/textures/entity/steve.png")
	}
	if len(fallback) == 0 {
		fallback = skins.DemoPNG()
		if !demo {
			sc.Warnings = append(sc.Warnings, "Текстура Стива не найдена в JAR; используется демонстрационный скин")
		}
	}
	client := skins.New(filepath.Join(s.cache, "skins"))
	for _, p := range info.Players {
		if p.Dimension != req.Dimension || !inside(bounds, p.Position) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		skin := skins.Skin{PNG: fallback, Fallback: true}
		if !demo {
			skin = client.Get(ctx, p.UUID, fallback)
			if skin.Name != "" {
				p.Name = skin.Name
				s.setPlayerName(p.UUID, skin.Name)
			}
			if skin.Warning != "" {
				sc.Warnings = append(sc.Warnings, skin.Warning)
			}
		}
		skins.AddPlayer(sc, p, skin)
		s.setPlayerSkin(info.Path, p.UUID, skin)
	}
	return nil
}

func copyBuildFile(ctx context.Context, dest, source string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(dest), ".astra-*.glb")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	copyTo := func(w io.Writer) error {
		buf := make([]byte, 1<<20)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, e := in.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					return err
				}
			}
			if e == io.EOF {
				return ctx.Err()
			}
			if e != nil {
				return e
			}
		}
	}
	if err = copyTo(temp); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(name, dest); err == nil {
		return nil
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			os.Remove(dest)
		}
	}()
	if _, err = in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err = copyTo(out); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}
