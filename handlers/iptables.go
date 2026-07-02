package handlers

import (
	"log"
	"os/exec"
)

const (
	routerForwardChain = "TS-ROUTER-FWD"
	routerNatChain     = "TS-ROUTER-NAT"
	routerMSSChain     = "TS-ROUTER-MSS"
)

func ensureRouterIPTablesChains() {
	exec.Command("iptables", "-N", routerForwardChain).Run()
	exec.Command("iptables", "-t", "nat", "-N", routerNatChain).Run()

	if exec.Command("iptables", "-C", "FORWARD", "-j", routerForwardChain).Run() != nil {
		exec.Command("iptables", "-A", "FORWARD", "-j", routerForwardChain).Run()
	}
	if exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-j", routerNatChain).Run() != nil {
		exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-j", routerNatChain).Run()
	}
}

func flushRouterIPTablesRules() {
	ensureRouterIPTablesChains()
	exec.Command("iptables", "-F", routerForwardChain).Run()
	exec.Command("iptables", "-t", "nat", "-F", routerNatChain).Run()
	clearRouterMSSClamp()
}

func ensureRouterMSSChain() {
	exec.Command("iptables", "-t", "mangle", "-N", routerMSSChain).Run()
	if exec.Command("iptables", "-t", "mangle", "-C", "FORWARD", "-j", routerMSSChain).Run() != nil {
		exec.Command("iptables", "-t", "mangle", "-A", "FORWARD", "-j", routerMSSChain).Run()
	}
}

// ensureRouterMSSClamp prevents LAN TCP sessions from exceeding tailscale0 MTU (1280).
func ensureRouterMSSClamp(outIface string) {
	ensureRouterMSSChain()
	args := []string{
		"-t", "mangle", "-C", routerMSSChain,
		"-o", outIface, "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN",
		"-j", "TCPMSS", "--clamp-mss-to-pmtu",
	}
	if exec.Command("iptables", args...).Run() == nil {
		return
	}
	appendArgs := []string{
		"-t", "mangle", "-A", routerMSSChain,
		"-o", outIface, "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN",
		"-j", "TCPMSS", "--clamp-mss-to-pmtu",
	}
	if err := exec.Command("iptables", appendArgs...).Run(); err != nil {
		log.Printf("iptables MSS clamp for %s: %v", outIface, err)
	} else {
		log.Printf("iptables MSS clamp enabled on %s", outIface)
	}
}

func clearRouterMSSClamp() {
	exec.Command("iptables", "-t", "mangle", "-F", routerMSSChain).Run()
}

func appendRouterForwardRule(args ...string) {
	ensureRouterIPTablesChains()
	cmdArgs := append([]string{"-A", routerForwardChain}, args...)
	if err := exec.Command("iptables", cmdArgs...).Run(); err != nil {
		log.Printf("iptables forward rule failed: %v", err)
	}
}

func appendRouterNatMasquerade(outIface string) {
	ensureRouterIPTablesChains()
	if err := exec.Command("iptables", "-t", "nat", "-A", routerNatChain, "-o", outIface, "-j", "MASQUERADE").Run(); err != nil {
		log.Printf("iptables NAT rule failed for %s: %v", outIface, err)
	}
}
