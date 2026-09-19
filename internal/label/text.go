package label

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/golang/freetype/truetype"

	"nelko-print/internal/imaging"
)

type TextElement struct {
	Base
	Text     string
	FontSize float64
	Invert   bool
}

func NewText(text string) *TextElement {
	return &TextElement{
		Base:     Base{B: Rect{0, 0, 200, 80}},
		Text:     text,
		FontSize: 24,
	}
}

func (t *TextElement) Raster(scale float64) image.Image {
	w := int(math.Round(t.B.W * scale))
	h := int(math.Round(t.B.H * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Transparent background unless inverted (then opaque black box).
	if t.Invert {
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
	}
	if t.Text == "" {
		return RenderRotated(img, t.A)
	}

	f := imaging.PrimaryFont()
	if f == nil {
		return RenderRotated(img, t.A)
	}
	size := t.FontSize * scale
	fg := color.RGBA{0, 0, 0, 255}
	if t.Invert {
		fg = color.RGBA{255, 255, 255, 255}
	}

	primaryFace := truetype.NewFace(f, &truetype.Options{Size: size, DPI: 203})
	emojiFace := imaging.NewEmojiFace(size, 203)

	metrics := primaryFace.Metrics()
	lineH := metrics.Height.Ceil()
	lines := imaging.WrapWordOnlyWithEmoji(t.Text, primaryFace, emojiFace, w-2)
	y := metrics.Ascent.Ceil()
	for _, line := range lines {
		lw := imaging.MeasureStringWithEmoji(primaryFace, emojiFace, line)
		x := (w - lw) / 2
		if x < 0 {
			x = 0
		}
		imaging.DrawStringFallback(img, &image.Uniform{fg}, primaryFace, emojiFace, line, x, y)
		y += lineH
	}

	// Close faces to avoid resource leaks.
	primaryFace.Close()
	if emojiFace != nil {
		emojiFace.Close()
	}

	return RenderRotated(img, t.A)
}
