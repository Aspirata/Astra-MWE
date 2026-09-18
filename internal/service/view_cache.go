package service

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/mesher"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

type cachedView struct {
	tile    *ViewTile
	used    uint64
	expires time.Time
}
type viewResources struct {
	key    string
	assets *assets.Stack
}

// viewCache owns immutable tile results and the shared resource archives.
// mu guards the bounded result cache; resourceMu keeps archives open while
// borrowed by workers. Each worker owns its painter and uses it exclusively.
type viewCache struct {
	mu         sync.Mutex
	resourceMu sync.RWMutex
	demoOnce   sync.Once
	demoPath   string
	demoErr    error
	entries    map[string]cachedView
	bytes      int
	tick       uint64
	resources  viewResources
}

func resourceStamp(paths []string) string {
	data, _ := json.Marshal(paths)
	key := string(data)
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil {
			key += fmt.Sprintf("|%d:%d", st.Size(), st.ModTime().UnixNano())
		}
	}
	return key
}
func (c *viewCache) get(key string) *ViewTile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		if time.Now().Before(e.expires) {
			c.tick++
			e.used = c.tick
			c.entries[key] = e
			return e.tile
		}
		c.bytes -= len(e.tile.Data)
		delete(c.entries, key)
	}
	return nil
}
func (c *viewCache) put(key string, tile *ViewTile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(tile.Data) > 48<<20 {
		return
	}
	if c.entries == nil {
		c.entries = map[string]cachedView{}
	}
	if e, ok := c.entries[key]; ok {
		c.bytes -= len(e.tile.Data)
	}
	c.tick++
	c.entries[key] = cachedView{tile, c.tick, time.Now().Add(2 * time.Minute)}
	c.bytes += len(tile.Data)
	for c.bytes > 48<<20 || len(c.entries) > 256 {
		oldest := ""
		var used uint64 = ^uint64(0)
		for k, e := range c.entries {
			if e.used < used {
				oldest, used = k, e.used
			}
		}
		c.bytes -= len(c.entries[oldest].tile.Data)
		delete(c.entries, oldest)
	}
}

// acquire returns resources borrowed until the release function is called.
// Replacing archives requires the write lock, so it waits for all borrowers.
func (c *viewCache) acquire(paths []string, revision uint64, worker *viewWorker) (*assets.Stack, *mesher.SurfacePainter, func(), error) {
	key := fmt.Sprintf("%d:%s", revision, resourceStamp(paths))
	for {
		c.resourceMu.RLock()
		if c.resources.assets != nil && c.resources.key == key {
			if worker.key != key {
				worker.key, worker.painter = key, mesher.NewSurfacePainter(c.resources.assets)
			}
			return c.resources.assets, worker.painter, c.resourceMu.RUnlock, nil
		}
		c.resourceMu.RUnlock()
		c.resourceMu.Lock()
		if c.resources.assets == nil || c.resources.key != key {
			a, err := assets.Open(paths)
			if err != nil {
				c.resourceMu.Unlock()
				return nil, nil, nil, err
			}
			if c.resources.assets != nil {
				c.resources.assets.Close()
			}
			c.resources = viewResources{key, a}
		}
		c.resourceMu.Unlock()
	}
}

type viewWorker struct {
	key     string
	painter *mesher.SurfacePainter
}

func newViewWorkers() chan *viewWorker {
	// Bound temporary volumes/meshes as well as CPU use. Keep CPU capacity for
	// the WebView, OS and export job, even on machines with many logical CPUs.
	n := max(1, min(4, runtime.GOMAXPROCS(0)/2))
	workers := make(chan *viewWorker, n)
	for i := 0; i < n; i++ {
		workers <- &viewWorker{}
	}
	return workers
}

func (s *Service) acquireViewWorker(ctx context.Context) (*viewWorker, func(), error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case worker := <-s.viewWorkers:
		if err := ctx.Err(); err != nil {
			s.viewWorkers <- worker
			return nil, nil, err
		}
		return worker, func() { s.viewWorkers <- worker }, nil
	}
}

func (c *viewCache) demoAssets(path string) (string, error) {
	c.demoOnce.Do(func() { c.demoPath, c.demoErr = DemoAssets(path) })
	return c.demoPath, c.demoErr
}

func (c *viewCache) close() {
	c.resourceMu.Lock()
	defer c.resourceMu.Unlock()
	if c.resources.assets != nil {
		c.resources.assets.Close()
		c.resources = viewResources{}
	}
	c.mu.Lock()
	c.entries = nil
	c.bytes = 0
	c.mu.Unlock()
}
