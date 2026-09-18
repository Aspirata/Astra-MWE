// Package assets reads a Minecraft resource stack. Later entries override earlier entries.
package assets

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const MaxResourceBytes = 64 << 20

type source struct {
	dir     string
	archive *zip.ReadCloser
	entries map[string]*zip.File
}
type Stack struct {
	mu      sync.RWMutex
	sources []source
	closed  bool
}

func Open(paths []string) (*Stack, error) {
	s := &Stack{}
	for _, p := range paths {
		info, e := os.Stat(p)
		if e != nil {
			s.Close()
			return nil, fmt.Errorf("resource %q: %w", p, e)
		}
		if info.IsDir() {
			abs, e := filepath.Abs(p)
			if e != nil {
				s.Close()
				return nil, e
			}
			s.sources = append(s.sources, source{dir: abs})
			continue
		}
		z, e := zip.OpenReader(p)
		if e != nil {
			s.Close()
			return nil, fmt.Errorf("resource %q: %w", p, e)
		}
		entries := make(map[string]*zip.File, len(z.File))
		for _, f := range z.File {
			entries[f.Name] = f
		}
		s.sources = append(s.sources, source{archive: z, entries: entries})
	}
	return s, nil
}
func (s *Stack) Read(name string) ([]byte, error) {
	if s == nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	if !fs.ValidPath(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") || path.Clean(name) != name {
		return nil, fmt.Errorf("invalid resource name %q", name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("resource stack is closed")
	}
	for i := len(s.sources) - 1; i >= 0; i-- {
		src := s.sources[i]
		if src.dir != "" {
			p := filepath.Join(src.dir, filepath.FromSlash(name))
			resolved, e := filepath.EvalSymlinks(p)
			if errors.Is(e, fs.ErrNotExist) {
				continue
			}
			if e != nil {
				return nil, e
			}
			rel, e := filepath.Rel(src.dir, resolved)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("resource escapes directory: %q", name)
			}
			f, e := os.Open(resolved)
			if e != nil {
				return nil, e
			}
			b, e := readBounded(f)
			f.Close()
			return b, e
		}
		if f, ok := src.entries[name]; ok {
			if f.UncompressedSize64 > MaxResourceBytes {
				return nil, fmt.Errorf("resource too large: %q", name)
			}
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			b, e := readBounded(r)
			r.Close()
			return b, e
		}
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

// ReadLayers returns every definition from lowest to highest priority. Minecraft
// data tags merge these definitions unless a higher layer requests replacement.
func (s *Stack) ReadLayers(name string) ([][]byte, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("resource stack is closed")
	}
	var out [][]byte
	for _, src := range s.sources {
		// The outer read lock keeps this shared source alive until the read completes.
		layer := &Stack{sources: []source{src}}
		data, err := layer.Read(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, nil
}
func readBounded(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxResourceBytes+1))
	if e != nil {
		return nil, e
	}
	if len(b) > MaxResourceBytes {
		return nil, errors.New("resource exceeds 64 MiB limit")
	}
	return b, nil
}
func (s *Stack) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var errs []error
	for _, src := range s.sources {
		if src.archive != nil {
			if e := src.archive.Close(); e != nil {
				errs = append(errs, e)
			}
		}
	}
	return errors.Join(errs...)
}
