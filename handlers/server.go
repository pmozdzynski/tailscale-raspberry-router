package handlers

import (
	"net/http"
)

func registerStaticAssets(mux *http.ServeMux, includeSetup bool) {
	fs := http.FileServer(http.Dir("./templates"))
	serveStatic := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fs.ServeHTTP(w, r)
		}
	}
	mux.HandleFunc("/styles.css", serveStatic("styles.css"))
	if includeSetup {
		mux.HandleFunc("/setup.js", serveStatic("setup.js"))
	}
	mux.HandleFunc("/script.js", serveStatic("script.js"))
	mux.HandleFunc("/friendly-names.json", serveStatic("friendly-names.json"))
}

// RegisterBootstrapRoutes wires the first-time setup wizard (port 4000).
// This listener stays up during and after bootstrap so the browser never loses connection.
func RegisterBootstrapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/setup", SetupPageHandler)
	mux.HandleFunc("/setup/status", SetupStatusHandler)
	mux.HandleFunc("/setup/progress", SetupProgressHandler)
	mux.HandleFunc("/setup/reset", SetupResetHandler)
	mux.HandleFunc("/setup/apply", SetupApplyHandler)
	mux.HandleFunc("/setup/router-status", SetupRouterStatusHandler)
	registerStaticAssets(mux, true)
}

// RegisterRouterRoutes wires the post-setup dashboard (port 5000).
func RegisterRouterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/login", LoginHandler)
	mux.HandleFunc("/logout", LogoutHandler)

	registerStaticAssets(mux, false)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !IsConfigured() {
			http.Redirect(w, r, BootstrapSetupURL(r), http.StatusSeeOther)
			return
		}
		RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.ServeFile(w, r, "./templates/index.html")
				return
			}
			http.FileServer(http.Dir("./templates")).ServeHTTP(w, r)
		})(w, r)
	})

	mux.HandleFunc("/status", RequireAuth(StatusHandler))
	mux.HandleFunc("/set-mode", RequireAuth(SetModeHandler))
	mux.HandleFunc("/diagnostics/run", RequireAuth(DiagnosticsRunHandler))
	mux.HandleFunc("/diagnostics/repair", RequireAuth(DiagnosticsRepairHandler))
}
