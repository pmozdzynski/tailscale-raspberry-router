#!/bin/sh
# Poll Tailscale status and reload dnsmasq upstream when connectivity changes.
set -eu

UPDATE_DNS="/usr/local/bin/update-dns.sh"
INTERVAL="${TAILSCALE_DNS_WATCH_INTERVAL:-15}"

if [ ! -x "$UPDATE_DNS" ]; then
	UPDATE_DNS="$(dirname "$0")/update-dns.sh"
fi

if [ ! -x "$UPDATE_DNS" ]; then
	echo "update-dns.sh not found" >&2
	exit 1
fi

hash_state() {
	if command -v tailscale >/dev/null 2>&1; then
		tailscale status 2>/dev/null | grep -E 'Exit node|100\.' | md5sum 2>/dev/null | awk '{print $1}'
	else
		echo "no-tailscale"
	fi
}

last="$(hash_state)"
"$UPDATE_DNS"

while true; do
	sleep "$INTERVAL"
	current="$(hash_state)"
	if [ "$current" != "$last" ]; then
		logger -t tailscale-router-dns "Tailscale state changed — updating DNS"
		"$UPDATE_DNS"
		last="$current"
	fi
done
