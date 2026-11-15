#!/usr/bin/env bash
# Verifies .deb and .rpm packages in dist/:
#   1. SHA256SUMS and archive/metadata inspection for all architectures.
#   2. Real container tests (Debian, Ubuntu, Fedora, Rocky Linux) exercising
#      local GPG-signed APT and DNF repos, fresh install, user/unit/config
#      permissions, functional smoke run, N-1 -> N upgrade via `apt-get` /
#      `dnf`, conffile preservation, and uninstall.
#
# Pass --structural-only to skip container tests.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"

structural_only=0
if [ "${1:-}" = "--structural-only" ]; then
  structural_only=1
fi

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64}"
DIST_DIR="${DIST_DIR:-dist}"
APT_IMAGES="${APT_IMAGES-debian:bookworm-slim ubuntu:24.04}"
RPM_IMAGES="${RPM_IMAGES-fedora:latest rockylinux:9}"

case "$(uname -m)" in
  x86_64) host_arch="amd64"; host_rpm_arch="x86_64" ;;
  aarch64|arm64) host_arch="arm64"; host_rpm_arch="aarch64" ;;
  *) echo "test-packages.sh: unsupported host arch $(uname -m)" >&2; exit 1 ;;
esac

pass=0
ok()   { pass=$((pass + 1)); echo "  ok: $1"; }
fail() { echo "  FAIL: $1" >&2; exit 1; }

pkg_deb_arch() {
  case "$1" in
    amd64|arm64) printf '%s\n' "$1" ;;
    *) fail "unsupported deb arch $1" ;;
  esac
}

pkg_rpm_arch() {
  case "$1" in
    amd64) echo "x86_64" ;;
    arm64) echo "aarch64" ;;
    *) fail "unsupported rpm arch $1" ;;
  esac
}

find_deb() {
  local arch="$1" dir="$2"
  local match=("$dir"/roost_*_"$arch".deb)
  [ -f "${match[0]}" ] || fail "missing .deb for $arch in $dir"
  printf '%s\n' "${match[0]}"
}

find_rpm() {
  local rpm_arch="$1" dir="$2"
  local match=("$dir"/roost-*-*."$rpm_arch".rpm)
  [ -f "${match[0]}" ] || fail "missing .rpm for $rpm_arch in $dir"
  printf '%s\n' "${match[0]}"
}

echo "== Part 1: structural package checks"

[ -f "$DIST_DIR/SHA256SUMS" ] || fail "missing $DIST_DIR/SHA256SUMS"
(cd "$DIST_DIR" && sha256sum -c SHA256SUMS >/dev/null) \
  && ok "SHA256SUMS verifies all release artifacts" \
  || fail "SHA256SUMS verification failed"

for p in $PLATFORMS; do
  os="${p%/*}"
  arch="${p#*/}"
  [ "$os" = "linux" ] || continue
  deb_file="$(find_deb "$(pkg_deb_arch "$arch")" "$DIST_DIR")"
  rpm_file="$(find_rpm "$(pkg_rpm_arch "$arch")" "$DIST_DIR")"
  ok "found $(basename "$deb_file") and $(basename "$rpm_file")"

  if command -v dpkg-deb >/dev/null 2>&1; then
    deb_arch_actual="$(dpkg-deb -f "$deb_file" Architecture)"
    [ "$deb_arch_actual" = "$arch" ] || fail "$deb_file Architecture=$deb_arch_actual, want $arch"
    deb_pkg="$(dpkg-deb -f "$deb_file" Package)"
    [ "$deb_pkg" = "roost" ] || fail "$deb_file Package=$deb_pkg, want roost"
    deb_list="$(dpkg-deb -c "$deb_file")"
    for path in ./usr/bin/roost ./usr/lib/systemd/system/roost.service ./etc/roost/roost.toml ./usr/share/doc/roost/LICENSE; do
      grep -qF "$path" <<<"$deb_list" || fail "$deb_file missing $path"
    done
    tmpctrl="$(mktemp -d)"
    dpkg-deb -e "$deb_file" "$tmpctrl"
    grep -qx '/etc/roost/roost.toml' "$tmpctrl/conffiles" || { rm -rf "$tmpctrl"; fail "$deb_file missing conffiles entry"; }
    for script in preinst postinst prerm postrm; do
      [ -x "$tmpctrl/$script" ] || { rm -rf "$tmpctrl"; fail "$deb_file missing executable $script"; }
    done
    rm -rf "$tmpctrl"
    ok "dpkg-deb metadata, contents, conffiles, scripts valid ($arch)"
  fi
done

if [ "$structural_only" -eq 1 ]; then
  echo "structural checks passed ($pass checks; container tests skipped)"
  exit 0
fi

runtime="${CONTAINER_RUNTIME:-}"
if [ -z "$runtime" ]; then
  for cand in podman docker; do
    if command -v "$cand" >/dev/null 2>&1; then
      runtime="$cand"
      break
    fi
  done
fi
[ -n "$runtime" ] || fail "neither podman nor docker found (pass --structural-only to skip container tests)"
"$runtime" info >/dev/null 2>&1 || fail "$runtime is not runnable in this environment"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Build a previous version for the host arch so containers can exercise a
# genuine N-1 -> N apt/dnf upgrade and observe the binary version change from
# `roost version`. Use 0.0.0~~prev as the package version: RPM ranks numeric
# segments above letters (so 0.0.0~0.prev > 0.0.0~e3afd3e), whereas a leading
# `~~` sorts strictly older than every `0.0.0~...` version in both dpkg and rpm.
prev_version="v0.0.0-0.prev"
prev_pkg_version="0.0.0~~prev"
mkdir -p "$work/prev" "$work/curr"
CGO_ENABLED=0 GOOS=linux GOARCH="$host_arch" go build -trimpath \
  -ldflags "-s -w -X main.version=${prev_version}" \
  -o "$work/prev/roost-${prev_version}-linux-${host_arch}" ./cmd/roost
VERSION="$prev_version" PKG_VERSION="$prev_pkg_version" PLATFORMS="linux/${host_arch}" DIST_DIR="$work/prev" \
  scripts/package.sh >/dev/null

find_deb "$host_arch" "$work/prev" >/dev/null
find_rpm "$host_rpm_arch" "$work/prev" >/dev/null
curr_deb="$(find_deb "$host_arch" "$DIST_DIR")"
curr_rpm="$(find_rpm "$host_rpm_arch" "$DIST_DIR")"
curr_deb_name="$(basename "$curr_deb")"
curr_rpm_name="$(basename "$curr_rpm")"
curr_bin_matches=("$DIST_DIR"/roost-*-linux-"$host_arch")
[ -f "${curr_bin_matches[0]}" ] || fail "missing raw binary for linux-$host_arch in $DIST_DIR"
expected_roost_version="$("${curr_bin_matches[0]}" version)"
cp "$curr_deb" "$work/curr/$curr_deb_name"
cp "$curr_rpm" "$work/curr/$curr_rpm_name"

# Also copy all arch RPMs so we can inspect headers with rpm inside a container.
mkdir -p "$work/inspect"
cp "$DIST_DIR"/roost-*.rpm "$work/inspect/"

cat > "$work/apt-test.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
trap 'echo "apt-test.sh failed at line $LINENO" >&2' ERR
export DEBIAN_FRONTEND=noninteractive
arch="$1"
expected_curr="$2"

apt-get update -qq
apt-get install -y -qq --no-install-recommends dpkg-dev apt-utils gnupg ca-certificates >/dev/null

export GNUPGHOME=/tmp/gpg
mkdir -p -m 0700 "$GNUPGHOME"
gpg --batch --generate-key <<KEY
%no-protection
Key-Type: RSA
Key-Length: 3072
Name-Real: ClackStack Test
Name-Email: test@clackstack.dev
Expire-Date: 0
%commit
KEY

mkdir -p /etc/apt/keyrings
gpg --export "ClackStack Test" > /etc/apt/keyrings/roost-test.gpg

repo_dir=/tmp/apt-repo
mkdir -p "$repo_dir/pool/main" "$repo_dir/dists/stable/main/binary-$arch"

publish_deb() {
  local src="$1" name="$2"
  cp "$src" "$repo_dir/pool/main/$name"
  (
    cd "$repo_dir"
    dpkg-scanpackages --multiversion pool/main > "dists/stable/main/binary-$arch/Packages"
    gzip -kf "dists/stable/main/binary-$arch/Packages"
    apt-ftparchive \
      -o APT::FTPArchive::Release::Origin="ClackStack" \
      -o APT::FTPArchive::Release::Suite="stable" \
      -o APT::FTPArchive::Release::Codename="stable" \
      -o APT::FTPArchive::Release::Architectures="$arch" \
      -o APT::FTPArchive::Release::Components="main" \
      release dists/stable > dists/stable/Release
    rm -f dists/stable/InRelease dists/stable/Release.gpg
    gpg --batch --yes --clearsign -o dists/stable/InRelease dists/stable/Release
    gpg --batch --yes -abs -o dists/stable/Release.gpg dists/stable/Release
  )
  chmod -R a+rX "$repo_dir"
}

cat > /etc/apt/sources.list.d/roost-test.list <<SRC
deb [signed-by=/etc/apt/keyrings/roost-test.gpg] file:$repo_dir stable main
SRC

# 1. Fresh install of N-1 from signed APT repo
prev_src=(/work/prev/*.deb)
publish_deb "${prev_src[0]}" "$(basename "${prev_src[0]}")"
apt-get update -qq
apt-get install -y -qq roost >/dev/null

[ "$(roost version)" = "roost v0.0.0-0.prev" ]
id roost >/dev/null
[ "$(stat -c '%a %U:%G' /etc/roost)" = "750 root:roost" ]
[ "$(stat -c '%a %U:%G' /etc/roost/roost.toml)" = "640 root:roost" ]
[ "$(stat -c '%a %U:%G' /var/lib/roost)" = "700 roost:roost" ]
grep -q '^ExecStart=/usr/bin/roost serve --config /etc/roost/roost.toml$' /usr/lib/systemd/system/roost.service
! grep -q '/usr/local/bin/roost' /usr/lib/systemd/system/roost.service

# Smoke test running roost as the roost service user
mkdir -p /tmp/smoke/docs /tmp/smoke/data
chown -R roost:roost /tmp/smoke
printf 'kestrel milestone report\n' > /tmp/smoke/docs/note.txt
cat > /tmp/smoke/roost.toml <<TOML
[server]
listen = "127.0.0.1:8484"
[storage]
data_dir = "/tmp/smoke/data"
[[collections]]
name = "docs"
path = "/tmp/smoke/docs"
TOML
status_out="$(runuser -u roost -- sh -c 'roost index --config /tmp/smoke/roost.toml && roost status --config /tmp/smoke/roost.toml')"
grep -q '^docs' <<<"$status_out"

# 2. Customize config, publish N, verify apt update sees it, and upgrade preserves config
printf '\n# operator-preserved\n' >> /etc/roost/roost.toml
curr_src=(/work/curr/*.deb)
expected_pkg_ver="$(dpkg-deb -f "${curr_src[0]}" Version)"
publish_deb "${curr_src[0]}" "$(basename "${curr_src[0]}")"
apt-get update -qq
upgrade_plan="$(apt-get -s upgrade)"
grep -q '^Inst roost ' <<<"$upgrade_plan"
apt-get upgrade -y -qq -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" >/dev/null

[ "$(roost version)" = "$expected_curr" ]
[ "$(dpkg-query -W -f='${Version}\n' roost)" = "$expected_pkg_ver" ]
grep -q '^# operator-preserved$' /etc/roost/roost.toml

# 3. Remove keeps config and data
apt-get remove -y -qq roost >/dev/null
! command -v roost >/dev/null 2>&1
[ -f /etc/roost/roost.toml ]
[ -d /var/lib/roost ]
EOF

cat > "$work/rpm-test.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
trap 'echo "rpm-test.sh failed at line $LINENO" >&2' ERR
expected_curr="$1"

dnf -y -q install --allowerasing gnupg2 createrepo_c rpm-sign util-linux >/dev/null

# Verify RPM metadata for all architectures generated in dist/
for rpmf in /work/inspect/*.rpm; do
  [ "$(rpm -qp --qf '%{NAME}\n' "$rpmf")" = "roost" ]
  rpm_files="$(rpm -qpl "$rpmf")"
  grep -qx '/usr/bin/roost' <<<"$rpm_files"
  grep -qx '/usr/lib/systemd/system/roost.service' <<<"$rpm_files"
  rpm_confs="$(rpm -qp --configfiles "$rpmf")"
  grep -qx '/etc/roost/roost.toml' <<<"$rpm_confs"
done

export GNUPGHOME=/tmp/gpg
mkdir -p -m 0700 "$GNUPGHOME"
gpg --batch --generate-key <<KEY
%no-protection
Key-Type: RSA
Key-Length: 3072
Name-Real: ClackStack Test
Name-Email: test@clackstack.dev
Expire-Date: 0
%commit
KEY

gpg --armor --export "ClackStack Test" > /tmp/RPM-GPG-KEY-clackstack-test
rpm --import /tmp/RPM-GPG-KEY-clackstack-test
printf '%%_gpg_name ClackStack Test\n' > ~/.rpmmacros

repo_dir=/tmp/rpm-repo
mkdir -p "$repo_dir"

publish_rpm() {
  local src="$1" name="$2"
  cp "$src" "$repo_dir/$name"
  rpm --addsign "$repo_dir/$name" >/dev/null
  createrepo_c -q "$repo_dir"
  rm -f "$repo_dir/repodata/repomd.xml.asc"
  gpg --batch --yes --detach-sign --armor -o "$repo_dir/repodata/repomd.xml.asc" "$repo_dir/repodata/repomd.xml"
}

cat > /etc/yum.repos.d/roost-test.repo <<REPO
[roost-test]
name=Roost Test Repo
baseurl=file://$repo_dir
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=file:///tmp/RPM-GPG-KEY-clackstack-test
REPO

# 1. Fresh install of N-1 from signed DNF repo
prev_src=(/work/prev/*.rpm)
publish_rpm "${prev_src[0]}" "$(basename "${prev_src[0]}")"
dnf -y -q --disablerepo='*' --enablerepo=roost-test makecache
dnf -y -q --disablerepo='*' --enablerepo=roost-test install roost >/dev/null

[ "$(roost version)" = "roost v0.0.0-0.prev" ]
id roost >/dev/null
[ "$(stat -c '%a %U:%G' /etc/roost)" = "750 root:roost" ]
[ "$(stat -c '%a %U:%G' /etc/roost/roost.toml)" = "640 root:roost" ]
[ "$(stat -c '%a %U:%G' /var/lib/roost)" = "700 roost:roost" ]
grep -q '^ExecStart=/usr/bin/roost serve --config /etc/roost/roost.toml$' /usr/lib/systemd/system/roost.service
! grep -q '/usr/local/bin/roost' /usr/lib/systemd/system/roost.service

mkdir -p /tmp/smoke/docs /tmp/smoke/data
chown -R roost:roost /tmp/smoke
printf 'kestrel milestone report\n' > /tmp/smoke/docs/note.txt
cat > /tmp/smoke/roost.toml <<TOML
[server]
listen = "127.0.0.1:8484"
[storage]
data_dir = "/tmp/smoke/data"
[[collections]]
name = "docs"
path = "/tmp/smoke/docs"
TOML
status_out="$(runuser -u roost -- sh -c 'roost index --config /tmp/smoke/roost.toml && roost status --config /tmp/smoke/roost.toml')"
grep -q '^docs' <<<"$status_out"

# 2. Customize config, publish N, verify dnf check-update sees it, and upgrade preserves config
printf '\n# operator-preserved\n' >> /etc/roost/roost.toml
curr_src=(/work/curr/*.rpm)
expected_pkg_ver="$(rpm -qp --qf '%{VERSION}\n' "${curr_src[0]}")"
publish_rpm "${curr_src[0]}" "$(basename "${curr_src[0]}")"
rc=0
dnf -q --refresh --disablerepo='*' --enablerepo=roost-test check-update roost >/tmp/check-update.out || rc=$?
[ "$rc" -eq 100 ]
grep -q '^roost' /tmp/check-update.out
dnf -y -q --disablerepo='*' --enablerepo=roost-test upgrade roost >/dev/null

[ "$(roost version)" = "$expected_curr" ]
[ "$(rpm -q --qf '%{VERSION}\n' roost)" = "$expected_pkg_ver" ]
grep -q '^# operator-preserved$' /etc/roost/roost.toml

# 3. Remove cleans binary without deleting data or user edits
dnf -y -q remove roost >/dev/null
! command -v roost >/dev/null 2>&1
[ -f /etc/roost/roost.toml ] || [ -f /etc/roost/roost.toml.rpmsave ]
[ -d /var/lib/roost ]
EOF

chmod +x "$work/apt-test.sh" "$work/rpm-test.sh"

echo "== Part 2: container repo install, upgrade, and removal checks ($runtime)"

for img in $APT_IMAGES; do
  echo "testing APT on $img..."
  "$runtime" run --rm -v "$work:/work:ro" "$img" \
    /bin/bash /work/apt-test.sh "$host_arch" "$expected_roost_version"
  ok "APT install, update, upgrade, conffile, remove passed on $img"
done

for img in $RPM_IMAGES; do
  echo "testing DNF on $img..."
  "$runtime" run --rm -v "$work:/work:ro" "$img" \
    /bin/bash /work/rpm-test.sh "$expected_roost_version"
  ok "DNF install, check-update, upgrade, conffile, remove passed on $img"
done

echo "all package tests passed ($pass checks)"
