package main

import (
	"astra-mwe/internal/discovery"
	"astra-mwe/internal/scene"
	"astra-mwe/internal/service"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

type paths []string

func (p *paths) String() string     { return fmt.Sprint([]string(*p)) }
func (p *paths) Set(s string) error { *p = append(*p, s); return nil }
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	var resources paths
	worldPath := flag.String("world", "", "World folder containing level.dat")
	inspect := flag.Bool("inspect", false, "Print world metadata and saved players, without exporting")
	discover := flag.Bool("discover", false, "Find installed Minecraft client JARs and launcher worlds")
	demo := flag.Bool("demo", false, "Use original built-in demo world")
	out := flag.String("out", "world.glb", "Output GLB file (must not exist)")
	dim := flag.String("dimension", "minecraft:overworld", "Dimension ID")
	minX := flag.Int("min-x", -32, "Minimum X")
	maxX := flag.Int("max-x", 32, "Maximum X")
	minZ := flag.Int("min-z", -32, "Minimum Z")
	maxZ := flag.Int("max-z", 32, "Maximum Z")
	minY := flag.Int("min-y", -64, "Minimum Y when auto-min disabled")
	maxY := flag.Int("max-y", 319, "Maximum Y")
	auto := flag.Bool("auto-min", true, "Minimum surface height minus padding")
	padding := flag.Int("padding", 4, "At least four blocks beneath surface")
	hollow := flag.Bool("hollow-leaves", false, "Cull internal leaf faces")
	players := flag.Bool("players", true, "Include saved player models")
	optimize := flag.Bool("optimize-mesh", false, "Merge compatible block faces without stretching textures")
	flag.Var(&resources, "resource", "Client JAR, mod JAR, ZIP or directory; repeat in priority order")
	flag.Parse()
	if *discover {
		result, err := discovery.Scan(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	cache = filepath.Join(cache, "AstraMWE")
	if override := os.Getenv("ASTRA_CACHE_DIR"); override != "" {
		cache = override
	}
	s := service.New(cache)
	defer s.Close()
	if *demo {
		_, err = s.Demo()
	} else {
		_, err = s.Open(*worldPath)
	}
	if err != nil {
		return err
	}
	if *inspect {
		return json.NewEncoder(os.Stdout).Encode(s.Status().World)
	}
	err = s.StartBuild(service.BuildRequest{Dimension: *dim, Bounds: scene.Bounds{MinX: *minX, MaxX: *maxX, MinZ: *minZ, MaxZ: *maxZ, MinY: *minY, MaxY: *maxY}, Resources: resources, AutoMin: *auto, Padding: *padding, HollowLeaves: *hollow, BiomeColors: true, Players: *players, OptimizeMesh: *optimize})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	wait := func() error {
		phase := ""
		interrupt := ctx.Done()
		for {
			st := s.Status()
			if st.Phase != phase {
				fmt.Println(st.Phase)
				phase = st.Phase
			}
			if !st.Busy {
				if st.Error != "" {
					return fmt.Errorf("%s", st.Error)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return nil
			}
			select {
			case <-interrupt:
				s.Cancel()
				interrupt = nil
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err = wait(); err != nil {
		return err
	}
	if err = s.Export(*out); err != nil {
		return err
	}
	if err = wait(); err != nil {
		return err
	}
	st := s.Status()
	fmt.Printf("%s: %d blocks, %d triangles, %d materials\n", *out, st.Preview.Blocks, st.Preview.Triangles, st.Preview.Materials)
	for _, warning := range st.Preview.Warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	return nil
}
