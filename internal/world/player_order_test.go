package world

import (
	fixture "astra-mwe/internal/world/testdata"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestPlayerFallbackWithPrimaryAndLocalPriority(t *testing.T) {
	for _, mode := range []string{"newest", "primary", "local"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			oldID, newID := "00000000-0000-0000-0000-000000000001", "ffffffff-ffff-ffff-ffff-ffffffffffff"
			player := fixture.Compound{"Pos": fixture.List{Type: 6, Values: []any{float64(1), float64(65), float64(2)}}}
			for i, id := range []string{oldID, newID} {
				p := filepath.Join(dir, "playerdata", id+".dat")
				if err := fixture.GzipNBT(p, player); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(int64(100+i), 0)
				if err := os.Chtimes(p, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			data := fixture.Compound{"DataVersion": int32(3465)}
			want := newID
			if mode == "primary" {
				data["singleplayer_uuid"] = oldID
				want = oldID
			}
			if mode == "local" {
				data["Player"] = player
				want = ""
			}
			if err := fixture.GzipNBT(filepath.Join(dir, "level.dat"), fixture.Compound{"Data": data}); err != nil {
				t.Fatal(err)
			}
			r, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if len(r.Info.Players) == 0 || r.Info.Players[0].UUID != want {
				t.Fatalf("wanted %q first, got %+v", want, r.Info.Players)
			}
		})
	}
}
