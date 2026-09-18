package mesher

import "math"

var directions = []string{"down", "up", "north", "south", "west", "east"}
var offsets = map[string][3]int{"down": {0, -1, 0}, "up": {0, 1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0}}

type point [3]float64

func corners(e element, dir string) [4]point {
	x, y, z := e.From[0]/16, e.From[1]/16, e.From[2]/16
	X, Y, Z := e.To[0]/16, e.To[1]/16, e.To[2]/16
	switch dir {
	case "up":
		return [4]point{{x, Y, z}, {x, Y, Z}, {X, Y, Z}, {X, Y, z}}
	case "down":
		return [4]point{{x, y, Z}, {x, y, z}, {X, y, z}, {X, y, Z}}
	case "north":
		return [4]point{{X, y, z}, {x, y, z}, {x, Y, z}, {X, Y, z}}
	case "south":
		return [4]point{{x, y, Z}, {X, y, Z}, {X, Y, Z}, {x, Y, Z}}
	case "west":
		return [4]point{{x, y, z}, {x, y, Z}, {x, Y, Z}, {x, Y, z}}
	default:
		return [4]point{{X, y, Z}, {X, y, z}, {X, Y, z}, {X, Y, Z}}
	}
}
func rotate(p point, axis string, degrees float64) point {
	s, c := math.Sincos(degrees * math.Pi / 180)
	switch axis {
	case "x":
		return point{p[0], p[1]*c - p[2]*s, p[1]*s + p[2]*c}
	case "y":
		return point{p[0]*c + p[2]*s, p[1], -p[0]*s + p[2]*c}
	default:
		return point{p[0]*c - p[1]*s, p[0]*s + p[1]*c, p[2]}
	}
}
func blockRotate(p point, v variant) point {
	p = rotate(p, "x", float64(-v.X))
	return rotate(p, "y", float64(-v.Y))
}
func transform(p point, e element, v variant) point {
	if e.Rotation != nil {
		r := e.Rotation
		o := point{r.Origin[0] / 16, r.Origin[1] / 16, r.Origin[2] / 16}
		for i := range p {
			p[i] -= o[i]
		}
		p = rotate(p, r.Axis, r.Angle)
		if r.Rescale {
			scale := 1 / math.Cos(r.Angle*math.Pi/180)
			for i, axis := range []string{"x", "y", "z"} {
				if axis != r.Axis {
					p[i] *= scale
				}
			}
		}
		for i := range p {
			p[i] += o[i]
		}
	}
	for i := range p {
		p[i] -= .5
	}
	p = blockRotate(p, v)
	for i := range p {
		p[i] += .5
	}
	return p
}
func rotatedDirection(dir string, v variant) string {
	d, ok := offsets[dir]
	if !ok {
		return ""
	}
	p := blockRotate(point{float64(d[0]), float64(d[1]), float64(d[2])}, v)
	for k, x := range offsets {
		if int(math.Round(p[0])) == x[0] && int(math.Round(p[1])) == x[1] && int(math.Round(p[2])) == x[2] {
			return k
		}
	}
	return ""
}
func normal(q [4]point) point {
	a, b := point{}, point{}
	for i := range a {
		a[i] = q[1][i] - q[0][i]
		b[i] = q[2][i] - q[0][i]
	}
	n := point{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
	if l > 0 {
		for i := range n {
			n[i] /= l
		}
	}
	return n
}
func defaultUV(e element, dir string) [4]float64 {
	x, y, z := e.From[0], e.From[1], e.From[2]
	X, Y, Z := e.To[0], e.To[1], e.To[2]
	switch dir {
	case "down":
		return [4]float64{x, 16 - Z, X, 16 - z}
	case "up":
		return [4]float64{x, z, X, Z}
	case "north":
		return [4]float64{16 - X, 16 - Y, 16 - x, 16 - y}
	case "south":
		return [4]float64{x, 16 - Y, X, 16 - y}
	case "west":
		return [4]float64{z, 16 - Y, Z, 16 - y}
	default:
		return [4]float64{16 - Z, 16 - Y, 16 - z, 16 - y}
	}
}
func faceUV(e element, dir string, f face, v variant) [4][2]float64 {
	uv := defaultUV(e, dir)
	if len(f.UV) == 4 {
		copy(uv[:], f.UV)
	}
	q := [4][2]float64{{uv[0] / 16, uv[3] / 16}, {uv[2] / 16, uv[3] / 16}, {uv[2] / 16, uv[1] / 16}, {uv[0] / 16, uv[1] / 16}}
	uvDir := dir
	if v.UVLock {
		uvDir = rotatedDirection(dir, v)
	}
	if uvDir == "up" || uvDir == "down" {
		q = [4][2]float64{{uv[0] / 16, uv[1] / 16}, {uv[0] / 16, uv[3] / 16}, {uv[2] / 16, uv[3] / 16}, {uv[2] / 16, uv[1] / 16}}
	}
	steps := ((f.Rotation/90)%4 + 4) % 4
	out := q
	for i := range out {
		out[i] = q[(i+steps)%4]
	}
	if v.UVLock && (v.X != 0 || v.Y != 0) { // Recover world face orientation without changing the selected texture rectangle.
		original := corners(element{To: [3]float64{16, 16, 16}}, dir)
		target := corners(element{To: [3]float64{16, 16, 16}}, rotatedDirection(dir, v))
		for i, p := range original {
			tp := transform(p, element{}, v)
			best, dist := 0, math.MaxFloat64
			for j, t := range target {
				d := 0.0
				for c := 0; c < 3; c++ {
					d += (tp[c] - t[c]) * (tp[c] - t[c])
				}
				if d < dist {
					best, dist = j, d
				}
			}
			out[i] = q[(best+steps)%4]
		}
	}
	return out
}
