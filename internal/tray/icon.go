package tray

import "image"

const iconSize = 22

func iconPixmap(online bool) (int32, int32, []byte) {
	data := make([]byte, iconSize*iconSize*4)
	cx, cy := float64(iconSize-1)/2, float64(iconSize-1)/2
	outer := float64(iconSize)/2 - 1.2
	inner := outer - 3.2
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			d := dx*dx + dy*dy
			var a, r, g, b uint8
			if d <= outer*outer {
				// NerdyRMM blue ring.
				a, r, g, b = 255, 30, 136, 229
				if d <= inner*inner {
					if online {
						a, r, g, b = 255, 46, 204, 113
					} else {
						a, r, g, b = 255, 231, 76, 60
					}
				}
			}
			i := (y*iconSize + x) * 4
			data[i] = a
			data[i+1] = r
			data[i+2] = g
			data[i+3] = b
		}
	}
	return int32(iconSize), int32(iconSize), data
}

func iconRGBA(online bool) *image.RGBA {
	w, h, pix := iconPixmap(online)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(pix); i += 4 {
		// ARGB -> RGBA
		img.Pix[i] = pix[i+1]
		img.Pix[i+1] = pix[i+2]
		img.Pix[i+2] = pix[i+3]
		img.Pix[i+3] = pix[i]
	}
	return img
}
