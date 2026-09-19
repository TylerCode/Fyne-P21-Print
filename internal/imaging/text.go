package imaging

import (
	"image"
	"image/color"
	"image/draw"
	"log"
	"strings"
	"sync"
	"unicode"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"nelko-print/internal/imaging/fonts"
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

// ---- Shared parsed fonts (parse once, reuse forever) ----

var (
	primaryFont  *truetype.Font
	emojiFont    *opentype.Font
	emojiFontErr error
	fontInitOnce sync.Once
)

func initFonts() {
	fontInitOnce.Do(func() {
		var err error
		primaryFont, err = truetype.Parse(goregular.TTF)
		if err != nil {
			log.Printf("WARNING: failed to parse primary font: %v", err)
		}
		emojiFont, emojiFontErr = opentype.Parse(fonts.NotoEmojiTTF)
		if emojiFontErr != nil {
			log.Printf("WARNING: failed to parse emoji font: %v", emojiFontErr)
		}
	})
}

// PrimaryFont returns the parsed Go Regular font.
func PrimaryFont() *truetype.Font {
	initFonts()
	return primaryFont
}

// HasPrimaryGlyph reports whether the primary font contains a glyph for r.
func HasPrimaryGlyph(r rune) bool {
	initFonts()
	if primaryFont == nil {
		return false
	}
	return primaryFont.Index(r) != 0
}

// EmojiFont returns the parsed Noto Emoji opentype.Font (nil if unavailable).
func EmojiFont() *opentype.Font {
	initFonts()
	return emojiFont
}

// NewEmojiFace creates a font.Face from the Noto Emoji font at the given
// size and DPI. Returns nil if the emoji font is unavailable.
func NewEmojiFace(size, dpi float64) font.Face {
	ef := EmojiFont()
	if ef == nil {
		return nil
	}
	face, err := opentype.NewFace(ef, &opentype.FaceOptions{Size: size, DPI: dpi})
	if err != nil {
		return nil
	}
	return face
}

// ---- Rendering ----

// RenderText creates an image from text (legacy wrapper)
func RenderText(text string, width, height int, fontSize float64, orientation Orientation) (image.Image, error) {
	return RenderTextWithOptions(text, width, height, TextOptions{
		FontSize:    fontSize,
		Orientation: orientation,
	})
}

// RenderTextWithOptions creates an image from text with full options
func RenderTextWithOptions(text string, width, height int, opts TextOptions) (image.Image, error) {
	initFonts()

	f := primaryFont
	if f == nil {
		// Catastrophic: can't render anything
		return image.NewRGBA(image.Rect(0, 0, width, height)), nil
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

	// Set up freetype context for primary font
	c := freetype.NewContext()
	c.SetDPI(203) // Match printer DPI
	c.SetFont(f)
	c.SetFontSize(opts.FontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(&image.Uniform{fgColor})
	c.SetHinting(font.HintingFull)

	// Build font faces for measurement and emoji rendering
	primaryFace := truetype.NewFace(f, &truetype.Options{Size: opts.FontSize, DPI: 203})
	emojiFace := NewEmojiFace(opts.FontSize, 203)

	metrics := primaryFace.Metrics()
	textHeight := metrics.Ascent.Ceil()

	// Word wrap and draw
	var lines []string
	if opts.WordBreakOnly {
		lines = wrapTextFallback(text, primaryFace, emojiFace, renderW-10)
	} else {
		lines = wrapTextFallbackAny(text, primaryFace, emojiFace, renderW-10)
	}
	y := (renderH-len(lines)*int(metrics.Height.Ceil()))/2 + textHeight

	for _, line := range lines {
		// Center each line horizontally
		lineWidth := measureStringFallback(primaryFace, emojiFace, line)
		x := (renderW - lineWidth) / 2

		drawStringFallback(img, &image.Uniform{fgColor}, primaryFace, emojiFace, line, x, y)
		y += int(metrics.Height.Ceil())
	}

	// Rotate if vertical
	if opts.Orientation == Vertical {
		return rotate90CW(img), nil
	}

	return img, nil
}

// ---- Fallback-aware drawing ----

// drawStringFallback draws text character-by-character, using emojiFace for
// characters that the primary font doesn't have glyphs for.
func drawStringFallback(dst draw.Image, src image.Image, primary, emoji font.Face, s string, x, y int) {
	dot := fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}
	for _, r := range s {
		face := primary
		if !HasPrimaryGlyph(r) && emoji != nil {
			face = emoji
		}
		dr := font.Drawer{
			Dst:  dst,
			Src:  src,
			Face: face,
			Dot:  dot,
		}
		dr.DrawString(string(r))
		dot = dr.Dot
	}
}

// ---- Fallback-aware measurement ----

// measureStringFallback returns the pixel width of s, using the emoji face
// for characters missing from the primary face.
func measureStringFallback(primary, emoji font.Face, s string) int {
	var width fixed.Int26_6
	for _, r := range s {
		face := primary
		if !HasPrimaryGlyph(r) && emoji != nil {
			face = emoji
		}
		adv, ok := face.GlyphAdvance(r)
		if ok {
			width += adv
		}
	}
	return width.Ceil()
}

// ---- Fallback-aware wrapping ----

// wrapTextFallbackAny splits text into lines that fit within maxWidth (breaks anywhere)
func wrapTextFallbackAny(text string, primary, emoji font.Face, maxWidth int) []string {
	var lines []string
	var currentLine string

	for _, char := range text {
		if char == '\n' {
			lines = append(lines, currentLine)
			currentLine = ""
			continue
		}
		testLine := currentLine + string(char)
		if measureStringFallback(primary, emoji, testLine) > maxWidth && currentLine != "" {
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

// wrapTextFallback splits text into lines, only breaking at word boundaries
func wrapTextFallback(text string, primary, emoji font.Face, maxWidth int) []string {
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

			if measureStringFallback(primary, emoji, testLine) > maxWidth {
				// Current line is full, start new line
				lines = append(lines, currentLine)

				// Check if single word is too long
				if measureStringFallback(primary, emoji, word) > maxWidth {
					// Word itself is too long, we need to break it
					currentLine = breakLongWordFallback(word, primary, emoji, maxWidth, &lines)
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

// breakLongWordFallback breaks a single word that's too long to fit
func breakLongWordFallback(word string, primary, emoji font.Face, maxWidth int, lines *[]string) string {
	var currentPart string
	for _, char := range word {
		testPart := currentPart + string(char)
		if measureStringFallback(primary, emoji, testPart) > maxWidth && currentPart != "" {
			*lines = append(*lines, currentPart)
			currentPart = string(char)
		} else {
			currentPart = testPart
		}
	}
	return currentPart
}

// ---- Legacy public API (maintained for backward compat) ----

// wrapText splits text into lines that fit within maxWidth (breaks anywhere)
func wrapText(text string, face font.Face, maxWidth int) []string {
	return wrapTextFallbackAny(text, face, NewEmojiFace(24, 203), maxWidth)
}

// wrapTextWordOnly splits text into lines, only breaking at word boundaries
func wrapTextWordOnly(text string, face font.Face, maxWidth int) []string {
	return wrapTextFallback(text, face, NewEmojiFace(24, 203), maxWidth)
}

// measureString returns the width of a string in pixels
func measureString(face font.Face, s string) int {
	return measureStringFallback(face, NewEmojiFace(24, 203), s)
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

// MeasureString returns the pixel width of s rendered with face.
func MeasureString(face font.Face, s string) int { return measureString(face, s) }

// MeasureStringWithEmoji returns the pixel width using primary+emoji fallback.
func MeasureStringWithEmoji(primary, emoji font.Face, s string) int {
	return measureStringFallback(primary, emoji, s)
}

// WrapText wraps text to maxWidth, breaking anywhere.
func WrapText(text string, face font.Face, maxWidth int) []string {
	return wrapText(text, face, maxWidth)
}

// WrapWordOnly wraps text to maxWidth, breaking only at spaces.
func WrapWordOnly(text string, face font.Face, maxWidth int) []string {
	return wrapTextWordOnly(text, face, maxWidth)
}

// WrapWordOnlyWithEmoji wraps text to maxWidth with emoji fallback measurement.
func WrapWordOnlyWithEmoji(text string, primary, emoji font.Face, maxWidth int) []string {
	return wrapTextFallback(text, primary, emoji, maxWidth)
}

// DrawStringFallback draws text with primary+emoji font fallback.
func DrawStringFallback(dst draw.Image, src image.Image, primary, emoji font.Face, s string, x, y int) {
	drawStringFallback(dst, src, primary, emoji, s, x, y)
}
