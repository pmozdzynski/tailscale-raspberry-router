#!/bin/sh
# Regenerate dnsmasq upstream resolvers and reload dnsmasq.
# Supports NetworkManager (Debian 13+) and Tailscale exit-node DNS.
set -eu

UPSTREAM_DIR="/run/tailscale-router"
UPSTREAM_FILE="${UPSTREAM_DIR}/upstream.conf"
NM_RESOLV="/run/NetworkManager/no-stub-resolv.conf"
SYSTEM_RESOLV="/etc/resolv.conf"
DNSMASQ_SERVICE="dnsmasq"
LOG_TAG="tailscale-router-dns"

log() {
	echo "$1"
	logger -t "$LOG_TAG" "$1" 2>/dev/null || true
}

mkdir -p "$UPSTREAM_DIR"

# Returns 0 when an exit node is in use.
exit_node_active() {
	if ! command -v tailscale >/dev/null 2>&1; then
		return 1
	fi

	if tailscale status 2>/dev/null | grep -qiE 'exit node (selected|in use)'; then
		return 0
	fi

	# JSON fallback (newer tailscale)
	if tailscale status --json 2>/dev/null | grep -q '"ExitNodeStatus"'; then
		if tailscale status --json 2>/dev/null | grep -qE '"ID"|"TailscaleIPs"'; then
			return 0
		fi
	fi

	return 1
}

# Prefer Tailscale MagicDNS when routing through an exit node.
write_tailscale_upstream() {
	cat >"$UPSTREAM_FILE" <<EOF
# Managed by tailscale-router update-dns.sh (exit node active)
nameserver 100.100.100.100
EOF
}

copy_upstream_from() {
	src="$1"
	if [ ! -f "$src" ] || [ ! -s "$src" ]; then
		return 1
	fi
	{
		echo "# Managed by tailscale-router update-dns.sh (source: $src)"
		grep -E '^nameserver[[:space:]]' "$src" || true
	} >"$UPSTREAM_FILE"
	[ -s "$UPSTREAM_FILE" ]
}

if exit_node_active; then
	log "Exit node active — using Tailscale DNS (100.100.100.100)"
	write_tailscale_upstream
else
	log "No exit node — using system upstream resolvers"
	if copy_upstream_from "$NM_RESOLV"; then
		:
	elif copy_upstream_from "$SYSTEM_RESOLV"; then
		:
	else
		log "WARNING: no upstream resolvers found; using Tailscale MagicDNS fallback"
		write_tailscale_upstream
	fi
fi

if command -v systemctl >/dev/null 2>&1; then
	if systemctl is-active --quiet "$DNSMASQ_SERVICE" 2>/dev/null; then
		if systemctl reload "$DNSMASQ_SERVICE" 2>/dev/null; then
			log "dnsmasq reloaded"
		elif systemctl restart "$DNSMASQ_SERVICE" 2>/dev/null; then
			log "dnsmasq restarted"
		else
			log "ERROR: failed to reload/restart dnsmasq"
			exit 1
		fi
	else
		log "dnsmasq is not running — upstream file updated only"
	fi
else
	# OpenRC / no systemd
	if command -v service >/dev/null 2>&1; then
		service "$DNSMASQ_SERVICE" reload 2>/dev/null || service "$DNSMASQ_SERVICE" restart
		log "dnsmasq reloaded via service"
	fi
fi
