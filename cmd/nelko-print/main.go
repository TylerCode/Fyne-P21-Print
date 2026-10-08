package main

import (
	"fmt"
	"image"
	"net/url"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"nelko-print/internal/imaging"
	"nelko-print/internal/label"
	"nelko-print/internal/printer"
	"nelko-print/internal/tspl"
)

const (
	AppVersion = "1.3.0"
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

	// Bluetooth LE (BLE-only P21 units)
	bleDeviceSelect *widget.Select
	bleScanBtn      *widget.Button
	bleConnectBtn   *widget.Button
	bleDevices      []printer.BluetoothDevice

	// Text mode
	textEntry     *widget.Entry
	orientation   imaging.Orientation
	fontSize      float64
	textInvert    bool
	wordBreakOnly bool

	// Designer mode
	designDoc    *label.Document
	designCanvas *labelCanvas
	designProps  *fyne.Container

	// Connection health monitor
	healthStop chan struct{}
}

func main() {
	a := app.New()
	w := a.NewWindow(fmt.Sprintf("%s v%s", AppName, AppVersion))
	w.Resize(fyne.NewSize(720, 660))

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
		designDoc:     &label.Document{},
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
	a.refreshBTBtn = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
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

	// === BLUETOOTH LE SECTION (BLE-only P21 units) ===
	bleLabel := widget.NewLabel("Bluetooth LE Printer (if Connect above fails):")
	a.bleDeviceSelect = widget.NewSelect([]string{}, func(s string) {})
	a.bleDeviceSelect.PlaceHolder = "Click scan to find printers"
	a.bleScanBtn = widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		go a.scanBLEDevices()
	})
	a.bleConnectBtn = widget.NewButton("Connect", func() {
		a.connectBLE()
	})

	bleRow := container.NewBorder(
		nil, nil, nil,
		container.NewHBox(a.bleScanBtn, a.bleConnectBtn),
		a.bleDeviceSelect,
	)

	// === MANUAL PORT SECTION (collapsible/advanced) ===
	a.portSelect = widget.NewSelect([]string{}, func(s string) {})
	a.refreshPorts()

	manualConnectBtn := widget.NewButton("Connect Port", func() {
		a.connectManualPort()
	})

	manualRefreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
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
				if a.designCanvas != nil {
					a.designCanvas.SetSize(size)
				}
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
		container.NewTabItem("Designer", a.buildDesignerTab()),
	)
	tabs.OnSelected = func(ti *container.TabItem) {
		if ti.Text == "Designer" {
			a.renderDesignPreview()
		}
	}

	// Preview
	a.previewImg = canvas.NewImageFromImage(nil)
	a.previewImg.SetMinSize(fyne.NewSize(150, 160))
	a.previewImg.FillMode = canvas.ImageFillContain

	// Left panel - Connection and Settings
	leftPanel := container.NewVBox(
		btLabel,
		btRow,
		bleLabel,
		bleRow,
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

	// Right panel: draggable split between the editing tabs (top) and the
	// shared mono preview (bottom). Default gives the tabs/canvas most of the
	// height; drag the divider to resize either side.
	rightPanel := container.NewVSplit(tabs, container.NewCenter(a.previewImg))
	rightPanel.SetOffset(0.66)

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
	// Look for /dev/rfcomm* devices (Linux)
	ports, _ := printer.FindRFCOMMDevices()

	// Add platform-detected serial ports (COM* on Windows, /dev/cu.* on macOS, etc.)
	if platPorts, err := printer.ListSerialPorts(); err == nil {
		for _, p := range platPorts {
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

		// Connected: allow printing. print() guards against an empty design.
		a.printBtn.Enable()
		a.startHealthMonitor()

		// Refresh ports list to show the new device
		a.refreshPorts()
	}()
}

func (a *App) scanBLEDevices() {
	a.bleScanBtn.Disable()
	defer a.bleScanBtn.Enable()
	a.statusLabel.SetText("Scanning for Bluetooth LE printers...")

	devices, err := printer.ScanBLEDevices(5 * time.Second)
	if err != nil {
		a.statusLabel.SetText(fmt.Sprintf("BLE scan failed: %v", err))
		return
	}

	a.bleDevices = devices
	options := make([]string, len(devices))
	for i, d := range devices {
		options[i] = fmt.Sprintf("%s (%s)", d.Name, d.MAC)
	}
	a.bleDeviceSelect.Options = options
	a.bleDeviceSelect.ClearSelected()
	if len(options) > 0 {
		a.bleDeviceSelect.SetSelected(options[0])
	}
	a.bleDeviceSelect.Refresh()

	a.statusLabel.SetText(fmt.Sprintf("Found %d Bluetooth LE printer(s)", len(devices)))
}

func (a *App) connectBLE() {
	// If already connected, disconnect
	if a.printer != nil {
		a.disconnect()
		return
	}

	idx := a.bleDeviceSelect.SelectedIndex()
	if idx < 0 || idx >= len(a.bleDevices) {
		dialog.ShowError(fmt.Errorf("no Bluetooth LE printer selected - click scan first"), a.window)
		return
	}
	device := a.bleDevices[idx]

	a.bleConnectBtn.Disable()
	a.bleScanBtn.Disable()
	a.bleDeviceSelect.Disable()

	go func() {
		a.statusLabel.SetText(fmt.Sprintf("Connecting to %s over Bluetooth LE...", device.Name))

		p, err := printer.ConnectBLE(device.MAC, 20*time.Second)
		if err != nil {
			a.statusLabel.SetText(fmt.Sprintf("Connection failed: %v", err))
			a.bleConnectBtn.Enable()
			a.bleScanBtn.Enable()
			a.bleDeviceSelect.Enable()
			dialog.ShowError(err, a.window)
			return
		}

		a.printer = p
		a.bleConnectBtn.SetText("Disconnect")
		a.bleConnectBtn.Enable()
		a.statusLabel.SetText(fmt.Sprintf("Connected to %s (Bluetooth LE)", device.Name))

		if batt, err := p.GetBattery(); err == nil {
			a.statusLabel.SetText(fmt.Sprintf("Connected to %s (Bluetooth LE, Battery: %d%%)", device.Name, batt))
		}

		// Connected: allow printing. print() guards against an empty design.
		a.printBtn.Enable()
		a.startHealthMonitor()
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

	// Connected: allow printing. print() guards against an empty design.
	a.printBtn.Enable()
	a.startHealthMonitor()
}

// startHealthMonitor polls the printer in the background and reflects its
// liveness in the status label. It stops when stopHealthMonitor is called.
func (a *App) startHealthMonitor() {
	a.stopHealthMonitor()
	stop := make(chan struct{})
	a.healthStop = stop
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p := a.printer
				if p == nil {
					return
				}
				if !p.Alive() {
					a.statusLabel.SetText("⚠ Printer disconnected (Bluetooth link lost / powered off)")
				}
			}
		}
	}()
}

func (a *App) stopHealthMonitor() {
	if a.healthStop != nil {
		close(a.healthStop)
		a.healthStop = nil
	}
}

func (a *App) disconnect() {
	a.stopHealthMonitor()
	// Capture and clear references first so the UI updates immediately.
	p := a.printer
	conn := a.rfcommConn
	a.printer = nil
	a.rfcommConn = nil

	// Closing the serial port can block in the OS driver (notably on macOS
	// when the Bluetooth link is already gone). Do it off the UI thread so
	// the app does not freeze.
	if p != nil || conn != nil {
		go func() {
			if p != nil {
				p.Close()
			}
			if conn != nil {
				conn.Close()
			}
		}()
	}

	a.connectBtn.SetText("Connect")
	a.btDeviceSelect.Enable()
	a.refreshBTBtn.Enable()
	a.bleConnectBtn.SetText("Connect")
	a.bleConnectBtn.Enable()
	a.bleScanBtn.Enable()
	a.bleDeviceSelect.Enable()
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
			dialog.ShowError(err, a.window)
		} else {
			a.statusLabel.SetText("Print complete!")
		}
		a.printBtn.Enable()
	}()
}
