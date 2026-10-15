// Package sleep puts an empty server to sleep and wakes it up on demand.
//
// Once the server has been empty for a while it is stopped cleanly (the
// world is saved by the stop command) and the manager listens on the game
// port in its place (the sentinel). The first QUIC packet of a player trying
// to connect starts the server, which then binds the port itself: the
// player's client keeps retransmitting its handshake until the server
// answers. Nothing sits between the players and a running server.
//
// While the server boots, the sentinel keeps the port (handoff). Hytale
// clients send a single packet per connection attempt (one attempt every 20
// seconds, two attempts) and never retransmit it, so on Linux these packets
// are left unread in the sentinel's queue. The server's bind() is
// intercepted by a small LD_PRELOAD library (native/bindhook) which asks the
// manager, through the hook socket, for the port: the sentinel socket itself
// is handed over, and the server reads the queued packets as if it had
// received them, once it has booted (it turns players away before). The
// player's very first attempt then succeeds. If the
// handover is not possible, the port is released at that moment instead;
// without the hook (non-Linux, library missing) it is released right before
// the server starts.
package sleep

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tickInterval = time.Second
	// retryDelay spaces out attempts to listen on the port or to wake up
	// after a failure, so that a broken setup does not spin.
	retryDelay = 30 * time.Second
	// minInitialSize is the smallest datagram carrying a QUIC Initial
	// packet: clients must pad it to 1200 bytes (RFC 9000, section 14.1).
	minInitialSize = 1200
	// handoffStartGrace lets the process start before a handoff whose
	// server is not running is considered failed.
	handoffStartGrace = 5 * time.Second
	// maxHandoff bounds the time the port is held while the server boots.
	maxHandoff = 2 * time.Minute
	// maxGate bounds the time queued connections wait for the end of the
	// boot once the socket is handed over (clients give up after ~40 s).
	maxGate = 30 * time.Second
)

// Console lines of the Hytale server watched during a handoff.
const (
	lineBindFailed = "Failed to bind"
	lineBooted     = "Hytale Server Booted!"
)

// Config is the operator-tunable behaviour, re-read on every tick.
type Config struct {
	Enabled   bool
	IdleAfter time.Duration // empty server for this long => sleep
	Port      int           // UDP game port
}

// Deps lists the collaborators of the Sleeper.
type Deps struct {
	Config      func() Config
	Running     func() bool
	PlayerCount func() int
	// Deferred reports that the server must stay up for now (e.g. a pending
	// automatic update that will restart it anyway).
	Deferred func() bool
	Stop     func() error
	Start    func(ctx context.Context) error
	// Restart recovers a server that could not bind the port during a
	// handoff (optional).
	Restart func(ctx context.Context) error
	// Listen opens the sentinel socket on network ("udp" or "udp4") on every
	// interface.
	Listen func(network string, port int) (net.PacketConn, error)
	// HookSocket is the Unix socket the bind hook library talks to; empty
	// when the hook is unavailable.
	HookSocket string
	// TestWakeDelay postpones the start after a wake-up (testing only).
	TestWakeDelay time.Duration
	Log           func(message string)
	Now           func() time.Time
}

// Sleeper watches the server and handles sleeping and waking up.
type Sleeper struct {
	d    Deps
	wake chan wakeEvent

	mu         sync.Mutex
	sleeping   bool           // asleep, waiting for a player
	handoff    bool           // server booting, port still held
	hookReady  bool           // the hook socket is listening
	keepFirst  bool           // first packets are kept for the server (Linux)
	hookBroken bool           // a handoff failed: release before starting
	watchBind  bool           // a handoff happened, the bind outcome is unknown
	gate       net.Conn       // hook waiting for "go" before reading the socket
	gateAt     time.Time      // when the socket was handed over
	conn       net.PacketConn // sentinel, nil when not listening
	connPort   int
	listenAt   time.Time // earliest next attempt to open the sentinel
	handoffAt  time.Time // start of the current handoff
	wokenAt    time.Time // first packet of the current wake-up
	flows      map[string]*flow
	idleSince  time.Time // only touched by the Run goroutine
}

// wakeEvent is a connection attempt seen by a sentinel.
type wakeEvent struct {
	conn net.PacketConn
	from net.Addr
}

// flow counts the connection attempts of one client address.
type flow struct {
	packets     int
	first, last time.Time
}

// New creates a sleeper; call Run to start it.
func New(d Deps) *Sleeper {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Listen == nil {
		d.Listen = func(network string, port int) (net.PacketConn, error) {
			lc := net.ListenConfig{}
			if network == "udp4" {
				// The socket to hand over shares the port with the
				// server's IPv6 socket, bound before it.
				lc.Control = sharePort
			}
			return lc.ListenPacket(context.Background(), network, ":"+strconv.Itoa(port))
		}
	}
	if d.Deferred == nil {
		d.Deferred = func() bool { return false }
	}
	return &Sleeper{d: d, wake: make(chan wakeEvent, 1)}
}

// Sleeping reports whether the server is asleep, waiting for a player.
func (s *Sleeper) Sleeping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sleeping
}

// Release is called right before the server starts, whoever starts it. With
// a working bind hook the port stays held until the server binds it;
// otherwise it is freed now.
func (s *Sleeper) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	wasAsleep := s.sleeping
	s.sleeping = false
	if s.conn == nil || s.handoff {
		return
	}
	if wasAsleep && s.hookReady && !s.hookBroken {
		s.handoff, s.handoffAt, s.watchBind = true, s.d.Now(), true
		return
	}
	s.closeLocked("")
}

// Cancel ends the sleep without starting the server (manual stop). It
// reports whether the server was asleep.
func (s *Sleeper) Cancel() bool {
	s.mu.Lock()
	was := s.sleeping
	s.sleeping = false
	if was {
		s.closeLocked("")
	}
	s.mu.Unlock()
	if was {
		s.log("Sleep cancelled: the server stays stopped.")
	}
	return was
}

// ObserveLine follows the server console to detect a failed handoff.
func (s *Sleeper) ObserveLine(line string) {
	switch {
	case strings.Contains(line, lineBooted):
		s.mu.Lock()
		s.watchBind = false
		s.openGateLocked("Server ready: it now handles the waiting connections.")
		s.mu.Unlock()
	case strings.Contains(line, lineBindFailed):
		s.mu.Lock()
		failed := s.watchBind
		s.watchBind = false
		if failed {
			s.hookBroken = true // never hold the port at startup again
			s.handoff = false
			s.closeLocked("")
		}
		s.mu.Unlock()
		if failed && s.d.Restart != nil {
			s.log("The server could not take the port while waking up: restarting without holding the port.")
			go func() {
				if err := s.d.Restart(context.Background()); err != nil {
					s.log("Restart failed: " + err.Error())
				}
			}()
		}
	}
}

// Run evaluates the situation every second until ctx ends.
func (s *Sleeper) Run(ctx context.Context) {
	if stop := s.startHook(); stop != nil {
		defer stop()
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		s.sleeping, s.handoff = false, false
		s.closeLocked("")
		s.openGateLocked("")
		s.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.wake:
			s.wakeUp(ctx, ev)
		case <-ticker.C:
			s.tick()
		}
	}
}

// startHook serves the bind hook socket, if configured. It returns the
// function stopping it, or nil.
func (s *Sleeper) startHook() func() {
	if s.d.HookSocket == "" {
		return nil
	}
	ln, err := listenHook(s.d.HookSocket)
	if err != nil {
		s.log(fmt.Sprintf("Bind interception unavailable (%s): the port will be released as soon as the server starts.", err))
		return nil
	}
	s.mu.Lock()
	s.hookReady, s.keepFirst = true, canKeepFirst
	s.mu.Unlock()
	go s.serveHook(ln)
	return func() { ln.Close() }
}

func (s *Sleeper) tick() {
	s.checkHandoff()
	cfg := s.d.Config()
	if !cfg.Enabled {
		s.idleSince = time.Time{}
		if s.Cancel() {
			s.log("Sleep mode disabled.")
		}
		return
	}
	if !s.d.Running() {
		s.idleSince = time.Time{}
		s.ensureListening(cfg.Port)
		return
	}
	if s.d.PlayerCount() > 0 || s.d.Deferred() {
		s.idleSince = time.Time{}
		return
	}
	now := s.d.Now()
	if s.idleSince.IsZero() {
		s.idleSince = now
	}
	if now.Sub(s.idleSince) >= cfg.IdleAfter {
		s.idleSince = time.Time{}
		s.fallAsleep(cfg)
	}
}

// checkHandoff frees the port if the server died during the handoff or
// never bound it.
func (s *Sleeper) checkHandoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil {
		switch {
		case s.d.Now().Sub(s.gateAt) >= maxGate:
			s.openGateLocked(fmt.Sprintf("End of boot not detected after %s: waiting connections released.", maxGate))
		case !s.d.Running():
			s.openGateLocked("")
		}
	}
	if !s.handoff {
		return
	}
	held := s.d.Now().Sub(s.handoffAt)
	switch {
	case held >= maxHandoff:
		s.handoff = false
		s.closeLocked(fmt.Sprintf("The server did not take the port after %s: port released.", maxHandoff))
	case held >= handoffStartGrace && !s.d.Running():
		s.handoff, s.watchBind = false, false
		s.closeLocked("The server stopped while waking up: port released.")
	}
}

// fallAsleep stops the empty server and starts listening in its place.
func (s *Sleeper) fallAsleep(cfg Config) {
	s.log(fmt.Sprintf("No player for %s: putting the server to sleep…", cfg.IdleAfter))
	if err := s.d.Stop(); err != nil {
		s.log("Could not put the server to sleep: " + err.Error())
		return
	}
	s.mu.Lock()
	s.sleeping = true
	s.listenAt = time.Time{}
	s.mu.Unlock()
	if s.ensureListening(cfg.Port) {
		s.log(fmt.Sprintf("Server asleep: it will start again on the next connection (UDP port %d).", cfg.Port))
	}
}

// ensureListening opens the sentinel while asleep, or moves it when the port
// setting changed. It reports whether the sentinel is listening.
func (s *Sleeper) ensureListening(port int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sleeping {
		return false
	}
	if s.conn != nil && s.connPort == port {
		return true
	}
	if s.d.Now().Before(s.listenAt) {
		return false
	}
	s.closeLocked("")
	// The socket handed over replaces the server's IPv4 one: same family.
	network := "udp"
	if s.keepFirst && !s.hookBroken {
		network = "udp4"
	}
	conn, err := s.d.Listen(network, port)
	if err != nil {
		s.listenAt = s.d.Now().Add(retryDelay)
		s.log(fmt.Sprintf("Could not listen on UDP port %d (%s), retrying in %s.", port, err, retryDelay))
		return false
	}
	s.conn, s.connPort = conn, port
	s.flows, s.wokenAt = make(map[string]*flow), time.Time{}
	if network == "udp4" {
		go s.watchKeep(conn)
	} else {
		go s.watch(conn)
	}
	return true
}

// watch reads the sentinel until it is closed: the first packet opening a
// QUIC connection wakes the server up, and every attempt is counted for the
// report printed when the port is handed over.
func (s *Sleeper) watch(conn net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		if !isQUICInitial(buf[:n]) {
			continue
		}
		s.count(conn, from)
		s.signal(conn, from)
	}
}

// signal asks the Run goroutine to wake the server up.
func (s *Sleeper) signal(conn net.PacketConn, from net.Addr) {
	select {
	case s.wake <- wakeEvent{conn, from}:
	default: // a wake-up is already pending
	}
}

func (s *Sleeper) count(conn net.PacketConn, from net.Addr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != conn {
		return
	}
	now := s.d.Now()
	if s.wokenAt.IsZero() {
		s.wokenAt = now
	}
	f := s.flows[from.String()]
	if f == nil {
		f = &flow{first: now}
		s.flows[from.String()] = f
	}
	f.packets++
	f.last = now
}

// wakeUp starts the server; the port is released through Release.
func (s *Sleeper) wakeUp(ctx context.Context, ev wakeEvent) {
	s.mu.Lock()
	current := s.sleeping && s.conn == ev.conn
	s.mu.Unlock()
	if !current {
		return // woken up or cancelled meanwhile, a retransmission, or stale
	}

	s.log(fmt.Sprintf("Incoming connection (%s): waking the server up…", ev.from))
	if s.d.TestWakeDelay > 0 {
		s.log(fmt.Sprintf("Test: start delayed by %s.", s.d.TestWakeDelay))
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.d.TestWakeDelay):
		}
	}
	if err := s.d.Start(ctx); err != nil {
		s.log("Wake-up failed: " + err.Error())
		s.mu.Lock()
		s.handoff = false
		s.closeLocked("")
		s.sleeping = true
		s.listenAt = s.d.Now().Add(retryDelay)
		s.mu.Unlock()
	}
}

// Socket families as reported by the bind hook (Linux values).
const (
	afInet  = 2
	afInet6 = 10
)

// bindRequest is a bind of the game port by the server, reported by the hook.
type bindRequest struct {
	domain    int  // socket family
	v6only    bool // IPv6 socket not receiving IPv4
	any       bool // wildcard address
	reuseAddr bool // SO_REUSEADDR set by the server
	reusePort bool // SO_REUSEPORT set by the server
}

func (r bindRequest) String() string {
	family := map[int]string{afInet: "IPv4", afInet6: "IPv6"}[r.domain]
	if family == "" {
		family = "unknown"
	}
	return fmt.Sprintf("family %s, v6only=%t, wildcard=%t, SO_REUSEADDR=%t, SO_REUSEPORT=%t",
		family, r.v6only, r.any, r.reuseAddr, r.reusePort)
}

// bindRequested is called by the hook when the server binds the game port.
// It hands the sentinel socket over through reply when possible and reports
// whether it did; otherwise the hook performs the real bind once the port
// is free.
func (s *Sleeper) bindRequested(req bindRequest, reply *net.UnixConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.handoff {
		return false // already handed over or released, or not sleeping
	}
	elapsed := s.d.Now().Sub(s.handoffAt).Round(100 * time.Millisecond)
	if s.keepFirst && reply != nil {
		switch {
		case req.domain == afInet && req.any:
			err := passSocket(reply, s.conn)
			if err == nil {
				s.handoff = false
				s.closeLocked(fmt.Sprintf("UDP port %d handed over to the server with the waiting connections (%s after the start).", s.connPort, elapsed))
				// The hook keeps the server from reading the socket until
				// it has booted: it would turn the players away before.
				s.openGateLocked("")
				_ = reply.SetDeadline(time.Time{})
				s.gate, s.gateAt = reply, s.d.Now()
				return true
			}
			s.log(fmt.Sprintf("Could not hand the socket over (%s): the port is simply released.", err))
		case req.domain == afInet6 && (req.v6only || req.reuseAddr || req.reusePort):
			// No conflict with the IPv4 sentinel, which shares the port
			// (SO_REUSEADDR and SO_REUSEPORT): keep holding it.
			return false
		}
	}
	s.handoff = false
	s.closeLocked(fmt.Sprintf("UDP port %d released to the server when it bound it (%s after the start; bind: %s).", s.connPort, elapsed, req))
	return false
}

// openGateLocked lets the server read the socket handed over, logging
// message if the gate was closed.
func (s *Sleeper) openGateLocked(message string) {
	if s.gate == nil {
		return
	}
	_ = s.gate.SetDeadline(time.Now().Add(hookTimeout))
	_, _ = s.gate.Write([]byte("go\n"))
	_ = s.gate.Close()
	s.gate = nil
	if message != "" {
		s.log(message)
	}
}

// closeLocked closes the sentinel, logging message and the connection
// attempts it received since the wake-up, if any.
func (s *Sleeper) closeLocked(message string) {
	if s.conn == nil {
		return
	}
	_ = s.conn.Close()
	s.conn, s.connPort = nil, 0
	if message != "" {
		s.log(message)
	}
	if report := s.reportLocked(); report != "" {
		s.log(report)
	}
	s.flows = nil
}

// reportLocked describes the connection attempts received by the sentinel.
func (s *Sleeper) reportLocked() string {
	if len(s.flows) == 0 {
		return ""
	}
	addrs := make([]string, 0, len(s.flows))
	for a := range s.flows {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool { return s.flows[addrs[i]].first.Before(s.flows[addrs[j]].first) })
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		f := s.flows[a]
		parts = append(parts, fmt.Sprintf("%s ×%d (from +%.1fs to +%.1fs)", a, f.packets,
			f.first.Sub(s.wokenAt).Seconds(), f.last.Sub(s.wokenAt).Seconds()))
	}
	return "Connection attempts received while asleep: " + strings.Join(parts, ", ")
}

func (s *Sleeper) log(message string) {
	if s.d.Log != nil {
		s.d.Log(message)
	}
}

// isQUICInitial recognises the datagram a client sends to open a QUIC
// connection: a long header packet (header form and fixed bits set), padded
// to at least 1200 bytes. Stray datagrams (scans, probes) are ignored.
func isQUICInitial(b []byte) bool {
	return len(b) >= minInitialSize && b[0]&0xC0 == 0xC0
}
