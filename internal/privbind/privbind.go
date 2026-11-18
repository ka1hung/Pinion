// Package privbind opens listening sockets on privileged ports (below 1024).
//
// On Linux, binding such a port needs CAP_NET_BIND_SERVICE. Giving that
// capability to Pinion itself makes the kernel mark the whole GUI process
// non-dumpable, which locks /proc/<pid>/ and breaks xdg-desktop-portal (file
// drag and drop, see issue #1). Instead, a tiny separate program,
// pinion-bind, carries the capability: Pinion runs it, it creates and binds
// the socket, passes the file descriptor back over a Unix socket
// (SCM_RIGHTS) and exits. The kernel only checks the capability at bind(),
// so Pinion can then use the socket without any privilege.
//
// On other systems Listen and ListenPacket are plain net.Listen and
// net.ListenPacket.
package privbind

import (
	"errors"
	"fmt"
)

// HelperName is the helper's file name, installed next to the Pinion binary.
const HelperName = "pinion-bind"

// Allowed reports whether the helper may bind proto ("tcp" or "udp") on a
// privileged port. The list is kept short so the helper is not a general
// tool for grabbing any privileged port: TFTP, FTP and HTTP(S).
func Allowed(proto string, port int) bool {
	switch proto {
	case "udp":
		return port == 69
	case "tcp":
		return port == 21 || port == 80 || port == 443
	}
	return false
}

// HelperError is returned when the direct bind was denied and the helper
// could not help either. It unwraps to the original bind error, so
// errors.Is(err, syscall.EACCES) still holds.
type HelperError struct {
	Err    error // the original bind error
	Helper error // why the helper failed
}

func (e *HelperError) Error() string { return fmt.Sprintf("%v (%s: %v)", e.Err, HelperName, e.Helper) }
func (e *HelperError) Unwrap() error { return e.Err }

// LinuxHint explains a "permission denied" bind of a port below 1024 on
// Linux, including why the helper could not open it.
func LinuxHint(proto string, port int, err error) string {
	if !Allowed(proto, port) {
		return "ports below 1024 need privileges on Linux and Pinion only opens UDP 69 and TCP 21/80/443 there; choose a port above 1024"
	}
	hint := "Pinion opens this port through its " + HelperName + " helper, installed next to the Pinion binary with `sudo setcap cap_net_bind_service=+ep <folder>/" + HelperName + "` (the .deb/.rpm does this); or choose a port above 1024"
	var he *HelperError
	if errors.As(err, &he) {
		hint += " [" + HelperName + ": " + he.Helper.Error() + "]"
	}
	return hint
}
