package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/fsutil"
)

const (
	javaFeatureVersion = 25
	adoptiumAssetsURL  = "https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=%s&image_type=jre&os=linux&vendor=eclipse"
	maxJREBytes        = 400 << 20
	javaMarkerFile     = "current.json"
)

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9.+_-]`)

type javaMarker struct {
	Home    string `json:"home"` // relative to the Java directory
	Version string `json:"version"`
}

type adoptiumAsset struct {
	ReleaseName string `json:"release_name"`
	Binary      struct {
		Package struct {
			Link     string `json:"link"`
			Checksum string `json:"checksum"`
			Name     string `json:"name"`
		} `json:"package"`
	} `json:"binary"`
}

// JavaRuntime installs the Eclipse Temurin JRE required by the server.
type JavaRuntime struct {
	dir    string
	tmpDir string
	fetch  *Fetcher
}

// NewJavaRuntime manages runtimes stored below dir.
func NewJavaRuntime(dir, tmpDir string, fetch *Fetcher) *JavaRuntime {
	return &JavaRuntime{dir: dir, tmpDir: tmpDir, fetch: fetch}
}

// Binary returns the path of the java executable when installed.
func (j *JavaRuntime) Binary() (string, bool) {
	m, ok := j.marker()
	if !ok {
		return "", false
	}
	bin := filepath.Join(j.dir, m.Home, "bin", "java")
	return bin, fsutil.Exists(bin)
}

// Version returns the installed release name, or "" when absent.
func (j *JavaRuntime) Version() string {
	if _, ok := j.Binary(); !ok {
		return ""
	}
	m, _ := j.marker()
	return m.Version
}

// Install downloads the latest Java runtime unless it is already present.
func (j *JavaRuntime) Install(ctx context.Context, onProgress ProgressFunc) error {
	arch, err := adoptiumArch()
	if err != nil {
		return err
	}
	var assets []adoptiumAsset
	if err := j.fetch.JSON(ctx, fmt.Sprintf(adoptiumAssetsURL, javaFeatureVersion, arch), &assets); err != nil {
		return fmt.Errorf("query Adoptium: %w", err)
	}
	if len(assets) == 0 || assets[0].Binary.Package.Checksum == "" {
		return errors.New("aucun JRE Java 25 disponible chez Adoptium")
	}
	asset := assets[0]
	if j.Version() == asset.ReleaseName {
		return nil
	}

	archivePath := filepath.Join(j.tmpDir, "jre-download.tar.gz")
	defer os.Remove(archivePath)
	pkg := asset.Binary.Package
	if err := j.fetch.ToFile(ctx, pkg.Link, archivePath, pkg.Checksum, maxJREBytes, onProgress); err != nil {
		return err
	}
	return j.installArchive(archivePath, asset.ReleaseName)
}

func (j *JavaRuntime) installArchive(archivePath, release string) error {
	staging, err := os.MkdirTemp(j.tmpDir, "jre-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	err = archive.ExtractTarGz(f, staging, archive.Limits{MaxFiles: 5000, MaxTotalBytes: 1 << 30})
	f.Close()
	if err != nil {
		return fmt.Errorf("extract JRE: %w", err)
	}
	home, err := findJavaHome(staging)
	if err != nil {
		return err
	}

	name := unsafeNameChars.ReplaceAllString(release, "_")
	final := filepath.Join(j.dir, name)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	if err := os.Rename(home, final); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(final, "bin", "java"), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(j.dir, javaMarkerFile), javaMarker{Home: name, Version: release}, 0o644); err != nil {
		return err
	}
	return j.removeOtherRuntimes(name)
}

func (j *JavaRuntime) removeOtherRuntimes(keep string) error {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			if err := os.RemoveAll(filepath.Join(j.dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (j *JavaRuntime) marker() (javaMarker, bool) {
	var m javaMarker
	found, err := fsutil.ReadJSON(filepath.Join(j.dir, javaMarkerFile), &m)
	return m, found && err == nil && m.Home != ""
}

// findJavaHome locates the directory containing bin/java in an extracted JRE.
func findJavaHome(root string) (string, error) {
	var home string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && fsutil.Exists(filepath.Join(p, "bin", "java")) {
			home = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("bin/java introuvable dans l'archive Java")
	}
	return home, nil
}

func adoptiumArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("architecture non supportée: %s", runtime.GOARCH)
	}
}
