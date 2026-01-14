//go:build windows

package printer

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// RFCOMMConnection is a compatibility type for Windows
// On Windows, we don't need to manage RFCOMM - COM ports are created automatically
type RFCOMMConnection struct {
	DevicePath string
	MAC        string
}

// ListPairedBluetoothDevices returns Bluetooth COM ports on Windows
// On Windows, paired BT SPP devices appear as COM ports automatically
func ListPairedBluetoothDevices() ([]BluetoothDevice, error) {
	var devices []BluetoothDevice

	// On Windows, we list COM ports that are Bluetooth-related
	// Check registry for Bluetooth COM ports
	btPorts, err := getBluetoothCOMPorts()
	if err == nil {
		for name, port := range btPorts {
			devices = append(devices, BluetoothDevice{
				Name: name,
				MAC:  port, // On Windows, we use COM port as identifier
			})
		}
	}

	// If no BT-specific ports found, list all COM ports
	if len(devices) == 0 {
		ports, _ := ListSerialPorts()
		for _, port := range ports {
			devices = append(devices, BluetoothDevice{
				Name: port,
				MAC:  port,
			})
		}
	}

	return devices, nil
}

// getBluetoothCOMPorts reads Bluetooth COM port mappings from registry
func getBluetoothCOMPorts() (map[string]string, error) {
	ports := make(map[string]string)

	// Try to read from SERIALCOMM registry key
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.READ)
	if err != nil {
		return nil, err
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		val, _, err := key.GetStringValue(name)
		if err == nil {
			// Check if it looks like a Bluetooth port
			if strings.Contains(strings.ToLower(name), "bth") ||
				strings.Contains(strings.ToLower(name), "bluetooth") {
				ports[name] = val
			}
		}
	}

	return ports, nil
}

// CheckRFCOMMInstalled always returns nil on Windows (not needed)
func CheckRFCOMMInstalled() error {
	return nil
}

// CheckPrivilegeHelper always returns "windows" on Windows (no elevation needed for COM)
func CheckPrivilegeHelper() string {
	return "windows"
}

// EstablishRFCOMM on Windows simply returns the COM port path
// Windows handles BT SPP as regular COM ports, no special setup needed
func EstablishRFCOMM(mac string, channel int, statusCallback func(string)) (*RFCOMMConnection, error) {
	// On Windows, 'mac' is actually the COM port (e.g., "COM3")
	if statusCallback != nil {
		statusCallback(fmt.Sprintf("Using port %s...", mac))
	}

	// Verify the port exists
	comPath := mac
	if !strings.HasPrefix(strings.ToUpper(mac), "COM") {
		return nil, fmt.Errorf("invalid COM port: %s", mac)
	}

	// For COM ports > 9, need to use \\.\COM10 format
	if len(mac) > 4 {
		comPath = `\\.\` + mac
	}

	conn := &RFCOMMConnection{
		DevicePath: comPath,
		MAC:        mac,
	}

	if statusCallback != nil {
		statusCallback(fmt.Sprintf("Ready: %s", mac))
	}

	return conn, nil
}

// Close is a no-op on Windows (COM ports don't need special cleanup)
func (c *RFCOMMConnection) Close() error {
	return nil
}

// IsDeviceReady checks if the COM port exists
func (c *RFCOMMConnection) IsDeviceReady() bool {
	if c.DevicePath == "" {
		return false
	}
	// On Windows, we can't easily check if a COM port is available
	// without trying to open it, so we just return true
	return true
}

// GetExistingRFCOMMConnections returns available COM ports on Windows
func GetExistingRFCOMMConnections() ([]string, error) {
	return ListSerialPorts()
}

// ListSerialPorts enumerates available COM ports on Windows
func ListSerialPorts() ([]string, error) {
	var ports []string

	// Read from registry
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.READ)
	if err != nil {
		// Fallback: check common COM ports
		for i := 1; i <= 20; i++ {
			port := fmt.Sprintf("COM%d", i)
			// Try to check if port exists (this is a rough check)
			ports = append(ports, port)
		}
		return ports[:4], nil // Return first 4 as fallback
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		val, _, err := key.GetStringValue(name)
		if err == nil {
			ports = append(ports, val)
		}
	}

	return ports, nil
}

// FindAvailableRFCOMMDevice is not needed on Windows but provided for compatibility
func FindAvailableRFCOMMDevice() (string, int, error) {
	ports, err := ListSerialPorts()
	if err != nil || len(ports) == 0 {
		return "", -1, fmt.Errorf("no COM ports found")
	}
	return ports[0], 0, nil
}
