package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type diagnosticReporter func(line string)

// DiagnosticsRunHandler streams a full router health report (SSE or plain text).
func DiagnosticsRunHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stream := r.URL.Query().Get("stream") == "1" ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	if stream {
		runDiagnosticsStream(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	RunDiagnostics(func(line string) {
		fmt.Fprintln(w, line)
	})
}

func runDiagnosticsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func(line string) {
		payload, _ := json.Marshal(map[string]string{
			"status": "line",
			"detail": line,
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	RunDiagnostics(send)

	done, _ := json.Marshal(map[string]string{
		"status": "done",
		"detail": "Diagnostics complete",
	})
	fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

// DiagnosticsRepairHandler re-applies routing, DNS, and MSS clamp for the saved mode.
func DiagnosticsRepairHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stream := r.URL.Query().Get("stream") == "1" ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	if stream {
		repairRoutingStream(w)
		return
	}

	var err error
	if CurrentMode != "direct" && strings.HasPrefix(CurrentMode, "tailscale:") {
		node := strings.TrimPrefix(CurrentMode, "tailscale:")
		err = SetTailscaleExitNode(node)
	} else {
		err = DisableTailscaleExitNode()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ensureDnsmasqRunning()
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Repair complete. Mode: %s\n", CurrentMode)
}

func repairRoutingStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	emit := func(line string) {
		payload, _ := json.Marshal(map[string]string{
			"status": "line",
			"detail": line,
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		f.Flush()
	}

	emit("=== Repair routing & DNS ===")
	emit(fmt.Sprintf("Current mode: %s", CurrentMode))

	var err error
	if CurrentMode != "direct" && strings.HasPrefix(CurrentMode, "tailscale:") {
		node := strings.TrimPrefix(CurrentMode, "tailscale:")
		emit("Re-applying exit node mode: " + node)
		err = SetTailscaleExitNode(node)
	} else {
		emit("Re-applying direct mode")
		err = DisableTailscaleExitNode()
	}

	if err != nil {
		emit("FAIL: " + err.Error())
		fail, _ := json.Marshal(map[string]string{"status": "error", "detail": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", fail)
		f.Flush()
		return
	}

	emit("Ensuring dnsmasq is running")
	ensureDnsmasqRunning()
	emit("OK: Repair complete")

	done, _ := json.Marshal(map[string]string{"status": "done", "detail": "Repair complete"})
	fmt.Fprintf(w, "data: %s\n\n", done)
	f.Flush()
}

// RunDiagnostics writes a copy-paste friendly health report.
func RunDiagnostics(emit diagnosticReporter) {
	emitSection := func(title string) {
		emit("")
		emit("========== " + title + " ==========")
	}

	emitSection("CONFIG")
	emit(redactedConfigJSON())
	if data, err := os.ReadFile(modeFile); err == nil {
		emit(string(data))
	} else {
		emit("NO MODE FILE")
	}

	emitSection("INTERFACES")
	emit(shellOutput("ip -br link"))
	emit(shellOutput("ip -4 addr show"))

	emitSection("IP FORWARDING")
	emit(shellOutput("sysctl net.ipv4.ip_forward net.ipv4.conf.all.forwarding net.ipv4.conf.all.rp_filter"))

	emitSection("ROUTING")
	emit(shellOutput("ip route show"))
	emit(shellOutput("ip rule show"))

	emitSection("TAILSCALE")
	emit(shellOutput("tailscale status 2>&1 | head -20"))
	if out := shellOutput("tailscale status --json 2>/dev/null"); out != "" {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "ExitNode") {
				emit(line)
			}
		}
	}

	emitSection("ROUTER IPTABLES")
	emit(shellOutput("iptables -L FORWARD -n -v --line-numbers"))
	emit(shellOutput("iptables -L TS-ROUTER-FWD -n -v"))
	emit(shellOutput("iptables -t mangle -L TS-ROUTER-MSS -n -v 2>/dev/null"))
	emit(shellOutput("iptables -t nat -L POSTROUTING -n -v --line-numbers"))
	emit(shellOutput("iptables -t nat -L TS-ROUTER-NAT -n -v"))

	emitSection("TAILSCALE IPTABLES")
	emit(shellOutput("iptables -L ts-forward -n -v 2>/dev/null || echo no ts-forward"))
	emit(shellOutput("iptables -t nat -L ts-postrouting -n -v 2>/dev/null || echo no ts-postrouting"))

	emitSection("DNS / DHCP")
	emit(shellOutput("systemctl is-active dnsmasq tailscaled tailscale-router 2>&1"))
	if data, err := os.ReadFile("/run/tailscale-router/upstream-servers.conf"); err == nil {
		emit(string(data))
	} else {
		emit("NO UPSTREAM FILE")
	}
	emit(shellOutput("grep -E 'dhcp-range|interface|listen-address' /etc/dnsmasq.d/tailscale-router.conf 2>/dev/null"))

	emitSection("CONNTRACK")
	emit(shellOutput("cat /proc/sys/net/netfilter/nf_conntrack_count 2>/dev/null || echo n/a"))
	emit(shellOutput("cat /proc/sys/net/netfilter/nf_conntrack_max 2>/dev/null || echo n/a"))

	emitSection("CONNECTIVITY FROM PI")
	cfg := GetRouterConfig()
	emit(fmt.Sprintf("WAN=%s LAN=%s LAN_IP=%s", cfg.WANInterface, cfg.LANInterface, cfg.LANAddress))
	emit(shellOutput("ping -c 2 -W 2 1.1.1.1"))
	if cfg.WANInterface != "" {
		emit(shellOutput("ping -c 2 -W 2 -I " + cfg.WANInterface + " 1.1.1.1 2>&1"))
	}
	if cfg.LANInterface != "" && cfg.LANAddress != "" {
		emit(shellOutput("ping -c 2 -W 2 -I " + cfg.LANInterface + " " + cfg.LANAddress + " 2>&1"))
	}
	if cfg.LANAddress != "" {
		emit(shellOutput("dig @" + cfg.LANAddress + " google.com +short +time=2 2>&1 | head -5"))
	}

	emitSection("RECENT LOGS")
	emit(shellOutput("journalctl -u tailscale-router -n 40 --no-pager 2>/dev/null"))

	emitSection("SUMMARY")
	emit(diagnosticSummary())
}

func redactedConfigJSON() string {
	cfg := GetRouterConfig()
	cfg.AdminPassword = "***"
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "CONFIG READ ERROR"
	}
	return string(data)
}

func shellOutput(cmdline string) string {
	out, err := exec.Command("sh", "-c", cmdline).CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil && text == "" {
		return err.Error()
	}
	return text
}

func diagnosticSummary() string {
	var issues []string

	if !IsIPForwardingEnabled() {
		issues = append(issues, "FAIL: IPv4 forwarding disabled")
	} else {
		issues = append(issues, "OK: IPv4 forwarding enabled")
	}

	if exec.Command("systemctl", "is-active", "--quiet", "dnsmasq").Run() != nil {
		issues = append(issues, "FAIL: dnsmasq not active")
	} else {
		issues = append(issues, "OK: dnsmasq active")
	}

	natOut := shellOutput("iptables -t nat -L TS-ROUTER-NAT -n -v")
	if !strings.Contains(natOut, "MASQUERADE") {
		issues = append(issues, "FAIL: TS-ROUTER-NAT has no MASQUERADE rule")
	} else if strings.Contains(CurrentMode, "tailscale:") && !strings.Contains(natOut, "tailscale0") {
		issues = append(issues, "WARN: exit node mode but NAT may not target tailscale0")
	} else if CurrentMode == "direct" && IsConfigured() {
		wan := ConfiguredWAN()
		if wan != "" && !strings.Contains(natOut, wan) {
			issues = append(issues, "WARN: direct mode but NAT may not target WAN "+wan)
		}
	} else {
		issues = append(issues, "OK: NAT masquerade present")
	}

	if strings.Contains(CurrentMode, "tailscale:") {
		mss := shellOutput("iptables -t mangle -L TS-ROUTER-MSS -n -v 2>/dev/null")
		if strings.Contains(mss, "TCPMSS") {
			issues = append(issues, "OK: TCP MSS clamp for tailscale0")
		} else {
			issues = append(issues, "WARN: TCP MSS clamp missing (use Repair routing)")
		}
	}

	if !IsTailscaleRunning() {
		issues = append(issues, "FAIL: tailscale not connected")
	} else {
		issues = append(issues, "OK: tailscale connected")
	}

	if len(issues) == 0 {
		return "No issues detected"
	}
	return strings.Join(issues, "\n")
}
