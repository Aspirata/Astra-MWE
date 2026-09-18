//go:build production

package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"astra-mwe/internal/service"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var embedded embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	cache = filepath.Join(cache, "AstraMWE")
	if override := os.Getenv("ASTRA_CACHE_DIR"); override != "" {
		cache = override
	}
	backend := service.New(cache)
	defer backend.Close()
	frontend, err := fs.Sub(embedded, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	handler := backend.Handler()
	if len(os.Args) > 1 && os.Args[1] == "--serve" {
		address := "127.0.0.1:34115"
		if len(os.Args) > 2 {
			address = os.Args[2]
		}
		if !strings.HasPrefix(address, "127.0.0.1:") {
			log.Fatal("development server only permits 127.0.0.1")
		}
		mux := http.NewServeMux()
		mux.Handle("/api/", handler)
		mux.Handle("/", http.FileServer(http.FS(frontend)))
		fmt.Println("Astra MWE preview: http://" + address)
		log.Fatal(http.ListenAndServe(address, mux))
		return
	}
	app := &App{}
	err = wails.Run(&options.App{
		Title: "Astra MWE", Width: 1500, Height: 940, MinWidth: 1100, MinHeight: 720,
		BackgroundColour: options.NewRGBA(25, 28, 33, 255),
		AssetServer:      &assetserver.Options{Assets: frontend, Handler: handler},
		Windows:          &windows.Options{Theme: windows.Dark, WebviewUserDataPath: filepath.Join(cache, "webview")},
		Linux:            &linux.Options{Icon: appIcon, ProgramName: "astra-mwe", WebviewGpuPolicy: linux.WebviewGpuPolicyAlways},
		OnStartup:        app.startup, Bind: []interface{}{app},
	})
	if err != nil {
		log.Print(err)
	}
}
