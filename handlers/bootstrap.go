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
	return ApplyBootstrapWithProgress(cfg, tailscaleAuthKey, nil)
}

// ApplyBootstrapWithProgress runs bootstrap and streams step updates when progress is set.
func ApplyBootstrapWithProgress(cfg RouterConfig, tailscaleAuthKey string, progress setupProgressReporter) error {
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
			return fmt.Errorf("WAN interface not selected and no default route detected. Connect WAN and retry")
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
		{"enable health watch", enableHealthWatch},
		{"enable hardware watchdog", enableHardwareWatchdog},
	}

	for _, step := range steps {
		log.Printf("Bootstrap: %s", step.name)
		progress.running(step.name, "started")
		if err := step.fn(); err != nil {
			progress.fail(step.name, err.Error())
			return fmt.Errorf("%s: %w", step.name, err)
		}
		progress.ok(step.name, "completed")
	}

	progress.running("save configuration", "writing /etc/tailscale-router/config.json")
	cfg.Configured = true
	if err := SaveRouterConfig(cfg); err != nil {
		progress.fail("save configuration", err.Error())
		return fmt.Errorf("save config: %w", err)
	}
	progress.ok("save configuration", "saved")

	progress.running("initial routing", "direct mode + DNS reload")
	ReloadDnsmasqUpstream()

	if err := DisableTailscaleExitNode(); err != nil {
		log.Printf("Bootstrap: initial direct mode setup: %v", err)
		progress.running("initial routing", "warning: "+err.Error())
	} else {
		progress.ok("initial routing", "direct mode active")
	}

	log.Println("Bootstrap completed successfully")
	return nil
}

func enableIPForwarding() error {
	sysctl := "net.ipv4.ip_forward=1\n" +
		"net.ipv4.conf.all.rp_filter=2\n" +
		"net.ipv4.conf.default.rp_filter=2\n"
	if err := os.WriteFile("/etc/sysctl.d/99-tailscale-router.conf", []byte(sysctl), 0644); err != nil {
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
		log.Printf("Bootstrap: WAN %s has no IPv4 yet. DHCP will continue on that interface", cfg.WANInterface)
	}
	return nil
}

func configureLANInterface(cfg RouterConfig) error {
	exec.Command("ip", "link", "set", cfg.LANInterface, "up").Run()

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
	if err := ensureDnsmasqInstalled(); err != nil {
		return err
	}

	if err := os.MkdirAll("/etc/dnsmasq.d", 0755); err != nil {
		return err
	}

	if err := prepareDNSPort53(); err != nil {
		return fmt.Errorf("prepare port 53: %w", err)
	}

	if err := migrateDnsmasqForRouter(); err != nil {
		return fmt.Errorf("migrate dnsmasq config: %w", err)
	}

	netmask := prefixToNetmask(cfg.LANPrefix)
	conf := fmt.Sprintf(`# Managed by tailscale-raspberry-router
interface=%s
bind-interfaces
listen-address=%s
except-interface=%s
dhcp-range=%s,%s,%s,%dh
dhcp-option=option:router,%s
dhcp-option=option:dns-server,%s
no-resolv
resolv-file=/run/tailscale-router/upstream.conf
`, cfg.LANInterface, cfg.LANAddress, cfg.WANInterface,
		cfg.DHCPRangeStart, cfg.DHCPRangeEnd, netmask, cfg.DHCPLeaseHours,
		cfg.LANAddress, cfg.LANAddress)

	path := "/etc/dnsmasq.d/tailscale-router.conf"
	if err := os.WriteFile(path, []byte(conf), 0644); err != nil {
		return err
	}

	if out, err := exec.Command("dnsmasq", "--test").CombinedOutput(); err != nil {
		return fmt.Errorf("dnsmasq config test failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	exec.Command("systemctl", "enable", "dnsmasq").Run()
	if out, err := exec.Command("systemctl", "restart", "dnsmasq").CombinedOutput(); err != nil {
		journal := dnsmasqJournalTail()
		return fmt.Errorf("systemctl restart dnsmasq: %v: %s%s", err, strings.TrimSpace(string(out)), journal)
	}
	return nil
}

func migrateDnsmasqForRouter() error {
	const backupMain = "/etc/dnsmasq.conf.pre-tailscale-router"
	const minimalMain = "conf-dir=/etc/dnsmasq.d/,*.conf\n"

	conflictKeys := []string{
		"interface=", "bind-interfaces", "listen-address=", "except-interface=",
		"dhcp-range=", "dhcp-option", "no-resolv", "resolv-file=",
		"cache-size", "domain-needed", "bogus-priv", "port=",
	}

	data, err := os.ReadFile("/etc/dnsmasq.conf")
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	needsMainReplace := false
	if err == nil {
		text := string(data)
		for _, key := range conflictKeys {
			if strings.Contains(text, key) {
				needsMainReplace = true
				break
			}
		}
	}

	if needsMainReplace {
		log.Println("Bootstrap: backing up /etc/dnsmasq.conf (duplicate keys would break dnsmasq)")
		_ = os.Rename("/etc/dnsmasq.conf", backupMain)
		if err := os.WriteFile("/etc/dnsmasq.conf", []byte(minimalMain), 0644); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err := os.WriteFile("/etc/dnsmasq.conf", []byte(minimalMain), 0644); err != nil {
			return err
		}
	} else if !strings.Contains(string(data), "conf-dir=/etc/dnsmasq.d") {
		if err := appendUniqueBlock("/etc/dnsmasq.conf", "conf-dir=/etc/dnsmasq.d",
			"\n# tailscale-raspberry-router\nconf-dir=/etc/dnsmasq.d/,*.conf\n", func() error { return nil }); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir("/etc/dnsmasq.d")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "tailscale-router.conf" || !strings.HasSuffix(name, ".conf") {
			continue
		}
		src := filepath.Join("/etc/dnsmasq.d", name)
		dst := src + ".pre-tailscale-router"
		log.Printf("Bootstrap: disabling extra dnsmasq drop-in %s", name)
		_ = os.Rename(src, dst)
	}

	return nil
}

func prepareDNSPort53() error {
	out, _ := exec.Command("sh", "-c", "ss -ulnp | grep ':53 ' || true").CombinedOutput()
	text := string(out)
	if !strings.Contains(text, "systemd-resolve") && !strings.Contains(text, "127.0.0.53") {
		return nil
	}

	log.Println("Bootstrap: disabling systemd-resolved DNS stub on port 53")
	if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0755); err != nil {
		return err
	}
	stub := "[Resolve]\nDNSStubListener=no\n"
	if err := os.WriteFile("/etc/systemd/resolved.conf.d/tailscale-router.conf", []byte(stub), 0644); err != nil {
		return err
	}
	exec.Command("systemctl", "restart", "systemd-resolved").Run()
	return nil
}

func dnsmasqJournalTail() string {
	out, err := exec.Command("journalctl", "-u", "dnsmasq", "-n", "15", "--no-pager").CombinedOutput()
	if err != nil {
		return ""
	}
	return "\n--- journalctl -u dnsmasq ---\n" + strings.TrimSpace(string(out))
}

func configureTailscale(cfg RouterConfig, authKey string) error {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return fmt.Errorf("tailscale is not installed. Bootstrap could not install it automatically")
	}

	exec.Command("systemctl", "enable", "tailscaled").Run()
	if err := exec.Command("systemctl", "start", "tailscaled").Run(); err != nil {
		log.Printf("Warning: could not start tailscaled: %v", err)
	}

	args := []string{
		"up",
		"--advertise-exit-node=false",
		"--accept-routes=false",
		"--accept-dns=false",
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
			return fmt.Errorf("tailscale requires login. Provide an auth key in setup or run: sudo tailscale up")
		}
		return fmt.Errorf("%v: %s", err, msg)
	}

	ApplyLocalPolicyRouting(cfg)
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
