#!/bin/sh
# Minimal installer: copies the router app and starts the bootstrap wizard on :4000.
# The dashboard on :5000 is started automatically when web setup completes.
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

build_binaries() {
	if [ -f "$REPO_DIR/tailscale-raspberry-router" ] && [ -f "$REPO_DIR/tailscale-router-bootstrap" ]; then
		return
	fi
	if ! command -v go >/dev/null 2>&1; then
		echo "No prebuilt binaries found and Go is not installed." >&2
		exit 1
	fi
	log "Building binaries on device"
	(
		cd "$REPO_DIR"
		go build -o tailscale-raspberry-router main.go
		go build -o tailscale-router-bootstrap bootstrap/main.go
	)
}

install_app_files() {
	log "Installing application to $INSTALL_ROOT"
	mkdir -p "$INSTALL_ROOT/templates" "$INSTALL_ROOT/scripts" "$INSTALL_ROOT/configs"

	build_binaries

	cp "$REPO_DIR/tailscale-raspberry-router" "$INSTALL_ROOT/"
	cp "$REPO_DIR/tailscale-router-bootstrap" "$INSTALL_ROOT/"
	chmod +x "$INSTALL_ROOT/tailscale-raspberry-router" "$INSTALL_ROOT/tailscale-router-bootstrap"
	cp -r "$REPO_DIR/templates/." "$INSTALL_ROOT/templates/"
	cp -r "$REPO_DIR/scripts/." "$INSTALL_ROOT/scripts/"
	cp -r "$REPO_DIR/configs/." "$INSTALL_ROOT/configs/"
}

install_helper_scripts() {
	log "Installing helper scripts"
	for script in update-dns.sh tailscale-dns-watch.sh router-health-check.sh router-health-watch.sh router-watchdog-test.sh bootstrap-verify.sh; do
		install -m 755 "$REPO_DIR/scripts/$script" "/usr/local/bin/$script"
	done
	install -m 755 "$REPO_DIR/scripts/post-setup.sh" "/usr/local/bin/tailscale-router-post-setup.sh"
}

install_systemd() {
	log "Installing systemd services"
	cp "$REPO_DIR/configs/tailscale-router-bootstrap.service" /etc/systemd/system/
	cp "$REPO_DIR/configs/tailscale-router.service" /etc/systemd/system/tailscale-router.service
	if [ -f "$REPO_DIR/configs/tailscale-router-health-watch.service" ]; then
		cp "$REPO_DIR/configs/tailscale-router-health-watch.service" /etc/systemd/system/
	fi
	systemctl daemon-reload
	systemctl enable tailscale-router-bootstrap.service
	systemctl restart tailscale-router-bootstrap.service
	# Dashboard starts after setup; enable unit file only so bootstrap can start it later.
	systemctl enable tailscale-router.service 2>/dev/null || true
	if systemctl is-active --quiet tailscale-router.service 2>/dev/null; then
		systemctl restart tailscale-router.service || true
	fi
	if systemctl list-unit-files tailscale-router-health-watch.service >/dev/null 2>&1; then
		systemctl enable tailscale-router-health-watch.service
	fi
}

print_access_help() {
	ips="$(hostname -I 2>/dev/null | tr ' ' '\n' | sed '/^$/d' | head -n 5)"
	log "Installation complete"
	echo
	echo "Open the bootstrap setup wizard (always on port 4000):"
	echo
	if [ -n "$ips" ]; then
		echo "$ips" | while read -r ip; do
			[ -n "$ip" ] && echo "  http://${ip}:4000/setup"
		done
	else
		echo "  http://<device-ip>:4000/setup"
	fi
	echo
	echo "After setup completes, the dashboard runs on port 5000."
	echo "The bootstrap wizard stays on port 4000 (shows completed status)."
	echo
}

install_app_files
install_helper_scripts
install_systemd
print_access_help
