# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Pre-push testing (ci/README.md): Graphify impact logic, the n8n workflow's code, the bundle
checkout and the Jenkinsfile wiring. Standard library only; git, and node for the n8n code."""

import functools
import http.server
import importlib.util
import json
import os
import re
import shutil
import subprocess
import tempfile
import threading
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(ROOT, path))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


review = load("ci/graphify_review.py", "graphify_review")
gen = load("ci/prepush/gen-n8n-workflow.py", "gen_n8n_workflow")


class Impact(unittest.TestCase):
    NODES = [
        {"id": "a_run", "label": "Run", "source_file": "backend/a/a.go"},
        {"id": "a_test", "label": "TestRun", "source_file": "backend/a/a_test.go"},
        {"id": "b_use", "label": "Use", "source_file": "backend/b/b.go"},
        {"id": "c_lib", "label": "Lib", "source_file": "backend/c/c.go"},
        {"id": "ui", "label": "Page", "source_file": "frontend/app/src/Page.tsx"},
    ]
    LINKS = [
        {"source": "a_test", "target": "a_run", "relation": "calls"},
        {"source": "b_use", "target": "a_run", "relation": "calls"},
        {"source": "a_run", "target": "c_lib", "relation": "imports_from"},
    ]

    def test_dependents_dependencies_and_tests(self):
        r = review.impact(self.NODES, self.LINKS, [("M", "backend/a/a.go")])
        self.assertEqual(r["status"], "complete")
        f = r["files"][0]
        self.assertEqual(f["nodes"], 1)
        self.assertEqual([d["path"] for d in f["dependents"]], ["backend/a/a_test.go", "backend/b/b.go"])
        self.assertEqual(f["dependencies"], [{"path": "backend/c/c.go", "relations": ["imports_from"]}])
        self.assertEqual(f["tests"], ["backend/a/a_test.go"])
        self.assertEqual(f["testsChanged"], [])

    def test_changed_source_without_node_is_incomplete(self):
        r = review.impact(self.NODES, self.LINKS, [("M", "backend/a/a.go"), ("A", "backend/d/new.go")])
        self.assertEqual(r["status"], "incomplete")
        self.assertEqual(r["gaps"], ["backend/d/new.go"])

    def test_unparsed_types_and_deletions_are_listed_not_gaps(self):
        r = review.impact(self.NODES, self.LINKS, [("M", "docs/x.md"), ("M", "ci/run.sh"), ("D", "backend/old.go")])
        self.assertEqual(r["status"], "not-applicable")
        self.assertEqual(r["gaps"], [])
        self.assertEqual(r["unindexed"], ["docs/x.md", "ci/run.sh"])
        self.assertIn("deleted", r["files"][2]["note"])

    def test_markdown_names_the_commit_and_gaps(self):
        r = review.impact(self.NODES, self.LINKS, [("A", "backend/d/new.go")])
        r.update(indexedCommit="c" * 40, indexedTree="t" * 40, headCommit="h" * 40, baseCommit="b" * 40,
                 graphifyVersion="0.0", graph={"nodes": 5, "edges": 3, "files": 5})
        md = review.markdown(r)
        self.assertIn("`" + "c" * 40 + "`", md)
        self.assertIn("review incomplete", md)
        self.assertIn("`backend/d/new.go`", md)

    def test_test_file_pattern(self):
        for path in ("backend/x/y_test.go", "frontend/app/src/a.test.tsx", "tests/release/test_x.py", "tests/e2e/run.sh"):
            self.assertRegex(path, review.TEST_FILE)
        for path in ("backend/x/y.go", "frontend/app/src/testing.ts"):
            self.assertNotRegex(path, review.TEST_FILE)


@unittest.skipIf(shutil.which("node") is None, "node is not installed")
class N8nCode(unittest.TestCase):
    """Runs the workflow's Code nodes in node with a stand-in for n8n's $ and $input."""

    def run_js(self, node_name, nodes, input_items):
        wf = json.loads(gen.OUT.read_text(encoding="utf-8"))
        js = next(n["parameters"]["jsCode"] for n in wf["nodes"] if n["name"] == node_name)
        harness = (
            "const nodes = %s; const input = %s;\n"
            "const $ = (n) => { if (!(n in nodes)) throw new Error('not executed: ' + n);"
            " return { first: () => ({ json: nodes[n] }) }; };\n"
            "const $input = { first: () => input[0], all: () => input };\n"
            "(async () => { const out = await (async () => { %s })(); console.log(JSON.stringify(out)); })();\n"
        ) % (json.dumps(nodes), json.dumps(input_items), js)
        out = subprocess.run(["node", "-"], input=harness, capture_output=True, text=True, check=True)
        return json.loads(out.stdout)[0]["json"]

    SHA = "a" * 40

    def test_submit_validation(self):
        ok = self.run_js("Validate submit", {}, [{"json": {"query": {"sha": self.SHA, "branch": "feat/x", "nonce": "b" * 32}},
                                                  "binary": {"bundle": {}}}])
        self.assertTrue(ok["ok"])
        self.assertTrue(ok["bundleUrl"].endswith("/webhook/hc-prepush/bundle?name=%s-%s.bundle" % (self.SHA, "b" * 32)))
        for q in ({"sha": "abc", "branch": "x", "nonce": "b" * 32},
                  {"sha": self.SHA, "branch": "../x", "nonce": "b" * 32},
                  {"sha": self.SHA, "branch": "x", "nonce": "short"}):
            bad = self.run_js("Validate submit", {}, [{"json": {"query": q}, "binary": {"bundle": {}}}])
            self.assertFalse(bad["ok"], q)
        nobody = self.run_js("Validate submit", {}, [{"json": {"query": {"sha": self.SHA, "branch": "x", "nonce": "b" * 32}}}])
        self.assertFalse(nobody["ok"])

    def test_bundle_name_is_checked_exactly(self):
        good = "%s-%s.bundle" % (self.SHA, "c" * 32)
        self.assertTrue(self.run_js("Validate bundle", {}, [{"json": {"query": {"name": good}}}])["ok"])
        for name in ("../" + good, good + "/x", "x.bundle", ""):
            self.assertFalse(self.run_js("Validate bundle", {}, [{"json": {"query": {"name": name}}}])["ok"], name)

    def build(self, sha, result, in_progress=False, number=7):
        return {"number": number, "url": "http://j/job/p/%d/" % number, "result": result, "inProgress": in_progress,
                "actions": [{}, {"parameters": [{"name": "HC_PREPUSH_SHA", "value": sha}]}]}

    def test_find_build(self):
        nodes = {"Validate status": {"sha": self.SHA},
                 "Jenkins builds": {"builds": [self.build("b" * 40, "SUCCESS", number=8), self.build(self.SHA, None, True)]},
                 "Jenkins queue": {"items": []}}
        r = self.run_js("Find build", nodes, [{"json": {}}])
        self.assertEqual((r["state"], r["finished"], r["build"]["number"]), ("running", False, 7))
        nodes["Jenkins builds"] = {"builds": []}
        nodes["Jenkins queue"] = {"items": [{"id": 5, "task": {"name": "Homelab-Control-prepush"},
                                             "params": "\nHC_PREPUSH_SHA=%s\nHC_PREPUSH_BRANCH=x" % self.SHA}]}
        r = self.run_js("Find build", nodes, [{"json": {}}])
        self.assertEqual((r["state"], r["queue"]), ("queued", 5))
        nodes["Jenkins queue"] = {"items": []}
        self.assertEqual(self.run_js("Find build", nodes, [{"json": {}}])["state"], "unknown")

    def status(self, found, result):
        nodes = {"Find build": found}
        if result is not None:
            nodes["ci-result.json"] = {"statusCode": 200, "body": json.dumps(result)}
        return self.run_js("Status reply", nodes, [{"json": {}}])

    def test_pass_needs_success_same_commit_and_graphify(self):
        found = {"sha": self.SHA, "finished": True, "state": "SUCCESS"}
        good = {"commit": self.SHA, "mode": "prepush", "graphify": {"status": "complete"}}
        self.assertTrue(self.status(found, good)["pass"])
        self.assertFalse(self.status(found, {**good, "commit": "b" * 40})["pass"])
        self.assertFalse(self.status(found, {**good, "mode": "branch"})["pass"])
        self.assertFalse(self.status(found, {**good, "graphify": {"status": "incomplete"}})["pass"])
        self.assertFalse(self.status({**found, "state": "UNSTABLE"}, good)["pass"])
        self.assertFalse(self.status({**found, "finished": False, "state": "running"}, None)["pass"])


class _Quiet(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


@unittest.skipIf(shutil.which("git") is None, "git is not installed")
class PrepushCheckout(unittest.TestCase):
    """ci/prepush-checkout.sh against a real bundle served over HTTP."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="hc-prepush-test-")
        self.addCleanup(shutil.rmtree, self.tmp)
        self.env = {**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
                    "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@invalid",
                    "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@invalid"}
        self.serve = os.path.join(self.tmp, "serve")
        os.mkdir(self.serve)
        handler = functools.partial(_Quiet, directory=self.serve)
        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()
        self.addCleanup(self.httpd.server_close)
        self.addCleanup(self.httpd.shutdown)
        # "github": main with one commit; "dev": main plus local commits; "ci": a clone of main.
        self.gh = self.repo("github")
        self.commit(self.gh, "a.txt", "one\n")
        self.git(self.gh, "branch", "-M", "main")
        self.git(self.tmp, "clone", "-q", self.gh, "dev")
        self.dev = os.path.join(self.tmp, "dev")

    def repo(self, name):
        path = os.path.join(self.tmp, name)
        self.git(self.tmp, "init", "-q", "-b", "main", name)
        return path

    def git(self, cwd, *args):
        return subprocess.run(["git", *args], cwd=cwd, env=self.env, check=True, capture_output=True, text=True).stdout.strip()

    def commit(self, repo, path, text):
        with open(os.path.join(repo, path), "w", encoding="utf-8") as f:
            f.write(text)
        self.git(repo, "add", path)
        self.git(repo, "commit", "-q", "-m", "change " + path)
        return self.git(repo, "rev-parse", "HEAD")

    def bundle(self, sha):
        self.git(self.dev, "fetch", "-q", "origin")
        self.git(self.dev, "update-ref", "refs/hc-prepush/head", sha)
        name = "%s-%s.bundle" % (sha, "0" * 32)
        self.git(self.dev, "bundle", "create", "-q", os.path.join(self.serve, name), "refs/hc-prepush/head", "--not", "origin/main")
        return "http://127.0.0.1:%d/%s" % (self.httpd.server_address[1], name)

    def checkout(self, sha, url, base=""):
        ci = os.path.join(self.tmp, "ci-%d" % len(os.listdir(self.tmp)))
        self.git(self.tmp, "clone", "-q", self.gh, ci)
        r = subprocess.run([os.path.join(ROOT, "ci/prepush-checkout.sh")], cwd=ci, capture_output=True, text=True,
                           env={**self.env, "HC_PREPUSH_SHA": sha, "HC_PREPUSH_BUNDLE_URL": url, "HC_PREPUSH_BASE": base})
        return r, ci

    def test_head_is_merged_with_main_the_same_way_each_time(self):
        head = self.commit(self.dev, "b.txt", "local\n")
        main = self.commit(self.gh, "c.txt", "main moved\n")
        url = self.bundle(head)
        r1, ci1 = self.checkout(head, url)
        self.assertEqual(r1.returncode, 0, r1.stderr)
        self.assertEqual(r1.stdout.strip(), "base " + main)
        merged = self.git(ci1, "rev-parse", "HEAD")
        self.assertEqual(self.git(ci1, "rev-parse", "HEAD^1", "HEAD^2").split(), [head, main])
        r2, ci2 = self.checkout(head, url, base=main)
        self.assertEqual(r2.returncode, 0, r2.stderr)
        self.assertEqual(self.git(ci2, "rev-parse", "HEAD"), merged)

    def test_head_on_top_of_main_is_tested_as_it_is(self):
        head = self.commit(self.dev, "b.txt", "local\n")
        r, ci = self.checkout(head, self.bundle(head))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.git(ci, "rev-parse", "HEAD"), head)

    def test_wrong_commit_conflict_and_bad_input_fail(self):
        head = self.commit(self.dev, "a.txt", "local\n")
        url = self.bundle(head)
        r, _ = self.checkout("b" * 40, url)
        self.assertEqual(r.returncode, 2)
        self.assertIn("not " + "b" * 40, r.stderr)
        self.commit(self.gh, "a.txt", "conflict\n")
        r, _ = self.checkout(head, url)
        self.assertEqual(r.returncode, 1)
        self.assertIn("does not merge cleanly", r.stderr)
        r, _ = self.checkout(head, "file:///etc/passwd")
        self.assertEqual(r.returncode, 2)
        r, _ = self.checkout("HEAD", url)
        self.assertEqual(r.returncode, 2)


class Wiring(unittest.TestCase):
    def read(self, path):
        with open(os.path.join(ROOT, path), encoding="utf-8") as f:
            return f.read()

    def test_n8n_workflow_is_generated(self):
        out = subprocess.run(["python3", "-I", "ci/prepush/gen-n8n-workflow.py", "--check"], cwd=ROOT,
                             capture_output=True, text=True)
        self.assertEqual(out.returncode, 0, out.stderr)

    def test_every_stage_checks_out_through_checkout_source(self):
        text = self.read("Jenkinsfile")
        # The definition, and its one use inside checkoutSource.
        self.assertEqual(len(re.findall(r"checkoutFull\(\)", text)), 2)
        self.assertEqual(len(re.findall(r"checkoutSource\(", text)), 6)  # definition + 5 stages
        self.assertIn("stage('graphify')", text)
        self.assertLess(text.index("stage('graphify')"), text.index("stage('check')"))

    def test_toolchain_image_tag_covers_graphify_pins(self):
        self.assertIn("cat ci/Dockerfile ci/graphify-requirements.txt", self.read("ci/run.sh"))
        req = self.read("ci/graphify-requirements.txt")
        pins = re.findall(r"^([a-z0-9-]+)==\S+ \\$", req, re.M)
        self.assertIn("graphifyy", pins)
        for name in pins:  # every package has a hash, so --require-hashes can install it
            self.assertRegex(req, r"(?m)^%s==\S+ \\\n    --hash=sha256:[0-9a-f]{64}" % re.escape(name))

    def test_job_parameters_match_the_jenkinsfile(self):
        job = self.read("ci/prepush/jenkins-job.xml")
        jf = self.read("Jenkinsfile")
        names = re.findall(r"<name>(HC_PREPUSH_[A-Z_]+)</name>", job)
        self.assertEqual(sorted(names), ["HC_PREPUSH_BRANCH", "HC_PREPUSH_BUNDLE_URL", "HC_PREPUSH_RUN", "HC_PREPUSH_SHA"])
        for name in names:
            self.assertIn("params." + name, jf)
        wf = self.read("ci/prepush/n8n-workflow.json")
        for name in names:
            self.assertIn('"name": "%s"' % name, wf)


if __name__ == "__main__":
    unittest.main()
