package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

const (
	updateDnsScript     = "/usr/local/bin/update-dns.sh"
	updateDnsScriptLocal = "/opt/tailscale-raspberry-router/scripts/update-dns.sh"
)

// ReloadDnsmasqUpstream refreshes LAN DNS upstreams after routing/DNS changes.
func ReloadDnsmasqUpstream() {
	script := updateDnsScript
	if _, err := os.Stat(script); err != nil {
		script = updateDnsScriptLocal
	}
	if _, err := os.Stat(script); err != nil {
		log.Println("update-dns.sh not found; falling back to systemctl reload dnsmasq")
		if err := exec.Command("systemctl", "reload", "dnsmasq").Run(); err != nil {
			_ = exec.Command("systemctl", "restart", "dnsmasq").Run()
		}
		return
	}

	cmd := exec.Command(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("update-dns.sh failed: %v: %s", err, string(output))
		return
	}
	if len(output) > 0 {
		log.Printf("update-dns.sh: %s", string(output))
	}
}

// writeInitialUpstreamDNS creates upstream files before dnsmasq first starts.
func writeInitialUpstreamDNS(wanInterface string) error {
	script := updateDnsScript
	if _, err := os.Stat(script); err != nil {
		script = updateDnsScriptLocal
	}
	if _, err := os.Stat(script); err != nil {
		return writeFallbackUpstreamDNS(wanInterface)
	}

	cmd := exec.Command(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("update-dns.sh: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return ensureUpstreamServersFile()
}

func ensureUpstreamServersFile() error {
	path := "/run/tailscale-router/upstream-servers.conf"
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeFallbackUpstreamDNS("")
}

func writeFallbackUpstreamDNS(wanInterface string) error {
	dir := "/run/tailscale-router"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	servers, source := discoverWANDNS(wanInterface)
	if len(servers) == 0 {
		servers = []string{"1.1.1.1", "9.9.9.9"}
		source = "no WAN DNS from DHCP"
	}

	var resolv strings.Builder
	fmt.Fprintf(&resolv, "# Managed by tailscale-raspberry-router bootstrap (%s)\n", source)
	var serverConf strings.Builder
	fmt.Fprintf(&serverConf, "# Managed by tailscale-raspberry-router bootstrap (%s)\n", source)
	for _, ns := range servers {
		fmt.Fprintf(&resolv, "nameserver %s\n", ns)
		fmt.Fprintf(&serverConf, "server=%s\n", ns)
	}

	if err := os.WriteFile(dir+"/upstream.conf", []byte(resolv.String()), 0644); err != nil {
		return err
	}
	return os.WriteFile(dir+"/upstream-servers.conf", []byte(serverConf.String()), 0644)
}

func discoverWANDNS(wanOverride string) ([]string, string) {
	wan := wanOverride
	if wan == "" {
		wan = ConfiguredWAN()
	}
	if wan == "" {
		iface, err := detectDefaultRouteInterface()
		if err == nil {
			wan = iface
		}
	}

	var servers []string
	if wan != "" {
		if out, err := exec.Command("resolvectl", "dns", wan).Output(); err == nil {
			servers = appendUniqueNameservers(servers, parseResolvectlDNS(string(out))...)
		}
		if len(servers) == 0 {
			if out, err := exec.Command("nmcli", "-t", "-f", "IP4.DNS", "dev", "show", wan).Output(); err == nil {
				for _, line := range strings.Split(string(out), "\n") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						servers = appendUniqueNameservers(servers, strings.TrimSpace(parts[1]))
					}
				}
			}
		}
		if len(servers) > 0 {
			return servers, "WAN " + wan + " DHCP"
		}
	}

	for _, path := range []string{"/run/NetworkManager/no-stub-resolv.conf", "/etc/resolv.conf"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		servers = appendUniqueNameservers(servers, parseNameserverLines(string(data))...)
		if len(servers) > 0 {
			return servers, path
		}
	}

	return nil, ""
}

func parseResolvectlDNS(output string) []string {
	var servers []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.Contains(field, ".") || strings.Contains(field, ":") {
				servers = appendUniqueNameservers(servers, field)
			}
		}
	}
	return servers
}

func parseNameserverLines(text string) []string {
	var servers []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			servers = appendUniqueNameservers(servers, fields[1])
		}
	}
	return servers
}

func appendUniqueNameservers(existing []string, candidates ...string) []string {
	seen := make(map[string]bool, len(existing))
	for _, ns := range existing {
		seen[ns] = true
	}
	for _, ns := range candidates {
		if !isUsableNameserver(ns) || seen[ns] {
			continue
		}
		seen[ns] = true
		existing = append(existing, ns)
	}
	return existing
}

func isUsableNameserver(ns string) bool {
	if ns == "" || ns == "0.0.0.0" {
		return false
	}
	if strings.HasPrefix(ns, "127.") || ns == "::1" {
		return false
	}
	return true
}
