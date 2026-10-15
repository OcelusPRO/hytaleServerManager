// Package metricsstream integrates the optional MetricsStream game plugin.
//
// The plugin pushes metrics and gameplay events over HMSP (see internal/hmsp).
// It is opt-in: the plugin jar is only copied into the mods of the world
// being started when the operator enabled it, and removed again otherwise.
// While enabled, the manager listens for the stream on the loopback UDP port
// the plugin is configured to send to.
package metricsstream

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/hmsp"
)

// JarName is the file the plugin is installed under. Files with this name in
// a mods directory are managed here, not by the mods panel.
const JarName = "MetricsStream-1.0.0.jar"

// pluginDataDir is the plugin's own directory below mods/ (group_name).
const pluginDataDir = "Ocelus_MetricsStream"

// DefaultPort is the UDP port the manager listens on.
const DefaultPort = 9877

// connectedWindow is how long without a datagram before the stream counts
// as interrupted.
const connectedWindow = 15 * time.Second

//go:embed MetricsStream-1.0.0.jar
var pluginJar []byte

// Deps configures a Service.
type Deps struct {
	// SecretPath is where the shared HMAC secret is kept.
	SecretPath string
	Port       int
	Enabled    func() bool
	Logger     *slog.Logger
	// OnPlayer, optional, is told when a player connects or disconnects: the
	// plugin sees it from the inside, which is more reliable than console
	// output. Calls must be idempotent.
	OnPlayer func(joined bool, uuid, name string)
}

// Service installs the plugin and receives its stream.
type Service struct {
	deps  Deps
	store *Store

	mu         sync.Mutex
	conn       *net.UDPConn
	decoder    *hmsp.Decoder
	listenErr  string
	loaded     bool // the plugin was part of the last server launch
	lastReason string
}

// New returns a service; call Reconcile to start listening.
func New(d Deps) *Service {
	if d.Port == 0 {
		d.Port = DefaultPort
	}
	return &Service{deps: d, store: NewStore()}
}

// Store gives access to the received data.
func (s *Service) Store() *Store { return s.store }

// Reconcile starts or stops the receiver so that it matches the setting.
func (s *Service) Reconcile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch enabled := s.deps.Enabled(); {
	case enabled && s.conn == nil:
		s.listen()
	case !enabled && s.conn != nil:
		s.conn.Close()
		s.conn, s.decoder = nil, nil
		s.listenErr = ""
		s.store.Reset()
	case !enabled:
		s.listenErr = ""
	}
}

// Close stops the receiver.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn, s.decoder = nil, nil
	}
}

func (s *Service) listen() {
	secret, err := s.secret()
	if err != nil {
		s.listenErr = err.Error()
		s.deps.Logger.Warn("metrics stream: no secret", "error", err)
		return
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: s.deps.Port})
	if err != nil {
		s.listenErr = err.Error()
		s.deps.Logger.Warn("metrics stream: cannot listen", "port", s.deps.Port, "error", err)
		return
	}
	s.listenErr = ""
	s.conn = conn
	s.decoder = hmsp.NewDecoder(secret)
	go s.receive(conn, s.decoder)
}

func (s *Service) receive(conn *net.UDPConn, decoder *hmsp.Decoder) {
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.deps.Logger.Warn("metrics stream: read failed", "error", err)
			continue
		}
		msg, err := decoder.Decode(buf[:n])
		if err != nil {
			s.noteProblem(err)
			continue
		}
		if msg == nil {
			continue // fragment of a larger message
		}
		records, err := hmsp.DecodeRecords(msg)
		if err != nil {
			s.noteProblem(err)
		}
		s.store.Add(records)
		s.notifyPlayers(records)
	}
}

// noteProblem logs a rejected datagram, at most once per minute.
func (s *Service) noteProblem(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reason := err.Error()
	if reason == s.lastReason {
		return
	}
	s.lastReason = reason
	s.deps.Logger.Warn("metrics stream: datagram rejected", "error", reason)
	time.AfterFunc(time.Minute, func() {
		s.mu.Lock()
		s.lastReason = ""
		s.mu.Unlock()
	})
}

// secret returns the shared HMAC secret, creating it on first use.
func (s *Service) secret() (string, error) {
	if data, err := os.ReadFile(s.deps.SecretPath); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v, nil
		}
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)
	if err := fsutil.WriteFileAtomic(s.deps.SecretPath, []byte(secret+"\n"), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

// Prepare is called right before the server starts in saveDir: it installs
// the plugin (and its configuration) when the stream is enabled, and removes
// it otherwise. It also starts from an empty state, as a new server process
// starts a new stream.
func (s *Service) Prepare(saveDir string) error {
	s.store.Reset()
	enabled := s.deps.Enabled()
	s.mu.Lock()
	s.loaded = enabled
	s.mu.Unlock()
	if !enabled {
		return removePlugin(saveDir)
	}
	s.Reconcile()
	secret, err := s.secret()
	if err != nil {
		return err
	}
	return installPlugin(saveDir, s.deps.Port, secret)
}

func installPlugin(saveDir string, port int, secret string) error {
	modsDir := filepath.Join(saveDir, "mods")
	dataDir := filepath.Join(modsDir, pluginDataDir)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	jar := filepath.Join(modsDir, JarName)
	if existing, err := os.ReadFile(jar); err != nil || string(existing) != string(pluginJar) {
		if err := fsutil.WriteFileAtomic(jar, pluginJar, 0o640); err != nil {
			return fmt.Errorf("install the metrics plugin: %w", err)
		}
	}
	// A copy disabled through the mods panel would load twice otherwise.
	_ = os.Remove(filepath.Join(saveDir, "mods-disabled", JarName))
	config, err := json.MarshalIndent(pluginConfig(port, secret), "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dataDir, "config.json"), config, 0o600)
}

func removePlugin(saveDir string) error {
	modsDir := filepath.Join(saveDir, "mods")
	err := os.Remove(filepath.Join(modsDir, JarName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove the metrics plugin: %w", err)
	}
	// Its configuration holds the stream secret.
	return os.RemoveAll(filepath.Join(modsDir, pluginDataDir))
}

// pluginConfig is the plugin's config.json (see StreamConfig of the plugin).
// Everything not set keeps the plugin's default.
func pluginConfig(port int, secret string) map[string]any {
	return map[string]any{
		"Targets":         []string{fmt.Sprintf("udp://127.0.0.1:%d", port)},
		"ServerKey":       "hytale",
		"SecretKey":       secret,
		"Compression":     true,
		"DisabledEvents":  []string{"BLOCK_DAMAGE"},
		"StatsIntervalMs": 5000,
		// The full metrics dump is tens of MB once fragmented: it overflows a UDP
		// receive buffer and is lost. The live readings cover what the manager shows.
		"RawDumpIntervalMs": 0,
	}
}

// Status is the state of the integration, as shown by the interface.
type Status struct {
	Enabled bool `json:"enabled"`
	// Listening reports whether the manager is receiving on Port.
	Listening bool   `json:"listening"`
	Port      int    `json:"port"`
	Error     string `json:"error,omitempty"`
	// Loaded reports whether the plugin was part of the last server launch;
	// when it differs from Enabled, the server needs a restart.
	Loaded bool `json:"loaded"`
	// Connected is true while datagrams keep arriving.
	Connected bool   `json:"connected"`
	LastSeen  int64  `json:"lastSeenMs,omitempty"`
	Records   uint64 `json:"records"`
	hmsp.DecoderStats
}

// Overview combines the status and the live state.
type Overview struct {
	Status
	Live
}

// Overview returns what the interface displays.
func (s *Service) Overview() Overview {
	s.mu.Lock()
	st := Status{
		Enabled:   s.deps.Enabled(),
		Listening: s.conn != nil,
		Port:      s.deps.Port,
		Error:     s.listenErr,
		Loaded:    s.loaded,
	}
	if s.decoder != nil {
		st.DecoderStats = s.decoder.Stats()
	}
	s.mu.Unlock()
	records, last := s.store.Activity()
	st.Records = records
	if !last.IsZero() {
		st.LastSeen = last.UnixMilli()
		st.Connected = time.Since(last) < connectedWindow
	}
	return Overview{Status: st, Live: s.store.Live()}
}

func sortByName(list []map[string]any, key string) {
	slices.SortFunc(list, func(a, b map[string]any) int {
		x, _ := a[key].(string)
		y, _ := b[key].(string)
		return strings.Compare(strings.ToLower(x), strings.ToLower(y))
	})
}

func (s *Service) notifyPlayers(records []hmsp.Record) {
	if s.deps.OnPlayer == nil {
		return
	}
	for _, r := range records {
		if r.Type != "PLAYER_JOIN" && r.Type != "PLAYER_QUIT" {
			continue
		}
		uuid, _ := r.Data["uuid"].(string)
		name, _ := r.Data["name"].(string)
		if uuid != "" && name != "" {
			s.deps.OnPlayer(r.Type == "PLAYER_JOIN", uuid, name)
		}
	}
}

// Sample is the condensed reading of the plugin used by the resource history.
type Sample struct {
	TPS, MSPT, MSPTMax float64
	HeapMB             float64
	Players            int
}

// Sample returns the latest reading, or false while the plugin is not
// connected (disabled, server stopped or stream interrupted).
func (s *Service) Sample() (Sample, bool) {
	if _, last := s.store.Activity(); last.IsZero() || time.Since(last) >= connectedWindow {
		return Sample{}, false
	}
	live := s.store.Live()
	if live.Server == nil {
		return Sample{}, false
	}
	out := Sample{Players: int(num(live.Server["online_players"]))}
	for _, w := range live.Worlds {
		tps, _ := w["tps"].(map[string]any)
		mspt, _ := w["mspt"].(map[string]any)
		out.TPS += num(tps["10s"])
		out.MSPT += num(mspt["avg_10s"])
		out.MSPTMax = max(out.MSPTMax, num(mspt["max_10s"]))
	}
	if n := float64(len(live.Worlds)); n > 0 {
		out.TPS /= n
		out.MSPT /= n
	}
	if live.JVM != nil {
		out.HeapMB = num(live.JVM["heap_used"]) / (1 << 20)
	}
	return out, len(live.Worlds) > 0
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case uint64:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
