package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ApplyLocalPolicyRouting keeps traffic between local subnets on the main routing
// table so SSH and HTTP management on WAN/LAN IPs keep working after tailscale up.
func ApplyLocalPolicyRouting(cfg RouterConfig) {
	if cfg.WANInterface != "" {
		wanIP, wanPrefix := getInterfaceIPv4CIDR(cfg.WANInterface)
		if wanIP != "" && wanPrefix > 0 {
			wanNet := networkCIDR(wanIP, wanPrefix)
			ensureIPRule(90, wanNet, wanNet)
		}
	}

	if cfg.LANAddress != "" && cfg.LANPrefix > 0 {
		lanNet := networkCIDR(cfg.LANAddress, cfg.LANPrefix)
		ensureIPRule(91, lanNet, lanNet)
	}
}

func networkCIDR(ip string, prefix int) string {
	network := networkAddr(ip, prefix)
	if network == nil {
		return fmt.Sprintf("%s/%d", ip, prefix)
	}
	return fmt.Sprintf("%s/%d", network.String(), prefix)
}

func ensureIPRule(priority int, fromCIDR, toCIDR string) {
	pref := fmt.Sprintf("%d:", priority)
	out, _ := exec.Command("ip", "rule", "show").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), pref) {
			continue
		}
		if strings.Contains(line, "from "+fromCIDR) && strings.Contains(line, "to "+toCIDR) {
			return
		}
	}

	args := []string{
		"rule", "add",
		"from", fromCIDR,
		"to", toCIDR,
		"lookup", "main",
		"priority", fmt.Sprint(priority),
	}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "File exists") {
			return
		}
		log.Printf("policy routing (%s -> %s): %v: %s", fromCIDR, toCIDR, err, msg)
	} else {
		log.Printf("policy routing: local traffic %s -> %s uses main table", fromCIDR, toCIDR)
	}
}

const ipForwardSysctlPath = "/etc/sysctl.d/99-tailscale-router.conf"

var ipForwardSettings = map[string]string{
	"net.ipv4.ip_forward":              "1",
	"net.ipv4.conf.all.forwarding":     "1",
	"net.ipv4.conf.default.forwarding": "1",
	"net.ipv4.conf.all.rp_filter":      "2",
	"net.ipv4.conf.default.rp_filter":  "2",
}

// EnsureIPForwarding enables IPv4 routing and persists it across reboots.
func EnsureIPForwarding() error {
	var sysctlLines strings.Builder
	for key, val := range ipForwardSettings {
		fmt.Fprintf(&sysctlLines, "%s=%s\n", key, val)
		if out, err := exec.Command("sysctl", "-w", key+"="+val).CombinedOutput(); err != nil {
			log.Printf("sysctl -w %s=%s: %v: %s", key, val, err, strings.TrimSpace(string(out)))
		}
	}

	if err := os.WriteFile(ipForwardSysctlPath, []byte(sysctlLines.String()), 0644); err != nil {
		return err
	}
	exec.Command("sysctl", "-p", ipForwardSysctlPath).Run()

	if !IsIPForwardingEnabled() {
		return fmt.Errorf("net.ipv4.ip_forward is still disabled after enable attempt")
	}
	return nil
}

// IsIPForwardingEnabled reports whether the kernel will route between interfaces.
func IsIPForwardingEnabled() bool {
	output, err := exec.Command("sysctl", "-n", "net.ipv4.ip_forward").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}
