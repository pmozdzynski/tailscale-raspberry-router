package main

import (
	"log"
	"net/http"
	"os"

	"tailscale-raspberry-router/handlers"
)

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("run as root")
	}

	mux := http.NewServeMux()
	handlers.RegisterBootstrapRoutes(mux)

	addr := ":" + handlers.BootstrapPort
	log.Printf("Setup wizard on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
