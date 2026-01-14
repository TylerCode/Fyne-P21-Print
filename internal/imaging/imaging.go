package imaging

import (
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// LoadImage loads an image from file
func LoadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

// ToMonochrome converts an image to 1-bit monochrome bitmap
// Returns raw bytes suitable for TSPL BITMAP command
// Width must be divisible by 8
func ToMonochrome(img image.Image, width, height int, threshold uint8, invert bool) []byte {
	// Resize/fit image to target dimensions
	resized := resizeToFit(img, width, height)

	// Width in bytes (8 pixels per byte)
	widthBytes := width / 8
	data := make([]byte, widthBytes*height)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Get pixel, convert to grayscale
			var gray uint8
			if x < resized.Bounds().Dx() && y < resized.Bounds().Dy() {
				c := resized.At(resized.Bounds().Min.X+x, resized.Bounds().Min.Y+y)
				gray = rgbToGray(c)
			} else {
				gray = 255 // white for out of bounds
			}

			// Apply threshold
			var bit uint8
			if gray < threshold {
				bit = 1 // dark pixel
			}

			if invert {
				bit = 1 - bit
			}

			// Pack into byte (MSB first)
			byteIdx := y*widthBytes + x/8
			bitIdx := 7 - (x % 8)
			data[byteIdx] |= bit << bitIdx
		}
	}

	return data
}

// rgbToGray converts a color to grayscale value
func rgbToGray(c color.Color) uint8 {
	r, g, b, _ := c.RGBA()
	// Standard luminance formula, values are 16-bit so divide by 256
	gray := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 256
	return uint8(gray)
}

// resizeToFit scales image to fit within bounds while maintaining aspect ratio
func resizeToFit(img image.Image, maxW, maxH int) image.Image {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Calculate scale factor
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}

	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)

	// Simple nearest-neighbor resize (good enough for thermal printing)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))

	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := int(float64(x) / scale)
			srcY := int(float64(y) / scale)
			if srcX >= srcW {
				srcX = srcW - 1
			}
			if srcY >= srcH {
				srcY = srcH - 1
			}
			dst.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}

	return dst
}

// PreviewMonochrome creates a viewable image from monochrome bitmap data
func PreviewMonochrome(data []byte, width, height int) image.Image {
	widthBytes := width / 8
	img := image.NewGray(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			byteIdx := y*widthBytes + x/8
			bitIdx := 7 - (x % 8)
			bit := (data[byteIdx] >> bitIdx) & 1

			if bit == 1 {
				img.SetGray(x, y, color.Gray{0}) // black
			} else {
				img.SetGray(x, y, color.Gray{255}) // white
			}
		}
	}

	return img
}
