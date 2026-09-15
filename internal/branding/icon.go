// Package branding renders the NerdyRMM NR mark used by the tray and hicolor icons.
package branding

import (
	"image"
	"image/color"
	"image/draw"
)

var (
	NRBlue      = color.RGBA{R: 30, G: 136, B: 229, A: 255}
	NRNavy      = color.RGBA{R: 13, G: 71, B: 161, A: 255}
	NRWhite     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	NRGreen     = color.RGBA{R: 46, G: 204, B: 113, A: 255}
	NRRed       = color.RGBA{R: 231, G: 76, B: 60, A: 255}
	NRAmber     = color.RGBA{R: 241, G: 196, B: 15, A: 255}
	Transparent = color.RGBA{A: 0}
)

// RenderAppIcon draws a square NR favicon (rounded blue tile, white NR).
func RenderAppIcon(size int) *image.RGBA {
	return render(size, -1)
}

// RenderStatusIcon is the tray pixmap: brand mark plus a status dot.
// online: 1=online, 0=offline, 2=update available.
func RenderStatusIcon(size int, status int) *image.RGBA {
	return render(size, status)
}

func render(size, status int) *image.RGBA {
	if size < 8 {
		size = 8
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: Transparent}, image.Point{}, draw.Src)

	radius := float64(size) * 0.22
	fillRounded(img, 0, 0, size-1, size-1, radius, NRBlue)

	// Inner highlight band.
	inset := int(float64(size) * 0.06)
	if inset < 1 {
		inset = 1
	}
	fillRounded(img, inset, inset, size-1-inset, int(float64(size)*0.42), radius*0.6, color.RGBA{R: 66, G: 165, B: 245, A: 90})

	drawNR(img, size)

	if status >= 0 {
		dotR := size / 7
		if dotR < 2 {
			dotR = 2
		}
		cx := size - dotR - size/12
		cy := size - dotR - size/12
		c := NRRed
		switch status {
		case 1:
			c = NRGreen
		case 2:
			c = NRAmber
		}
		fillCircle(img, cx, cy, dotR, c)
		fillCircle(img, cx, cy, dotR/2, NRWhite)
	}
	return img
}

func drawNR(img *image.RGBA, size int) {
	// 5x7 bitmap glyphs for N and R, scaled to the tile.
	n := [7]string{
		"#   #",
		"##  #",
		"# # #",
		"# # #",
		"#  ##",
		"#   #",
		"#   #",
	}
	r := [7]string{
		"#### ",
		"#   #",
		"#   #",
		"#### ",
		"#  # ",
		"#   #",
		"#   #",
	}
	cell := float64(size) / 18.0
	if cell < 1 {
		cell = 1
	}
	originX := float64(size) * 0.18
	originY := float64(size) * 0.26
	gap := cell * 1.15
	blitGlyph(img, n, originX, originY, cell, NRWhite)
	blitGlyph(img, r, originX+5*cell+gap, originY, cell, NRWhite)
}

func blitGlyph(img *image.RGBA, glyph [7]string, ox, oy, cell float64, c color.RGBA) {
	for row, line := range glyph {
		for col, ch := range line {
			if ch != '#' {
				continue
			}
			x0 := int(ox + float64(col)*cell)
			y0 := int(oy + float64(row)*cell)
			x1 := int(ox + float64(col+1)*cell)
			y1 := int(oy + float64(row+1)*cell)
			fillRect(img, x0, y0, x1, y1, c)
		}
	}
}

func fillRounded(img *image.RGBA, x0, y0, x1, y1 int, radius float64, c color.RGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	r2 := radius * radius
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			cx, cy := float64(x), float64(y)
			inside := true
			switch {
			case cx < float64(x0)+radius && cy < float64(y0)+radius:
				dx, dy := cx-(float64(x0)+radius-1), cy-(float64(y0)+radius-1)
				inside = dx*dx+dy*dy <= r2
			case cx > float64(x1)-radius && cy < float64(y0)+radius:
				dx, dy := cx-(float64(x1)-radius+1), cy-(float64(y0)+radius-1)
				inside = dx*dx+dy*dy <= r2
			case cx < float64(x0)+radius && cy > float64(y1)-radius:
				dx, dy := cx-(float64(x0)+radius-1), cy-(float64(y1)-radius+1)
				inside = dx*dx+dy*dy <= r2
			case cx > float64(x1)-radius && cy > float64(y1)-radius:
				dx, dy := cx-(float64(x1)-radius+1), cy-(float64(y1)-radius+1)
				inside = dx*dx+dy*dy <= r2
			}
			if inside {
				img.SetRGBA(x, y, blend(img.RGBAAt(x, y), c))
			}
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				img.SetRGBA(x, y, blend(img.RGBAAt(x, y), c))
			}
		}
	}
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	for y := y0; y < y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := x0; x < x1; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			img.SetRGBA(x, y, c)
		}
	}
}

func blend(dst, src color.RGBA) color.RGBA {
	if src.A == 255 {
		return src
	}
	if src.A == 0 {
		return dst
	}
	a := uint32(src.A)
	ia := 255 - a
	return color.RGBA{
		R: uint8((uint32(src.R)*a + uint32(dst.R)*ia) / 255),
		G: uint8((uint32(src.G)*a + uint32(dst.G)*ia) / 255),
		B: uint8((uint32(src.B)*a + uint32(dst.B)*ia) / 255),
		A: 255,
	}
}

// PixmapARGB returns StatusNotifier-style ARGB bytes (alpha first per pixel, top-left).
func PixmapARGB(img *image.RGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, w*h*4)
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			out[i] = c.A
			out[i+1] = c.R
			out[i+2] = c.G
			out[i+3] = c.B
			i += 4
		}
	}
	return out
}
