package handlers

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// Struct to store exit node information
type ExitNode struct {
	IP       string
	Hostname string
	Active   bool
}

// Get available Tailscale exit nodes
func GetExitNodes() (map[string]ExitNode, error) {
	nodes := make(map[string]ExitNode)
	privateNodes := make(map[string]ExitNode) // Stores private nodes
	mullvadNodes := make(map[string]ExitNode) // Stores Mullvad nodes

	cmd := exec.Command("tailscale", "exit-node", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)

		// Ignore empty lines and headers
		if len(fields) < 5 || strings.Contains(line, "HOSTNAME") || strings.Contains(line, "To (have") {
			continue
		}

		ip, hostname, country, city, status := fields[0], fields[1], fields[2], fields[3], fields[4]
		isOffline := strings.Contains(status, "offline")

		// Ignore malformed entries that do not have a valid IP address
		if !strings.Contains(ip, "100.") {
			continue
		}

		// Private nodes (self-hosted) have no country/city info
		if country == "-" && city == "-" {
			privateNodes[hostname] = ExitNode{
				IP:       ip,
				Hostname: hostname,
				Active:   !isOffline,
			}
		} else {
			// Mullvad nodes
			displayName := fmt.Sprintf("%s (%s, %s)", hostname, country, city)
			mullvadNodes[displayName] = ExitNode{
				IP:       ip,
				Hostname: displayName,
				Active:   !isOffline,
			}
		}
	}

	// Merge valid private and Mullvad nodes
	for k, v := range privateNodes {
		nodes[k] = v
	}
	for k, v := range mullvadNodes {
		nodes[k] = v
	}

	return nodes, nil
}

// Send an ARP ping to the exit node
func sendArpPing(ip string) {
	cmd := exec.Command("ping", "-c", "2", "-W", "1", ip)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ARP ping failed for %s: %s", ip, string(output))
	}
}

// Enable Tailscale exit node
func SetTailscaleExitNode(node string) error {
	mu.Lock()
	defer mu.Unlock()

	exitNodes, err := GetExitNodes()
	if err != nil {
		return err
	}

	exitNode, exists := exitNodes[node]
	if !exists {
		return fmt.Errorf("exit node not found")
	}

	exec.Command("iptables", "-t", "nat", "-F").Run()
	exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	err = exec.Command("tailscale", "set", "--exit-node="+exitNode.IP).Run()
	if err != nil {
		return err
	}

	// Detect the active internet interface
	interfaceName, err := GetActiveInternetInterface()
	if err != nil {
		log.Println("Error detecting active interface, defaulting to eth0")
		interfaceName = "eth0"
	}

	log.Println("Using interface for NAT:", interfaceName) // Log to stderr

	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", interfaceName, "-j", "MASQUERADE").Run()

	go sendArpPing(exitNode.IP)

	CurrentMode = "tailscale:" + node
	SaveMode(CurrentMode)

	return nil
}

// Disable Tailscale exit node
func DisableTailscaleExitNode() error {
	mu.Lock()
	defer mu.Unlock()

	exec.Command("iptables", "-t", "nat", "-F").Run()
	err := exec.Command("tailscale", "set", "--exit-node=").Run()
	if err != nil {
		return err
	}

	// Detect the active internet interface
	interfaceName, err := GetActiveInternetInterface()
	if err != nil {
		log.Println("Error detecting active interface, defaulting to eth0")
		interfaceName = "eth0"
	}

	log.Println("Using interface for NAT:", interfaceName) // Log to stderr

	// Apply NAT masquerading using the detected interface
	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", interfaceName, "-j", "MASQUERADE").Run()

	go sendArpPing("1.1.1.1")

	CurrentMode = "direct"
	SaveMode(CurrentMode)

	return nil
}
