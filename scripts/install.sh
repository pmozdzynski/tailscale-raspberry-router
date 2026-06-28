#!/bin/sh
# Minimal installer: copies the router app and starts the web setup wizard.
# dnsmasq, Tailscale, iptables, and LAN config are installed via http://<ip>:5000/setup
set -eu

INSTALL_ROOT="/opt/tailscale-raspberry-router"
REPO_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

if [ "$(id -u)" -ne 0 ]; then
	echo "Run as root: sudo $0" >&2
	exit 1
fi

log() {
	echo "==> $1"
}

install_app_files() {
	log "Installing application to $INSTALL_ROOT"
	mkdir -p "$INSTALL_ROOT/templates" "$INSTALL_ROOT/scripts" "$INSTALL_ROOT/configs"

	if [ -f "$REPO_DIR/tailscale-raspberry-router" ]; then
		cp "$REPO_DIR/tailscale-raspberry-router" "$INSTALL_ROOT/"
	elif command -v go >/dev/null 2>&1; then
		log "Building binary on device"
		(
			cd "$REPO_DIR"
			go build -o "$INSTALL_ROOT/tailscale-raspberry-router" main.go
		)
	else
		echo "No prebuilt binary found and Go is not installed." >&2
		echo "Build on another machine, copy binary into repo root, then re-run:" >&2
		echo "  GOOS=linux GOARCH=arm GOARM=6 go build -o tailscale-raspberry-router main.go" >&2
		exit 1
	fi

	chmod +x "$INSTALL_ROOT/tailscale-raspberry-router"
	cp -r "$REPO_DIR/templates/." "$INSTALL_ROOT/templates/"
	cp -r "$REPO_DIR/scripts/." "$INSTALL_ROOT/scripts/"
	cp -r "$REPO_DIR/configs/." "$INSTALL_ROOT/configs/"
}

install_helper_scripts() {
	log "Installing helper scripts"
	for script in update-dns.sh tailscale-dns-watch.sh router-health-check.sh router-health-watch.sh; do
		install -m 755 "$REPO_DIR/scripts/$script" "/usr/local/bin/$script"
	done
}

install_systemd() {
	log "Installing systemd services"
	cp "$REPO_DIR/configs/tailscale-router.service" /etc/systemd/system/tailscale-router.service
	if [ -f "$REPO_DIR/configs/tailscale-router-health-watch.service" ]; then
		cp "$REPO_DIR/configs/tailscale-router-health-watch.service" /etc/systemd/system/
	fi
	systemctl daemon-reload
	systemctl enable tailscale-router.service
	systemctl restart tailscale-router.service
	if systemctl list-unit-files tailscale-router-health-watch.service >/dev/null 2>&1; then
		systemctl enable tailscale-router-health-watch.service
		systemctl restart tailscale-router-health-watch.service
	fi
}

print_access_help() {
	ips="$(hostname -I 2>/dev/null | tr ' ' '\n' | sed '/^$/d' | head -n 5)"
	log "Installation complete"
	echo
	echo "Nothing else is configured yet."
	echo "Open the setup wizard in a browser:"
	echo
	if [ -n "$ips" ]; then
		echo "$ips" | while read -r ip; do
			[ -n "$ip" ] && echo "  http://${ip}:5000/setup"
		done
	else
		echo "  http://<device-ip>:5000/setup"
		echo
		echo "The device IP is assigned by your router/ISP and may be unknown."
		echo "Find it with: ip -4 addr show   or check your router DHCP leases."
	fi
	echo
	echo "The wizard will auto-detect interfaces, install dnsmasq/Tailscale,"
	echo "configure LAN DHCP/DNS, and leave WAN on existing DHCP."
	echo
}

install_app_files
install_helper_scripts
install_systemd
print_access_help
