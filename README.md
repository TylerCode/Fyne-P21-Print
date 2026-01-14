# Fyne-P21-Print
Printing/Label maker app for linux. Prints to the Nelko P21, tested on Zorin, offered as-is <3

Written in GO using FYNE

<img width="615" height="577" alt="image" src="https://github.com/user-attachments/assets/f2d1a4f7-9385-47fd-a460-ce4689f2fdec" />

Based on the reverse engineering work from [merlinschumacher/nelko-p21-print](https://github.com/merlinschumacher/nelko-p21-print).

Here are some vibe docs, I really don't expect anyone to use it but if you stumble upon this and think it's useful, create a new issue and I'll help you get it working and get better docs. Just call the issue "Software missing a good onboarding process" or some shit and we'll work it out. I would be down to package for Flatpak, Snap, and AppImage (plus deb/rpm) if there was genuine interest.

## Quick Start

```bash
# One-time setup
./setup.sh

# Run the app
./nelko-print
```

That's it! The app handles Bluetooth connection automatically.

## Prerequisites

- Go 1.22+
- BlueZ (for Bluetooth)
- Fyne dependencies (OpenGL, etc.)

### Install dependencies (Ubuntu/Zorin)

```bash
# Fyne deps
sudo apt install -y libgl1-mesa-dev xorg-dev

# Bluetooth
sudo apt install -y bluez

# Add yourself to dialout group (one-time, then logout/login)
sudo usermod -aG dialout $USER
```

## Building

```bash
go mod tidy
go build -o nelko-print ./cmd/nelko-print
```

Or just:
```bash
make build
```

## Usage

### The Easy Way (New!)

1. **Pair the printer** via your system's Bluetooth settings (one-time setup)
2. **Run the app**: `./nelko-print`
3. **Select your printer** from the dropdown (auto-detects Nelko devices)
4. **Click Connect** - the app will prompt for your password via `pkexec` to establish the Bluetooth connection
5. **Load an image or type text**, then print!

The app automatically handles the RFCOMM connection that previously required manual terminal commands.

### Manual/Advanced Method

If you prefer manual control or the auto-connect doesn't work:

```bash
# Pair the printer first (one-time)
bluetoothctl
> scan on
> pair XX:XX:XX:XX:XX:XX
> trust XX:XX:XX:XX:XX:XX
> quit

# Create RFCOMM device manually
sudo rfcomm connect /dev/rfcomm0 XX:XX:XX:XX:XX:XX 1

# Run the app and use "Advanced" section to connect to /dev/rfcomm0
./nelko-print
```

## Supported Label Sizes

- 12x40mm
- 14x40mm (default)
- 14x50mm
- 14x75mm
- 15x30mm

## How It Works

The Nelko P21 uses Bluetooth Serial Port Profile (SPP/RFCOMM) for communication. The Linux `rfcomm` tool creates a virtual serial device (`/dev/rfcommN`) that the app can write print commands to.

The app now handles this automatically:
1. Lists paired Bluetooth devices using `bluetoothctl`
2. Uses `pkexec` (or `sudo`) to run `rfcomm connect` with elevated privileges
3. Waits for the device to appear, then opens it as a serial port
4. Sends TSPL2 print commands to the printer

## Troubleshooting

### "Permission denied" on /dev/rfcomm0

Add yourself to the `dialout` group:

```bash
sudo usermod -aG dialout $USER
```

Then log out and back in.

### rfcomm command not found

```bash
sudo apt install bluez
```

### pkexec not working / No authentication agent

Install PolicyKit agent for your desktop:
```bash
# GNOME/Zorin
sudo apt install policykit-1-gnome

# KDE
sudo apt install polkit-kde-agent-1
```

### Printer not showing in dropdown

Make sure the printer is paired in your system's Bluetooth settings first. The app only shows devices that are already paired.

### Connection timeout

1. Make sure the printer is powered on and in range
2. Try re-pairing the printer
3. Use the manual method with `rfcomm connect` to see detailed error messages

## License

MIT

