package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// Global variables
var (
	mu        sync.Mutex
	exitNodes = make(map[string]ExitNode)
)

// Status API Handler
func StatusHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	// Check if Tailscale is running before responding
	if !IsTailscaleRunning() {
		response := map[string]interface{}{
			"mode":       CurrentMode,
			"exitNodes":  map[string]ExitNode{},
			"configured": IsConfigured(),
			"network":    GetNetworkSnapshot(),
			"warning":    "Tailscale is not connected",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Refresh exit nodes dynamically
	nodes, err := GetExitNodes()
	if err == nil {
		exitNodes = nodes
	}

	response := map[string]interface{}{
		"mode":       CurrentMode,
		"exitNodes":  exitNodes,
		"configured": IsConfigured(),
		"network":    GetNetworkSnapshot(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Set Mode API Handler
func SetModeHandler(w http.ResponseWriter, r *http.Request) {
	modeType := r.URL.Query().Get("mode")

	if modeType == "direct" {
		err := DisableTailscaleExitNode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if modeType == "tailscale" {
		node := r.URL.Query().Get("node")
		if node == "" {
			http.Error(w, "Missing node parameter", http.StatusBadRequest)
			return
		}
		err := SetTailscaleExitNode(node)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Invalid mode", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Switched to mode: %s\n", CurrentMode)
}

// GetActiveInternetInterface detects the main internet interface dynamically.
func GetActiveInternetInterface() (string, error) {
	if IsConfigured() {
		iface := ConfiguredWAN()
		if iface != "" {
			return iface, nil
		}
	}

	cmd := exec.Command("sh", "-c", "ip route | grep default | awk '{print $5}'")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect active interface: %v", err)
	}

	interfaceName := strings.TrimSpace(string(output))
	if interfaceName == "" {
		return "", fmt.Errorf("no active internet interface detected")
	}

	log.Println("Detected active internet interface:", interfaceName) // Log the detected interface
	return interfaceName, nil
}

// GetLANInterfaces detects LAN interfaces (non-default route, non-loopback, non-tailscale interfaces)
func GetLANInterfaces() ([]string, error) {
	if IsConfigured() {
		return ConfiguredLANInterfaces()
	}

	// Get the default route interface
	defaultInterface, err := GetActiveInternetInterface()
	if err != nil {
		return nil, err
	}

	// Get all interfaces
	cmd := exec.Command("sh", "-c", "ip -o link show | awk -F': ' '{print $2}'")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var lanInterfaces []string

	for _, iface := range lines {
		iface = strings.TrimSpace(iface)
		// Skip loopback, default route interface, and tailscale interface
		if iface != "lo" && iface != defaultInterface && iface != "tailscale0" && iface != "" {
			lanInterfaces = append(lanInterfaces, iface)
		}
	}

	return lanInterfaces, nil
}

// Check if Mullvad Exit Nodes are Enabled for this Device
func IsMullvadEnabled() bool {
	cmd := exec.Command("tailscale", "status")
	output, err := cmd.Output()
	if err != nil {
		log.Println("Error checking Tailscale status:", err)
		return false // Assume not enabled if we can't check
	}

	// Mullvad is enabled if "Exit Node Available" includes Mullvad
	return strings.Contains(string(output), "Exit Node Available: Mullvad")
}

// Check if Tailscale is Running
func IsTailscaleRunning() bool {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return false
	}
	cmd := exec.Command("tailscale", "status")
	err := cmd.Run()
	return err == nil
}
