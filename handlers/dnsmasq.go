package handlers

import (
	"log"
	"os"
	"os/exec"
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
