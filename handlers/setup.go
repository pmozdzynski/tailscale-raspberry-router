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

	cfg, authKey, err := parseSetupRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stream := strings.Contains(r.Header.Get("Accept"), "text/event-stream") ||
		r.URL.Query().Get("stream") == "1"

	if stream {
		setupApplyStream(w, cfg, authKey)
		return
	}

	if err := ApplyBootstrapWithProgress(cfg, authKey, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	SetRuntimeCredentials(cfg.AdminUsername, cfg.AdminPassword)
	writeSetupOK(w, cfg)
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

func setupApplyStream(w http.ResponseWriter, cfg RouterConfig, authKey string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func(status, step, detail string) {
		payload, _ := json.Marshal(map[string]string{
			"status": status,
			"step":   step,
			"detail": detail,
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	progress := setupProgressReporter(func(status, step, detail string) {
		send(status, step, detail)
	})

	err := ApplyBootstrapWithProgress(cfg, authKey, progress)
	if err != nil {
		send("error", "", err.Error())
		return
	}

	SetRuntimeCredentials(cfg.AdminUsername, cfg.AdminPassword)

	ips := getManagementAccessIPs()
	loginHint := "Open /login with your dashboard username and password"
	if len(ips) > 0 {
		loginHint = fmt.Sprintf("Exit node dashboard: http://%s:5000/ (login: %s)", ips[0], cfg.AdminUsername)
	}
	send("done", "", fmt.Sprintf("Router configured. LAN %s on %s. %s", cfg.LANAddress, cfg.LANInterface, loginHint))
}

func writeSetupOK(w http.ResponseWriter, cfg RouterConfig) {
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
