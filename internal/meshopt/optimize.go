// Package meshopt merges coplanar, equally tinted faces with continuous
// repeating UVs on Minecraft's 1/16 grid, including snow layers and ice.
package meshopt

import (
	"astra-mwe/internal/scene"
	"context"
	"math"
	"slices"
	"strings"
)

type vertex struct {
	p, n [3]float32
	uv   [2]float32
	c    [4]float32
}
type quad [4]vertex
type key struct {
	axis, plane, sign int
	width, height     int
	winding           int
	phase             [2]int
	gradient          [4]int
	color             [4]float32
}
type cell struct{ u, v int }
type group struct {
	k     key
	cells map[cell]int
	faces []face
}
type face struct {
	cell
	source int
}
type rectangle struct {
	cell
	width, height, source int
}

func read(p *scene.Primitive, i uint32) vertex {
	v := vertex{c: [4]float32{1, 1, 1, 1}}
	copy(v.p[:], p.Positions[int(i)*3:int(i)*3+3])
	if len(p.Normals) > 0 {
		copy(v.n[:], p.Normals[int(i)*3:int(i)*3+3])
	}
	if len(p.UVs) > 0 {
		copy(v.uv[:], p.UVs[int(i)*2:int(i)*2+2])
	}
	if len(p.Colors) > 0 {
		copy(v.c[:], p.Colors[int(i)*4:int(i)*4+4])
	}
	return v
}
func appendVertex(p *scene.Primitive, v vertex) {
	p.Positions = append(p.Positions, v.p[:]...)
	p.Normals = append(p.Normals, v.n[:]...)
	p.UVs = append(p.UVs, v.uv[:]...)
	p.Colors = append(p.Colors, v.c[:]...)
}
func appendQuad(p *scene.Primitive, q quad) {
	base := uint32(len(p.Positions) / 3)
	for _, v := range q {
		appendVertex(p, v)
	}
	p.Indices = append(p.Indices, base, base+1, base+2, base, base+2, base+3)
}
func integer(v float32) (int, bool) {
	n := int(math.Round(float64(v)))
	return n, math.Abs(float64(v)-float64(n)) < 1e-6
}

func classify(q quad) (key, cell, bool) {
	// Quarter-turn block rotations leave tiny floating-point residuals.
	// Normalize only the classification copy; unmerged geometry stays intact.
	for i := range q {
		for j := 0; j < 3; j++ {
			if v, ok := integer(q[i].n[j]); ok {
				q[i].n[j] = float32(v)
			}
			if v, ok := integer(q[i].p[j] * 16); ok {
				q[i].p[j] = float32(v) / 16
			}
		}
	}
	k := key{axis: -1, color: q[0].c}
	for a := 0; a < 3; a++ {
		if q[0].n[a] == 1 || q[0].n[a] == -1 {
			k.axis = a
			k.sign = int(q[0].n[a])
		}
	}
	if k.axis < 0 {
		return k, cell{}, false
	}
	a := (k.axis + 1) % 3
	b := (k.axis + 2) % 3
	var ok bool
	k.plane, ok = integer(q[0].p[k.axis] * 16)
	if !ok {
		return k, cell{}, false
	}
	lo, hi := q[0].p, q[0].p
	for _, v := range q {
		if v.n != q[0].n || v.c != k.color || v.p[k.axis] != q[0].p[k.axis] {
			return k, cell{}, false
		}
		for j := 0; j < 3; j++ {
			if j != k.axis && v.n[j] != 0 {
				return k, cell{}, false
			}
			if _, ok := integer(v.p[j] * 16); !ok {
				return k, cell{}, false
			}
			lo[j] = min(lo[j], v.p[j])
			hi[j] = max(hi[j], v.p[j])
		}
		for _, uv := range v.uv {
			if _, ok := integer(uv * 16); !ok {
				return k, cell{}, false
			}
		}
	}
	k.width, k.height = int((hi[a]-lo[a])*16), int((hi[b]-lo[b])*16)
	if k.width < 1 || k.height < 1 {
		return k, cell{}, false
	}
	seen := 0
	var corners [4]int
	for i, v := range q {
		if (v.p[a] != lo[a] && v.p[a] != hi[a]) || (v.p[b] != lo[b] && v.p[b] != hi[b]) {
			return k, cell{}, false
		}
		if v.p[a] == hi[a] {
			corners[i] |= 1
		}
		if v.p[b] == hi[b] {
			corners[i] |= 2
		}
		seen |= 1 << corners[i]
	}
	if seen != 15 {
		return k, cell{}, false
	}
	for i := range corners {
		if edge := corners[i] ^ corners[(i+1)%4]; edge != 1 && edge != 2 {
			return k, cell{}, false // Bow-ties are not rectangle surfaces.
		}
	}
	k.winding = 1
	if (q[1].p[a]-q[0].p[a])*(q[2].p[b]-q[0].p[b])-(q[1].p[b]-q[0].p[b])*(q[2].p[a]-q[0].p[a]) < 0 {
		k.winding = -1
	}
	// Determine the two UV gradients and validate the affine map at all corners.
	for _, v := range q[1:] {
		da, db := v.p[a]-q[0].p[a], v.p[b]-q[0].p[b]
		if da != 0 && db == 0 {
			for i := 0; i < 2; i++ {
				k.gradient[i] = int((v.uv[i] - q[0].uv[i]) / da)
			}
		}
		if db != 0 && da == 0 {
			for i := 0; i < 2; i++ {
				k.gradient[i+2] = int((v.uv[i] - q[0].uv[i]) / db)
			}
		}
	}
	if abs(k.gradient[0])+abs(k.gradient[1]) != 1 || abs(k.gradient[2])+abs(k.gradient[3]) != 1 || k.gradient[0]*k.gradient[2]+k.gradient[1]*k.gradient[3] != 0 {
		return k, cell{}, false
	}
	for _, v := range q {
		for i := 0; i < 2; i++ {
			want := q[0].uv[i] + float32(k.gradient[i])*(v.p[a]-q[0].p[a]) + float32(k.gradient[i+2])*(v.p[b]-q[0].p[b])
			if v.uv[i] != want {
				return k, cell{}, false
			}
		}
	}
	for i := 0; i < 2; i++ {
		phase := q[0].uv[i]*16 - float32(k.gradient[i])*q[0].p[a]*16 - float32(k.gradient[i+2])*q[0].p[b]*16
		k.phase[i] = ((int(math.Round(float64(phase))) % 16) + 16) % 16
	}
	return k, cell{int(lo[a] * 16), int(lo[b] * 16)}, true
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// greedy covers equal-sized tiles without subdividing them. The source mesh
// stays immutable; small integer maps and a visited bit per face replace maps
// containing complete copies of every vertex attribute.
func greedy(ctx context.Context, g *group, vertical bool) ([]rectangle, error) {
	order := make([]int, len(g.faces))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int {
		a, b := g.faces[i], g.faces[j]
		if vertical {
			if a.u != b.u {
				return cmpInt(a.u, b.u)
			}
			return cmpInt(a.v, b.v)
		}
		if a.v != b.v {
			return cmpInt(a.v, b.v)
		}
		return cmpInt(a.u, b.u)
	})
	used := make([]bool, len(g.faces))
	var out []rectangle
	checks := 0
	available := func(c cell) bool { id, ok := g.cells[c]; return ok && !used[id] }
	for oi, id := range order {
		if oi%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if used[id] {
			continue
		}
		f := g.faces[id]
		du, dv := cell{g.k.width, 0}, cell{0, g.k.height}
		if vertical {
			du, dv = dv, du
		}
		w, h := 1, 1
		for available(cell{f.u + w*du.u, f.v + w*du.v}) {
			w++
			checks++
			if checks%1024 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
		}
		for {
			full := true
			for x := 0; x < w; x++ {
				checks++
				if checks%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				if !available(cell{f.u + x*du.u + h*dv.u, f.v + x*du.v + h*dv.v}) {
					full = false
					break
				}
			}
			if !full {
				break
			}
			h++
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				used[g.cells[cell{f.u + x*du.u + y*dv.u, f.v + x*du.v + y*dv.v}]] = true
			}
		}
		if vertical {
			w, h = h, w
		}
		out = append(out, rectangle{f.cell, w * g.k.width, h * g.k.height, f.source})
	}
	return out, nil
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Coalesce rectangles sharing a complete edge. Unlike a per-grid grouping,
// this also joins snow layers, slabs and previously optimized repeating faces.
// Overlapping rectangles are never discarded: each merge preserves coverage
// multiplicity because only disjoint, exactly adjacent interiors are joined.
func coalesce(ctx context.Context, rects []rectangle) ([]rectangle, error) {
	if len(rects) < 2 {
		return rects, nil
	}
	for {
		previous := len(rects)
		for _, vertical := range []bool{false, true} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			slices.SortFunc(rects, func(a, b rectangle) int {
				if vertical {
					if a.u != b.u {
						return cmpInt(a.u, b.u)
					}
					if a.width != b.width {
						return cmpInt(a.width, b.width)
					}
					if a.v != b.v {
						return cmpInt(a.v, b.v)
					}
				} else {
					if a.v != b.v {
						return cmpInt(a.v, b.v)
					}
					if a.height != b.height {
						return cmpInt(a.height, b.height)
					}
					if a.u != b.u {
						return cmpInt(a.u, b.u)
					}
				}
				return cmpInt(a.source, b.source)
			})
			n := 0
			for i, r := range rects {
				if i%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				if n > 0 {
					last := &rects[n-1]
					if vertical && last.u == r.u && last.width == r.width && last.v+last.height == r.v {
						last.height += r.height
						continue
					}
					if !vertical && last.v == r.v && last.height == r.height && last.u+last.width == r.u {
						last.width += r.width
						continue
					}
				}
				rects[n] = r
				n++
			}
			rects = rects[:n]
		}
		if len(rects) == previous {
			return rects, nil
		}
	}
}

func sourceQuad(p *scene.Primitive, i int) quad {
	return quad{read(p, p.Indices[i]), read(p, p.Indices[i+1]), read(p, p.Indices[i+2]), read(p, p.Indices[i+5])}
}

func extendQuad(p *scene.Primitive, k key, r rectangle) quad {
	q := sourceQuad(p, r.source)
	a, b := (k.axis+1)%3, (k.axis+2)%3
	for i := range q {
		old := q[i].p
		if q[i].p[a] > float32(r.u)/16+1e-6 {
			q[i].p[a] = float32(r.u+r.width) / 16
		}
		if q[i].p[b] > float32(r.v)/16+1e-6 {
			q[i].p[b] = float32(r.v+r.height) / 16
		}
		for j := 0; j < 2; j++ {
			q[i].uv[j] += float32(k.gradient[j])*(q[i].p[a]-old[a]) + float32(k.gradient[j+2])*(q[i].p[b]-old[b])
		}
	}
	return q
}

type indexedOutput struct {
	p   scene.Primitive
	ids map[vertex]uint32
}

func newOutput(material, vertices, indices int) indexedOutput {
	return indexedOutput{p: scene.Primitive{Material: material, Positions: make([]float32, 0, vertices*3), Normals: make([]float32, 0, vertices*3), UVs: make([]float32, 0, vertices*2), Colors: make([]float32, 0, vertices*4), Indices: make([]uint32, 0, indices)}, ids: make(map[vertex]uint32, vertices)}
}
func (o *indexedOutput) vertex(v vertex) uint32 {
	if id, ok := o.ids[v]; ok {
		return id
	}
	id := uint32(len(o.p.Positions) / 3)
	o.ids[v] = id
	appendVertex(&o.p, v)
	return id
}
func (o *indexedOutput) quad(q quad) {
	var ids [4]uint32
	for i, v := range q {
		ids[i] = o.vertex(v)
	}
	o.p.Indices = append(o.p.Indices, ids[0], ids[1], ids[2], ids[0], ids[2], ids[3])
}

func Optimize(ctx context.Context, sc *scene.Scene) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for mi := range sc.Meshes {
		for pi := range sc.Meshes[mi].Primitives {
			p := &sc.Meshes[mi].Primitives[pi]
			if strings.HasPrefix(sc.Meshes[mi].Name, "Player /") || len(p.Normals) == 0 || len(p.UVs) == 0 {
				if err := deduplicatePrimitive(ctx, p); err != nil {
					return err
				}
				continue
			}
			groups := map[key]*group{}
			var ordered []*group
			var unsupported []uint32
			for i := 0; i < len(p.Indices); {
				if i%1536 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				if i+5 < len(p.Indices) && p.Indices[i] == p.Indices[i+3] && p.Indices[i+2] == p.Indices[i+4] {
					if k, c, ok := classify(sourceQuad(p, i)); ok {
						g := groups[k]
						if g == nil {
							g = &group{k: k, cells: map[cell]int{}}
							groups[k] = g
							ordered = append(ordered, g)
						}
						if _, exists := g.cells[c]; !exists {
							g.cells[c] = len(g.faces)
							g.faces = append(g.faces, face{c, i})
						} else {
							unsupported = append(unsupported, p.Indices[i:i+6]...)
						}
						i += 6
						continue
					}
				}
				unsupported = append(unsupported, p.Indices[i:i+3]...)
				i += 3
			}
			merged := map[key][]rectangle{}
			var keys []key
			for _, g := range ordered {
				rects, err := greedy(ctx, g, false)
				if err != nil {
					return err
				}
				// One or two rectangles are already minimal. Disconnected singleton
				// faces cannot benefit from changing the sweep direction either.
				if len(rects) > 2 && len(rects) < len(g.faces) {
					alternative, err := greedy(ctx, g, true)
					if err != nil {
						return err
					}
					if len(alternative) < len(rects) {
						rects = alternative
					}
				}
				k := g.k
				k.width, k.height = 0, 0
				if _, ok := merged[k]; !ok {
					keys = append(keys, k)
				}
				merged[k] = append(merged[k], rects...)
			}
			count := 0
			for _, k := range keys {
				rects, err := coalesce(ctx, merged[k])
				if err != nil {
					return err
				}
				merged[k] = rects
				count += len(rects)
			}
			out := newOutput(p.Material, min(len(p.Positions)/3, count*4+len(unsupported)), count*6+len(unsupported))
			for i, idx := range unsupported {
				if i%1536 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				out.p.Indices = append(out.p.Indices, out.vertex(read(p, idx)))
			}
			for _, k := range keys {
				for i, r := range merged[k] {
					if i%256 == 0 {
						if err := ctx.Err(); err != nil {
							return err
						}
					}
					out.quad(extendQuad(p, k, r))
				}
			}
			*p = out.p
		}
	}
	return ctx.Err()
}

// Deduplicate reuses only vertices whose complete attributes match, preserving
// every triangle. Call DeduplicateContext in cancellable export/preview jobs.
func Deduplicate(sc *scene.Scene) { _ = DeduplicateContext(context.Background(), sc) }
func DeduplicateContext(ctx context.Context, sc *scene.Scene) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for mi := range sc.Meshes {
		for pi := range sc.Meshes[mi].Primitives {
			if err := deduplicatePrimitive(ctx, &sc.Meshes[mi].Primitives[pi]); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
func deduplicatePrimitive(ctx context.Context, p *scene.Primitive) error {
	out := newOutput(p.Material, min(len(p.Positions)/3, len(p.Indices)), len(p.Indices))
	for i, idx := range p.Indices {
		if i%1536 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		out.p.Indices = append(out.p.Indices, out.vertex(read(p, idx)))
	}
	*p = out.p
	return ctx.Err()
}
