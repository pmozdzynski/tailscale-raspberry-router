package handlers

import (
	"log"
	"strings"
	"time"
)

// RestorePreviousMode restores the last known mode after startup.
// This function calls the handler functions directly (not via HTTP) to avoid authentication issues.
func RestorePreviousMode() {
	log.Println("Checking saved mode on startup...")

	if err := EnsureIPForwarding(); err != nil {
		log.Printf("Warning: IP forwarding: %v", err)
	}

	if IsConfigured() {
		ApplyLocalPolicyRouting(GetRouterConfig())
	}

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
		log.Println("No exit nodes detected! Staying in direct mode.")
		if CurrentMode == "direct" {
			_ = DisableTailscaleExitNode()
		}
		return
	}

	// Wait a bit more for Tailscale to be fully ready
	time.Sleep(2 * time.Second)

	// Restore mode from JSON by calling handler functions directly
	if CurrentMode != "direct" && strings.HasPrefix(CurrentMode, "tailscale:") {
		node := strings.TrimPrefix(CurrentMode, "tailscale:")
		log.Printf("Restoring exit node mode: %s", node)

		err := SetTailscaleExitNode(node)
		if err != nil {
			log.Printf("Failed to restore exit node mode: %v", err)
		} else {
			log.Printf("Successfully restored exit node mode: %s", node)
		}
	} else {
		log.Println("Restoring direct mode")

		err := DisableTailscaleExitNode()
		if err != nil {
			log.Printf("Failed to restore direct mode: %v", err)
		} else {
			log.Println("Successfully restored direct mode")
		}
	}
}
