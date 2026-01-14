.PHONY: build run clean deps setup install-deps

build:
	go build -o nelko-print ./cmd/nelko-print

run: build
	./nelko-print

clean:
	rm -f nelko-print

deps:
	go mod tidy

# Full setup (install deps + build)
setup:
	./setup.sh

# Install system deps (Ubuntu/Zorin)
install-deps:
	sudo apt install -y libgl1-mesa-dev xorg-dev bluez

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
