package transport

import (
	"testing"

	"go.bug.st/serial"

	"github.com/ka1hung/pinion/internal/store"
)

func TestSerialMode(t *testing.T) {
	off := false
	m, err := serialMode(&store.SerialConfig{Baud: 9600, DataBits: 7, Parity: "even", StopBits: "2", DTR: &off})
	if err != nil {
		t.Fatal(err)
	}
	if m.BaudRate != 9600 || m.DataBits != 7 || m.Parity != serial.EvenParity || m.StopBits != serial.TwoStopBits || m.InitialStatusBits.DTR || !m.InitialStatusBits.RTS {
		t.Fatalf("%+v", m)
	}
	if _, err := serialMode(&store.SerialConfig{Parity: "x"}); err == nil {
		t.Fatal("want error")
	}
	s := &Serial{Config: &store.SerialConfig{Port: "COM3", Baud: 115200, DataBits: 8, Parity: "none", StopBits: "1"}}
	if s.Describe() != "Serial COM3 115200 8N1" {
		t.Fatal(s.Describe())
	}
}
