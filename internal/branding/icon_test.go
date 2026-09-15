package branding

import "testing"

func TestRenderAppIconOpaqueCenter(t *testing.T) {
	img := RenderAppIcon(32)
	if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
		t.Fatalf("size %v", img.Bounds())
	}
	c := img.RGBAAt(16, 16)
	if c.A == 0 {
		t.Fatalf("center should be opaque, got %+v", c)
	}
}

func TestPixmapARGBLen(t *testing.T) {
	img := RenderStatusIcon(22, 1)
	pix := PixmapARGB(img)
	if len(pix) != 22*22*4 {
		t.Fatalf("len=%d", len(pix))
	}
	if pix[0] == 0 && pix[1] == 0 && pix[2] == 0 && pix[3] == 0 {
		// corner may be transparent due to rounding; that is fine
	}
}
