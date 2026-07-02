package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
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

type setupResetRequest struct {
	Rebootstrap bool `json:"rebootstrap"`
}

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

func SetupProgressHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(GetSetupJobProgress())
}

func SetupResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req setupResetRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if isSetupJobRunning() {
		http.Error(w, "Setup is already running", http.StatusConflict)
		return
	}

	if IsConfigured() {
		if !req.Rebootstrap {
			http.Error(w, "Already configured. Send {\"rebootstrap\":true} to reset.", http.StatusConflict)
			return
		}
		_ = runSystemctlWithTimeout(15*time.Second, "stop", "tailscale-router.service")
		if err := DeleteRouterConfig(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	ClearSetupProgress()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func SetupApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ReloadRouterConfigFromDisk()

	if IsConfigured() {
		http.Error(w, "Already configured", http.StatusConflict)
		return
	}
	if isSetupJobRunning() {
		http.Error(w, "Setup already running", http.StatusConflict)
		return
	}

	cfg, authKey, err := parseSetupRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !tryStartSetupJob() {
		http.Error(w, "Setup already running", http.StatusConflict)
		return
	}

	startSetupJobAsync(cfg, authKey)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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

func SetupPageHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./templates/setup.html")
}
