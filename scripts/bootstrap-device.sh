#!/bin/sh
# Full pre-web bootstrap on a fresh Debian / Raspberry Pi OS device.
# Installs git, clones the repo, installs Go, compiles, then runs install.sh.
#
# Usage (on the device, as root):
#   curl -fsSL https://raw.githubusercontent.com/pmozdzynski/tailscale-raspberry-router/main/scripts/bootstrap-device.sh | sh
#   wget -qO- https://raw.githubusercontent.com/pmozdzynski/tailscale-raspberry-router/main/scripts/bootstrap-device.sh | sh
#
# Or after cloning:
#   sudo ./scripts/bootstrap-device.sh
set -eu

REPO_URL="${REPO_URL:-https://github.com/pmozdzynski/tailscale-raspberry-router.git}"
REPO_DIR="${REPO_DIR:-/opt/tailscale-raspberry-router-src}"
BRANCH="${BRANCH:-main}"

if [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		exec sudo sh "$0" "$@"
	fi
	echo "Run as root: sudo $0" >&2
	exit 1
fi

log() {
	echo "==> $1"
}

detect_goarm() {
	case "$(uname -m)" in
		armv6l) echo "6" ;;
		armv7l) echo "7" ;;
		*) echo "" ;;
	esac
}

install_base_packages() {
	if ! command -v apt-get >/dev/null 2>&1; then
		echo "This script requires apt-get (Debian / Raspberry Pi OS)." >&2
		exit 1
	fi

	log "Updating package lists"
	apt-get update

	log "Installing git, curl, ca-certificates, and Go"
	apt-get install -y git curl ca-certificates golang-go 2>/dev/null \
		|| apt-get install -y git curl ca-certificates golang
}

clone_or_update_repo() {
	if [ -d "$REPO_DIR/.git" ]; then
		log "Updating existing repo at $REPO_DIR"
		git -C "$REPO_DIR" fetch origin
		git -C "$REPO_DIR" checkout "$BRANCH"
		git -C "$REPO_DIR" pull --ff-only origin "$BRANCH" || true
		return
	fi

	log "Cloning $REPO_URL into $REPO_DIR"
	mkdir -p "$(dirname "$REPO_DIR")"
	git clone --branch "$BRANCH" --depth 1 "$REPO_URL" "$REPO_DIR"
}

build_binary() {
	log "Compiling tailscale-raspberry-router"
	cd "$REPO_DIR"

	export CGO_ENABLED=0
	goarm="$(detect_goarm)"
	if [ -n "$goarm" ]; then
		export GOARM="$goarm"
		log "Building for GOARM=$GOARM"
	fi

	go build -o tailscale-raspberry-router main.go
	chmod +x tailscale-raspberry-router
}

run_installer() {
	log "Running install.sh"
	sh "$REPO_DIR/scripts/install.sh"
}

install_base_packages
clone_or_update_repo
build_binary
run_installer
