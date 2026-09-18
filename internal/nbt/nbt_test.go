package nbt

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDecodeTypedValues(t *testing.T) {
	// Named root compound containing a negative int and a UTF-8 string.
	data := []byte{10, 0, 0, 3, 0, 1, 'y', 255, 255, 255, 192, 8, 0, 1, 's', 0, 2, 0xc3, 0xa9, 0}
	v, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if v["y"] != int32(-64) || v["s"] != "é" {
		t.Fatalf("wrong decoded values: %#v", v)
	}
}

func TestJavaModifiedUTF8(t *testing.T) {
	got, err := javaString([]byte{'a', 0xc0, 0x80, 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80})
	if err != nil || got != "a\x00😀" {
		t.Fatalf("%q, %v", got, err)
	}
}
func TestRejectMalformedLengthsAndDepth(t *testing.T) {
	cases := [][]byte{{10, 0, 0, 7, 0, 1, 'a', 255, 255, 255, 255}, {10, 0, 0, 12, 0, 1, 'a', 127, 255, 255, 255}, {10, 0, 0, 9, 0, 1, 'a', 0, 0, 0, 0, 1}, {10, 0, 0, 8, 0, 1, 's', 0, 3, 'a'}}
	deep := []byte{10, 0, 0}
	for i := 0; i < 100; i++ {
		deep = append(deep, 10, 0, 0)
	}
	cases = append(cases, deep)
	for i, data := range cases {
		if _, err := Decode(bytes.NewReader(data)); err == nil {
			t.Errorf("case %d accepted malformed NBT", i)
		}
	}
}
func TestDecodeArraysAndList(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{10, 0, 0, 12, 0, 1, 'l'})
	binary.Write(&b, binary.BigEndian, int32(2))
	binary.Write(&b, binary.BigEndian, []int64{-1, 42})
	b.Write([]byte{9, 0, 1, 'v', 6})
	binary.Write(&b, binary.BigEndian, int32(2))
	binary.Write(&b, binary.BigEndian, []float64{1.5, -4.25})
	b.WriteByte(0)
	v, err := Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if v["l"].([]int64)[0] != -1 || v["v"].([]any)[1] != -4.25 {
		t.Fatalf("wrong arrays: %#v", v)
	}
}
