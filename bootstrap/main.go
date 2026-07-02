package main

import (
	"log"
	"net/http"
	"os"

	"tailscale-raspberry-router/handlers"
)

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("tailscale-router-bootstrap must run as root")
	}

	mux := http.NewServeMux()
	handlers.RegisterBootstrapRoutes(mux)

	addr := ":" + handlers.BootstrapPort
	log.Printf("Bootstrap wizard listening on %s (stays up after setup completes)", addr)
	log.Printf("Dashboard runs separately on :%s after setup finishes", handlers.RouterPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Bootstrap server failed: %v", err)
	}
}
