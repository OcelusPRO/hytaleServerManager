package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the single-page interface: existing files are served
// as-is, any other path returns index.html so that client-side routes
// (/worlds, /players…) survive a reload or a direct link.
func spaHandler(static fs.FS) http.Handler {
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeMessage(w, http.StatusMethodNotAllowed, "méthode non autorisée")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && isFile(static, name) {
			if strings.HasPrefix(name, "assets/") {
				// Vite fingerprints these file names: they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r) // a missing asset, not a page
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, static, "index.html")
	})
}

func isFile(static fs.FS, name string) bool {
	info, err := fs.Stat(static, name)
	return err == nil && !info.IsDir()
}
