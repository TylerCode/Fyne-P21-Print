package imaging

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

type Orientation int

const (
	Horizontal Orientation = iota
	Vertical
)

// RenderText creates an image from text
func RenderText(text string, width, height int, fontSize float64, orientation Orientation) (image.Image, error) {
	// Load font
	f, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}

	// For vertical, we swap dimensions for initial render, then rotate
	renderW, renderH := width, height
	if orientation == Vertical {
		renderW, renderH = height, width
	}

	// Create white background
	img := image.NewRGBA(image.Rect(0, 0, renderW, renderH))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// Set up freetype context
	c := freetype.NewContext()
	c.SetDPI(203) // Match printer DPI
	c.SetFont(f)
	c.SetFontSize(fontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.Black)
	c.SetHinting(font.HintingFull)

	// Calculate text position (centered vertically)
	face := truetype.NewFace(f, &truetype.Options{Size: fontSize, DPI: 203})
	metrics := face.Metrics()
	textHeight := metrics.Ascent.Ceil()

	// Word wrap and draw
	lines := wrapText(text, face, renderW-10)
	y := (renderH-len(lines)*int(metrics.Height.Ceil()))/2 + textHeight

	for _, line := range lines {
		// Center each line horizontally
		lineWidth := measureString(face, line)
		x := (renderW - lineWidth) / 2

		pt := freetype.Pt(x, y)
		c.DrawString(line, pt)
		y += int(metrics.Height.Ceil())
	}

	// Rotate if vertical
	if orientation == Vertical {
		return rotate90CW(img), nil
	}

	return img, nil
}

// wrapText splits text into lines that fit within maxWidth
func wrapText(text string, face font.Face, maxWidth int) []string {
	var lines []string
	var currentLine string

	for _, char := range text {
		testLine := currentLine + string(char)
		if measureString(face, testLine) > maxWidth && currentLine != "" {
			lines = append(lines, currentLine)
			currentLine = string(char)
		} else {
			currentLine = testLine
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// measureString returns the width of a string in pixels
func measureString(face font.Face, s string) int {
	var width fixed.Int26_6
	for _, r := range s {
		adv, ok := face.GlyphAdvance(r)
		if ok {
			width += adv
		}
	}
	return width.Ceil()
}

// rotate90CW rotates an image 90 degrees clockwise
func rotate90CW(src image.Image) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, h, w))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return dst
}