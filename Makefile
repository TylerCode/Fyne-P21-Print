.PHONY: build run clean deps setup install-deps build-windows build-linux build-all

# Default build for current platform
build:
	go build -o nelko-print ./cmd/nelko-print

run: build
	./nelko-print

clean:
	rm -f nelko-print nelko-print.exe

deps:
	go mod tidy

# Full setup (install deps + build)
setup:
	./setup.sh

# === Cross-compilation targets ===

# Build for Windows (from Linux)
build-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		go build -o nelko-print.exe ./cmd/nelko-print

# Build for Linux explicitly
build-linux:
	GOOS=linux GOARCH=amd64 go build -o nelko-print ./cmd/nelko-print

# Build for all platforms
build-all: build-linux build-windows

# === System dependency installation ===

# Install system deps (Ubuntu/Zorin)
install-deps:
	sudo apt install -y libgl1-mesa-dev xorg-dev bluez

# Install cross-compilation tools for Windows builds
install-cross-deps:
	sudo apt install -y mingw-w64

# === Bluetooth utilities ===

# List paired Bluetooth devices
list-bt:
	bluetoothctl devices Paired

# Legacy manual connection commands (no longer needed for normal usage)
# Connect to printer (usage: make connect MAC=XX:XX:XX:XX:XX:XX)
connect:
	sudo rfcomm connect /dev/rfcomm0 $(MAC) 1

# Release rfcomm device
disconnect:
	sudo rfcomm release /dev/rfcomm0
