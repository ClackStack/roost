#!/bin/sh
set -eu

if [ ! -d /var/lib/roost ]; then
  install -d -m 0700 -o roost -g roost /var/lib/roost
fi

is_upgrade=0
case "${1:-}" in
  configure)
    [ -n "${2:-}" ] && is_upgrade=1
    ;;
  [0-9]*)
    [ "$1" -ge 2 ] 2>/dev/null && is_upgrade=1
    ;;
esac

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$is_upgrade" -eq 1 ]; then
    systemctl try-restart roost.service >/dev/null 2>&1 || true
  fi
fi
