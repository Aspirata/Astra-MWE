// Package only distributable files, never caches, test worlds or old binaries.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const releaseVersion = "0.1.0"

func main() {
	targetOS := flag.String("os", "windows", "windows or linux")
	arch := flag.String("arch", "amd64", "amd64 or arm64")
	cliOnly := flag.Bool("cli-only", false, "package CLI without the desktop executable")
	flag.Parse()
	if (*targetOS != "windows" && *targetOS != "linux") || (*arch != "amd64" && *arch != "arm64") {
		log.Fatal("unsupported platform")
	}
	if err := pack(*targetOS, *arch, *cliOnly); err != nil {
		log.Fatal(err)
	}
}

func pack(targetOS, arch string, cliOnly bool) error {
	suffix := ""
	if targetOS == "windows" {
		suffix = ".exe"
	}
	files := []string{
		"README.md", "README.en.md", "CONTRIBUTING.md", "LICENSE",
		"CHANGELOG.md", "THIRD_PARTY_NOTICES.txt", "docs/verification.md",
		"build/appicon.png", "build/bin/astra-cli-" + targetOS + "-" + arch + suffix,
	}
	kind := "astra-mwe"
	if cliOnly {
		kind = "astra-cli"
	} else {
		files = append(files, "build/bin/astra-mwe-"+targetOS+"-"+arch+suffix)
	}
	for _, path := range files {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll("build/releases", 0755); err != nil {
		return err
	}
	path := "build/releases/" + kind + "-" + releaseVersion + "-" + targetOS + "-" + arch + ".zip"
	// Build beside the destination so a read or ZIP error cannot replace a
	// previously valid release. Only a fully closed archive is published.
	f, err := os.CreateTemp(filepath.Dir(path), ".release-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	err = writeArchive(io.MultiWriter(f, hash), files)
	err = errors.Join(err, f.Chmod(0644), f.Close())
	if err != nil {
		return err
	}
	// Retire the old checksum before replacing its archive; a failed checksum
	// write must never leave a new ZIP paired with an old digest.
	if err = os.Remove(path + ".sha256"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err = os.WriteFile(path+".sha256", []byte(fmt.Sprintf("%x  %s\n", hash.Sum(nil), filepath.Base(path))), 0644); err != nil {
		removeErr := os.Remove(path + ".sha256")
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		return errors.Join(err, os.Remove(path), removeErr)
	}
	return nil
}

func writeArchive(output io.Writer, files []string) (err error) {
	archive := zip.NewWriter(output)
	defer func() { err = errors.Join(err, archive.Close()) }()
	for _, source := range files {
		if err := addFile(archive, source); err != nil {
			return fmt.Errorf("archive %s: %w", source, err)
		}
	}
	return nil
}

func addFile(archive *zip.Writer, source string) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	stat, err := input.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(source)
	if filepath.ToSlash(filepath.Dir(source)) == "build/bin" {
		header.Name = filepath.Base(source)
	}
	header.Method = zip.Deflate
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, input)
	return err
}
