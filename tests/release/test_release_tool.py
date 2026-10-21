# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Unit tests for scripts/release-tool.py (no network, no packaging tools, synthetic fixtures).

The same checks run against real release candidates in CI (release-build.yml, verify job, including
tests/release/negative-tests.sh). Run: python3 -m unittest discover -s tests/release -v
"""
import contextlib
import importlib.util
import io
import json
import os
import shutil
import tarfile
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
spec = importlib.util.spec_from_file_location("release_tool", os.path.join(ROOT, "scripts", "release-tool.py"))
rt = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rt)

V = "0.1.0-dev.1"
C = "0123456789abcdef0123456789abcdef01234567"
PINS = "GO_VERSION=1.26.9\nNODE_VERSION=22.23.3\nNPM_VERSION=10.9.9\n"


def wr(path, data):
    with open(path, "wb" if isinstance(data, bytes) else "w") as f:
        f.write(data)


def add(t, name, data=b"x", mode=0o644):
    ti = tarfile.TarInfo(name)
    ti.size, ti.mode = len(data), mode
    t.addfile(ti, io.BytesIO(data))


class Fixture:
    """A synthetic release directory that passes every check except the package inventory."""

    def __init__(self, tmp, version=V, extra_source=()):
        self.d = os.path.join(tmp, "rel")
        os.makedirs(self.d)
        self.toolchain = os.path.join(tmp, "toolchain.env")
        with open(self.toolchain, "w") as f:
            f.write(PINS)
        n = rt.expected_names(version)
        bi = (f"version={version}\ncommit={C}\nsource_date_epoch=1760000000\ngo=go1.26.9\nnode=v22.23.3\n"
              f"npm=10.9.9\nexcluded=compat/webmin/ (reference tree)\n").encode()
        notices = {k: f"{k} text\n".encode() for k in rt.NOTICES}
        p = f"homelab-control-{version}/"
        with tarfile.open(os.path.join(self.d, n["source"]), "w:gz") as t:
            add(t, p + "RELEASE-BUILDINFO", bi)
            add(t, p + "LICENSE.md", notices["LICENSE.md"])
            add(t, p + "NOTICE.md", notices["NOTICE.md"])
            add(t, p + "docs/compliance/THIRD-PARTY-NOTICES.md", notices["THIRD-PARTY-NOTICES.md"])
            for rel in ["BUILDING.md", "REUSE.toml", "frontend/app/package-lock.json",
                        "vendor-sources/npm/MANIFEST.txt", "vendor-sources/npm/react-19.3.1.tgz",
                        "packaging/release/toolchain.env", *extra_source]:
                if isinstance(rel, tuple):
                    add(t, p + rel[0], rel[1])
                else:
                    add(t, p + rel)
        top = f"homelab-control-{version}-linux-amd64/"
        with tarfile.open(os.path.join(self.d, n["binaries"]), "w:gz") as t:
            for b in rt.BINARIES + rt.NOTICES:
                add(t, top + b)
        for k, data in notices.items():
            wr(os.path.join(self.d, k), data)
        wr(os.path.join(self.d, "RELEASE-BUILDINFO"), bi)
        wr(os.path.join(self.d, n["deb"]), b"deb")
        wr(os.path.join(self.d, "homelab-control-0.1.0-0.m1.0_1_0_dev_1.x86_64.rpm"), b"rpm")
        wr(os.path.join(self.d, "homelab-control-0.1.0dev.1-1-x86_64.pkg.tar.zst"), b"zst")
        sbom = os.path.join(tmp, "in.spdx.json")
        shutil.copy(os.path.join(ROOT, "docs", "compliance", "sbom", "homelab-control.spdx.json"), sbom)
        rt.stamp_sbom(sbom, os.path.join(self.d, n["sbom"]), version, C, "go1.26.9")
        rt.write_sums(self.d)

    def resum(self):
        rt.write_sums(self.d)


class Versions(unittest.TestCase):
    def test_release_versions(self):
        for ok in ["0.1.0-dev.1", "1.2.3-rc.10", "0.0.1-dev.0"]:
            self.assertEqual(rt.check_version(ok), ok)
        for bad in ["v0.1.0-dev.1", "0.1.0", "0.1.0-beta.1", "01.1.0-dev.1", "0.1.0-dev.01", "0.1.0-dev",
                    "0.1.0-dev.1\n", "0.1.0-dev.1 ; rm -rf /", "", "0.0.0-dryrun.0123abcd"]:
            with self.assertRaises(rt.Fail, msg=bad):
                rt.check_version(bad)

    def test_dry_run_versions(self):
        self.assertTrue(rt.check_version("0.0.0-dryrun.0123abcd", "dry-run"))
        for bad in ["0.1.0-dev.1", "0.0.0-dryrun.XYZ", "0.0.0-dryrun.0123abc"]:
            with self.assertRaises(rt.Fail):
                rt.check_version(bad, "dry-run")

    def test_deb_version_sorts_before_release(self):
        self.assertEqual(rt.deb_version("0.1.0-dev.1"), "0.1.0~dev.1")
        self.assertEqual(rt.deb_version("1.2.3-rc.2"), "1.2.3~rc.2")
        self.assertEqual(rt.deb_version("v0.1.0-3-gabc"), "0.1.0+3+gabc")  # git describe: unchanged rule
        self.assertEqual(rt.deb_version("259cf5bb"), "0.0.0+git.259cf5bb")

    def test_asset_names_are_github_safe(self):
        for n in rt.expected_names(V).values():
            self.assertTrue(rt.SAFE_NAME.fullmatch(n), n)
        self.assertIsNone(rt.SAFE_NAME.fullmatch("homelab-control_0.1.0~dev.1_amd64.deb"))


class Toolchain(unittest.TestCase):
    def test_pins_must_be_exact(self):
        with tempfile.TemporaryDirectory() as t:
            p = os.path.join(t, "tc.env")
            wr(p, PINS)
            self.assertEqual(rt.load_pins(p), {"go": "1.26.9", "node": "22.23.3", "npm": "10.9.9"})
            wr(p, PINS.replace("1.26.9", "1.26.x"))
            with self.assertRaises(rt.Fail):
                rt.load_pins(p)

    def test_repository_pins_are_exact(self):
        rt.load_pins(os.path.join(ROOT, "packaging", "release", "toolchain.env"))


class Verify(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.f = Fixture(self.tmp)

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def run_checks(self, version=V, commit=C):
        c = rt.Checks()
        for msg, fn in [("set", lambda: rt.check_file_set(self.f.d, version)),
                        ("sums", lambda: rt.check_sums(self.f.d)),
                        ("buildinfo", lambda: rt.check_buildinfo(self.f.d, version, commit, rt.load_pins(self.f.toolchain))),
                        ("sbom", lambda: rt.check_sbom(self.f.d, version, commit, rt.load_pins(self.f.toolchain), False)),
                        ("source", lambda: rt.check_source_archive(self.f.d, version)),
                        ("binaries", lambda: rt.check_binaries_tarball(self.f.d, version))]:
            c.run(msg, fn)
        return c

    def assertOnlyFails(self, *names, **kw):
        c = self.run_checks(**kw)
        failed = sorted(m.split(":")[0] for m in c.failures)
        self.assertEqual(failed, sorted(names), c.failures)

    def test_fixture_passes(self):
        self.assertOnlyFails()

    def test_missing_artifact(self):
        os.remove(os.path.join(self.f.d, rt.expected_names(V)["deb"]))
        self.f.resum()
        self.assertOnlyFails("set")

    def test_extra_artifact(self):
        wr(os.path.join(self.f.d, "stray.txt"), "x")
        self.f.resum()
        self.assertOnlyFails("set")

    def test_unsafe_name(self):
        os.rename(os.path.join(self.f.d, rt.expected_names(V)["deb"]), os.path.join(self.f.d, "homelab-control_0.1.0~dev.1_amd64.deb"))
        self.f.resum()
        self.assertOnlyFails("set")

    def test_checksum_mismatch(self):
        with open(os.path.join(self.f.d, "NOTICE.md"), "ab") as f:
            f.write(b"tampered")
        self.assertOnlyFails("sums", "source")  # also differs from the archive copy

    def test_sums_must_cover_every_file(self):
        p = os.path.join(self.f.d, "SHA256SUMS")
        lines = open(p).read().splitlines()
        wr(p, "\n".join(lines[1:]) + "\n")
        self.assertOnlyFails("sums")

    def test_malformed_sums(self):
        open(os.path.join(self.f.d, "SHA256SUMS"), "a").write("not a checksum line\n")
        self.assertOnlyFails("sums")

    def test_wrong_version_or_commit(self):
        self.assertOnlyFails("buildinfo", "sbom", commit="f" * 40)

    def test_toolchain_mismatch(self):
        wr(self.f.toolchain, PINS.replace("22.23.3", "22.23.2"))
        self.assertOnlyFails("buildinfo")

    def test_unstamped_sbom(self):
        n = rt.expected_names(V)["sbom"]
        shutil.copy(os.path.join(ROOT, "docs", "compliance", "sbom", "homelab-control.spdx.json"), os.path.join(self.f.d, n))
        self.f.resum()
        self.assertOnlyFails("sbom")

    def test_webmin_in_source_archive(self):
        shutil.rmtree(self.tmp)
        self.tmp = tempfile.mkdtemp()
        self.f = Fixture(self.tmp, extra_source=["compat/webmin/miniserv.pl"])
        self.assertOnlyFails("source")

    def test_git_dir_in_source_archive(self):
        shutil.rmtree(self.tmp)
        self.tmp = tempfile.mkdtemp()
        self.f = Fixture(self.tmp, extra_source=[".git/config"])
        self.assertOnlyFails("source")

    def test_go_module_sources_required(self):
        allow = ("backend/go-deps-allowlist.json",
                 b'{"modules": [{"path": "github.com/BurntSushi/x", "version": "v1.0.0", "linked": true},'
                 b' {"path": "example.org/graph-only", "version": "v1.0.0", "linked": false}]}')
        shutil.rmtree(self.tmp)
        self.tmp = tempfile.mkdtemp()
        self.f = Fixture(self.tmp, extra_source=[allow])
        self.assertOnlyFails("source")
        shutil.rmtree(self.tmp)
        self.tmp = tempfile.mkdtemp()
        # Module paths are case-escaped in the proxy layout (!burnt!sushi).
        self.f = Fixture(self.tmp, extra_source=[allow, "vendor-sources/go/download/github.com/!burnt!sushi/x/@v/v1.0.0.zip"])
        self.assertOnlyFails()

    def test_links_must_stay_inside_the_tree(self):
        n = rt.expected_names(V)["source"]
        p = f"homelab-control-{V}/"
        for target, ok in [("../../sda", True), ("/etc/passwd", False), ("../../../../../../etc/passwd", False), ("../../../../../../homelab-control-x/a", False)]:
            path = os.path.join(self.f.d, n)
            with tarfile.open(path, "r:gz") as t:
                members = [(m, t.extractfile(m).read() if m.isfile() else None) for m in t.getmembers()]
            with tarfile.open(path, "w:gz") as t:
                for m, data in members:
                    t.addfile(m, io.BytesIO(data) if data is not None else None)
                ti = tarfile.TarInfo(p + "backend/testdata/dev/disk/by-id/link")
                ti.type, ti.linkname = tarfile.SYMTYPE, target
                t.addfile(ti)
            self.f.resum()
            if ok:
                self.assertOnlyFails()
            else:
                self.assertOnlyFails("source")
            shutil.rmtree(self.tmp)
            self.tmp = tempfile.mkdtemp()
            self.f = Fixture(self.tmp)

    def test_verify_cli_fails_closed_on_empty_dir(self):
        empty = os.path.join(self.tmp, "empty")
        os.makedirs(empty)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(rt.main(["verify", "--dir", empty, "--version", V, "--commit", C,
                                      "--toolchain", self.f.toolchain]), 1)

    def test_verify_cli_rejects_bad_version(self):
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(rt.main(["verify", "--dir", self.f.d, "--version", "0.1.0", "--commit", C,
                                      "--toolchain", self.f.toolchain]), 1)


class Assemble(unittest.TestCase):
    def test_assemble_renames_deb_and_writes_sums(self):
        with tempfile.TemporaryDirectory() as t:
            run, arch, out = (os.path.join(t, x) for x in ("run", "arch", "out"))
            os.makedirs(run); os.makedirs(arch)
            for n in ["homelab-control_0.1.0~dev.1_amd64.deb", "LICENSE.md", "SHA256SUMS"]:
                wr(os.path.join(run, n), n)
            wr(os.path.join(arch, "homelab-control-0.1.0dev.1-1-x86_64.pkg.tar.zst"), "z")
            wr(os.path.join(arch, "SHA256SUMS"), "ignored")
            names = rt.assemble(run, arch, out, V)
            self.assertEqual(names, sorted(["homelab-control_0.1.0-dev.1_amd64.deb", "LICENSE.md",
                                            "homelab-control-0.1.0dev.1-1-x86_64.pkg.tar.zst"]))
            rt.check_sums(out)
            with self.assertRaises(rt.Fail):  # never into a non-empty directory
                rt.assemble(run, arch, out, V)

    def test_assemble_requires_one_arch_package(self):
        with tempfile.TemporaryDirectory() as t:
            run, arch, out = (os.path.join(t, x) for x in ("run", "arch", "out"))
            os.makedirs(run); os.makedirs(arch)
            wr(os.path.join(run, "a.deb"), "d")
            with self.assertRaises(rt.Fail):
                rt.assemble(run, arch, out, V)


class SbomAndNotes(unittest.TestCase):
    def test_stamp_is_deterministic(self):
        with tempfile.TemporaryDirectory() as t:
            src = os.path.join(ROOT, "docs", "compliance", "sbom", "homelab-control.spdx.json")
            os.environ["SOURCE_DATE_EPOCH"] = "1760000000"
            try:
                rt.stamp_sbom(src, os.path.join(t, "a"), V, C, "go1.26.9")
                rt.stamp_sbom(src, os.path.join(t, "b"), V, C, "go1.26.9")
            finally:
                del os.environ["SOURCE_DATE_EPOCH"]
            a = open(os.path.join(t, "a")).read()
            self.assertEqual(a, open(os.path.join(t, "b")).read())
            d = json.loads(a)
            self.assertEqual(d["creationInfo"]["created"], "2025-10-09T08:53:20Z")
            prod = [p for p in d["packages"] if p["SPDXID"] == "SPDXRef-Package-homelab-control"][0]
            self.assertEqual(prod["versionInfo"], V)
            with self.assertRaises(rt.Fail):
                rt.stamp_sbom(src, os.path.join(t, "c"), V, "abc", "go1.26.9")

    def test_notes_unsigned_banner_and_checksums(self):
        with tempfile.TemporaryDirectory() as t:
            f = Fixture(t)
            out = os.path.join(t, "notes.md")
            rt.notes(f.d, V, C, rt.REPO_URL, "https://example.invalid/run/1", "",
                     os.path.join(ROOT, "packaging", "release", "RELEASE-NOTES.template.md"), out)
            text = open(out).read()
            self.assertTrue(text.startswith("**UNSIGNED DEVELOPMENT BUILD — v0.1.0-dev.1**"))
            self.assertIn("No cryptographic signatures", text)
            self.assertIn("publishes this release by hand", text)
            self.assertNotIn("SPDX-License-Identifier", text)
            self.assertNotIn("{{", text)
            for n, h in rt.read_sums(os.path.join(f.d, "SHA256SUMS")).items():
                self.assertIn(f"| `{n}` | `{h}` |", text)


if __name__ == "__main__":
    unittest.main()
