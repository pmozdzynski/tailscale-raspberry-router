package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	tailscaledUnitPath = "/etc/systemd/system/tailscaled.service"
	tailscaledDefaults = "/etc/default/tailscaled"
	tailscaledSocket   = "/run/tailscale/tailscaled.sock"
)

func ensureTailscaledServiceInstalled() error {
	if err := os.MkdirAll("/etc/systemd/system", 0755); err != nil {
		return err
	}

	// Always refresh the unit: older Pis cannot use StateDirectory/RuntimeDirectory.
	data, err := readConfigAsset("tailscaled.service")
	if err != nil {
		return fmt.Errorf("tailscaled.service template: %w", err)
	}
	if err := os.WriteFile(tailscaledUnitPath, data, 0644); err != nil {
		return err
	}

	flags := ""
	if detectMachineArch() == "armv6" {
		flags = "--tun=userspace-networking"
		log.Println("Bootstrap: Tailscale userspace networking for ARMv6")
	}
	defaults := fmt.Sprintf("FLAGS=%q\n", flags)
	if err := os.WriteFile(tailscaledDefaults, []byte(defaults), 0644); err != nil {
		return err
	}

	exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func ensureTailscaledRunning() error {
	if err := ensureTailscaledServiceInstalled(); err != nil {
		return err
	}

	if !commandExists("tailscale") {
		return fmt.Errorf("tailscale CLI not found")
	}

	exec.Command("systemctl", "enable", "tailscaled").Run()

	if tailscaledDaemonReady() {
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		cmd := "start"
		if attempt > 1 {
			cmd = "restart"
		}
		out, err := exec.Command("systemctl", cmd, "tailscaled").CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("systemctl %s tailscaled: %v: %s", cmd, err, strings.TrimSpace(string(out)))
			if strings.Contains(strings.ToLower(string(out)), "failed to load") {
				lastErr = fmt.Errorf("tailscaled.service invalid on this systemd version: %s%s", strings.TrimSpace(string(out)), tailscaledJournalTail())
				break
			}
		}

		for i := 0; i < 12; i++ {
			if tailscaledDaemonReady() {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		log.Printf("Bootstrap: waiting for tailscaled daemon (attempt %d/5)", attempt)
	}

	journal := tailscaledJournalTail()
	if lastErr != nil {
		return fmt.Errorf("%v%s", lastErr, journal)
	}
	return fmt.Errorf("tailscaled daemon not reachable (NeedsLogin before tailscale up is OK; socket missing?)%s", journal)
}

// tailscaledDaemonReady is true when the daemon accepts CLI calls.
// NeedsLogin / not logged in yet is expected before bootstrap runs tailscale up.
func tailscaledDaemonReady() bool {
	if exec.Command("systemctl", "is-active", "--quiet", "tailscaled").Run() != nil {
		return false
	}
	if _, err := os.Stat(tailscaledSocket); err != nil {
		return false
	}

	out, err := exec.Command("tailscale", "status", "--json").CombinedOutput()
	if err == nil {
		return true
	}

	text := strings.ToLower(string(out) + " " + err.Error())
	if strings.Contains(text, "needslogin") ||
		strings.Contains(text, "not logged in") ||
		strings.Contains(text, "logged out") ||
		strings.Contains(text, "nostate") {
		return true
	}

	// Last resort: any stdout from status means the socket works.
	return len(strings.TrimSpace(string(out))) > 0
}

func tailscaledJournalTail() string {
	out, err := exec.Command("journalctl", "-u", "tailscaled", "-n", "20", "--no-pager").CombinedOutput()
	if err != nil {
		return ""
	}
	return "\n--- journalctl -u tailscaled ---\n" + strings.TrimSpace(string(out))
}

func readConfigAsset(name string) ([]byte, error) {
	candidates := []string{
		filepath.Join(ScriptsDir(), "..", "configs", name),
		filepath.Join("/opt/tailscale-raspberry-router/configs", name),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(filepath.Clean(path))
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("missing %s in configs/", name)
}

func tailscaleStaticURL(arch string) string {
	if arch == "armv6" {
		return "https://pkgs.tailscale.com/stable/tailscale_1.62.0_arm.tgz"
	}
	return "https://pkgs.tailscale.com/stable/tailscale_latest_arm.tgz"
}

func tailscaleBinaryWorks() bool {
	if !commandExists("tailscale") {
		return false
	}
	out, err := exec.Command("tailscale", "version").CombinedOutput()
	if err != nil {
		log.Printf("Bootstrap: existing tailscale binary not usable: %v: %s", err, strings.TrimSpace(string(out)))
		return false
	}
	return true
}
