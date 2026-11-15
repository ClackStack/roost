#!/bin/sh
set -eu

if ! getent group roost >/dev/null 2>&1; then
  groupadd --system roost
fi

if ! getent passwd roost >/dev/null 2>&1; then
  nologin="$(command -v nologin || echo /bin/false)"
  useradd --system \
    --gid roost \
    --home-dir /var/lib/roost \
    --no-create-home \
    --shell "$nologin" \
    --comment "Roost document server" \
    roost
fi
