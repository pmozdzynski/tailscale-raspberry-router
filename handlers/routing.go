package handlers

import (
	"fmt"
	"log"
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
