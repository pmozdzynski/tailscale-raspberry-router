package handlers

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	BootstrapPort = "4000"
	RouterPort    = "5000"
)

func bootstrapHostFromRequest(r *http.Request) string {
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		host = "localhost"
	}
	return host
}

// BootstrapSetupURL returns the setup wizard URL for the requesting client.
func BootstrapSetupURL(r *http.Request) string {
	return fmt.Sprintf("http://%s:%s/setup", bootstrapHostFromRequest(r), BootstrapPort)
}

// RouterLoginURL returns the dashboard login URL for a device IP.
func RouterLoginURL(ip string) string {
	return fmt.Sprintf("http://%s:%s/login", ip, RouterPort)
}

// RouterDashboardURL returns the dashboard root URL for a device IP.
func RouterDashboardURL(ip string) string {
	return fmt.Sprintf("http://%s:%s/", ip, RouterPort)
}
