package handlers

import (
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

func enableHardwareWatchdog() error {
	// Raspberry Pi / BCM watchdog module
	exec.Command("modprobe", "bcm2835_wdt").Run()
	appendUniqueBlock("/etc/modules", "bcm2835_wdt", "bcm2835_wdt\n", func() error { return nil })

	if !commandExists("watchdog") {
		if !commandExists("apt-get") {
			log.Println("watchdog package not installed and apt-get unavailable, skipping hardware watchdog")
			return nil
		}
		log.Println("Installing hardware watchdog package")
		exec.Command("apt-get", "update").Run()
		if out, err := exec.Command("apt-get", "install", "-y", "watchdog").CombinedOutput(); err != nil {
			log.Printf("Could not install watchdog package: %v — %s", err, strings.TrimSpace(string(out)))
			return nil
		}
	}

	confSrc := findConfigFile("watchdog-tailscale-router.conf")
	if confSrc == "" {
		log.Println("watchdog config template not found, skipping")
		return nil
	}

	confData, err := os.ReadFile(confSrc)
	if err != nil {
		return err
	}

	marker := "test-binary = /usr/local/bin/router-health-check.sh"
	if err := appendUniqueBlock("/etc/watchdog.conf", marker, "\n# tailscale-raspberry-router\n"+string(confData), func() error { return nil }); err != nil {
		return err
	}

	if !commandExists("watchdog") {
		return nil
	}

	exec.Command("systemctl", "enable", "watchdog").Run()
	if err := exec.Command("systemctl", "restart", "watchdog").Run(); err != nil {
		log.Printf("Warning: could not start watchdog service: %v", err)
	}
	return nil
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
