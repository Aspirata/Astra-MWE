// Package scene contains the data shared by world decoding, meshing and export.
// It has no dependency on the GUI, resource archives or file formats.
package scene

type Vec3 [3]float64

// Bounds uses inclusive block coordinates: width is MaxX-MinX+1.
// Preview renderers and GLB geometry must account for the outer edge (+1).
type Bounds struct {
	MinX int `json:"minX"`
	MaxX int `json:"maxX"`
	MinY int `json:"minY"`
	MaxY int `json:"maxY"`
	MinZ int `json:"minZ"`
	MaxZ int `json:"maxZ"`
}
type Block struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties,omitempty"`
}
type Player struct {
	Face      string     `json:"face,omitempty"`
	UUID      string     `json:"uuid"`
	Name      string     `json:"name"`
	Dimension string     `json:"dimension"`
	Position  Vec3       `json:"position"`
	Rotation  [2]float64 `json:"rotation"`
}
type WorldInfo struct {
	Path           string   `json:"path"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	DataVersion    int      `json:"dataVersion"`
	Dimensions     []string `json:"dimensions"`
	Players        []Player `json:"players"`
	Spawn          Vec3     `json:"spawn"`
	SpawnDimension string   `json:"spawnDimension"`
	MinY           int      `json:"minY"`
	MaxY           int      `json:"maxY"`
}

// Volume is sparse: an absent block is air. Biome keys are block coordinates
// aligned to multiples of four, with one sample per 4x4x4 cell.
type Volume struct {
	Bounds  Bounds
	Blocks  map[[3]int]Block
	Biomes  map[[3]int]string
	Players []Player
}
type Material struct {
	Name         string
	TextureName  string
	PNG          []byte
	EmissivePNG  []byte
	EmissiveName string
	Alpha        string
	DoubleSided  bool
}

// Primitive stores flat vertex attributes for one material. Positions/normals
// have three components, UVs two, and linear vertex colors four (RGBA).
type Primitive struct {
	Material                        int
	Positions, Normals, UVs, Colors []float32
	Indices                         []uint32
}
type Mesh struct {
	Name       string
	Primitives []Primitive
}

// Scene positions are relative to Origin, keeping float32 vertices accurate
// even when the selected region is far from the Minecraft coordinate origin.
type Scene struct {
	Meshes    []Mesh
	Materials []Material
	Origin    Vec3
	Warnings  []string
}
type MeshOptions struct {
	HollowLeaves bool
	BiomeColors  bool
	BiomeBlend   int
}
