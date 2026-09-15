package tray

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// favicon.png is copied from the NerdyRMM web app (web/public/favicon.png).
// Source is 366×317; we square-crop and scale to 22 and 32 for the tray.
//
//go:embed favicon.png
var faviconPNG []byte

const (
	iconName      = "nerdyrmm-agent"
	iconSize      = 32
	iconSizeSmall = 22
)

var iconSizes = []int{iconSizeSmall, iconSize}

type preparedIcons struct {
	themePath string
	pngPath   map[bool]string
	pngBytes  map[bool][]byte // 32px PNG
	images    map[bool]*image.RGBA
	pixmaps   map[bool][]pixmapDesc
}

type pixmapDesc struct {
	Width  int32
	Height int32
	ARGB   []byte // network-endian ARGB32 (byte order A,R,G,B)
}

var (
	prepOnce sync.Once
	prepared preparedIcons
)

func iconPixmap(online bool) (int32, int32, []byte) {
	pms := iconPixmaps(online)
	for _, p := range pms {
		if p.Width == int32(iconSize) {
			return p.Width, p.Height, p.ARGB
		}
	}
	if len(pms) == 0 {
		return 0, 0, nil
	}
	return pms[0].Width, pms[0].Height, pms[0].ARGB
}

func iconPixmaps(online bool) []pixmapDesc {
	ensurePrepared()
	return prepared.pixmaps[online]
}

func iconFile(online bool) string {
	ensurePrepared()
	return prepared.pngPath[online]
}

func iconThemePath() string {
	ensurePrepared()
	return prepared.themePath
}

func ensurePrepared() {
	prepOnce.Do(func() {
		prepared = buildPreparedIcons()
	})
}

func buildPreparedIcons() preparedIcons {
	out := preparedIcons{
		pngPath:  map[bool]string{},
		pngBytes: map[bool][]byte{},
		images:   map[bool]*image.RGBA{},
		pixmaps:  map[bool][]pixmapDesc{},
	}
	base, err := decodeFaviconRGBA()
	if err != nil || base == nil {
		base = fallbackSquare(64)
	}
	cropped := squareCropLogo(base)
	for _, online := range []bool{true, false} {
		var pms []pixmapDesc
		for _, sz := range iconSizes {
			scaled := scaleRGBA(cropped, sz, sz)
			badged := overlayBadgeRGBA(scaled, online)
			pms = append(pms, pixmapDesc{
				Width:  int32(sz),
				Height: int32(sz),
				ARGB:   rgbaToNetworkARGB(badged),
			})
			if sz == iconSize {
				out.images[online] = badged
				var buf bytes.Buffer
				_ = png.Encode(&buf, badged)
				out.pngBytes[online] = buf.Bytes()
			}
		}
		out.pixmaps[online] = pms
	}
	return out
}

func decodeFaviconRGBA() (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(faviconPNG))
	if err != nil {
		return nil, err
	}
	return imageToRGBA(img), nil
}

func fallbackSquare(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 30, G: 136, B: 229, A: 255})
		}
	}
	return img
}

// squareCropLogo flood-fills near-black backdrop from the edges (keeps the NR
// mark), then square-crops the remaining opaque bounds with a little padding.
func squareCropLogo(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	alpha := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := src.RGBAAt(b.Min.X+x, b.Min.Y+y)
			alpha[y*w+x] = p.A
		}
	}
	// Flood-fill backdrop from the border so interior black in the glyph stays.
	vis := make([]bool, w*h)
	type pt struct{ x, y int }
	q := make([]pt, 0, w+h)
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h {
			return
		}
		i := y*w + x
		if vis[i] {
			return
		}
		p := src.RGBAAt(b.Min.X+x, b.Min.Y+y)
		if !isBackdrop(p) {
			return
		}
		vis[i] = true
		alpha[i] = 0
		q = append(q, pt{x, y})
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}
	for len(q) > 0 {
		p := q[0]
		q = q[1:]
		push(p.x-1, p.y)
		push(p.x+1, p.y)
		push(p.x, p.y-1)
		push(p.x, p.y+1)
	}

	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alpha[y*w+x] < 16 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX {
		return src
	}
	bw, bh := maxX-minX+1, maxY-minY+1
	side := bw
	if bh > side {
		side = bh
	}
	pad := side / 10
	if pad < 2 {
		pad = 2
	}
	side += pad * 2
	cx := (minX + maxX) / 2
	cy := (minY + maxY) / 2
	x0 := cx - side/2
	y0 := cy - side/2

	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			sx, sy := x0+x, y0+y
			if sx < 0 || sy < 0 || sx >= w || sy >= h {
				continue
			}
			p := src.RGBAAt(b.Min.X+sx, b.Min.Y+sy)
			p.A = alpha[sy*w+sx]
			if p.A == 0 {
				p.R, p.G, p.B = 0, 0, 0
			}
			dst.SetRGBA(x, y, p)
		}
	}
	return dst
}

func isBackdrop(p color.RGBA) bool {
	if p.A < 16 {
		return true
	}
	return p.R < 28 && p.G < 28 && p.B < 28
}

func scaleRGBA(src *image.RGBA, dw, dh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if sw <= 0 || sh <= 0 || dw <= 0 || dh <= 0 {
		return dst
	}
	for y := 0; y < dh; y++ {
		sy := (float64(y)+0.5)*float64(sh)/float64(dh) - 0.5
		for x := 0; x < dw; x++ {
			sx := (float64(x)+0.5)*float64(sw)/float64(dw) - 0.5
			dst.SetRGBA(x, y, sampleBilinear(src, sx, sy))
		}
	}
	return dst
}

func sampleBilinear(src *image.RGBA, x, y float64) color.RGBA {
	b := src.Bounds()
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	maxX := float64(b.Dx() - 1)
	maxY := float64(b.Dy() - 1)
	if x > maxX {
		x = maxX
	}
	if y > maxY {
		y = maxY
	}
	x0 := int(x)
	y0 := int(y)
	x1 := x0 + 1
	y1 := y0 + 1
	if x1 > b.Dx()-1 {
		x1 = b.Dx() - 1
	}
	if y1 > b.Dy()-1 {
		y1 = b.Dy() - 1
	}
	fx := x - float64(x0)
	fy := y - float64(y0)
	c00 := src.RGBAAt(b.Min.X+x0, b.Min.Y+y0)
	c10 := src.RGBAAt(b.Min.X+x1, b.Min.Y+y0)
	c01 := src.RGBAAt(b.Min.X+x0, b.Min.Y+y1)
	c11 := src.RGBAAt(b.Min.X+x1, b.Min.Y+y1)
	return lerpRGBA(lerpRGBA(c00, c10, fx), lerpRGBA(c01, c11, fx), fy)
}

func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: lerpU8(a.R, b.R, t),
		G: lerpU8(a.G, b.G, t),
		B: lerpU8(a.B, b.B, t),
		A: lerpU8(a.A, b.A, t),
	}
}

func lerpU8(a, b uint8, t float64) uint8 {
	return uint8(float64(a)*(1-t) + float64(b)*t + 0.5)
}

func overlayBadgeRGBA(src *image.RGBA, online bool) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.SetRGBA(x, y, src.RGBAAt(x, y))
		}
	}
	side := b.Dx()
	badge := side / 5
	if badge < 4 {
		badge = 4
	}
	r, g, bl := uint8(46), uint8(204), uint8(113)
	if !online {
		r, g, bl = 231, 76, 60
	}
	cx := b.Max.X - badge/2 - 1
	cy := b.Max.Y - badge/2 - 1
	rad := float64(badge) / 2
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x - cx)
			dy := float64(y - cy)
			d2 := dx*dx + dy*dy
			if d2 > rad*rad {
				continue
			}
			if d2 > (rad-1)*(rad-1) {
				dst.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: bl, A: 255})
		}
	}
	return dst
}

// rgbaToNetworkARGB packs pixels as big-endian ARGB32 (byte order A,R,G,B).
// xapp-sn-watcher then rotates A,R,G,B → R,G,B,A for GdkPixbuf.
func rgbaToNetworkARGB(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	out := make([]byte, w*h*4)
	i := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			out[i] = p.A
			out[i+1] = p.R
			out[i+2] = p.G
			out[i+3] = p.B
			i += 4
		}
	}
	return out
}

func imageToRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x-b.Min.X, y-b.Min.Y, img.At(x, y))
		}
	}
	return dst
}

func writeIconPNG(img *image.RGBA, online bool) (absPNG, themeRoot string, err error) {
	roots := iconInstallRoots()
	suffix := ""
	if !online {
		suffix = "-offline"
	}
	var firstPNG string
	var firstTheme string
	for _, root := range roots {
		for _, sz := range iconSizes {
			dir := filepath.Join(root, "hicolor", itoa(sz)+"x"+itoa(sz), "apps")
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				continue
			}
			scaled := img
			if img.Bounds().Dx() != sz {
				scaled = scaleRGBA(img, sz, sz)
			}
			path := filepath.Join(dir, iconName+suffix+".png")
			if werr := encodePNG(path, scaled); werr != nil {
				continue
			}
			if sz == iconSize && firstPNG == "" {
				firstPNG = path
				firstTheme = root
			}
		}
		pixmaps := filepath.Join(filepath.Dir(root), "pixmaps")
		if root == "/usr/share/icons" {
			pixmaps = "/usr/share/pixmaps"
		}
		_ = os.MkdirAll(pixmaps, 0o755)
		_ = encodePNG(filepath.Join(pixmaps, iconName+suffix+".png"), img)
		updateIconCache(filepath.Join(root, "hicolor"))
	}
	if firstPNG == "" {
		return "", "", os.ErrPermission
	}
	return firstPNG, firstTheme, nil
}

func iconInstallRoots() []string {
	var roots []string
	if os.Geteuid() == 0 {
		roots = append(roots, "/usr/share/icons")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		roots = append(roots, filepath.Join(xdg, "icons"))
	}
	if len(roots) == 0 {
		roots = append(roots, filepath.Join(os.TempDir(), "nerdyrmm-icons"))
	}
	return roots
}

func encodePNG(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func updateIconCache(hicolor string) {
	if _, err := os.Stat(hicolor); err != nil {
		return
	}
	_ = exec.Command("gtk-update-icon-cache", "-f", "-t", hicolor).Run()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// InstallThemeIcons writes hicolor + pixmaps PNGs. Root writes /usr/share;
// otherwise $XDG_DATA_HOME/icons (or ~/.local/share/icons).
func InstallThemeIcons() (absPNG, themeRoot string, err error) {
	ensurePrepared()
	for _, online := range []bool{true, false} {
		img := prepared.images[online]
		if img == nil {
			continue
		}
		path, theme, werr := writeIconPNG(img, online)
		if werr != nil {
			err = werr
			continue
		}
		prepared.pngPath[online] = path
		if themeRoot == "" {
			themeRoot = theme
			prepared.themePath = theme
		}
		if online {
			absPNG = path
		}
	}
	if absPNG == "" && err == nil {
		err = os.ErrNotExist
	}
	return absPNG, themeRoot, err
}
