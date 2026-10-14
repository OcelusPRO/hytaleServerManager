//go:build linux

package sleep

import (
	"errors"
	"net"
	"syscall"
)

// canKeepFirst: on Linux the first packet of a client is left unread in the
// sentinel's queue and the socket itself is handed over to the server.
const canKeepFirst = true

// watchKeep peeks at the sentinel instead of reading it: the first packet
// opening a QUIC connection stays queued (with every packet arriving after
// it) for the server, which receives the socket when it binds the port.
// Stray datagrams in front of it are discarded.
func (s *Sleeper) watchKeep(conn net.PacketConn) {
	rc, err := rawConn(conn)
	if err != nil {
		s.watch(conn)
		return
	}
	buf := make([]byte, 2048)
	for {
		var n int
		var from syscall.Sockaddr
		var rerr error
		err := rc.Read(func(fd uintptr) bool {
			n, from, rerr = syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK)
			return rerr != syscall.EAGAIN
		})
		if err != nil || rerr != nil {
			return // closed
		}
		if isQUICInitial(buf[:n]) {
			addr := udpAddr(from)
			s.count(conn, addr)
			s.signal(conn, addr)
			return // left in the queue for the server
		}
		err = rc.Read(func(fd uintptr) bool {
			_, _, rerr = syscall.Recvfrom(int(fd), buf, 0)
			return rerr != syscall.EAGAIN
		})
		if err != nil || rerr != nil {
			return
		}
	}
}

// sharePort sets SO_REUSEADDR and SO_REUSEPORT on the sentinel, so that the
// server can bind its IPv6 socket (dual-stack) while the IPv4 sentinel still
// holds the port.
func sharePort(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if serr == nil {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
		}
	})
	return errors.Join(err, serr)
}

// soReusePort is SO_REUSEPORT, missing from the syscall package.
const soReusePort = 0xf

// passSocket sends the sentinel socket to the bind hook (SCM_RIGHTS).
func passSocket(to *net.UnixConn, conn net.PacketConn) error {
	rc, err := rawConn(conn)
	if err != nil {
		return err
	}
	var werr error
	err = rc.Control(func(fd uintptr) {
		_, _, werr = to.WriteMsgUnix([]byte("fd\n"), syscall.UnixRights(int(fd)), nil)
	})
	return errors.Join(err, werr)
}

func rawConn(conn net.PacketConn) (syscall.RawConn, error) {
	uc, ok := conn.(*net.UDPConn)
	if !ok {
		return nil, errors.New("not a UDP socket")
	}
	return uc.SyscallConn()
}

func udpAddr(sa syscall.Sockaddr) net.Addr {
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		return &net.UDPAddr{IP: net.IP(a.Addr[:]).To16(), Port: a.Port}
	case *syscall.SockaddrInet6:
		return &net.UDPAddr{IP: net.IP(a.Addr[:]), Port: a.Port}
	}
	return &net.UDPAddr{}
}
