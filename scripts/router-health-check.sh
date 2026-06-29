#!/bin/sh
# One-shot health check for tailscale-raspberry-router.
# Exit 0 = healthy (or repaired). Exit 1 = still unhealthy (watchdog may reboot).
set -eu

LOG_TAG="tailscale-router-health"
CONFIG="/etc/tailscale-router/config.json"
FAIL=0

log() {
	logger -t "$LOG_TAG" "$1" 2>/dev/null || true
	echo "$1"
}

service_active() {
	systemctl is-active --quiet "$1" 2>/dev/null
}

restart_service() {
	log "Restarting $1"
	systemctl restart "$1" 2>/dev/null || return 1
	sleep 2
	service_active "$1"
}

is_configured() {
	[ -f "$CONFIG" ] && grep -q '"configured"[[:space:]]*:[[:space:]]*true' "$CONFIG"
}

get_config_val() {
	key="$1"
	if [ ! -f "$CONFIG" ]; then
		return 1
	fi
	sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$CONFIG" | head -n 1
}

check_service() {
	name="$1"
	if service_active "$name"; then
		return 0
	fi
	log "Service $name is not active"
	if restart_service "$name"; then
		log "Service $name recovered after restart"
		return 0
	fi
	log "Service $name failed to recover"
	FAIL=1
	return 1
}

check_default_route() {
	if ip route show default 2>/dev/null | grep -q .; then
		return 0
	fi
	log "No default route"
	FAIL=1
	return 1
}

check_wan_interface() {
	wan="$(get_config_val wan_interface || true)"
	[ -n "$wan" ] || return 0

	if ip link show "$wan" 2>/dev/null | grep -q "state UP"; then
		return 0
	fi
	log "WAN interface $wan is down"
	FAIL=1
	return 1
}

check_ip_forwarding() {
	val="$(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo 0)"
	if [ "$val" = "1" ]; then
		return 0
	fi
	log "IP forwarding disabled, enabling"
	sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
}

# --- checks ---

check_ip_forwarding

if is_configured; then
	check_default_route
	check_wan_interface
	check_service tailscaled
	check_service dnsmasq
	check_service tailscale-router
else
	# Pre-setup: only ensure the web installer stays up
	check_service tailscale-router
fi

if [ "$FAIL" -eq 0 ]; then
	exit 0
fi

exit 1
