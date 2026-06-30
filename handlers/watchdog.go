package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func enableHealthWatch() error {
	scripts := map[string]string{
		"router-health-check.sh": "/usr/local/bin/router-health-check.sh",
		"router-health-watch.sh": "/usr/local/bin/router-health-watch.sh",
	}
	srcDir := ScriptsDir()

	for name, dest := range scripts {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0755); err != nil {
			return err
		}
	}

	serviceSrc := findConfigFile("tailscale-router-health-watch.service")
	if serviceSrc == "" {
		log.Println("Health watch service file not found, skipping")
		return nil
	}

	data, err := os.ReadFile(serviceSrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile("/etc/systemd/system/tailscale-router-health-watch.service", data, 0644); err != nil {
		return err
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "tailscale-router-health-watch.service").Run()
	return exec.Command("systemctl", "restart", "tailscale-router-health-watch.service").Run()
}

// enableHardwareWatchdog configures the Pi hardware watchdog when available.
// Returns a non-empty warning string on partial failure (bootstrap continues).
func enableHardwareWatchdog() string {
	exec.Command("modprobe", "bcm2835_wdt").Run()
	appendUniqueBlock("/etc/modules", "bcm2835_wdt", "bcm2835_wdt\n", func() error { return nil })

	if !commandExists("watchdog") {
		return "watchdog package not installed (optional; software health watch still active)"
	}

	if _, err := os.Stat("/dev/watchdog"); err != nil {
		return "no /dev/watchdog on this board (optional hardware watchdog skipped)"
	}

	confSrc := findConfigFile("watchdog-tailscale-router.conf")
	if confSrc == "" {
		return "watchdog config template not found (skipped)"
	}

	confData, err := os.ReadFile(confSrc)
	if err != nil {
		return fmt.Sprintf("read watchdog config: %v", err)
	}

	marker := "test-binary = /usr/local/bin/router-health-check.sh"
	if err := appendUniqueBlock("/etc/watchdog.conf", marker, "\n# tailscale-raspberry-router\n"+string(confData), func() error { return nil }); err != nil {
		return fmt.Sprintf("update watchdog.conf: %v", err)
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "watchdog").Run()
	if err := exec.Command("systemctl", "restart", "watchdog").Run(); err != nil {
		journal := watchdogJournalTail()
		return fmt.Sprintf("watchdog service did not start: %v%s", err, journal)
	}

	if !watchdogServiceActive() {
		return "watchdog service is not active after restart" + watchdogJournalTail()
	}

	return ""
}

func watchdogServiceActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "watchdog").Run() == nil
}

func watchdogJournalTail() string {
	out, err := exec.Command("journalctl", "-u", "watchdog", "-n", "8", "--no-pager").CombinedOutput()
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	return "\n" + text
}

func findConfigFile(name string) string {
	candidates := []string{
		filepath.Join(ScriptsDir(), "..", "configs", name),
		filepath.Join("/opt/tailscale-raspberry-router/configs", name),
	}
	for _, path := range candidates {
		if _, err := os.Stat(filepath.Clean(path)); err == nil {
			return filepath.Clean(path)
		}
	}
	return ""
}
