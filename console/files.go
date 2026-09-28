package console

import (
	"net/http"
	"strings"

	"github.com/plat5dev/operator/internal/apierr"
)

func Files(dir string) http.Handler {
	files := http.StripPrefix("/modules/", http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			apierr.NotFound(w)
			return
		}
		if strings.Contains(r.URL.Path, "..") {
			apierr.NotFound(w)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
