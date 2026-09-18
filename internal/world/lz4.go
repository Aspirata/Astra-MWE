package world

import (
	"astra-mwe/internal/nbt"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/pierrec/lz4/v4"
	"math/bits"
)

// Java Anvil uses lz4-java's LZ4Block stream, not the standard LZ4 frame.
// Format reference: https://github.com/lz4/lz4-java/blob/master/src/java/net/jpountz/lz4/LZ4BlockInputStream.java
func decodeLZ4(raw []byte) ([]byte, error) {
	var out []byte
	for {
		if len(raw) < 21 {
			return nil, errors.New("truncated Java LZ4 stream")
		}
		if string(raw[:8]) != "LZ4Block" {
			return nil, errors.New("invalid Java LZ4 magic")
		}
		token := raw[8]
		method := token & 0xf0
		n := int(binary.LittleEndian.Uint32(raw[9:13]))
		size := int(binary.LittleEndian.Uint32(raw[13:17]))
		check := binary.LittleEndian.Uint32(raw[17:21])
		raw = raw[21:]
		if method != 0x10 && method != 0x20 {
			return nil, errors.New("invalid Java LZ4 method")
		}
		if n == 0 && size == 0 {
			if check != 0 || len(raw) != 0 {
				return nil, errors.New("invalid Java LZ4 terminator")
			}
			return out, nil
		}
		if n <= 0 || size <= 0 || size > 1<<uint(10+token&15) || n > len(raw) || size > nbt.MaxBytes-len(out) {
			return nil, errors.New("invalid or excessive Java LZ4 block length")
		}
		start := len(out)
		out = append(out, make([]byte, size)...)
		block := out[start:]
		if method == 0x10 {
			if n != size {
				return nil, errors.New("raw LZ4 length mismatch")
			}
			copy(block, raw[:n])
		} else {
			decoded, e := lz4.UncompressBlock(raw[:n], block)
			if e != nil || decoded != size {
				return nil, fmt.Errorf("invalid compressed LZ4 block: decoded %d of %d: %v", decoded, size, e)
			}
		}
		// lz4-java StreamingXXHash32.asChecksum intentionally masks to 28 bits.
		if xxhash32(block, 0x9747b28c)&0x0fffffff != check {
			return nil, errors.New("Java LZ4 checksum mismatch")
		}
		raw = raw[n:]
	}
}

func xxhash32(data []byte, seed uint32) uint32 {
	const p1 uint32 = 2654435761
	const p2 uint32 = 2246822519
	const p3 uint32 = 3266489917
	const p4 uint32 = 668265263
	const p5 uint32 = 374761393
	round := func(v, n uint32) uint32 { return bits.RotateLeft32(v+n*p2, 13) * p1 }
	length := len(data)
	var h uint32
	if length >= 16 {
		v1, v2, v3, v4 := seed+p1+p2, seed+p2, seed, seed-p1
		for len(data) >= 16 {
			v1 = round(v1, binary.LittleEndian.Uint32(data))
			v2 = round(v2, binary.LittleEndian.Uint32(data[4:]))
			v3 = round(v3, binary.LittleEndian.Uint32(data[8:]))
			v4 = round(v4, binary.LittleEndian.Uint32(data[12:]))
			data = data[16:]
		}
		h = bits.RotateLeft32(v1, 1) + bits.RotateLeft32(v2, 7) + bits.RotateLeft32(v3, 12) + bits.RotateLeft32(v4, 18)
	} else {
		h = seed + p5
	}
	h += uint32(length)
	for len(data) >= 4 {
		h = bits.RotateLeft32(h+binary.LittleEndian.Uint32(data)*p3, 17) * p4
		data = data[4:]
	}
	for _, b := range data {
		h = bits.RotateLeft32(h+uint32(b)*p5, 11) * p1
	}
	h ^= h >> 15
	h *= p2
	h ^= h >> 13
	h *= p3
	h ^= h >> 16
	return h
}
