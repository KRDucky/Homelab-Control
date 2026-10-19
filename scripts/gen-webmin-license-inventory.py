#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Licence inventory of the upstream Webmin reference tree (compat/webmin/).

The tree is an unmodified copy of Webmin 2.670 and is never shipped. Its files are NOT
relicensed: this inventory only records what upstream states, so that REUSE metadata does
not present every file as having one licence.

    scripts/gen-webmin-license-inventory.py          # regenerate outputs
    scripts/gen-webmin-license-inventory.py --check  # fail on stale outputs or unreviewed files

Outputs:
  * the generated compat/webmin section of REUSE.toml (between the BEGIN/END markers);
  * docs/compliance/WEBMIN-LICENSE-INVENTORY.md (human-readable table with evidence).

--check also re-scans every tracked file under compat/webmin/ for licence or third-party
copyright statements and fails if such a file is neither classified in ENTRIES nor listed
as a reviewed false positive. That keeps the inventory honest across upstream syncs.

Method and limits: classification uses statements *in the files* (or a directory's own
licence/AUTHORS/README file, or the header of the component a file belongs to). Files with
no statement default to the upstream top-level LICENCE (BSD-3-Clause). Binary assets and
files without any statement were not individually reviewed; third-party ones are marked
LicenseRef-Webmin-unverified rather than guessed. Stdlib only.
"""
import os
import re
import subprocess
import sys

ROOT = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True,
                      check=True).stdout.strip()
SUB = "compat/webmin"
REUSE = os.path.join(ROOT, "REUSE.toml")
DOC = os.path.join(ROOT, "docs", "compliance", "WEBMIN-LICENSE-INVENTORY.md")
BEGIN = "# BEGIN generated: compat/webmin licence inventory (scripts/gen-webmin-license-inventory.py)"
END = "# END generated: compat/webmin licence inventory"
PERL = "Artistic-1.0-Perl OR GPL-1.0-or-later"  # "same terms as Perl itself" (SPDX convention)
GPLU = "LicenseRef-Webmin-GPL-unversioned"
UNV = "LicenseRef-Webmin-unverified"

# Order matters: for a file matched by several entries, the LAST one applies (REUSE.toml rule).
# basis: default | file (statement in the file) | directory (the directory's own licence/AUTHORS/
# README) | component (header of the library/package the file belongs to) | none (third-party,
# no licence statement found: unverified).
ENTRIES = [
    dict(paths=["**"], spdx="BSD-3-Clause", copyright="Jamie Cameron and the Webmin contributors",
         basis="default", evidence="LICENCE: Webmin's top-level BSD 3-Clause licence. Applies to files "
         "with no statement of their own; not individually reviewed."),
    # --- explicit statements in Webmin-module files ---
    dict(paths=["dhcpd/edit_keys.cgi", "dhcpd/edit_text.cgi", "dhcpd/edit_zones.cgi",
                "dhcpd/save_keys.cgi", "dhcpd/save_text.cgi", "dhcpd/save_zones.cgi"],
         spdx="GPL-2.0-or-later", copyright="2005 Johannes Franken <jfranken@jfranken.de>", basis="file",
         evidence='line 4: "Distributed under the terms of the GNU General Public License, v2 or later"'),
    dict(paths=["webmin-openrc-init"], spdx="GPL-2.0-or-later", copyright="2024 webmin & mckaygerhard",
         basis="file", evidence='line 4: "Distributed under the terms of the GNU General Public License, v2 or later"'),
    dict(paths=["samba/smbhash.pl"], spdx="GPL-2.0-or-later",
         copyright=["2001 Benjamin Kuit <bj@it.uts.edu.au>", "Andrew Tridgell 1997-1998 (Samba)"], basis="file",
         evidence='POD: "either version 2 of the License, or (at your option) any later version"'),
    dict(paths=["openslp/**"], spdx=GPLU, copyright="2000 Caldera Systems", basis="directory",
         evidence='openslp/README and each .cgi/.pl header: "under the terms of the GNU General Public '
         'License" (no version stated)'),
    dict(paths=["webmin/acme_tiny.py"], spdx="MIT", copyright="Daniel Roesler", basis="file",
         evidence='line 2: "Copyright Daniel Roesler, under MIT license"'),
    # --- bundled third-party front-end libraries ---
    dict(paths=["filemin/images/icons/**"], spdx=GPLU, copyright="Matthieu James (Faenza icon theme)",
         basis="directory", evidence='filemin/images/icons/AUTHORS: "Faenza icons are all licensed under the '
         'GPL"; COPYING there is the GPL version 3 text; no "or later" statement found'),
    dict(paths=["filemin/unauthenticated/dropdown/fg.menu.js", "filemin/unauthenticated/dropdown/fg.menu.css"],
         spdx=f"MIT OR {GPLU}", copyright="2008-2009 Filament Group", basis="file",
         evidence='fg.menu.js header: "Dual licensed under the MIT ... and GPL ... licenses" (GPL version not '
         'stated); fg.menu.css is the same component'),
    dict(paths=["bacula-backup/jstree/jquery-3.6.0.min.js", "filemin/unauthenticated/jquery/jquery.min.js"],
         spdx="MIT", copyright="OpenJS Foundation and other contributors", basis="file",
         evidence='header: "jQuery v3.6.0 | (c) OpenJS Foundation and other contributors | jquery.org/license" '
         '(jQuery\'s licence page: MIT)'),
    dict(paths=["filemin/unauthenticated/jquery/jquery-ui.min.js", "filemin/unauthenticated/jquery/jquery-ui.min.css",
                "filemin/unauthenticated/jquery/images/**"],
         spdx="MIT", copyright="jQuery Foundation and other contributors", basis="component",
         evidence='jquery-ui.min.js header: "Copyright jQuery Foundation and other contributors; Licensed MIT"; '
         'the CSS and images are the same jQuery UI 1.13.0 build'),
    dict(paths=["bacula-backup/jstree/jstree.min.js", "bacula-backup/jstree/themes/**"], spdx="MIT",
         copyright="jsTree contributors (holder not named in the file)", basis="component",
         evidence='jstree.min.js header: "jsTree - v3.3.11 - 2020-12-18 - (MIT)"; themes are part of jsTree'),
    dict(paths=["filemin/unauthenticated/js/bootstrap-hover-dropdown.min.js"], spdx="MIT",
         copyright="Cameron Spear", basis="file", evidence='header: "License: MIT"'),
    dict(paths=["fdisk/range.js", "fdisk/slider.js", "fdisk/timer.js"], spdx="Apache-2.0",
         copyright="2002-2006 Erik Arvidsson (WebFX)", basis="file",
         evidence='header: "Licensed under the Apache License, Version 2.0"'),
    dict(paths=["unauthenticated/js/highlight/highlight.min.js", "unauthenticated/js/highlight/languages/**"],
         spdx="BSD-3-Clause", copyright="2006-2022 Highlight.js contributors", basis="component",
         evidence='highlight.min.js header: "Highlight.js v11.7.0 ... License: BSD-3-Clause"; each language '
         'file says "grammar compiled for Highlight.js 11.7.0"'),
    dict(paths=["unauthenticated/css/highlight/highlight.min.css", "unauthenticated/css/highlight/highlight-dark.min.css"],
         spdx="MIT", copyright=["Defman21 (GitHub theme)", "Wimer Hazenberg (Monokai theme)"], basis="file",
         evidence='headers: "License: ~ MIT (or more permissive) [via base16-schemes-source]"'),
    dict(paths=["unauthenticated/js/quill.min.js", "unauthenticated/css/quill.min.css"], spdx="BSD-3-Clause",
         copyright=["2017 Slab", "2014 Jason Chen", "2013 salesforce.com"], basis="file",
         evidence='header: "Licensed under BSD 3-Clause"'),
    dict(paths=["xterm/xterm.js", "xterm/xterm.css", "xterm/xterm-addon-attach.js", "xterm/xterm-addon-fit.js",
                "xterm/xterm-addon-webgl.js"],
         spdx="MIT", copyright=["2017-2026 The xterm.js authors", "2014-2016 SourceLair Private Company",
                                "2012-2013 Christopher Jeffrey"], basis="component",
         evidence='xterm.js/xterm.css header: "Licensed under MIT"; the addon files carry only version comments '
         'and are the @xterm addons named in xterm.js'),
    dict(paths=["gray-theme/unauthenticated/reset-fonts-base.css", "gray-theme/unauthenticated/reset-fonts-grids-base.css"],
         spdx="BSD-3-Clause", copyright="2008 Yahoo! Inc.", basis="file",
         evidence='header: "Code licensed under the BSD License: http://developer.yahoo.net/yui/license.txt" '
         '(YUI\'s BSD licence is the 3-clause form)'),
    # --- bundled Perl modules (CPAN) ---
    dict(paths=["vendor_perl/**"], spdx=UNV, copyright="respective CPAN module authors", basis="none",
         evidence="Third-party Perl modules; files below with their own statement are classified individually. "
         "Remaining files (Digest::HMAC*, File::Basename, File::Find, File::Grep, Math::ReedSolomon, "
         "QRCode::Encoder*) state no licence in the file."),
    dict(paths=["vendor_perl/Convert/TNEF.pm"], spdx=PERL, copyright="1999 Douglas Wilson <dougw@cpan.org>",
         basis="file", evidence='"under the same terms as Perl itself"'),
    dict(paths=["vendor_perl/File/BaseDir.pm"], spdx=PERL, copyright="2003-2021 Jaap Karssenberg", basis="file",
         evidence='"under the same terms as the Perl 5 programming language system itself"'),
    dict(paths=["vendor_perl/File/Copy.pm"], spdx=PERL, copyright="1996 Charles Bailey (additions)", basis="file",
         evidence='"source code has been placed in the public domain by the author ... revised code under the same '
         'terms as Perl itself"'),
    dict(paths=["vendor_perl/File/MimeInfo.pm"], spdx=PERL, copyright="2003, 2012 Jaap G Karssenberg",
         basis="file", evidence='"under the same terms as Perl itself"'),
    dict(paths=["vendor_perl/File/Path.pm"], spdx=PERL,
         copyright="Charles Bailey, Tim Bunce, David Landgren, James Keenan and Richard Elberger", basis="file",
         evidence='"under the same terms as Perl itself"'),
    dict(paths=["vendor_perl/File/Spec.pm", "vendor_perl/File/Spec/*.pm"], spdx=PERL,
         copyright="2004-2014 the Perl 5 Porters", basis="file",
         evidence='each module: "under the same terms as Perl itself" (File/Spec/AmigaOS.pm states nothing and is '
         'classified by its distribution, PathTools)'),
    dict(paths=["vendor_perl/JSON/PP.pm", "vendor_perl/JSON/PP/Boolean.pm"], spdx=PERL,
         copyright="2007-2016 Makamaka Hannyaharamitu", basis="file", evidence='"under the same terms as Perl itself"'),
    dict(paths=["vendor_perl/MIME/Body.pm", "vendor_perl/MIME/Tools.pm"], spdx=PERL,
         copyright=["1998-1999 ZeeGee Software Inc", "2004 Roaring Penguin Software Inc", "2022 Dianne Skoll"],
         basis="file", evidence='"under the same terms as Perl itself"'),
    dict(paths=["vendor_perl/Net/WebSocket/Server.pm", "vendor_perl/Net/WebSocket/Server/Connection.pm"],
         spdx="Artistic-2.0", copyright="2013 Eric Wastl", basis="file",
         evidence='"under the terms of the the Artistic License (2.0)"'),
    dict(paths=["vendor_perl/Protocol/WebSocket.pm", "vendor_perl/Protocol/WebSocket/**"], spdx=PERL,
         copyright="2010-2018 Viacheslav Tykhanovskyi", basis="component",
         evidence='Protocol/WebSocket.pm: "under the same terms as Perl 5.10"; submodules state nothing and '
         'belong to the same distribution'),
    dict(paths=["vendor_perl/Text/ASCIITable.pm", "vendor_perl/Text/ASCIITable/Wrap.pm"], spdx=PERL,
         copyright="2002-2011 Håkon Nessjøen", basis="file", evidence='"under the same terms as Perl itself"'),
    # --- third-party material with no licence statement: not guessed ---
    dict(paths=["vendor_perl/QRCode/Encoder/Matrix.pm", "vendor_perl/QRCode/Encoder/QRSpec.pm"], spdx=UNV,
         copyright="QRCode::Encoder authors", basis="none",
         evidence='comment: "Parts liberally taken from libqrencode/qrspec.c, which is distributed with LGPL '
         'license"; the file\'s own licence is not stated'),
    dict(paths=["gray-theme/**/images/**", "gray-theme/images/**"], spdx=UNV, copyright="David Vignoni (icons)",
         basis="none", evidence='gray-theme/theme.pl: "Icons copyright David Vignoni"; no licence stated in the tree'),
    dict(paths=["gray-theme/pngfix.js", "gray-theme/unauthenticated/pngfix.js"], spdx=UNV,
         copyright="Bob Osola (bobosola)", basis="none", evidence="header names the author's site; no licence"),
    dict(paths=["filemin/unauthenticated/js/chmod-calculator.js"], spdx=UNV,
         copyright="Jeroen Vermeulen (Alphamega Hosting)", basis="none",
         evidence='header: "This notice must stay intact"; no licence'),
    dict(paths=["dhcpd/mac-vendor.txt"], spdx=UNV, copyright="IEEE Registration Authority (OUI data), presumed",
         basis="none", evidence="MAC address vendor (OUI) list; source and licence not stated (the scanner hit is a "
         "vendor named 'Artistic Licence Engineering Ltd')"),
    dict(paths=["fdisk/winclassic.css"], spdx=UNV, copyright="Erik Arvidsson (WebFX), presumed", basis="none",
         evidence="styles for the WebFX slider; no header, so not assumed to share range.js's Apache-2.0"),
]

# Files the scanner flags that were reviewed and contain no licence grant of their own.
FALSE_POSITIVES = {
    "CHANGELOG.md": "mentions the virtualmin-gpl repository",
    "README.md": "describes Webmin's own BSD licence",
    "bin/patch": "mentions virtualmin-gpl; copyright Ilia Ross (Webmin/Virtualmin developer), no other licence",
    "bin/language-manager": "copyright Ilia Ross (Webmin/Virtualmin developer), no other licence",
    "miniserv-lib.pl": "URL to virtualmin-gpl",
    "webmin/system_info.pl": "string match on 'gpl' module versions",
    "gray-theme/theme.pl": "the gray-theme copyright comment itself (icons classified separately)",
    "LICENCE": "Webmin's top-level licence text (the default entry's evidence)",
    "LICENCE.ja": "Japanese translation of LICENCE",
}

LIC_RE = re.compile(rb"SPDX-License-Identifier|General Public Licen|\bGNU GPL\b|\bGPL\b|GPLv[123]|\bLGPL\b|"
                    rb"Permission is hereby granted|\bMIT[ -]?Licen|Licensed MIT|\(MIT\)|Apache License|"
                    rb"Redistribution and use in source and binary|\bBSD[ -]Licen|Mozilla Public|Artistic Licen|"
                    rb"same terms as Perl|public domain|Creative Commons|Open Font Licen", re.I)
COPY_RE = re.compile(rb"copyright\s*(\(c\)|\xc2\xa9|[0-9]{4}|by\s)", re.I)
OWN_RE = re.compile(rb"jamie|webmin|virtualmin", re.I)
SKIP_SCAN = re.compile(r"(^|/)(lang|help|ulang)/|(^|/)config\.info(\.|$)|(^|/)module\.info(\.|$)|^LICENCE")


def glob_re(pattern):
    out, i = "", 0
    while i < len(pattern):
        if pattern.startswith("**", i):
            out += ".*"; i += 2
        elif pattern[i] == "*":
            out += "[^/]*"; i += 1
        else:
            out += re.escape(pattern[i]); i += 1
    return re.compile(out + r"\Z")


def tracked():
    res = subprocess.run(["git", "ls-files", "-z", "--", SUB], cwd=ROOT, capture_output=True, check=True)
    return [p[len(SUB) + 1:] for p in res.stdout.decode().split("\0") if p]


def classify(files):
    compiled = [[glob_re(p) for p in e["paths"]] for e in ENTRIES]
    result = {}
    for f in files:
        for idx, pats in enumerate(compiled):
            if any(r.match(f) for r in pats):
                result[f] = idx
    return result


def toml_str(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def render_reuse():
    lines = [BEGIN,
             "# Upstream Webmin (compat/webmin/, never shipped). NOT relicensed: these annotations record what",
             "# upstream states. Basis and evidence per entry: docs/compliance/WEBMIN-LICENSE-INVENTORY.md.",
             "# Order matters: the last matching annotation applies."]
    for e in ENTRIES:
        cr = e["copyright"] if isinstance(e["copyright"], list) else [e["copyright"]]
        lines += ["", f"# basis: {e['basis']}", "[[annotations]]",
                  "path = [" + ", ".join(toml_str(f"{SUB}/{p}") for p in e["paths"]) + "]",
                  'precedence = "override"',
                  "SPDX-FileCopyrightText = [" + ", ".join(toml_str(c) for c in cr) + "]",
                  f"SPDX-License-Identifier = {toml_str(e['spdx'])}"]
    lines.append(END)
    return "\n".join(lines)


def render_doc(files, cls):
    counts = [0] * len(ENTRIES)
    for idx in cls.values():
        counts[idx] += 1
    by_basis = {}
    for idx, n in enumerate(counts):
        b = ENTRIES[idx]["basis"]
        by_basis[b] = by_basis.get(b, 0) + n
    by_lic = {}
    for idx, n in enumerate(counts):
        by_lic[ENTRIES[idx]["spdx"]] = by_lic.get(ENTRIES[idx]["spdx"], 0) + n
    out = [
        "# Webmin reference tree: licence inventory",
        "",
        "<!-- Generated by scripts/gen-webmin-license-inventory.py. Do not edit by hand. -->",
        "",
        "`compat/webmin/` is an unmodified copy of upstream Webmin 2.670 (verified byte-for-byte by",
        "`scripts/verify-upstream.sh`). It is **not shipped** in any Homelab Control package or release source",
        "archive, and it is **not relicensed**: Homelab Control's GPL-3.0-or-later and MIT grants do not apply to",
        "it. This inventory records what upstream states, file by file where a statement exists, so that the",
        "REUSE metadata does not present the whole tree as having one licence. Engineering record, not legal",
        "advice.",
        "",
        "## Coverage",
        f"- Tracked files in the tree: **{len(files)}** (REUSE ignores symlinks, empty files and `LICENCE*`).",
        "- Every file was scanned for licence statements and for third-party copyright lines (translation,",
        "  help and module-info files excluded from the copyright-line scan). Every hit was reviewed by hand and",
        "  is either classified below or recorded as a false positive. `--check` fails on any new unreviewed hit.",
        "- Files with **no statement** take the upstream top-level `LICENCE` (BSD-3-Clause, basis `default`).",
        "  This is upstream's own declaration for Webmin as a whole, not a per-file verification. It is the main",
        "  limitation: unlabelled third-party assets (for example images or embedded fonts copied from elsewhere)",
        "  could be mis-attributed by it. Binary files were not inspected.",
        "- Third-party material **without** a licence statement is marked `LicenseRef-Webmin-unverified`, never",
        "  guessed. GPL statements without a version are `LicenseRef-Webmin-GPL-unversioned`.",
        "",
        "| Basis | Files |", "|---|---|",
    ]
    for b in ["file", "directory", "component", "none", "default"]:
        out.append(f"| {b} | {by_basis.get(b, 0)} |")
    out += ["", "| Licence (SPDX) | Files |", "|---|---|"]
    for lic, n in sorted(by_lic.items(), key=lambda kv: -kv[1]):
        out.append(f"| `{lic}` | {n} |")
    out += ["", "## Entries (later entries override earlier ones)", "",
            "| # | Paths (under `compat/webmin/`) | Files | Licence | Copyright | Basis | Evidence |",
            "|---|---|---|---|---|---|---|"]
    for idx, e in enumerate(ENTRIES):
        cr = "; ".join(e["copyright"] if isinstance(e["copyright"], list) else [e["copyright"]])
        paths = "<br>".join(f"`{p}`" for p in e["paths"])
        out.append(f"| {idx} | {paths} | {counts[idx]} | `{e['spdx']}` | {cr} | {e['basis']} | "
                   f"{e['evidence'].replace('|', '/')} |")
    out += ["", "## Reviewed false positives", "", "| File | Why it is not a licence statement |", "|---|---|"]
    for f, why in sorted(FALSE_POSITIVES.items()):
        out.append(f"| `{f}` | {why} |")
    out += ["", "## Licence identifiers used",
            "- `Artistic-1.0-Perl OR GPL-1.0-or-later`: SPDX's expression for \"the same terms as Perl itself\".",
            "- `LicenseRef-Webmin-GPL-unversioned`: the file or its directory says \"GPL\" without a version or an",
            "  \"or later\" statement (`LICENSES/LicenseRef-Webmin-GPL-unversioned.txt`).",
            "- `LicenseRef-Webmin-unverified`: third-party material whose licence is not stated in the tree",
            "  (`LICENSES/LicenseRef-Webmin-unverified.txt`). Must be resolved before any such file is shipped.",
            ""]
    return "\n".join(out)


def scan(files, cls):
    """Files with licence/third-party-copyright text that are only covered by the default entry."""
    bad = []
    for f in files:
        p = os.path.join(ROOT, SUB, f)
        if os.path.islink(p) or not os.path.isfile(p) or f in FALSE_POSITIVES:
            continue
        data = open(p, "rb").read()
        if b"\0" in data[:4096]:
            continue
        hit = LIC_RE.search(data)
        if not hit and not SKIP_SCAN.search(f):
            for m in COPY_RE.finditer(data):
                line = data[max(0, data.rfind(b"\n", 0, m.start())):data.find(b"\n", m.end())]
                if not OWN_RE.search(line):
                    hit = m
                    break
        if hit and ENTRIES[cls.get(f, 0)]["basis"] == "default":
            bad.append(f"{f}: {hit.group(0)[:60]!r}")
    return bad


def main():
    files = tracked()
    cls = classify(files)
    unused = [e["paths"] for idx, e in enumerate(ENTRIES) if idx and idx not in set(cls.values())]
    if unused:
        sys.exit(f"inventory entries match no file (stale after an upstream sync?): {unused}")
    reuse_text = open(REUSE, encoding="utf-8").read()
    if BEGIN not in reuse_text or END not in reuse_text:
        sys.exit("REUSE.toml lacks the generated-section markers")
    head, rest = reuse_text.split(BEGIN, 1)
    tail = rest.split(END, 1)[1]
    new_reuse = head + render_reuse() + tail
    new_doc = render_doc(files, cls)
    if "--check" in sys.argv:
        problems = []
        if new_reuse != reuse_text:
            problems.append("REUSE.toml compat/webmin section is stale")
        if not os.path.exists(DOC) or open(DOC, encoding="utf-8").read() != new_doc:
            problems.append("docs/compliance/WEBMIN-LICENSE-INVENTORY.md is stale")
        bad = scan(files, cls)
        if bad:
            problems.append("unreviewed licence/copyright statements in compat/webmin (classify them in ENTRIES "
                            "or FALSE_POSITIVES):\n  " + "\n  ".join(bad))
        if problems:
            sys.exit("\n".join(problems) + "\nrun scripts/gen-webmin-license-inventory.py after reviewing")
        print(f"OK: Webmin licence inventory current ({len(files)} files, {len(ENTRIES)} entries, no unreviewed statements)")
        return
    with open(REUSE, "w", encoding="utf-8") as f:
        f.write(new_reuse)
    with open(DOC, "w", encoding="utf-8") as f:
        f.write(new_doc)
    bad = scan(files, cls)
    print(f"wrote REUSE.toml section and {os.path.relpath(DOC, ROOT)}; unreviewed hits: {len(bad)}")
    for b in bad:
        print("  " + b)


if __name__ == "__main__":
    main()
