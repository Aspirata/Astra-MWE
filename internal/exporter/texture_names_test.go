package exporter

import (
	"astra-mwe/internal/scene"
	"astra-mwe/internal/skins"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestSourceImageNamesAndSharedTextureStorage(t *testing.T) {
	sc := triangle()
	sc.Materials = []scene.Material{{Name: "minecraft:grass_block", TextureName: "grass_block_top.png", PNG: skins.DemoPNG()}, {Name: "other:block", TextureName: "grass_block_top.png", PNG: skins.DemoPNG()}}
	raw, err := GLB(sc)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Images    []struct{ Name string }
		Textures  []json.RawMessage
		Materials []json.RawMessage
	}
	if err = json.Unmarshal(raw[20:20+binary.LittleEndian.Uint32(raw[12:16])], &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Images) != 1 || doc.Images[0].Name != "grass_block_top.png" || len(doc.Textures) != 1 || len(doc.Materials) != 2 {
		t.Fatalf("names/materials/storage wrong: %+v", doc)
	}
}
