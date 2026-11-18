//go:build !linux

// Command pinion-bind is only needed on Linux.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "pinion-bind: only used on Linux")
	os.Exit(1)
}
