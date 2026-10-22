package errorreports

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

const maxManifestBytes = 64 << 10

// pluginPackages maps the Java package of each plugin installed in a save
// to its name. Errors a plugin causes in a listener are reported by the
// server's own logger: the package of the frames tells the plugin apart.
type pluginPackages map[string]string

// scanPlugins reads the manifest.json of every jar in saveDir/mods. The
// package is the one of the plugin's main class.
func scanPlugins(saveDir string) pluginPackages {
	jars, _ := filepath.Glob(filepath.Join(saveDir, "mods", "*.jar"))
	found := pluginPackages{}
	for _, jar := range jars {
		m, ok := readManifest(jar)
		if !ok || m.Name == "" {
			continue
		}
		if i := strings.LastIndexByte(m.Main, '.'); i > 0 {
			found[m.Main[:i]] = m.Name
		}
	}
	return found
}

type pluginManifest struct {
	Name string `json:"Name"`
	Main string `json:"Main"`
}

func readManifest(jar string) (pluginManifest, bool) {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return pluginManifest{}, false
	}
	defer zr.Close()
	f, err := zr.Open("manifest.json")
	if err != nil {
		return pluginManifest{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes))
	var m pluginManifest
	if err != nil || json.Unmarshal(data, &m) != nil {
		return pluginManifest{}, false
	}
	return m, true
}

// owner returns the plugin whose package holds module, the longest
// package winning ("a.b.c" over "a.b").
func (p pluginPackages) owner(module string) string {
	best := ""
	for pkg := range p {
		if (module == pkg || strings.HasPrefix(module, pkg+".")) && len(pkg) > len(best) {
			best = pkg
		}
	}
	return p[best]
}

// attribute names the plugin of the innermost frame belonging to one.
func (p pluginPackages) attribute(ev Event) string {
	if len(p) == 0 {
		return ""
	}
	for _, ex := range slices.Backward(ev.Exceptions) {
		for _, f := range slices.Backward(ex.Frames) {
			if name := p.owner(f.Module); name != "" {
				return name
			}
		}
	}
	return ""
}
