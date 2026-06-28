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

// SetupApplyHandler runs first-time bootstrap from the web wizard.
func SetupApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if IsConfigured() {
		http.Error(w, "Router is already configured", http.StatusConflict)
		return
	}

	var req setupApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
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
		cfg.WANInterface = strings.TrimSpace(req.WANInterface)
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

	if err := ApplyBootstrap(cfg, strings.TrimSpace(req.TailscaleAuthKey)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	SetRuntimeCredentials(cfg.AdminUsername, cfg.AdminPassword)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"message":  fmt.Sprintf("Router configured. LAN %s on %s", cfg.LANAddress, cfg.LANInterface),
		"login_at": "/login",
	})
}

// SetupPageHandler serves the first-time setup wizard.
func SetupPageHandler(w http.ResponseWriter, r *http.Request) {
	if IsConfigured() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.ServeFile(w, r, "./templates/setup.html")
}
