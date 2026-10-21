# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Static policy checks for the release workflows (ADR 0009 Phase 1, docs/release/RELEASING.md).

The release workflow must be manual-only, least-privilege, draft-only and unable to publish, delete
or tag; the reusable build workflow must be read-only. Needs PyYAML (installed with spdx-tools in
CI); outside CI the tests are skipped when it is missing.
"""
import os
import re
import unittest

try:
    import yaml
except ImportError:  # pragma: no cover
    yaml = None

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
WF = os.path.join(ROOT, ".github", "workflows")
SHA_USE = re.compile(r"^([\w.-]+/[\w.-]+)@([0-9a-f]{40})$")


def load(name):
    with open(os.path.join(WF, name), encoding="utf-8") as f:
        text = f.read()
    return text, yaml.safe_load(text)


def triggers(doc):
    return doc.get("on", doc.get(True))  # YAML 1.1 reads the key `on` as boolean True


def steps(doc):
    for jname, job in doc["jobs"].items():
        for st in job.get("steps", []):
            yield jname, st


def ci_pins():
    _, ci = load("ci.yml")
    pins = {}
    for _, st in steps(ci):
        m = SHA_USE.match(st.get("uses", ""))
        if m:
            pins.setdefault(m.group(1), set()).add(m.group(2))
    return pins


@unittest.skipIf(yaml is None and not os.environ.get("GITHUB_ACTIONS"), "PyYAML not installed")
class ReleaseWorkflowPolicy(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if yaml is None:
            raise AssertionError("PyYAML is required in CI")
        cls.text, cls.rel = load("release.yml")
        cls.btext, cls.build = load("release-build.yml")
        cls.ctext, cls.ci = load("ci.yml")

    def test_manual_trigger_only(self):
        on = triggers(self.rel)
        self.assertEqual(set(on), {"workflow_dispatch"}, "release.yml must not run on push, tags, PRs or schedules")
        inputs = on["workflow_dispatch"]["inputs"]
        self.assertEqual(set(inputs), {"version"})
        self.assertTrue(inputs["version"]["required"])
        self.assertEqual(set(triggers(self.build)), {"workflow_call"}, "release-build.yml is reusable only")

    def test_least_privilege(self):
        self.assertEqual(self.rel["permissions"], {"contents": "read"})
        self.assertEqual(self.build["permissions"], {"contents": "read"})
        self.assertEqual(self.ci["permissions"], {"contents": "read"})
        writers = {j for j, job in self.rel["jobs"].items()
                   if any(v == "write" for v in (job.get("permissions") or {}).values())}
        self.assertEqual(writers, {"draft"}, "only the draft job may have write permission")
        self.assertEqual(self.rel["jobs"]["draft"]["permissions"], {"contents": "write"})
        for doc in (self.rel, self.build, self.ci):
            for j, job in doc["jobs"].items():
                perms = job.get("permissions") or {}
                for k in ("id-token", "attestations", "packages", "actions", "pull-requests", "issues"):
                    if k == "actions" and j == "preflight" and perms.get(k) == "read":
                        continue
                    self.assertNotIn(k, perms, f"{j}: unexpected permission {k}")
        for doc in (self.build, self.ci):
            for j, job in doc["jobs"].items():
                self.assertNotIn("write", (job.get("permissions") or {}).values(), j)

    def test_no_secrets_or_signing(self):
        for text in (self.text, self.btext):
            self.assertNotRegex(text, r"secrets\.(?!GITHUB_TOKEN)", "no secrets other than GITHUB_TOKEN")
            for word in ("gpg", "cosign", "attest-build-provenance", "id-token: write", "rpmsign", "signing_key"):
                self.assertNotIn(word, text.lower())

    def test_sequence_and_serialisation(self):
        jobs = self.rel["jobs"]
        self.assertEqual(self.rel["concurrency"]["cancel-in-progress"], False)
        self.assertEqual(jobs["preflight"]["if"], "github.ref == 'refs/heads/main'")
        self.assertEqual(set(jobs["gate"]["needs"] if isinstance(jobs["gate"]["needs"], list) else [jobs["gate"]["needs"]]), {"preflight"})
        self.assertEqual(set(jobs["build"]["needs"]), {"preflight", "gate"})
        self.assertEqual(jobs["build"]["uses"], "./.github/workflows/release-build.yml")
        self.assertEqual(jobs["build"]["with"]["mode"], "release")
        self.assertEqual(set(jobs["draft"]["needs"]), {"preflight", "build"})
        self.assertEqual(jobs["draft"]["environment"], "release")
        dry = self.ci["jobs"]["release-dry-run"]
        self.assertEqual(dry["uses"], "./.github/workflows/release-build.yml")
        self.assertEqual(dry["with"]["mode"], "dry-run")

    def test_draft_only_never_publish_delete_or_tag(self):
        runs = "\n".join(st.get("run", "") for _, st in steps(self.rel))
        creates = [l for l in runs.splitlines() if "gh release create" in l]
        self.assertEqual(len(creates), 1)
        create_cmd = runs[runs.index("gh release create"):].split("\n          gh ")[0]
        self.assertIn("--draft", create_cmd)
        self.assertIn("--prerelease", create_cmd)
        forbidden = [r"draft\s*=\s*false", r"\"draft\"\s*:\s*false", r"--draft=false", r"gh release (edit|delete|upload)",
                     r"-X\s+DELETE", r"--method\s+DELETE", r"git\s+(push|tag)", r"/git/refs", r"--clobber",
                     r"make_latest", r"-F\s+draft", r"-f\s+draft"]
        for pat in forbidden:
            self.assertNotRegex(runs, pat)
        for text in (self.btext,):
            self.assertNotIn("gh release", text)
            self.assertNotRegex(text, r"git\s+(push|tag)")
        # The only PATCH changes the title (name) and nothing else.
        for line in runs.splitlines():
            if "-X PATCH" in line:
                self.assertRegex(line, r'-X PATCH "repos/\$REPO/releases/\$ID" -f name="[^"]*" >/dev/null$')

    def test_no_untrusted_expressions_in_scripts(self):
        # Inputs and event data reach scripts only through env (no template injection).
        for doc in (self.rel, self.build):
            for j, st in steps(doc):
                run = st.get("run", "")
                self.assertNotRegex(run, r"\$\{\{\s*(inputs|github\.event|needs|steps)\.", f"{j}: expression in run")

    def test_actions_pinned_like_ci(self):
        pins = ci_pins()
        for doc in (self.rel, self.build):
            for j, st in steps(doc):
                u = st.get("uses")
                if not u:
                    continue
                m = SHA_USE.match(u)
                self.assertIsNotNone(m, f"{j}: {u} must be pinned by full commit SHA")
                self.assertIn(m.group(1), pins, f"{j}: {m.group(1)} is not used in ci.yml")
                self.assertIn(m.group(2), pins[m.group(1)], f"{j}: {u} differs from the ci.yml pin")
                if m.group(1) == "actions/checkout":
                    self.assertIs(st["with"]["persist-credentials"], False, j)

    def test_ci_and_release_use_the_same_go(self):
        with open(os.path.join(ROOT, "packaging", "release", "toolchain.env"), encoding="utf-8") as f:
            pin = re.search(r"^GO_VERSION=(\S+)$", f.read(), re.M).group(1)
        versions = [st["with"]["go-version"] for _, st in steps(self.ci) if st.get("uses", "").startswith("actions/setup-go")]
        self.assertTrue(versions)
        for v in versions:
            self.assertEqual(str(v), pin, "ci.yml go-version must equal GO_VERSION in toolchain.env (no floating .x)")

    def test_build_uses_exact_toolchain(self):
        for j, st in steps(self.build):
            u = st.get("uses", "")
            if u.startswith("actions/setup-go"):
                self.assertEqual(st["with"]["go-version"], "${{ steps.pins.outputs.go }}")
            if u.startswith("actions/setup-node"):
                self.assertEqual(st["with"]["node-version"], "${{ steps.pins.outputs.node }}")
                self.assertIs(st["with"]["package-manager-cache"], False)
        self.assertIn("check-toolchain packaging/release/toolchain.env", self.btext)
        self.assertIn("--require-tools", self.btext)
        self.assertIn("negative-tests.sh", self.btext)


if __name__ == "__main__":
    unittest.main()
