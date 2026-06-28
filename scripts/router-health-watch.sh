#!/bin/sh
# Periodic health monitor — restarts failed services before hardware watchdog reboots.
set -eu

CHECK="/usr/local/bin/router-health-check.sh"
INTERVAL="${TAILSCALE_HEALTH_INTERVAL:-60}"

if [ ! -x "$CHECK" ]; then
	CHECK="$(dirname "$0")/router-health-check.sh"
fi

if [ ! -x "$CHECK" ]; then
	echo "router-health-check.sh not found" >&2
	exit 1
fi

while true; do
	"$CHECK" || true
	sleep "$INTERVAL"
done
