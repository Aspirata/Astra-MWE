package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPreviewWorkersOverlapAndBoundQueue(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	var releases []func()
	for i := 0; i < cap(s.viewWorkers); i++ {
		_, release, err := s.acquireViewWorker(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, release, err := s.acquireViewWorker(ctx)
		if release != nil {
			release()
		}
		finished <- err
	}()
	select {
	case <-finished:
		t.Fatal("worker budget exceeded")
	default:
	}
	cancel()
	select {
	case err := <-finished:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request did not cancel")
	}
	for _, release := range releases {
		release()
	}
}

func TestParallelMapRequestsShareArchiveNotPainter(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	info, err := s.Demo()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tile, err := s.MapTile(context.Background(), MapRequest{World: info.Path, Step: 1, X: i % 4, Z: i / 4})
			if err != nil {
				t.Error(err)
			} else if len(tile.Data) == 0 {
				t.Error("empty demo tile")
			}
		}(i)
	}
	wg.Wait()
	// Every worker has independent mutable model/tint caches but one zip stack.
	workers := make([]*viewWorker, 0, cap(s.viewWorkers))
	for i := 0; i < cap(s.viewWorkers); i++ {
		workers = append(workers, <-s.viewWorkers)
	}
	for i, w := range workers {
		for _, other := range workers[:i] {
			if w.painter != nil && w.painter == other.painter {
				t.Error("shared mutable painter")
			}
		}
	}
	for _, w := range workers {
		s.viewWorkers <- w
	}
}

func TestTileCacheConcurrentAndMemoryBound(t *testing.T) {
	c := &viewCache{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i))
			for j := 0; j < 20; j++ {
				c.put(key, &ViewTile{Data: make([]byte, 8<<20)})
				c.get(key)
			}
		}(i)
	}
	wg.Wait()
	if c.bytes > 48<<20 {
		t.Fatal("cache exceeded byte budget", c.bytes)
	}
}
