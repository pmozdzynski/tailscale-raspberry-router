package handlers

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// bootstrapTailscaleArgs returns explicit tailscale up flags for first-time / re-setup.
// All non-default settings must be listed for newer Tailscale versions.
func bootstrapTailscaleArgs(hostname, authKey string) []string {
	args := []string{
		"up",
		"--advertise-exit-node=false",
		"--accept-routes=false",
		"--accept-dns=false",
		"--exit-node=",
		"--hostname=" + hostname,
	}
	if authKey != "" {
		args = append(args, "--auth-key="+authKey)
	}
	return args
}

func tailscaleNeedsExplicitFlags(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "non-default flags") ||
		strings.Contains(msg, "requires mentioning all")
}

func clearStaleTailscaleRouterState() {
	if err := clearTailscaleExitNode(); err != nil {
		log.Printf("Bootstrap: clear exit node (non-fatal): %v", err)
	}
	// Leftover from a previous router session; ignore errors if unsupported.
	exec.Command("tailscale", "set", "--exit-node-allow-lan-access=false").Run()
}

func applyTailscaleSettingsInPlace(hostname string) error {
	clearStaleTailscaleRouterState()
	out, err := exec.Command("tailscale", "set",
		"--advertise-exit-node=false",
		"--accept-routes=false",
		"--accept-dns=false",
		"--hostname="+hostname,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	log.Println("Bootstrap: updated existing Tailscale session via tailscale set")
	return nil
}

// RunBootstrapTailscale connects or reconfigures Tailscale during web setup.
// Handles partial installs where Tailscale already has exit-node settings.
func RunBootstrapTailscale(hostname, authKey string) error {
	if authKey == "" && !getTailscaleSnapshot().Connected {
		return fmt.Errorf("tailscale auth key is required on fresh installs")
	}

	clearStaleTailscaleRouterState()

	args := bootstrapTailscaleArgs(hostname, authKey)
	out, err := exec.Command("tailscale", args...).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))

	if tailscaleNeedsExplicitFlags(msg) {
		log.Println("Bootstrap: tailscale has leftover settings; retrying with --reset")
		resetArgs := append([]string{"up", "--reset"}, args[1:]...)
		out, err = exec.Command("tailscale", resetArgs...).CombinedOutput()
		if err == nil {
			log.Println("Bootstrap: tailscale up --reset succeeded")
			return nil
		}
		msg = strings.TrimSpace(string(out))
	}

	if authKey == "" && getTailscaleSnapshot().Connected {
		if err := applyTailscaleSettingsInPlace(hostname); err == nil {
			return nil
		}
	}

	if authKey == "" && strings.Contains(strings.ToLower(msg), "login") {
		return fmt.Errorf("tailscale requires login. Provide an auth key in setup or run: sudo tailscale up")
	}

	if msg != "" {
		return fmt.Errorf("%v: %s", err, msg)
	}
	return err
}
