package handlers

import (
	"fmt"
	"net"
	"strings"
)

// SuggestedLANConfig is auto-generated to avoid overlapping the WAN subnet.
type SuggestedLANConfig struct {
	Address     string `json:"address"`
	Prefix      int    `json:"prefix"`
	DHCPStart   string `json:"dhcp_start"`
	DHCPEnd     string `json:"dhcp_end"`
	Reason      string `json:"reason"`
	WANAddress  string `json:"wan_address,omitempty"`
	WANPrefix   int    `json:"wan_prefix,omitempty"`
}

func getInterfaceIPv4CIDR(iface string) (ip string, prefix int) {
	if iface == "" {
		return "", 0
	}
	output, err := execCommandOutput("ip", "-o", "-4", "addr", "show", "dev", iface)
	if err != nil {
		return "", 0
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		parts := strings.Split(fields[3], "/")
		if len(parts) != 2 {
			continue
		}
		p := 24
		fmt.Sscanf(parts[1], "%d", &p)
		return parts[0], p
	}
	return "", 0
}

func SuggestLANSubnet(wanInterface string) SuggestedLANConfig {
	wanIP, wanPrefix := getInterfaceIPv4CIDR(wanInterface)

	candidates := []SuggestedLANConfig{
		{Address: "192.168.50.1", Prefix: 24, DHCPStart: "192.168.50.100", DHCPEnd: "192.168.50.200"},
		{Address: "192.168.51.1", Prefix: 24, DHCPStart: "192.168.51.100", DHCPEnd: "192.168.51.200"},
		{Address: "10.10.0.1", Prefix: 24, DHCPStart: "10.10.0.100", DHCPEnd: "10.10.0.200"},
		{Address: "172.31.255.1", Prefix: 24, DHCPStart: "172.31.255.100", DHCPEnd: "172.31.255.200"},
	}

	suggestion := candidates[0]
	suggestion.Reason = "Default private LAN subnet"

	if wanIP != "" {
		suggestion.WANAddress = wanIP
		suggestion.WANPrefix = wanPrefix
		for _, c := range candidates {
			if !subnetsOverlap(wanIP, wanPrefix, c.Address, c.Prefix) {
				c.Reason = fmt.Sprintf("Does not overlap WAN %s/%d on %s", wanIP, wanPrefix, wanInterface)
				c.WANAddress = wanIP
				c.WANPrefix = wanPrefix
				return c
			}
		}
		suggestion.Reason = fmt.Sprintf("WAN uses %s/%d. Picked fallback subnet; verify before apply", wanIP, wanPrefix)
	} else if wanInterface != "" {
		suggestion.Reason = fmt.Sprintf("WAN %s has no IPv4 yet (DHCP pending). Safe default LAN subnet", wanInterface)
	} else {
		suggestion.Reason = "No default route detected. Safe default LAN subnet"
	}

	return suggestion
}

func subnetsOverlap(aIP string, aPrefix int, bIP string, bPrefix int) bool {
	a := networkAddr(aIP, aPrefix)
	b := networkAddr(bIP, bPrefix)
	if a == nil || b == nil {
		return false
	}

	aNet := &net.IPNet{IP: a, Mask: net.CIDRMask(aPrefix, 32)}
	bNet := &net.IPNet{IP: b, Mask: net.CIDRMask(bPrefix, 32)}
	return aNet.Contains(bNet.IP) || bNet.Contains(aNet.IP)
}

func networkAddr(ip string, prefix int) net.IP {
	parsed := net.ParseIP(ip)
	if parsed == nil || prefix <= 0 || prefix > 32 {
		return nil
	}
	ipv4 := parsed.To4()
	if ipv4 == nil {
		return nil
	}
	mask := net.CIDRMask(prefix, 32)
	return ipv4.Mask(mask)
}

func validateLANDoesNotOverlapWAN(wanIface, lanAddr string, lanPrefix int) error {
	wanIP, wanPrefix := getInterfaceIPv4CIDR(wanIface)
	if wanIP == "" {
		return nil
	}
	if subnetsOverlap(wanIP, wanPrefix, lanAddr, lanPrefix) {
		return fmt.Errorf("LAN %s/%d overlaps WAN %s/%d on %s. Choose a different LAN subnet",
			lanAddr, lanPrefix, wanIP, wanPrefix, wanIface)
	}
	return nil
}

func getManagementAccessIPs() []string {
	output, err := execCommandOutput("ip", "-o", "-4", "addr", "show")
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var ips []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		iface := strings.TrimSuffix(fields[1], ":")
		if shouldSkipInterface(iface) {
			continue
		}
		ip := strings.Split(fields[3], "/")[0]
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		ips = append(ips, ip)
	}
	return ips
}
