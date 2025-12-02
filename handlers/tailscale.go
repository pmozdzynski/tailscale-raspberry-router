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

// Flush ARP cache to speed up routing changes
func flushARPCache() {
	exec.Command("ip", "-s", "-s", "neigh", "flush", "all").Run()
	log.Println("ARP cache flushed")
}

// Flush routing cache to force route recalculation
func flushRoutingCache() {
	exec.Command("ip", "route", "flush", "cache").Run()
	log.Println("Routing cache flushed")
}

// Send gratuitous ARP on LAN interfaces to notify clients of changes
func sendGratuitousARP(interfaceName string) {
	// Get the IP address of the interface
	cmd := exec.Command("sh", "-c", fmt.Sprintf("ip -4 addr show %s | grep 'inet ' | awk '{print $2}' | cut -d/ -f1", interfaceName))
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Could not get IP for interface %s: %v", interfaceName, err)
		return
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		log.Printf("No IP address found for interface %s", interfaceName)
		return
	}

	// Send gratuitous ARP using arping (if available)
	// -U: Unsolicited ARP (gratuitous)
	// -c 2: Send 2 packets
	// -I: Interface to use
	cmd = exec.Command("arping", "-U", "-c", "2", "-I", interfaceName, ip)
	err = cmd.Run()
	if err != nil {
		// arping might not be installed, try alternative method using ip neigh
		log.Printf("arping not available, using alternative method for %s", interfaceName)
		// Alternative: use ip neigh to update ARP table
		exec.Command("ip", "neigh", "flush", "dev", interfaceName).Run()
	} else {
		log.Printf("Sent gratuitous ARP on %s (%s)", interfaceName, ip)
	}
}

// Speed up routing changes by flushing caches and sending ARP announcements
func speedUpRoutingChanges() {
	// Flush ARP and routing caches
	flushARPCache()
	flushRoutingCache()

	// Get LAN interfaces and send gratuitous ARP
	lanInterfaces, err := GetLANInterfaces()
	if err != nil {
		log.Printf("Warning: Could not detect LAN interfaces for ARP announcements: %v", err)
		return
	}

	// Send gratuitous ARP on each LAN interface
	for _, lanIface := range lanInterfaces {
		go sendGratuitousARP(lanIface)
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

	// Flush existing NAT and FORWARD rules
	exec.Command("iptables", "-t", "nat", "-F").Run()
	exec.Command("iptables", "-F", "FORWARD").Run()

	// Enable IP forwarding
	exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	// Ensure connection tracking modules are loaded
	exec.Command("modprobe", "nf_conntrack").Run()
	exec.Command("modprobe", "nf_conntrack_ipv4").Run()

	// Set connection tracking timeouts (helps with Tailscale connection tracking)
	exec.Command("sysctl", "-w", "net.netfilter.nf_conntrack_tcp_timeout_established=432000").Run()
	exec.Command("sysctl", "-w", "net.netfilter.nf_conntrack_tcp_timeout_time_wait=120").Run()

	// Set Tailscale exit node
	err = exec.Command("tailscale", "set", "--exit-node="+exitNode.IP).Run()
	if err != nil {
		return err
	}

	// When using Tailscale exit node, traffic goes through tailscale0 interface
	// Apply NAT masquerading on tailscale0 for LAN clients
	log.Println("Using tailscale0 interface for NAT (exit node mode)")

	// NAT masquerading for traffic going out through tailscale0
	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", "tailscale0", "-j", "MASQUERADE").Run()

	// Get LAN interfaces and set up forwarding rules
	lanInterfaces, err := GetLANInterfaces()
	if err != nil {
		log.Printf("Warning: Could not detect LAN interfaces: %v", err)
		log.Println("Using permissive forwarding rules (allowing all interfaces)")
		// Allow forwarding from any interface to tailscale0 (NEW and ESTABLISHED connections)
		exec.Command("iptables", "-A", "FORWARD", "-o", "tailscale0", "-m", "state", "--state", "NEW,RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
		// Allow return traffic from tailscale0
		exec.Command("iptables", "-A", "FORWARD", "-i", "tailscale0", "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	} else {
		// Set up forwarding rules for each LAN interface
		for _, lanIface := range lanInterfaces {
			log.Printf("Setting up forwarding from %s to tailscale0", lanIface)
			// Allow forwarding from LAN interface to tailscale0 (NEW and ESTABLISHED connections)
			exec.Command("iptables", "-A", "FORWARD", "-i", lanIface, "-o", "tailscale0", "-m", "state", "--state", "NEW,RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
			// Allow return traffic from tailscale0 to LAN interface
			exec.Command("iptables", "-A", "FORWARD", "-i", "tailscale0", "-o", lanIface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
		}
	}

	go sendArpPing(exitNode.IP)

	// Speed up routing changes by flushing caches and sending ARP announcements
	speedUpRoutingChanges()

	CurrentMode = "tailscale:" + node
	SaveMode(CurrentMode)

	return nil
}

// Disable Tailscale exit node
func DisableTailscaleExitNode() error {
	mu.Lock()
	defer mu.Unlock()

	// Flush existing NAT and FORWARD rules
	exec.Command("iptables", "-t", "nat", "-F").Run()
	exec.Command("iptables", "-F", "FORWARD").Run()

	err := exec.Command("tailscale", "set", "--exit-node=").Run()
	if err != nil {
		return err
	}

	// Enable IP forwarding
	exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	// Ensure connection tracking modules are loaded
	exec.Command("modprobe", "nf_conntrack").Run()
	exec.Command("modprobe", "nf_conntrack_ipv4").Run()

	// Detect the active internet interface
	interfaceName, err := GetActiveInternetInterface()
	if err != nil {
		log.Println("Error detecting active interface, defaulting to eth0")
		interfaceName = "eth0"
	}

	log.Println("Using interface for NAT:", interfaceName) // Log to stderr

	// Apply NAT masquerading using the detected interface
	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", interfaceName, "-j", "MASQUERADE").Run()

	// Get LAN interfaces and set up forwarding rules
	lanInterfaces, err := GetLANInterfaces()
	if err != nil {
		log.Printf("Warning: Could not detect LAN interfaces: %v", err)
		log.Println("Using permissive forwarding rules (allowing all interfaces)")
		// Allow forwarding from any interface to WAN (NEW and ESTABLISHED connections)
		exec.Command("iptables", "-A", "FORWARD", "-o", interfaceName, "-m", "state", "--state", "NEW,RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
		// Allow return traffic from WAN
		exec.Command("iptables", "-A", "FORWARD", "-i", interfaceName, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	} else {
		// Set up forwarding rules for each LAN interface
		for _, lanIface := range lanInterfaces {
			log.Printf("Setting up forwarding from %s to %s", lanIface, interfaceName)
			// Allow forwarding from LAN interface to WAN interface (NEW and ESTABLISHED connections)
			exec.Command("iptables", "-A", "FORWARD", "-i", lanIface, "-o", interfaceName, "-m", "state", "--state", "NEW,RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
			// Allow return traffic from WAN to LAN interface
			exec.Command("iptables", "-A", "FORWARD", "-i", interfaceName, "-o", lanIface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
		}
	}

	go sendArpPing("1.1.1.1")

	// Speed up routing changes by flushing caches and sending ARP announcements
	speedUpRoutingChanges()

	CurrentMode = "direct"
	SaveMode(CurrentMode)

	return nil
}
