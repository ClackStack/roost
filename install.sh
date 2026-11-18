#!/bin/sh
# Installs a roost release on Linux (amd64 or arm64).
#
#   curl -fsSL https://roost.clackstack.dev/install.sh | sh
#   curl -fsSL https://roost.clackstack.dev/install.sh | sh -s -- v1.2.3
#   curl -fsSL https://roost.clackstack.dev/install.sh | sh -s -- --pkg archive
#
# Releases ship a self-contained binary plus .deb and .rpm packages. The
# installer uses a system package when this machine can install one (.deb on
# apt/dpkg systems, .rpm on dnf/yum/zypper systems, with root or sudo) and
# falls back to the self-contained binary otherwise; --pkg forces a choice.
# A package install also installs the hardened systemd unit and the starter
# /etc/roost/roost.toml. Whatever it installs is verified against the
# release's SHA256SUMS first. Re-running upgrades in place; it never touches
# your data or roost.toml.
#
# Environment:
#   ROOST_VERSION      pin a release tag (same as the positional argument)
#   ROOST_PKG          deb, rpm or archive (same as --pkg)
#   ROOST_INSTALL_DIR  archive target directory (default: /usr/local/bin if
#                      writable, else ~/.local/bin); not used for packages
#   ROOST_REPO         GitHub repo to install from (default ClackStack/roost)
#
# Needs curl and sha256sum.

set -eu

repo="${ROOST_REPO:-ClackStack/roost}"
tag_arg=
pkg_forced="${ROOST_PKG:-}"
dest="${ROOST_INSTALL_DIR:-}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Install roost on Linux (amd64 or arm64).

Usage: install.sh [--pkg deb|rpm|archive] [TAG]

  --pkg        How to install: a system package when this machine can install
               one (.deb on apt/dpkg systems, .rpm on dnf/yum/zypper systems,
               with root or sudo) or the self-contained `archive` binary.
               Default: the package kind this machine can install, staying
               with the kind an earlier install used.
  TAG          A release tag such as v1.2.3 (default: the latest release).

Environment: ROOST_VERSION, ROOST_PKG, ROOST_INSTALL_DIR, ROOST_REPO.
EOF
}

while [ $# -gt 0 ]; do
  case $1 in
    --pkg)
      [ $# -ge 2 ] || die "--pkg needs deb, rpm or archive"
      pkg_forced=$2
      shift
      ;;
    --pkg=*) pkg_forced=${1#--pkg=} ;;
    -h | --help) usage; exit 0 ;;
    --*) die "unknown option $1 (see --help)" ;;
    *) [ -z "$tag_arg" ] || die "unexpected extra argument '$1'"; tag_arg=$1 ;;
  esac
  shift
done

case $pkg_forced in
  deb | rpm | archive | "") ;;
  *) die "--pkg wants deb, rpm or archive (got '$pkg_forced')" ;;
esac

tag="${ROOST_VERSION:-$tag_arg}"

for tool in curl sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || die "missing $tool (on Debian/Ubuntu: apt-get install curl coreutils)"
done

[ "$(uname -s)" = "Linux" ] ||
  die "release binaries are Linux-only; on other platforms build from source: task build"
case "$(uname -m)" in
  x86_64) arch=amd64; deb_arch=amd64; rpm_arch=x86_64 ;;
  aarch64 | arm64) arch=arm64; deb_arch=arm64; rpm_arch=aarch64 ;;
  *) die "unsupported architecture '$(uname -m)' (need x86_64 or aarch64)" ;;
esac

if [ -z "$tag" ]; then
  say "looking up the latest release"
  url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
    die "could not resolve the latest release; pin one with ROOST_VERSION=vX.Y.Z"
  case "$url" in
    */tag/?*) tag=${url##*/tag/} ;;
    *) die "no latest release found; pin a tag with ROOST_VERSION=vX.Y.Z" ;;
  esac
fi

# --- system package helpers -------------------------------------------------

# Can we actually act as root? uid 0, or a sudo that works. This may prompt
# for the password once, up front, rather than mid-install; a sudo that cannot
# elevate (no terminal, no permissions) means no package install.
can_elevate() {
  if [ "$(id -u)" = 0 ]; then
    return 0
  fi
  command -v sudo >/dev/null 2>&1 || return 1
  sudo -v >/dev/null 2>&1
}

as_root() {
  if [ "$(id -u)" = 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    die "this step needs root; re-run as root (or with sudo available), or use --pkg archive"
  fi
}

deb_tooling() {
  command -v apt-get >/dev/null 2>&1 || command -v dpkg >/dev/null 2>&1
}

rpm_tooling() {
  command -v dnf >/dev/null 2>&1 || command -v microdnf >/dev/null 2>&1 \
    || command -v yum >/dev/null 2>&1 || command -v zypper >/dev/null 2>&1 \
    || command -v rpm >/dev/null 2>&1
}

# Which package kind roost is currently installed as, if any ("deb", "rpm" or
# empty). Keeps re-runs upgrading through the same package manager.
installed_pkg_kind() {
  if command -v dpkg >/dev/null 2>&1 \
    && dpkg-query -W -f='${Status}' roost 2>/dev/null | grep -q 'ok installed'; then
    printf '%s\n' deb
    return 0
  fi
  if command -v rpm >/dev/null 2>&1 && rpm -q roost >/dev/null 2>&1; then
    printf '%s\n' rpm
    return 0
  fi
  return 1
}

install_deb() {
  _deb=$1
  if command -v apt-get >/dev/null 2>&1; then
    if as_root apt-get install -y --no-install-recommends "$_deb"; then
      return 0
    fi
    say "apt-get could not install $(basename "$_deb"); retrying with dpkg"
  fi
  command -v dpkg >/dev/null 2>&1 || die "neither apt-get nor dpkg can install $_deb"
  as_root dpkg -i "$_deb" || die "dpkg could not install $_deb"
}

install_rpm() {
  _rpm=$1
  if command -v dnf >/dev/null 2>&1; then
    as_root dnf install -y "$_rpm" || die "dnf could not install $_rpm"
  elif command -v microdnf >/dev/null 2>&1; then
    as_root microdnf install -y "$_rpm" || die "microdnf could not install $_rpm"
  elif command -v yum >/dev/null 2>&1; then
    as_root yum install -y "$_rpm" || die "yum could not install $_rpm"
  elif command -v zypper >/dev/null 2>&1; then
    as_root zypper --non-interactive install "$_rpm" || die "zypper could not install $_rpm"
  elif command -v rpm >/dev/null 2>&1; then
    as_root rpm -U "$_rpm" || die "rpm could not install $_rpm"
  else
    die "no rpm tooling found to install $(basename "$_rpm")"
  fi
}

# --- pick the install path --------------------------------------------------

dest_explicit=0
[ -n "$dest" ] && dest_explicit=1

pkg_kind=
if [ -n "$pkg_forced" ]; then
  if [ "$pkg_forced" != archive ]; then
    [ "$dest_explicit" = 0 ] ||
      die "--pkg $pkg_forced cannot be combined with ROOST_INSTALL_DIR (system packages install to /usr/bin)"
    pkg_kind=$pkg_forced
  fi
elif [ "$dest_explicit" = 0 ]; then
  have=$(installed_pkg_kind || true)
  case $have in
    deb) deb_tooling && pkg_kind=deb ;;
    rpm) rpm_tooling && pkg_kind=rpm ;;
  esac
  if [ -z "$pkg_kind" ]; then
    if can_elevate && deb_tooling; then
      pkg_kind=deb
    elif can_elevate && rpm_tooling; then
      pkg_kind=rpm
    fi
  fi
fi

if [ "$pkg_kind" = deb ] && ! deb_tooling; then
  die "installing a .deb needs apt-get or dpkg on this machine (try --pkg archive)"
fi
if [ "$pkg_kind" = deb ] && ! can_elevate; then
  die "installing a .deb needs root, or a sudo that can elevate (try --pkg archive, or re-run as root)"
fi
if [ "$pkg_kind" = rpm ] && ! rpm_tooling; then
  die "installing an .rpm needs dnf, yum, zypper or rpm on this machine (try --pkg archive)"
fi
if [ "$pkg_kind" = rpm ] && ! can_elevate; then
  die "installing an .rpm needs root, or a sudo that can elevate (try --pkg archive, or re-run as root)"
fi

name="roost-$tag-linux-$arch"
pkg_version=${tag#v}
pkg_name=
if [ -n "$pkg_kind" ]; then
  if [ "$pkg_kind" = deb ]; then
    pkg_name="roost_${pkg_version}_${deb_arch}.deb"
  else
    pkg_name="roost-${pkg_version}-1.${rpm_arch}.rpm"
  fi
fi

dl="https://github.com/$repo/releases/download/$tag"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT

say "downloading SHA256SUMS for $tag"
curl -fsSL -o "$tmp/SHA256SUMS" "$dl/SHA256SUMS" ||
  die "could not download SHA256SUMS for $tag"

# A release without this package asset (older tags) falls back to the binary.
if [ -n "$pkg_kind" ] && ! grep -q " $pkg_name\$" "$tmp/SHA256SUMS"; then
  say "note: release $tag has no $pkg_name; installing the self-contained binary instead"
  pkg_kind=
fi

verify() {
  (cd "$tmp" && grep " $1\$" SHA256SUMS | sha256sum -c -) ||
    die "checksum verification failed for $1"
}

# --- system package install -------------------------------------------------

if [ -n "$pkg_kind" ]; then
  say "downloading $pkg_name"
  curl -fsSL -o "$tmp/$pkg_name" "$dl/$pkg_name" ||
    die "download failed; check https://github.com/$repo/releases and the tag '$tag'"
  verify "$pkg_name"

  if [ "$pkg_kind" = deb ]; then
    install_deb "$tmp/$pkg_name"
  else
    install_rpm "$tmp/$pkg_name"
  fi

  # An earlier binary install would shadow /usr/bin/roost on PATH; drop it.
  for old in /usr/local/bin/roost "${HOME:-/nonexistent}/.local/bin/roost"; do
    if [ -e "$old" ]; then
      as_root rm -f "$old"
      say "removed the earlier $old (replaced by the system package)"
    fi
  done

  installed_version=$(/usr/bin/roost version 2>/dev/null || echo "roost $tag")
  if [ "$pkg_kind" = deb ]; then
    remove_cmd="apt remove roost"
  else
    remove_cmd="dnf remove roost"
  fi
  say ""
  say "installed $installed_version as a $pkg_kind package: /usr/bin/roost"
  say "the package also installed /etc/roost/roost.toml (starter config) and"
  say "/usr/lib/systemd/system/roost.service (hardened unit)"
  say ""
  say "try it:"
  say "  sudoedit /etc/roost/roost.toml           # set data_dir and collections"
  say "  sudo roost token create --config /etc/roost/roost.toml --name alice --collections legal,hr"
  say "  sudo systemctl enable --now roost         # serves http://127.0.0.1:8484"
  say ""
  say "remove it later with: sudo $remove_cmd"
  say ""
  say "docs: https://github.com/$repo#readme"
  exit 0
fi

# --- self-contained binary install ------------------------------------------

say "downloading $name"
curl -fsSL -o "$tmp/$name" "$dl/$name" ||
  die "download failed; check https://github.com/$repo/releases and the tag '$tag'"
grep -q " $name\$" "$tmp/SHA256SUMS" ||
  die "SHA256SUMS for $tag has no entry for $name"
verify "$name"

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
say "  $dest/roost init                    # writes roost.toml; set data_dir and collections"
say "  $dest/roost token create --name alice --collections legal,hr"
say "  $dest/roost serve                   # serves http://127.0.0.1:8484"
say ""
say "docs: https://github.com/$repo#readme"
