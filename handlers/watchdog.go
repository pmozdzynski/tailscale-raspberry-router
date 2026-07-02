package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// enableHardwareWatchdog configures the Pi hardware watchdog when available.
// Not used during web setup; kept for manual / future use.
func enableHardwareWatchdog() string {
	exec.Command("modprobe", "bcm2835_wdt").Run()
	appendUniqueBlock("/etc/modules", "bcm2835_wdt", "bcm2835_wdt\n", func() error { return nil })

	if !commandExists("watchdog") {
		return "watchdog package not installed"
	}
	if _, err := os.Stat("/dev/watchdog"); err != nil {
		return "no /dev/watchdog on this board"
	}

	srcDir := ScriptsDir()
	testSrc := filepath.Join(srcDir, "router-watchdog-test.sh")
	if data, err := os.ReadFile(testSrc); err == nil {
		_ = os.WriteFile("/usr/local/bin/router-watchdog-test.sh", data, 0755)
	}

	confSrc := findConfigFile("watchdog-tailscale-router.conf")
	if confSrc == "" {
		return "watchdog config template not found"
	}
	confData, err := os.ReadFile(confSrc)
	if err != nil {
		return fmt.Sprintf("read watchdog config: %v", err)
	}

	if err := os.MkdirAll("/etc/watchdog.d", 0755); err == nil {
		if err := os.WriteFile("/etc/watchdog.d/tailscale-router", confData, 0644); err != nil {
			return fmt.Sprintf("write watchdog drop-in: %v", err)
		}
	}

	if out, err := exec.Command("/usr/local/bin/router-watchdog-test.sh").CombinedOutput(); err != nil {
		return fmt.Sprintf("watchdog preflight: %v: %s", err, strings.TrimSpace(string(out)))
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "watchdog").Run()
	if err := exec.Command("systemctl", "restart", "watchdog").Run(); err != nil {
		log.Printf("hardware watchdog: %v", err)
		return "watchdog service did not start"
	}
	return ""
}
