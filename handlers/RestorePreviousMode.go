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

	savedExitNode := ""
	if CurrentMode != "direct" && strings.HasPrefix(CurrentMode, "tailscale:") {
		savedExitNode = strings.TrimPrefix(CurrentMode, "tailscale:")
	}

	if len(exitNodes) == 0 {
		log.Println("No exit nodes detected within 10s; applying direct mode routing")
		if err := DisableTailscaleExitNode(); err != nil {
			log.Printf("Failed to apply direct mode routing: %v", err)
		}
		if savedExitNode != "" {
			go retryExitNodeRestore(savedExitNode)
		}
		return
	}

	// Wait a bit more for Tailscale to be fully ready
	time.Sleep(2 * time.Second)

	if savedExitNode != "" {
		log.Printf("Restoring exit node mode: %s", savedExitNode)
		if err := SetTailscaleExitNode(savedExitNode); err != nil {
			log.Printf("Failed to restore exit node mode: %v; falling back to direct mode", err)
			if err := DisableTailscaleExitNode(); err != nil {
				log.Printf("Failed to apply direct mode routing: %v", err)
			}
			go retryExitNodeRestore(savedExitNode)
		} else {
			log.Printf("Successfully restored exit node mode: %s", savedExitNode)
		}
		return
	}

	log.Println("Restoring direct mode")
	if err := DisableTailscaleExitNode(); err != nil {
		log.Printf("Failed to restore direct mode: %v", err)
	} else {
		log.Println("Successfully restored direct mode")
	}
}

// retryExitNodeRestore waits for Tailscale exit nodes after slow boots, then
// reapplies the saved exit node mode if it is still the desired mode.
func retryExitNodeRestore(node string) {
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)

		expectedMode := "tailscale:" + node
		if CurrentMode != expectedMode && CurrentMode != "direct" {
			log.Printf("Exit node restore skipped: mode changed to %s", CurrentMode)
			return
		}

		nodes, err := GetExitNodes()
		if err != nil || len(nodes) == 0 {
			continue
		}

		exitNodes = nodes
		log.Printf("Retrying exit node restore: %s", node)
		if err := SetTailscaleExitNode(node); err != nil {
			log.Printf("Exit node restore retry failed: %v", err)
			continue
		}

		log.Printf("Successfully restored exit node mode on retry: %s", node)
		return
	}

	log.Printf("Exit node restore gave up after retries: %s", node)
}
