package service

import (
	"astra-mwe/internal/exporter"
	"astra-mwe/internal/mesher"
	"astra-mwe/internal/meshopt"
	"astra-mwe/internal/scene"
	"astra-mwe/internal/skins"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

const ViewTileSize = 32

type ViewRequest struct {
	World        string   `json:"world"`
	Dimension    string   `json:"dimension"`
	Resources    []string `json:"resources"`
	HollowLeaves bool     `json:"hollowLeaves"`
	X            int      `json:"x"`
	Z            int      `json:"z"`
}
type ViewTile struct {
	Data   []byte
	Origin scene.Vec3
}

// ViewTile reads one bounded neighborhood independently of the export pipeline.
// It never assigns s.data, selection bounds, or the export's busy/progress state.
func (s *Service) ViewTile(parent context.Context, req ViewRequest) (*ViewTile, error) {
	if req.X < -937500 || req.X > 937499 || req.Z < -937500 || req.Z > 937499 {
		return nil, fmt.Errorf("участок за границами Minecraft")
	}
	if len(req.Resources) > 128 {
		return nil, fmt.Errorf("слишком много пакетов ресурсов")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := context.AfterFunc(s.viewContext, cancel)
	defer stop()
	worker, releaseWorker, err := s.acquireViewWorker(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseWorker()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.state.World == nil || req.World != s.state.World.Path {
		s.mu.Unlock()
		return nil, fmt.Errorf("мир просмотра изменился")
	}
	info := *s.state.World
	info.Players = append([]scene.Player(nil), info.Players...)
	demo, r, revision := s.demo, s.reader, s.viewRevision
	s.mu.Unlock()
	keyJSON, _ := json.Marshal(req)
	cacheKey := fmt.Sprintf("3d:%d:%s:%s", revision, keyJSON, resourceStamp(req.Resources))
	if tile := s.viewCache.get(cacheKey); tile != nil {
		return tile, nil
	}
	if req.Dimension == "" {
		req.Dimension = "minecraft:overworld"
	}
	dimMin, dimMax := info.MinY, info.MaxY
	if r != nil {
		dimMin, dimMax = r.DimensionBounds(req.Dimension)
	}
	bounds := scene.Bounds{MinX: req.X * ViewTileSize, MaxX: (req.X+1)*ViewTileSize - 1, MinZ: req.Z * ViewTileSize, MaxZ: (req.Z+1)*ViewTileSize - 1, MinY: dimMin, MaxY: dimMax}
	load := bounds
	load.MinX = max(-30000000, load.MinX-7)
	load.MaxX = min(30000000, load.MaxX+7)
	load.MinZ = max(-30000000, load.MinZ-7)
	load.MaxZ = min(30000000, load.MaxZ+7)
	var v *scene.Volume
	if demo {
		v = DemoVolume(load)
	} else if r != nil {
		v, err = r.Load(ctx, req.Dimension, load)
	} else {
		return nil, fmt.Errorf("мир не открыт")
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	origin := scene.Vec3{float64(bounds.MinX), float64(bounds.MinY), float64(bounds.MinZ)}
	populated := false
	for p := range v.Blocks {
		if p[0] >= bounds.MinX && p[0] <= bounds.MaxX && p[2] >= bounds.MinZ && p[2] <= bounds.MaxZ {
			populated = true
			break
		}
	}
	hasPlayer := false
	for _, p := range info.Players {
		if p.Dimension == req.Dimension && inside(bounds, p.Position) {
			hasPlayer = true
		}
	}
	if !populated && !hasPlayer {
		tile := &ViewTile{Origin: origin}
		s.viewCache.put(cacheKey, tile)
		return tile, nil
	}
	v.Bounds = bounds
	paths := append([]string(nil), req.Resources...)
	if demo {
		p, e := s.viewCache.demoAssets(filepath.Join(s.cache, "view-demo-assets"))
		if e != nil {
			return nil, e
		}
		paths = append([]string{p}, paths...)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("выберите клиентский JAR для просмотра")
	}
	a, _, releaseResources, err := s.viewCache.acquire(paths, revision, worker)
	if err != nil {
		return nil, err
	}
	defer releaseResources()
	sc, err := mesher.Build(ctx, v, a, scene.MeshOptions{BiomeColors: true, BiomeBlend: 7, HollowLeaves: req.HollowLeaves})
	if err != nil {
		return nil, err
	}
	// Preview geometry is disposable and can always merge visually identical faces.
	if err = meshopt.Optimize(ctx, sc); err != nil {
		return nil, err
	}
	fallback, _ := a.Read("assets/minecraft/textures/entity/player/wide/steve.png")
	if len(fallback) == 0 {
		fallback, _ = a.Read("assets/minecraft/textures/entity/steve.png")
	}
	if len(fallback) == 0 {
		fallback = skins.DemoPNG()
	}
	client := skins.New(filepath.Join(s.cache, "skins"))
	for _, p := range info.Players {
		if p.Dimension != req.Dimension || !inside(bounds, p.Position) {
			continue
		}
		skin := skins.Skin{PNG: fallback, Fallback: true}
		if !demo {
			skin = client.Get(ctx, p.UUID, fallback)
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if skin.Name != "" {
			p.Name = skin.Name
		}
		skins.AddPlayer(sc, p, skin)
		s.setPlayerSkin(info.Path, p.UUID, skin)
	}
	if triangleCount(sc) == 0 {
		return &ViewTile{Origin: origin}, nil
	}
	data, err := exporter.GLB(sc)
	if err != nil {
		return nil, err
	}
	tile := &ViewTile{Data: data, Origin: sc.Origin}
	s.viewCache.put(cacheKey, tile)
	return tile, nil
}
