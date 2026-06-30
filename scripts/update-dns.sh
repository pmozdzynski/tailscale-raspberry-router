#!/bin/sh
# Regenerate dnsmasq upstream resolvers and reload dnsmasq.
# Direct mode: WAN DNS from DHCP (NetworkManager / resolvectl). Public DNS only if none.
# Exit node mode: Tailscale MagicDNS (100.100.100.100).
set -eu

UPSTREAM_DIR="/run/tailscale-router"
UPSTREAM_FILE="${UPSTREAM_DIR}/upstream.conf"
NM_RESOLV="/run/NetworkManager/no-stub-resolv.conf"
SYSTEM_RESOLV="/etc/resolv.conf"
DNSMASQ_SERVICE="dnsmasq"
LOG_TAG="tailscale-router-dns"
PUBLIC_DNS_1="1.1.1.1"
PUBLIC_DNS_2="9.9.9.9"

log() {
	echo "$1"
	logger -t "$LOG_TAG" "$1" 2>/dev/null || true
}

mkdir -p "$UPSTREAM_DIR"

wan_interface() {
	if [ -f /etc/tailscale-router/config.json ]; then
		wan="$(grep -o '"wan_interface"[[:space:]]*:[[:space:]]*"[^"]*"' /etc/tailscale-router/config.json \
			| head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
		if [ -n "$wan" ]; then
			echo "$wan"
			return
		fi
	fi
	ip route show default 2>/dev/null | awk '/default/ {print $5; exit}'
}

# Skip systemd-resolved stub and other local forwarders.
is_usable_nameserver() {
	case "$1" in
		127.*|0.0.0.0|::1|::* ) return 1 ;;
	esac
	return 0
}

write_upstream_nameservers() {
	header="$1"
	shift
	{
		echo "$header"
		for ns in "$@"; do
			echo "nameserver $ns"
		done
	} >"$UPSTREAM_FILE"
	[ -s "$UPSTREAM_FILE" ]
}

write_public_fallback() {
	log "No WAN DNS from DHCP; using public resolvers (${PUBLIC_DNS_1}, ${PUBLIC_DNS_2})"
	write_upstream_nameservers \
		"# Managed by tailscale-router update-dns.sh (no WAN DNS)" \
		"$PUBLIC_DNS_1" "$PUBLIC_DNS_2"
}

write_server_conf() {
	# dnsmasq ignores resolv-file when no-resolv is set; use server= lines instead.
	{
		echo "# Managed by tailscale-router update-dns.sh"
		grep -E '^nameserver[[:space:]]' "$UPSTREAM_FILE" 2>/dev/null \
			| awk '{print "server="$2}' || true
	} >"${UPSTREAM_DIR}/upstream-servers.conf"
	if [ ! -s "${UPSTREAM_DIR}/upstream-servers.conf" ]; then
		{
			echo "# Managed by tailscale-router update-dns.sh (no WAN DNS)"
			echo "server=${PUBLIC_DNS_1}"
			echo "server=${PUBLIC_DNS_2}"
		} >"${UPSTREAM_DIR}/upstream-servers.conf"
	fi
}

# Returns 0 when an exit node is in use.
exit_node_active() {
	if ! command -v tailscale >/dev/null 2>&1; then
		return 1
	fi

	if tailscale status 2>/dev/null | grep -qiE 'exit node (selected|in use)'; then
		return 0
	fi

	if tailscale status --json 2>/dev/null | grep -q '"ExitNodeStatus"'; then
		if tailscale status --json 2>/dev/null | grep -qE '"ID"|"TailscaleIPs"'; then
			return 0
		fi
	fi

	return 1
}

write_tailscale_upstream() {
	cat >"$UPSTREAM_FILE" <<EOF
# Managed by tailscale-router update-dns.sh (exit node active)
nameserver 100.100.100.100
EOF
}

collect_wan_dns() {
	wan="$(wan_interface)"
	[ -n "$wan" ] || return 1

	servers=""

	if command -v resolvectl >/dev/null 2>&1; then
		for ns in $(resolvectl dns "$wan" 2>/dev/null | awk '{print $2}'); do
			if is_usable_nameserver "$ns"; then
				servers="$servers $ns"
			fi
		done
	fi

	if [ -z "$servers" ] && command -v nmcli >/dev/null 2>&1; then
		for ns in $(nmcli -t -f IP4.DNS dev show "$wan" 2>/dev/null | cut -d: -f2); do
			[ -n "$ns" ] || continue
			if is_usable_nameserver "$ns"; then
				servers="$servers $ns"
			fi
		done
	fi

	if [ -z "$servers" ] && [ -n "$wan" ] && [ -r "/sys/class/net/$wan/ifindex" ]; then
		lease="/run/systemd/netif/leases/$(cat "/sys/class/net/$wan/ifindex")"
		if [ -f "$lease" ]; then
			for ns in $(grep -E '^DNS=' "$lease" 2>/dev/null | cut -d= -f2); do
				if is_usable_nameserver "$ns"; then
					servers="$servers $ns"
				fi
			done
		fi
	fi

	# shellcheck disable=SC2086
	[ -n "$servers" ] || return 1
	# shellcheck disable=SC2086
	set -- $servers
	write_upstream_nameservers \
		"# Managed by tailscale-router update-dns.sh (WAN $wan DHCP)" \
		"$@"
	log "Using WAN DNS from $wan: $*"
	return 0
}

copy_upstream_from() {
	src="$1"
	[ -f "$src" ] || return 1

	servers=""
	while read -r _ ns; do
		[ -n "$ns" ] || continue
		if is_usable_nameserver "$ns"; then
			servers="$servers $ns"
		fi
	done <<EOF
$(grep -E '^nameserver[[:space:]]' "$src" 2>/dev/null || true)
EOF

	# shellcheck disable=SC2086
	[ -n "$servers" ] || return 1
	# shellcheck disable=SC2086
	set -- $servers
	write_upstream_nameservers \
		"# Managed by tailscale-router update-dns.sh (source: $src)" \
		"$@"
	return 0
}

if exit_node_active; then
	log "Exit node active. Using Tailscale DNS (100.100.100.100)"
	write_tailscale_upstream
else
	log "No exit node. Using WAN/system upstream resolvers"
	if collect_wan_dns; then
		:
	elif copy_upstream_from "$NM_RESOLV"; then
		log "Using NetworkManager resolvers"
	elif copy_upstream_from "$SYSTEM_RESOLV"; then
		log "Using system resolvers"
	else
		write_public_fallback
	fi
fi

write_server_conf

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
		log "dnsmasq is not running. Upstream file updated only"
	fi
else
	if command -v service >/dev/null 2>&1; then
		service "$DNSMASQ_SERVICE" reload 2>/dev/null || service "$DNSMASQ_SERVICE" restart
		log "dnsmasq reloaded via service"
	fi
fi
