// Package discovery finds locally installed client assets and saves without network access.
package discovery

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type Root struct{ Path, Launcher string }
type Version struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Path     string `json:"path"`
	Launcher string `json:"launcher"`
}
type World struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Launcher string `json:"launcher"`
}
type Result struct {
	Versions []Version `json:"versions"`
	Worlds   []World   `json:"worlds"`
	Warnings []string  `json:"warnings"`
}

func Scan(ctx context.Context) (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	var roots []Root
	add := func(base, suffix, launcher string) {
		if base != "" {
			roots = append(roots, Root{filepath.Join(base, suffix), launcher})
		}
	}
	if runtime.GOOS == "windows" {
		app := os.Getenv("APPDATA")
		add(app, ".minecraft", "Minecraft")
		for _, launcher := range []string{"PrismLauncher", "ModrinthApp", "com.modrinth.theseus", ".tlauncher", "legacy", "Minecraft"} {
			add(app, launcher, launcher)
		}
		add(home, ".minecraft", "Minecraft")
	} else {
		add(home, ".minecraft", "Minecraft")
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		for _, launcher := range []string{"PrismLauncher", "ModrinthApp", "com.modrinth.theseus"} {
			add(data, launcher, launcher)
		}
		add(home, ".var/app/org.prismlauncher.PrismLauncher/data/PrismLauncher", "PrismLauncher")
		add(home, ".var/app/com.modrinth.ModrinthApp/data/ModrinthApp", "ModrinthApp")
		if runtime.GOOS == "darwin" {
			add(home, "Library/Application Support/minecraft", "Minecraft")
			add(home, "Library/Application Support/PrismLauncher", "PrismLauncher")
			add(home, "Library/Application Support/ModrinthApp", "ModrinthApp")
		}
	}
	return ScanRoots(ctx, roots)
}

// ScanRoots scans only known launcher subtrees within the supplied roots.
// Canonical paths deduplicate junctions/symlinks and prevent recursive cycles.
func ScanRoots(ctx context.Context, roots []Root) (Result, error) {
	s := scanner{ctx: ctx, result: Result{Versions: []Version{}, Worlds: []World{}, Warnings: []string{}}, visited: map[string]bool{}, versions: map[string]bool{}, worlds: map[string]bool{}}
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return s.result, err
		}
		// The root may itself be a game directory, while launchers place these
		// folders under instances or profiles (sometimes grouped in modpack folders).
		for _, part := range []string{"versions", "meta/versions", "libraries/com/mojang/minecraft", "saves", "instances", "profiles", ".minecraft", "minecraft", "game", "Minecraft/game", "legacy/Minecraft/game"} {
			if err := s.walk(filepath.Join(root.Path, filepath.FromSlash(part)), root.Launcher, 0); err != nil {
				return s.result, err
			}
		}
	}
	sort.Slice(s.result.Versions, func(i, j int) bool {
		a, b := s.result.Versions[i], s.result.Versions[j]
		if a.Version == b.Version {
			return a.Path < b.Path
		}
		return a.Version < b.Version
	})
	sort.Slice(s.result.Worlds, func(i, j int) bool {
		a, b := s.result.Worlds[i], s.result.Worlds[j]
		if a.Name == b.Name {
			return a.Path < b.Path
		}
		return a.Name < b.Name
	})
	return s.result, nil
}

type scanner struct {
	ctx                       context.Context
	result                    Result
	visited, versions, worlds map[string]bool
	count                     int
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
func pathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
func (s *scanner) warn(path string, err error) {
	s.result.Warnings = append(s.result.Warnings, fmt.Sprintf("%s: %v", path, err))
}
func (s *scanner) walk(path, launcher string, depth int) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if depth > 10 {
		s.warn(path, fmt.Errorf("launcher scan depth limit reached"))
		return nil
	}
	real, err := canonical(path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.warn(path, err)
		}
		return nil
	}
	key := pathKey(real)
	if s.visited[key] {
		return nil
	}
	s.visited[key] = true
	info, err := os.Stat(real)
	if err != nil {
		s.warn(real, err)
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	// A save is a leaf: never walk its chunk, player, or datapack directories.
	if level, err := os.Stat(filepath.Join(real, "level.dat")); err == nil && !level.IsDir() {
		if !s.worlds[key] {
			s.worlds[key] = true
			s.result.Worlds = append(s.result.Worlds, World{filepath.Base(real), real, launcher})
		}
		return nil
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		s.warn(real, err)
		return nil
	}
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		s.count++
		if s.count > 100000 {
			return fmt.Errorf("launcher discovery exceeded 100000 entries")
		}
		name := strings.ToLower(entry.Name())
		p := filepath.Join(real, entry.Name())
		if strings.HasSuffix(name, ".jar") && !entry.IsDir() {
			if !s.versions[pathKey(p)] {
				v, ok, err := clientVersion(p)
				if err != nil {
					s.warn(p, err)
				} else if ok {
					v.Path = p
					v.Launcher = launcher
					s.result.Versions = append(s.result.Versions, v)
					s.versions[pathKey(p)] = true
				}
			}
			continue
		}
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		switch name {
		case ".git", ".fabric", "remappedjars", "assets", "mods", "resourcepacks", "shaderpacks", "logs", "screenshots", "crash-reports", "natives", "libraries", "cache", "caches", "runtime", "java", "jre", "datapacks", "backups":
			continue
		}
		if err := s.walk(p, launcher, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func clientVersion(path string) (Version, bool, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return Version{}, false, err
	}
	defer z.Close()
	var meta *zip.File
	hasAssets := false
	for _, f := range z.File {
		if f.Name == "version.json" {
			meta = f
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/textures/") && strings.HasSuffix(f.Name, ".png") {
			hasAssets = true
		}
	}
	if !hasAssets {
		return Version{}, false, nil
	}
	var data struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if meta != nil {
		r, err := meta.Open()
		if err != nil {
			return Version{}, false, err
		}
		err = json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&data)
		r.Close()
		if err != nil {
			return Version{}, false, fmt.Errorf("version.json: %w", err)
		}
	} else {
		// Java 1.13 client jars predate embedded version.json. Launchers retain
		// their adjacent official version manifest, which supplies the identity.
		manifest := strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
		f, err := os.Open(manifest)
		if err != nil {
			if os.IsNotExist(err) {
				return Version{}, false, nil
			}
			return Version{}, false, err
		}
		err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&data)
		f.Close()
		if err != nil {
			return Version{}, false, err
		}
	}
	if data.ID == "" {
		data.ID = data.Name
	}
	if data.ID == "" {
		return Version{}, false, nil
	}
	if data.Name == "" {
		data.Name = data.ID
	}
	// Exclude known releases before the flattened 1.13 block format.
	if strings.HasPrefix(data.ID, "1.") {
		minorText := strings.SplitN(strings.TrimPrefix(data.ID, "1."), ".", 2)[0]
		parts := strings.FieldsFunc(minorText, func(r rune) bool { return r < '0' || r > '9' })
		if len(parts) > 0 {
			minorText = parts[0]
		}
		if minor, err := strconv.Atoi(minorText); err == nil && minor < 13 {
			return Version{}, false, nil
		}
	}
	return Version{Name: data.Name, Version: data.ID}, true, nil
}
