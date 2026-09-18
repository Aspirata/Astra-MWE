package mesher

import (
	"encoding/json"
	"strings"
)

func (b *builder) leaf(name string) bool { return leaf(name) || b.leaves[name] }

func (b *builder) loadLeaves() map[string]bool {
	cache := map[string]map[string]bool{}
	visiting := map[string]bool{}
	var load func(string) map[string]bool
	load = func(id string) map[string]bool {
		if cached, ok := cache[id]; ok {
			return cached
		}
		out := map[string]bool{}
		if visiting[id] || len(visiting) > 64 {
			b.warn("Cyclic or too deeply nested leaf tag: " + id)
			return out
		}
		visiting[id] = true
		defer delete(visiting, id)
		ns, n := splitID(id)
		for _, folder := range []string{"blocks", "block"} {
			layers, err := b.assets.ReadLayers("data/" + ns + "/tags/" + folder + "/" + n + ".json")
			if err != nil {
				b.warn("Cannot read leaf tag " + id + ": " + err.Error())
				continue
			}
			for _, raw := range layers {
				var tag struct {
					Replace bool              `json:"replace"`
					Values  []json.RawMessage `json:"values"`
				}
				if err := json.Unmarshal(raw, &tag); err != nil {
					b.warn("Invalid leaf tag " + id + ": " + err.Error())
					continue
				}
				if tag.Replace {
					out = map[string]bool{}
				}
				for _, rawValue := range tag.Values {
					var value string
					if json.Unmarshal(rawValue, &value) != nil {
						var entry struct {
							ID string `json:"id"`
						}
						if json.Unmarshal(rawValue, &entry) != nil {
							continue
						}
						value = entry.ID
					}
					if value == "" {
						continue
					}
					if strings.HasPrefix(value, "#") {
						for child := range load(strings.TrimPrefix(value, "#")) {
							out[child] = true
						}
					} else {
						ns, n := splitID(value)
						out[ns+":"+n] = true
					}
				}
			}
		}
		cache[id] = out
		return out
	}
	return load("minecraft:leaves")
}
