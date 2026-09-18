package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"nelko-print/internal/imaging"
	"nelko-print/internal/tspl"
)

// Separate format from Designer files, which contain positioned elements.
type textLabelFile struct {
	Format        string  `json:"format"`
	Text          string  `json:"text"`
	LabelSize     string  `json:"labelSize"`
	FontSize      float64 `json:"fontSize"`
	Orientation   string  `json:"orientation"`
	TextInvert    bool    `json:"textInvert"`
	WordBreakOnly bool    `json:"wordBreakOnly"`
	Density       int     `json:"density"`
	Copies        int     `json:"copies"`
	Threshold     int     `json:"threshold"`
	Invert        bool    `json:"invert"`
}

func decodeTextLabel(data []byte) (textLabelFile, error) {
	var d textLabelFile
	if err := json.Unmarshal(data, &d); err != nil {
		return d, err
	}
	if d.Format != "nelko-text-v1" {
		return d, fmt.Errorf("This is not a saved Text label (.ntext)")
	}
	validSize := false
	for _, size := range tspl.AllSizes {
		if size.Name == d.LabelSize {
			validSize = true
		}
	}
	if !validSize || math.IsNaN(d.FontSize) || math.IsInf(d.FontSize, 0) || d.FontSize < 4 || d.FontSize > 72 ||
		(d.Orientation != "Horizontal" && d.Orientation != "Vertical") || d.Density < 0 || d.Density > 15 ||
		d.Copies < 1 || d.Threshold < 0 || d.Threshold > 255 {
		return d, fmt.Errorf("The saved Text label contains invalid settings")
	}
	return d, nil
}

func (a *App) saveTextLabel() {
	orientation := "Horizontal"
	if a.orientation == imaging.Vertical {
		orientation = "Vertical"
	}
	d := textLabelFile{Format: "nelko-text-v1", Text: a.textEntry.Text, LabelSize: a.labelSize.Name,
		FontSize: a.fontSize, Orientation: orientation, TextInvert: a.textInvert, WordBreakOnly: a.wordBreakOnly,
		Density: a.density, Copies: a.copies, Threshold: int(a.threshold), Invert: a.invert}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if w == nil {
			return
		}
		_, err = w.Write(data)
		closeErr := w.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		a.statusLabel.SetText("Text label saved")
	}, a.window)
	fd.SetFileName("label.ntext")
	fd.Show()
}

func (a *App) loadTextLabel(apply func(textLabelFile)) {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if r == nil {
			return
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, 4*1024*1024+1))
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if len(data) > 4*1024*1024 {
			dialog.ShowError(fmt.Errorf("Text label file is too large"), a.window)
			return
		}
		d, err := decodeTextLabel(data)
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		apply(d)
		a.statusLabel.SetText("Text label loaded")
	}, a.window)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".ntext", ".json"}))
	fd.Show()
}

func (a *App) exportTextPNG() {
	if a.textEntry.Text == "" {
		dialog.ShowError(fmt.Errorf("Enter label text before exporting"), a.window)
		return
	}
	img, err := imaging.RenderTextWithOptions(a.textEntry.Text, a.labelSize.PixelW, a.labelSize.PixelH,
		imaging.TextOptions{FontSize: a.fontSize, Orientation: a.orientation, Invert: a.textInvert, WordBreakOnly: a.wordBreakOnly})
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	// Export in print orientation so Load Image can print the PNG again.
	mono := imaging.ToMonochrome(img, a.labelSize.PixelW, a.labelSize.PixelH, a.threshold, a.invert)
	output := imaging.PreviewMonochrome(mono, a.labelSize.PixelW, a.labelSize.PixelH)
	fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if w == nil {
			return
		}
		err = png.Encode(w, output)
		closeErr := w.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		a.statusLabel.SetText("Text label exported as PNG")
	}, a.window)
	fd.SetFileName("label.png")
	fd.Show()
}
