package tspl

import (
	"fmt"
	"strings"
)

// LabelSize represents a supported label dimension
type LabelSize struct {
	Name   string
	Width  float64 // mm
	Height float64 // mm
	PixelW int     // pixels (96 for P21)
	PixelH int     // pixels
}

// Common P21 label sizes
var (
	Label12x40 = LabelSize{"12x40mm", 12.0, 40.0, 96, 284}
	Label14x40 = LabelSize{"14x40mm", 14.0, 40.0, 96, 284}
	Label14x50 = LabelSize{"14x50mm", 14.0, 50.0, 96, 355}
	Label14x75 = LabelSize{"14x75mm", 14.0, 75.0, 96, 532}
	Label15x30 = LabelSize{"15x30mm", 15.0, 30.0, 96, 213}
)

var AllSizes = []LabelSize{Label12x40, Label14x40, Label14x50, Label14x75, Label15x30}

// Command builds TSPL2 commands
type Command struct {
	buf strings.Builder
}

func New() *Command {
	return &Command{}
}

// Size sets label dimensions
func (c *Command) Size(width, height float64) *Command {
	fmt.Fprintf(&c.buf, "SIZE %.1f mm,%.1f mm\r\n", width, height)
	return c
}

// Gap sets gap between labels
func (c *Command) Gap(gap, offset float64) *Command {
	fmt.Fprintf(&c.buf, "GAP %.1f mm,%.1f mm\r\n", gap, offset)
	return c
}

// Direction sets print direction (0 or 1)
func (c *Command) Direction(dir, mirror int) *Command {
	fmt.Fprintf(&c.buf, "DIRECTION %d,%d\r\n", dir, mirror)
	return c
}

// Density sets print darkness (0-15)
func (c *Command) Density(level int) *Command {
	if level < 0 {
		level = 0
	}
	if level > 15 {
		level = 15
	}
	fmt.Fprintf(&c.buf, "DENSITY %d\r\n", level)
	return c
}

// CLS clears the image buffer
func (c *Command) CLS() *Command {
	c.buf.WriteString("CLS\r\n")
	return c
}

// Bitmap adds a bitmap image
// x, y: position in dots
// widthBytes: width in bytes (pixels / 8)
// height: height in dots
// data: raw 1-bit bitmap data
func (c *Command) Bitmap(x, y, widthBytes, height int, data []byte) *Command {
	fmt.Fprintf(&c.buf, "BITMAP %d,%d,%d,%d,1,", x, y, widthBytes, height)
	c.buf.Write(data)
	c.buf.WriteString("\r\n")
	return c
}

// Print prints n copies
func (c *Command) Print(copies int) *Command {
	fmt.Fprintf(&c.buf, "PRINT %d\r\n", copies)
	return c
}

// Bytes returns the raw command bytes to send to printer
func (c *Command) Bytes() []byte {
	return []byte(c.buf.String())
}

// String returns the command as a string (for debugging)
func (c *Command) String() string {
	return c.buf.String()
}

// BuildPrintJob creates a complete print job for the P21
func BuildPrintJob(size LabelSize, density int, bitmap []byte, copies int) []byte {
	cmd := New()
	cmd.Size(size.Width, size.Height).
		Gap(5.0, 0).
		Direction(0, 0).
		Density(density).
		CLS().
		Bitmap(0, 0, size.PixelW/8, size.PixelH, bitmap).
		Print(copies)
	return cmd.Bytes()
}
