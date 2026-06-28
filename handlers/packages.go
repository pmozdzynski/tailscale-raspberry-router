package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PackageSnapshot reports whether common dependencies are present.
type PackageSnapshot struct {
	AptAvailable bool `json:"apt_available"`
	Dnsmasq      bool `json:"dnsmasq"`
	Tailscale    bool `json:"tailscale"`
	Iptables     bool `json:"iptables"`
}

func GetPackageSnapshot() PackageSnapshot {
	return PackageSnapshot{
		AptAvailable: commandExists("apt-get"),
		Dnsmasq:      commandExists("dnsmasq"),
		Tailscale:    commandExists("tailscale"),
		Iptables:     commandExists("iptables"),
	}
}

func installSystemPackages() error {
	required := []string{
		"dnsmasq",
		"iptables",
		"iproute2",
		"curl",
		"ca-certificates",
	}

	missing := filterMissingBinaries(required)
	if len(missing) == 0 {
		log.Println("Bootstrap: all required packages already present")
		return nil
	}

	if !commandExists("apt-get") {
		return fmt.Errorf("missing packages %v and apt-get is unavailable — install them manually on this OS", missing)
	}

	log.Printf("Bootstrap: installing packages via apt: %v", missing)
	if out, err := exec.Command("apt-get", "update").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update: %v: %s", err, strings.TrimSpace(string(out)))
	}

	args := append([]string{"install", "-y"}, missing...)
	if out, err := exec.Command("apt-get", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get install: %v: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func installTailscaleIfMissing() error {
	if commandExists("tailscale") {
		exec.Command("systemctl", "enable", "tailscaled").Run()
		exec.Command("systemctl", "start", "tailscaled").Run()
		return nil
	}

	if !commandExists("curl") {
		return fmt.Errorf("curl is required to install Tailscale automatically")
	}

	log.Println("Bootstrap: installing Tailscale")
	if out, err := exec.Command("sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh").CombinedOutput(); err == nil {
		exec.Command("systemctl", "enable", "tailscaled").Run()
		exec.Command("systemctl", "start", "tailscaled").Run()
		if commandExists("tailscale") {
			return nil
		}
		log.Printf("Tailscale install script output: %s", strings.TrimSpace(string(out)))
	}

	arch := detectMachineArch()
	if arch == "armv6" || arch == "armv7" {
		log.Println("Bootstrap: falling back to Tailscale static ARM package")
		tmp, err := os.MkdirTemp("", "tailscale-install-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		tgz := filepath.Join(tmp, "tailscale.tgz")
		if out, err := exec.Command("curl", "-fsSL", "-o", tgz, "https://pkgs.tailscale.com/stable/tailscale_latest_arm.tgz").CombinedOutput(); err != nil {
			return fmt.Errorf("download tailscale: %v: %s", err, string(out))
		}
		if out, err := exec.Command("tar", "-xzf", tgz, "-C", tmp).CombinedOutput(); err != nil {
			return fmt.Errorf("extract tailscale: %v: %s", err, string(out))
		}

		entries, err := os.ReadDir(tmp)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "tailscale_") {
				continue
			}
			dir := filepath.Join(tmp, entry.Name())
			for _, name := range []string{"tailscale", "tailscaled"} {
				src := filepath.Join(dir, name)
				data, err := os.ReadFile(src)
				if err != nil {
					return fmt.Errorf("read %s: %w", src, err)
				}
				if err := os.WriteFile(filepath.Join("/usr/sbin", name), data, 0755); err != nil {
					return err
				}
			}
			break
		}

		exec.Command("systemctl", "enable", "tailscaled").Run()
		exec.Command("systemctl", "start", "tailscaled").Run()
		if commandExists("tailscale") {
			return nil
		}
	}

	return fmt.Errorf("could not install Tailscale automatically — install it manually and re-run setup")
}

func filterMissingBinaries(packages []string) []string {
	binaryForPackage := map[string]string{
		"dnsmasq":         "dnsmasq",
		"iptables":        "iptables",
		"iproute2":        "ip",
		"curl":            "curl",
		"ca-certificates": "update-ca-certificates",
	}

	var missing []string
	for _, pkg := range packages {
		bin := binaryForPackage[pkg]
		if bin == "" {
			bin = pkg
		}
		if !commandExists(bin) {
			missing = append(missing, pkg)
		}
	}
	return missing
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectMachineArch() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "unknown"
	}
	switch strings.TrimSpace(string(out)) {
	case "armv6l":
		return "armv6"
	case "armv7l":
		return "armv7"
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return strings.TrimSpace(string(out))
	}
}

func execCommandOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}
