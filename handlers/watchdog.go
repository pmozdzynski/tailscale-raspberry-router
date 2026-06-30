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
		"router-health-check.sh":  "/usr/local/bin/router-health-check.sh",
		"router-health-watch.sh":  "/usr/local/bin/router-health-watch.sh",
		"router-watchdog-test.sh": "/usr/local/bin/router-watchdog-test.sh",
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

	// Install lite test script (may already exist from health watch step).
	srcDir := ScriptsDir()
	testSrc := filepath.Join(srcDir, "router-watchdog-test.sh")
	if data, err := os.ReadFile(testSrc); err == nil {
		_ = os.WriteFile("/usr/local/bin/router-watchdog-test.sh", data, 0755)
	}

	confSrc := findConfigFile("watchdog-tailscale-router.conf")
	if confSrc == "" {
		return "watchdog config template not found (skipped)"
	}

	confData, err := os.ReadFile(confSrc)
	if err != nil {
		return fmt.Sprintf("read watchdog config: %v", err)
	}

	// Prefer drop-in dir so we do not duplicate keys in /etc/watchdog.conf.
	if err := os.MkdirAll("/etc/watchdog.d", 0755); err == nil {
		if err := os.WriteFile("/etc/watchdog.d/tailscale-router", confData, 0644); err != nil {
			return fmt.Sprintf("write /etc/watchdog.d/tailscale-router: %v", err)
		}
	} else {
		marker := "test-binary = /usr/local/bin/router-watchdog-test.sh"
		if err := appendUniqueBlock("/etc/watchdog.conf", marker, "\n# tailscale-raspberry-router\n"+string(confData), func() error { return nil }); err != nil {
			return fmt.Sprintf("update watchdog.conf: %v", err)
		}
	}

	if out, err := exec.Command("/usr/local/bin/router-watchdog-test.sh").CombinedOutput(); err != nil {
		return fmt.Sprintf("watchdog preflight check failed: %v: %s (skipped starting hardware watchdog)", err, strings.TrimSpace(string(out)))
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
