package printer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

var (
	ErrNotConnected = errors.New("printer not connected")
	ErrTimeout      = errors.New("operation timed out")
)

// Port is the byte stream to the printer: a serial port (RFCOMM, COM, USB)
// or a BLE GATT link. Read must return after a timeout when no data arrives.
type Port interface {
	io.ReadWriteCloser
	ResetInputBuffer() error
}

// Printer represents a connection to the Nelko P21
type Printer struct {
	port     Port
	portName string
	mac      string
	mu       sync.Mutex // serializes all port I/O
}

// FindRFCOMMDevices lists available /dev/rfcomm* devices
func FindRFCOMMDevices() ([]string, error) {
	// Check for existing rfcomm devices
	out, err := exec.Command("ls", "/dev/rfcomm*").Output()
	if err != nil {
		return nil, nil // No devices found, not an error
	}

	devices := strings.Fields(string(out))
	return devices, nil
}

// ListPairedDevices returns paired Bluetooth devices (name, MAC)
func ListPairedDevices() (map[string]string, error) {
	// Use bluetoothctl to list paired devices
	out, err := exec.Command("bluetoothctl", "devices", "Paired").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}

	devices := make(map[string]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// Format: "Device XX:XX:XX:XX:XX:XX DeviceName"
		parts := strings.SplitN(strings.TrimPrefix(line, "Device "), " ", 2)
		if len(parts) == 2 {
			mac := parts[0]
			name := parts[1]
			devices[name] = mac
		}
	}

	return devices, nil
}

// ConnectRFCOMM establishes an RFCOMM connection to a Bluetooth MAC address
// Returns the device path (e.g., /dev/rfcomm0)
func ConnectRFCOMM(mac string, channel int) (string, error) {
	// Find an available rfcomm device number
	devNum := 0
	for i := 0; i < 10; i++ {
		devPath := fmt.Sprintf("/dev/rfcomm%d", i)
		// Try to use this device number
		cmd := exec.Command("rfcomm", "connect", devPath, mac, fmt.Sprintf("%d", channel))
		if err := cmd.Start(); err != nil {
			continue
		}
		
		// Give it a moment to connect
		time.Sleep(2 * time.Second)
		
		// Check if device exists now
		if _, err := exec.Command("test", "-e", devPath).Output(); err == nil {
			return devPath, nil
		}
		
		cmd.Process.Kill()
		devNum++
	}

	return "", errors.New("failed to establish RFCOMM connection")
}

// Connect opens a connection to the printer on the given serial port
func Connect(portName string) (*Printer, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open port %s: %w", portName, err)
	}

	port.SetReadTimeout(3 * time.Second)

	p := &Printer{
		port:     port,
		portName: portName,
	}

	return p, nil
}

// Close closes the printer connection
func (p *Printer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port != nil {
		err := p.port.Close()
		p.port = nil
		return err
	}
	return nil
}

// sendCommandLocked sends a command and optionally reads a response.
// Caller must hold mu.
func (p *Printer) sendCommandLocked(cmd string) (string, error) {
	if p.port == nil {
		return "", ErrNotConnected
	}

	// Send command with CRLF
	_, err := p.port.Write([]byte(cmd + "\r\n"))
	if err != nil {
		return "", fmt.Errorf("write failed: %w", err)
	}

	// Read response
	reader := bufio.NewReader(p.port)
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", nil // Some commands don't respond
	}

	return strings.TrimSpace(response), nil
}

// GetBattery queries the battery level
func (p *Printer) GetBattery() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp, err := p.sendCommandLocked("BATTERY?")
	if err != nil {
		return 0, err
	}

	// Response format: "BATTERY" followed by bytes
	if len(resp) > 7 {
		// First byte after "BATTERY" is percentage
		return int(resp[7]), nil
	}

	return 0, errors.New("invalid battery response")
}

// GetConfig queries printer configuration
func (p *Printer) GetConfig() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sendCommandLocked("CONFIG?")
}

// CancelPause sends escape sequence to cancel pause status
func (p *Printer) CancelPause() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port == nil {
		return ErrNotConnected
	}
	return p.cancelPauseLocked()
}

// cancelPauseLocked sends the cancel-pause escape sequence. Caller must hold mu.
func (p *Printer) cancelPauseLocked() error {
	_, err := p.port.Write([]byte("\x1b!o"))
	return err
}

// CheckReady checks if printer is ready
func (p *Printer) CheckReady() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port == nil {
		return false, ErrNotConnected
	}
	_, err := p.port.Write([]byte("\x1b!?"))
	if err != nil {
		return false, err
	}
	// Read response
	buf := make([]byte, 32)
	_, err = p.port.Read(buf)
	return err == nil, err
}

// Ping checks the printer is alive by sending a status query and waiting for
// a response. A bare Write always "succeeds" over an open serial port even if
// the device is off, so reading back is the only reliable liveness signal.
func (p *Printer) Ping() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port == nil {
		return ErrNotConnected
	}
	_ = p.port.ResetInputBuffer()
	if _, err := p.port.Write([]byte("BATTERY?\r\n")); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	buf := make([]byte, 32)
	n, err := p.port.Read(buf) // read timeout was set in Connect
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}
	if n == 0 {
		return errors.New("no response from device (powered off or out of range)")
	}
	return nil
}

// Print sends raw print data to the printer
func (p *Printer) Print(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port == nil {
		return ErrNotConnected
	}

	// Cancel any pause state first
	p.cancelPauseLocked()
	time.Sleep(100 * time.Millisecond)

	// Send print data
	_, err := p.port.Write(data)
	if err != nil {
		return fmt.Errorf("print failed: %w", err)
	}

	return nil
}

// PortName returns the current port name
func (p *Printer) PortName() string {
	return p.portName
}

// Alive reports whether the serial device still exists. On macOS/Linux a
// Bluetooth serial node disappears when the device truly disconnects, so this
// is a reliable liveness signal that (unlike a status read) does not require
// the printer to answer queries it may not support.
func (p *Printer) Alive() bool {
	if a, ok := p.port.(interface{ Alive() bool }); ok {
		return a.Alive()
	}
	if p.portName == "" {
		return false
	}
	// COM ports on Windows are not filesystem paths; treat as always present.
	if strings.HasPrefix(strings.ToUpper(p.portName), "COM") || strings.HasPrefix(p.portName, `\\.\`) {
		return true
	}
	_, err := os.Stat(p.portName)
	return err == nil
}
