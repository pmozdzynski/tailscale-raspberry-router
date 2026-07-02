package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"tailscale-raspberry-router/handlers"
)

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("tailscale-router-bootstrap must run as root")
	}

	mux := http.NewServeMux()
	handlers.RegisterBootstrapRoutes(mux)

	addr := ":" + handlers.BootstrapPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("Bootstrap wizard listening on %s (stays up during setup)", addr)
	log.Printf("Dashboard runs separately on :%s after setup finishes", handlers.RouterPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Bootstrap server failed: %v", err)
	}
}
