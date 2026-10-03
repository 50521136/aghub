package portal

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
)

// staticFiles contains the front-end of the portal.
//
// It is a plain page with no build step, so the very same directory can be
// copied to a web server of its own and used as a separately deployed
// front-end.  In that case the front-end and the API are on different origins
// and the API address has to be set in the page.
//
//go:embed static
var staticFiles embed.FS

// registerStatic registers the handlers that serve the bundled front-end.
func (m *Manager) registerStatic(reg aghhttp.Registrar) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Should not happen, since the embedded path is a constant.
		panic(fmt.Errorf("portal: getting the static subdirectory: %w", err))
	}

	// The file server sees the path without the /portal prefix, so that
	// /portal/ resolves to the index page.
	fileServer := http.StripPrefix("/portal", http.FileServer(http.FS(sub)))

	reg.Register(http.MethodGet, "/portal/", func(w http.ResponseWriter, r *http.Request) {
		// The page reads the API address from its own source, so it must not
		// be cached by an intermediary.
		w.Header().Set("Cache-Control", "no-cache")

		fileServer.ServeHTTP(w, r)
	})
}
