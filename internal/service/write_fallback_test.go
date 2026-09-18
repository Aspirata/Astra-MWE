package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExportFallbackWithoutHardlinks(t *testing.T) {
	noLinks := func(string, string) error { return errors.New("hard links unsupported") }
	p := filepath.Join(t.TempDir(), "export.glb")
	if err := writeFileWithLink(context.Background(), p, []byte("original"), noLinks); err != nil {
		t.Fatal(err)
	}
	if err := writeFileWithLink(context.Background(), p, []byte("replacement"), noLinks); err == nil {
		t.Fatal("overwrote existing output")
	}
	raw, err := os.ReadFile(p)
	if err != nil || string(raw) != "original" {
		t.Fatalf("output changed: %s %v", raw, err)
	}
}

func TestExportFallbackCancellationRemovesOnlyNewOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	noLinks := func(string, string) error { cancel(); return errors.New("hard links unsupported") }
	p := filepath.Join(t.TempDir(), "cancelled.glb")
	if err := writeFileWithLink(ctx, p, []byte("data"), noLinks); !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation, got %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("left partial output: %v", err)
	}
}
