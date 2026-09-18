// Package skins resolves player names and skin PNGs using a local cache,
// NameMC and a Mojang fallback. Failed lookups return a usable Steve skin.
package skins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Skin struct {
	PNG            []byte
	Name           string
	Slim, Fallback bool
	Warning        string
}
type Client struct {
	BaseURL, CacheDir string
	ProfileURL        string
	HTTP              *http.Client
}

func New(cache string) *Client {
	c := &Client{BaseURL: "https://namemc.com", ProfileURL: "https://sessionserver.mojang.com/session/minecraft/profile/", CacheDir: cache}
	c.HTTP = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 || !c.allowed(req.URL) {
			return fmt.Errorf("skin redirect is not allowed")
		}
		return nil
	}}
	return c
}
func (c *Client) allowed(u *url.URL) bool {
	base, _ := url.Parse(c.BaseURL)
	profile, _ := url.Parse(c.ProfileURL)
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	return (u.Scheme == base.Scheme && u.Host == base.Host) || (profile != nil && profile.Host != "" && u.Scheme == profile.Scheme && u.Host == profile.Host) || (u.Scheme == "https" && (host == "namemc.com" || strings.HasSuffix(host, ".namemc.com") || host == "textures.minecraft.net"))
}
func (c *Client) fetch(ctx context.Context, address string) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || !c.allowed(u) {
		return nil, fmt.Errorf("untrusted skin URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AstraMWE/0.1 (Minecraft world viewer)")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("NameMC: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, fmt.Errorf("skin response too large")
	}
	return data, nil
}
func normalize(data []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width != 64 || (cfg.Height != 64 && cfg.Height != 32) {
		return nil, fmt.Errorf("expected a 64x64 or 64x32 skin")
	}
	original, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Height == 64 {
		return data, nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(out, original.Bounds(), original, image.Point{}, draw.Src)
	// Classic skins reuse mirrored right arm and leg on their left counterparts.
	for _, part := range [][4]int{{0, 16, 16, 48}, {40, 16, 32, 48}} {
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				out.Set(part[2]+x, part[3]+y, original.At(part[0]+15-x, part[1]+y))
			}
		}
	}
	var b bytes.Buffer
	err = png.Encode(&b, out)
	return b.Bytes(), err
}
func links(data []byte) (skin, texture string, slim bool) {
	z := html.NewTokenizer(bytes.NewReader(data))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		attrs := map[string]string{}
		for _, a := range t.Attr {
			attrs[a.Key] = a.Val
		}
		if attrs["data-model"] == "slim" {
			slim = true
		}
		if t.Data == "a" {
			h := attrs["href"]
			if strings.HasPrefix(h, "/skin/") && skin == "" {
				skin = h
			}
			_, download := attrs["download"]
			if download || strings.Contains(h, "/texture/") {
				texture = h
			}
		}
	}
	return
}
func (c *Client) getNameMC(ctx context.Context, uuid string, fallback []byte) Skin {
	name := c.CachedName(uuid)
	if len(fallback) == 0 {
		fallback = DemoPNG()
	}
	fail := func(err error) Skin {
		return Skin{PNG: fallback, Name: name, Fallback: true, Warning: fmt.Sprintf("Скин %s: %v; используется Стив", uuid, err)}
	}
	if uuid == "" {
		return fail(fmt.Errorf("нет UUID"))
	}
	cached := c.cachePath(uuid) + ".png"
	if raw, err := os.ReadFile(cached); err == nil {
		if data, err := normalize(raw); err == nil {
			// Old caches contain a PNG but no nickname metadata. Resolve only
			// the profile; an unavailable name must not discard a valid skin.
			if name == "" {
				if base, e := url.Parse(c.BaseURL); e == nil {
					if profile, e := c.fetch(ctx, base.ResolveReference(&url.URL{Path: "/profile/" + uuid}).String()); e == nil {
						name = profileName(profile)
						c.storeName(uuid, name)
					}
				}
			}
			meta, _ := os.ReadFile(cached + ".model")
			return Skin{PNG: data, Name: name, Slim: string(meta) == "slim"}
		}
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return fail(err)
	}
	resolve := func(s string) string {
		u, e := url.Parse(s)
		if e != nil {
			return ""
		}
		return base.ResolveReference(u).String()
	}
	data, err := c.fetch(ctx, resolve("/profile/"+url.PathEscape(uuid)))
	if err != nil {
		return fail(err)
	}
	if found := profileName(data); found != "" {
		name = found
		c.storeName(uuid, name)
	}
	page, tex, slim := links(data)
	if page != "" {
		data, err = c.fetch(ctx, resolve(page))
		if err != nil {
			return fail(err)
		}
		_, tex, slim = links(data)
	}
	if tex == "" {
		return fail(fmt.Errorf("ссылка скачивания NameMC не найдена"))
	}
	raw, err := c.fetch(ctx, resolve(tex))
	if err != nil {
		return fail(err)
	}
	data, err = normalize(raw)
	if err != nil {
		return fail(err)
	}
	if os.MkdirAll(c.CacheDir, 0700) == nil {
		os.WriteFile(cached, data, 0600)
		model := "classic"
		if slim {
			model = "slim"
		}
		os.WriteFile(cached+".model", []byte(model), 0600)
	}
	return Skin{PNG: data, Name: name, Slim: slim}
}

func (c *Client) cachePath(uuid string) string {
	return filepath.Join(c.CacheDir, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(uuid)))))
}
func (c *Client) storeName(uuid, name string) {
	if validName(name) && os.MkdirAll(c.CacheDir, 0700) == nil {
		os.WriteFile(c.cachePath(uuid)+".name", []byte(name), 0600)
	}
}
func validName(name string) bool {
	if len(name) < 1 || len(name) > 16 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
func (c *Client) CachedName(uuid string) string {
	raw, err := os.ReadFile(c.cachePath(uuid) + ".name")
	if err != nil {
		return ""
	}
	name := string(raw)
	if validName(name) {
		return name
	}
	return ""
}
func profileName(data []byte) string {
	z := html.NewTokenizer(bytes.NewReader(data))
	inHeading := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.EndTagToken:
			if z.Token().Data == "h1" {
				inHeading = false
			}
		case html.TextToken:
			if inHeading {
				name := strings.TrimSpace(string(z.Text()))
				if validName(name) && name != "NameMC" {
					return name
				}
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if t.Data == "h1" {
				inHeading = true
			}
			if t.Data == "meta" {
				attrs := map[string]string{}
				for _, a := range t.Attr {
					attrs[a.Key] = a.Val
				}
				if attrs["property"] == "og:title" {
					name := strings.TrimSpace(strings.Split(attrs["content"], "|")[0])
					if validName(name) && name != "NameMC" {
						return name
					}
				}
			}
		}
	}
}

// DemoPNG is an original simple demo skin. Real worlds use Steve from the client JAR.
func DemoPNG() []byte {
	im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	fill := func(r image.Rectangle, c color.NRGBA) {
		draw.Draw(im, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	fill(image.Rect(0, 0, 32, 16), color.NRGBA{174, 124, 91, 255})
	fill(image.Rect(0, 0, 32, 8), color.NRGBA{66, 43, 30, 255})
	fill(image.Rect(16, 16, 40, 32), color.NRGBA{38, 158, 162, 255})
	fill(image.Rect(0, 16, 16, 32), color.NRGBA{64, 62, 142, 255})
	fill(image.Rect(16, 48, 32, 64), color.NRGBA{64, 62, 142, 255})
	fill(image.Rect(40, 16, 56, 32), color.NRGBA{174, 124, 91, 255})
	fill(image.Rect(32, 48, 48, 64), color.NRGBA{174, 124, 91, 255})
	fill(image.Rect(9, 10, 11, 11), color.NRGBA{60, 61, 120, 255})
	fill(image.Rect(13, 10, 15, 11), color.NRGBA{60, 61, 120, 255})
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}
