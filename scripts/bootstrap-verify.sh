#!/bin/sh
# Verify bootstrap / router health from the command line.
# Exit 0 = all checks passed. Exit 1 = one or more problems (details printed).
set -eu

CONFIG="/etc/tailscale-router/config.json"
MODE_FILE="/etc/tailscale-mode.json"
UPSTREAM="/run/tailscale-router/upstream-servers.conf"
FAIL=0

pass() { echo "OK   $1"; }
fail() { echo "FAIL $1"; FAIL=1; }
info() { echo "     $1"; }

check_file() {
	if [ -f "$1" ]; then
		pass "$2"
	else
		fail "$2 (missing: $1)"
	fi
}

check_service() {
	if systemctl is-active --quiet "$1" 2>/dev/null; then
		pass "service $1 is active"
	else
		fail "service $1 is not active"
		systemctl --no-pager -l status "$1" 2>/dev/null | tail -n 5 | sed 's/^/     /' || true
	fi
}

echo "=== tailscale-raspberry-router bootstrap verify ==="
echo

check_file "$CONFIG" "router config saved"
if [ -f "$CONFIG" ]; then
	if grep -q '"configured"[[:space:]]*:[[:space:]]*true' "$CONFIG"; then
		pass "router marked configured"
	else
		fail "router config exists but configured=false"
	fi
	wan="$(sed -n 's/.*"wan_interface"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG" | head -1)"
	lan="$(sed -n 's/.*"lan_interface"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG" | head -1)"
	[ -n "$wan" ] && info "WAN interface: $wan"
	[ -n "$lan" ] && info "LAN interface: $lan"
fi

echo
if [ "$(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo 0)" = "1" ]; then
	pass "IPv4 forwarding enabled"
else
	fail "IPv4 forwarding disabled (run: sudo sysctl -w net.ipv4.ip_forward=1)"
fi

echo
check_file "$UPSTREAM" "dnsmasq upstream file"
if [ -f "$UPSTREAM" ]; then
	info "upstream servers:"
	grep -E '^server=' "$UPSTREAM" | sed 's/^/     /' || fail "upstream file has no server= lines"
fi

echo
check_service tailscale-router
check_service tailscaled
check_service dnsmasq

echo
if systemctl list-unit-files watchdog.service >/dev/null 2>&1; then
	if systemctl is-active --quiet watchdog 2>/dev/null; then
		pass "hardware watchdog service active (optional)"
	else
		echo "WARN hardware watchdog installed but not active (optional)"
		journalctl -u watchdog -n 5 --no-pager 2>/dev/null | sed 's/^/     /' || true
	fi
else
	echo "WARN hardware watchdog not installed (optional)"
fi

echo
if command -v tailscale >/dev/null 2>&1; then
	if tailscale status >/dev/null 2>&1; then
		pass "tailscale connected"
		tailscale status 2>/dev/null | head -n 3 | sed 's/^/     /'
	else
		fail "tailscale not connected"
		tailscale status 2>&1 | tail -n 3 | sed 's/^/     /' || true
	fi
else
	fail "tailscale CLI missing"
fi

echo
if ip route show default 2>/dev/null | grep -q .; then
	pass "default route present"
	ip route show default | sed 's/^/     /'
else
	fail "no default route"
fi

echo
if iptables -t nat -L TS-ROUTER-NAT -n >/dev/null 2>&1; then
	pass "iptables NAT chain TS-ROUTER-NAT exists"
	iptables -t nat -L TS-ROUTER-NAT -n -v | sed 's/^/     /'
else
	fail "iptables NAT chain TS-ROUTER-NAT missing (direct mode NAT not applied)"
fi

if iptables -L TS-ROUTER-FWD -n >/dev/null 2>&1; then
	pass "iptables forward chain TS-ROUTER-FWD exists"
else
	fail "iptables forward chain TS-ROUTER-FWD missing"
fi

echo
if [ -f "$MODE_FILE" ]; then
	pass "routing mode file present"
	info "mode: $(cat "$MODE_FILE" 2>/dev/null || echo unknown)"
else
	fail "routing mode file missing ($MODE_FILE)"
fi

echo
echo "=== bootstrap log (last 30 lines) ==="
journalctl -u tailscale-router -n 30 --no-pager 2>/dev/null | sed 's/^/     /' || info "no journal entries"

echo
if [ "$FAIL" -eq 0 ]; then
	echo "All checks passed."
	exit 0
fi

echo "One or more checks failed. Review FAIL lines above."
exit 1
