package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"astra-mwe/internal/discovery"
)

// Handler exposes the service API used by the desktop WebView and browser UI.
func (s *Service) Handler() http.Handler { return http.HandlerFunc(s.serve) }
func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("expected application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return d.Decode(v)
}
func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		respond(w, 403, map[string]string{"error": "cross-site request rejected"})
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host {
			respond(w, 403, map[string]string{"error": "origin mismatch"})
			return
		}
	}
	fail := func(err error) { respond(w, 400, map[string]string{"error": err.Error()}) }
	switch r.URL.Path {
	case "/api/map-tile":
		if r.Method != "POST" {
			break
		}
		var req MapRequest
		if err := decode(r, &req); err != nil {
			fail(err)
			return
		}
		tile, err := s.MapTile(r.Context(), req)
		if err != nil {
			fail(err)
			return
		}
		origin, _ := json.Marshal(tile.Origin)
		w.Header().Set("X-Astra-Origin", string(origin))
		w.Header().Set("Cache-Control", "no-store")
		if len(tile.Data) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(tile.Data)
		return
	case "/api/view-tile":
		if r.Method != "POST" {
			break
		}
		var req ViewRequest
		if err := decode(r, &req); err != nil {
			fail(err)
			return
		}
		tile, err := s.ViewTile(r.Context(), req)
		if err != nil {
			fail(err)
			return
		}
		origin, _ := json.Marshal(tile.Origin)
		w.Header().Set("X-Astra-Origin", string(origin))
		w.Header().Set("Cache-Control", "no-store")
		if len(tile.Data) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "model/gltf-binary")
		w.Write(tile.Data)
		return
	case "/api/discover":
		if r.Method != "GET" {
			break
		}
		result, err := discovery.Scan(r.Context())
		if err != nil {
			fail(err)
		} else {
			respond(w, 200, result)
		}
		return
	case "/api/status":
		if r.Method != "GET" {
			break
		}
		respond(w, 200, s.Status())
		return
	case "/api/demo":
		if r.Method != "GET" {
			break
		}
		info, err := s.Demo()
		if err != nil {
			fail(err)
		} else {
			respond(w, 200, info)
		}
		return
	case "/api/open":
		if r.Method != "POST" {
			break
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := decode(r, &req); err != nil {
			fail(err)
			return
		}
		info, err := s.Open(req.Path)
		if err != nil {
			fail(err)
		} else {
			respond(w, 200, info)
		}
		return
	case "/api/build":
		if r.Method != "POST" {
			break
		}
		var req BuildRequest
		if err := decode(r, &req); err != nil {
			fail(err)
			return
		}
		if err := s.StartBuild(req); err != nil {
			fail(err)
		} else {
			respond(w, 200, map[string]bool{"started": true})
		}
		return
	case "/api/export":
		if r.Method != "POST" {
			break
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := decode(r, &req); err != nil {
			fail(err)
			return
		}
		if err := s.Export(req.Path); err != nil {
			fail(err)
		} else {
			respond(w, 200, map[string]bool{"started": true})
		}
		return
	case "/api/cancel":
		if r.Method != "POST" {
			break
		}
		s.Cancel()
		respond(w, 200, map[string]bool{"cancelled": true})
		return
	case "/api/preview.glb":
		if r.Method != "GET" {
			break
		}
		// Open while holding the publication lock, then stream through our own
		// handle without blocking status updates for the duration of the response.
		s.mu.Lock()
		data := s.previewData
		var file *os.File
		if s.previewFile != "" {
			file, _ = os.Open(s.previewFile)
		}
		s.mu.Unlock()
		if file != nil {
			defer file.Close()
			stat, err := file.Stat()
			if err != nil {
				fail(err)
				return
			}
			w.Header().Set("Content-Type", "model/gltf-binary")
			w.Header().Set("Cache-Control", "no-store")
			http.ServeContent(w, r, "preview.glb", stat.ModTime(), file)
			return
		}
		if len(data) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "model/gltf-binary")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
		return
	default:
		http.NotFound(w, r)
		return
	}
	respond(w, 405, map[string]string{"error": "method not allowed"})
}
