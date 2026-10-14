package install

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hytale-server-manager/internal/hytaleapi"
)

// fakeAssets serves one game build whose version can be changed.
type fakeAssets struct {
	url     string
	mu      sync.Mutex
	version string
	zip     []byte
	// byLine overrides version for some patchlines.
	byLine map[string]string
}

func (f *fakeAssets) GameManifest(_ context.Context, token, patchline string) (hytaleapi.Manifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(f.zip)
	version := f.version
	if v, ok := f.byLine[patchline]; ok {
		version = v
	}
	return hytaleapi.Manifest{Version: version, DownloadURL: "builds/game.zip", SHA256: hex.EncodeToString(sum[:])}, nil
}

func (f *fakeAssets) SignedAssetURL(_ context.Context, token, path string) (string, error) {
	return f.url + "/" + path, nil
}

func gameZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"Server/HytaleServer.jar", "Assets.zip"} {
		w, _ := zw.Create(name)
		w.Write([]byte("data"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeJava pretends a runtime is already installed.
func fakeJava(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "jre", "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "jre", "bin", "java"), nil, 0o755)
	os.WriteFile(filepath.Join(dir, javaMarkerFile), []byte(`{"home":"jre","version":"25"}`), 0o644)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProvisionerInstallsAndUpdates(t *testing.T) {
	root := t.TempDir()
	javaDir, gameDir, tmp := filepath.Join(root, "java"), filepath.Join(root, "game"), filepath.Join(root, "tmp")
	for _, d := range []string{javaDir, gameDir, tmp} {
		os.MkdirAll(d, 0o755)
	}
	fakeJava(t, javaDir)

	assets := &fakeAssets{version: "1.0", zip: gameZip(t)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assets.mu.Lock()
		defer assets.mu.Unlock()
		w.Write(assets.zip)
	}))
	defer srv.Close()
	assets.url = srv.URL

	var accountReady atomic.Bool
	fetch := NewFetcher(srv.Client())
	token := func(context.Context) (string, error) { return "token", nil }
	p := NewProvisioner(ProvisionerDeps{
		Java:          NewJavaRuntime(javaDir, tmp, fetch),
		Game:          NewGameInstall(gameDir, filepath.Join(root, "versions"), tmp, assets, token, fetch),
		AccountReady:  accountReady.Load,
		ServerRunning: func() bool { return false },
		Patchline:     func() string { return "release" },
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	if st := p.Status(); st.GameReady || !st.WaitingForAccount || !st.JavaReady {
		t.Fatalf("nothing should be installed before login: %+v", st)
	}

	accountReady.Store(true)
	p.Wake()
	waitFor(t, "initial install", func() bool { return p.Status().GameReady })
	if v := p.Status().GameVersion; v != "1.0" {
		t.Fatalf("installed version %q", v)
	}

	assets.mu.Lock()
	assets.version = "2.0"
	assets.mu.Unlock()
	updated := make(chan struct{})
	if err := p.RequestUpdate(func() { close(updated) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	case <-time.After(15 * time.Second):
		t.Fatal("update callback not called")
	}
	if st := p.Status(); st.GameVersion != "2.0" || st.UpdateAvailable || st.Error != "" {
		t.Fatalf("unexpected status after update: %+v", st)
	}
}

func TestPinAndRollback(t *testing.T) {
	root := t.TempDir()
	javaDir, gameDir, tmp := filepath.Join(root, "java"), filepath.Join(root, "game"), filepath.Join(root, "tmp")
	for _, d := range []string{javaDir, gameDir, tmp} {
		os.MkdirAll(d, 0o755)
	}
	fakeJava(t, javaDir)

	assets := &fakeAssets{version: "1.0", zip: gameZip(t)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(assets.zip) }))
	defer srv.Close()
	assets.url = srv.URL

	var pin atomic.Value
	pin.Store("")
	fetch := NewFetcher(srv.Client())
	token := func(context.Context) (string, error) { return "token", nil }
	p := NewProvisioner(ProvisionerDeps{
		Java:          NewJavaRuntime(javaDir, tmp, fetch),
		Game:          NewGameInstall(gameDir, filepath.Join(root, "versions"), tmp, assets, token, fetch),
		AccountReady:  func() bool { return true },
		ServerRunning: func() bool { return false },
		Patchline:     func() string { return "release" },
		PinnedVersion: func() string { return pin.Load().(string) },
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	waitFor(t, "initial install", func() bool { return p.Status().GameVersion == "1.0" })

	// Update to 2.0: 1.0 must be kept for rollback.
	assets.mu.Lock()
	assets.version = "2.0"
	assets.mu.Unlock()
	done := make(chan struct{})
	if err := p.RequestUpdate(func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	if st := p.Status(); st.GameVersion != "2.0" || len(st.StoredVersions) != 1 || st.StoredVersions[0] != "1.0" {
		t.Fatalf("1.0 was not stored: %+v", st)
	}

	// Roll back to 1.0 and freeze there: no update to 2.0 afterwards.
	pin.Store("1.0")
	done = make(chan struct{})
	if err := p.RequestUpdate(func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	st := p.Status()
	if st.GameVersion != "1.0" || st.Error != "" || st.UpdateAvailable || st.PinnedVersion != "1.0" {
		t.Fatalf("rollback failed: %+v", st)
	}
	if len(st.StoredVersions) != 1 || st.StoredVersions[0] != "2.0" {
		t.Fatalf("2.0 should now be stored: %v", st.StoredVersions)
	}
	if _, pending := p.UpdatePendingSince(); pending {
		t.Fatal("a frozen game must not report a pending update")
	}

	// A pin that is neither stored nor the latest cannot be satisfied.
	pin.Store("0.5")
	done = make(chan struct{})
	if err := p.RequestUpdate(func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	if st := p.Status(); st.GameVersion != "1.0" || st.Error == "" {
		t.Fatalf("unknown pin should fail and keep 1.0: %+v", st)
	}
}

func TestPatchlineChangeInstallsItsBuild(t *testing.T) {
	root := t.TempDir()
	javaDir, gameDir, tmp := filepath.Join(root, "java"), filepath.Join(root, "game"), filepath.Join(root, "tmp")
	for _, d := range []string{javaDir, gameDir, tmp} {
		os.MkdirAll(d, 0o755)
	}
	fakeJava(t, javaDir)

	assets := &fakeAssets{version: "1.0", zip: gameZip(t), byLine: map[string]string{"pre-release": "1.1-pre"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(assets.zip) }))
	defer srv.Close()
	assets.url = srv.URL

	var line atomic.Value
	line.Store("release")
	fetch := NewFetcher(srv.Client())
	token := func(context.Context) (string, error) { return "token", nil }
	p := NewProvisioner(ProvisionerDeps{
		Java:          NewJavaRuntime(javaDir, tmp, fetch),
		Game:          NewGameInstall(gameDir, filepath.Join(root, "versions"), tmp, assets, token, fetch),
		AccountReady:  func() bool { return true },
		ServerRunning: func() bool { return false },
		Patchline:     func() string { return line.Load().(string) },
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	waitFor(t, "initial install", func() bool { return p.Status().GameVersion == "1.0" })

	// No wait for the periodic check: the new patchline is fetched at once.
	line.Store("pre-release")
	p.Wake()
	waitFor(t, "pre-release install", func() bool { return p.Status().GameVersion == "1.1-pre" })
}
