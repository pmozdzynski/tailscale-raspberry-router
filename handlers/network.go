package handlers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// NetworkInterface describes a local network interface for the setup wizard.
type NetworkInterface struct {
	Name           string   `json:"name"`
	MAC            string   `json:"mac"`
	State          string   `json:"state"`
	IPv4           []string `json:"ipv4"`
	IsDefaultRoute bool     `json:"is_default_route"`
	Kind           string   `json:"kind"` // ethernet, wireless, tunnel, other
}

// RoutingSummary describes the current default route.
type RoutingSummary struct {
	DefaultInterface string `json:"default_interface"`
	DefaultGateway   string `json:"default_gateway"`
	IPForwarding     bool   `json:"ip_forwarding"`
}

// NetworkSnapshot is returned by the setup API.
type NetworkSnapshot struct {
	Interfaces     []NetworkInterface `json:"interfaces"`
	Routing        RoutingSummary     `json:"routing"`
	Tailscale      TailscaleSnapshot  `json:"tailscale"`
	Packages       PackageSnapshot    `json:"packages"`
	SuggestedLAN   SuggestedLANConfig `json:"suggested_lan"`
	ManagementIPs  []string           `json:"management_ips"`
	Hostname       string             `json:"hostname"`
	Configured     bool               `json:"configured"`
	Config         RouterConfig       `json:"config"`
}

type TailscaleSnapshot struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"`
	IPv4      string `json:"ipv4"`
	Hostname  string `json:"hostname"`
	Status    string `json:"status"`
}

func GetNetworkSnapshot() NetworkSnapshot {
	defaultIface, defaultGW := getDefaultRoute()
	ifaces := listNetworkInterfaces(defaultIface)
	suggested := SuggestLANSubnet(defaultIface)

	return NetworkSnapshot{
		Interfaces: ifaces,
		Routing: RoutingSummary{
			DefaultInterface: defaultIface,
			DefaultGateway:   defaultGW,
			IPForwarding:     isIPForwardingEnabled(),
		},
		Tailscale:     getTailscaleSnapshot(),
		Packages:      GetPackageSnapshot(),
		SuggestedLAN:  suggested,
		ManagementIPs: getManagementAccessIPs(),
		Hostname:      getSystemHostname(),
		Configured:    IsConfigured(),
		Config:        GetRouterConfig(),
	}
}

func getSystemHostname() string {
	name, err := execCommandOutput("hostname")
	if err != nil || name == "" {
		return "tailscale-router"
	}
	return name
}

func listNetworkInterfaces(defaultIface string) []NetworkInterface {
	output, err := exec.Command("ip", "-j", "link", "show").Output()
	if err == nil {
		return parseJSONLinks(output, defaultIface)
	}
	return parseTextLinks(defaultIface)
}

type ipLinkJSON struct {
	IfIndex   int    `json:"ifindex"`
	IfName    string `json:"ifname"`
	OperState string `json:"operstate"`
	Address   string `json:"address"`
	LinkType  string `json:"link_type"`
}

func parseJSONLinks(output []byte, defaultIface string) []NetworkInterface {
	var links []ipLinkJSON
	if err := json.Unmarshal(output, &links); err != nil {
		return parseTextLinks(defaultIface)
	}

	ipv4Map := getIPv4ByInterface()
	var result []NetworkInterface

	for _, link := range links {
		name := link.IfName
		if shouldSkipInterface(name) {
			continue
		}

		kind := classifyInterface(name, link.LinkType)
		result = append(result, NetworkInterface{
			Name:           name,
			MAC:            link.Address,
			State:          strings.ToLower(link.OperState),
			IPv4:           ipv4Map[name],
			IsDefaultRoute: name == defaultIface,
			Kind:           kind,
		})
	}

	return result
}

func parseTextLinks(defaultIface string) []NetworkInterface {
	output, err := exec.Command("sh", "-c", "ip -o link show | awk -F': ' '{print $2}'").Output()
	if err != nil {
		return nil
	}

	ipv4Map := getIPv4ByInterface()
	var result []NetworkInterface

	for _, name := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name = strings.TrimSpace(name)
		if shouldSkipInterface(name) {
			continue
		}

		state := "unknown"
		mac := ""
		if line, err := exec.Command("ip", "link", "show", name).Output(); err == nil {
			text := string(line)
			if strings.Contains(text, "state UP") {
				state = "up"
			} else if strings.Contains(text, "state DOWN") {
				state = "down"
			}
			for _, part := range strings.Fields(text) {
				if strings.Count(part, ":") == 5 {
					mac = part
					break
				}
			}
		}

		result = append(result, NetworkInterface{
			Name:           name,
			MAC:            mac,
			State:          state,
			IPv4:           ipv4Map[name],
			IsDefaultRoute: name == defaultIface,
			Kind:           classifyInterface(name, ""),
		})
	}

	return result
}

func getIPv4ByInterface() map[string][]string {
	result := make(map[string][]string)
	output, err := exec.Command("ip", "-o", "-4", "addr", "show").Output()
	if err != nil {
		return result
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		addr := strings.Split(fields[3], "/")[0]
		result[name] = append(result[name], addr)
	}

	return result
}

func getDefaultRoute() (iface, gateway string) {
	output, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", ""
	}

	line := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	fields := strings.Fields(line)
	for i, field := range fields {
		switch field {
		case "dev":
			if i+1 < len(fields) {
				iface = fields[i+1]
			}
		case "via":
			if i+1 < len(fields) {
				gateway = fields[i+1]
			}
		}
	}
	return iface, gateway
}

func detectDefaultRouteInterface() (string, error) {
	iface, _ := getDefaultRoute()
	if iface == "" {
		return "", fmt.Errorf("no default route interface detected")
	}
	return iface, nil
}

func detectLANInterfaces(wan string) ([]string, error) {
	if wan == "" {
		var err error
		wan, err = detectDefaultRouteInterface()
		if err != nil {
			return nil, err
		}
	}

	output, err := exec.Command("sh", "-c", "ip -o link show | awk -F': ' '{print $2}'").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %v", err)
	}

	var lan []string
	for _, iface := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		iface = strings.TrimSpace(iface)
		if shouldSkipInterface(iface) || iface == wan {
			continue
		}
		lan = append(lan, iface)
	}

	return lan, nil
}

func shouldSkipInterface(name string) bool {
	if name == "" || name == "lo" {
		return true
	}
	if strings.HasPrefix(name, "tailscale") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") {
		return true
	}
	return false
}

func classifyInterface(name, linkType string) string {
	lower := strings.ToLower(name + " " + linkType)
	switch {
	case strings.Contains(lower, "wlan") || strings.Contains(lower, "wifi"):
		return "wireless"
	case strings.Contains(lower, "eth") || linkType == "ether":
		return "ethernet"
	case strings.Contains(lower, "tun") || strings.Contains(lower, "tap"):
		return "tunnel"
	default:
		return "other"
	}
}

func isIPForwardingEnabled() bool {
	output, err := exec.Command("sysctl", "-n", "net.ipv4.ip_forward").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

func getTailscaleSnapshot() TailscaleSnapshot {
	snap := TailscaleSnapshot{}
	if _, err := exec.LookPath("tailscale"); err != nil {
		snap.Status = "not installed"
		return snap
	}
	snap.Installed = true

	statusOut, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		snap.Status = strings.TrimSpace(string(statusOut))
		if snap.Status == "" {
			snap.Status = "not connected"
		}
		return snap
	}

	snap.Running = true
	var status struct {
		Self struct {
			Online    bool     `json:"Online"`
			DNSName   string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if json.Unmarshal(statusOut, &status) == nil {
		snap.Connected = status.Self.Online
		snap.Hostname = strings.TrimSuffix(status.Self.DNSName, ".")
		for _, ip := range status.Self.TailscaleIPs {
			if strings.Contains(ip, ".") {
				snap.IPv4 = ip
				break
			}
		}
		snap.Status = "connected"
		return snap
	}

	if exec.Command("tailscale", "status").Run() == nil {
		snap.Connected = true
		snap.Status = "connected"
	}
	return snap
}
