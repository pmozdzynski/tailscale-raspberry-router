package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyBootstrap configures networking, dnsmasq, Tailscale, and helper scripts.
func ApplyBootstrap(cfg RouterConfig, tailscaleAuthKey string) error {
	if cfg.LANInterface == "" {
		return fmt.Errorf("LAN interface is required")
	}
	if cfg.LANAddress == "" {
		return fmt.Errorf("LAN address is required")
	}
	if cfg.LANPrefix == 0 {
		cfg.LANPrefix = 24
	}
	if cfg.DHCPRangeStart == "" || cfg.DHCPRangeEnd == "" {
		return fmt.Errorf("DHCP range is required")
	}
	if cfg.AdminUsername == "" {
		cfg.AdminUsername = "admin"
	}
	if cfg.AdminPassword == "" {
		return fmt.Errorf("admin password is required")
	}
	if cfg.TailscaleHost == "" {
		cfg.TailscaleHost = getSystemHostname()
	}

	if cfg.WANInterface == "" {
		iface, err := detectDefaultRouteInterface()
		if err != nil {
			return fmt.Errorf("WAN interface not selected and no default route detected — connect WAN and retry")
		}
		cfg.WANInterface = iface
	}

	if cfg.WANInterface == cfg.LANInterface {
		return fmt.Errorf("WAN and LAN must be different interfaces")
	}

	if err := validateLANDoesNotOverlapWAN(cfg.WANInterface, cfg.LANAddress, cfg.LANPrefix); err != nil {
		return err
	}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"install system packages", installSystemPackages},
		{"install Tailscale", installTailscaleIfMissing},
		{"enable IP forwarding", func() error { return enableIPForwarding() }},
		{"install helper scripts", installHelperScripts},
		{"verify WAN stays on DHCP", func() error { return ensureWANDHCP(cfg) }},
		{"configure LAN interface", func() error { return configureLANInterface(cfg) }},
		{"configure dnsmasq", func() error { return configureDnsmasq(cfg) }},
		{"configure Tailscale", func() error { return configureTailscale(cfg, tailscaleAuthKey) }},
		{"enable DNS watcher", enableDNSWatcher},
	}

	for _, step := range steps {
		log.Printf("Bootstrap: %s", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	cfg.Configured = true
	if err := SaveRouterConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	ReloadDnsmasqUpstream()

	if err := DisableTailscaleExitNode(); err != nil {
		log.Printf("Bootstrap: initial direct mode setup: %v", err)
	}

	log.Println("Bootstrap completed successfully")
	return nil
}

func enableIPForwarding() error {
	if err := os.WriteFile("/etc/sysctl.d/99-tailscale-router.conf", []byte("net.ipv4.ip_forward=1\n"), 0644); err != nil {
		return err
	}
	return exec.Command("sysctl", "-p", "/etc/sysctl.d/99-tailscale-router.conf").Run()
}

func installHelperScripts() error {
	srcDir := ScriptsDir()
	targets := map[string]string{
		"update-dns.sh":          "/usr/local/bin/update-dns.sh",
		"tailscale-dns-watch.sh": "/usr/local/bin/tailscale-dns-watch.sh",
	}

	for name, dest := range targets {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("missing script %s", src)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0755); err != nil {
			return err
		}
	}
	return nil
}

// ensureWANDHCP verifies WAN is left untouched (ISP/home router DHCP).
// We never assign a static address to the WAN interface.
func ensureWANDHCP(cfg RouterConfig) error {
	wanIP, wanPrefix := getInterfaceIPv4CIDR(cfg.WANInterface)
	if wanIP != "" {
		log.Printf("Bootstrap: WAN %s uses %s/%d from existing network config (unchanged)", cfg.WANInterface, wanIP, wanPrefix)
	} else {
		log.Printf("Bootstrap: WAN %s has no IPv4 yet — DHCP will continue on that interface", cfg.WANInterface)
	}
	return nil
}

func configureLANInterface(cfg RouterConfig) error {
	cidr := fmt.Sprintf("%s/%d", cfg.LANAddress, cfg.LANPrefix)

	if usesNetworkManager() {
		connName := "tailscale-router-lan"
		_ = exec.Command("nmcli", "con", "delete", connName).Run()
		cmd := exec.Command("nmcli", "con", "add", "type", "ethernet",
			"ifname", cfg.LANInterface,
			"con-name", connName,
			"ipv4.method", "manual",
			"ipv4.addresses", cidr,
			"ipv6.method", "ignore",
			"connection.autoconnect", "yes",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, string(out))
		}
		if out, err := exec.Command("nmcli", "con", "up", connName).CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, string(out))
		}
		return nil
	}

	block := fmt.Sprintf("\ninterface %s\nstatic ip_address=%s\nnohook wpa_supplicant\n", cfg.LANInterface, cidr)
	return appendUniqueBlock("/etc/dhcpcd.conf", "interface "+cfg.LANInterface, block, func() error {
		return exec.Command("systemctl", "restart", "dhcpcd").Run()
	})
}

func configureDnsmasq(cfg RouterConfig) error {
	if err := os.MkdirAll("/etc/dnsmasq.d", 0755); err != nil {
		return err
	}

	netmask := prefixToNetmask(cfg.LANPrefix)
	conf := fmt.Sprintf(`# Managed by tailscale-raspberry-router
interface=%s
bind-interfaces
except-interface=%s

dhcp-range=%s,%s,%s,%dh
dhcp-option=3,%s
dhcp-option=6,%s

no-resolv
resolv-file=/run/tailscale-router/upstream.conf

cache-size=1000
domain-needed
bogus-priv
`, cfg.LANInterface, cfg.WANInterface,
		cfg.DHCPRangeStart, cfg.DHCPRangeEnd, netmask, cfg.DHCPLeaseHours,
		cfg.LANAddress, cfg.LANAddress)

	path := "/etc/dnsmasq.d/tailscale-router.conf"
	if err := os.WriteFile(path, []byte(conf), 0644); err != nil {
		return err
	}

	enableConf := "conf-dir=/etc/dnsmasq.d/,*.conf\n"
	if data, err := os.ReadFile("/etc/dnsmasq.conf"); err != nil || !strings.Contains(string(data), "conf-dir=/etc/dnsmasq.d") {
		if err := os.WriteFile("/etc/dnsmasq.conf", []byte(enableConf), 0644); err != nil {
			return err
		}
	}

	exec.Command("systemctl", "enable", "dnsmasq").Run()
	if err := exec.Command("systemctl", "restart", "dnsmasq").Run(); err != nil {
		return err
	}
	return nil
}

func configureTailscale(cfg RouterConfig, authKey string) error {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return fmt.Errorf("tailscale is not installed — bootstrap could not install it automatically")
	}

	exec.Command("systemctl", "enable", "tailscaled").Run()
	if err := exec.Command("systemctl", "start", "tailscaled").Run(); err != nil {
		log.Printf("Warning: could not start tailscaled: %v", err)
	}

	args := []string{
		"up",
		"--advertise-exit-node=false",
		"--accept-routes",
		"--accept-dns",
		"--hostname=" + cfg.TailscaleHost,
	}
	if authKey != "" {
		args = append(args, "--auth-key="+authKey)
	} else if !getTailscaleSnapshot().Connected {
		return fmt.Errorf("tailscale auth key is required on fresh installs")
	}

	cmd := exec.Command("tailscale", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if authKey == "" && strings.Contains(msg, "login") {
			return fmt.Errorf("tailscale requires login — provide an auth key in setup or run: sudo tailscale up")
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}

func enableDNSWatcher() error {
	candidates := []string{
		filepath.Join(ScriptsDir(), "..", "configs", "tailscale-dns-watch.service"),
		"/opt/tailscale-raspberry-router/configs/tailscale-dns-watch.service",
	}

	var data []byte
	var err error
	for _, path := range candidates {
		data, err = os.ReadFile(filepath.Clean(path))
		if err == nil {
			break
		}
	}
	if err != nil {
		log.Printf("DNS watcher service file not found, skipping: %v", err)
		return nil
	}

	if err := os.WriteFile("/etc/systemd/system/tailscale-dns-watch.service", data, 0644); err != nil {
		return err
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "tailscale-dns-watch.service").Run()
	return exec.Command("systemctl", "restart", "tailscale-dns-watch.service").Run()
}

func usesNetworkManager() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "NetworkManager").Run() == nil
}

func prefixToNetmask(prefix int) string {
	if prefix <= 0 || prefix > 32 {
		return "255.255.255.0"
	}
	mask := uint32(0xFFFFFFFF << (32 - prefix))
	return fmt.Sprintf("%d.%d.%d.%d",
		(mask>>24)&0xFF, (mask>>16)&0xFF, (mask>>8)&0xFF, mask&0xFF)
}

func appendUniqueBlock(path, marker, block string, restart func() error) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	if strings.Contains(content, marker) {
		return restart()
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	return restart()
}
