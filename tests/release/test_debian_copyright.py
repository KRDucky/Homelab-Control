# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Regression tests for the generated Debian copyright file (LA-12, release gate G10).

No toolchain or network: reads the committed packaging/debian/copyright (kept in sync by
`scripts/gen-third-party-notices.py --check` in `make check`) and the generator's pure helpers.
Run: python3 -m unittest discover -s tests/release -v
"""
import contextlib
import importlib.util
import io
import os
import re
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
spec = importlib.util.spec_from_file_location("gen_notices", os.path.join(ROOT, "scripts", "gen-third-party-notices.py"))
gen = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gen)


def read(*parts):
    with open(os.path.join(ROOT, *parts), encoding="utf-8") as f:
        return f.read()


class DebianCopyright(unittest.TestCase):
    def test_no_full_apache2_licence(self):
        # Same three conditions as lintian's copyright-file-contains-full-apache-2-license
        # (lib/Lintian/Check/Debian/Copyright.pm): length > 10000, the licence title and its terms.
        c = read("packaging", "debian", "copyright")
        full = (len(c) > 10_000 and re.search(r"\bApache License\s+Version 2\.0,", c)
                and "TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION" in c)
        self.assertFalse(full, "Debian copyright embeds the full Apache-2.0 text")
        self.assertIn('"/usr/share/common-licenses/Apache-2.0"', c)
        self.assertEqual(c.count("\nLicense: Apache-2.0\n Licensed under"), 1)  # one standalone paragraph

    def test_full_apache2_text_and_notice_kept_in_third_party_notices(self):
        n = read("docs", "compliance", "THIRD-PARTY-NOTICES.md")
        for mod in ("github.com/coreos/go-oidc/v3", "github.com/go-jose/go-jose/v4"):
            self.assertRegex(n, re.escape(f"## {mod} ") + r"\S+: LICENSE\n\n````text\n\s*Apache License\n")
        self.assertRegex(n, r"## github\.com/coreos/go-oidc/v3 \S+: NOTICE\n\n````text\nCoreOS Project\nCopyright 2014 CoreOS, Inc")
        self.assertGreaterEqual(n.count("END OF TERMS AND CONDITIONS"), 2)

    def test_modified_apache2_terms_fail_closed(self):
        n = read("docs", "compliance", "THIRD-PARTY-NOTICES.md")
        start = n.index("## github.com/coreos/go-oidc/v3")
        body = n.index("````text\n", start) + len("````text\n")
        text = n[body:n.index("\n````", body)]
        gen.require_standard_apache2("canonical", text)  # unmodified terms are accepted
        for bad in (text.replace("irrevocable", "revocable", 1),
                    text.replace("END OF TERMS AND CONDITIONS", "")):
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                gen.require_standard_apache2("modified", bad)


if __name__ == "__main__":
    unittest.main()
