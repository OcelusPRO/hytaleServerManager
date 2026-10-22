package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/auth"
	"hytale-server-manager/internal/config"
	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/hytaleapi"
	"hytale-server-manager/internal/hytaleauth"
	"hytale-server-manager/internal/install"
	"hytale-server-manager/internal/lifecycle"
	"hytale-server-manager/internal/players"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/secretbox"
	"hytale-server-manager/internal/settings"
)

const testPassword = "test-password-123"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	dir := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tmp, savesDir := dir("tmp"), dir("saves")

	creds, err := auth.OpenCredentialStore(filepath.Join(root, "creds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Set(testPassword); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.LoadOrCreate(filepath.Join(root, "key"))
	if err != nil {
		t.Fatal(err)
	}
	hytale, err := hytaleauth.NewService(
		hytaleapi.NewClient(http.DefaultClient, hytaleapi.DefaultEndpoints()),
		hytaleauth.NewTokenStore(filepath.Join(root, "auth.enc"), box))
	if err != nil {
		t.Fatal(err)
	}
	settingsStore, err := settings.Open(filepath.Join(root, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	console := gameserver.NewConsole(100)
	process := gameserver.NewProcess(console)
	fetcher := install.NewFetcher(http.DefaultClient)
	api := hytaleapi.NewClient(http.DefaultClient, hytaleapi.DefaultEndpoints())
	provisioner := install.NewProvisioner(install.ProvisionerDeps{
		Java:          install.NewJavaRuntime(dir("java"), tmp, fetcher),
		Game:          install.NewGameInstall(dir("game"), tmp, api, hytale.AccessToken, fetcher),
		AccountReady:  hytale.Ready,
		ServerRunning: process.Active,
		Patchline:     func() string { return "release" },
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	saveStore := saves.NewStore(savesDir, tmp)

	handler := NewHandler(Deps{
		Config:         config.Config{MaxUploadBytes: 1 << 20},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Credentials:    creds,
		PasswordNotice: auth.NewInitialPasswordNotice(filepath.Join(root, "notice")),
		Sessions:       auth.NewSessionManager(time.Hour, time.Hour),
		LoginLimiter:   auth.NewLoginLimiter(5, time.Minute, time.Minute),
		HytaleAuth:     hytale,
		Provisioner:    provisioner,
		Controller: lifecycle.New(lifecycle.Deps{Process: process, Console: console, Settings: settingsStore,
			Saves: saveStore, Install: provisioner, Auth: hytale, HomeDir: root}),
		Console:  console,
		Players:  players.NewTracker(players.NewNameDirectory()),
		Saves:    saveStore,
		Mods:     saves.NewModStore(saveStore),
		Backups:  saves.NewBackupStore(dir("backups"), tmp, saveStore, archive.Limits{}),
		Settings: settingsStore,
		TmpDir:   tmp,
		Static: fstest.MapFS{
			"index.html":    {Data: []byte("<!doctype html>")},
			"assets/app.js": {Data: []byte("console.log(1)")},
		},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

type client struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

func (c *client) do(method, path, body string, headers map[string]string) *http.Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

var ajax = map[string]string{"X-Requested-With": "hsm"}

func expect(t *testing.T, resp *http.Response, status int, what string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("%s: got HTTP %d, want %d", what, resp.StatusCode, status)
	}
}

func TestAuthenticationAndCSRF(t *testing.T) {
	srv := newTestServer(t)
	c := &client{t: t, base: srv.URL}
	login := `{"password":"` + testPassword + `"}`

	expect(t, c.do("GET", "/api/server/status", "", nil), 401, "anonymous API access")
	expect(t, c.do("POST", "/api/login", login, nil), 403, "login without CSRF header")
	expect(t, c.do("POST", "/api/login", `{"password":"nope"}`, ajax), 401, "wrong password")

	resp := c.do("POST", "/api/login", login, ajax)
	expect(t, resp, 204, "valid login")
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = ck
			if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
				t.Error("session cookie must be HttpOnly and SameSite=Strict")
			}
		}
	}
	if c.cookie == nil {
		t.Fatal("no session cookie")
	}

	expect(t, c.do("GET", "/api/server/status", "", nil), 200, "authenticated status")
	expect(t, c.do("POST", "/api/saves", `{"name":"World"}`, nil), 403, "mutation without CSRF header")
	crossSite := map[string]string{"X-Requested-With": "hsm", "Origin": "https://evil.example"}
	expect(t, c.do("POST", "/api/saves", `{"name":"World"}`, crossSite), 403, "cross-origin mutation")
	expect(t, c.do("POST", "/api/saves", `{"name":"World"}`, ajax), 201, "create save")
	expect(t, c.do("POST", "/api/server/start", "", ajax), 409, "start without installation")

	expect(t, c.do("POST", "/api/logout", "", ajax), 204, "logout")
	expect(t, c.do("GET", "/api/server/status", "", nil), 401, "revoked session")
}

func TestLoginRateLimit(t *testing.T) {
	srv := newTestServer(t)
	c := &client{t: t, base: srv.URL}
	for range 5 {
		c.do("POST", "/api/login", `{"password":"nope"}`, ajax)
	}
	expect(t, c.do("POST", "/api/login", `{"password":"`+testPassword+`"}`, ajax), 429, "locked out client")
}

func TestStaticAndSecurityHeaders(t *testing.T) {
	srv := newTestServer(t)
	c := &client{t: t, base: srv.URL}
	resp := c.do("GET", "/", "", nil)
	expect(t, resp, 200, "index")
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("missing CSP, got %q", csp)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options")
	}
	expect(t, c.do("POST", "/", "", nil), 405, "static files are read-only")
}

func TestClientSideRoutesServeTheApp(t *testing.T) {
	srv := newTestServer(t)
	c := &client{t: t, base: srv.URL}
	for _, route := range []string{"/worlds", "/players", "/settings", "/unknown/page"} {
		resp := c.do("GET", route, "", nil)
		expect(t, resp, 200, route)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s served %q, want the HTML app", route, ct)
		}
	}
	resp := c.do("GET", "/assets/app.js", "", nil)
	expect(t, resp, 200, "asset")
	if !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Error("fingerprinted assets should be cached")
	}
	expect(t, c.do("GET", "/assets/missing.js", "", nil), 404, "missing asset")
	expect(t, c.do("GET", "/api/unknown", "", nil), 401, "API paths are not the app")
}
