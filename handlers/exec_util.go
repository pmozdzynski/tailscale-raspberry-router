package handlers

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

func runSystemctlWithTimeout(timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

// startDashboardAfterSetup runs post-setup.sh in the background (bootstrap stays on :4000).
func startDashboardAfterSetup() {
	cmd := exec.Command("/usr/local/bin/tailscale-router-post-setup.sh")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Printf("Bootstrap: post-setup script failed to start: %v", err)
	}
}
