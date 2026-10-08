package printer

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// Some P21 units are Bluetooth Low Energy only (no classic Bluetooth, so no
// RFCOMM/serial port). They accept the same TSPL commands over a GATT
// service: commands are written to one characteristic and replies arrive as
// notifications on another.
var (
	bleServiceUUID = bluetooth.New16BitUUID(0xff00)
	bleWriteUUID   = bluetooth.New16BitUUID(0xff02) // write without response
	bleNotifyUUID  = bluetooth.New16BitUUID(0xff01) // notify
)

const (
	// Largest write sent in one go. The ATT MTU allows more, but the printer
	// has a small receive buffer; 200 bytes with a short pause between writes
	// prints reliably.
	bleMaxChunk    = 200
	bleChunkPause  = 15 * time.Millisecond
	bleReadTimeout = 3 * time.Second
)

var (
	bleAdapter   = bluetooth.DefaultAdapter
	bleEnableMu  sync.Mutex
	bleEnabled   bool
	bleScanMu    sync.Mutex
	errBLEClosed = errors.New("BLE connection closed")
)

func enableBLE() error {
	bleEnableMu.Lock()
	defer bleEnableMu.Unlock()
	if bleEnabled {
		return nil
	}
	if err := bleAdapter.Enable(); err != nil {
		return fmt.Errorf("Bluetooth adapter not available: %w", err)
	}
	bleEnabled = true
	return nil
}

// ScanBLEDevices scans for BLE printers for the given duration. Devices that
// advertise the printer's GATT service or have "P21"/"Nelko" in their name
// are returned. MAC holds the platform address (a UUID on macOS) to pass to
// ConnectBLE.
func ScanBLEDevices(duration time.Duration) ([]BluetoothDevice, error) {
	if err := enableBLE(); err != nil {
		return nil, err
	}
	bleScanMu.Lock()
	defer bleScanMu.Unlock()

	var mu sync.Mutex
	found := make(map[string]BluetoothDevice)
	done := make(chan error, 1)
	go func() {
		done <- bleAdapter.Scan(func(_ *bluetooth.Adapter, r bluetooth.ScanResult) {
			name := r.LocalName()
			lower := strings.ToLower(name)
			if !r.HasServiceUUID(bleServiceUUID) &&
				!strings.Contains(lower, "p21") && !strings.Contains(lower, "nelko") {
				return
			}
			if name == "" {
				name = "Unknown BLE printer"
			}
			mu.Lock()
			found[r.Address.String()] = BluetoothDevice{Name: name, MAC: r.Address.String()}
			mu.Unlock()
		})
	}()

	select {
	case err := <-done: // scan failed to start
		return nil, fmt.Errorf("BLE scan failed: %w", err)
	case <-time.After(duration):
	}
	bleAdapter.StopScan()
	<-done

	mu.Lock()
	defer mu.Unlock()
	devices := make([]BluetoothDevice, 0, len(found))
	for _, d := range found {
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	return devices, nil
}

// ConnectBLE connects to a BLE printer by the address reported by
// ScanBLEDevices and returns a Printer that talks to it over GATT.
func ConnectBLE(address string, timeout time.Duration) (*Printer, error) {
	if err := enableBLE(); err != nil {
		return nil, err
	}
	var addr bluetooth.Address
	addr.Set(address)

	type result struct {
		dev bluetooth.Device
		err error
	}
	ch := make(chan result, 1)
	go func() {
		dev, err := bleAdapter.Connect(addr, bluetooth.ConnectionParams{})
		ch <- result{dev, err}
	}()

	var dev bluetooth.Device
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("BLE connect to %s failed: %w", address, r.err)
		}
		dev = r.dev
	case <-time.After(timeout):
		// Drop the connection if it completes after we gave up.
		go func() {
			if r := <-ch; r.err == nil {
				r.dev.Disconnect()
			}
		}()
		return nil, fmt.Errorf("BLE connect to %s: %w", address, ErrTimeout)
	}

	port, err := newBLEPort(dev)
	if err != nil {
		dev.Disconnect()
		return nil, err
	}
	return &Printer{port: port, portName: "BLE " + address, mac: address}, nil
}

// blePort adapts a GATT write/notify characteristic pair to the Port
// interface used by Printer.
type blePort struct {
	dev    bluetooth.Device
	write  bluetooth.DeviceCharacteristic
	notify bluetooth.DeviceCharacteristic
	chunk  int

	mu     sync.Mutex
	buf    []byte
	data   chan struct{} // signalled when buf grows
	closed bool
	dead   bool // a write failed: the link is gone
}

func newBLEPort(dev bluetooth.Device) (*blePort, error) {
	services, err := dev.DiscoverServices([]bluetooth.UUID{bleServiceUUID})
	if err != nil || len(services) == 0 {
		return nil, fmt.Errorf("printer service %s not found: %v", bleServiceUUID, err)
	}
	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{bleWriteUUID, bleNotifyUUID})
	if err != nil {
		return nil, fmt.Errorf("printer characteristics not found: %w", err)
	}

	p := &blePort{dev: dev, data: make(chan struct{}, 1), chunk: 20}
	var haveWrite, haveNotify bool
	for _, c := range chars {
		switch c.UUID() {
		case bleWriteUUID:
			p.write, haveWrite = c, true
		case bleNotifyUUID:
			p.notify, haveNotify = c, true
		}
	}
	if !haveWrite || !haveNotify {
		return nil, errors.New("printer write/notify characteristics not found")
	}

	if mtu, err := p.write.GetMTU(); err == nil && int(mtu) > 3 {
		p.chunk = min(int(mtu)-3, bleMaxChunk)
	}

	if err := p.notify.EnableNotifications(p.onNotify); err != nil {
		return nil, fmt.Errorf("enable notifications: %w", err)
	}
	return p, nil
}

func (p *blePort) onNotify(b []byte) {
	p.mu.Lock()
	p.buf = append(p.buf, b...)
	p.mu.Unlock()
	select {
	case p.data <- struct{}{}:
	default:
	}
}

// Write sends data in MTU-sized chunks, pausing between them so the printer
// can keep up.
func (p *blePort) Write(b []byte) (int, error) {
	sent := 0
	for sent < len(b) {
		if p.isClosed() {
			return sent, errBLEClosed
		}
		end := min(sent+p.chunk, len(b))
		if _, err := p.write.WriteWithoutResponse(b[sent:end]); err != nil {
			p.mu.Lock()
			p.dead = true
			p.mu.Unlock()
			return sent, err
		}
		sent = end
		if sent < len(b) {
			time.Sleep(bleChunkPause)
		}
	}
	return sent, nil
}

// Read returns buffered notification bytes, waiting up to the read timeout
// for some to arrive.
func (p *blePort) Read(b []byte) (int, error) {
	deadline := time.After(bleReadTimeout)
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return 0, errBLEClosed
		}
		if len(p.buf) > 0 {
			n := copy(b, p.buf)
			p.buf = p.buf[n:]
			p.mu.Unlock()
			return n, nil
		}
		p.mu.Unlock()

		select {
		case <-p.data:
		case <-deadline:
			return 0, ErrTimeout
		}
	}
}

func (p *blePort) ResetInputBuffer() error {
	p.mu.Lock()
	p.buf = nil
	p.mu.Unlock()
	return nil
}

func (p *blePort) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	p.notify.EnableNotifications(nil)
	return p.dev.Disconnect()
}

// Alive reports whether the link is still usable. Not every platform reports
// disconnects, so a failed write is also treated as a lost link.
func (p *blePort) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.closed && !p.dead
}

func (p *blePort) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
