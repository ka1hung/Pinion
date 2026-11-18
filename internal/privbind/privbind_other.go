//go:build !linux

package privbind

import "net"

// Listen is net.Listen.
func Listen(network, addr string) (net.Listener, error) { return net.Listen(network, addr) }

// ListenPacket is net.ListenPacket.
func ListenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}
