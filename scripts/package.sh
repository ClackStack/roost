#!/usr/bin/env bash
# Builds .deb and .rpm packages from compiled release binaries in dist/.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64}"
DIST_DIR="${DIST_DIR:-dist}"
NFPM_VERSION="${NFPM_VERSION:-v2.49.0}"

# Normalize git tag / describe output into valid semver for nfpm so both dpkg
# and rpm compare pre-releases (~) and post-tag snapshots (+) monotonically.
normalize_version() {
  local raw="${1#v}"
  if [[ "$raw" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)-(g[0-9a-f]+)(-dirty)?$ ]]; then
    local base="${BASH_REMATCH[1]}"
    local count="${BASH_REMATCH[2]}"
    local sha="${BASH_REMATCH[3]}"
    local dirty="${BASH_REMATCH[4]:+.dirty}"
    printf '%s+git.%s.%s%s\n' "$base" "$count" "$sha" "$dirty"
  elif [[ "$raw" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-dirty$ ]]; then
    printf '%s+dirty\n' "${BASH_REMATCH[1]}"
  elif [[ "$raw" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
    printf '%s\n' "$raw"
  else
    local clean
    clean="$(printf '%s' "$raw" | tr -cs '0-9A-Za-z.' '.' | sed 's/^\.//;s/\.$//')"
    printf '0.0.0-%s\n' "${clean:-dev}"
  fi
}

find_nfpm() {
  if command -v nfpm >/dev/null 2>&1; then
    command -v nfpm
    return
  fi
  local gobin
  gobin="$(go env GOPATH)/bin/nfpm"
  if [ ! -x "$gobin" ]; then
    echo "installing nfpm ${NFPM_VERSION} into $(go env GOPATH)/bin" >&2
    GOTOOLCHAIN=auto go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@${NFPM_VERSION}"
  fi
  printf '%s\n' "$gobin"
}

nfpm_bin="$(find_nfpm)"
# Allow callers (such as test-packages.sh synthetic N-1 builds) to supply an
# explicit deb/rpm version that is not semver.
pkg_version="${PKG_VERSION:-$(normalize_version "$VERSION")}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

sed 's|^ExecStart=/usr/local/bin/roost |ExecStart=/usr/bin/roost |' \
  deploy/roost.service > "$tmp/roost.service"
grep -q '^ExecStart=/usr/bin/roost ' "$tmp/roost.service" || {
  echo "package.sh: failed to rewrite ExecStart in deploy/roost.service" >&2
  exit 1
}

mkdir -p "$DIST_DIR"

for p in $PLATFORMS; do
  os="${p%/*}"
  arch="${p#*/}"
  [ "$os" = "linux" ] || continue
  bin="$DIST_DIR/roost-${VERSION}-${os}-${arch}"
  if [ ! -f "$bin" ]; then
    echo "package.sh: missing binary $bin (run release-binaries first)" >&2
    exit 1
  fi
  for fmt in deb rpm; do
    echo "packaging $fmt ($arch, version $pkg_version)"
    PKG_VERSION="$pkg_version" \
    PKG_ARCH="$arch" \
    PKG_BINARY="$bin" \
    PKG_SERVICE="$tmp/roost.service" \
      "$nfpm_bin" package -f packaging/nfpm.yaml -p "$fmt" -t "$DIST_DIR/"
  done
done

(cd "$DIST_DIR" && rm -f SHA256SUMS && sha256sum roost* > SHA256SUMS)
