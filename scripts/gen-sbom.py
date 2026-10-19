#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Generate the SPDX 2.3 JSON SBOM at docs/compliance/sbom/homelab-control.spdx.json.

Deterministic: content comes only from committed inputs (package-lock.json, go.mod,
compat/webmin.lock, the CI toolchain pins), so the same tree yields the same SBOM.
creationInfo.created is SOURCE_DATE_EPOCH when set; otherwise it is carried over
from the committed SBOM while the content is unchanged, and set to the current UTC
time when the content changes. --check compares everything except `created`:

    scripts/gen-sbom.py          # write
    scripts/gen-sbom.py --check  # fail if the committed SBOM is stale

Stdlib only; needs no network and no installed node_modules.
"""
import base64
import datetime
import hashlib
import json
import os
import re
import subprocess
import sys

GENERATOR = "homelab-control-gen-sbom-1.0"
ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
OUT = os.path.join(ROOT, "docs", "compliance", "sbom", "homelab-control.spdx.json")
LOCK = os.path.join(ROOT, "frontend", "app", "package-lock.json")
REPO = "https://github.com/KRDucky/Homelab-Control---Webmin-Fork"
INPUTS = ["frontend/app/package-lock.json", "backend/go.mod", "compat/webmin.lock", "scripts/gen-sbom.py"]


def read(rel):
    with open(os.path.join(ROOT, rel), encoding="utf-8") as f:
        return f.read()


def spdx_id(s):
    return "SPDXRef-" + re.sub(r"[^A-Za-z0-9.-]", "-", s)


def sri_to_hex(integrity):
    algo, _, b64 = (integrity or "").partition("-")
    if algo not in ("sha512", "sha256", "sha1") or not b64:
        return None
    return {"algorithm": algo.upper(), "checksumValue": base64.b64decode(b64).hex()}


def main():
    lock = json.loads(read("frontend/app/package-lock.json"))
    gomod = read("backend/go.mod")
    go_min = re.search(r"^go (\S+)", gomod, re.M).group(1)
    ci = read(".github/workflows/ci.yml")
    go_ci = re.search(r"go-version: '([^']+)'", ci).group(1)
    node_ci = re.search(r"node-version: '([^']+)'", ci).group(1)
    pin = dict(l.split("=", 1) for l in read("compat/webmin.lock").splitlines() if "=" in l and not l.startswith("#"))
    # Only the CI toolchain pins (not the whole workflow) feed the digest.
    digest = hashlib.sha256(("".join(read(p) for p in INPUTS) + go_ci + node_ci).encode()).hexdigest()
    root_pkg = lock["packages"][""]
    versions = {k.split("node_modules/")[-1]: v["version"] for k, v in lock["packages"].items() if k}

    pkgs, rels = [], []
    product = "SPDXRef-Package-homelab-control"
    pkgs.append({
        "SPDXID": product, "name": "homelab-control", "versionInfo": root_pkg.get("version", "0.0.0"),
        "supplier": "Person: Ian comings", "downloadLocation": "git+" + REPO + ".git",
        "filesAnalyzed": False, "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "GPL-3.0-or-later AND MIT",
        "copyrightText": "Copyright 2026 The Homelab Control Authors",
        "comment": "Release packages (DEB/RPM/Arch): hc-api, hlctl, hc-broker, systemd units, sysusers, default env, "
                   "LICENSE.md, NOTICE.md, THIRD-PARTY-NOTICES.md, README.md (+ Debian copyright). Core code is "
                   "GPL-3.0-or-later; the design tokens compiled into the UI bundle are MIT (file boundaries in "
                   "REUSE.toml). Third-party components are listed as separate packages.",
    })
    rels.append({"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": product})

    go_id = "SPDXRef-Package-go-stdlib"
    pkgs.append({
        "SPDXID": go_id, "name": "go-stdlib", "versionInfo": go_ci,
        "downloadLocation": "https://go.dev/dl/", "filesAnalyzed": False,
        "licenseConcluded": "BSD-3-Clause", "licenseDeclared": "BSD-3-Clause",
        "copyrightText": "Copyright 2009 The Go Authors.",
        "externalRefs": [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": "pkg:golang/stdlib@" + go_ci.replace(".x", "")}],
        "comment": f"Statically linked into all three binaries. go.mod minimum: go {go_min}; CI release toolchain: {go_ci}. "
                   "No third-party Go modules (ADR 0006).",
    })
    rels.append({"spdxElementId": product, "relationshipType": "CONTAINS", "relatedSpdxElement": go_id})

    seen = set()
    for key, meta in sorted(lock["packages"].items()):
        if not key:
            continue
        name = key.split("node_modules/")[-1]
        pid = spdx_id("Package-npm-" + name + "-" + meta["version"])
        if pid in seen:  # npm can install the same name@version at several paths
            continue
        seen.add(pid)
        lic = meta.get("license") or "NOASSERTION"
        p = {
            "SPDXID": pid, "name": name, "versionInfo": meta["version"],
            "downloadLocation": meta.get("resolved", "NOASSERTION"), "filesAnalyzed": False,
            "licenseConcluded": lic if not meta.get("dev") else "NOASSERTION", "licenseDeclared": lic,
            "copyrightText": "NOASSERTION",
            "externalRefs": [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
                              "referenceLocator": "pkg:npm/" + name.replace("@", "%40", 1 if name.startswith("@") else 0) + "@" + meta["version"]}],
        }
        cs = sri_to_hex(meta.get("integrity"))
        if cs:
            p["checksums"] = [cs]
        if meta.get("dev"):
            p["comment"] = "Development/test only; not redistributed."
            rels.append({"spdxElementId": pid, "relationshipType": "DEV_DEPENDENCY_OF", "relatedSpdxElement": product})
        else:
            p["comment"] = "Compiled into the web UI bundle embedded in hc-api; notice in THIRD-PARTY-NOTICES.md."
            rels.append({"spdxElementId": product, "relationshipType": "CONTAINS", "relatedSpdxElement": pid})
        pkgs.append(p)

    wm = "SPDXRef-Package-webmin-reference"
    pkgs.append({
        "SPDXID": wm, "name": "webmin", "versionInfo": pin.get("upstream_tag", "NOASSERTION"),
        "downloadLocation": "git+" + pin.get("upstream_url", "NOASSERTION") + "@" + pin.get("upstream_commit", ""),
        "filesAnalyzed": False, "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "BSD-3-Clause",
        "copyrightText": "Copyright (c) Jamie Cameron and contributors (see compat/webmin/LICENCE and per-file headers)",
        "comment": "Development reference subtree at compat/webmin (tree " + pin.get("upstream_tree", "?") + "). NOT included in "
                   "any release package or release source archive (verified by scripts/package-inventory.sh). "
                   "licenseDeclared is Webmin's top-level licence; many files differ (GPL-2.0-or-later, GPL without "
                   "version, MIT, Apache-2.0, Artistic, Perl terms, some unstated): per-file inventory in "
                   "docs/compliance/WEBMIN-LICENSE-INVENTORY.md and REUSE.toml.",
    })
    rels.append({"spdxElementId": wm, "relationshipType": "TEST_DEPENDENCY_OF", "relatedSpdxElement": product})

    def strip_created(d):
        d = json.loads(json.dumps(d))
        d["creationInfo"].pop("created", None)
        return d

    existing = json.load(open(OUT, encoding="utf-8")) if os.path.exists(OUT) else None
    doc = {
        "spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
        "name": "homelab-control-source-sbom",
        "documentNamespace": f"{REPO}/spdx/{digest}",
        "creationInfo": {
            "creators": ["Person: Ian comings", "Tool: " + GENERATOR],
            "comment": f"Inputs sha256 {digest} over {', '.join(INPUTS)}. Build toolchain (CI): Go {go_ci}, Node.js {node_ci}, "
                       f"npm lockfileVersion {lock.get('lockfileVersion')}, Vite {versions.get('vite', '?')}, TypeScript "
                       f"{versions.get('typescript', '?')}. Package-payload inventories are produced per build by "
                       "scripts/package-inventory.sh.",
        },
        "packages": pkgs,
        "relationships": rels,
    }
    if "--check" in sys.argv:
        if existing is None or strip_created(existing) != strip_created({**doc, "creationInfo": dict(doc["creationInfo"])}):
            sys.exit("docs/compliance/sbom/homelab-control.spdx.json is stale: run scripts/gen-sbom.py")
        print(f"OK: SBOM matches inputs ({len(pkgs)} packages)")
        return
    if os.environ.get("SOURCE_DATE_EPOCH"):
        created = datetime.datetime.fromtimestamp(int(os.environ["SOURCE_DATE_EPOCH"]), datetime.timezone.utc)
    elif existing is not None and strip_created(existing) == strip_created({**doc, "creationInfo": dict(doc["creationInfo"])}):
        created = None
    else:
        created = datetime.datetime.now(datetime.timezone.utc)
    doc["creationInfo"]["created"] = existing["creationInfo"]["created"] if created is None else created.strftime("%Y-%m-%dT%H:%M:%SZ")
    content = json.dumps(doc, indent=2, sort_keys=True) + "\n"
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w", encoding="utf-8") as f:
        f.write(content)
    print(f"wrote {os.path.relpath(OUT, ROOT)}: {len(pkgs)} packages, {len(rels)} relationships")


if __name__ == "__main__":
    main()
