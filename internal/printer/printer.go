package printer

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.bug.st/serial"
)

var (
	ErrNotConnected = errors.New("printer not connected")
	ErrTimeout      = errors.New("operation timed out")
)

// Printer represents a connection to the Nelko P21
type Printer struct {
	port     serial.Port
	portName string
	mac      string
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
	if p.port != nil {
		return p.port.Close()
	}
	return nil
}

// sendCommand sends a command and optionally reads response
func (p *Printer) sendCommand(cmd string) (string, error) {
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
	resp, err := p.sendCommand("BATTERY?")
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
	return p.sendCommand("CONFIG?")
}

// CancelPause sends escape sequence to cancel pause status
func (p *Printer) CancelPause() error {
	_, err := p.port.Write([]byte("\x1b!o"))
	return err
}

// CheckReady checks if printer is ready
func (p *Printer) CheckReady() (bool, error) {
	_, err := p.port.Write([]byte("\x1b!?"))
	if err != nil {
		return false, err
	}
	// Read response
	buf := make([]byte, 32)
	_, err = p.port.Read(buf)
	return err == nil, err
}

// Print sends raw print data to the printer
func (p *Printer) Print(data []byte) error {
	if p.port == nil {
		return ErrNotConnected
	}

	// Cancel any pause state first
	p.CancelPause()
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
