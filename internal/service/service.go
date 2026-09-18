package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"astra-mwe/internal/assets"
	"astra-mwe/internal/exporter"
	"astra-mwe/internal/mesher"
	"astra-mwe/internal/meshopt"
	"astra-mwe/internal/scene"
	"astra-mwe/internal/selection"
	"astra-mwe/internal/skins"
	"astra-mwe/internal/world"
)

type BuildRequest struct {
	Dimension    string       `json:"dimension"`
	Bounds       scene.Bounds `json:"bounds"`
	Resources    []string     `json:"resources"`
	AutoMin      bool         `json:"autoMin"`
	Padding      int          `json:"padding"`
	HollowLeaves bool         `json:"hollowLeaves"`
	BiomeColors  bool         `json:"biomeColors"`
	BiomeBlend   int          `json:"biomeBlend"`
	Players      bool         `json:"players"`
	OptimizeMesh bool         `json:"optimizeMesh"`
}
type Preview struct {
	Bounds               scene.Bounds `json:"bounds"`
	Blocks               int          `json:"blocks"`
	Triangles            int          `json:"triangles"`
	WorldTrianglesBefore int          `json:"worldTrianglesBefore"`
	WorldTrianglesAfter  int          `json:"worldTrianglesAfter"`
	Optimized            bool         `json:"optimized"`
	Materials            int          `json:"materials"`
	Warnings             []string     `json:"warnings"`
	Origin               scene.Vec3   `json:"origin"`
	Revision             int64        `json:"revision"`
}
type Status struct {
	Busy       bool             `json:"busy"`
	Phase      string           `json:"phase"`
	Progress   float64          `json:"progress"`
	Error      string           `json:"error"`
	World      *scene.WorldInfo `json:"world"`
	Preview    *Preview         `json:"preview"`
	Settings   BuildRequest     `json:"settings"`
	ExportPath string           `json:"exportPath,omitempty"`
}

// Service owns the open world, generated previews, and shared tile resources.
// Build and export jobs are serialized by state.Busy; tile requests use a
// separate bounded worker pool so navigation can continue during a build.
type Service struct {
	mu           sync.Mutex
	state        Status
	reader       *world.Reader
	demo         bool
	data         []byte
	previewData  []byte
	previewFile  string
	exportFile   string
	cancel       context.CancelFunc
	done         chan struct{}
	cache        string
	viewWorkers  chan *viewWorker
	viewContext  context.Context
	viewCancel   context.CancelFunc
	viewCache    viewCache
	viewRevision uint64
}

func New(cache string) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{cache: cache, viewWorkers: newViewWorkers(), viewContext: ctx, viewCancel: cancel, state: Status{Phase: "Выберите мир", Settings: BuildRequest{AutoMin: true, Padding: 4, BiomeColors: true, BiomeBlend: 7, Players: true}}}
}

// Status returns a snapshot whose nested slices and pointers are read-only.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Close cancels work and releases owned resources. Call it once, after stopping
// new requests; it waits for the current build/export and all tile workers.
func (s *Service) Close() {
	s.viewCancel()
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	// Cancellation stops queued requests. Drain every worker before releasing
	// shared resources; no request can use a closed archive or painter.
	for i := 0; i < cap(s.viewWorkers); i++ {
		<-s.viewWorkers
	}
	s.viewCache.close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearBuildFilesLocked()
	if s.reader != nil {
		s.reader.Close()
	}
}
func (s *Service) setProgress(phase string, value float64) {
	s.mu.Lock()
	s.state.Phase = phase
	s.state.Progress = value
	s.mu.Unlock()
}
func ValidateBounds(b scene.Bounds) error {
	if b.MinX > b.MaxX || b.MinY > b.MaxY || b.MinZ > b.MaxZ {
		return fmt.Errorf("минимальная координата должна быть не больше максимальной")
	}
	for _, n := range []int{b.MinX, b.MaxX, b.MinZ, b.MaxZ} {
		if n < -30000000 || n > 30000000 {
			return fmt.Errorf("координаты вне границ Minecraft")
		}
	}
	if b.MinY < -2048 || b.MaxY > 2047 {
		return fmt.Errorf("неподдерживаемый диапазон высот")
	}
	return nil
}
func (s *Service) Open(path string) (scene.WorldInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy {
		return scene.WorldInfo{}, fmt.Errorf("дождитесь завершения или отмените операцию")
	}
	r, err := world.Open(path)
	if err != nil {
		return scene.WorldInfo{}, err
	}
	nameCache := skins.New(filepath.Join(s.cache, "skins"))
	fallbackFace, _ := skins.FacePNG(skins.DemoPNG())
	for i := range r.Info.Players {
		p := &r.Info.Players[i]
		face := nameCache.CachedFace(p.UUID)
		if len(face) == 0 {
			face = fallbackFace
		}
		p.Face = "data:image/png;base64," + base64.StdEncoding.EncodeToString(face)
		if p.Name == "" || p.Name == p.UUID {
			if name := nameCache.CachedName(p.UUID); name != "" {
				p.Name = name
			}
		}
	}
	if s.reader != nil {
		s.reader.Close()
	}
	s.reader = r
	s.viewRevision++
	s.demo = false
	s.clearBuildFilesLocked()
	s.data = nil
	s.previewData = nil
	s.state.Preview = nil
	s.state.World = &r.Info
	s.state.Error = ""
	s.state.Phase = "Мир открыт"
	s.state.ExportPath = ""
	return r.Info, nil
}
func (s *Service) Demo() (scene.WorldInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy {
		return scene.WorldInfo{}, fmt.Errorf("сначала завершите текущую операцию")
	}
	if s.reader != nil {
		s.reader.Close()
		s.reader = nil
	}
	s.demo = true
	s.viewRevision++
	s.clearBuildFilesLocked()
	s.data = nil
	s.previewData = nil
	info := scene.WorldInfo{Path: "demo", Name: "Демо · Берёзовый берег", Version: "Встроенный пример", Dimensions: []string{"minecraft:overworld"}, Spawn: scene.Vec3{0, 66, 0}, MinY: 0, MaxY: 127, Players: []scene.Player{{UUID: "demo-player", Name: "Путешественник", Dimension: "minecraft:overworld", Position: scene.Vec3{2, 66, 2}}}}
	face, _ := skins.FacePNG(skins.DemoPNG())
	info.Players[0].Face = "data:image/png;base64," + base64.StdEncoding.EncodeToString(face)
	s.state.World = &info
	s.state.Preview = nil
	s.state.Phase = "Открыт демонстрационный мир"
	s.state.Error = ""
	return info, nil
}
func (s *Service) StartBuild(req BuildRequest) error {
	// Biome coloration is part of world appearance, not an export preference.
	req.BiomeColors, req.BiomeBlend = true, 7
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy {
		return fmt.Errorf("операция уже выполняется")
	}
	if s.state.World == nil {
		return fmt.Errorf("сначала откройте мир")
	}
	if req.Dimension == "" {
		req.Dimension = "minecraft:overworld"
	}
	dimMin, dimMax := s.state.World.MinY, s.state.World.MaxY
	if s.reader != nil {
		dimMin, dimMax = s.reader.DimensionBounds(req.Dimension)
	}
	if req.AutoMin {
		req.Bounds.MinY = dimMin
		req.Bounds.MaxY = dimMax
	}
	if req.Bounds.MaxY > dimMax {
		req.Bounds.MaxY = dimMax
	}
	if err := ValidateBounds(req.Bounds); err != nil {
		return err
	}
	if req.Padding < 4 {
		req.Padding = 4
	}
	if req.Padding > 256 {
		return fmt.Errorf("запас вниз должен быть от 4 до 256")
	}
	if len(req.Resources) > 128 {
		return fmt.Errorf("слишком много пакетов ресурсов")
	}
	if !s.demo && len(req.Resources) == 0 {
		return fmt.Errorf("добавьте клиентский JAR Minecraft в ресурсы")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.state.Busy = true
	s.state.Error = ""
	s.state.Progress = 0
	s.state.Phase = "Чтение чанков"
	s.state.Settings = req
	demo, r := s.demo, s.reader
	info := *s.state.World
	info.Players = append([]scene.Player(nil), info.Players...)
	info.MinY = dimMin
	info.MaxY = dimMax
	go func() {
		defer close(done)
		defer cancel()
		result, data, err := s.build(ctx, req, demo, r, info)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Busy = false
		s.cancel = nil
		if err != nil {
			s.state.Error = err.Error()
			s.state.Phase = "Ошибка"
			if errors.Is(err, context.Canceled) {
				s.state.Error = ""
				s.state.Phase = "Операция отменена"
			}
			return
		}
		// Cancellation can arrive after the build produced files but before
		// publication. Until this point the job still owns those files.
		if ctx.Err() != nil {
			data.cleanup()
			s.state.Phase = "Операция отменена"
			return
		}
		s.clearBuildFilesLocked()
		s.previewFile, s.exportFile = data.previewFile, data.exportFile
		s.state.Preview = result
		s.data = data.export
		s.previewData = data.preview
		s.state.Phase = "Предпросмотр готов"
		s.state.Progress = 1
	}()
	return nil
}

// buildOutput holds bytes for small builds or temporary files for tiled builds.
// A successful publication transfers file ownership to Service; otherwise the
// producing job must remove them.
type buildOutput struct {
	preview, export         []byte
	previewFile, exportFile string
}

func (o *buildOutput) cleanup() {
	if o != nil {
		removeBuildFiles(o.previewFile, o.exportFile)
	}
}

func (s *Service) build(ctx context.Context, req BuildRequest, demo bool, r *world.Reader, info scene.WorldInfo) (*Preview, *buildOutput, error) {
	if needsTiledBuild(req.Bounds) {
		return s.buildTiled(ctx, req, demo, r, info)
	}
	var v *scene.Volume
	var err error
	loadBounds := req.Bounds
	if req.BiomeColors && req.BiomeBlend > 0 {
		// The blend kernel needs biome samples outside the exported region.
		loadBounds.MinX = max(-30000000, loadBounds.MinX-req.BiomeBlend)
		loadBounds.MaxX = min(30000000, loadBounds.MaxX+req.BiomeBlend)
		loadBounds.MinZ = max(-30000000, loadBounds.MinZ-req.BiomeBlend)
		loadBounds.MaxZ = min(30000000, loadBounds.MaxZ+req.BiomeBlend)
	}
	if demo {
		v = DemoVolume(loadBounds)
		v.Players = info.Players
	} else {
		v, err = r.Load(ctx, req.Dimension, loadBounds)
	}
	if err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	v.Bounds = req.Bounds
	for p := range v.Blocks {
		if p[0] < req.Bounds.MinX || p[0] > req.Bounds.MaxX || p[2] < req.Bounds.MinZ || p[2] > req.Bounds.MaxZ {
			delete(v.Blocks, p)
		}
	}
	s.setProgress("Определение нижней границы", .25)
	if req.AutoMin {
		bottom, err := selection.AutoMin(v, req.Padding)
		if err != nil {
			return nil, nil, err
		}
		if bottom < info.MinY {
			bottom = info.MinY
		}
		v.Bounds.MinY = bottom
		for p := range v.Blocks {
			if p[1] < bottom {
				delete(v.Blocks, p)
			}
		}
		top := v.Bounds.MinY
		for p := range v.Blocks {
			if p[1] > top {
				top = p[1]
			}
		}
		for _, p := range info.Players {
			if p.Dimension == req.Dimension && inside(req.Bounds, p.Position) {
				top = max(top, int(math.Floor(p.Position[1]+1.9)))
			}
		}
		v.Bounds.MaxY = min(top, req.Bounds.MaxY)
	}
	paths := append([]string(nil), req.Resources...)
	if demo {
		p, err := DemoAssets(filepath.Join(s.cache, "demo-assets"))
		if err != nil {
			return nil, nil, err
		}
		paths = append([]string{p}, paths...)
	}
	a, err := assets.Open(paths)
	if err != nil {
		return nil, nil, err
	}
	defer a.Close()
	s.setProgress("Построение моделей и материалов", .4)
	sc, err := mesher.Build(ctx, v, a, scene.MeshOptions{HollowLeaves: req.HollowLeaves, BiomeColors: req.BiomeColors, BiomeBlend: req.BiomeBlend})
	if err != nil {
		return nil, nil, err
	}
	worldTrianglesBefore := triangleCount(sc)
	if req.OptimizeMesh {
		s.setProgress("Оптимизация одинаковых граней", .65)
		if err := meshopt.Optimize(ctx, sc); err != nil {
			return nil, nil, err
		}
	} else {
		if err = meshopt.DeduplicateContext(ctx, sc); err != nil {
			return nil, nil, err
		}
	}
	worldMeshCount, worldMaterialCount := len(sc.Meshes), len(sc.Materials)
	worldTrianglesAfter := triangleCount(sc)
	{
		s.setProgress("Модели игроков и скины", .75)
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
			if p.Dimension != req.Dimension || !inside(v.Bounds, p.Position) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, nil, err
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
	}
	s.setProgress("Подготовка GLB", .9)
	data, err := exporter.GLB(sc)
	if err != nil {
		return nil, nil, err
	}
	exportScene := *sc
	if !req.Players {
		exportScene.Meshes = sc.Meshes[:worldMeshCount]
		exportScene.Materials = sc.Materials[:worldMaterialCount]
	}
	exportData := data
	if !req.Players {
		exportData, err = exporter.GLB(&exportScene)
		if err != nil {
			return nil, nil, err
		}
	}
	tri := triangleCount(&exportScene)
	return &Preview{Bounds: v.Bounds, Blocks: len(v.Blocks), Triangles: tri, WorldTrianglesBefore: worldTrianglesBefore, WorldTrianglesAfter: worldTrianglesAfter, Optimized: req.OptimizeMesh, Materials: len(exportScene.Materials), Warnings: sc.Warnings, Origin: sc.Origin, Revision: time.Now().UnixMilli()}, &buildOutput{preview: data, export: exportData}, nil
}

func triangleCount(sc *scene.Scene) int {
	n := 0
	for _, m := range sc.Meshes {
		for _, p := range m.Primitives {
			n += len(p.Indices) / 3
		}
	}
	return n
}

func inside(b scene.Bounds, p scene.Vec3) bool {
	return p[0] >= float64(b.MinX) && p[0] < float64(b.MaxX+1) && p[1] >= float64(b.MinY) && p[1] < float64(b.MaxY+1) && p[2] >= float64(b.MinZ) && p[2] < float64(b.MaxZ+1)
}

// Publish an immutable world snapshot so status readers never race nickname updates.
func (s *Service) setPlayerName(uuid, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.World == nil {
		return
	}
	info := *s.state.World
	info.Players = append([]scene.Player(nil), info.Players...)
	for i := range info.Players {
		if info.Players[i].UUID == uuid {
			info.Players[i].Name = name
		}
	}
	s.state.World = &info
}

func (s *Service) setPlayerSkin(worldPath, uuid string, skin skins.Skin) {
	face, err := skins.FacePNG(skin.PNG)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.World == nil || s.state.World.Path != worldPath {
		return
	}
	info := *s.state.World
	info.Players = append([]scene.Player(nil), info.Players...)
	for i := range info.Players {
		if info.Players[i].UUID == uuid {
			info.Players[i].Face = "data:image/png;base64," + base64.StdEncoding.EncodeToString(face)
			if skin.Name != "" {
				info.Players[i].Name = skin.Name
			}
		}
	}
	s.state.World = &info
}

// Cancel requests cancellation; the job clears Busy after its cleanup finishes.
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Export(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Busy {
		return fmt.Errorf("операция уже выполняется")
	}
	if len(s.data) == 0 && s.exportFile == "" {
		return fmt.Errorf("сначала постройте предпросмотр")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("укажите файл экспорта")
	}
	if !strings.EqualFold(filepath.Ext(path), ".glb") {
		return fmt.Errorf("файл экспорта должен иметь расширение .glb")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("файл уже существует; выберите новое имя, чтобы сохранить предыдущий экспорт")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	data := s.data
	sourceFile := s.exportFile
	s.state.Busy = true
	s.state.Error = ""
	s.state.Phase = "Запись GLB"
	s.state.Progress = 0
	go func() {
		defer close(done)
		defer cancel()
		var err error
		if sourceFile != "" {
			err = copyBuildFile(ctx, path, sourceFile)
		} else {
			err = WriteFile(ctx, path, data)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Busy = false
		s.cancel = nil
		if err != nil {
			s.state.Error = err.Error()
			s.state.Phase = "Ошибка экспорта"
			if errors.Is(err, context.Canceled) {
				s.state.Error = ""
				s.state.Phase = "Операция отменена"
			}
			return
		}
		s.state.ExportPath = path
		s.state.Phase = "Экспорт завершён"
		s.state.Progress = 1
	}()
	return nil
}
func WriteFile(ctx context.Context, path string, data []byte) error {
	return writeFileWithLink(ctx, path, data, os.Link)
}

func writeFileWithLink(ctx context.Context, path string, data []byte, link func(string, string) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".astra-*.glb")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	for offset := 0; offset < len(data); {
		if err = ctx.Err(); err != nil {
			f.Close()
			return err
		}
		end := offset + (1 << 20)
		if end > len(data) {
			end = len(data)
		}
		n, e := f.Write(data[offset:end])
		if e != nil {
			f.Close()
			return e
		}
		offset += n
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Hard-link commits only if destination does not exist, avoiding TOCTOU overwrite.
	if err = link(temp, path); err == nil {
		return nil
	}
	// FAT/exFAT and some network filesystems cannot hard-link. Exclusive
	// creation preserves the no-overwrite guarantee; a failed copy removes
	// only the destination this call successfully created.
	dest, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("не удалось сохранить файл (проверьте путь и отсутствие файла): %w", err)
	}
	complete := false
	defer func() {
		dest.Close()
		if !complete {
			os.Remove(path)
		}
	}()
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := offset + (1 << 20)
		if end > len(data) {
			end = len(data)
		}
		n, err := dest.Write(data[offset:end])
		if err != nil {
			return err
		}
		offset += n
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := dest.Sync(); err != nil {
		return err
	}
	if err := dest.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}
