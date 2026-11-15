#!/bin/sh
set -eu

# deb passes "remove" on uninstall ("upgrade" on upgrade).
# rpm passes "0" on final uninstall ("1" on upgrade).
case "${1:-}" in
  remove|0)
    if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
      systemctl stop roost.service >/dev/null 2>&1 || true
      systemctl disable roost.service >/dev/null 2>&1 || true
    fi
    ;;
esac
