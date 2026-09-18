package world

import (
	"astra-mwe/internal/scene"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// VisitChunks enumerates present chunks intersecting bounds. It scans region
// headers in bounded directory batches, avoiding loops over empty world space.
func (r *Reader) VisitChunks(ctx context.Context, dimension string, b scene.Bounds, visit func(scene.Bounds) error) error {
	if err := validateCoordinates(b); err != nil {
		return err
	}
	if r.closed.Load() {
		return fmt.Errorf("world is closed")
	}
	root, ok := r.dimensions[dimension]
	if !ok {
		return fmt.Errorf("unknown dimension %q", dimension)
	}
	dir, err := os.Open(filepath.Join(root, "region"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			parts := strings.Split(entry.Name(), ".")
			if entry.IsDir() || len(parts) != 4 || parts[0] != "r" || parts[3] != "mca" {
				continue
			}
			rx, e1 := strconv.ParseInt(parts[1], 10, 32)
			rz, e2 := strconv.ParseInt(parts[2], 10, 32)
			if e1 != nil || e2 != nil {
				continue
			}
			x, z := int(rx)*512, int(rz)*512
			if x > b.MaxX || x+511 < b.MinX || z > b.MaxZ || z+511 < b.MinZ {
				continue
			}
			f, e := os.Open(filepath.Join(root, "region", entry.Name()))
			if e != nil {
				return e
			}
			header := make([]byte, 4096)
			_, e = io.ReadFull(f, header)
			f.Close()
			if e != nil {
				return fmt.Errorf("region %s: %w", entry.Name(), e)
			}
			for i := 0; i < 1024; i++ {
				if binary.BigEndian.Uint32(header[i*4:]) == 0 {
					continue
				}
				cb := b
				cb.MinX = max(b.MinX, x+(i%32)*16)
				cb.MaxX = min(b.MaxX, x+(i%32)*16+15)
				cb.MinZ = max(b.MinZ, z+(i/32)*16)
				cb.MaxZ = min(b.MaxZ, z+(i/32)*16+15)
				if cb.MinX <= cb.MaxX && cb.MinZ <= cb.MaxZ {
					if e := visit(cb); e != nil {
						return e
					}
				}
			}
		}
		if err == io.EOF {
			return ctx.Err()
		}
	}
}
