package webui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// dist holds the Vite build output, copied here by `make web-build` before
// `go build` runs. `all:dist` (not a bare `dist/*` pattern) so a leading-dot
// placeholder file is included even when no real build has been produced yet.
//
//go:embed all:dist
var distFS embed.FS

const notBuiltHTML = `<html><body style="font-family:monospace;padding:2rem">
<h1>Frontend not built</h1>
<p>Run <code>make web-build</code> then rebuild cmd/api.</p>
</body></html>`

func notBuiltHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(notBuiltHTML))
}

// Handler returns an http.Handler serving the embedded SPA: real files by
// exact path, falling back to index.html for client-side routed paths,
// and a 503 "not built yet" page if index.html is missing.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(notBuiltHandler)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if f, err := sub.Open(path); err == nil {
			stat, err := f.Stat()
			_ = f.Close()
			if err == nil && !stat.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else if path == "index.html" {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Fallback to index.html or not-built page
		ServeIndexOrFallback(w, sub)
	})
}

func ServeIndexOrFallback(w http.ResponseWriter, sub fs.FS) {
	indexFile, err := sub.Open("index.html")
	if err != nil {
		notBuiltHandler(w, nil)
		return
	}
	defer indexFile.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, indexFile)
}
