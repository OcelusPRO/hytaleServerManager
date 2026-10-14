//go:build !linux

package sleep

import (
	"errors"
	"net"
	"syscall"
)

// canKeepFirst: elsewhere the bind hook does not exist and the first packet
// cannot be kept for the server.
const canKeepFirst = false

func (s *Sleeper) watchKeep(conn net.PacketConn) { s.watch(conn) }

func sharePort(string, string, syscall.RawConn) error { return nil }

func passSocket(*net.UnixConn, net.PacketConn) error {
	return errors.New("socket handover unsupported on this system")
}
