package service

import (
	"astra-mwe/internal/scene"
	fixture "astra-mwe/internal/world/testdata"
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestEmptyViewTileReturnsNoContent(t *testing.T) {
	dir := t.TempDir()
	if err := fixture.World(dir, 3465, 2); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir())
	defer s.Close()
	info, err := s.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/view-tile", bytes.NewBufferString(fmt.Sprintf(`{"world":%q,"dimension":"minecraft:overworld","x":100,"z":100}`, info.Path)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 204 || response.Body.Len() != 0 || response.Header().Get("X-Astra-Origin") != "[3200,-64,3200]" {
		t.Fatalf("empty tile: %d %s %s", response.Code, response.Header(), response.Body.String())
	}
	if s.Status().Preview != nil || s.Status().Busy {
		t.Fatal("empty view changed export state")
	}
}

func TestViewTileDoesNotChangeExportSelectionOrBytes(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	info, err := s.Demo()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartBuild(BuildRequest{Bounds: scene.Bounds{MinX: -3, MaxX: 3, MinY: 0, MaxY: 100, MinZ: -3, MaxZ: 3}, AutoMin: true, Padding: 4}); err != nil {
		t.Fatal(err)
	}
	<-s.done
	before := s.Status()
	export := append([]byte(nil), s.data...)
	if before.Error != "" {
		t.Fatal(before.Error)
	}
	tile, err := s.ViewTile(context.Background(), ViewRequest{World: info.Path, Dimension: "minecraft:overworld", X: 3, Z: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(tile.Data, []byte("glTF")) || tile.Origin[0] != 96 || tile.Origin[2] != 64 {
		t.Fatalf("wrong neighbor tile: %+v", tile.Origin)
	}
	after := s.Status()
	if after.Preview != before.Preview || after.Settings.Bounds != before.Settings.Bounds || !bytes.Equal(export, s.data) {
		t.Fatal("view request changed export")
	}
	if !after.Settings.BiomeColors || after.Settings.BiomeBlend != 7 {
		t.Fatal("biome policy not enforced")
	}
}

func TestViewTileRejectsStaleWorldAndUnboundedCoordinates(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()
	s.Demo()
	for _, r := range []ViewRequest{{World: "old"}, {World: "demo", X: 937500}, {World: "demo", Z: -937501}} {
		if _, err := s.ViewTile(context.Background(), r); err == nil {
			t.Fatal("accepted invalid tile", r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ViewTile(ctx, ViewRequest{World: "demo"}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
