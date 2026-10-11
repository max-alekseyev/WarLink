package adminweb

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html style.css app.js
var staticFiles embed.FS

// Handler returns an http.Handler that serves the embedded admin web panel assets.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFiles, ".")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

// GetIndexHTML returns the index.html content as string.
func GetIndexHTML() ([]byte, error) {
	return staticFiles.ReadFile("index.html")
}

// GetStyleCSS returns the style.css content.
func GetStyleCSS() ([]byte, error) {
	return staticFiles.ReadFile("style.css")
}

// GetAppJS returns the app.js content.
func GetAppJS() ([]byte, error) {
	return staticFiles.ReadFile("app.js")
}
