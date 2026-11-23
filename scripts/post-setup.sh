#!/bin/sh
# Starts the dashboard (:5000) and watcher units after web setup.
# Bootstrap on :4000 is never restarted.
set -eu

sleep 2
systemctl daemon-reload
systemctl enable tailscale-router.service 2>/dev/null || true
systemctl enable tailscale-dns-watch.service 2>/dev/null || true
systemctl enable tailscale-router-health-watch.service 2>/dev/null || true
systemctl restart tailscale-router.service
systemctl start tailscale-dns-watch.service 2>/dev/null || true
systemctl start tailscale-router-health-watch.service 2>/dev/null || true
