package service

import (
	"context"
	"os"
	"sync"
	"testing"
)

// Opt-in: reads the supplied save and resources, never modifies either.
func BenchmarkViewRealTile(b *testing.B) {
	path, jar := os.Getenv("ASTRA_BENCH_WORLD"), os.Getenv("ASTRA_BENCH_JAR")
	if path == "" || jar == "" {
		b.Skip("set ASTRA_BENCH_WORLD and ASTRA_BENCH_JAR")
	}
	s := New(b.TempDir())
	defer s.Close()
	info, err := s.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	req := ViewRequest{World: info.Path, Dimension: "minecraft:overworld", Resources: []string{jar}, X: 98, Z: 40}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Measure generation, not the separate repeat-request cache.
		s.viewCache.entries = nil
		s.viewCache.bytes = 0
		tile, err := s.ViewTile(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(tile.Data)), "GLB-bytes")
	}
}

// Compare scheduling on the same four uncached tiles with one warmed archive.
func BenchmarkParallelPreview(b *testing.B) {
	path, jar := os.Getenv("ASTRA_BENCH_WORLD"), os.Getenv("ASTRA_BENCH_JAR")
	if path == "" || jar == "" {
		b.Skip("set ASTRA_BENCH_WORLD and ASTRA_BENCH_JAR")
	}
	for _, kind := range []string{"3d", "2d"} {
		for _, parallel := range []bool{false, true} {
			name := kind + "/serial"
			if parallel {
				name = kind + "/parallel"
			}
			b.Run(name, func(b *testing.B) {
				s := New(b.TempDir())
				defer s.Close()
				info, err := s.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				build := func(i int) error {
					if kind == "3d" {
						_, err := s.ViewTile(context.Background(), ViewRequest{World: info.Path, Resources: []string{jar}, X: 97 + i%2, Z: 41 + i/2})
						return err
					}
					_, err := s.MapTile(context.Background(), MapRequest{World: info.Path, Resources: []string{jar}, X: 48 + i%2, Z: 20 + i/2, Step: 1})
					return err
				}
				for i := 0; i < 4; i++ {
					if err := build(i); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					s.viewCache.mu.Lock()
					s.viewCache.entries = nil
					s.viewCache.bytes = 0
					s.viewCache.mu.Unlock()
					var wg sync.WaitGroup
					for i := 0; i < 4; i++ {
						if parallel {
							wg.Add(1)
							go func(i int) {
								defer wg.Done()
								if err := build(i); err != nil {
									b.Error(err)
								}
							}(i)
						} else if err := build(i); err != nil {
							b.Fatal(err)
						}
					}
					wg.Wait()
				}
			})
		}
	}
}

func BenchmarkMapRealTile(b *testing.B) {
	path, jar := os.Getenv("ASTRA_BENCH_WORLD"), os.Getenv("ASTRA_BENCH_JAR")
	if path == "" || jar == "" {
		b.Skip("set ASTRA_BENCH_WORLD and ASTRA_BENCH_JAR")
	}
	for _, cached := range []bool{false, true} {
		name := "generate"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			s := New(b.TempDir())
			defer s.Close()
			info, err := s.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			req := MapRequest{World: info.Path, Dimension: "minecraft:overworld", Resources: []string{jar}, X: 49, Z: 20, Step: 1}
			if _, err = s.MapTile(context.Background(), req); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !cached {
					s.viewCache.entries = nil
					s.viewCache.bytes = 0
				}
				tile, err := s.MapTile(context.Background(), req)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(tile.Data)), "PNG-bytes")
			}
		})
	}
}
