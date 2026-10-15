package metricsstream

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/hmsp"
)

func newTestService(t *testing.T, enabled *bool, port int) *Service {
	t.Helper()
	return New(Deps{
		SecretPath: filepath.Join(t.TempDir(), "key"),
		Port:       port,
		Enabled:    func() bool { return *enabled },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestPrepareIsOptIn(t *testing.T) {
	enabled := false
	s := newTestService(t, &enabled, 0)
	save := t.TempDir()
	jar := filepath.Join(save, "mods", JarName)
	config := filepath.Join(save, "mods", pluginDataDir, "config.json")

	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	if fsutil.Exists(jar) || fsutil.Exists(filepath.Join(save, "mods")) {
		t.Fatal("the plugin was installed although the stream is disabled")
	}

	enabled = true
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if data, err := os.ReadFile(jar); err != nil || len(data) != len(pluginJar) {
		t.Fatalf("jar not installed: %v", err)
	}
	var cfg map[string]any
	raw, err := os.ReadFile(config)
	if err != nil || json.Unmarshal(raw, &cfg) != nil {
		t.Fatalf("config not written: %v", err)
	}
	secret, _ := s.secret()
	if cfg["SecretKey"] != secret || cfg["Targets"].([]any)[0] != "udp://127.0.0.1:9877" {
		t.Errorf("unexpected config: %v", cfg)
	}
	if !s.Overview().Loaded {
		t.Error("Loaded should be true after launching with the plugin")
	}

	enabled = false
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	if fsutil.Exists(jar) || fsutil.Exists(config) {
		t.Error("the plugin was not removed after opting out")
	}
}

func TestReceivesStream(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skip("no loopback UDP:", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	enabled := true
	s := newTestService(t, &enabled, port)
	s.Reconcile()
	defer s.Close()
	if !s.Overview().Listening {
		t.Fatalf("not listening: %s", s.Overview().Error)
	}
	secret, _ := s.secret()

	// SERVER record: uptime 5 s, 1 player, 10 slots, 1 world, 2 plugins, no whitelist, RUNNING.
	body := []byte{0x02, 7, 0xA8, 0x27, 1, 20, 1, 2, 0, 1}
	body[1] = byte(len(body) - 2)
	var dg []byte
	dg = binary.BigEndian.AppendUint16(dg, 0x484D)
	dg = append(dg, 1, 4)
	dg = binary.BigEndian.AppendUint32(dg, 1)
	dg = binary.BigEndian.AppendUint32(dg, 1)
	dg = binary.BigEndian.AppendUint64(dg, uint64(time.Now().UnixMilli()))
	dg = append(dg, body...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(dg)
	dg = append(dg, mac.Sum(nil)[:8]...)

	conn, err := net.Dial("udp4", net.JoinHostPort("127.0.0.1", itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("garbage"))
	time.Sleep(100 * time.Millisecond) // some Windows loopback stacks drop back-to-back datagrams
	if _, err := conn.Write(dg); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		o := s.Overview()
		if o.Server != nil {
			if o.Server["state"] != "RUNNING" || o.Server["online_players"] != uint64(1) || !o.Connected || o.Rejected != 1 {
				t.Fatalf("unexpected overview: %+v", o)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stream not received: %+v", s.Overview().Status)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSampleAndPlayerEvents(t *testing.T) {
	enabled := true
	s := newTestService(t, &enabled, 0)
	var joined []string
	s.deps.OnPlayer = func(j bool, uuid, name string) {
		if j {
			joined = append(joined, name)
		} else {
			joined = append(joined, "-"+name)
		}
	}
	if _, ok := s.Sample(); ok {
		t.Fatal("sample without data")
	}
	records := []hmsp.Record{
		{Type: "SERVER", Kind: hmsp.KindSnapshot, Data: map[string]any{"online_players": uint64(2)}},
		{Type: "JVM", Kind: hmsp.KindSnapshot, Data: map[string]any{"heap_used": uint64(512 << 20)}},
		{Type: "WORLD", Kind: hmsp.KindSnapshot, Data: map[string]any{"name": "a",
			"tps": map[string]any{"10s": 20.0}, "mspt": map[string]any{"avg_10s": 5.0, "max_10s": 9.0}}},
		{Type: "WORLD", Kind: hmsp.KindSnapshot, Data: map[string]any{"name": "b",
			"tps": map[string]any{"10s": 10.0}, "mspt": map[string]any{"avg_10s": 15.0, "max_10s": 40.0}}},
		{Type: "PLAYER_JOIN", Kind: hmsp.KindEvent, Data: map[string]any{"uuid": "u1", "name": "Alice"}},
		{Type: "PLAYER_QUIT", Kind: hmsp.KindEvent, Data: map[string]any{"uuid": "u1", "name": "Alice"}},
		{Type: "PLAYER_JOIN", Kind: hmsp.KindEvent, Data: map[string]any{"uuid": "", "name": "Ghost"}},
	}
	s.store.Add(records)
	s.notifyPlayers(records)

	got, ok := s.Sample()
	if !ok || got.Players != 2 || got.TPS != 15 || got.MSPT != 10 || got.MSPTMax != 40 || got.HeapMB != 512 {
		t.Errorf("sample = %+v, %v", got, ok)
	}
	if len(joined) != 2 || joined[0] != "Alice" || joined[1] != "-Alice" {
		t.Errorf("player notifications = %v", joined)
	}
}
