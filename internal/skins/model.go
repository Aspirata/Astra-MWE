package skins

import (
	"astra-mwe/internal/scene"
	"math"
)

func AddPlayer(s *scene.Scene, p scene.Player, skin Skin) {
	mi := len(s.Materials)
	s.Materials = append(s.Materials, scene.Material{Name: "player", TextureName: "skin.png", PNG: skin.PNG, Alpha: "MASK"})
	name := p.Name
	if name == "" {
		name = p.UUID
	}
	mesh := scene.Mesh{Name: "Player / " + name}
	arm := 4.
	if skin.Slim {
		arm = 3
	}
	type part struct{ x, y, z, w, h, d, u, v float64 }
	parts := []part{{-4, 24, -4, 8, 8, 8, 0, 0}, {-4, 12, -2, 8, 12, 4, 16, 16}, {-4, 0, -2, 4, 12, 4, 0, 16}, {0, 0, -2, 4, 12, 4, 16, 48}, {-4 - arm, 12, -2, arm, 12, 4, 40, 16}, {4, 12, -2, arm, 12, 4, 32, 48}}
	overlays := [][2]float64{{32, 0}, {16, 32}, {0, 32}, {0, 48}, {40, 32}, {48, 48}}
	yaw := math.Pi + p.Rotation[0]*math.Pi/180
	cs, sn := math.Cos(yaw), math.Sin(yaw)
	for layer := 0; layer < 2; layer++ {
		for index, b := range parts {
			inflate := 0.
			if layer == 1 {
				inflate = .25
				b.u = overlays[index][0]
				b.v = overlays[index][1]
			}
			x0, y0, z0 := b.x-inflate, math.Max(0, b.y-inflate), b.z-inflate
			x1, y1, z1 := b.x+b.w+inflate, b.y+b.h+inflate, b.z+b.d+inflate
			// Each face is counterclockwise when viewed from outside, UV unwrap uses the skin's cuboid net.
			faces := [][4][3]float64{
				{{x0, y0, z0}, {x0, y0, z1}, {x0, y1, z1}, {x0, y1, z0}},
				{{x1, y0, z0}, {x0, y0, z0}, {x0, y1, z0}, {x1, y1, z0}},
				{{x1, y0, z1}, {x1, y0, z0}, {x1, y1, z0}, {x1, y1, z1}},
				{{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1}},
				{{x0, y1, z1}, {x1, y1, z1}, {x1, y1, z0}, {x0, y1, z0}},
				{{x0, y0, z0}, {x1, y0, z0}, {x1, y0, z1}, {x0, y0, z1}},
			}
			normals := [][3]float64{{-1, 0, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, -1, 0}}
			rects := [][4]float64{{b.u, b.v + b.d, b.d, b.h}, {b.u + b.d, b.v + b.d, b.w, b.h}, {b.u + b.d + b.w, b.v + b.d, b.d, b.h}, {b.u + 2*b.d + b.w, b.v + b.d, b.w, b.h}, {b.u + b.d, b.v, b.w, b.d}, {b.u + b.d + b.w, b.v, b.w, b.d}}
			q := scene.Primitive{Material: mi}
			for f, vs := range faces {
				r := rects[f]
				uv := [][2]float64{{r[0] / 64, (r[1] + r[3]) / 64}, {(r[0] + r[2]) / 64, (r[1] + r[3]) / 64}, {(r[0] + r[2]) / 64, r[1] / 64}, {r[0] / 64, r[1] / 64}}
				if index == 0 && (f == 0 || f == 2) {
					for j := range uv {
						uv[j][0] = (2*r[0]+r[2])/64 - uv[j][0]
					}
				}
				n := normals[f]
				base := uint32(len(q.Positions) / 3)
				for j, v := range vs {
					scale := .9375 / 16
					q.Positions = append(q.Positions, float32(p.Position[0]-s.Origin[0]+(v[0]*cs-v[2]*sn)*scale), float32(p.Position[1]-s.Origin[1]+v[1]*scale), float32(p.Position[2]-s.Origin[2]+(v[0]*sn+v[2]*cs)*scale))
					q.Normals = append(q.Normals, float32(n[0]*cs-n[2]*sn), float32(n[1]), float32(n[0]*sn+n[2]*cs))
					q.UVs = append(q.UVs, float32(uv[j][0]), float32(uv[j][1]))
					q.Colors = append(q.Colors, 1, 1, 1, 1)
				}
				q.Indices = append(q.Indices, base, base+1, base+2, base, base+2, base+3)
			}
			mesh.Primitives = append(mesh.Primitives, q)
		}
	}
	s.Meshes = append(s.Meshes, mesh)
}
