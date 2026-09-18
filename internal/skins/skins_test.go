package skins

import (
	"astra-mwe/internal/scene"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCorruptSkinDataRejected(t *testing.T) {
	raw := testPNG()
	if _, err := normalize(raw[:40]); err == nil {
		t.Fatal("valid PNG header with truncated image must not be cached")
	}
}

func TestPlayerFaceOrientation(t *testing.T) {
	for _, yaw := range []float64{0, 90, 180, 270} {
		s := &scene.Scene{}
		AddPlayer(s, scene.Player{Rotation: [2]float64{yaw, 0}}, Skin{PNG: testPNG()})
		q := s.Meshes[0].Primitives[0]
		// The front head face occupies four vertices after the right side.
		front := q.Positions[12:24]
		if front[1] != front[4] || front[7] != front[10] || front[1] >= front[7] {
			t.Fatal("skin rows must follow horizontal face edges")
		}
		radians := yaw * math.Pi / 180
		if math.Abs(float64(q.Normals[12])+math.Sin(radians)) > 1e-5 || math.Abs(float64(q.Normals[14])-math.Cos(radians)) > 1e-5 {
			t.Fatalf("yaw %v must follow Minecraft south/west/north/east convention", yaw)
		}
	}
}

func TestHeadSidesMirroredWithoutChangingOtherFaces(t *testing.T) {
	s := &scene.Scene{}
	AddPlayer(s, scene.Player{}, Skin{PNG: testPNG()})
	for _, part := range []int{0, 6} {
		q := s.Meshes[0].Primitives[part]
		for _, face := range []int{0, 2} {
			if q.UVs[face*8] <= q.UVs[face*8+2] {
				t.Fatal("both head side UVs must be horizontally mirrored, including overlay")
			}
		}
		if q.UVs[8] >= q.UVs[10] {
			t.Fatal("front UV must retain horizontal direction")
		}
	}
}

func TestProfileNamePersistsWhenSkinDownloadFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/profile/player" {
			w.Write([]byte(`<meta property="og:title" content="Test_Player | Minecraft Profile | NameMC"><a href="/skin/abc">skin</a>`))
			return
		}
		http.Error(w, "unavailable", 503)
	}))
	defer server.Close()
	c := New(t.TempDir())
	c.BaseURL = server.URL
	got := c.Get(context.Background(), "player", testPNG())
	if !got.Fallback || got.Name != "Test_Player" {
		t.Fatalf("nickname should survive skin failure: %+v", got)
	}
	if c.CachedName("player") != "Test_Player" {
		t.Fatal("nickname must be cached independently of PNG")
	}
}

func TestLegacyCachedSkinStillResolvesNickname(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`<meta property="og:title" content="CachedPlayer | Minecraft Profile | NameMC">`))
	}))
	defer server.Close()
	c := New(t.TempDir())
	c.BaseURL = server.URL
	raw := testPNG()
	if err := os.WriteFile(c.cachePath("player")+".png", raw, 0600); err != nil {
		t.Fatal(err)
	}
	got := c.Get(context.Background(), "player", nil)
	if got.Name != "CachedPlayer" || got.Fallback || !bytes.Equal(got.PNG, raw) || calls != 1 {
		t.Fatal("cached PNG must not block nickname lookup")
	}
}

func testPNG() []byte {
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			im.Set(x, y, color.NRGBA{170, 100, 60, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}
func TestFailureUsesSteve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer server.Close()
	c := New(t.TempDir())
	c.BaseURL = server.URL
	c.ProfileURL = server.URL + "/session/"
	fallback := testPNG()
	skin := c.Get(context.Background(), "12345678-1234-1234-1234-123456789abc", fallback)
	if !skin.Fallback || !bytes.Equal(skin.PNG, fallback) {
		t.Fatal("failed download must use supplied Steve")
	}
}
func TestPlayerGeometryAndBounds(t *testing.T) {
	s := &scene.Scene{Origin: scene.Vec3{100, 64, 100}}
	p := scene.Player{Name: "Test", UUID: "test", Position: scene.Vec3{102, 65, 103}}
	AddPlayer(s, p, Skin{PNG: testPNG()})
	if len(s.Meshes) == 0 || len(s.Materials) != 1 {
		t.Fatal("player mesh and own material required")
	}
	for _, m := range s.Meshes {
		for _, q := range m.Primitives {
			for i := 0; i < len(q.Positions); i += 3 {
				if q.Positions[i+1] < 1 || q.Positions[i+1] > 3.1 {
					t.Fatal("player must be placed relative to world origin")
				}
			}
		}
	}
}
func TestNameMCFlowCachesDownloadedSkin(t *testing.T) {
	calls := 0
	raw := testPNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/profile/player":
			w.Write([]byte(`<meta property="og:title" content="PlayerName | Minecraft Profile | NameMC"><a href="/skin/abc">skin</a>`))
		case "/skin/abc":
			w.Write([]byte(`<a download href="/texture/abc.png">Download</a>`))
		case "/texture/abc.png":
			w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := New(t.TempDir())
	c.BaseURL = server.URL
	got := c.Get(context.Background(), "player", nil)
	if got.Fallback || !bytes.Equal(got.PNG, raw) {
		t.Fatalf("download failed: %s", got.Warning)
	}
	got = c.Get(context.Background(), "player", nil)
	if got.Fallback || calls != 3 {
		t.Fatal("cached skin should avoid network requests")
	}
}
