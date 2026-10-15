package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/hytaleapi"
)

const (
	versionFile  = "version.txt"
	maxGameBytes = 20 << 30
	// keptVersions is how many replaced builds stay available for rollback.
	keptVersions = 3
)

// ValidVersion reports whether v is usable as a stored build name.
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// ErrVersionNotStored is returned when a rollback target is not available locally.
var ErrVersionNotStored = errors.New("this version is not stored locally")

// GameFiles points at the files needed to launch the server.
type GameFiles struct {
	ServerJar string
	AssetsZip string
	AOTCache  string // empty when the build ships no AOT cache
}

// assetAPI is the part of the Hytale API used to fetch game builds.
type assetAPI interface {
	GameManifest(ctx context.Context, accessToken, patchline string) (hytaleapi.Manifest, error)
	SignedAssetURL(ctx context.Context, accessToken, assetPath string) (string, error)
}

// TokenSource returns a valid OAuth access token of the server account.
type TokenSource func(ctx context.Context) (string, error)

// GameInstall downloads the server binaries and assets directly from the
// Hytale asset service, exactly like the server's built-in updater.
type GameInstall struct {
	dir         string
	versionsDir string
	tmpDir      string
	api         assetAPI
	token       TokenSource
	fetch       *Fetcher
}

// NewGameInstall manages the game files in dir.
// Replaced builds are kept in versionsDir.
func NewGameInstall(dir, versionsDir, tmpDir string, api assetAPI, token TokenSource, fetch *Fetcher) *GameInstall {
	return &GameInstall{dir: dir, versionsDir: versionsDir, tmpDir: tmpDir, api: api, token: token, fetch: fetch}
}

// Files returns the installed game files.
func (g *GameInstall) Files() (GameFiles, bool) {
	files := GameFiles{
		ServerJar: filepath.Join(g.dir, "Server", "HytaleServer.jar"),
		AssetsZip: filepath.Join(g.dir, "Assets.zip"),
	}
	if !fsutil.Exists(files.ServerJar) || !fsutil.Exists(files.AssetsZip) {
		return GameFiles{}, false
	}
	if aot := filepath.Join(g.dir, "Server", "HytaleServer.aot"); fsutil.Exists(aot) {
		files.AOTCache = aot
	}
	return files, true
}

// Version returns the installed game version, or "" when unknown.
func (g *GameInstall) Version() string {
	return readVersion(g.dir)
}

// Latest returns the manifest of the newest build of patchline.
func (g *GameInstall) Latest(ctx context.Context, patchline string) (hytaleapi.Manifest, error) {
	token, err := g.token(ctx)
	if err != nil {
		return hytaleapi.Manifest{}, err
	}
	return g.api.GameManifest(ctx, token, patchline)
}

// Install downloads the build described by m, verifies its checksum and
// swaps it in place. Progress covers the download only.
func (g *GameInstall) Install(ctx context.Context, m hytaleapi.Manifest, onProgress ProgressFunc) error {
	token, err := g.token(ctx)
	if err != nil {
		return err
	}
	signed, err := g.api.SignedAssetURL(ctx, token, m.DownloadURL)
	if err != nil {
		return fmt.Errorf("sign download URL: %w", err)
	}

	work, err := os.MkdirTemp(g.tmpDir, "game-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	zipPath := filepath.Join(work, "game.zip")
	if err := g.fetch.ToFile(ctx, signed, zipPath, m.SHA256, maxGameBytes, onProgress); err != nil {
		return fmt.Errorf("download game: %w", err)
	}
	extracted := filepath.Join(work, "extracted")
	if err := archive.ExtractZip(zipPath, extracted, archive.Limits{MaxFiles: 100_000, MaxTotalBytes: 40 << 30}); err != nil {
		return fmt.Errorf("extract game: %w", err)
	}
	if err := os.Remove(zipPath); err != nil {
		return err
	}
	root, err := findGameRoot(extracted)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, versionFile), []byte(m.Version+"\n"), 0o644); err != nil {
		return err
	}
	return g.swapIn(root)
}

// Stored lists the previous builds available for rollback, newest first.
func (g *GameInstall) Stored() []string {
	entries, err := os.ReadDir(g.versionsDir)
	if err != nil {
		return nil
	}
	type build struct {
		name string
		at   time.Time
	}
	var builds []build
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.IsDir() || !ValidVersion(e.Name()) || !completeBuild(filepath.Join(g.versionsDir, e.Name())) {
			continue
		}
		builds = append(builds, build{e.Name(), info.ModTime()})
	}
	slices.SortFunc(builds, func(a, b build) int { return b.at.Compare(a.at) })
	names := make([]string, len(builds))
	for i, b := range builds {
		names[i] = b.name
	}
	return names
}

// HasStored reports whether version can be activated without downloading.
func (g *GameInstall) HasStored(version string) bool {
	return ValidVersion(version) && slices.Contains(g.Stored(), version)
}

// Activate switches the installed game to a stored build. The build it
// replaces is stored in turn, so the switch can be undone.
func (g *GameInstall) Activate(version string) error {
	if !g.HasStored(version) {
		return ErrVersionNotStored
	}
	return g.swapIn(filepath.Join(g.versionsDir, version))
}

// archive stores a replaced build for later rollback, or deletes it when it
// cannot be identified.
func (g *GameInstall) archive(dir, replacedBy string) {
	name := readVersion(dir)
	if name == "" || name == replacedBy || !ValidVersion(name) || !completeBuild(dir) {
		_ = os.RemoveAll(dir)
		return
	}
	dest := filepath.Join(g.versionsDir, name)
	if err := os.MkdirAll(g.versionsDir, 0o750); err != nil {
		_ = os.RemoveAll(dir)
		return
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(dir, dest); err != nil {
		_ = os.RemoveAll(dir)
		return
	}
	now := time.Now()
	_ = os.Chtimes(dest, now, now) // storage order = replacement order
	for _, stale := range g.Stored()[min(keptVersions, len(g.Stored())):] {
		_ = os.RemoveAll(filepath.Join(g.versionsDir, stale))
	}
}

func completeBuild(root string) bool {
	return fsutil.Exists(filepath.Join(root, "Assets.zip")) &&
		fsutil.Exists(filepath.Join(root, "Server", "HytaleServer.jar"))
}

func readVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, versionFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// swapIn atomically replaces the game directory with newRoot.
func (g *GameInstall) swapIn(newRoot string) error {
	old := g.dir + ".old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if fsutil.Exists(g.dir) {
		if err := os.Rename(g.dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(newRoot, g.dir); err != nil {
		_ = os.Rename(old, g.dir) // roll back
		return err
	}
	g.archive(old, readVersion(g.dir))
	return nil
}

// findGameRoot finds the directory holding Assets.zip and Server/.
func findGameRoot(dir string) (string, error) {
	var root string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && fsutil.Exists(filepath.Join(p, "Assets.zip")) &&
			fsutil.Exists(filepath.Join(p, "Server", "HytaleServer.jar")) {
			root = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", errors.New("unexpected game archive: Assets.zip or Server/HytaleServer.jar missing")
	}
	return root, nil
}
