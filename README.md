# Fyne-P21-Print
Printing/Label maker app for linux. Prints to the Nelko P21, tested on Zorin, offered as-is &lt;3

Written in GO using FYNE

<img width="615" height="577" alt="image" src="https://github.com/user-attachments/assets/f2d1a4f7-9385-47fd-a460-ce4689f2fdec" />

Based on the reverse engineering work from [merlinschumacher/nelko-p21-print](https://github.com/merlinschumacher/nelko-p21-print).

Here are some vibe docs, I really don't expect anyone to use it but if you stumble upon this and think it's useful, create a new issue and I'll help you get it working and get better docs. Just call the issue "Software missing a good onboarding process" or some shit and we'll work it out. I would be down to package for Flatpak, Snap, and AppImage (plus deb/rpm) if there was genuine interest. 

## Prerequisites

- Go 1.22+
- BlueZ (for Bluetooth)
- Fyne dependencies (OpenGL, etc.)

### Install dependencies (Ubuntu/Zorin)

```bash
# Fyne deps
sudo apt install -y libgl1-mesa-dev xorg-dev

# Bluetooth
sudo apt install -y bluez rfcomm
```

## Building

```bash
go mod tidy
go build -o nelko-print ./cmd/nelko-print
```

## Usage

### 1. Pair the printer

Power on the P21 and pair it using your system's Bluetooth settings or:

```bash
bluetoothctl
> scan on
> pair XX:XX:XX:XX:XX:XX
> trust XX:XX:XX:XX:XX:XX
> quit
```

### 2. Create RFCOMM device

```bash
sudo rfcomm connect /dev/rfcomm0 XX:XX:XX:XX:XX:XX 1
```

Replace `XX:XX:XX:XX:XX:XX` with your printer's MAC address.

> **Note:** You may need to keep this terminal open or run it in the background.

### 3. Run the app

```bash
./nelko-print
```

Select `/dev/rfcomm0` (or whatever device), connect, load an image, and print!

## Supported Label Sizes

- 12x40mm
- 14x40mm (default)
- 14x50mm
- 14x75mm
- 15x30mm

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

### Printer not responding

Make sure the RFCOMM connection is established first. The printer doesn't respond over USB properly.

## License

MIT


