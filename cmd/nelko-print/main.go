package main

import (
	"fmt"
	"image"
	"net/url"
	"os"
	"strings"

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

const (
	AppVersion = "1.2.0"
	AppName    = "Nelko P21 Print"
)

type App struct {
	fyneApp    fyne.App
	window     fyne.Window
	printer    *printer.Printer
	rfcommConn *printer.RFCOMMConnection
	sourceImg  image.Image
	previewImg *canvas.Image

	// Settings
	labelSize tspl.LabelSize
	density   int
	threshold uint8
	copies    int
	invert    bool

	// Widgets that need updating
	statusLabel    *widget.Label
	connectBtn     *widget.Button
	printBtn       *widget.Button
	btDeviceSelect *widget.Select
	portSelect     *widget.Select
	refreshBTBtn   *widget.Button

	// Bluetooth devices cache
	btDevices []printer.BluetoothDevice

	// Text mode
	textEntry     *widget.Entry
	orientation   imaging.Orientation
	fontSize      float64
	textInvert    bool
	wordBreakOnly bool
}

func main() {
	a := app.New()
	w := a.NewWindow(fmt.Sprintf("%s v%s", AppName, AppVersion))
	w.Resize(fyne.NewSize(650, 550))

	nelkoApp := &App{
		fyneApp:       a,
		window:        w,
		labelSize:     tspl.Label14x40,
		density:       10,
		threshold:     128,
		copies:        1,
		invert:        false,
		fontSize:      24,
		orientation:   imaging.Horizontal,
		textInvert:    false,
		wordBreakOnly: false,
	}

	// Set up menu
	w.SetMainMenu(nelkoApp.buildMenu())
	w.SetContent(nelkoApp.buildUI())
	w.SetOnClosed(func() {
		nelkoApp.cleanup()
	})
	w.ShowAndRun()
}

func (a *App) buildMenu() *fyne.MainMenu {
	// Help menu with About
	aboutItem := fyne.NewMenuItem("About", func() {
		a.showAboutDialog()
	})

	helpMenu := fyne.NewMenu("Help", aboutItem)

	return fyne.NewMainMenu(helpMenu)
}

func (a *App) showAboutDialog() {
	content := container.NewVBox(
		widget.NewLabelWithStyle(AppName, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(fmt.Sprintf("Version %s", AppVersion)),
		widget.NewSeparator(),
		widget.NewLabel("A label printing app for the Nelko P21 thermal printer."),
		widget.NewLabel(""),
		widget.NewLabel("Based on reverse engineering work from:"),
		widget.NewHyperlink("merlinschumacher/nelko-p21-print", parseURL("https://github.com/merlinschumacher/nelko-p21-print")),
		widget.NewLabel(""),
		widget.NewLabel("Built with Fyne and Go"),
	)

	dialog.ShowCustom("About", "Close", content, a.window)
}

func parseURL(urlStr string) *url.URL {
	u, _ := url.Parse(urlStr)
	return u
}

func (a *App) cleanup() {
	if a.printer != nil {
		a.printer.Close()
	}
	if a.rfcommConn != nil {
		a.rfcommConn.Close()
	}
}

func (a *App) buildUI() fyne.CanvasObject {
	// Status bar
	a.statusLabel = widget.NewLabel("Not connected")

	// === BLUETOOTH CONNECTION SECTION ===
	btLabel := widget.NewLabel("Bluetooth Printer:")
	a.btDeviceSelect = widget.NewSelect([]string{}, func(s string) {})
	a.refreshBTBtn = widget.NewButton("↻", func() {
		a.refreshBluetoothDevices()
	})

	a.connectBtn = widget.NewButton("Connect", func() {
		a.connectBluetooth()
	})

	// Refresh BT devices on startup
	go a.refreshBluetoothDevices()

	btRow := container.NewBorder(
		nil, nil, nil,
		container.NewHBox(a.refreshBTBtn, a.connectBtn),
		a.btDeviceSelect,
	)

	// === MANUAL PORT SECTION (collapsible/advanced) ===
	a.portSelect = widget.NewSelect([]string{}, func(s string) {})
	a.refreshPorts()

	manualConnectBtn := widget.NewButton("Connect Port", func() {
		a.connectManualPort()
	})

	manualRefreshBtn := widget.NewButton("↻", func() {
		a.refreshPorts()
	})

	manualRow := container.NewBorder(
		nil, nil, nil,
		container.NewHBox(manualRefreshBtn, manualConnectBtn),
		a.portSelect,
	)

	// Advanced section (manual port)
	advancedContent := container.NewVBox(
		widget.NewLabel("Manual Port (if already connected):"),
		manualRow,
	)

	// Print settings
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

	fontSizeSlider := widget.NewSlider(4, 72) // Reduced min from 8 to 4
	fontSizeSlider.Value = a.fontSize
	fontSizeSlider.OnChanged = func(f float64) {
		a.fontSize = f
		a.updateTextPreview()
	}

	textInvertCheck := widget.NewCheck("Invert", func(b bool) {
		a.textInvert = b
		a.updateTextPreview()
	})

	wordBreakCheck := widget.NewCheck("Break on space only", func(b bool) {
		a.wordBreakOnly = b
		a.updateTextPreview()
	})

	textSettings := widget.NewForm(
		widget.NewFormItem("Orientation", orientationSelect),
		widget.NewFormItem("Font Size", fontSizeSlider),
		widget.NewFormItem("", textInvertCheck),
		widget.NewFormItem("", wordBreakCheck),
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

	// Left panel - Connection and Settings
	leftPanel := container.NewVBox(
		btLabel,
		btRow,
		widget.NewSeparator(),
		widget.NewAccordion(
			widget.NewAccordionItem("Advanced", advancedContent),
		),
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
	content.SetOffset(0.38)

	return container.NewBorder(
		nil,
		container.NewHBox(a.statusLabel),
		nil, nil,
		content,
	)
}

func (a *App) refreshBluetoothDevices() {
	a.statusLabel.SetText("Scanning for paired devices...")

	devices, err := printer.ListPairedBluetoothDevices()
	if err != nil {
		a.statusLabel.SetText(fmt.Sprintf("BT scan failed: %v", err))
		return
	}

	a.btDevices = devices

	// Build display list
	options := make([]string, len(devices))
	for i, d := range devices {
		options[i] = fmt.Sprintf("%s (%s)", d.Name, d.MAC)
	}

	a.btDeviceSelect.Options = options
	if len(options) > 0 {
		// Try to auto-select a Nelko device if present
		selectedIdx := 0
		for i, d := range devices {
			if strings.Contains(strings.ToLower(d.Name), "nelko") ||
				strings.Contains(strings.ToLower(d.Name), "p21") {
				selectedIdx = i
				break
			}
		}
		a.btDeviceSelect.SetSelected(options[selectedIdx])
	}

	a.statusLabel.SetText(fmt.Sprintf("Found %d paired device(s)", len(devices)))
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

func (a *App) getSelectedBluetoothDevice() *printer.BluetoothDevice {
	selectedIdx := a.btDeviceSelect.SelectedIndex()
	if selectedIdx < 0 || selectedIdx >= len(a.btDevices) {
		return nil
	}
	return &a.btDevices[selectedIdx]
}

func (a *App) connectBluetooth() {
	// If already connected, disconnect
	if a.printer != nil {
		a.disconnect()
		return
	}

	device := a.getSelectedBluetoothDevice()
	if device == nil {
		dialog.ShowError(fmt.Errorf("no Bluetooth device selected"), a.window)
		return
	}

	// Check for rfcomm
	if err := printer.CheckRFCOMMInstalled(); err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	// Check for privilege helper
	helper := printer.CheckPrivilegeHelper()
	if helper == "" {
		dialog.ShowError(fmt.Errorf("no privilege helper found (need pkexec or sudo)"), a.window)
		return
	}

	// Disable button during connection
	a.connectBtn.Disable()
	a.btDeviceSelect.Disable()
	a.refreshBTBtn.Disable()

	go func() {
		a.statusLabel.SetText(fmt.Sprintf("Connecting to %s...", device.Name))

		// Establish RFCOMM connection
		conn, err := printer.EstablishRFCOMM(device.MAC, 1, func(status string) {
			a.statusLabel.SetText(status)
		})

		if err != nil {
			a.statusLabel.SetText(fmt.Sprintf("Connection failed: %v", err))
			a.connectBtn.Enable()
			a.btDeviceSelect.Enable()
			a.refreshBTBtn.Enable()

			// Show error dialog
			dialog.ShowError(fmt.Errorf("failed to connect: %v", err), a.window)
			return
		}

		a.rfcommConn = conn

		// Now connect to the serial port
		p, err := printer.Connect(conn.DevicePath)
		if err != nil {
			conn.Close()
			a.rfcommConn = nil
			a.statusLabel.SetText(fmt.Sprintf("Serial connect failed: %v", err))
			a.connectBtn.Enable()
			a.btDeviceSelect.Enable()
			a.refreshBTBtn.Enable()
			dialog.ShowError(err, a.window)
			return
		}

		a.printer = p
		a.connectBtn.SetText("Disconnect")
		a.connectBtn.Enable()
		a.statusLabel.SetText(fmt.Sprintf("Connected to %s via %s", device.Name, conn.DevicePath))

		// Try to get battery
		if batt, err := p.GetBattery(); err == nil {
			a.statusLabel.SetText(fmt.Sprintf("Connected to %s (Battery: %d%%)", device.Name, batt))
		}

		if a.sourceImg != nil {
			a.printBtn.Enable()
		}

		// Refresh ports list to show the new device
		a.refreshPorts()
	}()
}

func (a *App) connectManualPort() {
	// If already connected, disconnect
	if a.printer != nil {
		a.disconnect()
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

func (a *App) disconnect() {
	if a.printer != nil {
		a.printer.Close()
		a.printer = nil
	}

	if a.rfcommConn != nil {
		a.rfcommConn.Close()
		a.rfcommConn = nil
	}

	a.connectBtn.SetText("Connect")
	a.btDeviceSelect.Enable()
	a.refreshBTBtn.Enable()
	a.statusLabel.SetText("Disconnected")
	a.printBtn.Disable()
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

	// For vertical orientation, rotate the preview so text is readable on screen
	if a.orientation == imaging.Vertical {
		preview = imaging.RotatePreviewForDisplay(preview)
	}

	a.previewImg.Image = preview
	a.previewImg.Refresh()
}

func (a *App) updateTextPreview() {
	text := a.textEntry.Text
	if text == "" {
		return
	}

	opts := imaging.TextOptions{
		FontSize:      a.fontSize,
		Orientation:   a.orientation,
		Invert:        a.textInvert,
		WordBreakOnly: a.wordBreakOnly,
	}

	img, err := imaging.RenderTextWithOptions(text, a.labelSize.PixelW, a.labelSize.PixelH, opts)
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
	bitmap := imaging.ToMonochrome(a.sourceImg, a.labelSize.PixelW, a.labelSize.PixelH, a.threshold, !a.invert)

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
