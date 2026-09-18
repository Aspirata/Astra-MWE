package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseFixture(t *testing.T, targetOS, arch string) map[string]string {
	t.Helper()
	t.Chdir(t.TempDir())
	suffix := ""
	if targetOS == "windows" {
		suffix = ".exe"
	}
	files := map[string]string{
		"README.md": "readme", "README.en.md": "English readme", "LICENSE": "license",
		"CHANGELOG.md": "changes", "THIRD_PARTY_NOTICES.txt": "notices", "CONTRIBUTING.md": "source guide",
		"docs/verification.md": "verification", "build/appicon.png": "icon",
		"build/bin/astra-cli-" + targetOS + "-" + arch + suffix: "cli binary",
		"build/bin/astra-mwe-" + targetOS + "-" + arch + suffix: "desktop binary",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestPackArchiveAndChecksum(t *testing.T) {
	for _, targetOS := range []string{"windows", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			for _, cliOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/cli=%v", targetOS, arch, cliOnly), func(t *testing.T) {
					files := releaseFixture(t, targetOS, arch)
					if err := pack(targetOS, arch, cliOnly); err != nil {
						t.Fatal(err)
					}
					kind := "astra-mwe"
					if cliOnly {
						kind = "astra-cli"
					}
					path := fmt.Sprintf("build/releases/%s-0.1.0-%s-%s.zip", kind, targetOS, arch)
					archive, err := zip.OpenReader(path)
					if err != nil {
						t.Fatal(err)
					}
					defer archive.Close()
					want := make(map[string]string)
					for source, content := range files {
						if cliOnly && content == "desktop binary" {
							continue
						}
						want[strings.TrimPrefix(source, "build/bin/")] = content
					}
					for _, entry := range archive.File {
						input, err := entry.Open()
						if err != nil {
							t.Fatal(err)
						}
						data, err := io.ReadAll(input)
						input.Close()
						if err != nil || string(data) != want[entry.Name] {
							t.Fatalf("entry %q: data=%q error=%v", entry.Name, data, err)
						}
						if entry.Method != zip.Deflate || entry.Modified.IsZero() {
							t.Fatalf("lost archive metadata for %q", entry.Name)
						}
						delete(want, entry.Name)
					}
					if len(want) != 0 {
						t.Fatalf("missing entries: %v", want)
					}
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					checksum, err := os.ReadFile(path + ".sha256")
					if err != nil || string(checksum) != fmt.Sprintf("%x  %s\n", sha256.Sum256(data), filepath.Base(path)) {
						t.Fatalf("invalid checksum %q: %v", checksum, err)
					}
				})
			}
		}
	}
}

func TestPackReadFailurePreservesPreviousRelease(t *testing.T) {
	releaseFixture(t, "windows", "amd64")
	if err := pack("windows", "amd64", false); err != nil {
		t.Fatal(err)
	}
	path := "build/releases/astra-mwe-0.1.0-windows-amd64.zip"
	previous := make(map[string][]byte)
	for _, name := range []string{path, path + ".sha256"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = data
	}
	if err := os.Remove("README.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("README.md", 0755); err != nil {
		t.Fatal(err)
	}
	if err := pack("windows", "amd64", false); err == nil {
		t.Fatal("expected directory read failure")
	}
	for name, want := range previous {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("previous release changed at %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir("build/releases")
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary release files remain: %v, %v", entries, err)
	}
}

func TestPackChecksumFailureLeavesNoArchive(t *testing.T) {
	releaseFixture(t, "windows", "amd64")
	path := "build/releases/astra-mwe-0.1.0-windows-amd64.zip"
	// A nonempty directory prevents checksum publication on every platform.
	if err := os.MkdirAll(path+".sha256", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sha256/blocker", []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := pack("windows", "amd64", false); err == nil {
		t.Fatal("expected checksum publication failure")
	}
	entries, err := os.ReadDir("build/releases")
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path)+".sha256" {
		t.Fatalf("failed release left artifacts: %v, %v", entries, err)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteArchiveReportsFinalFlushFailure(t *testing.T) {
	releaseFixture(t, "windows", "amd64")
	want := errors.New("disk full")
	// This small entry stays buffered until ZIP Close flushes the directory.
	if err := writeArchive(failingWriter{want}, []string{"README.md"}); !errors.Is(err, want) {
		t.Fatalf("lost final ZIP write error: %v", err)
	}
}
