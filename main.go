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

	mux := http.NewServeMux()
	handlers.RegisterRouterRoutes(mux)

	go func() {
		addr := ":" + handlers.RouterPort
		log.Printf("Router dashboard listening on %s", addr)
		if handlers.IsConfigured() {
			log.Println("Dashboard ready at http://<device-ip>:" + handlers.RouterPort + "/")
		} else {
			log.Println("Not configured yet — use bootstrap wizard on :" + handlers.BootstrapPort + "/setup")
		}
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("Router server failed: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)

	if err := handlers.EnsureIPForwarding(); err != nil {
		log.Printf("Warning: IP forwarding: %v", err)
	}

	if handlers.IsConfigured() {
		go handlers.RestorePreviousMode()
	} else {
		log.Println("Skipping mode restore until first-time setup completes")
	}

	select {}
}
