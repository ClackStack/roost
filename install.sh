#!/bin/sh
# Installs a roost release binary on Linux (amd64 or arm64).
#
#   curl -fsSL https://roost.clackstack.dev/install.sh | sh
#   curl -fsSL https://roost.clackstack.dev/install.sh | sh -s -- v1.2.3
#
# The binary is verified against the release's SHA256SUMS before it is
# installed. Re-running upgrades in place; it never touches your data or
# roost.toml.
#
# Environment:
#   ROOST_VERSION      pin a release tag (same as the positional argument)
#   ROOST_INSTALL_DIR  target directory (default: /usr/local/bin if
#                      writable, else ~/.local/bin)
#   ROOST_REPO         GitHub repo to install from (default ClackStack/roost)
#
# Needs curl and sha256sum.

set -eu

repo="${ROOST_REPO:-ClackStack/roost}"
tag="${ROOST_VERSION:-${1:-}}"
dl="https://github.com/$repo/releases"
dest="${ROOST_INSTALL_DIR:-}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

for tool in curl sha256sum install; do
  command -v "$tool" >/dev/null 2>&1 || die "missing $tool (on Debian/Ubuntu: apt-get install curl coreutils)"
done

[ "$(uname -s)" = "Linux" ] ||
  die "release binaries are Linux-only; on other platforms build from source: make build"
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported architecture '$(uname -m)' (need x86_64 or aarch64)" ;;
esac

if [ -z "$tag" ]; then
  say "looking up the latest release"
  tag=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$dl/latest") ||
    die "could not resolve the latest release; pin one with ROOST_VERSION=vX.Y.Z"
  tag=${tag##*/tag/}
fi

name="roost-$tag-linux-$arch"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT

say "downloading $name"
curl -fsSL -o "$tmp/$name" "$dl/download/$tag/$name" ||
  die "download failed; check https://github.com/$repo/releases and the tag '$tag'"
curl -fsSL -o "$tmp/SHA256SUMS" "$dl/download/$tag/SHA256SUMS" ||
  die "could not download SHA256SUMS for $tag"
grep -q " $name\$" "$tmp/SHA256SUMS" ||
  die "SHA256SUMS for $tag has no entry for $name"
(cd "$tmp" && grep " $name\$" SHA256SUMS | sha256sum -c -) ||
  die "checksum verification failed for $name"

if [ -z "$dest" ]; then
  if [ -d /usr/local/bin ] && { [ -w /usr/local/bin ] || [ "$(id -u)" = 0 ]; }; then
    dest=/usr/local/bin
  else
    dest=$HOME/.local/bin
  fi
fi
mkdir -p "$dest"
[ -d "$dest/roost" ] && die "$dest/roost exists and is a directory"

tmpbin=$dest/.roost.tmp.$$
cp "$tmp/$name" "$tmpbin"
chmod 0755 "$tmpbin"
mv "$tmpbin" "$dest/roost"

case ":$PATH:" in
  *":$dest:"*) ;;
  *)
    say "note: $dest is not on your PATH; add it, or rerun with"
    say "      ROOST_INSTALL_DIR=/usr/local/bin sh install.sh $tag"
    ;;
esac

installed_version=$("$dest/roost" version 2>/dev/null || echo "roost $tag")
say ""
say "installed $installed_version to $dest/roost"
say ""
say "try it:"
say "  curl -fsSLO https://raw.githubusercontent.com/$repo/$tag/roost.example.toml"
say "  cp roost.example.toml roost.toml    # then set data_dir and collections"
say "  $dest/roost token create --name alice --collections legal,hr"
say "  $dest/roost serve                   # serves http://127.0.0.1:8484"
say ""
say "docs: https://github.com/$repo#readme"
