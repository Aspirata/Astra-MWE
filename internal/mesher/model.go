package mesher

import (
	"astra-mwe/internal/assets"
	"astra-mwe/internal/scene"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

type face struct {
	UV        []float64 `json:"uv"`
	Texture   string    `json:"texture"`
	Cullface  string    `json:"cullface"`
	Rotation  int       `json:"rotation"`
	TintIndex *int      `json:"tintindex"`
}
type rotation struct {
	Origin  [3]float64 `json:"origin"`
	Axis    string     `json:"axis"`
	Angle   float64    `json:"angle"`
	Rescale bool       `json:"rescale"`
}
type element struct {
	From     [3]float64      `json:"from"`
	To       [3]float64      `json:"to"`
	Rotation *rotation       `json:"rotation"`
	Faces    map[string]face `json:"faces"`
}
type model struct {
	Parent   string                `json:"parent"`
	Textures map[string]textureRef `json:"textures"`
	Elements []element             `json:"elements"`
	Loader   string                `json:"loader"`
}

// Newer client models allow a sprite object to override its render mode.
type textureRef struct {
	Sprite           string `json:"sprite"`
	ForceTranslucent bool   `json:"force_translucent"`
}

func (t *textureRef) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &t.Sprite)
	}
	type plain textureRef
	return json.Unmarshal(data, (*plain)(t))
}

type variant struct {
	Model  string `json:"model"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	UVLock bool   `json:"uvlock"`
	Weight int    `json:"weight"`
}
type part struct {
	When  map[string]json.RawMessage `json:"when"`
	Apply json.RawMessage            `json:"apply"`
}
type blockstate struct {
	Variants  map[string]json.RawMessage `json:"variants"`
	Multipart []part                     `json:"multipart"`
}
type resolved struct {
	model     *model
	transform variant
}
type modelResult struct {
	value *model
	err   error
}
type stateResult struct {
	value *blockstate
	err   error
}
type resolver struct {
	assets *assets.Stack
	models map[string]modelResult
	states map[string]stateResult
	plans  map[string][]*resolutionPlan
}

type resolutionGroup struct {
	choices variantChoices
	offset  uint64
}
type resolutionPlan struct {
	properties map[string]string
	groups     []resolutionGroup
	static     bool
	ready      bool
	resolved   []resolved
	err        error
}
type variantChoices struct {
	values   []variant
	weighted bool
	total    uint64
	err      error
}

func resource(id, kind, ext string) string {
	ns, n := splitID(id)
	return "assets/" + ns + "/" + kind + "/" + n + ext
}
func splitID(id string) (string, string) {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "minecraft", id
}
func (r *resolver) loadModel(id string, chain map[string]bool) (*model, error) {
	if c, ok := r.models[id]; ok {
		return c.value, c.err
	}
	if chain[id] || len(chain) >= 64 {
		return nil, fmt.Errorf("model inheritance cycle or depth limit at %s", id)
	}
	chain[id] = true
	defer delete(chain, id)
	b, e := r.assets.Read(resource(id, "models", ".json"))
	if e != nil {
		return nil, e
	}
	m := &model{}
	if e = json.Unmarshal(b, m); e != nil {
		return nil, fmt.Errorf("model %s: %w", id, e)
	}
	if m.Loader != "" && m.Loader != "minecraft:elements" {
		return nil, fmt.Errorf("unsupported model loader %q in %s", m.Loader, id)
	}
	if m.Parent != "" {
		if strings.Contains(m.Parent, "builtin/") {
			return nil, fmt.Errorf("unsupported built-in model %q", m.Parent)
		}
		parent, e := r.loadModel(m.Parent, chain)
		if e != nil {
			return nil, e
		}
		textures := map[string]textureRef{}
		for k, v := range parent.Textures {
			textures[k] = v
		}
		for k, v := range m.Textures {
			textures[k] = v
		}
		m.Textures = textures
		if m.Elements == nil {
			m.Elements = parent.Elements
		}
	}
	r.models[id] = modelResult{value: m}
	return m, nil
}
func (r *resolver) state(name string) (*blockstate, error) {
	if c, ok := r.states[name]; ok {
		return c.value, c.err
	}
	b, e := r.assets.Read(resource(name, "blockstates", ".json"))
	s := &blockstate{}
	if e == nil {
		e = json.Unmarshal(b, s)
	}
	r.states[name] = stateResult{s, e}
	return s, e
}
func (r *resolver) resolve(b scene.Block, pos [3]int) ([]resolved, error) {
	plan, e := r.plan(b)
	if e != nil {
		return nil, e
	}
	if plan.static && plan.ready {
		return plan.resolved, plan.err
	}
	out, e := r.resolvePlan(b.Name, pos, plan)
	if plan.static {
		plan.resolved, plan.err, plan.ready = out, e, true
	}
	return out, e
}

// Palette entries repeat throughout a chunk. Match their properties and decode
// weighted alternatives once, retaining the original position-dependent choice.
func (r *resolver) plan(b scene.Block) (*resolutionPlan, error) {
	for _, p := range r.plans[b.Name] {
		if len(p.properties) != len(b.Properties) {
			continue
		}
		match := true
		for key, value := range p.properties {
			if other, ok := b.Properties[key]; !ok || other != value {
				match = false
				break
			}
		}
		if match {
			return p, nil
		}
	}
	s, e := r.state(b.Name)
	if e != nil {
		return nil, e
	}
	p := &resolutionPlan{static: true, properties: make(map[string]string, len(b.Properties))}
	for key, value := range b.Properties {
		p.properties[key] = value
	}
	keys := make([]string, 0, len(s.Variants))
	for k := range s.Variants {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ci, cj := strings.Count(keys[i], "="), strings.Count(keys[j], "=")
		if ci != cj {
			return ci > cj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		if variantMatches(k, b.Properties) {
			p.groups = append(p.groups, resolutionGroup{choices: parseChoices(s.Variants[k])})
			break
		}
	}
	for i, part := range s.Multipart {
		if condition(part.When, b.Properties) {
			// The salt includes unmatched parts, just as in the client selection.
			p.groups = append(p.groups, resolutionGroup{parseChoices(part.Apply), uint64(i) * 0x9e3779b97f4a7c15})
		}
	}
	for _, group := range p.groups {
		if len(group.choices.values) > 1 {
			p.static = false
		}
	}
	if r.plans == nil {
		r.plans = map[string][]*resolutionPlan{}
	}
	r.plans[b.Name] = append(r.plans[b.Name], p)
	return p, nil
}

func (r *resolver) resolvePlan(name string, pos [3]int, plan *resolutionPlan) ([]resolved, error) {
	if len(plan.groups) == 0 {
		return nil, fmt.Errorf("no matching blockstate for %s", name)
	}
	seed := positionSeed(name, pos)
	// Resolve choice errors before model errors, preserving warning precedence.
	for _, group := range plan.groups {
		if _, e := group.choices.choose(seed + group.offset); e != nil {
			return nil, e
		}
	}
	out := make([]resolved, 0, len(plan.groups))
	elements := 0
	for _, group := range plan.groups {
		v, e := group.choices.choose(seed + group.offset)
		if e != nil {
			return nil, e
		}
		if v.X%90 != 0 || v.Y%90 != 0 {
			return nil, fmt.Errorf("invalid blockstate rotation in %s", name)
		}
		m, e := r.loadModel(v.Model, map[string]bool{})
		if e != nil {
			return nil, e
		}
		out = append(out, resolved{m, v})
		elements += len(m.Elements)
	}
	if elements == 0 {
		return nil, fmt.Errorf("no static JSON geometry for %s; special renderer adapter required", name)
	}
	return out, nil
}
func positionSeed(name string, p [3]int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	var digits [21]byte // slash plus a signed 64-bit coordinate
	for _, n := range p {
		digits[0] = '/'
		h.Write(strconv.AppendInt(digits[:1], int64(n), 10))
	}
	return h.Sum64()
}
func choose(raw json.RawMessage, seed uint64) (variant, error) {
	return parseChoices(raw).choose(seed)
}
func parseChoices(raw json.RawMessage) variantChoices {
	if len(raw) == 0 {
		return variantChoices{err: fmt.Errorf("missing blockstate model")}
	}
	if raw[0] != '[' {
		var v variant
		e := json.Unmarshal(raw, &v)
		if e == nil && v.Model == "" {
			e = fmt.Errorf("empty blockstate model")
		}
		return variantChoices{values: []variant{v}, err: e}
	}
	var vs []variant
	if e := json.Unmarshal(raw, &vs); e != nil {
		return variantChoices{err: e}
	}
	total := uint64(0)
	for _, x := range vs {
		if x.Weight <= 0 {
			x.Weight = 1
		}
		total += uint64(x.Weight)
	}
	if total == 0 {
		return variantChoices{err: fmt.Errorf("empty weighted variants")}
	}
	return variantChoices{values: vs, weighted: true, total: total}
}
func (c variantChoices) choose(seed uint64) (variant, error) {
	if c.err != nil {
		return variant{}, c.err
	}
	if !c.weighted {
		return c.values[0], nil
	}
	pick := seed % c.total
	for _, x := range c.values {
		w := x.Weight
		if w <= 0 {
			w = 1
		}
		if pick < uint64(w) {
			if x.Model == "" {
				return variant{}, fmt.Errorf("empty blockstate model")
			}
			return x, nil
		}
		pick -= uint64(w)
	}
	return variant{}, fmt.Errorf("invalid weighted variants")
}
func variantMatches(key string, props map[string]string) bool {
	if key == "" {
		return true
	}
	for _, pair := range strings.Split(key, ",") {
		p := strings.SplitN(pair, "=", 2)
		if len(p) != 2 || !valueMatches(props[p[0]], p[1]) {
			return false
		}
	}
	return true
}
func valueMatches(value, want string) bool {
	neg := strings.HasPrefix(want, "!")
	want = strings.TrimPrefix(want, "!")
	match := false
	for _, w := range strings.Split(want, "|") {
		if w == value {
			match = true
		}
	}
	if neg {
		return !match
	}
	return match
}
func condition(when map[string]json.RawMessage, props map[string]string) bool {
	for k, raw := range when {
		if k == "OR" || k == "AND" {
			var children []map[string]json.RawMessage
			if json.Unmarshal(raw, &children) != nil {
				return false
			}
			yes := k == "AND"
			for _, c := range children {
				match := condition(c, props)
				if k == "OR" {
					yes = yes || match
				} else {
					yes = yes && match
				}
			}
			if !yes {
				return false
			}
		} else {
			var v string
			if json.Unmarshal(raw, &v) != nil || !valueMatches(props[k], v) {
				return false
			}
		}
	}
	return true
}
func textureID(m *model, value string) (string, bool, error) {
	seen := map[string]bool{}
	translucent := false
	for strings.HasPrefix(value, "#") {
		key := strings.TrimPrefix(value, "#")
		if seen[key] {
			return "", false, fmt.Errorf("cyclic texture reference #%s", key)
		}
		seen[key] = true
		v, ok := m.Textures[key]
		if !ok {
			return "", false, fmt.Errorf("missing texture reference #%s", key)
		}
		value = v.Sprite
		translucent = translucent || v.ForceTranslucent
	}
	if value == "" {
		return "", false, fmt.Errorf("empty texture reference")
	}
	return value, translucent, nil
}
