#!/bin/sh
# Read-only health ping for the hardware watchdog daemon.
# Must not restart services or change sysctl (watchdog runs this unprivileged).
set -eu

CONFIG="/etc/tailscale-router/config.json"

configured() {
	[ -f "$CONFIG" ] && grep -q '"configured"[[:space:]]*:[[:space:]]*true' "$CONFIG"
}

running() {
	if systemctl is-active --quiet "$1" 2>/dev/null; then
		return 0
	fi
	pgrep -x "$1" >/dev/null 2>&1
}

if configured; then
	running tailscale-router || exit 1
	running tailscaled || exit 1
	running dnsmasq || exit 1
	ip route show default 2>/dev/null | grep -q . || exit 1
else
	running tailscale-router || exit 1
fi

exit 0
