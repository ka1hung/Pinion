//go:build windows

package sshx

import (
	"io"
	"os"
)

// Windows OpenSSH agent listens on a named pipe; os.OpenFile works for
// synchronous I/O on named pipes. Pageant users can enable its OpenSSH-pipe
// compatibility or point SSH_AUTH_SOCK-style tools at the same pipe.
func dialAgent() (io.ReadWriteCloser, error) {
	pipe := os.Getenv("PINION_SSH_AGENT_PIPE")
	if pipe == "" {
		pipe = `\\.\pipe\openssh-ssh-agent`
	}
	f, err := os.OpenFile(pipe, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return f, nil
}
