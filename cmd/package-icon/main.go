// Generate application resources from the supplied artwork. Run at repository root.
package main

import (
	"errors"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"

	"github.com/nfnt/resize"
	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}

func generate() error {
	f, err := os.Open("build/assets/astra-mwe.jpg")
	if err != nil {
		return err
	}
	img, err := jpeg.Decode(f)
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.MkdirAll("frontend/public", 0755); err != nil {
		return err
	}
	preview := resize.Resize(256, 256, img, resize.Lanczos3)
	for _, path := range []string{"build/appicon.png", "frontend/public/icon.png"} {
		if err := writeFile(path, func(out io.Writer) error { return png.Encode(out, preview) }); err != nil {
			return err
		}
	}
	icon, err := winres.NewIconFromResizedImage(img, []int{16, 24, 32, 48, 64, 128, 256})
	if err != nil {
		return err
	}
	if err = os.MkdirAll("build/windows", 0755); err != nil {
		return err
	}
	if err = writeFile("build/windows/icon.ico", icon.SaveICO); err != nil {
		return err
	}
	rs := winres.ResourceSet{}
	// Wails uses resource ID 3 for the window/taskbar icon.
	if err = rs.SetIcon(winres.ID(3), icon); err != nil {
		return err
	}
	rs.SetManifest(winres.AppManifest{Description: "Astra MWE", Compatibility: winres.Win10AndAbove, DPIAwareness: winres.DPIPerMonitorV2, LongPathAware: true, UseCommonControlsV6: true})
	info := version.Info{FileVersion: [4]uint16{0, 1, 0, 0}, ProductVersion: [4]uint16{0, 1, 0, 0}}
	for key, value := range map[string]string{version.ProductName: "Astra MWE", version.FileDescription: "Minecraft World Exporter", version.ProductVersion: "0.1.0", version.FileVersion: "0.1.0", version.LegalCopyright: "Copyright (c) 2026 Astra MWE contributors", version.Comments: "MIT License."} {
		if err = info.Set(version.LangDefault, key, value); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(info)
	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64} {
		if err := writeFile("appicon_windows_"+string(arch)+".syso", func(out io.Writer) error {
			return rs.WriteObject(out, arch)
		}); err != nil {
			return err
		}
	}
	return nil
}

// Encoding can succeed while flushing the file fails; report either error.
func writeFile(path string, encode func(io.Writer) error) (err error) {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	return encode(out)
}
