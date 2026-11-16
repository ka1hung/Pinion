package app

import "github.com/ka1hung/pinion/internal/serialx"

// SerialService lists serial ports.
type SerialService struct{ c *Core }

// NewSerialService creates the service.
func NewSerialService(c *Core) *SerialService { return &SerialService{c: c} }

// ListPorts returns serial ports with USB details, naturally sorted.
func (s *SerialService) ListPorts() ([]serialx.PortInfo, error) {
	ports, err := serialx.ListPorts()
	if ports == nil {
		ports = []serialx.PortInfo{}
	}
	return ports, err
}
