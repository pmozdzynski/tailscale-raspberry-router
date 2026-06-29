package handlers

import (
	"log"
	"os/exec"
)

const (
	routerForwardChain = "TS-ROUTER-FWD"
	routerNatChain     = "TS-ROUTER-NAT"
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
