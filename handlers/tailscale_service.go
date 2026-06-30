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
	tailscaledUnitPath  = "/etc/systemd/system/tailscaled.service"
	tailscaledDefaults  = "/etc/default/tailscaled"
	tailscaledSocket    = "/run/tailscale/tailscaled.sock"
)

func ensureTailscaledServiceInstalled() error {
	if err := os.MkdirAll("/etc/systemd/system", 0755); err != nil {
		return err
	}

	if _, err := os.Stat(tailscaledUnitPath); os.IsNotExist(err) {
		data, err := readConfigAsset("tailscaled.service")
		if err != nil {
			return fmt.Errorf("tailscaled.service template: %w", err)
		}
		if err := os.WriteFile(tailscaledUnitPath, data, 0644); err != nil {
			return err
		}
		log.Println("Bootstrap: installed tailscaled.service")
	}

	if _, err := os.Stat(tailscaledDefaults); os.IsNotExist(err) {
		data, err := readConfigAsset("tailscaled.default")
		if err != nil {
			return fmt.Errorf("tailscaled defaults template: %w", err)
		}
		if detectMachineArch() == "armv6" {
			data = []byte(`FLAGS="--tun=userspace-networking"
`)
			log.Println("Bootstrap: enabling Tailscale userspace networking for ARMv6")
		}
		if err := os.WriteFile(tailscaledDefaults, data, 0644); err != nil {
			return err
		}
	} else if detectMachineArch() == "armv6" {
		content, _ := os.ReadFile(tailscaledDefaults)
		if !strings.Contains(string(content), "userspace-networking") {
			if err := os.WriteFile(tailscaledDefaults, []byte(`FLAGS="--tun=userspace-networking"
`), 0644); err != nil {
				return err
			}
			log.Println("Bootstrap: updated /etc/default/tailscaled for ARMv6")
		}
	}

	exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func ensureTailscaledRunning() error {
	if err := ensureTailscaledServiceInstalled(); err != nil {
		return err
	}

	if out, err := exec.Command("tailscaled", "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("tailscaled binary cannot run on this CPU (ARMv6 may need an older Tailscale build): %v: %s",
			err, strings.TrimSpace(string(out)))
	}

	exec.Command("systemctl", "enable", "tailscaled").Run()

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		out, err := exec.Command("systemctl", "restart", "tailscaled").CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("systemctl restart tailscaled: %v: %s", err, strings.TrimSpace(string(out)))
		}

		for i := 0; i < 10; i++ {
			if _, err := os.Stat(tailscaledSocket); err == nil {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		log.Printf("Bootstrap: waiting for tailscaled socket (attempt %d/5)", attempt)
	}

	journal := tailscaledJournalTail()
	if lastErr != nil {
		return fmt.Errorf("%v%s", lastErr, journal)
	}
	return fmt.Errorf("tailscaled did not create %s%s", tailscaledSocket, journal)
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
