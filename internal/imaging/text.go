package imaging

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"unicode"

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

// TextOptions configures text rendering
type TextOptions struct {
	FontSize      float64
	Orientation   Orientation
	Invert        bool // White text on black background
	WordBreakOnly bool // Only break lines on spaces, not mid-word
}

// RenderText creates an image from text (legacy wrapper)
func RenderText(text string, width, height int, fontSize float64, orientation Orientation) (image.Image, error) {
	return RenderTextWithOptions(text, width, height, TextOptions{
		FontSize:    fontSize,
		Orientation: orientation,
	})
}

// RenderTextWithOptions creates an image from text with full options
func RenderTextWithOptions(text string, width, height int, opts TextOptions) (image.Image, error) {
	// Load font
	f, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}

	// For vertical, we swap dimensions for initial render, then rotate
	renderW, renderH := width, height
	if opts.Orientation == Vertical {
		renderW, renderH = height, width
	}

	// Set colors based on invert option
	bgColor := color.White
	fgColor := color.Black
	if opts.Invert {
		bgColor = color.Black
		fgColor = color.White
	}

	// Create background
	img := image.NewRGBA(image.Rect(0, 0, renderW, renderH))
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// Set up freetype context
	c := freetype.NewContext()
	c.SetDPI(203) // Match printer DPI
	c.SetFont(f)
	c.SetFontSize(opts.FontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(&image.Uniform{fgColor})
	c.SetHinting(font.HintingFull)

	// Calculate text position (centered vertically)
	face := truetype.NewFace(f, &truetype.Options{Size: opts.FontSize, DPI: 203})
	metrics := face.Metrics()
	textHeight := metrics.Ascent.Ceil()

	// Word wrap and draw
	var lines []string
	if opts.WordBreakOnly {
		lines = wrapTextWordOnly(text, face, renderW-10)
	} else {
		lines = wrapText(text, face, renderW-10)
	}
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
	if opts.Orientation == Vertical {
		return rotate90CW(img), nil
	}

	return img, nil
}

// wrapText splits text into lines that fit within maxWidth (breaks anywhere)
func wrapText(text string, face font.Face, maxWidth int) []string {
	var lines []string
	var currentLine string

	for _, char := range text {
		if char == '\n' {
			lines = append(lines, currentLine)
			currentLine = ""
			continue
		}
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

// wrapTextWordOnly splits text into lines, only breaking at word boundaries
func wrapTextWordOnly(text string, face font.Face, maxWidth int) []string {
	var lines []string

	// First split by explicit newlines
	paragraphs := strings.Split(text, "\n")

	for _, para := range paragraphs {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := words[0]
		for i := 1; i < len(words); i++ {
			word := words[i]
			testLine := currentLine + " " + word

			if measureString(face, testLine) > maxWidth {
				// Current line is full, start new line
				lines = append(lines, currentLine)

				// Check if single word is too long
				if measureString(face, word) > maxWidth {
					// Word itself is too long, we need to break it
					currentLine = breakLongWord(word, face, maxWidth, &lines)
				} else {
					currentLine = word
				}
			} else {
				currentLine = testLine
			}
		}

		if currentLine != "" {
			lines = append(lines, currentLine)
		}
	}

	return lines
}

// breakLongWord breaks a single word that's too long to fit
func breakLongWord(word string, face font.Face, maxWidth int, lines *[]string) string {
	var currentPart string
	for _, char := range word {
		testPart := currentPart + string(char)
		if measureString(face, testPart) > maxWidth && currentPart != "" {
			*lines = append(*lines, currentPart)
			currentPart = string(char)
		} else {
			currentPart = testPart
		}
	}
	return currentPart
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

// rotate90CCW rotates an image 90 degrees counter-clockwise
func rotate90CCW(src image.Image) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, h, w))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, w-1-x, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return dst
}

// RotatePreviewForDisplay rotates a vertical-orientation image for on-screen display
// so the text reads correctly (counter-clockwise)
func RotatePreviewForDisplay(img image.Image) image.Image {
	return rotate90CCW(img)
}

// IsWhitespace checks if a rune is whitespace
func IsWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}
