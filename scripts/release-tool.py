#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Release helpers for the Phase 1 unsigned prerelease workflow (ADR 0009, docs/release/RELEASING.md).

Used by .github/workflows/release-build.yml (CI dry run and release) and release.yml. Stdlib only.
Nothing here talks to GitHub, creates tags or publishes anything.

  release-tool.py version <v> [--mode release|dry-run]          validate a version, print the tag
  release-tool.py toolchain <toolchain.env>                     print go=/node=/npm= (GITHUB_OUTPUT format)
  release-tool.py check-toolchain <toolchain.env>               installed go/node/npm must equal the pins
  release-tool.py stamp-sbom <in> <out> --version V --commit C --go goX.Y.Z
  release-tool.py assemble --run DIR --arch DIR --out DIR --version V
  release-tool.py verify --dir DIR --version V --commit C --toolchain F [--mode M] [--require-tools]
  release-tool.py notes --dir DIR --version V --commit C --repo R --run-url U [--ci-run-url U]
                        --template T --out F

`verify` fails closed: every check runs, every failure is printed, and the exit status is 1 if any
check failed. A release must never be drafted from a directory that `verify` has not passed.
"""
import argparse
import datetime
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPO_URL = "https://github.com/KRDucky/Homelab-Control---Webmin-Fork"
NUM = r"(0|[1-9][0-9]*)"
# All patterns below are used with fullmatch() ('$' would also accept a trailing newline).
# Release workflow: SemVer prerelease tags only (RELEASE-PIPELINE.md §3: vX.Y.Z-dev.N / vX.Y.Z-rc.N).
RELEASE_RE = re.compile(rf"{NUM}\.{NUM}\.{NUM}-(dev|rc)\.{NUM}")
# CI dry run: fixed 0.0.0 plus the first 8 hex digits of the commit, so it can never look like a release.
DRYRUN_RE = re.compile(r"0\.0\.0-dryrun\.[0-9a-f]{8}")
SAFE_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*")  # names GitHub keeps unchanged as asset names
PIN_RE = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
NOTICES = ["LICENSE.md", "NOTICE.md", "THIRD-PARTY-NOTICES.md"]
BINARIES = ["hc-api", "hc-broker", "hlctl"]


class Fail(Exception):
    pass


# ---------------------------------------------------------------- versions and names
def check_version(v, mode="release"):
    rx = RELEASE_RE if mode == "release" else DRYRUN_RE
    if not rx.fullmatch(v or ""):
        want = "X.Y.Z-dev.N or X.Y.Z-rc.N (no leading 'v')" if mode == "release" else "0.0.0-dryrun.<8 hex>"
        raise Fail(f"invalid {mode} version {v!r}: expected {want}")
    return v


def deb_version(v):
    """Mirror of packaging/debian/build-deb.sh: SemVer prerelease '-' becomes '~' (sorts before X.Y.Z)."""
    v = re.sub(r"^v", "", v)
    if re.match(r"^[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z.]+$", v):
        v = v.replace("-", "~", 1)
    v = re.sub(r"[^A-Za-z0-9.+~]", "+", v)
    return v if re.match(r"^[0-9]+\.[0-9]+", v) else "0.0.0+git." + v


def expected_names(v):
    """Exact asset names, except the RPM and Arch package (one each, matched by pattern)."""
    n = f"homelab-control-{v}"
    return {
        "source": f"{n}-source.tar.gz",
        "binaries": f"{n}-linux-amd64.tar.gz",
        "deb": f"homelab-control_{v}_amd64.deb",
        "sbom": f"{n}.spdx.json",
        "buildinfo": "RELEASE-BUILDINFO",
        "sums": "SHA256SUMS",
        **{k: k for k in NOTICES},
    }


RPM_RE = re.compile(r"homelab-control-[0-9][A-Za-z0-9._]*-[A-Za-z0-9._]+\.x86_64\.rpm")
ARCH_RE = re.compile(r"homelab-control-[0-9][A-Za-z0-9._]*-[0-9]+-x86_64\.pkg\.tar\.zst")


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def write_sums(d):
    names = sorted(n for n in os.listdir(d) if n != "SHA256SUMS")
    with open(os.path.join(d, "SHA256SUMS"), "w", encoding="utf-8") as f:
        for n in names:
            f.write(f"{sha256(os.path.join(d, n))}  {n}\n")
    return names


def read_sums(path):
    out = {}
    with open(path, encoding="utf-8") as f:
        for i, line in enumerate(f, 1):
            m = re.match(r"^([0-9a-f]{64})  (\S+)$", line.rstrip("\n"))
            if not m:
                raise Fail(f"SHA256SUMS line {i} is malformed: {line.rstrip()!r}")
            if m.group(2) in out:
                raise Fail(f"SHA256SUMS lists {m.group(2)} twice")
            out[m.group(2)] = m.group(1)
    return out


def parse_kv(path):
    out = {}
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, _, v = line.partition("=")
                out[k.strip()] = v.strip()
    return out


def load_pins(path):
    p = parse_kv(path)
    pins = {"go": p.get("GO_VERSION", ""), "node": p.get("NODE_VERSION", ""), "npm": p.get("NPM_VERSION", "")}
    for k, v in pins.items():
        if not PIN_RE.fullmatch(v):
            raise Fail(f"{path}: {k} pin {v!r} is not an exact X.Y.Z version")
    return pins


# ---------------------------------------------------------------- SBOM
def stamp_sbom(src, dst, version, commit, go):
    """Release copy of the committed SBOM: product version, commit, exact Go toolchain, unique namespace."""
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise Fail(f"commit must be a full 40-hex SHA, got {commit!r}")
    if not re.fullmatch(r"go[0-9]+\.[0-9]+\.[0-9]+", go):
        raise Fail(f"go toolchain must look like go1.2.3, got {go!r}")
    with open(src, encoding="utf-8") as f:
        doc = json.load(f)
    product = next((p for p in doc["packages"] if p["SPDXID"] == "SPDXRef-Package-homelab-control"), None)
    std = next((p for p in doc["packages"] if p["SPDXID"] == "SPDXRef-Package-go-stdlib"), None)
    if product is None or std is None:
        raise Fail("SBOM lacks the homelab-control or go-stdlib package")
    doc["name"] = f"homelab-control-{version}"
    doc["documentNamespace"] = f"{REPO_URL}/spdx/release/{version}/{commit}"
    product["versionInfo"] = version
    product["downloadLocation"] = f"git+{REPO_URL}.git@{commit}"
    product["sourceInfo"] = (f"Built from commit {commit}; complete corresponding source in "
                             f"homelab-control-{version}-source.tar.gz (compat/webmin/ excluded).")
    gv = go[2:]
    std["versionInfo"] = gv
    std["externalRefs"] = [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
                            "referenceLocator": f"pkg:golang/stdlib@{gv}"}]
    sde = os.environ.get("SOURCE_DATE_EPOCH")
    if sde:  # deterministic: the release commit's timestamp
        doc["creationInfo"]["created"] = datetime.datetime.fromtimestamp(
            int(sde), datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    doc["creationInfo"]["comment"] = (doc["creationInfo"].get("comment", "") +
                                      f" Release stamp: version {version}, commit {commit}, Go toolchain {go}.")
    with open(dst, "w", encoding="utf-8") as f:
        f.write(json.dumps(doc, indent=2, sort_keys=True) + "\n")


# ---------------------------------------------------------------- assemble
def assemble(run, arch, out, version):
    """Release directory = run a of release-bundle.sh + the Arch package; asset-safe names; new SHA256SUMS."""
    names = expected_names(version)
    os.makedirs(out, exist_ok=True)
    if os.listdir(out):
        raise Fail(f"refusing: {out} is not empty")
    debs = [n for n in os.listdir(run) if n.endswith(".deb")]
    if len(debs) != 1:
        raise Fail(f"expected exactly one .deb in {run}, found {debs}")
    for n in sorted(os.listdir(run)):
        if n == "SHA256SUMS":
            continue
        dst = names["deb"] if n.endswith(".deb") else n  # .deb file name carries '~'; the asset uses the SemVer
        shutil.copy2(os.path.join(run, n), os.path.join(out, dst))
    pkgs = [n for n in os.listdir(arch) if n.endswith(".pkg.tar.zst")]
    if len(pkgs) != 1:
        raise Fail(f"expected exactly one Arch package in {arch}, found {pkgs}")
    shutil.copy2(os.path.join(arch, pkgs[0]), os.path.join(out, pkgs[0]))
    for n in os.listdir(out):
        if not SAFE_NAME.fullmatch(n):
            raise Fail(f"asset name {n!r} contains characters GitHub would rename")
    return write_sums(out)


# ---------------------------------------------------------------- verify
class Checks:
    def __init__(self):
        self.failures, self.passed = [], []

    def ok(self, cond, msg):
        (self.passed if cond else self.failures).append(msg)
        return cond

    def run(self, msg, fn):
        try:
            fn()
            self.passed.append(msg)
        except Fail as e:
            self.failures.append(f"{msg}: {e}")
        except Exception as e:  # any unexpected error is a failure, never a pass
            self.failures.append(f"{msg}: {type(e).__name__}: {e}")


def check_file_set(d, version):
    names = expected_names(version)
    have = set(os.listdir(d))
    missing = [n for n in names.values() if n not in have]
    rpms = [n for n in have if RPM_RE.fullmatch(n)]
    archs = [n for n in have if ARCH_RE.fullmatch(n)]
    if len(rpms) != 1:
        missing.append(f"exactly one RPM (found {rpms})")
    if len(archs) != 1:
        missing.append(f"exactly one Arch package (found {archs})")
    extra = sorted(have - set(names.values()) - set(rpms) - set(archs))
    if missing:
        raise Fail("missing: " + ", ".join(missing))
    if extra:
        raise Fail("unexpected files: " + ", ".join(extra))
    bad = [n for n in have if not SAFE_NAME.fullmatch(n)]
    if bad:
        raise Fail("unsafe asset names: " + ", ".join(bad))
    for n in have:
        p = os.path.join(d, n)
        if os.path.islink(p) or not os.path.isfile(p) or os.path.getsize(p) == 0:
            raise Fail(f"{n} is not a non-empty regular file")
    return rpms[0], archs[0]


def check_sums(d):
    sums = read_sums(os.path.join(d, "SHA256SUMS"))
    files = set(os.listdir(d)) - {"SHA256SUMS"}
    if set(sums) != files:
        raise Fail(f"SHA256SUMS covers {sorted(set(sums) ^ files)} incorrectly (must list every other file once)")
    wrong = [n for n, h in sums.items() if sha256(os.path.join(d, n)) != h]
    if wrong:
        raise Fail("checksum mismatch: " + ", ".join(sorted(wrong)))


def check_buildinfo(d, version, commit, pins):
    bi = parse_kv(os.path.join(d, "RELEASE-BUILDINFO"))
    want = {"version": version, "commit": commit, "go": "go" + pins["go"], "node": "v" + pins["node"], "npm": pins["npm"]}
    diff = [f"{k}={bi.get(k)!r} (expected {v!r})" for k, v in want.items() if bi.get(k) != v]
    if not re.fullmatch(r"[0-9]+", bi.get("source_date_epoch", "")):
        diff.append("source_date_epoch missing")
    if "compat/webmin/" not in bi.get("excluded", ""):
        diff.append("excluded= does not record compat/webmin/")
    if diff:
        raise Fail("; ".join(diff))


def check_sbom(d, version, commit, pins, require_tools):
    path = os.path.join(d, expected_names(version)["sbom"])
    with open(path, encoding="utf-8") as f:
        doc = json.load(f)
    pk = {p["SPDXID"]: p for p in doc.get("packages", [])}
    prod, std = pk.get("SPDXRef-Package-homelab-control", {}), pk.get("SPDXRef-Package-go-stdlib", {})
    errs = []
    if doc.get("spdxVersion") != "SPDX-2.3":
        errs.append("spdxVersion is not SPDX-2.3")
    if prod.get("versionInfo") != version:
        errs.append(f"product versionInfo {prod.get('versionInfo')!r} != {version!r} (SBOM not stamped)")
    if commit not in prod.get("downloadLocation", ""):
        errs.append("product downloadLocation does not name the commit")
    if std.get("versionInfo") != pins["go"]:
        errs.append(f"go-stdlib versionInfo {std.get('versionInfo')!r} != {pins['go']!r}")
    if version not in doc.get("documentNamespace", "") or commit not in doc.get("documentNamespace", ""):
        errs.append("documentNamespace is not release-specific")
    if errs:
        raise Fail("; ".join(errs))
    tool = shutil.which("pyspdxtools")
    if tool:
        r = subprocess.run([tool, "-i", path], capture_output=True, text=True)
        if r.returncode != 0:
            raise Fail("pyspdxtools rejected the SBOM: " + (r.stdout + r.stderr).strip()[-400:])
    elif require_tools:
        raise Fail("pyspdxtools not installed (required)")


def _members(path):
    with tarfile.open(path, "r:gz") as t:
        return t.getmembers()


def check_source_archive(d, version):
    names = expected_names(version)
    prefix = f"homelab-control-{version}/"
    ms = _members(os.path.join(d, names["source"]))
    paths = [m.name for m in ms]
    errs = []
    outside = [p for p in paths if not (p + "/").startswith(prefix) or ".." in p.split("/") or p.startswith("/")]
    if outside:
        errs.append(f"members outside {prefix}: {outside[:5]}")
    webmin = [p for p in paths if p.startswith(prefix + "compat/webmin/") or p.rstrip("/") == prefix + "compat/webmin"]
    if webmin:
        errs.append(f"contains compat/webmin/ ({len(webmin)} entries, e.g. {webmin[0]})")
    if any(p.split("/")[1:2] == [".git"] for p in paths):
        errs.append("contains .git")
    def escapes(m):  # a link whose target resolves outside the archive's top directory
        if m.issym():
            if m.linkname.startswith("/"):
                return True
            target = os.path.normpath(os.path.join(os.path.dirname(m.name), m.linkname))
        else:
            target = os.path.normpath(m.linkname)
        return not (target + "/").startswith(prefix)
    links = [f"{m.name} -> {m.linkname}" for m in ms if (m.issym() or m.islnk()) and escapes(m)]
    if links:
        errs.append(f"links pointing outside the tree: {links[:5]}")
    for req in ["RELEASE-BUILDINFO", "BUILDING.md", "LICENSE.md", "NOTICE.md", "REUSE.toml",
                "docs/compliance/THIRD-PARTY-NOTICES.md", "frontend/app/package-lock.json",
                "vendor-sources/npm/MANIFEST.txt", "packaging/release/toolchain.env"]:
        if prefix + req not in paths:
            errs.append(f"missing {req}")
    if not any(p.startswith(prefix + "vendor-sources/npm/") and p.endswith(".tgz") for p in paths):
        errs.append("no npm source tarballs under vendor-sources/npm/")
    # Go module sources (ADR 0006 amendment 1): every linked module's zip must be in the archive.
    if prefix + "backend/go-deps-allowlist.json" in paths:
        with tarfile.open(os.path.join(d, names["source"]), "r:gz") as t:
            allow = json.loads(t.extractfile(prefix + "backend/go-deps-allowlist.json").read())["modules"]
        for m in (m for m in allow if m.get("linked")):
            esc = re.sub(r"[A-Z]", lambda c: "!" + c.group(0).lower(), m["path"])
            z = f"{prefix}vendor-sources/go/download/{esc}/@v/{m['version']}.zip"
            if z not in paths:
                errs.append(f"Go module source missing: {m['path']} {m['version']} (vendor-sources/go/download)")
    if errs:
        raise Fail("; ".join(errs))
    # The archive's copies must be the published ones.
    with tarfile.open(os.path.join(d, names["source"]), "r:gz") as t:
        def read(rel):
            return t.extractfile(prefix + rel).read()
        pairs = [("RELEASE-BUILDINFO", "RELEASE-BUILDINFO"), ("LICENSE.md", "LICENSE.md"), ("NOTICE.md", "NOTICE.md"),
                 ("THIRD-PARTY-NOTICES.md", "docs/compliance/THIRD-PARTY-NOTICES.md")]
        for asset, rel in pairs:
            with open(os.path.join(d, asset), "rb") as f:
                if f.read() != read(rel):
                    errs.append(f"{asset} differs from the copy in the source archive")
    if errs:
        raise Fail("; ".join(errs))


def check_binaries_tarball(d, version):
    names = expected_names(version)
    top = f"homelab-control-{version}-linux-amd64"
    ms = _members(os.path.join(d, names["binaries"]))
    files = sorted(m.name for m in ms if m.isfile())
    want = sorted(f"{top}/{n}" for n in BINARIES + NOTICES)
    if files != want:
        raise Fail(f"binaries tarball contents {files} != {want}")
    other = [m.name for m in ms if not (m.isfile() or (m.isdir() and m.name.rstrip("/") == top))]
    if other:
        raise Fail(f"unexpected entries {other}")


def check_packages(d, version, rpm, arch, require_tools):
    inv = os.path.join(ROOT, "scripts", "package-inventory.sh")
    names = expected_names(version)
    needs = {"deb": "dpkg-deb", "rpm": "rpm", "arch": "zstd"}
    for kind, pkg in (("deb", names["deb"]), ("rpm", rpm), ("arch", arch)):
        if not shutil.which(needs[kind]):
            if require_tools:
                raise Fail(f"{needs[kind]} not installed (required to inspect {pkg})")
            print(f"SKIPPED: {pkg} inventory ({needs[kind]} not installed)")
            continue
        r = subprocess.run([inv, os.path.join(d, pkg)], capture_output=True, text=True)
        if r.returncode != 0:
            raise Fail(f"package-inventory.sh {pkg}: {(r.stdout + r.stderr).strip()[-600:]}")
    if shutil.which("dpkg-deb"):
        got = subprocess.run(["dpkg-deb", "-f", os.path.join(d, names["deb"]), "Version"],
                             capture_output=True, text=True, check=True).stdout.strip()
        if got != deb_version(version):
            raise Fail(f".deb Version {got!r} != {deb_version(version)!r}")


def verify(d, version, commit, toolchain, mode, require_tools):
    c = Checks()
    pins = None
    c.run("version format", lambda: check_version(version, mode))
    c.ok(bool(re.fullmatch(r"[0-9a-f]{40}", commit or "")), f"commit is a full 40-hex SHA ({commit!r})")
    try:
        pins = load_pins(toolchain)
        c.passed.append("toolchain pins readable")
    except Fail as e:
        c.failures.append(f"toolchain pins: {e}")
    rpm = arch = None
    try:
        rpm, arch = check_file_set(d, version)
        c.passed.append("exact release file set, safe names, non-empty regular files")
    except Fail as e:
        c.failures.append(f"release file set: {e}")
    present = set(os.listdir(d)) if os.path.isdir(d) else set()
    n = expected_names(version)
    if "SHA256SUMS" in present:
        c.run("SHA256SUMS covers every file and matches", lambda: check_sums(d))
    if pins and "RELEASE-BUILDINFO" in present:
        c.run("RELEASE-BUILDINFO: version, commit, exact toolchain (R6)",
              lambda: check_buildinfo(d, version, commit, pins))
    if pins and n["sbom"] in present:
        c.run("SBOM: SPDX 2.3, stamped with version/commit/toolchain (R4), validated",
              lambda: check_sbom(d, version, commit, pins, require_tools))
    if n["source"] in present:
        c.run("source archive: complete, no compat/webmin, notices identical", lambda: check_source_archive(d, version))
    if n["binaries"] in present:
        c.run("binaries tarball: exact contents", lambda: check_binaries_tarball(d, version))
    if rpm and arch:
        c.run("packages: payload inventory (no Webmin, allow-list, notices), .deb version",
              lambda: check_packages(d, version, rpm, arch, require_tools))
    for m in c.passed:
        print(f"PASS  {m}")
    for m in c.failures:
        print(f"FAIL  {m}")
        if os.environ.get("GITHUB_ACTIONS"):
            print(f"::error title=release verify::{m}")
    print(f"release verify: {'OK' if not c.failures else str(len(c.failures)) + ' failed'} ({d})")
    return not c.failures


# ---------------------------------------------------------------- notes
def notes(d, version, commit, repo, run_url, ci_run_url, template, out):
    with open(template, encoding="utf-8") as f:
        text = f.read()
    text = re.sub(r"\A<!--.*?-->\s*", "", text, flags=re.S)  # licence header of the template
    bi = parse_kv(os.path.join(d, "RELEASE-BUILDINFO"))
    sums = read_sums(os.path.join(d, "SHA256SUMS"))
    table = "\n".join(f"| `{n}` | `{h}` |" for n, h in sorted(sums.items()))
    values = {
        "VERSION": version, "TAG": "v" + version, "COMMIT": commit, "SHORT": commit[:12], "REPO": repo,
        "RUN_URL": run_url, "CI_RUN_URL": ci_run_url or "(no successful main CI run was linked)",
        "GO": bi.get("go", "?"), "NODE": bi.get("node", "?"), "NPM": bi.get("npm", "?"),
        "SDE": bi.get("source_date_epoch", "?"), "CHECKSUMS": table,
        "DEB": expected_names(version)["deb"], "DEBVER": deb_version(version),
    }
    for k, v in values.items():
        text = text.replace("{{" + k + "}}", v)
    left = re.findall(r"\{\{[A-Z_]+\}\}", text)
    if left:
        raise Fail(f"unreplaced placeholders in notes: {left}")
    if not text.startswith("**UNSIGNED DEVELOPMENT BUILD"):
        raise Fail("notes must start with **UNSIGNED DEVELOPMENT BUILD")
    with open(out, "w", encoding="utf-8") as f:
        f.write(text)


# ---------------------------------------------------------------- CLI
def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sp = ap.add_subparsers(dest="cmd", required=True)
    a = sp.add_parser("version"); a.add_argument("v"); a.add_argument("--mode", default="release", choices=["release", "dry-run"])
    a = sp.add_parser("toolchain"); a.add_argument("file")
    a = sp.add_parser("check-toolchain"); a.add_argument("file")
    a = sp.add_parser("stamp-sbom"); a.add_argument("src"); a.add_argument("dst")
    a.add_argument("--version", required=True); a.add_argument("--commit", required=True); a.add_argument("--go", required=True)
    a = sp.add_parser("assemble")
    for o in ("--run", "--arch", "--out", "--version"):
        a.add_argument(o, required=True)
    a = sp.add_parser("verify")
    for o in ("--dir", "--version", "--commit", "--toolchain"):
        a.add_argument(o, required=True)
    a.add_argument("--mode", default="release", choices=["release", "dry-run"]); a.add_argument("--require-tools", action="store_true")
    a = sp.add_parser("notes")
    for o in ("--dir", "--version", "--commit", "--repo", "--run-url", "--template", "--out"):
        a.add_argument(o, required=True)
    a.add_argument("--ci-run-url", default="")
    args = ap.parse_args(argv)
    try:
        if args.cmd == "version":
            print("v" + check_version(args.v, args.mode))
        elif args.cmd == "toolchain":
            for k, v in load_pins(args.file).items():
                print(f"{k}={v}")
        elif args.cmd == "check-toolchain":
            pins = load_pins(args.file)
            got = {
                "go": subprocess.run(["go", "env", "GOVERSION"], capture_output=True, text=True).stdout.strip(),
                "node": subprocess.run(["node", "--version"], capture_output=True, text=True).stdout.strip(),
                "npm": subprocess.run(["npm", "--version"], capture_output=True, text=True).stdout.strip(),
            }
            want = {"go": "go" + pins["go"], "node": "v" + pins["node"], "npm": pins["npm"]}
            bad = [f"{k}: installed {got[k]!r}, pinned {want[k]!r}" for k in want if got[k] != want[k]]
            if bad:
                raise Fail("toolchain does not match packaging/release/toolchain.env: " + "; ".join(bad))
            print("OK: toolchain " + " ".join(want.values()))
            if os.environ.get("GITHUB_ACTIONS"):  # evidence readable through the checks API
                print("::notice title=release toolchain::" + " ".join(want.values()) + " (matches packaging/release/toolchain.env)")
        elif args.cmd == "stamp-sbom":
            stamp_sbom(args.src, args.dst, args.version, args.commit, args.go)
        elif args.cmd == "assemble":
            for n in assemble(args.run, args.arch, args.out, args.version):
                print(n)
        elif args.cmd == "verify":
            return 0 if verify(args.dir, args.version, args.commit, args.toolchain, args.mode, args.require_tools) else 1
        elif args.cmd == "notes":
            notes(args.dir, args.version, args.commit, args.repo, args.run_url, args.ci_run_url, args.template, args.out)
    except Fail as e:
        print(f"FAIL: {e}", file=sys.stderr)
        if os.environ.get("GITHUB_ACTIONS"):
            print(f"::error title=release-tool {args.cmd}::{e}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
