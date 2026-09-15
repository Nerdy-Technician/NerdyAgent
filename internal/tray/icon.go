package tray

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"
	"sync"
)

// favicon.png is copied from the NerdyRMM web app (web/public/favicon.png).
//
//go:embed favicon.png
var faviconPNG []byte

const iconSize = 32

var (
	iconCacheOnce sync.Once
	iconOnline    []byte
	iconOffline   []byte
)

func iconPixmap(online bool) (int32, int32, []byte) {
	iconCacheOnce.Do(func() {
		base := scaleFavicon(iconSize)
		iconOnline = overlayBadge(base, true)
		iconOffline = overlayBadge(base, false)
	})
	if online {
		return int32(iconSize), int32(iconSize), iconOnline
	}
	return int32(iconSize), int32(iconSize), iconOffline
}

func scaleFavicon(size int) []byte {
	img, err := png.Decode(bytes.NewReader(faviconPNG))
	if err != nil || size <= 0 {
		return make([]byte, size*size*4)
	}
	src := imageToRGBA(img)
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	// Contain-fit with transparent padding so the NR mark is not stretched.
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 {
		return rgbaToARGB(dst)
	}
	scale := float64(size) / float64(sw)
	if h := float64(size) / float64(sh); h < scale {
		scale = h
	}
	tw := int(float64(sw)*scale + 0.5)
	th := int(float64(sh)*scale + 0.5)
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	ox := (size - tw) / 2
	oy := (size - th) / 2
	for y := 0; y < th; y++ {
		sy := sb.Min.Y + y*sh/th
		for x := 0; x < tw; x++ {
			sx := sb.Min.X + x*sw/tw
			dst.Set(ox+x, oy+y, src.At(sx, sy))
		}
	}
	return rgbaToARGB(dst)
}

func overlayBadge(srcARGB []byte, online bool) []byte {
	out := append([]byte(nil), srcARGB...)
	// Tiny 6px status dot in the bottom-right; does not replace the favicon.
	const badge = 6
	r, g, b := uint8(46), uint8(204), uint8(113)
	if !online {
		r, g, b = 231, 76, 60
	}
	cx := iconSize - badge/2 - 1
	cy := iconSize - badge/2 - 1
	rad := float64(badge) / 2
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx := float64(x - cx)
			dy := float64(y - cy)
			d2 := dx*dx + dy*dy
			if d2 > rad*rad {
				continue
			}
			i := (y*iconSize + x) * 4
			if d2 > (rad-1)*(rad-1) {
				out[i] = 255
				out[i+1] = 255
				out[i+2] = 255
				out[i+3] = 255
				continue
			}
			out[i] = 255
			out[i+1] = r
			out[i+2] = g
			out[i+3] = b
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

func rgbaToARGB(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	out := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		r := img.Pix[i*4]
		g := img.Pix[i*4+1]
		b := img.Pix[i*4+2]
		a := img.Pix[i*4+3]
		out[i*4] = a
		out[i*4+1] = r
		out[i*4+2] = g
		out[i*4+3] = b
	}
	return out
}
