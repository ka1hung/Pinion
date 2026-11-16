package serialx

import (
	"errors"
	"strings"
	"testing"

	"go.bug.st/serial"
)

func TestFinalize(t *testing.T) {
	in := []PortInfo{{Name: "COM10"}, {Name: "COM2"}, {Name: "COM3", IsUSB: true, VID: "1A86", PID: "7523", Product: "USB-SERIAL CH340", Chip: "CH340"}}
	out := finalize("windows", in)
	if out[0].Name != "COM2" || out[1].Name != "COM3" || out[2].Name != "COM10" {
		t.Fatalf("%+v", out)
	}
	if out[1].Display != "COM3 — USB-SERIAL CH340 (1A86:7523)" {
		t.Fatal(out[1].Display)
	}
	mac := finalize("darwin", []PortInfo{{Name: "/dev/tty.usbserial-1"}, {Name: "/dev/cu.usbserial-1", IsUSB: true, VID: "10C4", PID: "EA60", Chip: "CP210x"}})
	if len(mac) != 1 || mac[0].Display != "/dev/cu.usbserial-1 — CP210x (10C4:EA60)" {
		t.Fatalf("%+v", mac)
	}
	if ChipName("067b", "2303") != "PL2303" {
		t.Fatal("chip")
	}
}

func TestHints(t *testing.T) {
	h := OpenErrorHint("linux", "/dev/ttyUSB0", &serial.PortError{})
	_ = h
	if !strings.Contains(OpenErrorHint("linux", "/dev/ttyUSB0", errors.New("open /dev/ttyUSB0: permission denied")), "dialout") {
		t.Fatal("dialout")
	}
	if !strings.Contains(OpenErrorHint("windows", "COM9", errors.New("Serial port not found")), "CH340") {
		t.Fatal("driver")
	}
}
