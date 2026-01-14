package main

import (
	"fmt"
	"image"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"nelko-print/internal/imaging"
	"nelko-print/internal/printer"
	"nelko-print/internal/tspl"
)

type App struct {
	window     fyne.Window
	printer    *printer.Printer
	sourceImg  image.Image
	previewImg *canvas.Image

	// Settings
	labelSize   tspl.LabelSize
	density     int
	threshold   uint8
	copies      int
	invert      bool

	// Widgets that need updating
	statusLabel   *widget.Label
	connectBtn    *widget.Button
	printBtn      *widget.Button
	portSelect    *widget.Select

	// Text mode
    textEntry    *widget.Entry
    orientation  imaging.Orientation
    fontSize     float64
}

func main() {
	a := app.New()
	w := a.NewWindow("Nelko P21 Print")
	w.Resize(fyne.NewSize(600, 500))

	nelkoApp := &App{
		window:    w,
		labelSize: tspl.Label14x40,
		density:   10,
		threshold: 128,
		copies:    1,
		invert:    false,
		fontSize:    24,
    	orientation: imaging.Horizontal,
	}

	w.SetContent(nelkoApp.buildUI())
	w.ShowAndRun()
}

func (a *App) buildUI() fyne.CanvasObject {
    // Status bar
    a.statusLabel = widget.NewLabel("Not connected")

    // Connection section (same as before)
    a.portSelect = widget.NewSelect([]string{}, func(s string) {})
    a.refreshPorts()

    refreshBtn := widget.NewButton("↻", func() {
        a.refreshPorts()
    })

    a.connectBtn = widget.NewButton("Connect", func() {
        a.connect()
    })

    connectionRow := container.NewBorder(
        nil, nil, nil,
        container.NewHBox(refreshBtn, a.connectBtn),
        a.portSelect,
    )

    // Print settings (shared)
    sizeOptions := make([]string, len(tspl.AllSizes))
    for i, s := range tspl.AllSizes {
        sizeOptions[i] = s.Name
    }
    sizeSelect := widget.NewSelect(sizeOptions, func(s string) {
        for _, size := range tspl.AllSizes {
            if size.Name == s {
                a.labelSize = size
                a.updatePreview()
                break
            }
        }
    })
    sizeSelect.SetSelected(a.labelSize.Name)

    densitySlider := widget.NewSlider(0, 15)
    densitySlider.Value = float64(a.density)
    densitySlider.OnChanged = func(f float64) {
        a.density = int(f)
    }

    copiesEntry := widget.NewEntry()
    copiesEntry.SetText("1")
    copiesEntry.OnChanged = func(s string) {
        var n int
        fmt.Sscanf(s, "%d", &n)
        if n > 0 {
            a.copies = n
        }
    }

    // Print button
    a.printBtn = widget.NewButton("Print", func() {
        a.print()
    })
    a.printBtn.Importance = widget.HighImportance
    a.printBtn.Disable()

    // === IMAGE TAB ===
    thresholdSlider := widget.NewSlider(0, 255)
    thresholdSlider.Value = float64(a.threshold)
    thresholdSlider.OnChanged = func(f float64) {
        a.threshold = uint8(f)
        a.updatePreview()
    }

    invertCheck := widget.NewCheck("Invert", func(b bool) {
        a.invert = b
        a.updatePreview()
    })

    loadBtn := widget.NewButton("Load Image", func() {
        a.loadImage()
    })

    imageSettings := widget.NewForm(
        widget.NewFormItem("Threshold", thresholdSlider),
        widget.NewFormItem("", invertCheck),
    )

    imageTab := container.NewVBox(
        loadBtn,
        imageSettings,
    )

    // === TEXT TAB ===
    a.textEntry = widget.NewMultiLineEntry()
    a.textEntry.SetPlaceHolder("Enter label text...")
    a.textEntry.SetMinRowsVisible(3)
    a.textEntry.OnChanged = func(s string) {
        a.updateTextPreview()
    }

    orientationSelect := widget.NewSelect([]string{"Horizontal", "Vertical"}, func(s string) {
        if s == "Vertical" {
            a.orientation = imaging.Vertical
        } else {
            a.orientation = imaging.Horizontal
        }
        a.updateTextPreview()
    })
    orientationSelect.SetSelected("Horizontal")

    fontSizeSlider := widget.NewSlider(8, 72)
    fontSizeSlider.Value = a.fontSize
    fontSizeSlider.OnChanged = func(f float64) {
        a.fontSize = f
        a.updateTextPreview()
    }

    textSettings := widget.NewForm(
        widget.NewFormItem("Orientation", orientationSelect),
        widget.NewFormItem("Font Size", fontSizeSlider),
    )

    textTab := container.NewVBox(
        a.textEntry,
        textSettings,
    )

    // === TABS ===
    tabs := container.NewAppTabs(
        container.NewTabItem("Image", imageTab),
        container.NewTabItem("Text", textTab),
    )

    // Preview
    a.previewImg = canvas.NewImageFromImage(nil)
    a.previewImg.SetMinSize(fyne.NewSize(200, 300))
    a.previewImg.FillMode = canvas.ImageFillContain

    // Left panel
    leftPanel := container.NewVBox(
        widget.NewLabel("Connection"),
        connectionRow,
        widget.NewSeparator(),
        widget.NewLabel("Label Size"),
        sizeSelect,
        widget.NewLabel("Density"),
        densitySlider,
        widget.NewLabel("Copies"),
        copiesEntry,
        widget.NewSeparator(),
        a.printBtn,
    )

    // Right panel
    rightPanel := container.NewBorder(
        tabs,
        nil, nil, nil,
        container.NewCenter(a.previewImg),
    )

    content := container.NewHSplit(leftPanel, rightPanel)
    content.SetOffset(0.35)

    return container.NewBorder(
        nil,
        container.NewHBox(a.statusLabel),
        nil, nil,
        content,
    )
}

func (a *App) refreshPorts() {
	// Look for /dev/rfcomm* devices
	ports, _ := printer.FindRFCOMMDevices()
	
	// Also add common serial ports
	commonPorts := []string{"/dev/rfcomm0", "/dev/rfcomm1", "/dev/ttyUSB0", "/dev/ttyACM0"}
	for _, p := range commonPorts {
		if _, err := os.Stat(p); err == nil {
			found := false
			for _, existing := range ports {
				if existing == p {
					found = true
					break
				}
			}
			if !found {
				ports = append(ports, p)
			}
		}
	}

	a.portSelect.Options = ports
	if len(ports) > 0 {
		a.portSelect.SetSelected(ports[0])
	}
}

func (a *App) connect() {
	if a.printer != nil {
		a.printer.Close()
		a.printer = nil
		a.connectBtn.SetText("Connect")
		a.statusLabel.SetText("Disconnected")
		a.printBtn.Disable()
		return
	}

	port := a.portSelect.Selected
	if port == "" {
		dialog.ShowError(fmt.Errorf("no port selected"), a.window)
		return
	}

	p, err := printer.Connect(port)
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	a.printer = p
	a.connectBtn.SetText("Disconnect")
	a.statusLabel.SetText(fmt.Sprintf("Connected to %s", port))

	// Try to get battery
	if batt, err := p.GetBattery(); err == nil {
		a.statusLabel.SetText(fmt.Sprintf("Connected to %s (Battery: %d%%)", port, batt))
	}

	if a.sourceImg != nil {
		a.printBtn.Enable()
	}
}

func (a *App) loadImage() {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()

		img, _, err := image.Decode(reader)
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		a.sourceImg = img
		a.updatePreview()

		if a.printer != nil {
			a.printBtn.Enable()
		}
	}, a.window)

	fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp"}))
	fd.Show()
}

func (a *App) updatePreview() {
	if a.sourceImg == nil {
		return
	}

	// Convert to monochrome for preview
	mono := imaging.ToMonochrome(a.sourceImg, a.labelSize.PixelW, a.labelSize.PixelH, a.threshold, a.invert)
	preview := imaging.PreviewMonochrome(mono, a.labelSize.PixelW, a.labelSize.PixelH)

	a.previewImg.Image = preview
	a.previewImg.Refresh()
}

func (a *App) updateTextPreview() {
    text := a.textEntry.Text
    if text == "" {
        return
    }

    img, err := imaging.RenderText(text, a.labelSize.PixelW, a.labelSize.PixelH, a.fontSize, a.orientation)
    if err != nil {
        return
    }

    a.sourceImg = img
    a.updatePreview()

    if a.printer != nil {
        a.printBtn.Enable()
    }
}

func (a *App) print() {
	if a.printer == nil {
		dialog.ShowError(fmt.Errorf("not connected to printer"), a.window)
		return
	}

	if a.sourceImg == nil {
		dialog.ShowError(fmt.Errorf("no image loaded"), a.window)
		return
	}

	// Convert image to bitmap
	bitmap := imaging.ToMonochrome(a.sourceImg, a.labelSize.PixelW, a.labelSize.PixelH, a.threshold, a.invert)

	// Build print job
	job := tspl.BuildPrintJob(a.labelSize, a.density, bitmap, a.copies)

	// Send to printer
	a.statusLabel.SetText("Printing...")
	a.printBtn.Disable()

	go func() {
		err := a.printer.Print(job)

		// Update UI on main thread
		a.window.Canvas().Refresh(a.statusLabel)

		if err != nil {
			a.statusLabel.SetText(fmt.Sprintf("Print error: %v", err))
		} else {
			a.statusLabel.SetText("Print complete!")
		}
		a.printBtn.Enable()
	}()
}
