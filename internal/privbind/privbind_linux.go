package privbind

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const exchangeTimeout = 10 * time.Second

// Listen is net.Listen; when that fails with "permission denied" on an
// allowed privileged port it asks the pinion-bind helper for the socket.
func Listen(network, addr string) (net.Listener, error) {
	ln, err := net.Listen(network, addr)
	if err == nil || !useHelper("tcp", addr, err) {
		return ln, err
	}
	f, herr := viaHelper("tcp", addr)
	if herr != nil {
		return nil, &HelperError{Err: err, Helper: herr}
	}
	defer f.Close()
	return net.FileListener(f)
}

// ListenPacket is net.ListenPacket with the same helper fallback as Listen.
func ListenPacket(network, addr string) (net.PacketConn, error) {
	pc, err := net.ListenPacket(network, addr)
	if err == nil || !useHelper("udp", addr, err) {
		return pc, err
	}
	f, herr := viaHelper("udp", addr)
	if herr != nil {
		return nil, &HelperError{Err: err, Helper: herr}
	}
	defer f.Close()
	return net.FilePacketConn(f)
}

func useHelper(proto, addr string, err error) bool {
	if !errors.Is(err, syscall.EACCES) {
		return false
	}
	_, ps, e := net.SplitHostPort(addr)
	if e != nil {
		return false
	}
	port, e := strconv.Atoi(ps)
	return e == nil && Allowed(proto, port)
}

// helperPath finds pinion-bind next to the running executable. It is never
// looked up on $PATH.
func helperPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	p := filepath.Join(filepath.Dir(exe), HelperName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("helper not installed (%s)", p)
	}
	return p, nil
}

// viaHelper runs the helper with one end of a socketpair as fd 3 and returns
// the bound socket it sends back.
func viaHelper(proto, addr string) (*os.File, error) {
	path, err := helperPath()
	if err != nil {
		return nil, err
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fds[0]), "privbind")
	child := os.NewFile(uintptr(fds[1]), "privbind-child")
	defer parent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), exchangeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.ExtraFiles = []*os.File{child} // becomes fd 3
	cmd.Env = []string{}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Start()
	child.Close() // only the helper holds it now, so its exit gives us EOF
	if err != nil {
		return nil, err
	}
	f, xerr := exchange(parent, proto, addr)
	werr := cmd.Wait()
	if xerr != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return nil, fmt.Errorf("%v: %s", xerr, s)
		}
		if werr != nil {
			return nil, fmt.Errorf("%v (%v)", xerr, werr)
		}
		return nil, xerr
	}
	return f, nil
}

// exchange sends the request on ctl and receives the socket.
// Request: "<tcp|udp> <host or *> <port>\n".
// Reply:   "ok" with the fd attached, or "err <message>".
func exchange(ctl *os.File, proto, addr string) (*os.File, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if host == "" {
		host = "*"
	}
	c, err := net.FileConn(ctl)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return nil, errors.New("control socket is not a Unix socket")
	}
	_ = uc.SetDeadline(time.Now().Add(exchangeTimeout))
	if _, err := fmt.Fprintf(uc, "%s %s %s\n", proto, host, port); err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, _, _, err := uc.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, fmt.Errorf("no reply from helper: %w", err)
	}
	reply := string(buf[:n])
	var fd = -1
	if msgs, err := unix.ParseSocketControlMessage(oob[:oobn]); err == nil {
		for _, m := range msgs {
			if got, err := unix.ParseUnixRights(&m); err == nil {
				for _, x := range got {
					if fd < 0 {
						fd = x
					} else {
						unix.Close(x)
					}
				}
			}
		}
	}
	if reply != "ok" || fd < 0 {
		if fd >= 0 {
			unix.Close(fd)
		}
		if msg, ok := strings.CutPrefix(reply, "err "); ok {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("unexpected reply %q", reply)
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "privbind-socket"), nil
}

// Serve is the helper side: it reads one request from ctl, binds the
// socket and sends it back. Used by cmd/pinion-bind.
func Serve(ctl *os.File) error { return serve(ctl, Allowed) }

func serve(ctl *os.File, allow func(string, int) bool) error {
	c, err := net.FileConn(ctl)
	if err != nil {
		return fmt.Errorf("fd 3 is not a socket (run by Pinion, not by hand): %w", err)
	}
	defer c.Close()
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return errors.New("fd 3 is not a Unix socket")
	}
	_ = uc.SetDeadline(time.Now().Add(exchangeTimeout))
	line, err := bufio.NewReader(io.LimitReader(uc, 256)).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	fd, err := bindRequest(strings.TrimSpace(line), allow)
	if err != nil {
		_, _ = uc.Write([]byte("err " + err.Error()))
		return err
	}
	defer unix.Close(fd)
	_, _, err = uc.WriteMsgUnix([]byte("ok"), unix.UnixRights(fd), nil)
	return err
}

func bindRequest(line string, allow func(string, int) bool) (int, error) {
	f := strings.Fields(line)
	if len(f) != 3 {
		return -1, errors.New("bad request")
	}
	proto, host := f[0], f[1]
	port, err := strconv.Atoi(f[2])
	if err != nil || port < 0 || port > 65535 {
		return -1, errors.New("bad port")
	}
	if proto != "tcp" && proto != "udp" {
		return -1, errors.New("bad protocol")
	}
	if !allow(proto, port) {
		return -1, fmt.Errorf("%s port %d is not one Pinion may open (allowed: udp 69, tcp 21/80/443)", proto, port)
	}
	typ := unix.SOCK_DGRAM
	if proto == "tcp" {
		typ = unix.SOCK_STREAM
	}
	if host == "*" {
		// All interfaces: dual-stack like Go's net.Listen(":port"), falling
		// back to IPv4 when IPv6 is unavailable.
		if fd, err := bindFD(unix.AF_INET6, typ, &unix.SockaddrInet6{Port: port}, true); err == nil {
			return fd, nil
		} else if !errors.Is(err, unix.EAFNOSUPPORT) && !errors.Is(err, unix.EADDRNOTAVAIL) {
			return -1, err
		}
		return bindFD(unix.AF_INET, typ, &unix.SockaddrInet4{Port: port}, false)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return -1, errors.New("bad address")
	}
	if ip4 := ip.To4(); ip4 != nil {
		sa := &unix.SockaddrInet4{Port: port}
		copy(sa.Addr[:], ip4)
		return bindFD(unix.AF_INET, typ, sa, false)
	}
	sa := &unix.SockaddrInet6{Port: port}
	copy(sa.Addr[:], ip.To16())
	return bindFD(unix.AF_INET6, typ, sa, false)
}

func bindFD(family, typ int, sa unix.Sockaddr, dualStack bool) (int, error) {
	fd, err := unix.Socket(family, typ|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	fail := func(err error) (int, error) {
		unix.Close(fd)
		return -1, err
	}
	if typ == unix.SOCK_STREAM {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			return fail(err)
		}
	}
	if family == unix.AF_INET6 {
		v6only := 1
		if dualStack {
			v6only = 0
		}
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, v6only); err != nil {
			return fail(err)
		}
	}
	if err := unix.Bind(fd, sa); err != nil {
		return fail(err)
	}
	if typ == unix.SOCK_STREAM {
		if err := unix.Listen(fd, unix.SOMAXCONN); err != nil {
			return fail(err)
		}
	}
	return fd, nil
}
