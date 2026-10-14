package curseforge

import (
	"archive/zip"
	"bytes"
	"crypto/sha1" //nolint:gosec
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/secretbox"
)

func jarBytes(tag string) []byte {
	return append([]byte("PK\x03\x04"), []byte(tag)...)
}

func sha1Hex(b []byte) string {
	h := sha1.New() //nolint:gosec
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func packZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	add("manifest.json", `{"name":"Big Pack","overrides":"overrides","files":[{"projectID":10,"fileID":101,"required":true},{"projectID":11,"fileID":111,"required":false}]}`)
	add("overrides/config/pack.json", `{"ok":true}`)
	add("overrides/.hsm-mods.json", `{"evil":true}`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fake struct {
	t       *testing.T
	srv     *httptest.Server
	v2      bool // serve the newer file of mod 10
	blocked bool // mod 12 has no download URL
}

func (f *fake) file(id, modID int, name, tag string, date time.Time) map[string]any {
	body := jarBytes(tag)
	return map[string]any{
		"id": id, "modId": modID, "displayName": name, "fileName": name, "fileDate": date,
		"fileLength": len(body), "releaseType": 1, "isAvailable": true,
		"downloadUrl":  f.srv.URL + "/dl/" + fmt.Sprint(id) + "/" + name,
		"hashes":       []map[string]any{{"value": sha1Hex(body), "algo": 1}},
		"dependencies": []map[string]any{},
	}
}

func (f *fake) handler() http.Handler {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(48 * time.Hour)
	mux := http.NewServeMux()
	data := func(w http.ResponseWriter, v any, total int) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": v, "pagination": map[string]any{"totalCount": total}})
	}
	mux.HandleFunc("/v1/games", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "good-key-1234" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		data(w, []map[string]any{{"id": 1, "name": "Other", "slug": "other"}, {"id": 70216, "name": "Hytale", "slug": "hytale"}}, 2)
	})
	mux.HandleFunc("/v1/categories", func(w http.ResponseWriter, _ *http.Request) {
		data(w, []map[string]any{
			{"id": 900, "name": "Mods", "slug": "mods", "isClass": true},
			{"id": 901, "name": "Modpacks", "slug": "modpacks", "isClass": true},
		}, 2)
	})
	mux.HandleFunc("GET /v1/mods/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("classId") != "900" || r.URL.Query().Get("gameId") != "70216" {
			f.t.Errorf("unexpected search %v", r.URL.RawQuery)
		}
		data(w, []map[string]any{f.mod(10, "Cool Mod")}, 1)
	})
	mux.HandleFunc("POST /v1/mods", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ModIDs []int `json:"modIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var out []map[string]any
		for _, id := range body.ModIDs {
			out = append(out, f.mod(id, fmt.Sprintf("Mod %d", id)))
		}
		data(w, out, len(out))
	})
	mux.HandleFunc("POST /v1/mods/files", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FileIDs []int `json:"fileIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var out []map[string]any
		for _, id := range body.FileIDs {
			out = append(out, f.file(id, id/10, fmt.Sprintf("pack-mod-%d.jar", id), "v1", old))
		}
		data(w, out, len(out))
	})
	mux.HandleFunc("GET /v1/mods/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id int
		_, _ = fmt.Sscan(r.PathValue("id"), &id)
		data(w, f.mod(id, fmt.Sprintf("Mod %d", id)), 1)
	})
	mux.HandleFunc("GET /v1/mods/{id}/files/{file}/download-url", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/999/pack.zip":
			_, _ = w.Write(packZip(f.t))
		case "/dl/102/Cool Mod 2.jar":
			_, _ = w.Write(jarBytes("v2"))
		default:
			_, _ = w.Write(jarBytes("v1"))
			if r.URL.Path == "/dl/101/Cool Mod 1.jar" {
				return
			}
		}
	})
	_ = newer
	return mux
}

func (f *fake) mod(id int, name string) map[string]any {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	files := []map[string]any{f.file(101, id, "Cool Mod 1.jar", "v1", old)}
	if f.v2 {
		files = append(files, f.file(102, id, "Cool Mod 2.jar", "v2", old.Add(48*time.Hour)))
	}
	if id == 12 {
		files[0]["downloadUrl"] = nil
	}
	if id == 20 { // modpack
		pack := f.file(999, id, "pack.zip", "pack", old)
		pack["hashes"] = []map[string]any{}
		files = []map[string]any{pack}
	}
	return map[string]any{
		"id": id, "name": name, "slug": "mod", "summary": "s", "downloadCount": 5.0,
		"links": map[string]any{"websiteUrl": "https://example.test/m"}, "latestFiles": files,
		"authors": []map[string]any{{"name": "me"}},
	}
}

func newTestService(t *testing.T, f *fake) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"saves", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.srv = httptest.NewTLSServer(f.handler())
	t.Cleanup(f.srv.Close)

	store := saves.NewStore(filepath.Join(root, "saves"), filepath.Join(root, "tmp"))
	save, err := store.Create("w")
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.LoadOrCreate(filepath.Join(root, "key"))
	if err != nil {
		t.Fatal(err)
	}
	keys := OpenKeyStore(filepath.Join(root, "cf.enc"), box)
	svc := NewService(Deps{
		Client:       NewClient(f.srv.Client(), f.srv.URL, keys.Get),
		Keys:         keys,
		Mods:         saves.NewModStore(store),
		TmpDir:       filepath.Join(root, "tmp"),
		MaxFileBytes: 1 << 20,
		PackLimits:   archive.Limits{MaxFiles: 100, MaxTotalBytes: 1 << 20},
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return svc, save.ID
}

func waitJob(t *testing.T, s *Service, id string) Job {
	t.Helper()
	for range 200 {
		j, _ := s.Job(id)
		if j.State != JobRunning {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func TestKeyValidationAndSearch(t *testing.T) {
	f := &fake{t: t}
	svc, _ := newTestService(t, f)
	ctx := t.Context()

	if _, err := svc.Search(ctx, SearchQuery{Text: "x"}); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("search without key: %v", err)
	}
	if err := svc.SetKey(ctx, "bad-key"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("bad key accepted: %v", err)
	}
	if svc.Keys.Get() != "" {
		t.Fatal("rejected key was stored")
	}
	if err := svc.SetKey(ctx, " good-key-1234 "); err != nil {
		t.Fatal(err)
	}
	st := svc.Status(ctx)
	if !st.Ready || !st.Modpacks || st.KeyHint != "••••1234" {
		t.Fatalf("unexpected status %+v", st)
	}
	res, err := svc.Search(ctx, SearchQuery{Text: "cool"})
	if err != nil || len(res.Items) != 1 || res.Items[0].Latest == nil || !res.Items[0].Latest.Downloadable {
		t.Fatalf("unexpected search %+v, %v", res, err)
	}
}

func TestInstallAndUpdate(t *testing.T) {
	f := &fake{t: t}
	svc, saveID := newTestService(t, f)
	ctx := t.Context()
	if err := svc.SetKey(ctx, "good-key-1234"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.InstallMod(ctx, saveID, 10, 0, false); err != nil {
		t.Fatal(err)
	}
	mods, _ := svc.Mods.List(saveID)
	if len(mods) != 1 || mods[0].FileName != "Cool Mod 1.jar" || mods[0].CurseForge == nil || mods[0].CurseForge.FileID != 101 {
		t.Fatalf("unexpected mods %+v", mods)
	}
	if updates, err := svc.CheckUpdates(ctx, saveID); err != nil || len(updates) != 0 {
		t.Fatalf("unexpected updates %v, %v", updates, err)
	}

	// A manual file with the same name must never be overwritten.
	if _, err := svc.Mods.Add(saveID, "Cool Mod 3.jar", bytes.NewReader(jarBytes("x")), 1<<20); err != nil {
		t.Fatal(err)
	}

	f.v2 = true
	if err := svc.Mods.SetEnabled(saveID, "Cool Mod 1.jar", false); err != nil {
		t.Fatal(err)
	}
	updates, err := svc.CheckUpdates(ctx, saveID)
	if err != nil || len(updates) != 1 || updates[0].Latest.ID != 102 {
		t.Fatalf("expected one update, got %v, %v", updates, err)
	}
	job, err := svc.StartUpdates(saveID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done := waitJob(t, svc, job.ID); done.State != JobDone || len(done.Warnings) != 0 {
		t.Fatalf("update job: %+v", done)
	}
	mods, _ = svc.Mods.List(saveID)
	var updated *saves.Mod
	for i := range mods {
		if mods[i].CurseForge != nil {
			updated = &mods[i]
		}
	}
	if len(mods) != 2 || updated == nil || updated.FileName != "Cool Mod 2.jar" || updated.Enabled {
		t.Fatalf("update should replace the file and keep it disabled: %+v", mods)
	}
}

func TestDownloadDisabled(t *testing.T) {
	f := &fake{t: t}
	svc, saveID := newTestService(t, f)
	_ = svc.SetKey(t.Context(), "good-key-1234")
	_, err := svc.InstallMod(t.Context(), saveID, 12, 0, false)
	var disabled *DownloadDisabledError
	if !errors.As(err, &disabled) || disabled.URL == "" {
		t.Fatalf("expected DownloadDisabledError, got %v", err)
	}
}

func TestModpack(t *testing.T) {
	f := &fake{t: t}
	svc, saveID := newTestService(t, f)
	_ = svc.SetKey(t.Context(), "good-key-1234")

	job, err := svc.StartModpack(saveID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, svc, job.ID)
	if done.State != JobDone {
		t.Fatalf("modpack job: %+v", done)
	}
	mods, _ := svc.Mods.List(saveID)
	if len(mods) != 1 || mods[0].CurseForge == nil || mods[0].CurseForge.Pack != "Big Pack" {
		t.Fatalf("only the required file should be installed: %+v", mods)
	}
	dir, _ := svc.Mods.SaveDir(saveID)
	if _, err := os.Stat(filepath.Join(dir, "config", "pack.json")); err != nil {
		t.Errorf("override not applied: %v", err)
	}
	links, _ := svc.Mods.Links(saveID)
	if _, evil := links["evil"]; evil {
		t.Error("pack overwrote the manager metadata")
	}
}

func TestModFileName(t *testing.T) {
	cases := map[string]string{
		"Cool Mod-1.0.jar": "Cool Mod-1.0.jar",
		"évil/../x?.JAR":   "vil_.._x_.jar",
		"_hidden.zip":      "hidden.zip",
	}
	for in, want := range cases {
		got, err := modFileName(in)
		if err != nil || got != want {
			t.Errorf("modFileName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := modFileName("readme.txt"); !errors.Is(err, ErrNotAModArchive) {
		t.Errorf("txt accepted: %v", err)
	}
}

func TestModpackRejectsEscapingEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write([]byte(`{"files":[]}`))
	w, _ = zw.Create("overrides/../../escape.txt")
	_, _ = w.Write([]byte("no"))
	_ = zw.Close()
	path := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fake{t: t}
	svc, saveID := newTestService(t, f)
	err := svc.applyOverrides(path, saveID, "overrides")
	if !errors.Is(err, archive.ErrUnsafePath) {
		t.Fatalf("escaping entry accepted: %v", err)
	}
}
