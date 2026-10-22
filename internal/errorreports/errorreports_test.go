package errorreports

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hytale-server-manager/internal/database"
)

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *Store {
	return NewStore(openDB(t, filepath.Join(t.TempDir(), "manager.db")))
}

func mustAdd(t *testing.T, s *Store, ev Event) string {
	t.Helper()
	id, err := s.Add(ev)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustGet(t *testing.T, s *Store, id string) Issue {
	t.Helper()
	issue, ok, err := s.Get(id)
	if err != nil || !ok {
		t.Fatalf("issue %s: found %v, %v", id, ok, err)
	}
	return issue
}

func newTestService(t *testing.T, enabled *bool, forward *string) *Service {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Deps{
		KeyPath:    filepath.Join(dir, "key"),
		DB:         openDB(t, filepath.Join(dir, "manager.db")),
		Enabled:    func() bool { return *enabled },
		ForwardDSN: func() string { return *forward },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// javaEvent is shaped like the events of the 0.7.3 server: a plugin logs
// through "<Name>|P", no tag names it and no frame is flagged in_app.
func javaEvent(id, exType string, line int, plugin string) []byte {
	ev := map[string]any{
		"event_id":  id,
		"timestamp": "2026-10-21T18:03:12.123Z",
		"level":     "error",
		"logger":    plugin + "|P",
		"platform":  "java",
		"release":   "0.7.3",
		"tags":      map[string]any{"has-plugins": "true", "has-packs": "false", "patchline": "release"},
		"exception": map[string]any{"values": []any{map[string]any{
			"type":   exType,
			"value":  "Cannot invoke \"Foo.bar()\"",
			"module": "java.lang",
			"stacktrace": map[string]any{"frames": []any{
				map[string]any{"module": "java.lang.Thread", "function": "run", "lineno": 1583},
				map[string]any{"module": "com.hypixel.hytale.server.core.plugin.PluginBase", "function": "setup0", "lineno": 90},
				map[string]any{"module": "com.example.myplugin.Listener", "function": "onJoin", "lineno": line},
				map[string]any{"module": "java.util.ImmutableCollections", "function": "get", "lineno": 12},
			}},
		}}},
	}
	b, _ := json.Marshal(ev)
	return b
}

func envelope(dsn string, events ...[]byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `{"event_id":"e0","dsn":%q,"sent_at":"2026-10-21T18:03:12Z"}`+"\n", dsn)
	for i, ev := range events {
		if i%2 == 0 { // both item forms: with and without a length
			fmt.Fprintf(&b, `{"type":"event","length":%d,"content_type":"application/json"}`+"\n", len(ev))
		} else {
			b.WriteString(`{"type":"event"}` + "\n")
		}
		b.Write(ev)
		b.WriteByte('\n')
	}
	b.WriteString(`{"type":"client_report"}` + "\n" + `{"discarded_events":[]}`)
	return b.Bytes()
}

func post(t *testing.T, s *Service, body []byte, key string) *httptest.ResponseRecorder {
	t.Helper()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(body)
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/1/envelope/", &gz)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	if key != "" {
		req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7,sentry_client=sentry.java/8.0.0,sentry_key="+key)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestEnvelopesAreGroupedIntoIssues(t *testing.T) {
	enabled, forward := true, ""
	s := newTestService(t, &enabled, &forward)

	body := envelope(s.DSN(),
		javaEvent("a1", "NullPointerException", 42, "MyPlugin"),
		javaEvent("a2", "NullPointerException", 43, "MyPlugin"), // another build: same issue
		javaEvent("b1", "IllegalStateException", 42, "MyPlugin"),
	)
	if rec := post(t, s, body, s.key); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	issues := s.Overview().Issues
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2: %+v", len(issues), issues)
	}
	var npe Summary
	for _, i := range issues {
		if strings.HasPrefix(i.Title, "NullPointerException") {
			npe = i
		}
	}
	if npe.Count != 2 || npe.Plugin != "MyPlugin" || npe.Culprit != "com.example.myplugin.Listener.onJoin" {
		t.Errorf("unexpected issue %+v", npe)
	}
	if npe.Title != `NullPointerException: Cannot invoke "Foo.bar()"` {
		t.Errorf("title %q", npe.Title)
	}
	issue := mustGet(t, s.Store(), npe.ID)
	frames := issue.Last.Exceptions[0].Frames
	if frames[0].InApp || frames[1].InApp || !frames[2].InApp || frames[3].InApp {
		t.Errorf("plugin frames not told apart: %+v", frames)
	}
	if issue.Last.ID != "a2" || issue.Last.Release != "0.7.3" || len(issue.Last.Raw) == 0 {
		t.Errorf("unexpected last event %+v", issue.Last)
	}
	if got := issue.Last.Time; !got.Equal(time.Date(2026, 10, 21, 18, 3, 12, 123e6, time.UTC)) {
		t.Errorf("time %v", got)
	}
}

func TestEnvelopeKeyIsChecked(t *testing.T) {
	enabled, forward := true, ""
	s := newTestService(t, &enabled, &forward)
	ev := javaEvent("a1", "NullPointerException", 1, "")

	if rec := post(t, s, envelope("", ev), "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status %d", rec.Code)
	}
	if rec := post(t, s, envelope("", ev), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no key: status %d", rec.Code)
	}
	if len(s.Overview().Issues) != 0 {
		t.Fatal("an unauthorised event was recorded")
	}
	// The DSN of the envelope header is accepted as credentials.
	if rec := post(t, s, envelope(s.DSN(), ev), ""); rec.Code != http.StatusOK {
		t.Errorf("header DSN: status %d", rec.Code)
	}
}

func TestReportsAreRelayed(t *testing.T) {
	type received struct {
		path, auth string
		body       []byte
	}
	got := make(chan received, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{r.URL.Path, r.Header.Get("X-Sentry-Auth"), body}
	}))
	defer upstream.Close()

	enabled := true
	forward := strings.Replace(upstream.URL, "http://", "http://pub123@", 1) + "/sentry/42"
	s := newTestService(t, &enabled, &forward)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.forwardLoop(ctx)

	ev := javaEvent("a1", "NullPointerException", 1, "")
	post(t, s, envelope(s.DSN(), ev), s.key)

	select {
	case r := <-got:
		if r.path != "/sentry/api/42/envelope/" || authKey(r.auth) != "pub123" {
			t.Errorf("relayed to %s with %q", r.path, r.auth)
		}
		header, items, err := parseEnvelope(r.body)
		if err != nil || header["dsn"] != forward || len(items) != 2 || !bytes.Equal(items[0].Payload, ev) {
			t.Errorf("relayed envelope %v %v %+v", err, header, items)
		}
		if strings.Contains(string(r.body), s.key) {
			t.Error("the local key was relayed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing relayed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.Overview().Status.Forward.Sent != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fwd := s.Overview().Status.Forward; fwd.Sent != 1 || fwd.Failed != 0 {
		t.Errorf("forward status %+v", fwd)
	}
}

func TestPrepareEditsSentryDsn(t *testing.T) {
	enabled, forward := false, ""
	s := newTestService(t, &enabled, &forward)
	save := t.TempDir()
	config := filepath.Join(save, "config.json")

	// Disabled without config: nothing is created.
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); err == nil {
		t.Fatal("config.json created while disabled")
	}

	os.WriteFile(config, []byte(`{"ServerName":"A","Big":9007199254740993}`), 0o640)
	enabled = true
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(config)
	if !strings.Contains(string(data), `"SentryDsn": "`+s.DSN()+`"`) || !strings.Contains(string(data), "9007199254740993") {
		t.Fatalf("config.json: %s", data)
	}
	if !s.Overview().Status.Loaded {
		t.Error("not reported as loaded")
	}

	enabled = false
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(config)
	if strings.Contains(string(data), "SentryDsn") {
		t.Fatalf("the manager DSN was kept: %s", data)
	}

	// A DSN set by hand is left alone.
	os.WriteFile(config, []byte(`{"SentryDsn":"https://abc@o1.ingest.sentry.io/7"}`), 0o640)
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(config)
	if !strings.Contains(string(data), "o1.ingest.sentry.io") {
		t.Fatalf("hand-set DSN removed: %s", data)
	}
}

func TestStorePersistsAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	ev, err := parseEvent(javaEvent("a1", "NullPointerException", 1, ""), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := mustAdd(t, store, ev)
	if ok, err := store.SetResolved(id, true); !ok || err != nil {
		t.Fatalf("resolve: %v %v", ok, err)
	}
	db.Close()

	reopened := NewStore(openDB(t, path))
	if issue := mustGet(t, reopened, id); !issue.Resolved || issue.Count != 1 || issue.Last.ID != "a1" || len(issue.Occurrences) != 1 {
		t.Fatalf("reopened issue %+v", issue)
	}
	mustAdd(t, reopened, ev) // a regression reopens the issue
	if issue := mustGet(t, reopened, id); issue.Resolved || issue.Count != 2 {
		t.Fatalf("after a new occurrence %+v", issue.Summary)
	}
	if ok, _ := reopened.Delete(id); !ok {
		t.Fatal("not deleted")
	}
	if _, ok, _ := reopened.Get(id); ok {
		t.Fatal("still there after delete")
	}
}

func TestLegacyJSONIsImported(t *testing.T) {
	store := newTestStore(t)
	path := filepath.Join(t.TempDir(), "error-reports.json")
	legacy := `{"issues":[{"id":"abc","title":"NullPointerException: x","plugin":"Shop","level":"error","count":7,
		"firstSeen":"2026-10-22T13:00:00Z","lastSeen":"2026-10-22T14:00:00Z","resolved":true,
		"last":{"id":"e7","time":"2026-10-22T14:00:00Z","level":"error"},
		"occurrences":[{"time":"2026-10-22T13:59:59.5Z","eventId":"e6"},{"time":"2026-10-22T14:00:00Z","eventId":"e7"}]}]}`
	os.WriteFile(path, []byte(legacy), 0o600)

	n, err := store.ImportJSON(path)
	if err != nil || n != 1 {
		t.Fatalf("imported %d, %v", n, err)
	}
	issue := mustGet(t, store, "abc")
	if issue.Count != 7 || !issue.Resolved || issue.Plugin != "Shop" || len(issue.Occurrences) != 2 || issue.Last.ID != "e7" {
		t.Fatalf("imported issue %+v", issue)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the JSON file was not renamed")
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Error(err)
	}
	if n, err := store.ImportJSON(path); n != 0 || err != nil { // nothing left to import
		t.Errorf("second import %d, %v", n, err)
	}
}

func TestParseDSN(t *testing.T) {
	d, err := ParseDSN("https://pub:sec@o1.ingest.sentry.io/prefix/123")
	if err != nil || d.PublicKey != "pub" || d.SecretKey != "sec" || d.Envelope != "https://o1.ingest.sentry.io/prefix/api/123/envelope/" {
		t.Errorf("got %+v, %v", d, err)
	}
	for _, bad := range []string{"", "o1.ingest.sentry.io/1", "https://o1.ingest.sentry.io/1", "ftp://k@h/1", "https://k@h/", "https://k@h/1?x=1"} {
		if _, err := ParseDSN(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMessageEventsWithoutException(t *testing.T) {
	payload := []byte(`{"event_id":"m1","timestamp":1761069792.5,"logger":"x","logentry":{"message":"Failed to load %s","formatted":"Failed to load Foo"},"tags":[["has-plugins","true"]]}`)
	ev, err := parseEvent(payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Title() != "Failed to load Foo" || ev.Level != "error" || ev.Plugin() != "" || ev.Time.Unix() != 1761069792 {
		t.Errorf("unexpected event %+v", ev)
	}
}

func writePluginJar(t *testing.T, path, manifest string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(manifest))
	zw.Close()
	f.Close()
}

func TestListenerErrorsAreAttributedToTheirPlugin(t *testing.T) {
	enabled, forward := true, ""
	s := newTestService(t, &enabled, &forward)
	save := t.TempDir()
	os.MkdirAll(filepath.Join(save, "mods"), 0o750)
	writePluginJar(t, filepath.Join(save, "mods", "Shop.jar"), `{"Group":"Acme","Name":"Shop","Main":"com.example.shop.ShopPlugin"}`)
	writePluginJar(t, filepath.Join(save, "mods", "ShopAddon.jar"), `{"Group":"Acme","Name":"ShopAddon","Main":"com.example.shop.addon.AddonPlugin"}`)
	if err := s.Prepare(save); err != nil {
		t.Fatal(err)
	}

	// Reported by the event bus of the server, as in 0.7.3.
	payload := []byte(`{"event_id":"l1","logger":"EventBus","exception":{"values":[{"type":"NullPointerException","module":"java.lang","stacktrace":{"frames":[
		{"module":"com.hypixel.hytale.event.SyncEventBusRegistry","function":"dispatchEventMap"},
		{"module":"com.example.shop.addon.AddonPlugin","function":"onBoot"},
		{"module":"java.util.Objects","function":"requireNonNull"}]}}]}}`)
	ev, ok := s.record(payload)
	if !ok {
		t.Fatal("event rejected")
	}
	issue := mustGet(t, s.Store(), ev.Fingerprint())
	if issue.Plugin != "ShopAddon" {
		t.Errorf("plugin %q, want ShopAddon", issue.Plugin)
	}
}

func TestResolvedIssueRegresses(t *testing.T) {
	store := newTestStore(t)
	ev, _ := parseEvent(javaEvent("a1", "NullPointerException", 1, "Shop"), time.Now())
	id := mustAdd(t, store, ev)
	store.SetResolved(id, true)
	if issue := mustGet(t, store, id); !issue.Resolved || issue.ResolvedAt == nil || issue.Regressed {
		t.Fatalf("after resolving %+v", issue.Summary)
	}

	mustAdd(t, store, ev)
	if issue := mustGet(t, store, id); issue.Resolved || !issue.Regressed || issue.Count != 2 || issue.ResolvedAt == nil {
		t.Fatalf("after a new occurrence %+v", issue.Summary)
	}
	mustAdd(t, store, ev) // stays regressed until resolved again
	if issue := mustGet(t, store, id); !issue.Regressed {
		t.Fatal("regression forgotten")
	}
	store.SetResolved(id, true)
	if issue := mustGet(t, store, id); issue.Regressed {
		t.Fatal("still regressed once resolved")
	}
	if ok, err := store.SetResolved("unknown", true); ok || err != nil {
		t.Errorf("unknown issue: %v %v", ok, err)
	}
}

func TestOccurrencesAreKept(t *testing.T) {
	enabled, forward := true, ""
	s := newTestService(t, &enabled, &forward)
	if err := s.Prepare(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	launch := s.launch.Truncate(time.Millisecond) // stored to the millisecond

	var id string
	for i := range maxOccurrences + 5 {
		payload := fmt.Appendf(nil, `{"event_id":"e%d","timestamp":%d.25,"release":"0.7.3","exception":{"values":[{"type":"IllegalStateException","value":"port %d"}]}}`, i, 1761069792+i, i)
		ev, ok := s.record(payload)
		if !ok {
			t.Fatal("event rejected")
		}
		id = ev.Fingerprint()
	}
	issue := mustGet(t, s.Store(), id)
	if issue.Count != maxOccurrences+5 || len(issue.Occurrences) != maxOccurrences {
		t.Fatalf("count %d, history %d", issue.Count, len(issue.Occurrences))
	}
	first, last := issue.Occurrences[0], issue.Occurrences[maxOccurrences-1]
	if first.EventID != "e5" || last.EventID != fmt.Sprintf("e%d", maxOccurrences+4) {
		t.Errorf("history kept %s..%s, want the latest", first.EventID, last.EventID)
	}
	if last.Message != fmt.Sprintf("port %d", maxOccurrences+4) || last.Release != "0.7.3" {
		t.Errorf("occurrence %+v", last)
	}
	if want := time.Unix(1761069792+5, 25e7).UTC(); !first.Time.Equal(want) {
		t.Errorf("time %v, want %v", first.Time, want)
	}
	if last.Launch == nil || !last.Launch.Equal(launch) {
		t.Errorf("launch %v, want %v", last.Launch, launch)
	}
}
