package world

import (
	"astra-mwe/internal/nbt"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func readChunk(dir string, cx, cz int) (nbt.Compound, error) {
	path := filepath.Join(dir, "region", fmt.Sprintf("r.%d.%d.mca", cx>>5, cz>>5))
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if s.Size() < 8192 {
		return nil, errors.New("region header is truncated")
	}
	var loc [4]byte
	if _, e = f.ReadAt(loc[:], int64(((cx&31)+(cz&31)*32)*4)); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(loc[:])
	if n == 0 {
		return nil, nil
	}
	sector, count := int64(n>>8), int64(n&255)
	if sector < 2 || count == 0 || sector*4096+count*4096 > s.Size() {
		return nil, errors.New("invalid region sector offset or length")
	}
	var header [5]byte
	if _, e = f.ReadAt(header[:], sector*4096); e != nil {
		return nil, e
	}
	length := int64(binary.BigEndian.Uint32(header[:4]))
	if length < 1 || length+4 > count*4096 {
		return nil, errors.New("chunk record length exceeds allocated sectors")
	}
	compression := header[4]
	var raw []byte
	if compression&128 != 0 {
		if length != 1 {
			return nil, errors.New("external chunk stub has invalid length")
		}
		compression &= 127
		external := filepath.Join(dir, "region", fmt.Sprintf("c.%d.%d.mcc", cx, cz))
		ef, e := os.Open(external)
		if e != nil {
			return nil, fmt.Errorf("external chunk: %w", e)
		}
		raw, e = io.ReadAll(io.LimitReader(ef, nbt.MaxBytes+1))
		ef.Close()
		if e != nil {
			return nil, e
		}
		if len(raw) > nbt.MaxBytes {
			return nil, errors.New("external compressed chunk exceeds 64 MiB")
		}
	} else {
		raw = make([]byte, length-1)
		if _, e = f.ReadAt(raw, sector*4096+5); e != nil {
			return nil, e
		}
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch compression {
	case 1:
		z, e := gzip.NewReader(reader)
		if e != nil {
			return nil, e
		}
		defer z.Close()
		reader = z
	case 2:
		z, e := zlib.NewReader(reader)
		if e != nil {
			return nil, e
		}
		defer z.Close()
		reader = z
	case 3:
	case 4:
		b, e := decodeLZ4(raw)
		if e != nil {
			return nil, e
		}
		reader = bytes.NewReader(b)
	default:
		return nil, fmt.Errorf("unsupported region compression %d", compression)
	}
	return nbt.Decode(reader)
}
