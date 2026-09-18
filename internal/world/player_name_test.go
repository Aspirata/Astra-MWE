package world

import (
	fixture "astra-mwe/internal/world/testdata"
	"os"
	"path/filepath"
	"testing"
)

func TestPlayerNameFromGameDirectoryCache(t *testing.T) {
	game := t.TempDir()
	dir := filepath.Join(game, "saves", "World")
	if err := fixture.World(dir, 3465, 2); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "usercache.json"), []byte(`[{"uuid":"12345678-1234-1234-1234-123412345678","name":"LocalNickname"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Info.Players[0].Name != "LocalNickname" {
		t.Fatalf("game directory nickname not loaded: %+v", r.Info.Players)
	}
}
