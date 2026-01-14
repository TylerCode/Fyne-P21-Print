#!/bin/bash
# Setup script for Fyne-P21-Print
# Run this once to install dependencies and build the app

set -e

echo "=== Fyne-P21-Print Setup ==="
echo

# Detect package manager
if command -v apt &> /dev/null; then
    PKG_MGR="apt"
elif command -v dnf &> /dev/null; then
    PKG_MGR="dnf"
elif command -v pacman &> /dev/null; then
    PKG_MGR="pacman"
else
    echo "Warning: Could not detect package manager. You may need to install dependencies manually."
    PKG_MGR=""
fi

# Install dependencies
echo "Installing system dependencies..."
if [ "$PKG_MGR" = "apt" ]; then
    sudo apt update
    sudo apt install -y libgl1-mesa-dev xorg-dev bluez golang
elif [ "$PKG_MGR" = "dnf" ]; then
    sudo dnf install -y mesa-libGL-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel bluez golang
elif [ "$PKG_MGR" = "pacman" ]; then
    sudo pacman -S --needed mesa xorg-server bluez go
fi

# Check Go version
echo
echo "Checking Go installation..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    echo "Go version: $GO_VERSION"
else
    echo "Error: Go is not installed. Please install Go 1.22 or later."
    exit 1
fi

# Add user to dialout group if not already
if ! groups | grep -q dialout; then
    echo
    echo "Adding $USER to dialout group (for serial port access)..."
    sudo usermod -aG dialout $USER
    echo "Note: You'll need to log out and back in for this to take effect."
fi

# Build the application
echo
echo "Building nelko-print..."
go mod tidy
go build -o nelko-print ./cmd/nelko-print

echo
echo "=== Setup Complete! ==="
echo
echo "To use the app:"
echo "1. Pair your Nelko P21 printer via system Bluetooth settings"
echo "2. Run: ./nelko-print"
echo "3. Select your printer from the dropdown and click Connect"
echo
echo "If you just got added to the dialout group, log out and back in first."
