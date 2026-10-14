// Package web embeds the compiled management interface (web/dist), built
// from the React sources with `npm run build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// Static returns the interface files rooted at the dist directory.
func Static() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err) // the embedded tree is fixed at build time
	}
	return sub
}
