package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// RestorePreviousMode restores the last known mode after startup.
func RestorePreviousMode() {
	log.Println("Checking saved mode on startup...")

	// Ensure exit nodes are available before restoring mode
	for i := 0; i < 10; i++ { // Try for 10 seconds
		nodes, err := GetExitNodes()
		if err == nil && len(nodes) > 0 {
			exitNodes = nodes // Store them globally
			break
		}
		log.Println("Waiting for exit nodes to become available...")
		time.Sleep(1 * time.Second)
	}

	// If no nodes were found, log an error
	if len(exitNodes) == 0 {
		log.Println("No exit nodes detected! Cannot restore previous mode.")
		return
	}

	// Restore mode from JSON
	if CurrentMode != "direct" && strings.HasPrefix(CurrentMode, "tailscale:") {
		node := strings.TrimPrefix(CurrentMode, "tailscale:")
		log.Printf("Requesting mode restore via API: %s", node)

		resp, err := http.Post(fmt.Sprintf("http://localhost:5000/set-mode?mode=tailscale&node=%s", node), "application/json", nil)
		if err != nil {
			log.Printf("Failed to restore mode via API: %v", err)
		} else {
			log.Printf("Successfully restored mode via API: %s", node)
			resp.Body.Close()
		}
	} else {
		log.Println("Requesting direct mode restore via API")

		resp, err := http.Post("http://localhost:5000/set-mode?mode=direct", "application/json", nil)
		if err != nil {
			log.Printf("Failed to restore direct mode via API: %v", err)
		} else {
			log.Println("Successfully restored direct mode via API")
			resp.Body.Close()
		}
	}
}
