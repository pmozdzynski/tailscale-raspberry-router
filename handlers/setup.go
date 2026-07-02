package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type setupApplyRequest struct {
	WANInterface     string `json:"wan_interface"`
	LANInterface     string `json:"lan_interface"`
	LANAddress       string `json:"lan_address"`
	LANPrefix        int    `json:"lan_prefix"`
	DHCPRangeStart   string `json:"dhcp_range_start"`
	DHCPRangeEnd     string `json:"dhcp_range_end"`
	DHCPLeaseHours   int    `json:"dhcp_lease_hours"`
	TailscaleHost    string `json:"tailscale_hostname"`
	TailscaleAuthKey string `json:"tailscale_auth_key"`
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
}

// SetupStatusHandler returns interfaces, routing, and Tailscale state for the wizard.
func SetupStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	snapshot := GetNetworkSnapshot()
	if wan := strings.TrimSpace(r.URL.Query().Get("wan")); wan != "" {
		snapshot.SuggestedLAN = SuggestLANSubnet(wan)
	}

	json.NewEncoder(w).Encode(snapshot)
}

// SetupProgressHandler returns async bootstrap progress for polling clients.
func SetupProgressHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(GetSetupJobProgress())
}

// SetupResetHandler clears abandoned setup progress so a fresh install can start.
func SetupResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if IsConfigured() {
		http.Error(w, "Router is already configured", http.StatusConflict)
		return
	}
	if isSetupJobRunning() {
		http.Error(w, "Setup is already running", http.StatusConflict)
		return
	}
	ClearSetupProgress()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cleared": true})
}

// SetupApplyHandler starts first-time bootstrap asynchronously.
// The browser polls GET /setup/progress until complete (no long-lived SSE).
func SetupApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if IsConfigured() {
		http.Error(w, "Router is already configured", http.StatusConflict)
		return
	}

	if isSetupJobRunning() {
		http.Error(w, "Setup is already running", http.StatusConflict)
		return
	}

	reconcileStaleSetupJob()

	cfg, authKey, err := parseSetupRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !tryStartSetupJob() {
		http.Error(w, "Setup is already running", http.StatusConflict)
		return
	}

	startSetupJobAsync(cfg, authKey)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"started": true,
		"poll":    "/setup/progress",
	})
}

func parseSetupRequest(r *http.Request) (RouterConfig, string, error) {
	var req setupApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return RouterConfig{}, "", fmt.Errorf("invalid JSON body")
	}

	cfg := RouterConfig{
		WANInterface:   strings.TrimSpace(req.WANInterface),
		LANInterface:   strings.TrimSpace(req.LANInterface),
		LANAddress:     strings.TrimSpace(req.LANAddress),
		LANPrefix:      req.LANPrefix,
		DHCPRangeStart: strings.TrimSpace(req.DHCPRangeStart),
		DHCPRangeEnd:   strings.TrimSpace(req.DHCPRangeEnd),
		DHCPLeaseHours: req.DHCPLeaseHours,
		TailscaleHost:  strings.TrimSpace(req.TailscaleHost),
		AdminUsername:  strings.TrimSpace(req.AdminUsername),
		AdminPassword:  req.AdminPassword,
	}

	if cfg.LANPrefix == 0 {
		cfg.LANPrefix = 24
	}
	if cfg.DHCPLeaseHours == 0 {
		cfg.DHCPLeaseHours = 12
	}
	if cfg.AdminUsername == "" {
		cfg.AdminUsername = "admin"
	}
	if cfg.WANInterface == "" {
		iface, err := detectDefaultRouteInterface()
		if err == nil {
			cfg.WANInterface = iface
		}
	}

	suggested := SuggestLANSubnet(cfg.WANInterface)
	if cfg.LANAddress == "" {
		cfg.LANAddress = suggested.Address
	}
	if cfg.LANPrefix == 0 {
		cfg.LANPrefix = suggested.Prefix
	}
	if cfg.DHCPRangeStart == "" {
		cfg.DHCPRangeStart = suggested.DHCPStart
	}
	if cfg.DHCPRangeEnd == "" {
		cfg.DHCPRangeEnd = suggested.DHCPEnd
	}
	if cfg.TailscaleHost == "" {
		cfg.TailscaleHost = getSystemHostname()
	}

	return cfg, strings.TrimSpace(req.TailscaleAuthKey), nil
}

// SetupPageHandler serves the first-time setup wizard.
func SetupPageHandler(w http.ResponseWriter, r *http.Request) {
	if IsConfigured() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.ServeFile(w, r, "./templates/setup.html")
}
