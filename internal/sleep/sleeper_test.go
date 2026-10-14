package sleep

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeServer struct {
	mu       sync.Mutex
	running  bool
	players  int
	deferred bool
	stops    int
	starts   int
	startErr error
	release  func()
	restarts int
}

func (f *fakeServer) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeServer) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.running = false
	return nil
}

func (f *fakeServer) Start(context.Context) error {
	f.mu.Lock()
	err := f.startErr
	f.starts++
	f.mu.Unlock()
	if err != nil {
		return err
	}
	f.release() // what lifecycle.Controller does through BeforeStart
	f.mu.Lock()
	f.running = true
	f.mu.Unlock()
	return nil
}

type harness struct {
	t      *testing.T
	s      *Sleeper
	server *fakeServer
	clock  sync.Mutex
	now    time.Time
	cfg    Config
	addr   net.Addr // last sentinel address
}

func newHarness(t *testing.T) *harness {
	h := &harness{
		t:      t,
		server: &fakeServer{running: true},
		now:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		cfg:    Config{Enabled: true, IdleAfter: 10 * time.Minute, Port: 5520},
	}
	h.s = New(Deps{
		Config:      func() Config { return h.cfg },
		Running:     h.server.Running,
		PlayerCount: func() int { return h.server.players },
		Deferred:    func() bool { return h.server.deferred },
		Stop:        h.server.Stop,
		Start:       h.server.Start,
		Listen: func(string, int) (net.PacketConn, error) {
			conn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err == nil {
				h.addr = conn.LocalAddr()
			}
			return conn, err
		},
		Now: func() time.Time {
			h.clock.Lock()
			defer h.clock.Unlock()
			return h.now
		},
	})
	h.server.release = h.s.Release
	t.Cleanup(h.s.Release)
	return h
}

// advance moves the fake clock forward.
func (h *harness) advance(d time.Duration) {
	h.clock.Lock()
	h.now = h.now.Add(d)
	h.clock.Unlock()
}

// idle advances the clock by d, ticking along the way.
func (h *harness) idle(d time.Duration) {
	h.s.tick()
	h.advance(d)
	h.s.tick()
}

func (h *harness) send(size int, first byte) {
	h.t.Helper()
	conn, err := net.Dial("udp", h.addr.String())
	if err != nil {
		h.t.Fatal(err)
	}
	// Closed at the end of the test: on Windows, closing right after the
	// write may drop the datagram.
	h.t.Cleanup(func() { conn.Close() })
	packet := make([]byte, size)
	packet[0] = first
	if _, err := conn.Write(packet); err != nil {
		h.t.Fatal(err)
	}
}

// awaitWake processes the wake-up the sentinel signals, if any.
func (h *harness) awaitWake(timeout time.Duration) bool {
	select {
	case ev := <-h.s.wake:
		h.s.wakeUp(context.Background(), ev)
		return true
	case <-time.After(timeout):
		return false
	}
}

func (h *harness) listening() bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.conn != nil
}

func TestSleepsWhenEmptyAndWakesOnQUICInitial(t *testing.T) {
	h := newHarness(t)
	h.idle(9 * time.Minute)
	if h.server.stops != 0 {
		t.Fatal("slept before the idle delay")
	}
	h.idle(time.Minute)
	if h.server.stops != 1 || !h.s.Sleeping() || !h.listening() {
		t.Fatalf("stops=%d sleeping=%v listening=%v", h.server.stops, h.s.Sleeping(), h.listening())
	}

	h.send(100, 0xC0)  // too small for an Initial
	h.send(1200, 0x40) // short header
	if h.awaitWake(200 * time.Millisecond) {
		t.Fatal("woken up by a stray datagram")
	}
	h.send(1200, 0xC3)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up by a QUIC Initial")
	}
	if h.server.starts != 1 || h.s.Sleeping() || h.listening() {
		t.Fatalf("starts=%d sleeping=%v listening=%v", h.server.starts, h.s.Sleeping(), h.listening())
	}
}

func TestStaysUpWithPlayersOrDeferred(t *testing.T) {
	h := newHarness(t)
	h.server.players = 1
	h.idle(time.Hour)
	h.server.players, h.server.deferred = 0, true
	h.idle(time.Hour)
	if h.server.stops != 0 {
		t.Fatal("slept while busy")
	}
	h.server.deferred = false
	h.idle(5 * time.Minute)
	if h.server.stops != 0 {
		t.Fatal("idle time counted while busy")
	}
}

func TestDisabledNeverSleeps(t *testing.T) {
	h := newHarness(t)
	h.cfg.Enabled = false
	h.idle(time.Hour)
	if h.server.stops != 0 || h.listening() {
		t.Fatal("slept while disabled")
	}
}

func TestDisablingCancelsSleep(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	h.cfg.Enabled = false
	h.s.tick()
	if h.s.Sleeping() || h.listening() {
		t.Fatal("still asleep once disabled")
	}
}

func TestCancelKeepsServerStopped(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	if !h.s.Cancel() {
		t.Fatal("Cancel did not report the sleep")
	}
	h.s.tick()
	if h.s.Sleeping() || h.listening() || h.server.starts != 0 {
		t.Fatal("sleep not cancelled")
	}
	if h.s.Cancel() {
		t.Fatal("Cancel reported a sleep twice")
	}
}

func TestManualStartReleasesPort(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	if err := h.server.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.s.Sleeping() || h.listening() {
		t.Fatal("port still held after a start")
	}
}

func TestFailedWakeUpListensAgainLater(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	h.server.startErr = errors.New("boom")
	h.send(1200, 0xC0)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up")
	}
	if !h.s.Sleeping() || h.listening() {
		t.Fatal("should stay asleep, port released until the retry")
	}
	h.s.tick()
	if h.listening() {
		t.Fatal("listening again before the retry delay")
	}
	h.advance(retryDelay)
	h.s.tick()
	if !h.listening() {
		t.Fatal("not listening again after the retry delay")
	}
}

func TestPortChangeMovesSentinel(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	first := h.addr
	h.cfg.Port = 5521
	h.s.tick()
	if h.addr == first || !h.listening() {
		t.Fatal("sentinel not moved to the new port")
	}
}

// newHookHarness enables the bind hook socket.
func newHookHarness(t *testing.T) (*harness, func()) {
	h := newHarness(t)
	// Short path: Unix socket paths are limited to 108 bytes.
	dir, err := os.MkdirTemp("", "hk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h.s.d.HookSocket = filepath.Join(dir, "hook.sock")
	h.s.d.Restart = func(context.Context) error {
		h.server.mu.Lock()
		h.server.restarts++
		h.server.mu.Unlock()
		return nil
	}
	stop := h.s.startHook()
	if stop == nil {
		t.Fatal("hook socket not started")
	}
	return h, stop
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// bindHook plays the part of the LD_PRELOAD library.
func bindHook(t *testing.T, path string) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("bind 5520\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "ok\n" {
		t.Fatalf("reply %q, %v", reply, err)
	}
}

func TestHookHoldsPortUntilServerBinds(t *testing.T) {
	h, cancel := newHookHarness(t)
	defer cancel()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC0)
	if !h.awaitWake(2 * time.Second) {
		t.Fatal("not woken up")
	}
	if h.s.Sleeping() || !h.listening() {
		t.Fatal("the port must stay held while the server boots")
	}
	h.send(1200, 0xC0) // retransmission during the boot: no second start
	h.awaitWake(200 * time.Millisecond)
	if h.server.starts != 1 {
		t.Fatalf("starts=%d", h.server.starts)
	}

	bindHook(t, h.s.d.HookSocket)
	if h.listening() {
		t.Fatal("port not released on bind")
	}
	bindHook(t, h.s.d.HookSocket) // second address family: still answered
}

func TestHookHandoffEndsIfServerDies(t *testing.T) {
	h, cancel := newHookHarness(t)
	defer cancel()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC0)
	h.awaitWake(2 * time.Second)
	h.server.mu.Lock()
	h.server.running = false
	h.server.mu.Unlock()
	h.advance(handoffStartGrace)
	h.s.checkHandoff()
	if h.listening() {
		t.Fatal("port still held although the server died")
	}
}

func TestBindFailureDuringHandoffRestartsWithoutHolding(t *testing.T) {
	h, cancel := newHookHarness(t)
	defer cancel()
	h.idle(10 * time.Minute)
	h.send(1200, 0xC0)
	h.awaitWake(2 * time.Second)
	h.s.ObserveLine("[ServerManager|P] Failed to bind to /0.0.0.0:5520")
	waitUntil(t, "restart", func() bool {
		h.server.mu.Lock()
		defer h.server.mu.Unlock()
		return h.server.restarts == 1
	})
	if h.listening() {
		t.Fatal("port still held")
	}
	// Next time, the port is released before the server starts.
	h.idle(10 * time.Minute)
	h.send(1200, 0xC0)
	h.awaitWake(2 * time.Second)
	if h.listening() {
		t.Fatal("port held again despite the failed handoff")
	}
}

func TestStaleWakeIgnored(t *testing.T) {
	h := newHarness(t)
	h.idle(10 * time.Minute)
	h.s.mu.Lock()
	stale := h.s.conn
	h.s.mu.Unlock()
	h.s.Cancel()
	h.s.mu.Lock()
	h.s.sleeping = true // asleep again with a new sentinel
	h.s.mu.Unlock()
	h.s.ensureListening(h.cfg.Port)
	h.s.wakeUp(context.Background(), wakeEvent{conn: stale})
	if h.server.starts != 0 {
		t.Fatal("stale wake-up started the server")
	}
}
