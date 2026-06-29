package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"tailscale-raspberry-router/handlers"
)

func checkRootPrivileges() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root. Try using: sudo ./tailscale-raspberry-router")
	}
}

func main() {
	checkRootPrivileges()

	http.HandleFunc("/setup", handlers.SetupPageHandler)
	http.HandleFunc("/setup/status", handlers.SetupStatusHandler)
	http.HandleFunc("/setup/apply", handlers.SetupApplyHandler)

	http.HandleFunc("/login", handlers.LoginHandler)
	http.HandleFunc("/logout", handlers.LogoutHandler)

	fs := http.FileServer(http.Dir("./templates"))
	serveStatic := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fs.ServeHTTP(w, r)
		}
	}
	http.HandleFunc("/styles.css", serveStatic("styles.css"))
	http.HandleFunc("/script.js", serveStatic("script.js"))
	http.HandleFunc("/setup.js", serveStatic("setup.js"))
	http.HandleFunc("/friendly-names.json", serveStatic("friendly-names.json"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !handlers.IsConfigured() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		handlers.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.ServeFile(w, r, "./templates/index.html")
				return
			}
			fs.ServeHTTP(w, r)
		})(w, r)
	})

	http.HandleFunc("/status", handlers.RequireAuth(handlers.StatusHandler))
	http.HandleFunc("/set-mode", handlers.RequireAuth(handlers.SetModeHandler))

	go func() {
		log.Println("Starting server on :5000")
		if handlers.IsConfigured() {
			log.Println("Router is configured. Dashboard at http://<device-ip>:5000/")
		} else {
			log.Println("First boot. Open http://<device-ip>:5000/setup to configure")
		}
		if err := http.ListenAndServe(":5000", nil); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)

	if handlers.IsConfigured() {
		go handlers.RestorePreviousMode()
	} else {
		log.Println("Skipping mode restore until first-time setup completes")
	}

	select {}
}
