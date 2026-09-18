package discovery

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func jar(t *testing.T, path, version string, assets bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	if version != "" {
		w, _ := z.Create("version.json")
		w.Write([]byte(`{"id":"` + version + `","name":"Minecraft ` + version + `"}`))
	}
	if assets {
		w, _ := z.Create("assets/minecraft/textures/block/stone.png")
		w.Write([]byte("fixture"))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}
func save(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "level.dat"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestLauncherLayoutsAndClientAssets(t *testing.T) {
	base := t.TempDir()
	roots := []Root{{filepath.Join(base, "vanilla"), "Minecraft"}, {filepath.Join(base, "prism"), "PrismLauncher"}, {filepath.Join(base, "modrinth"), "ModrinthApp"}, {filepath.Join(base, "legacy"), "Legacy"}}
	jar(t, filepath.Join(roots[0].Path, "versions", "1.13.2", "1.13.2.jar"), "", true)
	os.WriteFile(filepath.Join(roots[0].Path, "versions", "1.13.2", "1.13.2.json"), []byte(`{"id":"1.13.2"}`), 0644)
	jar(t, filepath.Join(roots[1].Path, "libraries/com/mojang/minecraft/1.21.11/minecraft-1.21.11-client.jar"), "1.21.11", true)
	jar(t, filepath.Join(roots[2].Path, "meta/versions/26.1.2/26.1.2.jar"), "26.1.2", true)
	jar(t, filepath.Join(roots[0].Path, "versions/fabric-loader/fabric-loader.jar"), "fabric-loader", false)
	jar(t, filepath.Join(roots[0].Path, "versions/1.12.2/1.12.2.jar"), "1.12.2", true)
	jar(t, filepath.Join(roots[2].Path, "profiles/pack/.fabric/remappedJars/26.1.2.jar"), "26.1.2", true)
	save(t, filepath.Join(roots[0].Path, "saves/Old World"))
	save(t, filepath.Join(roots[1].Path, "instances/My Pack/.minecraft/saves/Prism World"))
	save(t, filepath.Join(roots[2].Path, "profiles/group/My Pack/saves/Modrinth World"))
	save(t, filepath.Join(roots[3].Path, "Minecraft/game/saves/Legacy World"))
	// Saves are leaves; a spurious nested save is never considered a second world.
	save(t, filepath.Join(roots[0].Path, "saves/Old World/backups/saves/Do Not Discover"))
	roots = append(roots, roots[0])
	got, err := ScanRoots(context.Background(), roots)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 3 || len(got.Worlds) != 4 || len(got.Warnings) != 0 {
		t.Fatalf("unexpected scan: %+v", got)
	}
	if got.Versions[0].Version != "1.13.2" || got.Versions[2].Version != "26.1.2" {
		t.Fatal(got.Versions)
	}
	for _, v := range got.Versions {
		if !filepath.IsAbs(v.Path) || v.Launcher == "" {
			t.Fatal(v)
		}
	}
}
func TestCancellationMissingRootsAndCorruptJar(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanRoots(ctx, []Root{{t.TempDir(), "test"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "versions"), 0755)
	os.WriteFile(filepath.Join(base, "versions/broken.jar"), []byte("not zip"), 0644)
	got, err := ScanRoots(context.Background(), []Root{{base, "test"}, {filepath.Join(base, "missing"), "missing"}})
	if err != nil || len(got.Warnings) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}
func TestSymlinkCyclesAndDuplicateRoots(t *testing.T) {
	base := t.TempDir()
	save(t, filepath.Join(base, "profiles/pack/saves/World"))
	if err := os.Symlink(filepath.Join(base, "profiles"), filepath.Join(base, "profiles/pack/loop")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	got, err := ScanRoots(context.Background(), []Root{{base, "test"}})
	if err != nil || len(got.Worlds) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}
