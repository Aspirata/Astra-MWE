package world

import (
	"bytes"
	"encoding/binary"
	"github.com/pierrec/lz4/v4"
	"testing"
)

func TestXXHashReferenceVectors(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want uint32
	}{{"", 0x02cc5d05}, {"a", 0x550d7456}, {"abc", 0x32d153ff}, {"123456789", 0x937bad67}} {
		if got := xxhash32([]byte(tc.s), 0); got != tc.want {
			t.Fatalf("xxhash %q got %08x want %08x", tc.s, got, tc.want)
		}
	}
}
func lz4Stream(data []byte, compressed bool) []byte {
	payload := data
	token := byte(0x16)
	if compressed {
		buf := make([]byte, lz4.CompressBlockBound(len(data)))
		n, e := lz4.CompressBlock(data, buf, nil)
		if e != nil || n == 0 {
			panic("fixture did not compress")
		}
		payload = buf[:n]
		token = 0x26
	}
	b := make([]byte, 21+len(payload)+21)
	copy(b, "LZ4Block")
	b[8] = token
	binary.LittleEndian.PutUint32(b[9:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(b[13:], uint32(len(data)))
	binary.LittleEndian.PutUint32(b[17:], xxhash32(data, 0x9747b28c)&0xfffffff)
	copy(b[21:], payload)
	copy(b[21+len(payload):], "LZ4Block")
	b[21+len(payload)+8] = 0x16
	return b
}
func TestJavaLZ4BlockStreamAndChecksum(t *testing.T) {
	data := bytes.Repeat([]byte("Minecraft Anvil section NBT data "), 50)
	for _, compressed := range []bool{false, true} {
		raw := lz4Stream(data, compressed)
		got, err := decodeLZ4(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, got) {
			t.Fatal("LZ4 decode changed bytes")
		}
		raw[17] ^= 1
		if _, err = decodeLZ4(raw); err == nil {
			t.Fatal("bad checksum accepted")
		}
	}
	if _, err := decodeLZ4([]byte("LZ4Block")); err == nil {
		t.Fatal("truncated header accepted")
	}
}
