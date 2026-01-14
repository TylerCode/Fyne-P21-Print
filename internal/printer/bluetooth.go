package printer

import "errors"

// Common errors
var (
	ErrNoDevicesFound     = errors.New("no paired Bluetooth devices found")
	ErrRFCOMMFailed       = errors.New("failed to establish RFCOMM connection")
	ErrPrivilegeRequired  = errors.New("root privileges required for RFCOMM")
	ErrConnectionCanceled = errors.New("connection canceled")
	ErrNotSupported       = errors.New("operation not supported on this platform")
)

// BluetoothDevice represents a paired Bluetooth device
type BluetoothDevice struct {
	Name string
	MAC  string // MAC address on Linux, or COM port on Windows
}

// BluetoothConnection manages a Bluetooth serial connection
// Implementation is platform-specific
type BluetoothConnection struct {
	DevicePath string
	MAC        string
	platform   interface{} // Platform-specific data
}

// IsDeviceReady checks if the connection is still valid
func (c *BluetoothConnection) IsDeviceReady() bool {
	return c != nil && c.DevicePath != ""
}
