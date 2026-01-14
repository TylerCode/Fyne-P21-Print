//go:build linux

package printer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RFCOMMConnection manages an RFCOMM connection process (Linux-specific)
type RFCOMMConnection struct {
	DevicePath string
	MAC        string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// ListPairedBluetoothDevices returns all paired Bluetooth devices
func ListPairedBluetoothDevices() ([]BluetoothDevice, error) {
	out, err := exec.Command("bluetoothctl", "devices", "Paired").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list paired devices: %w", err)
	}

	var devices []BluetoothDevice
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Device ") {
			continue
		}
		// Format: "Device XX:XX:XX:XX:XX:XX DeviceName"
		parts := strings.SplitN(strings.TrimPrefix(line, "Device "), " ", 2)
		if len(parts) == 2 {
			devices = append(devices, BluetoothDevice{
				MAC:  parts[0],
				Name: parts[1],
			})
		}
	}

	return devices, nil
}

// FindAvailableRFCOMMDevice finds an unused /dev/rfcommN device number
func FindAvailableRFCOMMDevice() (string, int, error) {
	for i := 0; i < 10; i++ {
		devPath := fmt.Sprintf("/dev/rfcomm%d", i)
		// Check if device is currently bound
		out, _ := exec.Command("rfcomm", "show", devPath).Output()
		if len(out) == 0 || strings.Contains(string(out), "No such device") {
			return devPath, i, nil
		}
	}
	return "", -1, fmt.Errorf("no available RFCOMM device slots")
}

// CheckRFCOMMInstalled verifies rfcomm binary is available
func CheckRFCOMMInstalled() error {
	_, err := exec.LookPath("rfcomm")
	if err != nil {
		return fmt.Errorf("rfcomm not found - install with: sudo apt install bluez")
	}
	return nil
}

// CheckPrivilegeHelper checks which privilege escalation method is available
func CheckPrivilegeHelper() string {
	// Check for pkexec (PolicyKit - works with GUI)
	if _, err := exec.LookPath("pkexec"); err == nil {
		return "pkexec"
	}
	// Check for sudo
	if _, err := exec.LookPath("sudo"); err == nil {
		return "sudo"
	}
	return ""
}

// EstablishRFCOMM creates an RFCOMM connection to the given MAC address
// This runs rfcomm connect in the background and returns when the device is ready
func EstablishRFCOMM(mac string, channel int, statusCallback func(string)) (*RFCOMMConnection, error) {
	if err := CheckRFCOMMInstalled(); err != nil {
		return nil, err
	}

	devPath, devNum, err := FindAvailableRFCOMMDevice()
	if err != nil {
		return nil, err
	}

	helper := CheckPrivilegeHelper()
	if helper == "" {
		return nil, ErrPrivilegeRequired
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn := &RFCOMMConnection{
		DevicePath: devPath,
		MAC:        mac,
		cancel:     cancel,
	}

	// Build the command based on available privilege helper
	var cmd *exec.Cmd
	rfcommArgs := []string{"connect", fmt.Sprintf("/dev/rfcomm%d", devNum), mac, fmt.Sprintf("%d", channel)}

	if helper == "pkexec" {
		cmd = exec.CommandContext(ctx, "pkexec", append([]string{"rfcomm"}, rfcommArgs...)...)
	} else {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{"-n", "rfcomm"}, rfcommArgs...)...)
	}

	conn.cmd = cmd

	// Capture stderr for status messages
	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()

	if statusCallback != nil {
		statusCallback(fmt.Sprintf("Connecting to %s...", mac))
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start rfcomm: %w", err)
	}

	// Read output in background
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if statusCallback != nil {
				statusCallback(scanner.Text())
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if statusCallback != nil {
				statusCallback(scanner.Text())
			}
		}
	}()

	// Wait for device to appear
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ErrConnectionCanceled
		default:
		}

		if _, err := os.Stat(devPath); err == nil {
			// Device exists, give it a moment to be ready
			time.Sleep(500 * time.Millisecond)
			if statusCallback != nil {
				statusCallback(fmt.Sprintf("Connected: %s", devPath))
			}
			return conn, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timeout - kill the process
	conn.Close()
	return nil, fmt.Errorf("timeout waiting for %s to appear", devPath)
}

// Close terminates the RFCOMM connection
func (c *RFCOMMConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Also explicitly release the device
	if c.DevicePath != "" {
		// Try to release - may need privileges
		helper := CheckPrivilegeHelper()
		if helper == "pkexec" {
			exec.Command("pkexec", "rfcomm", "release", c.DevicePath).Run()
		} else if helper == "sudo" {
			exec.Command("sudo", "-n", "rfcomm", "release", c.DevicePath).Run()
		}
	}

	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}

	return nil
}

// IsDeviceReady checks if the RFCOMM device is still available
func (c *RFCOMMConnection) IsDeviceReady() bool {
	if c.DevicePath == "" {
		return false
	}
	_, err := os.Stat(c.DevicePath)
	return err == nil
}

// GetExistingRFCOMMConnections returns currently active RFCOMM connections
func GetExistingRFCOMMConnections() ([]string, error) {
	out, err := exec.Command("rfcomm", "-a").Output()
	if err != nil {
		// rfcomm -a might fail if no connections, check for devices directly
		var devices []string
		for i := 0; i < 10; i++ {
			devPath := fmt.Sprintf("/dev/rfcomm%d", i)
			if _, err := os.Stat(devPath); err == nil {
				devices = append(devices, devPath)
			}
		}
		return devices, nil
	}

	var devices []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "rfcomm") {
			// Extract device path
			parts := strings.Fields(line)
			if len(parts) > 0 {
				devName := strings.TrimSuffix(parts[0], ":")
				devices = append(devices, filepath.Join("/dev", devName))
			}
		}
	}

	return devices, nil
}

// ListSerialPorts returns available serial ports (for manual connection)
func ListSerialPorts() ([]string, error) {
	var ports []string

	// Check for rfcomm devices
	rfcomm, _ := GetExistingRFCOMMConnections()
	ports = append(ports, rfcomm...)

	// Check common serial ports
	commonPorts := []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "/dev/ttyACM0", "/dev/ttyACM1"}
	for _, p := range commonPorts {
		if _, err := os.Stat(p); err == nil {
			ports = append(ports, p)
		}
	}

	return ports, nil
}
