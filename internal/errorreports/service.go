// Package errorreports receives the error reports of the game server.
//
// Since 0.7.3 the server sends its errors, and those of third-party plugins,
// to the Sentry project named by the SentryDsn key of its config.json. When
// the operator chooses to keep the reports in the manager, that key points
// to a small Sentry-compatible endpoint served here on the loopback
// interface: events are grouped into issues shown by the interface, and can
// also be relayed unchanged to a Sentry project of the operator.
package errorreports

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/fsutil"
)

// DefaultPort is the loopback TCP port of the endpoint.
const DefaultPort = 9878

const (
	// configKey is the key of the server's config.json naming the project.
	configKey = "SentryDsn"
	projectID = "1"

	maxBodyBytes = 20 << 20
	forwardQueue = 64
	forwardTime  = 15 * time.Second
)

// Deps configures a Service.
type Deps struct {
	// KeyPath is where the public key of the local DSN is kept.
	KeyPath string
	// DB is the manager database holding the issues.
	DB   *sql.DB
	Port int
	// Enabled reports whether the server should report to the manager.
	Enabled func() bool
	// ForwardDSN returns the DSN reports are relayed to ("" for none).
	ForwardDSN func() string
	Client     *http.Client
	Logger     *slog.Logger
}

// ForwardStatus describes the relay to the operator's Sentry project.
type ForwardStatus struct {
	Sent        uint64     `json:"sent"`
	Failed      uint64     `json:"failed"`
	Dropped     uint64     `json:"dropped"`
	LastSentAt  *time.Time `json:"lastSentAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	LastErrorAt *time.Time `json:"lastErrorAt,omitempty"`
}

// Status is the state of the integration, as shown by the interface.
type Status struct {
	Enabled bool `json:"enabled"`
	// Loaded reports whether the last server launch reported to the manager;
	// when it differs from Enabled, the server needs a restart.
	Loaded    bool   `json:"loaded"`
	Listening bool   `json:"listening"`
	Port      int    `json:"port"`
	Error     string `json:"error,omitempty"`
	Received  uint64 `json:"received"`
	Rejected  uint64 `json:"rejected"`
	// Forward is set while a relay DSN is configured.
	Forward *ForwardStatus `json:"forward,omitempty"`
}

// Overview combines the status and the issues.
type Overview struct {
	Status Status    `json:"status"`
	Issues []Summary `json:"issues"`
}

type forwardJob struct {
	dsn  DSN
	body []byte
}

// Service serves the endpoint and keeps the issues.
type Service struct {
	deps  Deps
	store *Store
	key   string
	queue chan forwardJob

	mu        sync.Mutex
	listening bool
	listenErr string
	loaded    bool
	plugins   pluginPackages // of the save last started
	launch    time.Time      // when the server was last started
	received  uint64
	rejected  uint64
	forward   ForwardStatus
}

// New loads the saved issues; call Run to serve the endpoint.
func New(d Deps) (*Service, error) {
	if d.Port == 0 {
		d.Port = DefaultPort
	}
	if d.Client == nil {
		d.Client = &http.Client{Timeout: forwardTime}
	}
	if d.ForwardDSN == nil {
		d.ForwardDSN = func() string { return "" }
	}
	key, err := loadKey(d.KeyPath)
	if err != nil {
		return nil, err
	}
	return &Service{deps: d, store: NewStore(d.DB), key: key, queue: make(chan forwardJob, forwardQueue)}, nil
}

// loadKey returns the public key of the local DSN, creating it on first use.
func loadKey(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw)
	if err := fsutil.WriteFileAtomic(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// Store gives access to the issues.
func (s *Service) Store() *Store { return s.store }

// DSN is the DSN given to the server.
func (s *Service) DSN() string {
	return fmt.Sprintf("http://%s@127.0.0.1:%d/%s", s.key, s.deps.Port, projectID)
}

// Run serves the endpoint until ctx ends, saving the issues periodically.
func (s *Service) Run(ctx context.Context) {
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.deps.Port)))
	s.mu.Lock()
	if err != nil {
		s.listenErr = err.Error()
		s.deps.Logger.Warn("error reports: cannot listen", "port", s.deps.Port, "error", err)
	} else {
		s.listening = true
	}
	s.mu.Unlock()

	var srv *http.Server
	if ln != nil {
		srv = &http.Server{
			Handler:           s.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       time.Minute,
			MaxHeaderBytes:    64 << 10,
		}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.deps.Logger.Warn("error reports: endpoint stopped", "error", err)
			}
		}()
	}
	go s.forwardLoop(ctx)
	<-ctx.Done()
	if srv != nil {
		srv.Close()
	}
}

// Handler is the Sentry-compatible endpoint.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/{project}/envelope/", s.handleEnvelope)
	mux.HandleFunc("POST /api/{project}/store/", s.handleStore)
	return mux
}

func (s *Service) handleEnvelope(w http.ResponseWriter, r *http.Request) {
	body, status := readBody(w, r)
	if status != 0 {
		s.reject(w, status)
		return
	}
	header, items, err := parseEnvelope(body)
	if err != nil {
		s.reject(w, http.StatusBadRequest)
		return
	}
	key := requestKey(r)
	if key == "" {
		if raw, _ := header["dsn"].(string); raw != "" {
			if dsn, err := ParseDSN(raw); err == nil {
				key = dsn.PublicKey
			}
		}
	}
	if !s.authorized(key) {
		s.reject(w, http.StatusUnauthorized)
		return
	}
	id, _ := header["event_id"].(string)
	for _, it := range items {
		if it.Type == "event" {
			if ev, ok := s.record(it.Payload); ok && ev.ID != "" {
				id = ev.ID
			}
		}
	}
	s.relay(body, header)
	writeID(w, id)
}

// handleStore accepts the legacy endpoint taking a bare event.
func (s *Service) handleStore(w http.ResponseWriter, r *http.Request) {
	body, status := readBody(w, r)
	if status != 0 {
		s.reject(w, status)
		return
	}
	if !s.authorized(requestKey(r)) {
		s.reject(w, http.StatusUnauthorized)
		return
	}
	ev, ok := s.record(body)
	if !ok {
		s.reject(w, http.StatusBadRequest)
		return
	}
	header := map[string]any{"event_id": ev.ID}
	line, _ := json.Marshal(map[string]any{"type": "event", "length": len(body)})
	envelope := append(append(append([]byte("{}\n"), line...), '\n'), body...)
	s.relay(envelope, header)
	writeID(w, ev.ID)
}

func (s *Service) record(payload []byte) (Event, bool) {
	ev, err := parseEvent(payload, time.Now())
	if err != nil {
		return Event{}, false
	}
	s.mu.Lock()
	ev.plugin = s.plugins.attribute(ev)
	ev.launch = s.launch
	s.received++
	s.mu.Unlock()
	if _, err := s.store.Add(ev); err != nil {
		s.deps.Logger.Warn("error reports: cannot record an event", "error", err)
	}
	return ev, true
}

func (s *Service) reject(w http.ResponseWriter, status int) {
	s.mu.Lock()
	s.rejected++
	s.mu.Unlock()
	http.Error(w, http.StatusText(status), status)
}

func (s *Service) authorized(key string) bool {
	return key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(s.key)) == 1
}

func writeID(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// requestKey reads the public key from the auth header or the query.
func requestKey(r *http.Request) string {
	if key := authKey(r.Header.Get("X-Sentry-Auth")); key != "" {
		return key
	}
	return r.URL.Query().Get("sentry_key")
}

// readBody returns the decompressed body, or the status to answer.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, int) {
	var in io.Reader = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(in)
		if err != nil {
			return nil, http.StatusBadRequest
		}
		defer gz.Close()
		in = gz
	case "deflate":
		zr, err := zlib.NewReader(in)
		if err != nil {
			return nil, http.StatusBadRequest
		}
		defer zr.Close()
		in = zr
	default:
		return nil, http.StatusUnsupportedMediaType
	}
	body, err := io.ReadAll(io.LimitReader(in, maxBodyBytes+1))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge) || len(body) > maxBodyBytes:
		return nil, http.StatusRequestEntityTooLarge
	case err != nil:
		return nil, http.StatusBadRequest
	}
	return body, 0
}

// relay queues the envelope for the operator's project, if one is set.
func (s *Service) relay(body []byte, header map[string]any) {
	raw := s.deps.ForwardDSN()
	if raw == "" {
		return
	}
	dsn, err := ParseDSN(raw)
	if err != nil || dsn.PublicKey == s.key { // never relay to ourselves
		return
	}
	header["dsn"] = dsn.Raw
	out, err := replaceHeader(body, header)
	if err != nil {
		return
	}
	select {
	case s.queue <- forwardJob{dsn: dsn, body: out}:
	default:
		s.mu.Lock()
		s.forward.Dropped++
		s.mu.Unlock()
	}
}

func (s *Service) forwardLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.queue:
			err := s.send(ctx, job)
			now := time.Now().UTC()
			s.mu.Lock()
			if err != nil {
				s.forward.Failed++
				s.forward.LastError, s.forward.LastErrorAt = err.Error(), &now
			} else {
				s.forward.Sent++
				s.forward.LastSentAt = &now
			}
			s.mu.Unlock()
		}
	}
}

func (s *Service) send(ctx context.Context, job forwardJob) error {
	ctx, cancel := context.WithTimeout(ctx, forwardTime)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.dsn.Envelope, bytes.NewReader(job.body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", job.dsn.authHeader())
	resp, err := s.deps.Client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // without the URL, which holds the key
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Sentry answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// Prepare is called right before the server starts in saveDir: it points
// SentryDsn of config.json to the manager when enabled, and removes it
// otherwise if it is the manager's (a DSN set by hand is left alone).
func (s *Service) Prepare(saveDir string) error {
	enabled := s.deps.Enabled()
	plugins := scanPlugins(saveDir)
	s.mu.Lock()
	s.loaded = enabled
	s.plugins = plugins
	s.launch = time.Now().UTC()
	s.mu.Unlock()
	return editConfig(filepath.Join(saveDir, "config.json"), enabled, func(cfg map[string]any) bool {
		current, _ := cfg[configKey].(string)
		if enabled {
			if current == s.DSN() {
				return false
			}
			cfg[configKey] = s.DSN()
			return true
		}
		if dsn, err := ParseDSN(current); err == nil && dsn.PublicKey == s.key {
			delete(cfg, configKey)
			return true
		}
		return false
	})
}

// editConfig applies mutate to the JSON object in path, preserving the
// other values exactly. A missing file is only created when create is set.
func editConfig(path string, create bool, mutate func(map[string]any) bool) error {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist) && create:
		data = []byte("{}")
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cfg map[string]any
	if err := dec.Decode(&cfg); err != nil || cfg == nil {
		return fmt.Errorf("%s is unreadable", filepath.Base(path))
	}
	if !mutate(cfg) {
		return nil
	}
	return fsutil.WriteJSONAtomic(path, cfg, 0o640)
}

// Overview returns what the interface displays.
func (s *Service) Overview() Overview {
	s.mu.Lock()
	st := Status{
		Enabled:   s.deps.Enabled(),
		Loaded:    s.loaded,
		Listening: s.listening,
		Port:      s.deps.Port,
		Error:     s.listenErr,
		Received:  s.received,
		Rejected:  s.rejected,
	}
	if s.deps.ForwardDSN() != "" {
		fwd := s.forward
		st.Forward = &fwd
	}
	s.mu.Unlock()
	issues, err := s.store.List()
	if err != nil {
		s.deps.Logger.Warn("error reports: cannot list", "error", err)
		issues = []Summary{}
	}
	return Overview{Status: st, Issues: issues}
}
