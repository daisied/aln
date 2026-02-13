package ui

import (
	"image"
	"image/color"
	"os"
	"testing"
)

func TestLayoutForAreaLandscapeFillsWidth(t *testing.T) {
	iv := &ImageView{
		img: image.NewRGBA(image.Rect(0, 0, 1600, 900)),
	}

	p := iv.layoutForArea(120, 80, ProtoHalfBlock)
	if p.cols != 120 {
		t.Fatalf("expected landscape image to fill width (120 cols), got %d", p.cols) 
	}
	if p.rows <= 0 || p.rows > 80 {
		t.Fatalf("expected rows in range 1..80, got %d", p.rows)
	}
	if p.offsetX != 0 {
		t.Fatalf("expected no horizontal offset when width is filled, got %d", p.offsetX)
	}
}

func TestLayoutForAreaPortraitFillsHeight(t *testing.T) {
	iv := &ImageView{
		img: image.NewRGBA(image.Rect(0, 0, 900, 1600)),
	}

	p := iv.layoutForArea(120, 40, ProtoHalfBlock)
	if p.rows != 40 {
		t.Fatalf("expected portrait image to fill height (40 rows), got %d", p.rows)
	}
	if p.cols <= 0 || p.cols > 120 {
		t.Fatalf("expected cols in range 1..120, got %d", p.cols)
	}
	if p.offsetY != 0 {
		t.Fatalf("expected no vertical offset when height is filled, got %d", p.offsetY)
	}
}

func TestBlendOverBackgroundTransparentPixel(t *testing.T) {
	bg := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	out := blendOverBackground(color.NRGBA{R: 200, G: 100, B: 50, A: 0}, bg)
	if out != bg {
		t.Fatalf("expected fully transparent pixel to resolve to bg %v, got %v", bg, out)
	}
}

func TestBlendOverBackgroundPartialAlpha(t *testing.T) {
	bg := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	out := blendOverBackground(color.NRGBA{R: 255, G: 0, B: 0, A: 128}, bg)
	if out.R < 126 || out.R > 129 || out.G != 0 || out.B != 0 || out.A != 255 {
		t.Fatalf("unexpected blend result: %+v", out)
	}
}

func TestEffectiveProtocolFallsBackFromSixelWithoutCellMetrics(t *testing.T) {
	iv := &ImageView{protocol: ProtoSixel}
	if got := iv.effectiveProtocol(); got != ProtoSixel {
		t.Fatalf("expected ProtoSixel passthrough, got %v", got)
	}
}

func TestDetectProtocolOverride(t *testing.T) {
	original := os.Getenv("ALN_IMAGE_PROTOCOL")
	t.Cleanup(func() {
		if original == "" {
			_ = os.Unsetenv("ALN_IMAGE_PROTOCOL")
		} else {
			_ = os.Setenv("ALN_IMAGE_PROTOCOL", original)
		}
	})

	if err := os.Setenv("ALN_IMAGE_PROTOCOL", "halfblock"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	if got := DetectProtocol(""); got != ProtoHalfBlock {
		t.Fatalf("expected override ProtoHalfBlock, got %v", got)
	}

	if err := os.Setenv("ALN_IMAGE_PROTOCOL", "sixel"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	if got := DetectProtocol(""); got != ProtoSixel {
		t.Fatalf("expected override ProtoSixel, got %v", got)
	}
}

func TestLayoutCellPixelSizeDefaultsOnBogusRatio(t *testing.T) {
	iv := &ImageView{}
	// No tty in unit tests => should use stable default.
	pw, ph := iv.layoutCellPixelSize(ProtoHalfBlock)
	if pw != 8 || ph != 16 {
		t.Fatalf("expected default 8x16, got %dx%d", pw, ph)
	}
}

func TestLayoutCellPixelSizeSixelDefault(t *testing.T) {
	iv := &ImageView{}
	pw, ph := iv.layoutCellPixelSize(ProtoSixel)
	if pw != 10 || ph != 20 {
		t.Fatalf("expected sixel default 10x20, got %dx%d", pw, ph)
	}
}

func TestParseCellPixels(t *testing.T) {
	w, h, ok := parseCellPixels("12x24")
	if !ok || w != 12 || h != 24 {
		t.Fatalf("expected 12x24 parse, got %dx%d ok=%v", w, h, ok)
	}
	w, h, ok = parseCellPixels("10,20")
	if !ok || w != 10 || h != 20 {
		t.Fatalf("expected 10x20 parse, got %dx%d ok=%v", w, h, ok)
	}
}

func TestProtocolScaleFromEnv(t *testing.T) {
	origScalar := os.Getenv("ALN_SIXEL_SCALE")
	origX := os.Getenv("ALN_SIXEL_SCALE_X")
	origY := os.Getenv("ALN_SIXEL_SCALE_Y")
	t.Cleanup(func() {
		if origScalar == "" {
			_ = os.Unsetenv("ALN_SIXEL_SCALE")
		} else {
			_ = os.Setenv("ALN_SIXEL_SCALE", origScalar)
		}
		if origX == "" {
			_ = os.Unsetenv("ALN_SIXEL_SCALE_X")
		} else {
			_ = os.Setenv("ALN_SIXEL_SCALE_X", origX)
		}
		if origY == "" {
			_ = os.Unsetenv("ALN_SIXEL_SCALE_Y")
		} else {
			_ = os.Setenv("ALN_SIXEL_SCALE_Y", origY)
		}
	})
	_ = os.Unsetenv("ALN_SIXEL_SCALE_X")
	_ = os.Unsetenv("ALN_SIXEL_SCALE_Y")
	if err := os.Setenv("ALN_SIXEL_SCALE", "1.5"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	sx, sy := protocolScaleXY(ProtoSixel)
	if sx < 1.49 || sx > 1.51 {
		t.Fatalf("expected sixel x scale ~1.5, got %f", sx)
	}
	if sy != 1 {
		t.Fatalf("expected sixel y scale 1.0 from scalar fallback, got %f", sy)
	}

	if err := os.Setenv("ALN_SIXEL_SCALE_Y", "1.2"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	_, sy = protocolScaleXY(ProtoSixel)
	if sy < 1.19 || sy > 1.21 {
		t.Fatalf("expected sixel y scale override ~1.2, got %f", sy)
	}
}

func TestLayoutForAreaSixelScalarDoesNotChangeCellPlacement(t *testing.T) {
	orig := os.Getenv("ALN_SIXEL_SCALE")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("ALN_SIXEL_SCALE")
		} else {
			_ = os.Setenv("ALN_SIXEL_SCALE", orig)
		}
	})

	if err := os.Setenv("ALN_SIXEL_SCALE", "1.6"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}

	iv := &ImageView{
		img: image.NewRGBA(image.Rect(0, 0, 1600, 900)),
	}
	p := iv.layoutForArea(120, 80, ProtoSixel)
	if p.cols != 120 {
		t.Fatalf("expected cols to remain fit-driven (120), got %d", p.cols)
	}
	if p.pixelW <= p.cols {
		t.Fatalf("expected pixel width scaling to apply, got pixelW=%d cols=%d", p.pixelW, p.cols)
	}
}

func TestSixelCellPixelsOverride(t *testing.T) {
	orig := os.Getenv("ALN_SIXEL_CELL_PIXELS")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("ALN_SIXEL_CELL_PIXELS")
		} else {
			_ = os.Setenv("ALN_SIXEL_CELL_PIXELS", orig)
		}
	})
	if err := os.Setenv("ALN_SIXEL_CELL_PIXELS", "13x26"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	iv := &ImageView{}
	pw, ph := iv.layoutCellPixelSize(ProtoSixel)
	if pw != 13 || ph != 26 {
		t.Fatalf("expected sixel override 13x26, got %dx%d", pw, ph)
	}
}
