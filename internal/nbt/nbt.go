// Package nbt reads Java Edition's big-endian Named Binary Tag format.
package nbt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
	"unicode/utf8"
)

const MaxBytes = 64 << 20
const maxElements = 1 << 20

type Compound = map[string]any
type decoder struct {
	data       []byte
	pos, nodes int
}

// Decode accepts a named root compound. Limits apply after decompression.
func Decode(r io.Reader) (Compound, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, errors.New("NBT exceeds 64 MiB limit")
	}
	d := decoder{data: data}
	tag, err := d.byte()
	if err != nil {
		return nil, err
	}
	if tag != 10 {
		return nil, fmt.Errorf("NBT root tag %d is not a compound", tag)
	}
	if _, err = d.str(); err != nil {
		return nil, err
	}
	v, err := d.value(10, 0)
	if err != nil {
		return nil, err
	}
	return v.(Compound), nil
}
func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.data)-d.pos {
		return nil, fmt.Errorf("truncated NBT at byte %d (need %d)", d.pos, n)
	}
	v := d.data[d.pos : d.pos+n]
	d.pos += n
	return v, nil
}
func (d *decoder) byte() (byte, error) {
	b, e := d.take(1)
	if e != nil {
		return 0, e
	}
	return b[0], nil
}
func (d *decoder) str() (string, error) {
	b, e := d.take(2)
	if e != nil {
		return "", e
	}
	b, e = d.take(int(binary.BigEndian.Uint16(b)))
	if e != nil {
		return "", e
	}
	return javaString(b)
}
func javaString(b []byte) (string, error) {
	// Java's modified UTF-8 encodes NUL as C0 80 and supplementary characters as surrogate pairs.
	if utf8.Valid(b) {
		return string(b), nil
	}
	u := make([]uint16, 0, len(b))
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			u = append(u, uint16(c))
			i++
		case c&0xe0 == 0xc0:
			if i+1 >= len(b) || b[i+1]&0xc0 != 0x80 {
				return "", errors.New("invalid NBT UTF-8")
			}
			u = append(u, uint16(c&31)<<6|uint16(b[i+1]&63))
			i += 2
		case c&0xf0 == 0xe0:
			if i+2 >= len(b) || b[i+1]&0xc0 != 0x80 || b[i+2]&0xc0 != 0x80 {
				return "", errors.New("invalid NBT UTF-8")
			}
			u = append(u, uint16(c&15)<<12|uint16(b[i+1]&63)<<6|uint16(b[i+2]&63))
			i += 3
		default:
			return "", errors.New("invalid NBT UTF-8")
		}
	}
	return string(utf16.Decode(u)), nil
}
func (d *decoder) count() (int, error) {
	b, e := d.take(4)
	if e != nil {
		return 0, e
	}
	n := int32(binary.BigEndian.Uint32(b))
	if n < 0 || n > maxElements {
		return 0, fmt.Errorf("invalid NBT collection length %d", n)
	}
	return int(n), nil
}
func (d *decoder) value(tag byte, depth int) (any, error) {
	d.nodes++
	if depth > 64 || d.nodes > maxElements {
		return nil, errors.New("NBT depth or element limit exceeded")
	}
	switch tag {
	case 1:
		b, e := d.byte()
		return int8(b), e
	case 2:
		b, e := d.take(2)
		if e != nil {
			return nil, e
		}
		return int16(binary.BigEndian.Uint16(b)), nil
	case 3:
		b, e := d.take(4)
		if e != nil {
			return nil, e
		}
		return int32(binary.BigEndian.Uint32(b)), nil
	case 4:
		b, e := d.take(8)
		if e != nil {
			return nil, e
		}
		return int64(binary.BigEndian.Uint64(b)), nil
	case 5:
		b, e := d.take(4)
		if e != nil {
			return nil, e
		}
		return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
	case 6:
		b, e := d.take(8)
		if e != nil {
			return nil, e
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	case 7:
		n, e := d.count()
		if e != nil {
			return nil, e
		}
		return d.take(n)
	case 8:
		return d.str()
	case 9:
		t, e := d.byte()
		if e != nil {
			return nil, e
		}
		n, e := d.count()
		if e != nil {
			return nil, e
		}
		if t > 12 || t == 0 && n != 0 {
			return nil, fmt.Errorf("invalid NBT list type %d", t)
		}
		if n > len(d.data)-d.pos {
			return nil, errors.New("NBT list length exceeds remaining input")
		}
		v := make([]any, n)
		for i := range v {
			v[i], e = d.value(t, depth+1)
			if e != nil {
				return nil, e
			}
		}
		return v, nil
	case 10:
		v := Compound{}
		for {
			t, e := d.byte()
			if e != nil {
				return nil, e
			}
			if t == 0 {
				break
			}
			name, e := d.str()
			if e != nil {
				return nil, e
			}
			if _, exists := v[name]; exists {
				return nil, fmt.Errorf("duplicate NBT tag %q", name)
			}
			value, e := d.value(t, depth+1)
			if e != nil {
				return nil, fmt.Errorf("NBT tag %q: %w", name, e)
			}
			v[name] = value
		}
		return v, nil
	case 11:
		n, e := d.count()
		if e != nil {
			return nil, e
		}
		b, e := d.take(n * 4)
		if e != nil {
			return nil, e
		}
		v := make([]int32, n)
		for i := range v {
			v[i] = int32(binary.BigEndian.Uint32(b[i*4:]))
		}
		return v, nil
	case 12:
		n, e := d.count()
		if e != nil {
			return nil, e
		}
		b, e := d.take(n * 8)
		if e != nil {
			return nil, e
		}
		v := make([]int64, n)
		for i := range v {
			v[i] = int64(binary.BigEndian.Uint64(b[i*8:]))
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unknown NBT tag %d", tag)
	}
}
