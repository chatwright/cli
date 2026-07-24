// ui.go is the static-file seam that lets this same server also serve a
// built Studio web UI locally (--ui-dir), enabling an offline/local-first
// mode later. It does not fetch, bundle, or know anything about how that
// UI is built or packaged — it only serves whatever directory the operator
// points it at, with an SPA fallback to index.html for any unknown
// non-API path, since a client-side-routed SPA's own routes (e.g.
// "/runs/42") are not real files on disk.
//
// API routes are registered on the same http.ServeMux as their own
// exact-match patterns ("/health", "/metrics", "/v1/chat/completions",
// "/datastate/query"), which always win over the "/" subtree pattern the UI
// handler is mounted on — see buildMux. This is what "never collide with UI
// routes" means in practice: there is no path-prefix reservation to
// maintain, Go's own ServeMux longest-match rule does it.
package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
)

// newUIHandler serves dir's static files at the request path, falling back
// to dir/index.html for any path that is not an existing regular file —
// the standard SPA-hosting pattern. path.Clean on the request's URL path
// (always slash-rooted) cannot escape above the root, so this never serves
// a file outside dir.
func newUIHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	indexPath := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet, http.MethodHead)
			return
		}

		cleaned := path.Clean(r.URL.Path)
		candidate := filepath.Join(dir, filepath.FromSlash(cleaned))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}
