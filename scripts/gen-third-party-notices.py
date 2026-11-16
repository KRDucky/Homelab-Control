#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Generate docs/compliance/THIRD-PARTY-NOTICES.md and packaging/debian/copyright for release artifacts.

Covers exactly what Homelab Control redistributes in its binaries and packages:
the npm production dependencies compiled into the web UI bundle (which hc-api
embeds) and the Go standard library compiled into every binary. License texts
are copied verbatim from the installed packages and the Go toolchain, so the
file must be regenerated whenever a shipped dependency changes:

    scripts/gen-third-party-notices.py          # write
    scripts/gen-third-party-notices.py --check  # fail if a committed file is stale

packaging/debian/copyright is the same information in Debian's machine-readable
format (DEP-5, https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/),
installed by the .deb as /usr/share/doc/homelab-control/copyright.

Also covers the crates compiled into hc-audit-retention (Rust, ADR 0006 amendment 4): the
reviewed allow-list backend/audit-retention/rust-deps-allowlist.json, with each crate's licence
and notice files read from the cargo registry (CARGO_HOME) or vendor-sources/cargo.

Requires `npm ci` (frontend/app/node_modules), `go` on PATH and the crate sources
(`cargo fetch --locked` in backend/audit-retention). Stdlib only.
"""
import glob
import hashlib
import json
import os
import re
import subprocess
import sys

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=True).stdout.strip()
APP = os.path.join(ROOT, "frontend", "app")
OUT = os.path.join(ROOT, "docs", "compliance", "THIRD-PARTY-NOTICES.md")
OUT_DEB = os.path.join(ROOT, "packaging", "debian", "copyright")
REPO = "https://github.com/KRDucky/Homelab-Control---Webmin-Fork"
# MIT boundary for Homelab Control's own files (must match REUSE.toml, LA-03).
MIT_PATHS = ["sdk/*", "schemas/*", "themes/*", "frontend/design-system/*",
             "docs/ui/theme.schema.v1.draft.json", "docs/ui/examples/themes/*"]
DEB_LICENSE = {"MIT": "Expat", "OFL-1.1": "OFL-1.1", "BSD-3-Clause": "BSD-3-clause", "Apache-2.0": "Apache-2.0"}
# SPDX -> DEP-5 short names for crate licences (ADR 0006 amendment 4 approved set).
DEB_CRATE_LICENSE = {"MIT": "Expat", "Apache-2.0": "Apache-2.0", "Zlib": "Zlib", "Unicode-3.0": "Unicode-3.0",
                     "BSL-1.0": "BSL-1.0", "0BSD": "0BSD", "Unlicense": "Unlicense", "ISC": "ISC",
                     "BSD-2-Clause": "BSD-2-clause", "BSD-3-Clause": "BSD-3-clause"}
CRATE_FILES = ("LICENSE", "LICENCE", "COPYING", "NOTICE", "UNLICENSE")
MIT_TEMPLATE = """Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE."""
# sha256 of the whitespace-collapsed Apache-2.0 terms ("TERMS AND CONDITIONS ..." up to
# "END OF TERMS AND CONDITIONS"), identical to the SPDX reference text. Only a licence whose terms
# match exactly may be replaced by a reference to /usr/share/common-licenses/Apache-2.0.
APACHE_2_TERMS_SHA256 = "efc42672d5e2255417ab2bbcef2a39e8400f0ab6d32f4c790d95a3640087923c"
# Debian Policy 12.5 / lintian copyright-file-contains-full-apache-2-license: refer to the
# common-licenses copy instead of embedding the full text. The full licence texts and every NOTICE
# file stay verbatim in THIRD-PARTY-NOTICES.md (shipped in /usr/share/doc/homelab-control/).
APACHE_2_DEB = """Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this software except in compliance with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the
License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
either express or implied. See the License for the specific language governing permissions
and limitations under the License.

On Debian systems, the complete text of the Apache License, Version 2.0 can be found in
"/usr/share/common-licenses/Apache-2.0". The complete licence texts and NOTICE files shipped
with the Apache-2.0 components are reproduced verbatim in THIRD-PARTY-NOTICES.md."""


def license_file(pkg_dir):
    for name in sorted(os.listdir(pkg_dir)):
        if name.upper().startswith(("LICENSE", "LICENCE", "COPYING")):
            return os.path.join(pkg_dir, name)
    return None


def shipped_npm():
    lock = json.load(open(os.path.join(APP, "package-lock.json"), encoding="utf-8"))
    out = []
    for key, meta in sorted(lock["packages"].items()):
        if not key or meta.get("dev") or meta.get("optional") and not os.path.isdir(os.path.join(APP, key)):
            continue
        d = os.path.join(APP, key)
        pj = json.load(open(os.path.join(d, "package.json"), encoding="utf-8"))
        repo = pj.get("repository")
        if isinstance(repo, dict):
            repo = repo.get("url", "")
        lf = license_file(d)
        if not lf:
            sys.exit(f"{key}: no license file shipped with the package; resolve before release")
        out.append({
            "name": key.split("node_modules/")[-1], "version": meta["version"],
            "license": meta.get("license") or pj.get("license"),
            "source": (repo or pj.get("homepage") or "").replace("git+", ""),
            "text": open(lf, encoding="utf-8").read().strip(),
        })
    return out


def go_modules():
    """Third-party Go modules linked into the binaries (reviewed allow-list, ADR 0006
    amendment 1) with every licence/notice file each module ships."""
    allow = json.load(open(os.path.join(ROOT, "backend", "go-deps-allowlist.json"), encoding="utf-8"))["modules"]
    cache = subprocess.run(["go", "env", "GOMODCACHE"], capture_output=True, text=True, check=True,
                           cwd=os.path.join(ROOT, "backend")).stdout.strip()
    out = []
    for m in allow:
        if not m["linked"]:
            continue
        esc = re.sub(r"[A-Z]", lambda c: "!" + c.group(0).lower(), m["path"])
        d = os.path.join(cache, esc + "@" + m["version"])
        if not os.path.isdir(d):
            sys.exit(f"{m['path']} {m['version']} not in the module cache; run `go mod download` in backend/")
        files = sorted(f for f in os.listdir(d) if f.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"))
                       and os.path.isfile(os.path.join(d, f)))
        if not any(f.upper().startswith(("LICENSE", "LICENCE", "COPYING")) for f in files):
            sys.exit(f"{m['path']}: no licence file; resolve before release")
        out.append({**m, "files": [(f, open(os.path.join(d, f), encoding="utf-8").read().strip()) for f in files]})
    return out


def crate_dir(name, version):
    home = os.environ.get("CARGO_HOME") or os.path.join(os.path.expanduser("~"), ".cargo")
    for d in sorted(glob.glob(os.path.join(home, "registry", "src", "index.crates.io-*", f"{name}-{version}"))):
        return d
    v = os.path.join(ROOT, "vendor-sources", "cargo", f"{name}-{version}")
    if os.path.isdir(v):
        return v
    sys.exit(f"crate {name} {version}: source not found; run `cargo fetch --locked` in backend/audit-retention")


def crate_authors(d):
    import tomllib
    with open(os.path.join(d, "Cargo.toml"), "rb") as f:
        authors = tomllib.load(f).get("package", {}).get("authors", [])
    return ", ".join(re.sub(r"\s*<[^>]*>", "", a) for a in authors) if authors else ""


def rust_crates():
    """Crates compiled into hc-audit-retention with every licence/notice file each ships.
    A crate without a licence file needs a reviewed `elected` entry in the allow-list."""
    allow = json.load(open(os.path.join(ROOT, "backend", "audit-retention", "rust-deps-allowlist.json"), encoding="utf-8"))["crates"]
    out = []
    for c in allow:
        if not c["linked"]:
            continue
        d = crate_dir(c["name"], c["version"])
        files = sorted(f for f in os.listdir(d) if f.upper().startswith(CRATE_FILES) and os.path.isfile(os.path.join(d, f)))
        # Trailing spaces in a crate's licence file carry no meaning; drop them so the notices stay clean.
        texts = [(f, "\n".join(l.rstrip() for l in open(os.path.join(d, f), encoding="utf-8").read().strip().splitlines()))
                 for f in files]
        authors = crate_authors(d) or f"the {c['name']} authors"
        if not any(f.upper().startswith(("LICENSE", "LICENCE", "COPYING", "UNLICENSE")) for f in files):
            if c.get("elected") != "MIT":
                sys.exit(f"crate {c['name']} {c['version']}: no licence file; record a reviewed `elected` licence in the allow-list")
            texts = [("LICENSE-MIT (standard text; the crate ships no licence file)", f"Copyright (c) {authors}\n\n{MIT_TEMPLATE}")]
        out.append({**c, "dir": d, "files": texts, "authors": authors})
    return out


def crate_holder(c):
    for _, t in c["files"]:
        for line in t.splitlines():
            m = re.match(r"\s*Copyright\s+(?:\([cC]\)\s*)?(.*\S)", line)
            if m and not re.search(r"[\[{<](yyyy|year|name)", m.group(1), re.I):
                return m.group(1)
    return c["authors"]


def crate_deb_license(c):
    """DEP-5 licence field for a crate, with the SPDX operators spelled the Debian way."""
    expr = c.get("elected") or c["license"]
    toks = expr.replace("/", " OR ").replace("(", " ( ").replace(")", " ) ").split()
    out = []
    for t in toks:
        if t in ("OR", "AND"):
            out.append(t.lower())
        elif t in ("(", ")"):
            out.append(t)
        else:
            if t not in DEB_CRATE_LICENSE:
                sys.exit(f"crate {c['name']}: no Debian licence short name for {t}")
            out.append(DEB_CRATE_LICENSE[t])
    return " ".join(out).replace("( ", "(").replace(" )", ")")


def go_stdlib():
    goroot = subprocess.run(["go", "env", "GOROOT"], capture_output=True, text=True, check=True).stdout.strip()
    return open(os.path.join(goroot, "LICENSE"), encoding="utf-8").read().strip()


def holder(text):
    """First copyright statement of a licence file, without the 'Copyright (c)' prefix."""
    for line in text.splitlines():
        m = re.match(r"\s*Copyright\s+(?:\([cC]\)\s*)?(.*\S)", line)
        if m and not re.search(r"[\[{]yyyy[\]}]", m.group(1)):  # skip the Apache-2.0 appendix template
            h = m.group(1)
            if "Copyright" in h:  # OFL files repeat the statement per font file
                h = h.split(")", 1)[0] + ")"
            return h
    sys.exit("no copyright line found in a shipped licence; resolve before release")


def apache2_terms_sha256(text):
    """sha256 of an Apache-2.0 licence's terms with whitespace collapsed, or None if absent."""
    start = text.find("TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION")
    end = text.find("END OF TERMS AND CONDITIONS")
    if start < 0 or end < start:
        return None
    return hashlib.sha256(" ".join(text[start:end].split()).encode()).hexdigest()


def require_standard_apache2(what, text):
    """Fail closed unless `text` is the unmodified Apache License 2.0."""
    if apache2_terms_sha256(text) != APACHE_2_TERMS_SHA256:
        sys.exit(f"{what}: licence text is not the unmodified Apache-2.0 terms; review it and "
                 "render it in full instead of referring to /usr/share/common-licenses/Apache-2.0")


def deb_text(text):
    """Licence text as a DEP-5 continuation block (one leading space, '.' for blank lines)."""
    return "\n".join(" " + l.rstrip() if l.strip() else " ." for l in text.strip().splitlines())


def render_debian():
    npm = shipped_npm()
    go = go_stdlib()
    out = [
        "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/",
        "Upstream-Name: Homelab Control",
        f"Upstream-Contact: {REPO}/issues",
        f"Source: {REPO}",
        "Comment: Generated by scripts/gen-third-party-notices.py. Do not edit by hand.",
        " This binary package contains no Webmin code: compat/webmin/ in the source repository",
        " is a development reference that is not used to build or included in this package.",
        f" The Go standard library ({holder(go)}, BSD-3-clause-Go below) is statically linked into",
        " /usr/bin/hc-api, /usr/bin/hlctl and /usr/lib/homelab-control/hc-broker; it comes from",
        " the Go toolchain, not this source tree. See also NOTICE.md and THIRD-PARTY-NOTICES.md.",
        "",
        "Files: *",
        "Copyright: 2026 The Homelab Control Authors",
        "License: GPL-3+",
        "",
        "Files: " + "\n ".join(MIT_PATHS),
        "Copyright: 2026 The Homelab Control Authors",
        "License: Expat",
        "Comment: Public SDK, schemas, theme definitions and design tokens. The design tokens are",
        " compiled into the web UI embedded in /usr/bin/hc-api.",
        "",
        "Files: compat/webmin/*",
        "Copyright: Jamie Cameron and the Webmin contributors",
        "License: BSD-3-clause",
        "Comment: Unmodified upstream Webmin reference tree. Not used to build, and not included in,",
        " this package or its release source archive. BSD-3-clause is Webmin's top-level licence; many",
        " files state other licences (GPL-2+, GPL without version, Expat, Apache-2.0, Artistic, Perl",
        " terms) and some third-party files state none. Per-file inventory:",
        " docs/compliance/WEBMIN-LICENSE-INVENTORY.md (machine-readable in REUSE.toml).",
    ]
    texts = {"Expat": None}
    for p in npm:
        lic = DEB_LICENSE.get(p["license"])
        if not lic:
            sys.exit(f"{p['name']}: no Debian licence short name for {p['license']}")
        what = "Font files" if "fontsource" in p["name"] else "JavaScript"
        out += ["", f"Files: frontend/app/node_modules/{p['name']}/*",
                f"Copyright: {holder(p['text'])}", f"License: {lic}",
                f"Comment: {p['name']} {p['version']} ({p['source']}). {what} compiled into the",
                " web UI embedded in /usr/bin/hc-api."]
        if lic == "Apache-2.0":
            require_standard_apache2(p["name"], p["text"])
            texts[lic] = APACHE_2_DEB
        elif lic != "Expat" or texts["Expat"] is None:
            texts[lic] = p["text"]
    for m in go_modules():
        lic = "BSD-3-clause" if m["license"] == "BSD-3-Clause" else DEB_LICENSE.get(m["license"])
        if not lic:
            sys.exit(f"{m['path']}: no Debian licence short name for {m['license']}")
        main_text = next(t for f, t in m["files"] if f.upper() in ("LICENSE", "LICENCE", "COPYING"))
        if lic == "Apache-2.0":  # one standard text, shared by every Apache-2.0 component
            require_standard_apache2(m["path"], main_text)
            name = lic
        else:
            name = lic + "~" + m["path"].replace("/", "-")
        others = [f for f, _ in m["files"] if f.upper() not in ("LICENSE", "LICENCE", "COPYING")]
        out += ["", f"Files: vendor-sources/go/download/{m['path']}/*",
                f"Copyright: {m.get('copyright') or holder(main_text)}", f"License: {name}",
                f"Comment: Go module {m['path']} {m['version']}, compiled into /usr/bin/hc-api and /usr/bin/hlctl"
                + " (not hc-broker).",
                " Source (module zip) in the release source archive under vendor-sources/go/download."
                + (f" Further notices shipped with the module ({', '.join(others)}) are reproduced in" if others else ""),
                *([" THIRD-PARTY-NOTICES.md."] if others else [])]
        texts[name] = APACHE_2_DEB if lic == "Apache-2.0" else \
            re.sub(r"^\s*(MIT License\s*)?(Copyright[^\n]*\n+)+", "", main_text)
    crate_texts = {}
    for c in rust_crates():
        field = crate_deb_license(c)
        out += ["", f"Files: vendor-sources/cargo/{c['name']}-{c['version']}/*",
                f"Copyright: {crate_holder(c)}", f"License: {field}",
                f"Comment: Rust crate {c['name']} {c['version']}, compiled into /usr/bin/hc-audit-retention.",
                " Source (.crate) in the release source archive under vendor-sources/cargo; licence and",
                " notice files are reproduced in THIRD-PARTY-NOTICES.md."]
        names = {n for n in re.split(r"[ ()]+", field) if n and n not in ("or", "and")}
        single = next(iter(names)) if len(names) == 1 else None
        for f, t in c["files"]:
            u = f.upper()
            short = ("Apache-2.0" if "APACHE" in u else "Expat" if "MIT" in u else "Zlib" if "ZLIB" in u else
                     "BSL-1.0" if "BOOST" in u else "0BSD" if "0BSD" in u else "Unlicense" if u.startswith("UNLICENSE") else
                     single if u.startswith(("LICENSE", "LICENCE")) else None)
            if short == "Apache-2.0":
                require_standard_apache2(f"crate {c['name']} {f}", t)
                texts["Apache-2.0"] = APACHE_2_DEB
            elif short and short in names and short not in ("Expat",):
                crate_texts.setdefault(short, re.sub(r"^\s*((Copyright|\(c\))[^\n]*\n+)+", "", t))
        for n in names:
            if n not in ("Expat", "Apache-2.0") and n not in crate_texts:
                sys.exit(f"crate {c['name']}: no licence text found for {n}")
            if n == "Apache-2.0":
                texts["Apache-2.0"] = APACHE_2_DEB
    for n, t in sorted(crate_texts.items()):
        texts.setdefault(n, t)
    out += ["", "License: GPL-3+",
            " This program is free software: you can redistribute it and/or modify it under the",
            " terms of the GNU General Public License as published by the Free Software Foundation,",
            " either version 3 of the License, or (at your option) any later version.",
            " .",
            " This program is distributed in the hope that it will be useful, but WITHOUT ANY",
            " WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A",
            " PARTICULAR PURPOSE. See the GNU General Public License for more details.",
            " .",
            " On Debian systems, the complete text of the GNU General Public License version 3",
            ' can be found in "/usr/share/common-licenses/GPL-3".']
    # Expat: the licence body without the per-package copyright line.
    expat = re.sub(r"^.*?(Permission is hereby granted)", r"\1", texts["Expat"], flags=re.S)
    out += ["", "License: Expat", deb_text(expat)]
    for lic, text in texts.items():
        if lic != "Expat":
            out += ["", f"License: {lic}", deb_text(text)]
    webmin = open(os.path.join(ROOT, "compat", "webmin", "LICENCE"), encoding="utf-8").read()
    webmin = re.sub(r"^.*?(Redistribution and use)", r"\1", webmin, flags=re.S)
    out += ["", "License: BSD-3-clause", deb_text(webmin)]
    out += ["", "License: BSD-3-clause-Go", deb_text(re.sub(r"^Copyright[^\n]*\n+", "", go))]
    return "\n".join(out) + "\n"


def render():
    npm = shipped_npm()
    lines = [
        "# Third-party notices",
        "",
        "<!-- Generated by scripts/gen-third-party-notices.py. Do not edit by hand. -->",
        "",
        "Homelab Control release artifacts (`hc-api`, `hlctl`, `hc-broker`, `hc-audit-retention` and the DEB/RPM/Arch packages)",
        "contain the third-party components below. Homelab Control's own code is licensed as described in",
        "`LICENSE.md` and `NOTICE.md`. Development-only tools are not redistributed and are listed in",
        "`docs/compliance/DEPENDENCY-LICENSES.md`.",
        "",
        "Webmin (`compat/webmin/` in the source repository) is **not** part of any release artifact.",
        "",
        "| Component | Version | License | Where it ships | Source |",
        "|---|---|---|---|---|",
        "| Go standard library | toolchain used for the build (see `go version -m <binary>`) | BSD-3-Clause | compiled into all three binaries | https://go.dev/ |",
    ]
    gomods = go_modules()
    for m in gomods:
        lines.append(f"| {m['path']} | {m['version']} | {m['license']} | compiled into `hc-api` and `hlctl` (not `hc-broker`) | https://pkg.go.dev/{m['path']} |")
    for p in npm:
        where = "font files embedded in `hc-api` (web UI)" if "fontsource" in p["name"] else "web UI JavaScript bundle embedded in `hc-api`"
        lines.append(f"| {p['name']} | {p['version']} | {p['license']} | {where} | {p['source']} |")
    crates = rust_crates()
    for c in crates:
        lines.append(f"| {c['name']} (Rust crate) | {c['version']} | {c.get('elected') or c['license']} | compiled into `hc-audit-retention` | https://crates.io/crates/{c['name']} |")
    lines += ["", "## Go standard library", "", "```text", go_stdlib(), "```"]
    for m in gomods:
        for f, text in m["files"]:
            lines += ["", f"## {m['path']} {m['version']}: {f}", "", "````text", text, "````"]
    # Rust crates: identical licence files are printed once, listing every crate they cover.
    cgroups = {}
    for c in crates:
        for f, text in c["files"]:
            cgroups.setdefault(text, []).append(f"{c['name']} {c['version']} ({f})")
    for text, names in cgroups.items():
        lines += ["", "## Rust: " + ", ".join(names), "", "````text", text, "````"]
    # Identical license texts are printed once, listing every component they cover.
    groups = {}
    for p in npm:
        groups.setdefault(p["text"], []).append(f"{p['name']} {p['version']}")
    for text, names in groups.items():
        lines += ["", "## " + ", ".join(names), "", "```text", text, "```"]
    return "\n".join(lines) + "\n"


def main():
    outputs = {OUT: render(), OUT_DEB: render_debian()}
    if "--check" in sys.argv:
        for path, content in outputs.items():
            current = open(path, encoding="utf-8").read() if os.path.exists(path) else ""
            if current != content:
                sys.exit(f"{os.path.relpath(path, ROOT)} is stale: run scripts/gen-third-party-notices.py")
        print("OK: THIRD-PARTY-NOTICES.md and packaging/debian/copyright match the shipped dependencies")
        return
    for path, content in outputs.items():
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"wrote {os.path.relpath(path, ROOT)} ({len(content)} bytes)")


if __name__ == "__main__":
    main()
