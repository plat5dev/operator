package console

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/plat5dev/operator/internal/apierr"
)

const servicesPlaceholder = "__SERVICES__"

func Handler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			apierr.NotFound(w)
			return
		}
		rel, ok := relativePath(r.URL.Path)
		if !ok {
			apierr.NotFound(w)
			return
		}
		if rel == "" || rel == "index.html" {
			serveIndex(w, r, dir)
			return
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		f, err := os.Open(full)
		if err != nil {
			if path.Ext(rel) != "" {
				apierr.NotFound(w)
				return
			}
			serveIndex(w, r, dir)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			if path.Ext(rel) != "" {
				apierr.NotFound(w)
				return
			}
			serveIndex(w, r, dir)
			return
		}
		http.ServeContent(w, r, st.Name(), st.ModTime(), f)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	raw, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fallbackHTML))
		return
	}
	// The menu is not compiled into the bundle. The page reads this list.
	body := strings.ReplaceAll(string(raw), servicesPlaceholder, "[]")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func relativePath(urlPath string) (string, bool) {
	cleaned := path.Clean("/" + urlPath)
	rel := strings.TrimPrefix(cleaned, "/")
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

const fallbackHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Operator</title></head>
<body><p>Operator console assets are not built.</p></body></html>
`
