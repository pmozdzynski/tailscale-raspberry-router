package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	configDir  = "/etc/tailscale-router"
	configFile = configDir + "/config.json"
)

// RouterConfig is persisted after first-time web setup.
type RouterConfig struct {
	Configured       bool   `json:"configured"`
	WANInterface     string `json:"wan_interface"`
	LANInterface     string `json:"lan_interface"`
	LANAddress       string `json:"lan_address"`
	LANPrefix        int    `json:"lan_prefix"`
	DHCPRangeStart   string `json:"dhcp_range_start"`
	DHCPRangeEnd     string `json:"dhcp_range_end"`
	DHCPLeaseHours   int    `json:"dhcp_lease_hours"`
	TailscaleHost    string `json:"tailscale_hostname"`
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
}

var (
	configMu sync.RWMutex
	routerConfig RouterConfig
)

func init() {
	routerConfig = LoadRouterConfig()
}

func LoadRouterConfig() RouterConfig {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return RouterConfig{LANPrefix: 24, DHCPLeaseHours: 12}
	}
	var cfg RouterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RouterConfig{LANPrefix: 24, DHCPLeaseHours: 12}
	}
	if cfg.LANPrefix == 0 {
		cfg.LANPrefix = 24
	}
	if cfg.DHCPLeaseHours == 0 {
		cfg.DHCPLeaseHours = 12
	}
	return cfg
}

func SaveRouterConfig(cfg RouterConfig) error {
	configMu.Lock()
	defer configMu.Unlock()

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return err
	}

	routerConfig = cfg
	return nil
}

func GetRouterConfig() RouterConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	return routerConfig
}

func IsConfigured() bool {
	cfg := GetRouterConfig()
	return cfg.Configured && cfg.WANInterface != "" && cfg.LANInterface != ""
}

func ConfiguredWAN() string {
	cfg := GetRouterConfig()
	if cfg.WANInterface != "" {
		return cfg.WANInterface
	}
	iface, err := detectDefaultRouteInterface()
	if err == nil {
		return iface
	}
	return "eth0"
}

func ConfiguredLANInterfaces() ([]string, error) {
	cfg := GetRouterConfig()
	if cfg.LANInterface != "" {
		return []string{cfg.LANInterface}, nil
	}
	return detectLANInterfaces("")
}

func ScriptsDir() string {
	candidates := []string{
		"/opt/tailscale-raspberry-router/scripts",
		filepath.Join(".", "scripts"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return "/opt/tailscale-raspberry-router/scripts"
}
