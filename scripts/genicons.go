//go:build ignore

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/nerdyrmm/agent/internal/branding"
)

func main() {
	sizes := []int{16, 22, 24, 32, 48, 64, 128, 256}
	for _, sz := range sizes {
		dir := filepath.Join("packaging", "icons", "hicolor", fmt.Sprintf("%dx%d", sz, sz), "apps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		path := filepath.Join(dir, "nerdyrmm-agent.png")
		if err := writePNG(path, branding.RenderAppIcon(sz)); err != nil {
			panic(err)
		}
		fmt.Println("wrote", path)
	}
	if err := os.MkdirAll("internal/tray/web", 0o755); err != nil {
		panic(err)
	}
	if err := writePNG("internal/tray/web/favicon.png", branding.RenderAppIcon(32)); err != nil {
		panic(err)
	}
	fmt.Println("wrote internal/tray/web/favicon.png")
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
