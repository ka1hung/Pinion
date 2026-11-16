// Package serialx lists serial ports with USB details and explains open
// failures per platform.
package serialx

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// PortInfo describes one serial port.
type PortInfo struct {
	Name         string `json:"name"`
	Display      string `json:"display"`
	Product      string `json:"product"`
	Manufacturer string `json:"manufacturer"`
	VID          string `json:"vid"`
	PID          string `json:"pid"`
	SerialNumber string `json:"serialNumber"`
	Chip         string `json:"chip"`
	IsUSB        bool   `json:"isUsb"`
}

var chips = map[string]string{
	"1A86:7523": "CH340",
	"1A86:5523": "CH341",
	"1A86:7522": "CH340K",
	"1A86:55D4": "CH9102",
	"1A86:55D3": "CH343",
	"10C4:EA60": "CP210x",
	"10C4:EA70": "CP2105",
	"10C4:EA71": "CP2108",
	"0403:6001": "FTDI FT232R",
	"0403:6010": "FTDI FT2232",
	"0403:6011": "FTDI FT4232",
	"0403:6014": "FTDI FT232H",
	"0403:6015": "FTDI FT230X",
	"067B:2303": "PL2303",
	"067B:23A3": "PL2303GC",
	"067B:23C3": "PL2303GT",
	"2341:0043": "Arduino Uno",
	"2341:0001": "Arduino Uno",
	"2341:0042": "Arduino Mega 2560",
	"2341:8036": "Arduino Leonardo",
	"2A03:0043": "Arduino Uno",
	"303A:1001": "ESP32 USB-JTAG/Serial",
	"2E8A:000A": "Raspberry Pi Pico",
	"0483:5740": "STM32 Virtual COM",
}

// ChipName returns a known chip name for a VID/PID pair.
func ChipName(vid, pid string) string {
	return chips[strings.ToUpper(vid)+":"+strings.ToUpper(pid)]
}

// ListPorts returns available ports, naturally sorted.
func ListPorts() ([]PortInfo, error) {
	var out []PortInfo
	details, err := enumerator.GetDetailedPortsList()
	if err == nil {
		for _, d := range details {
			p := PortInfo{
				Name: d.Name, Product: d.Product, Manufacturer: d.Manufacturer,
				VID: strings.ToUpper(d.VID), PID: strings.ToUpper(d.PID),
				SerialNumber: d.SerialNumber, IsUSB: d.IsUSB,
			}
			if p.IsUSB {
				p.Chip = ChipName(p.VID, p.PID)
			}
			out = append(out, p)
		}
	} else {
		names, err2 := serial.GetPortsList()
		if err2 != nil {
			return nil, err2
		}
		for _, n := range names {
			out = append(out, PortInfo{Name: n})
		}
	}
	return finalize(runtime.GOOS, out), nil
}

func finalize(goos string, in []PortInfo) []PortInfo {
	out := in[:0]
	for _, p := range in {
		if goos == "darwin" && strings.HasPrefix(p.Name, "/dev/tty.") {
			continue
		}
		p.Display = display(p)
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return naturalLess(out[i].Name, out[j].Name) })
	return out
}

func display(p PortInfo) string {
	desc := p.Product
	if p.Chip != "" && !strings.Contains(strings.ToUpper(desc), strings.ToUpper(strings.Fields(p.Chip)[0])) {
		if desc == "" {
			desc = p.Chip
		} else {
			desc += " " + p.Chip
		}
	}
	if desc == "" && p.Manufacturer != "" {
		desc = p.Manufacturer
	}
	s := p.Name
	if desc != "" {
		s += " — " + desc
	}
	if p.IsUSB && p.VID != "" {
		s += fmt.Sprintf(" (%s:%s)", p.VID, p.PID)
	}
	return s
}

// naturalLess compares strings treating digit runs as numbers.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ca, cb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			na, ra := digits(a)
			nb, rb := digits(b)
			ia, _ := strconv.Atoi(na)
			ib, _ := strconv.Atoi(nb)
			if ia != ib {
				return ia < ib
			}
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			a, b = ra, rb
			continue
		}
		la, lb := unicode.ToLower(ca), unicode.ToLower(cb)
		if la != lb {
			return la < lb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

// OpenErrorHint returns a human-friendly message with a platform fix hint.
func OpenErrorHint(goos, port string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	var pe *serial.PortError
	code := serial.PortErrorCode(-1)
	if errors.As(err, &pe) {
		code = pe.Code()
	}
	var hint string
	switch {
	case code == serial.PermissionDenied || errors.Is(err, fs.ErrPermission) || strings.Contains(low, "permission denied") || strings.Contains(low, "access is denied"):
		switch goos {
		case "linux":
			hint = "Your user lacks access to " + port + ". Run `sudo usermod -aG dialout $USER` (or `uucp` on Arch), then log out and back in."
		case "windows":
			hint = port + " is probably open in another program (PuTTY, Arduino IDE, another Pinion tab). Close it and retry."
		default:
			hint = "Permission denied. Check that no other program is using " + port + "."
		}
	case code == serial.PortBusy || strings.Contains(low, "busy") || strings.Contains(low, "in use"):
		hint = port + " is in use by another program. Close it and retry."
	case code == serial.PortNotFound || errors.Is(err, fs.ErrNotExist) || strings.Contains(low, "not found") || strings.Contains(low, "no such file") || strings.Contains(low, "cannot find"):
		switch goos {
		case "windows":
			hint = port + " not found. Re-plug the adapter and check Device Manager; CH340/CH341 and PL2303 adapters need the vendor driver (WCH / Prolific)."
		case "darwin":
			hint = port + " not found. Re-plug the adapter. CH340/CH341 and PL2303 adapters may need the vendor driver (WCH / Prolific); use /dev/cu.* rather than /dev/tty.*."
		default:
			hint = port + " not found. Re-plug the adapter and check `dmesg` for ttyUSB/ttyACM devices."
		}
	case code == serial.InvalidSpeed:
		hint = "The adapter does not support this baud rate."
	}
	if goos == "darwin" && strings.HasPrefix(port, "/dev/tty.") && hint == "" {
		hint = "On macOS open /dev/cu.* instead of /dev/tty.* (tty.* waits for carrier detect)."
	}
	if hint == "" {
		return fmt.Sprintf("open %s: %s", port, msg)
	}
	return fmt.Sprintf("open %s: %s — %s", port, msg, hint)
}
