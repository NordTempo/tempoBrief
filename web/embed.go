// Package web embeds the built web app into the binary. Vite writes to
// web/dist/app; web/dist/.keep keeps the embed pattern valid before a build, in
// which case the server answers "web app not built".
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built web app, rooted at dist/app.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist/app")
	if err != nil {
		panic(err)
	}
	return sub
}
