#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

"""Writes ci/prepush/n8n-workflow.json, the n8n workflow "HC pre-push" (ci/README.md).

    python3 -I ci/prepush/gen-n8n-workflow.py [--check]

The workflow is generated so that its JavaScript is readable here and the JSON stays valid. Edit
this file, regenerate, and import the JSON into n8n. --check fails when the JSON is out of date
(tests/release/test_prepush.py runs it).
"""

import json
import sys
from pathlib import Path

OUT = Path(__file__).with_name("n8n-workflow.json")

# Edit these for another host. The Jenkins agents fetch bundles from N8N_URL, so it must be an address
# they reach (not localhost). BUNDLE_DIR is the n8n container's path of the host directory
# /containers/appdata/n8n/files/hc-prepush; n8n's file access must allow it (N8N_RESTRICT_FILE_ACCESS_TO).
CONFIG = """const CONFIG = {
  jenkinsUrl: 'http://192.168.1.44:8380',
  job: 'Homelab-Control-prepush',
  n8nUrl: 'http://192.168.1.44:5678',
  bundleDir: '/files/hc-prepush',
};
"""

VALIDATE_SUBMIT = CONFIG + r"""
// Input: POST /webhook/hc-prepush/submit?sha=<40 hex>&branch=<name>&nonce=<32 hex>, body = git bundle.
const item = $input.first();
const q = item.json.query || {};
const sha = String(q.sha || '');
const branch = String(q.branch || '');
const nonce = String(q.nonce || '');
const errors = [];
if (!/^[0-9a-f]{40}$/.test(sha)) errors.push('sha must be a full commit id');
if (!/^[A-Za-z0-9._\/-]{1,200}$/.test(branch) || branch.includes('..')) errors.push('bad branch name');
if (!/^[0-9a-f]{32}$/.test(nonce)) errors.push('nonce must be 32 hex characters');
if (!item.binary || !item.binary.bundle) errors.push('the request body must be the git bundle (application/octet-stream)');
if (errors.length) return [{ json: { ok: false, error: errors.join('; ') } }];
const name = `${sha}-${nonce}.bundle`;
return [{
  json: {
    ok: true, sha, branch, run: nonce,
    path: `${CONFIG.bundleDir}/${name}`,
    bundleUrl: `${CONFIG.n8nUrl}/webhook/hc-prepush/bundle?name=${name}`,
    buildUrl: `${CONFIG.jenkinsUrl}/job/${CONFIG.job}/buildWithParameters`,
    statusUrl: `${CONFIG.n8nUrl}/webhook/hc-prepush/status?sha=${sha}`,
  },
  binary: item.binary,
}];
"""

SUBMITTED = r"""
const req = $('Validate submit').first().json;
const res = $input.first().json;
const status = res.statusCode;
const location = (res.headers && (res.headers.location || res.headers.Location)) || null;
if (status !== 201 || !location) {
  return [{ json: { ok: false, httpStatus: 502, error: `Jenkins did not queue the build (HTTP ${status})` } }];
}
return [{ json: { ok: true, httpStatus: 202, sha: req.sha, branch: req.branch, run: req.run, queue: location, status: req.statusUrl } }];
"""

VALIDATE_STATUS = CONFIG + r"""
const sha = String(($input.first().json.query || {}).sha || '');
if (!/^[0-9a-f]{40}$/.test(sha)) return [{ json: { ok: false, error: 'sha must be a full commit id' } }];
const tree = 'builds[number,url,result,inProgress,actions[parameters[name,value]]]{0,30}';
return [{ json: {
  ok: true, sha,
  buildsUrl: `${CONFIG.jenkinsUrl}/job/${CONFIG.job}/api/json?tree=${encodeURIComponent(tree)}`,
  queueUrl: `${CONFIG.jenkinsUrl}/queue/api/json?tree=${encodeURIComponent('items[id,task[name],params,why]')}`,
} }];
"""

FIND_BUILD = r"""
// The newest build of the job whose HC_PREPUSH_SHA is this commit; else a queued item for it.
const sha = $('Validate status').first().json.sha;
const builds = ($('Jenkins builds').first().json.builds || []);
const param = (b, n) => {
  for (const a of (b.actions || [])) for (const p of (a.parameters || [])) if (p.name === n) return p.value;
  return null;
};
const build = builds.find(b => param(b, 'HC_PREPUSH_SHA') === sha);
if (build) {
  const finished = !build.inProgress && build.result !== null;
  return [{ json: {
    sha, finished,
    state: finished ? build.result : 'running',
    build: { number: build.number, url: build.url },
    resultUrl: `${build.url}artifact/ci-result.json`,
    impactUrl: `${build.url}artifact/reports/graphify/IMPACT.md`,
  } }];
}
const items = ($('Jenkins queue').first().json.items || []);
const queued = items.find(i => i.task && i.task.name === 'Homelab-Control-prepush' && String(i.params || '').includes(`HC_PREPUSH_SHA=${sha}`));
return [{ json: { sha, finished: false, state: queued ? 'queued' : 'unknown', queue: queued ? queued.id : null } }];
"""

STATUS_REPLY = r"""
const found = $('Find build').first().json;
let result = null;
try {
  const r = $('ci-result.json').first().json;
  // With fullResponse and responseFormat "text", n8n puts the body under "data", not "body".
  const raw = r && r.data !== undefined ? r.data : r && r.body;
  if (r && r.statusCode === 200) result = typeof raw === 'string' ? JSON.parse(raw) : raw;
} catch (e) { /* not fetched: the build is not finished */ }
// A finished build is only a pass for this commit when its summary names it and Graphify indexed what was tested.
let pass = false;
if (found.finished && result) {
  pass = found.state === 'SUCCESS' && result.commit === found.sha && result.mode === 'prepush'
    && result.graphify && ['complete', 'not-applicable'].includes(result.graphify.status);
}
return [{ json: { ...found, pass, result } }];
"""

VALIDATE_BUNDLE = CONFIG + r"""
// GET /webhook/hc-prepush/bundle?name=<sha>-<nonce>.bundle (the Jenkins agent). The random nonce in the
// name is the only key, so the name is checked exactly and never used to build any other path.
const name = String(($input.first().json.query || {}).name || '');
if (!/^[0-9a-f]{40}-[0-9a-f]{32}\.bundle$/.test(name)) return [{ json: { ok: false } }];
return [{ json: { ok: true, path: `${CONFIG.bundleDir}/${name}` } }];
"""


def code(name, js, x, y):
    return {"parameters": {"jsCode": js.strip() + "\n"}, "name": name, "type": "n8n-nodes-base.code",
            "typeVersion": 2, "position": [x, y]}


def if_ok(name, expr, x, y):
    return {"parameters": {"conditions": {
        "options": {"caseSensitive": True, "leftValue": "", "typeValidation": "strict"},
        "conditions": [{"id": name, "leftValue": expr, "rightValue": "",
                        "operator": {"type": "boolean", "operation": "true", "singleValue": True}}],
        "combinator": "and"}, "options": {}},
        "name": name, "type": "n8n-nodes-base.if", "typeVersion": 2, "position": [x, y]}


def webhook(name, method, path, auth, x, y, binary=False):
    p = {"httpMethod": method, "path": path, "responseMode": "responseNode", "options": {}}
    node = {"parameters": p, "name": name, "type": "n8n-nodes-base.webhook", "typeVersion": 2,
            "position": [x, y], "webhookId": path.replace("/", "-")}
    if binary:
        p["options"]["binaryPropertyName"] = "bundle"
    if auth:
        p["authentication"] = "headerAuth"
        node["credentials"] = {"httpHeaderAuth": {"name": "HC pre-push client"}}
    return node


def respond_json(name, body, code_expr, x, y):
    return {"parameters": {"respondWith": "json", "responseBody": body,
                           "options": {"responseCode": code_expr}},
            "name": name, "type": "n8n-nodes-base.respondToWebhook", "typeVersion": 1.1, "position": [x, y]}


def jenkins_get(name, url_expr, x, y):
    return {"parameters": {"url": url_expr, "authentication": "genericCredentialType",
                           "genericAuthType": "httpBasicAuth", "options": {"timeout": 20000}},
            "name": name, "type": "n8n-nodes-base.httpRequest", "typeVersion": 4.2, "position": [x, y],
            "credentials": {"httpBasicAuth": {"name": "Jenkins hc-prepush"}}}


def workflow():
    n = [
        # submit
        webhook("Submit", "POST", "hc-prepush/submit", True, 0, 0, binary=True),
        code("Validate submit", VALIDATE_SUBMIT, 220, 0),
        if_ok("Submit valid?", "={{ $json.ok }}", 440, 0),
        {"parameters": {"operation": "write", "fileName": "={{ $json.path }}", "dataPropertyName": "bundle",
                        "options": {}},
         "name": "Store bundle", "type": "n8n-nodes-base.readWriteFile", "typeVersion": 1, "position": [660, -80]},
        {"parameters": {"method": "POST", "url": "={{ $('Validate submit').first().json.buildUrl }}",
                        "authentication": "genericCredentialType", "genericAuthType": "httpBasicAuth",
                        "sendQuery": True, "queryParameters": {"parameters": [
                            {"name": "HC_PREPUSH_SHA", "value": "={{ $('Validate submit').first().json.sha }}"},
                            {"name": "HC_PREPUSH_BRANCH", "value": "={{ $('Validate submit').first().json.branch }}"},
                            {"name": "HC_PREPUSH_BUNDLE_URL", "value": "={{ $('Validate submit').first().json.bundleUrl }}"},
                            {"name": "HC_PREPUSH_RUN", "value": "={{ $('Validate submit').first().json.run }}"}]},
                        "options": {"timeout": 20000, "redirect": {"redirect": {"followRedirects": False}},
                                    "response": {"response": {"fullResponse": True, "neverError": True}}}},
         "name": "Start Jenkins build", "type": "n8n-nodes-base.httpRequest", "typeVersion": 4.2,
         "position": [880, -80], "credentials": {"httpBasicAuth": {"name": "Jenkins hc-prepush"}}},
        code("Submitted", SUBMITTED, 1100, -80),
        respond_json("Reply submit", "={{ $json }}", "={{ $json.httpStatus }}", 1320, -80),
        respond_json("Reject submit", "={{ $json }}", 400, 660, 120),
        # status
        webhook("Status", "GET", "hc-prepush/status", True, 0, 400),
        code("Validate status", VALIDATE_STATUS, 220, 400),
        if_ok("Status valid?", "={{ $json.ok }}", 440, 400),
        jenkins_get("Jenkins builds", "={{ $json.buildsUrl }}", 660, 320),
        jenkins_get("Jenkins queue", "={{ $('Validate status').first().json.queueUrl }}", 880, 320),
        code("Find build", FIND_BUILD, 1100, 320),
        if_ok("Finished?", "={{ $json.finished }}", 1320, 320),
        {**jenkins_get("ci-result.json", "={{ $json.resultUrl }}", 1540, 240),
         "parameters": {"url": "={{ $json.resultUrl }}", "authentication": "genericCredentialType",
                        "genericAuthType": "httpBasicAuth",
                        "options": {"timeout": 20000, "response": {"response": {
                            "fullResponse": True, "neverError": True, "responseFormat": "text"}}}}},
        code("Status reply", STATUS_REPLY, 1760, 320),
        respond_json("Reply status", "={{ $json }}", 200, 1980, 320),
        respond_json("Reject status", "={{ $json }}", 400, 660, 520),
        # bundle
        webhook("Bundle", "GET", "hc-prepush/bundle", False, 0, 800),
        code("Validate bundle", VALIDATE_BUNDLE, 220, 800),
        if_ok("Bundle name valid?", "={{ $json.ok }}", 440, 800),
        {"parameters": {"operation": "read", "fileSelector": "={{ $json.path }}",
                        "options": {"dataPropertyName": "bundle"}},
         "name": "Read bundle", "type": "n8n-nodes-base.readWriteFile", "typeVersion": 1, "position": [660, 720],
         # A missing file gives no item; without one no reply node runs and n8n answers an empty 200.
         "alwaysOutputData": True},
        if_ok("Bundle found?", "={{ !!$binary.bundle }}", 880, 720),
        {"parameters": {"respondWith": "binary", "responseDataSource": "set", "inputFieldName": "bundle",
                        "options": {}},
         "name": "Send bundle", "type": "n8n-nodes-base.respondToWebhook", "typeVersion": 1.1, "position": [1100, 720]},
        respond_json("No such bundle", '={{ {"error": "not found"} }}', 404, 660, 920),
    ]

    def to(*names):
        return [[{"node": x, "type": "main", "index": 0}] for x in names]

    def branch(yes, no):
        return {"main": [[{"node": yes, "type": "main", "index": 0}], [{"node": no, "type": "main", "index": 0}]]}

    c = {
        "Submit": {"main": to("Validate submit")},
        "Validate submit": {"main": to("Submit valid?")},
        "Submit valid?": branch("Store bundle", "Reject submit"),
        "Store bundle": {"main": to("Start Jenkins build")},
        "Start Jenkins build": {"main": to("Submitted")},
        "Submitted": {"main": to("Reply submit")},
        "Status": {"main": to("Validate status")},
        "Validate status": {"main": to("Status valid?")},
        "Status valid?": branch("Jenkins builds", "Reject status"),
        "Jenkins builds": {"main": to("Jenkins queue")},
        "Jenkins queue": {"main": to("Find build")},
        "Find build": {"main": to("Finished?")},
        "Finished?": branch("ci-result.json", "Status reply"),
        "ci-result.json": {"main": to("Status reply")},
        "Status reply": {"main": to("Reply status")},
        "Bundle": {"main": to("Validate bundle")},
        "Validate bundle": {"main": to("Bundle name valid?")},
        "Bundle name valid?": branch("Read bundle", "No such bundle"),
        "Read bundle": {"main": to("Bundle found?")},
        "Bundle found?": branch("Send bundle", "No such bundle"),
    }
    return {"name": "HC pre-push", "nodes": n, "connections": c, "active": False,
            "settings": {"executionOrder": "v1", "saveDataSuccessExecution": "none"},
            "meta": {"note": "Generated by ci/prepush/gen-n8n-workflow.py; see ci/README.md"}}


def main():
    text = json.dumps(workflow(), indent=2) + "\n"
    if sys.argv[1:] == ["--check"]:
        if not OUT.exists() or OUT.read_text(encoding="utf-8") != text:
            print("ci/prepush/n8n-workflow.json is out of date; run python3 -I ci/prepush/gen-n8n-workflow.py",
                  file=sys.stderr)
            return 1
        return 0
    OUT.write_text(text, encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
