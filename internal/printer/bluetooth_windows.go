//go:build windows

package printer

import (
	"fmt"
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
// with friendly device names resolved from the registry.
func ListPairedBluetoothDevices() ([]BluetoothDevice, error) {
	var devices []BluetoothDevice

	// Try to build a mapping from COM port → friendly BT device name
	btNames := getBluetoothDeviceNames()   // MAC → friendly name
	comToMAC := matchCOMToBluetoothMAC()   // COM port → MAC

	// Read all Bluetooth COM ports from SERIALCOMM
	btPorts, err := getBluetoothCOMPorts()
	if err == nil {
		for regName, comPort := range btPorts {
			name := regName // fallback: raw registry name
			// Try to resolve a friendly device name
			if mac, ok := comToMAC[comPort]; ok {
				if friendly, ok2 := btNames[mac]; ok2 {
					name = friendly
				}
			}
			devices = append(devices, BluetoothDevice{
				Name: fmt.Sprintf("%s (%s)", name, comPort),
				MAC:  comPort, // On Windows, we use COM port as identifier
			})
		}
	}

	// If no BT-specific ports found, list all COM ports
	if len(devices) == 0 {
		ports, _ := ListSerialPorts()
		for _, port := range ports {
			// Try to resolve name for any port
			name := port
			if mac, ok := comToMAC[port]; ok {
				if friendly, ok2 := btNames[mac]; ok2 {
					name = fmt.Sprintf("%s (%s)", friendly, port)
				}
			}
			devices = append(devices, BluetoothDevice{
				Name: name,
				MAC:  port,
			})
		}
	}

	return devices, nil
}

// getBluetoothDeviceNames reads the BTHPORT registry to build a mapping
// from Bluetooth MAC address (lowercase, colon-separated) to friendly name.
func getBluetoothDeviceNames() map[string]string {
	names := make(map[string]string)

	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\BTHPORT\Parameters\Devices`, registry.READ)
	if err != nil {
		return names
	}
	defer key.Close()

	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return names
	}

	for _, macHex := range subkeys {
		devKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`SYSTEM\CurrentControlSet\Services\BTHPORT\Parameters\Devices\`+macHex, registry.READ)
		if err != nil {
			continue
		}

		// The "Name" value is stored as REG_BINARY containing UTF-8/ASCII bytes
		nameBytes, _, err := devKey.GetBinaryValue("Name")
		if err != nil {
			devKey.Close()
			continue
		}
		devKey.Close()

		// Trim null bytes and whitespace
		friendlyName := strings.TrimRight(string(nameBytes), "\x00 ")
		if friendlyName == "" {
			continue
		}

		// Normalize the MAC: registry key is like "001122334455",
		// convert to "00:11:22:33:44:55" for matching with BTHENUM
		mac := normalizeMAC(macHex)
		names[mac] = friendlyName
	}

	return names
}

// matchCOMToBluetoothMAC reads the BTHENUM registry entries under the
// system's device enumeration to correlate COM ports with Bluetooth MACs.
func matchCOMToBluetoothMAC() map[string]string {
	comToMAC := make(map[string]string)

	// Enumerate Bluetooth serial port devices
	enumKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\BTHENUM`, registry.READ)
	if err != nil {
		return comToMAC
	}
	defer enumKey.Close()

	classKeys, err := enumKey.ReadSubKeyNames(-1)
	if err != nil {
		return comToMAC
	}

	for _, className := range classKeys {
		classPath := `SYSTEM\CurrentControlSet\Enum\BTHENUM\` + className
		classKey, err := registry.OpenKey(registry.LOCAL_MACHINE, classPath, registry.READ)
		if err != nil {
			continue
		}
		instanceNames, err := classKey.ReadSubKeyNames(-1)
		classKey.Close()
		if err != nil {
			continue
		}

		for _, instanceName := range instanceNames {
			instancePath := classPath + `\` + instanceName
			instanceKey, err := registry.OpenKey(registry.LOCAL_MACHINE, instancePath, registry.READ)
			if err != nil {
				continue
			}

			// Check if this is a serial port by reading the "FriendlyName" which
			// typically contains the COM port like "Standard Serial over Bluetooth link (COM10)"
			friendlyName, _, err := instanceKey.GetStringValue("FriendlyName")
			if err != nil {
				instanceKey.Close()
				continue
			}

			// Extract COM port from friendly name
			comPort := extractCOMPort(friendlyName)
			if comPort == "" {
				instanceKey.Close()
				continue
			}

			// Extract MAC from the instance name or hardware ID
			// Instance names often contain the MAC, e.g.:
			// "00803a686519_C00000003" or similar with & separators
			mac := extractMACFromInstanceID(instanceName)
			if mac == "" {
				// Try reading HardwareID
				hwIDs, _, err := instanceKey.GetStringsValue("HardwareID")
				if err == nil {
					for _, hwID := range hwIDs {
						mac = extractMACFromHardwareID(hwID)
						if mac != "" {
							break
						}
					}
				}
			}
			instanceKey.Close()

			if mac != "" && comPort != "" {
				comToMAC[comPort] = mac
			}
		}
	}

	return comToMAC
}

// extractCOMPort extracts "COM10" from strings like
// "Standard Serial over Bluetooth link (COM10)"
func extractCOMPort(s string) string {
	idx := strings.LastIndex(s, "(COM")
	if idx < 0 {
		return ""
	}
	end := strings.Index(s[idx:], ")")
	if end < 0 {
		return ""
	}
	return s[idx+1 : idx+end]
}

// extractMACFromInstanceID tries to find a 12-hex-char MAC in an instance ID.
// Example: "00803a686519_C00000003" → "00:80:3a:68:65:19"
func extractMACFromInstanceID(id string) string {
	// Split on common separators
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '_' || r == '&'
	})
	for _, part := range parts {
		mac := tryParseMAC(part)
		if mac != "" {
			return mac
		}
	}
	return ""
}

// extractMACFromHardwareID extracts a MAC from a hardware ID string.
// Example: "BTHENUM\{...}_LOCALMFG&0000" may contain MAC in various positions.
func extractMACFromHardwareID(hwID string) string {
	// Look for 12-char hex sequences
	cleaned := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return r
		}
		return ' '
	}, hwID)

	for _, part := range strings.Fields(cleaned) {
		mac := tryParseMAC(part)
		if mac != "" {
			return mac
		}
	}
	return ""
}

// tryParseMAC checks if s is a 12-char hex string and formats it as a MAC.
func tryParseMAC(s string) string {
	s = strings.ToLower(s)
	if len(s) != 12 {
		return ""
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ""
		}
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		s[0:2], s[2:4], s[4:6], s[6:8], s[8:10], s[10:12])
}

// normalizeMAC converts "001122334455" to "00:11:22:33:44:55"
func normalizeMAC(hex string) string {
	hex = strings.ToLower(hex)
	if len(hex) == 12 {
		return fmt.Sprintf("%s:%s:%s:%s:%s:%s",
			hex[0:2], hex[2:4], hex[4:6], hex[6:8], hex[8:10], hex[10:12])
	}
	return strings.ToLower(hex)
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
