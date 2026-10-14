package sleep

import (
	"bufio"
	"errors"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// hookTimeout bounds one exchange with the bind hook library, which itself
// gives up after 5 seconds and binds anyway.
const hookTimeout = 3 * time.Second

// listenHook opens the Unix socket the bind hook library connects to.
func listenHook(path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600) // only the server process (same user) may talk to it
	return ln, nil
}

// serveHook answers the bind hook. For each bind of the game port, the
// manager either makes sure the port is free, answers "ok" and closes the
// connection, or hands its socket over ("fd" with SCM_RIGHTS, its own copy
// closed) and keeps the connection: "go" is sent once the server has booted,
// and only then does the hook let the server read the socket.
func (s *Sleeper) serveHook(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // closed
		}
		go s.handleHook(conn)
	}
}

func (s *Sleeper) handleHook(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(hookTimeout))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		conn.Close()
		return
	}
	if req, ok := parseBind(line); ok {
		uc, _ := conn.(*net.UnixConn)
		if s.bindRequested(req, uc) {
			return // socket handed over: conn is kept to send "go"
		}
	}
	_, _ = conn.Write([]byte("ok\n"))
	conn.Close()
}

// parseBind reads "bind <port> <domain> <v6only> <any> <reuseaddr>
// <reuseport>" (the short form "bind <port>" only asks for the port to be
// released).
func parseBind(line string) (bindRequest, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "bind" {
		return bindRequest{}, false
	}
	flag := func(i int) bool { return len(fields) > i && fields[i] == "1" }
	var req bindRequest
	if len(fields) > 2 {
		req.domain, _ = strconv.Atoi(fields[2])
	}
	req.v6only, req.any, req.reuseAddr, req.reusePort = flag(3), flag(4), flag(5), flag(6)
	return req, true
}
