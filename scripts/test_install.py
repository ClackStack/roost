"""Offline regression tests for install.sh. Run with unittest discovery."""

import hashlib
import json
import shutil
import subprocess
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path


SCRIPT = Path(__file__).resolve().parent.parent / "install.sh"
REAL_CURL = shutil.which("curl")


def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


class MockReleaseServer:
    def __init__(
        self,
        tag: str = "v0.2.0",
        *,
        pkg_version: str | None = None,
        redirect_mode: str = "ok",
        api_mode: str = "ok",
        include_pkgs: bool = True,
        corrupt_asset: str | None = None,
        omit_sum_for: str | None = None,
        star_sums: bool = False,
    ):
        self.tag = tag
        self.pkg_version = pkg_version or tag.lstrip("v")
        self.redirect_mode = redirect_mode
        self.api_mode = api_mode
        self.requests: list[tuple[str, str]] = []

        self.assets: dict[str, bytes] = {
            f"roost-{tag}-linux-amd64": b"#!/bin/sh\necho roost " + tag.encode() + b"\n",
            f"roost-{tag}-linux-arm64": b"#!/bin/sh\necho roost " + tag.encode() + b"\n",
        }
        if include_pkgs:
            self.assets.update({
                f"roost_{self.pkg_version}_amd64.deb": b"fake-deb-" + self.pkg_version.encode(),
                f"roost_{self.pkg_version}_arm64.deb": b"fake-deb-arm64-" + self.pkg_version.encode(),
                f"roost-{self.pkg_version}-1.x86_64.rpm": b"fake-rpm-" + self.pkg_version.encode(),
                f"roost-{self.pkg_version}-1.aarch64.rpm": b"fake-rpm-arm64-" + self.pkg_version.encode(),
            })

        sep = " *" if star_sums else "  "
        lines = []
        for name, data in sorted(self.assets.items()):
            if name == omit_sum_for:
                continue
            digest = "0" * 64 if name == corrupt_asset else sha256_hex(data)
            lines.append(f"{digest}{sep}{name}\n")
        self.assets["SHA256SUMS"] = "".join(lines).encode()

        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, fmt, *args):
                pass

            def _handle(self, method: str):
                outer.requests.append((method, self.path))
                if self.path == "/ClackStack/roost/releases/latest":
                    if outer.redirect_mode == "ok":
                        self.send_response(302)
                        self.send_header(
                            "Location",
                            f"http://127.0.0.1:{outer.port}/ClackStack/roost/releases/tag/{outer.tag}",
                        )
                        self.end_headers()
                    elif outer.redirect_mode == "chain":
                        self.send_response(302)
                        self.send_header("Location", f"http://127.0.0.1:{outer.port}/hop")
                        self.end_headers()
                    else:
                        self.send_response(500)
                        self.end_headers()
                    return

                if self.path == "/hop":
                    self.send_response(302)
                    self.send_header(
                        "Location",
                        f"http://127.0.0.1:{outer.port}/ClackStack/roost/releases/tag/{outer.tag}",
                    )
                    self.end_headers()
                    return

                if self.path == f"/ClackStack/roost/releases/tag/{outer.tag}":
                    self.send_response(200)
                    self.end_headers()
                    if method != "HEAD":
                        self.wfile.write(b"ok")
                    return

                if self.path == "/repos/ClackStack/roost/releases/latest":
                    if outer.api_mode == "ok":
                        payload = json.dumps({"tag_name": outer.tag}).encode()
                        self.send_response(200)
                        self.send_header("Content-Type", "application/json")
                        self.send_header("Content-Length", str(len(payload)))
                        self.end_headers()
                        if method != "HEAD":
                            self.wfile.write(payload)
                    else:
                        self.send_response(403)
                        self.end_headers()
                    return

                prefix = f"/ClackStack/roost/releases/download/{outer.tag}/"
                if self.path.startswith(prefix):
                    name = self.path[len(prefix):]
                    if name in outer.assets:
                        data = outer.assets[name]
                        self.send_response(200)
                        self.send_header("Content-Length", str(len(data)))
                        self.end_headers()
                        if method != "HEAD":
                            self.wfile.write(data)
                        return

                self.send_response(404)
                self.end_headers()

            def do_GET(self):
                self._handle("GET")

            def do_HEAD(self):
                self._handle("HEAD")

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.server.server_port
        self.thread = threading.Thread(
            target=self.server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True
        )
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class RoostInstallShTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.log = self.root / "actions.log"
        self.log.write_text("")

    def write_exec(self, name: str, body: str):
        path = self.bin / name
        path.write_text("#!/bin/sh\n" + body)
        path.chmod(0o755)
        return path

    def add_core_shims(self, port: int, *, arch: str = "x86_64"):
        needed = [
            "awk", "basename", "cat", "chmod", "cp", "cut", "grep", "head",
            "id", "mkdir", "mktemp", "mv", "rm", "sed", "sha256sum", "tr",
        ]
        for tool in needed:
            found = shutil.which(tool)
            if found and not (self.bin / tool).exists():
                (self.bin / tool).symlink_to(found)

        self.write_exec(
            "uname",
            f'case "${{1:-}}" in -s) echo Linux ;; -m) echo {arch} ;; *) echo Linux ;; esac\n',
        )

        assert REAL_CURL, "curl is required for install.sh tests"
        self.write_exec(
            "curl",
            f"""n=$#
i=0
while [ "$i" -lt "$n" ]; do
  a=$1
  shift
  i=$((i + 1))
  case "$a" in
    https://github.com/*) set -- "$@" "http://127.0.0.1:{port}/${{a#https://github.com/}}" ;;
    https://api.github.com/*) set -- "$@" "http://127.0.0.1:{port}/${{a#https://api.github.com/}}" ;;
    *) set -- "$@" "$a" ;;
  esac
done
exec "{REAL_CURL}" "$@"
""",
        )

    def add_sudo(self, *, usable: bool = True):
        if usable:
            self.write_exec(
                "sudo",
                f"""if [ "${{1:-}}" = "-v" ]; then
  printf "sudo -v\\n" >> "{self.log}"
  exit 0
fi
printf "sudo %s\\n" "$*" >> "{self.log}"
exec "$@"
""",
            )
        else:
            self.write_exec(
                "sudo",
                f'printf "sudo %s\\n" "$*" >> "{self.log}"\nexit 1\n',
            )

    def add_deb_tools(self, installed: bool, *, with_apt: bool = True):
        status = "install ok installed\\n" if installed else ""
        exit_code = 0 if installed else 1
        self.write_exec(
            "dpkg-query",
            f'printf "{status}"\nexit {exit_code}\n',
        )
        self.write_exec("dpkg", f'printf "dpkg %s\\n" "$*" >> "{self.log}"\n')
        if with_apt:
            self.write_exec("apt-get", f'printf "apt-get %s\\n" "$*" >> "{self.log}"\n')

    def add_rpm_tools(self, installed: bool, *, manager: str | None = "dnf"):
        self.write_exec(
            "rpm",
            f"""if [ "${{1:-}}" = "-q" ]; then
  exit {0 if installed else 1}
fi
printf "rpm %s\\n" "$*" >> "{self.log}"
""",
        )
        if manager:
            self.write_exec(manager, f'printf "{manager} %s\\n" "$*" >> "{self.log}"\n')

    def run_installer(self, *args: str, extra_env: dict[str, str] | None = None):
        env = {
            "HOME": str(self.home),
            "PATH": str(self.bin),
        }
        if extra_env:
            env.update(extra_env)
        return subprocess.run(
            ["/bin/sh", str(SCRIPT), *args],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            env=env,
            start_new_session=True,
            timeout=15,
        )

    def actions(self) -> str:
        return self.log.read_text()

    def test_existing_deb_updates_via_deb_without_binary_download(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        local_bin = self.home / ".local/bin"
        local_bin.mkdir(parents=True)
        stale = local_bin / "roost"
        stale.write_text("#!/bin/sh\n")
        stale.chmod(0o755)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("updating existing .deb installation", res.stdout)
        self.assertIn("apt-get install -y --no-install-recommends", self.actions())
        self.assertIn("roost_0.2.0_amd64.deb", self.actions())
        self.assertFalse(stale.exists())
        downloaded = [p for m, p in srv.requests if m == "GET"]
        self.assertFalse(any("linux-amd64" in p for p in downloaded), downloaded)

    def test_existing_deb_matches_nfpm_prerelease_tilde_version(self):
        srv = MockReleaseServer("v0.3.0-rc.1", pkg_version="0.3.0~rc.1")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("roost_0.3.0~rc.1_amd64.deb", self.actions())
        downloaded = [p for m, p in srv.requests if m == "GET"]
        self.assertFalse(any("linux-amd64" in p for p in downloaded), downloaded)

    def test_existing_deb_falls_back_to_api_when_redirect_fails(self):
        srv = MockReleaseServer("v0.2.0", redirect_mode="fail", api_mode="ok")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("roost_0.2.0_amd64.deb", self.actions())

    def test_existing_deb_follows_redirect_chain_without_api(self):
        srv = MockReleaseServer("v0.2.0", redirect_mode="chain", api_mode="fail")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("roost_0.2.0_amd64.deb", self.actions())
        self.assertFalse(any("/repos/" in p for _, p in srv.requests), srv.requests)

    def test_existing_rpm_wins_even_when_deb_tooling_present(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=False, with_apt=True)
        self.add_rpm_tools(installed=True, manager="dnf")

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("updating existing .rpm installation", res.stdout)
        self.assertIn("dnf install -y", self.actions())
        self.assertIn("roost-0.2.0-1.x86_64.rpm", self.actions())
        self.assertNotIn("apt-get", self.actions())

    def test_existing_rpm_bare_rpm_uses_replacepkgs(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_rpm_tools(installed=True, manager=None)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("rpm -U --replacepkgs", self.actions())
        self.assertIn("roost-0.2.0-1.x86_64.rpm", self.actions())

    def test_zypper_uses_no_gpg_checks(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_rpm_tools(installed=True, manager="zypper")

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("zypper --non-interactive --no-gpg-checks install", self.actions())

    def test_fresh_fedora_with_bare_dpkg_prefers_rpm(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=False, with_apt=False)
        self.add_rpm_tools(installed=False, manager="dnf")

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("dnf install -y", self.actions())
        self.assertIn("roost-0.2.0-1.x86_64.rpm", self.actions())
        self.assertNotIn("dpkg", self.actions())
        # sudo -v should only be called once thanks to caching.
        self.assertEqual(self.actions().count("sudo -v"), 1)

    def test_existing_deb_unusable_sudo_errors_instead_of_binary_fallback(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo(usable=False)
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertNotEqual(res.returncode, 0)
        self.assertIn("already installed as a .deb package", res.stderr)
        self.assertIn("--pkg archive", res.stderr)

    def test_fresh_install_unusable_sudo_falls_back_to_binary(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo(usable=False)
        self.add_deb_tools(installed=False)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertTrue((self.home / ".local/bin/roost").is_file())

    def test_existing_deb_missing_pkg_in_release_errors_instead_of_binary_fallback(self):
        srv = MockReleaseServer("v0.1.0", include_pkgs=False)
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer("v0.1.0")
        self.assertNotEqual(res.returncode, 0)
        self.assertIn("cannot update the installed .deb package", res.stderr)
        self.assertFalse(any("linux-amd64" in p for _, p in srv.requests), srv.requests)

    def test_explicit_pkg_deb_missing_pkg_errors(self):
        srv = MockReleaseServer("v0.1.0", include_pkgs=False)
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=False)

        res = self.run_installer("--pkg", "deb", "v0.1.0")
        self.assertNotEqual(res.returncode, 0)
        self.assertIn("has no roost .deb package", res.stderr)

    def test_fresh_install_missing_pkg_falls_back_to_binary(self):
        srv = MockReleaseServer("v0.1.0", include_pkgs=False)
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=False)

        res = self.run_installer("v0.1.0")
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("installing the self-contained binary instead", res.stdout)
        self.assertTrue((self.home / ".local/bin/roost").is_file())

    def test_corrupt_checksum_aborts_before_privileged_install(self):
        srv = MockReleaseServer("v0.2.0", corrupt_asset="roost_0.2.0_amd64.deb")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertNotEqual(res.returncode, 0)
        self.assertIn("checksum verification failed", res.stderr)
        self.assertNotIn("apt-get", self.actions())
        self.assertNotIn("dpkg", self.actions())

    def test_missing_binary_checksum_aborts(self):
        srv = MockReleaseServer(
            "v0.2.0", include_pkgs=False, omit_sum_for="roost-v0.2.0-linux-amd64"
        )
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_deb_tools(installed=False)

        res = self.run_installer("--pkg", "archive", "v0.2.0")
        self.assertNotEqual(res.returncode, 0)
        self.assertIn("has no entry for roost-v0.2.0-linux-amd64", res.stderr)

    def test_apt_failure_retries_with_dpkg(self):
        srv = MockReleaseServer("v0.2.0")
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port)
        self.add_sudo()
        self.add_deb_tools(installed=True)
        self.write_exec("apt-get", f'printf "apt-get %s\\n" "$*" >> "{self.log}"\nexit 1\n')

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("retrying with dpkg", res.stdout)
        self.assertIn("dpkg -i", self.actions())

    def test_arm64_and_star_prefixed_sums(self):
        srv = MockReleaseServer("v0.2.0", star_sums=True)
        self.addCleanup(srv.close)
        self.add_core_shims(srv.port, arch="aarch64")
        self.add_sudo()
        self.add_deb_tools(installed=True)

        res = self.run_installer()
        self.assertEqual(res.returncode, 0, res.stderr + res.stdout)
        self.assertIn("roost_0.2.0_arm64.deb", self.actions())


if __name__ == "__main__":
    unittest.main()
