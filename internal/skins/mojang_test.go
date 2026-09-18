package skins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveAuthorizedPlayer(t *testing.T) {
	id := os.Getenv("ASTRA_TEST_PLAYER_UUID")
	if id == "" {
		t.Skip("explicit opt-in live test")
	}
	c := New(filepath.Join("..", "..", ".cache", "live-skin-check"))
	got := c.Get(context.Background(), id, nil)
	if got.Fallback {
		t.Fatal(got.Warning)
	}
	t.Logf("Downloaded skin: name=%s slim=%v PNG=%d bytes", got.Name, got.Slim, len(got.PNG))
}

func TestOfficialFallbackAfterNameMC403(t *testing.T) {
	const id = "12345678123412341234123456789abc"
	calls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/profile/" + id:
			http.Error(w, "forbidden", 403)
		case "/session/" + id:
			value, _ := json.Marshal(map[string]any{"textures": map[string]any{"SKIN": map[string]any{"url": server.URL + "/texture/skin", "metadata": map[string]string{"model": "slim"}}}})
			json.NewEncoder(w).Encode(map[string]any{"id": id, "name": "Aspirata", "properties": []map[string]string{{"name": "textures", "value": base64.StdEncoding.EncodeToString(value)}}})
		case "/texture/skin":
			w.Write(testPNG())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := New(t.TempDir())
	c.BaseURL = server.URL
	c.ProfileURL = server.URL + "/session/"
	got := c.Get(context.Background(), id, nil)
	if got.Fallback || got.Name != "Aspirata" || !got.Slim {
		t.Fatalf("official fallback failed: %+v", got)
	}
	before := calls
	got = c.Get(context.Background(), id, nil)
	if got.Fallback || calls != before {
		t.Fatal("downloaded skin was not cached")
	}
}
