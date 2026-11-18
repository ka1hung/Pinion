// Command pinion-bind binds a privileged port (TFTP 69, FTP 21, HTTP 80/443)
// on behalf of Pinion and hands the socket back over fd 3. It is the only
// file that gets cap_net_bind_service, so the Pinion GUI process itself stays
// unprivileged. See internal/privbind.
//
// Pinion runs it; it is not meant to be run by hand.
package main

import (
	"fmt"
	"os"

	"github.com/ka1hung/pinion/internal/privbind"
)

func main() {
	if err := privbind.Serve(os.NewFile(3, "ctl")); err != nil {
		fmt.Fprintln(os.Stderr, "pinion-bind:", err)
		os.Exit(1)
	}
}
