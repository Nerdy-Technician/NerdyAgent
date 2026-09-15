package tray

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/nerdyrmm/agent/internal/branding"
)

const iconSize = 22

func iconPixmap(online bool, updateAvail bool) (int32, int32, []byte) {
	st := 0
	if updateAvail {
		st = 2
	} else if online {
		st = 1
	}
	img := branding.RenderStatusIcon(iconSize, st)
	return int32(iconSize), int32(iconSize), branding.PixmapARGB(img)
}

func iconThemeName() string {
	return "nerdyrmm-agent"
}

func writePNGFile(path string, size int) error {
	img := branding.RenderAppIcon(size)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// InstallHicolor writes nerdyrmm-agent.png into a hicolor icon theme tree.
func InstallHicolor(base string) error {
	if strings.TrimSpace(base) == "" {
		base = "/usr/share/icons/hicolor"
	}
	for _, sz := range []int{16, 22, 24, 32, 48, 64, 128, 256} {
		path := filepath.Join(base, fmt.Sprintf("%dx%d", sz, sz), "apps", "nerdyrmm-agent.png")
		if err := writePNGFile(path, sz); err != nil {
			return err
		}
	}
	return nil
}
