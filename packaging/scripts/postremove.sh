#!/bin/sh
set -eu

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi

case "${1:-}" in
  remove|purge|0)
    rmdir /etc/roost >/dev/null 2>&1 || true
    ;;
esac
