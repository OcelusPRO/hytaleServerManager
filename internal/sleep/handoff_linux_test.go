//go:build linux

package sleep

import (
	"bufio"
	"net"
	"syscall"
	"testing"
	"time"
)

// askSocket plays the part of the bind hook for an IPv4 wildcard bind and
// returns the socket handed over.
func askSocket(t *testing.T, path string) int {
	t.Helper()
	fd, conn := askSocketGate(t, path)
	conn.Close()
	return fd
}

// askSocketGate is askSocket, also returning the connection on which the
// manager sends "go".
func askSocketGate(t *testing.T, path string) (int, net.Conn) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("bind 5520 2 0 1\n")); err != nil {
		t.Fatal(err)
	}
	buf, oob := make([]byte, 16), make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, err := conn.(*net.UnixConn).ReadMsgUnix(buf, oob)
	if err != nil || string(buf[:n]) != "fd\n" {
		t.Fatalf("reply %q, %v", buf[:n], err)
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) != 1 {
		t.Fatalf("control messages %v, %v", msgs, err)
	}
	fds, err := syscall.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) != 1 {
		t.Fatalf("rights %v, %v", fds, err)
	}
	return fds[0], conn
}

func TestGoSentOnceServerBooted(t *testing.T) {
	h, stop := newHookHarness(t)
	defer stop()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC3)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up")
	}
	fd, conn := askSocketGate(t, h.s.d.HookSocket)
	defer syscall.Close(fd)
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if n, _ := conn.Read(make([]byte, 8)); n != 0 {
		t.Fatal("go sent before the server booted")
	}
	h.s.ObserveLine("[HytaleServer]          Hytale Server Booted! [Multiplayer] took 8sec")
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "go\n" {
		t.Fatalf("reply %q, %v", reply, err)
	}
}

func TestGoSentIfBootNeverDetected(t *testing.T) {
	h, stop := newHookHarness(t)
	defer stop()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC3)
	h.awaitWake(2 * time.Second)
	fd, conn := askSocketGate(t, h.s.d.HookSocket)
	defer syscall.Close(fd)
	defer conn.Close()

	waitUntil(t, "gate recorded", func() bool {
		h.s.mu.Lock()
		defer h.s.mu.Unlock()
		return h.s.gate != nil
	})
	h.advance(maxGate)
	h.s.checkHandoff()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if reply, _ := bufio.NewReader(conn).ReadString('\n'); reply != "go\n" {
		t.Fatalf("reply %q", reply)
	}
}

func TestSocketHandedOverWithFirstPacket(t *testing.T) {
	h, stop := newHookHarness(t)
	defer stop()
	if !h.s.keepFirst {
		t.Fatal("keepFirst should be enabled on Linux")
	}
	h.idle(10 * time.Minute)
	h.send(100, 0xC0) // stray datagram in front: discarded
	h.send(1200, 0xC3)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up")
	}
	h.send(1200, 0xC5) // second attempt while the server boots

	// IPv6-only bind first: the IPv4 sentinel is kept.
	conn, err := net.Dial("unix", h.s.d.HookSocket)
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("bind 5520 10 1 1\n"))
	if reply, _ := bufio.NewReader(conn).ReadString('\n'); reply != "ok\n" {
		t.Fatalf("reply %q", reply)
	}
	conn.Close()
	if !h.listening() {
		t.Fatal("IPv4 sentinel released by an IPv6-only bind")
	}

	fd := askSocket(t, h.s.d.HookSocket)
	defer syscall.Close(fd)
	if h.listening() {
		t.Fatal("manager kept its copy of the socket")
	}
	// The packets are still queued, as if the server had received them.
	buf := make([]byte, 2048)
	for _, first := range []byte{0xC3, 0xC5} {
		syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 2})
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil || n != 1200 || buf[0] != first {
			t.Fatalf("queued packet: n=%d first=%#x err=%v", n, buf[0], err)
		}
	}
}

// TestDualStackBindBesideSharedSentinel checks the kernel rule the handoff
// relies on: the server's dual-stack IPv6 socket can bind the port held by
// the IPv4 sentinel when both share it.
func TestDualStackBindBesideSharedSentinel(t *testing.T) {
	s := New(Deps{})
	sentinel, err := s.d.Listen("udp4", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sentinel.Close()
	port := sentinel.LocalAddr().(*net.UDPAddr).Port

	bindV6 := func(reuse bool) error {
		fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_DGRAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 0)
		if reuse {
			syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReusePort, 1)
		}
		return syscall.Bind(fd, &syscall.SockaddrInet6{Port: port})
	}
	if err := bindV6(false); err == nil {
		t.Log("note: dual-stack bind succeeded even without sharing options")
	}
	if err := bindV6(true); err != nil {
		t.Fatalf("dual-stack bind with sharing options failed: %v", err)
	}
}

func TestDualStackBindKeepsHolding(t *testing.T) {
	h, stop := newHookHarness(t)
	defer stop()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC3)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up")
	}
	conn, err := net.Dial("unix", h.s.d.HookSocket)
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("bind 5520 10 0 1 1 0\n"))
	if reply, _ := bufio.NewReader(conn).ReadString('\n'); reply != "ok\n" {
		t.Fatalf("reply %q", reply)
	}
	conn.Close()
	if !h.listening() {
		t.Fatal("sentinel released by a dual-stack bind sharing the port")
	}
	fd := askSocket(t, h.s.d.HookSocket)
	syscall.Close(fd)
}
