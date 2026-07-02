package handlers

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// scheduleDetachedServiceRestart runs systemctl restart in a child shell after a delay.
func scheduleDetachedServiceRestart(service string, delay time.Duration) {
	sec := int(delay.Seconds())
	if sec < 1 {
		sec = 1
	}
	script := fmt.Sprintf("sleep %d && exec systemctl restart %s", sec, service)
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Printf("Bootstrap: detached restart schedule failed: %v", err)
		time.AfterFunc(delay, func() {
			out, err := exec.Command("systemctl", "restart", service).CombinedOutput()
			if err != nil {
				log.Printf("Bootstrap: systemctl restart %s: %v: %s", service, err, strings.TrimSpace(string(out)))
			}
		})
	}
}

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

// schedulePostBootstrapStart enables and starts the dashboard service plus watcher units.
// Bootstrap on :4000 is never restarted, so the browser keeps its connection.
func schedulePostBootstrapStart(delay time.Duration) {
	sec := int(delay.Seconds())
	if sec < 1 {
		sec = 1
	}
	script := fmt.Sprintf(`sleep %d
systemctl daemon-reload
systemctl enable tailscale-router.service tailscale-dns-watch.service tailscale-router-health-watch.service 2>/dev/null || true
systemctl start tailscale-router.service
sleep 4
systemctl start tailscale-dns-watch.service tailscale-router-health-watch.service 2>/dev/null || true`, sec)
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Printf("Bootstrap: post-setup start script failed: %v", err)
		scheduleDetachedServiceRestart("tailscale-router", delay)
		return
	}
	log.Printf("Bootstrap: scheduled dashboard start in %s", delay)
}
