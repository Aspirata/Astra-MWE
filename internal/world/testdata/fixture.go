// Package fixture creates tiny, valid Anvil saves for integration tests.
// These are synthetic fixtures, not saves produced by a running Minecraft client.
package fixture

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type List struct {
	Type   byte
	Values []any
}
type Compound = map[string]any

func Encode(c Compound) []byte {
	var b bytes.Buffer
	b.Write([]byte{10, 0, 0})
	payload(&b, c)
	return b.Bytes()
}
func kind(v any) byte {
	switch v.(type) {
	case int8:
		return 1
	case int16:
		return 2
	case int32:
		return 3
	case int64:
		return 4
	case float32:
		return 5
	case float64:
		return 6
	case []byte:
		return 7
	case string:
		return 8
	case List:
		return 9
	case Compound:
		return 10
	case []int32:
		return 11
	case []int64:
		return 12
	}
	panic(fmt.Sprintf("unknown fixture type %T", v))
}
func str(b *bytes.Buffer, s string) {
	binary.Write(b, binary.BigEndian, uint16(len(s)))
	b.WriteString(s)
}
func payload(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case string:
		str(b, x)
	case Compound:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteByte(kind(x[k]))
			str(b, k)
			payload(b, x[k])
		}
		b.WriteByte(0)
	case List:
		b.WriteByte(x.Type)
		binary.Write(b, binary.BigEndian, int32(len(x.Values)))
		for _, v := range x.Values {
			payload(b, v)
		}
	case []byte:
		binary.Write(b, binary.BigEndian, int32(len(x)))
		b.Write(x)
	case []int32:
		binary.Write(b, binary.BigEndian, int32(len(x)))
		binary.Write(b, binary.BigEndian, x)
	case []int64:
		binary.Write(b, binary.BigEndian, int32(len(x)))
		binary.Write(b, binary.BigEndian, x)
	default:
		binary.Write(b, binary.BigEndian, v)
	}
}

// World creates a save with chunk (-1,-1), blocks at (-1,sectionY*16,-1)
// and (-16,sectionY*16+1,-16), and one saved player in the Nether.
// format is a Java data version; compression is 1 gzip, 2 zlib, or 3 raw.
func World(path string, format int, compression byte) error {
	if err := os.MkdirAll(filepath.Join(path, "region"), 0755); err != nil {
		return err
	}
	player := Compound{"UUID": []int32{0x12345678, 0x12341234, 0x12341234, 0x12345678}, "Pos": List{6, []any{float64(-1.5), float64(65), float64(-2.5)}}, "Rotation": List{5, []any{float32(90), float32(20)}}, "Dimension": "minecraft:the_nether"}
	data := Compound{"Data": Compound{"LevelName": "Fixture world", "DataVersion": int32(format), "Version": Compound{"Name": fmt.Sprintf("fixture-%d", format)}, "SpawnX": int32(-2), "SpawnY": int32(64), "SpawnZ": int32(3), "Player": player}}
	chunkPath := path
	if format >= 4790 {
		meta := data["Data"].(Compound)
		delete(meta, "Player")
		delete(meta, "SpawnX")
		delete(meta, "SpawnY")
		delete(meta, "SpawnZ")
		meta["singleplayer_uuid"] = player["UUID"]
		meta["spawn"] = Compound{"dimension": "minecraft:overworld", "pos": []int32{-2, 64, 3}, "yaw": float32(0), "pitch": float32(0)}
		if err := GzipNBT(filepath.Join(path, "players", "data", "12345678-1234-1234-1234-123412345678.dat"), player); err != nil {
			return err
		}
		chunkPath = filepath.Join(path, "dimensions", "minecraft", "overworld")
	}
	var level bytes.Buffer
	g := gzip.NewWriter(&level)
	g.Write(Encode(data))
	g.Close()
	if err := os.WriteFile(filepath.Join(path, "level.dat"), level.Bytes(), 0644); err != nil {
		return err
	}
	sy := int8(0)
	if format >= 2844 {
		sy = -4
	}
	palette := List{Type: 10, Values: []any{Compound{"Name": "minecraft:air"}, Compound{"Name": "minecraft:stone"}}}
	for i := 2; i < 17; i++ {
		palette.Values = append(palette.Values, Compound{"Name": fmt.Sprintf("test:block_%d", i)})
	}
	palette.Values = append(palette.Values, Compound{"Name": "minecraft:oak_log", "Properties": Compound{"axis": "x"}})
	count := 320
	if format >= 2529 {
		count = 342
	}
	words := make([]int64, count)
	for _, iv := range [][2]int{{255, 17}, {256, 1}} {
		idx, val := iv[0], uint64(iv[1])
		if format >= 2529 {
			words[idx/12] |= int64(val << uint((idx%12)*5))
		} else {
			bit := idx * 5
			words[bit/64] |= int64(val << uint(bit%64))
			if bit%64 > 59 {
				words[bit/64+1] |= int64(val >> uint(64-bit%64))
			}
		}
	}
	var chunk Compound
	if format >= 2844 {
		section := Compound{"Y": sy, "block_states": Compound{"palette": palette, "data": words}, "biomes": Compound{"palette": List{8, []any{"minecraft:desert"}}}}
		chunk = Compound{"DataVersion": int32(format), "xPos": int32(-1), "zPos": int32(-1), "sections": List{10, []any{section}}}
	} else {
		section := Compound{"Y": sy, "Palette": palette, "BlockStates": words}
		biomes := make([]int32, 1024)
		for i := range biomes {
			biomes[i] = 2
		}
		if format < 2203 {
			biomes = biomes[:256]
		}
		chunk = Compound{"DataVersion": int32(format), "Level": Compound{"xPos": int32(-1), "zPos": int32(-1), "Sections": List{10, []any{section}}, "Biomes": biomes}}
	}
	return Chunk(chunkPath, -1, -1, Encode(chunk), compression, false)
}

func GzipNBT(path string, c Compound) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(Encode(c))
	w.Close()
	return os.WriteFile(path, b.Bytes(), 0644)
}

// Chunk writes one independently encoded NBT chunk to a region, optionally external.
func Chunk(path string, cx, cz int, data []byte, compression byte, external bool) error {
	var b bytes.Buffer
	switch compression {
	case 1:
		w := gzip.NewWriter(&b)
		w.Write(data)
		w.Close()
	case 2:
		w := zlib.NewWriter(&b)
		w.Write(data)
		w.Close()
	case 3:
		b.Write(data)
	default:
		return fmt.Errorf("unsupported fixture compression %d", compression)
	}
	payload := b.Bytes()
	record := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(record, uint32(len(payload)+1))
	record[4] = compression
	copy(record[5:], payload)
	dir := filepath.Join(path, "region")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if external {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("c.%d.%d.mcc", cx, cz)), payload, 0644); err != nil {
			return err
		}
		record = []byte{0, 0, 0, 1, compression | 128}
	}
	sectors := (len(record) + 4095) / 4096
	file := make([]byte, 8192+sectors*4096)
	index := ((cx & 31) + (cz&31)*32) * 4
	binary.BigEndian.PutUint32(file[index:], uint32(2<<8|sectors))
	copy(file[8192:], record)
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("r.%d.%d.mca", cx>>5, cz>>5)), file, 0644)
}
