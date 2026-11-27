#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Graphify impact review of the commit under test (Jenkins "graphify" stage, ci/README.md).

    python3 ci/graphify_review.py --base REV --out DIR [--expect SHA]

Indexes the checked-out commit (HEAD, without compat/) with Graphify's AST extraction, which needs
no model and no network, and lists for every file changed since --base the definitions in it, the
files that use them, the files they use and the linked tests. It is the evidence for the Graphify
review that docs/project/08-TESTING-AND-ACCEPTANCE.md requires; a person or agent still has to read
it and confirm findings against the source. It does not execute code or prove behaviour.

The index is of HEAD exactly: a modified working tree, or a HEAD other than --expect, is an error.
Exit status: 0 complete or not applicable, 1 incomplete (a changed source file in a language
Graphify parses has no node in the graph), 2 usage or index error.

Writes to DIR: INDEXED_SHA, STATUS, impact.json, IMPACT.md, GRAPH_REPORT.md, graph.json.gz.
"""

import argparse
import gzip
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# Languages whose files Graphify's AST extraction turns into nodes. A changed file in one of these
# with no node means the index does not cover the change, so the review is incomplete.
PARSED = {".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rs"}
TEST_FILE = re.compile(r"(^|/)tests?/|_test\.go$|\.test\.[cm]?[jt]sx?$|(^|/)test_[^/]*\.py$")
EXCLUDE = ":(exclude)compat"  # pristine upstream subtree; not reviewed here (CLAUDE.md)
LIST_CAP = 40  # per list in IMPACT.md; impact.json has everything


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def changed_files(base):
    """[(status, path)] from base to HEAD, compat/ excluded, renames as delete + add."""
    out = git("diff", "--name-status", "--no-renames", base, "HEAD", "--", ".", EXCLUDE)
    rows = []
    for line in out.splitlines():
        status, _, path = line.partition("\t")
        rows.append((status[:1], path))
    return rows


def impact(nodes, links, changed):
    """Pure: the review data for each changed file, from Graphify's node and link lists."""
    file_of = {n["id"]: n.get("source_file") for n in nodes}
    per_file = {}
    for n in nodes:
        per_file.setdefault(n.get("source_file"), []).append(n)
    changed_paths = {p for _, p in changed}
    files, gaps, unindexed = [], [], []
    for status, path in changed:
        ext = os.path.splitext(path)[1]
        entry = {"path": path, "status": status, "nodes": 0, "dependents": [], "dependencies": [], "tests": []}
        if status == "D":
            entry["note"] = "deleted; Graphify indexes the tested commit only, so check former callers by search"
            files.append(entry)
            continue
        own = per_file.get(path, [])
        entry["nodes"] = len(own)
        if not own:
            (gaps if ext in PARSED else unindexed).append(path)
            entry["note"] = "no node in the graph" + ("" if ext in PARSED else " (Graphify does not parse this file type)")
            files.append(entry)
            continue
        ids = {n["id"] for n in own}
        dependents, dependencies = {}, {}
        for link in links:
            s, t = link["source"], link["target"]
            sf, tf = file_of.get(s), file_of.get(t)
            if t in ids and s not in ids and sf and sf != path:
                dependents.setdefault(sf, set()).add(link.get("relation", "?"))
            if s in ids and t not in ids and tf and tf != path:
                dependencies.setdefault(tf, set()).add(link.get("relation", "?"))
        linked = set(dependents) | set(dependencies)
        entry["dependents"] = [{"path": f, "relations": sorted(r)} for f, r in sorted(dependents.items())]
        entry["dependencies"] = [{"path": f, "relations": sorted(r)} for f, r in sorted(dependencies.items())]
        entry["tests"] = sorted(f for f in linked if TEST_FILE.search(f))
        entry["testsChanged"] = sorted(f for f in entry["tests"] if f in changed_paths)
        entry["definitions"] = sorted({n.get("label", n["id"]) for n in own})
        files.append(entry)
    parsed_changes = [p for s, p in changed if s != "D" and os.path.splitext(p)[1] in PARSED]
    if gaps:
        status = "incomplete"
    elif not parsed_changes:
        status = "not-applicable"
    else:
        status = "complete"
    return {"status": status, "gaps": gaps, "unindexed": unindexed, "files": files}


def markdown(result):
    r = result
    lines = [
        "# Graphify impact review",
        "",
        f"- Status: **{r['status']}**",
        f"- Indexed commit: `{r['indexedCommit']}` (tree `{r['indexedTree']}`)",
        f"- Head commit: `{r['headCommit']}`; base: `{r['baseCommit']}`",
        f"- Graphify {r['graphifyVersion']}: {r['graph']['nodes']} nodes, {r['graph']['edges']} edges, "
        f"{r['graph']['files']} files (AST extraction, compat/ excluded)",
        "",
        "This lists what the change touches. It is not a review verdict: read the source of each",
        "changed definition and its dependents, record findings, and fix the confirmed ones",
        "(docs/project/08-TESTING-AND-ACCEPTANCE.md). A missing edge is not proof that nothing depends on it.",
        "",
    ]
    if r["gaps"]:
        lines += ["## Not covered by the index (review incomplete)", ""] + [f"- `{p}`" for p in r["gaps"]] + [""]
    if r["unindexed"]:
        lines += ["## File types Graphify does not parse (review by reading)", ""]
        lines += [f"- `{p}`" for p in r["unindexed"]] + [""]
    lines += ["## Changed files", ""]
    for f in r["files"]:
        lines.append(f"### `{f['path']}` ({f['status']}, {f['nodes']} nodes)")
        if f.get("note"):
            lines.append(f"- {f['note']}")
        for key, title in (("dependents", "Used by"), ("dependencies", "Uses")):
            items = f[key]
            if items:
                shown = ", ".join(f"`{i['path']}`" for i in items[:LIST_CAP])
                more = f" and {len(items) - LIST_CAP} more" if len(items) > LIST_CAP else ""
                lines.append(f"- {title} ({len(items)}): {shown}{more}")
        if f["nodes"]:
            tests = ", ".join(f"`{t}`" for t in f["tests"]) or "none found"
            lines.append(f"- Linked tests: {tests}")
        lines.append("")
    return "\n".join(lines)


def build_graph(src, out, commit):
    """Graphify AST extraction of src; writes graph.json.gz and GRAPH_REPORT.md. Returns (nodes, links, meta)."""
    from importlib.metadata import version

    from graphify.analyze import god_nodes, suggest_questions, surprising_connections
    from graphify.build import build_from_json
    from graphify.cluster import cluster, score_all
    from graphify.detect import detect
    from graphify.export import to_json
    from graphify.extract import collect_files, extract
    from graphify.report import generate

    cache = out / ".cache"
    det = detect(src, gitignore=False, cache_root=cache)
    code = []
    for f in det["files"].get("code", []):
        p = Path(f)
        code.extend(collect_files(p) if p.is_dir() else [p])
    ext = extract(code, cache_root=cache, root=src)
    ext.setdefault("hyperedges", [])
    G = build_from_json(ext, root=str(src), directed=True)
    U = G.to_undirected()
    comm = cluster(U)
    labels = {c: f"Community {c}" for c in comm}
    graph_json = out / "graph.json"
    to_json(G, comm, str(graph_json), force=True, built_at_commit=commit)
    report = generate(U, comm, score_all(U, comm), labels, god_nodes(U), surprising_connections(U, comm), det,
                      {"input": 0, "output": 0}, "tested commit", suggested_questions=suggest_questions(U, comm, labels))
    (out / "GRAPH_REPORT.md").write_text(report, encoding="utf-8")
    data = json.loads(graph_json.read_text(encoding="utf-8"))
    with open(graph_json, "rb") as fin, gzip.open(out / "graph.json.gz", "wb") as fout:
        shutil.copyfileobj(fin, fout)
    graph_json.unlink()
    shutil.rmtree(cache, ignore_errors=True)
    nodes, links = data["nodes"], data.get("links", data.get("edges", []))
    meta = {"nodes": len(nodes), "edges": len(links), "files": len({n.get("source_file") for n in nodes})}
    return nodes, links, meta, version("graphifyy")


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--base", required=True, help="commit the change is compared with")
    ap.add_argument("--out", required=True, type=Path)
    ap.add_argument("--expect", help="commit that must be checked out (the commit under test)")
    a = ap.parse_args(argv)
    try:
        commit = git("rev-parse", "HEAD").strip()
        base = git("rev-parse", "--verify", a.base + "^{commit}").strip()
        if a.expect and git("rev-parse", "--verify", a.expect + "^{commit}").strip() != commit:
            raise SystemExit(f"graphify_review: HEAD is {commit}, not the commit under test {a.expect}")
        if git("status", "--porcelain", "--untracked-files=no").strip():
            raise SystemExit("graphify_review: the working tree differs from HEAD; the index would not be of the tested commit")
        parents = git("rev-list", "--parents", "-n1", "HEAD").split()[1:]
        head = parents[0] if len(parents) > 1 else commit  # a pull request build tests head merged with base
        tree = git("rev-parse", "HEAD^{tree}").strip()
    except subprocess.CalledProcessError as e:
        print(f"graphify_review: git failed: {e.stderr.strip()}", file=sys.stderr)
        return 2
    a.out.mkdir(parents=True, exist_ok=True)
    changed = changed_files(base)
    with tempfile.TemporaryDirectory(prefix=".src-", dir=a.out) as tmp:  # never /tmp (CLAUDE.md)
        src = Path(tmp) / "src"
        src.mkdir()
        archive = subprocess.Popen(["git", "archive", "--format=tar", commit, "--", ".", EXCLUDE], stdout=subprocess.PIPE)
        subprocess.run(["tar", "-x", "-C", str(src)], stdin=archive.stdout, check=True)
        if archive.wait() != 0:
            print("graphify_review: git archive failed", file=sys.stderr)
            return 2
        nodes, links, meta, gversion = build_graph(src, a.out, commit)
    if git("rev-parse", "HEAD").strip() != commit:
        print("graphify_review: HEAD moved while indexing", file=sys.stderr)
        return 2
    result = impact(nodes, links, changed)
    result.update(indexedCommit=commit, indexedTree=tree, headCommit=head, baseCommit=base,
                  graphifyVersion=gversion, graph=meta)
    (a.out / "INDEXED_SHA").write_text(commit + "\n", encoding="utf-8")
    (a.out / "STATUS").write_text(result["status"] + "\n", encoding="utf-8")
    (a.out / "impact.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    (a.out / "IMPACT.md").write_text(markdown(result), encoding="utf-8")
    print(f"graphify_review: {result['status']}: {len(changed)} changed files, "
          f"{meta['nodes']} nodes indexed at {commit}")
    for p in result["gaps"]:
        print(f"graphify_review: not covered by the index: {p}", file=sys.stderr)
    return 1 if result["status"] == "incomplete" else 0


if __name__ == "__main__":
    sys.exit(main())
