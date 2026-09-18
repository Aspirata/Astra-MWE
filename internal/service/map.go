package service

import (
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"path/filepath"
)

const MapTilePixels = 64

type MapRequest struct {
	World     string   `json:"world"`
	Dimension string   `json:"dimension"`
	Resources []string `json:"resources"`
	X         int      `json:"x"`
	Z         int      `json:"z"`
	Step      int      `json:"step"`
	Detail    int      `json:"detail"`
}

func (s *Service) MapTile(parent context.Context, req MapRequest) (*ViewTile, error) {
	if req.Detail == 0 {
		req.Detail = 1
	}
	if req.Detail != 1 && req.Detail != 2 && req.Detail != 4 && req.Detail != 8 && req.Detail != 16 {
		return nil, fmt.Errorf("недопустимая детализация карты")
	}
	if req.Step > 1 {
		req.Detail = 1
	}
	if req.Step != 1 && req.Step != 2 && req.Step != 4 && req.Step != 8 && req.Step != 16 {
		return nil, fmt.Errorf("недопустимый масштаб карты")
	}
	size := MapTilePixels * req.Step
	if req.X < -30000000/size-1 || req.X > 30000000/size || req.Z < -30000000/size-1 || req.Z > 30000000/size || len(req.Resources) > 128 {
		return nil, fmt.Errorf("участок за границами карты")
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
	demo, r, revision := s.demo, s.reader, s.viewRevision
	s.mu.Unlock()
	if req.Dimension == "" {
		req.Dimension = "minecraft:overworld"
	}
	raw, _ := json.Marshal(req)
	key := fmt.Sprintf("map:%d:%s:%s", revision, raw, resourceStamp(req.Resources))
	if tile := s.viewCache.get(key); tile != nil {
		return tile, nil
	}
	minY, maxY := info.MinY, info.MaxY
	if r != nil {
		minY, maxY = r.DimensionBounds(req.Dimension)
	}
	b := scene.Bounds{MinX: req.X * size, MaxX: (req.X+1)*size - 1, MinZ: req.Z * size, MaxZ: (req.Z+1)*size - 1, MinY: minY, MaxY: maxY}
	load := b
	load.MinX = max(-30000000, load.MinX-7)
	load.MaxX = min(30000000, load.MaxX+7)
	load.MinZ = max(-30000000, load.MinZ-7)
	load.MaxZ = min(30000000, load.MaxZ+7)
	var v *scene.Volume
	if demo {
		v = DemoSurface(load, req.Step)
	} else if r != nil {
		v, err = r.LoadSurfaceSampled(ctx, req.Dimension, load, req.Step)
	} else {
		return nil, fmt.Errorf("мир не открыт")
	}
	if err != nil {
		return nil, err
	}
	tile := &ViewTile{Origin: scene.Vec3{float64(b.MinX), 0, float64(b.MinZ)}}
	if len(v.Blocks) == 0 {
		s.viewCache.put(key, tile)
		return tile, nil
	}
	paths := append([]string(nil), req.Resources...)
	if demo {
		path, err := s.viewCache.demoAssets(filepath.Join(s.cache, "view-demo-assets"))
		if err != nil {
			return nil, err
		}
		paths = append([]string{path}, paths...)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("выберите клиентский JAR для карты")
	}
	_, painter, releaseResources, err := s.viewCache.acquire(paths, revision, worker)
	if err != nil {
		return nil, err
	}
	defer releaseResources()
	img, err := painter.PaintDetailed(ctx, v, b, req.Step, req.Detail)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err = enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	tile.Data = buf.Bytes()
	s.viewCache.put(key, tile)
	return tile, nil
}
