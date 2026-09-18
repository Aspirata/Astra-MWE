package main

import (
	"context"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct{ ctx context.Context }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) ChooseDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Папка мира или ресурспака"})
}
func (a *App) ChooseFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Minecraft JAR, моды и ресурспаки", Filters: []runtime.FileFilter{{DisplayName: "Minecraft / ресурспаки (*.jar, *.zip)", Pattern: "*.jar;*.zip"}}})
}
func (a *App) ChooseSaveFile() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Экспорт в Blender", DefaultFilename: "minecraft-world.glb", Filters: []runtime.FileFilter{{DisplayName: "glTF Binary (*.glb)", Pattern: "*.glb"}}})
}
