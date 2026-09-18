package skins

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// NameMC remains the first source. The user authorized Mojang as a fallback.
func (c *Client) Get(ctx context.Context, uuid string, fallback []byte) Skin {
	skin := c.getNameMC(ctx, uuid, fallback)
	if !skin.Fallback || ctx.Err() != nil || c.ProfileURL == "" {
		return skin
	}
	id := strings.ToLower(strings.ReplaceAll(uuid, "-", ""))
	if raw, err := hex.DecodeString(id); err != nil || len(raw) != 16 {
		return skin
	}
	official, err := c.mojang(ctx, id)
	if err != nil {
		skin.Warning += fmt.Sprintf("; Mojang: %v", err)
		return skin
	}
	c.storeName(uuid, official.Name)
	if os.MkdirAll(c.CacheDir, 0700) == nil {
		file := c.cachePath(uuid) + ".png"
		os.WriteFile(file, official.PNG, 0600)
		model := "classic"
		if official.Slim {
			model = "slim"
		}
		os.WriteFile(file+".model", []byte(model), 0600)
	}
	return official
}

func (c *Client) mojang(ctx context.Context, id string) (Skin, error) {
	raw, err := c.fetch(ctx, strings.TrimRight(c.ProfileURL, "/")+"/"+id)
	if err != nil {
		return Skin{}, err
	}
	var profile struct {
		ID         string                         `json:"id"`
		Name       string                         `json:"name"`
		Properties []struct{ Name, Value string } `json:"properties"`
	}
	if err = json.Unmarshal(raw, &profile); err != nil {
		return Skin{}, err
	}
	if strings.ToLower(profile.ID) != id || !validName(profile.Name) {
		return Skin{}, fmt.Errorf("profile does not match requested player")
	}
	for _, prop := range profile.Properties {
		if prop.Name != "textures" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(prop.Value)
		if err != nil {
			return Skin{}, err
		}
		var data struct {
			Textures struct {
				Skin struct {
					URL      string `json:"url"`
					Metadata struct {
						Model string `json:"model"`
					} `json:"metadata"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if err = json.Unmarshal(decoded, &data); err != nil {
			return Skin{}, err
		}
		u, err := url.Parse(data.Textures.Skin.URL)
		if err != nil {
			return Skin{}, err
		}
		if u.Hostname() == "textures.minecraft.net" {
			u.Scheme = "https"
		} else {
			// Local HTTP fixtures can replace the profile origin. Production accepts
			// only the official texture host, never arbitrary links from JSON.
			base, _ := url.Parse(c.ProfileURL)
			if base == nil || base.Scheme != "http" || (base.Hostname() != "127.0.0.1" && base.Hostname() != "localhost") || u.Host != base.Host {
				return Skin{}, fmt.Errorf("untrusted official texture URL")
			}
		}
		if u.User != nil || !strings.HasPrefix(u.Path, "/texture/") {
			return Skin{}, fmt.Errorf("invalid official texture URL")
		}
		raw, err := c.fetch(ctx, u.String())
		if err != nil {
			return Skin{}, err
		}
		png, err := normalize(raw)
		if err != nil {
			return Skin{}, err
		}
		return Skin{PNG: png, Name: profile.Name, Slim: data.Textures.Skin.Metadata.Model == "slim"}, nil
	}
	return Skin{}, fmt.Errorf("profile has no skin texture")
}
