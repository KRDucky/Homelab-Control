#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Read-only /files Playwright + axe against the opt-in files fixture.

Run TestFilesBrowserFixture with a private runtime directory first. No browser
request is mocked. The only tree is the labelled synthetic jail (readme.txt,
sub/inner.txt). Runtime credentials/state MUST NOT be copied into evidence or
Git. Not live host browse, not M2 close, not acceptance.

Usage: files_browser.py RUNTIME_DIR EVIDENCE_DIR AXE_MIN_JS
"""
import json
import os
from pathlib import Path
import sys
import time
from urllib.parse import parse_qs, urlsplit

from playwright.sync_api import sync_playwright

runtime = Path(sys.argv[1])
evidence = Path(sys.argv[2])
axe = Path(sys.argv[3])
checks = []
password = ""
WRITE_METHODS = {"POST", "PUT", "PATCH", "DELETE"}
FILES_PREFIX = "/api/v1/files/"


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    checks.append(name)
    print("PASS " + name, flush=True)


def files_path(url):
    return urlsplit(url).path


def is_files_api(url):
    return files_path(url).startswith(FILES_PREFIX)


def capture_files(bucket):
    def on_request(req):
        if not is_files_api(req.url):
            return
        bucket.append({"method": req.method.upper(), "url": req.url, "headers": dict(req.headers)})

    return on_request


def first_entries(bucket):
    return next((r for r in bucket if r["method"] == "GET" and files_path(r["url"]).endswith("/files/entries")), None)


def query(url, key):
    return parse_qs(urlsplit(url).query, keep_blank_values=True).get(key, [None])[0]


def page_origin(url):
    parts = urlsplit(url)
    return f"{parts.scheme}://{parts.netloc}"


try:
    connection = runtime / "connection.json"
    deadline = time.monotonic() + 45
    while not connection.exists():
        if time.monotonic() > deadline:
            raise RuntimeError("fixture connection file did not become ready")
        time.sleep(0.1)
    config = json.loads(connection.read_text())
    password = config["password"]
    connection.unlink()  # credentials are consumed, never evidence
    url = config["url"]
    files_user = config.get("files_username") or config["username"]
    origin = page_origin(url)
    jail = str((runtime / "jail").resolve())
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"), headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors = []
        files_log = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.on("request", capture_files(files_log))
        page.goto(url + "/files")
        page.get_by_label("Username", exact=True).fill(files_user)
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.get_by_role("heading", name="Files", exact=True).wait_for()
        page.get_by_text("readme.txt", exact=True).wait_for()
        check("real login and files heading", page.get_by_role("heading", name="Files", exact=True).is_visible())
        visible = page.locator("body").inner_text()
        for needle in ("/tmp", "/home", "/etc", jail):
            check("no host path text " + needle, needle not in visible)
        table = page.get_by_role("table", name="Contents of Lab files")
        check("table lists readme.txt", table.get_by_text("readme.txt", exact=True).is_visible())
        check("root label Lab files", page.get_by_text("Lab files", exact=True).count() > 0)
        first = first_entries(files_log)
        check("first entries GET uses empty path", first is not None and query(first["url"], "path") == "")
        files_gets = [r for r in files_log if r["method"] == "GET" and files_path(r["url"]).endswith(("/files/roots", "/files/entries"))]
        check("roots and entries requests observed", len(files_gets) >= 2)
        for r in files_gets:
            hdrs = r["headers"]
            check("X-CSRF-Token on " + files_path(r["url"]), bool(hdrs.get("x-csrf-token")))
            sent_origin = hdrs.get("origin")
            check("client did not set Origin on " + files_path(r["url"]), sent_origin in (None, "", origin))
        refused_roots = context.request.get(url + "/api/v1/files/roots")
        refused_entries = context.request.get(url + "/api/v1/files/entries?root=lab&path=")
        check(
            "cookie-only files roots refused (CSRF token required)",
            refused_roots.status == 403 and refused_roots.json().get("error") == "csrf_rejected",
        )
        check(
            "cookie-only files entries refused (CSRF token required)",
            refused_entries.status == 403 and refused_entries.json().get("error") == "csrf_rejected",
        )
        for name in ("New folder", "Permissions", "Download"):
            check(name + " absent", page.get_by_role("button", name=name).count() == 0)
        check(
            "no files write methods",
            not any(r["method"] in WRITE_METHODS for r in files_log),
        )
        check(
            "files traffic is roots/entries GET only",
            all(r["method"] == "GET" and files_path(r["url"]).endswith(("/files/roots", "/files/entries")) for r in files_log),
        )
        before_sub = len(files_log)
        page.get_by_role("button", name="sub", exact=True).first.click()
        page.get_by_text("inner.txt", exact=True).wait_for()
        after_sub = files_log[before_sub:]
        check("opening sub is GET-only", after_sub and all(r["method"] == "GET" for r in after_sub))
        check("opening sub does not download content", "example-inner" not in page.locator("body").inner_text())
        check(
            "still no files write methods after sub",
            not any(r["method"] in WRITE_METHODS for r in files_log),
        )
        for name in ("New folder", "Permissions", "Download"):
            check(name + " still absent", page.get_by_role("button", name=name).count() == 0)
        page.screenshot(path=str(evidence / "desktop.png"), full_page=True)
        page.evaluate(axe.read_text() + "\n;0")
        violations = page.evaluate("async () => (await axe.run(document, {runOnly: {type:'tag', values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v => ({id:v.id,impact:v.impact,targets:v.nodes.map(n=>n.target)}))")
        (evidence / "axe.json").write_text(json.dumps(violations, indent=2))
        check("desktop accessibility", not violations)
        page.set_viewport_size({"width": 390, "height": 844})
        page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
        page.screenshot(path=str(evidence / "mobile.png"), full_page=True)
        overflow = page.evaluate("() => [...document.querySelectorAll('body *')].map(e=>({tag:e.tagName,cls:typeof e.className==='string'?e.className:'',right:e.getBoundingClientRect().right,width:e.getBoundingClientRect().width})).filter(e=>e.right>innerWidth+1).slice(0,30)")
        (evidence / "overflow.json").write_text(json.dumps(overflow, indent=2))
        check("mobile layout has no document overflow", page.evaluate("document.documentElement.scrollWidth <= innerWidth + 1"))
        page.evaluate(axe.read_text() + "\n;0")
        mobile = page.evaluate("async () => (await axe.run(document, {runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v=>({id:v.id,impact:v.impact,targets:v.nodes.map(n=>n.target)}))")
        (evidence / "axe-mobile.json").write_text(json.dumps(mobile, indent=2))
        check("mobile accessibility", not mobile)
        limited = browser.new_context(viewport={"width": 1280, "height": 900})
        lp = limited.new_page()
        limited_files = []
        limited_ok = []
        lp.on("request", capture_files(limited_files))
        lp.on("response", lambda resp: limited_ok.append(resp.url) if is_files_api(resp.url) and resp.status == 200 else None)
        lp.goto(url + "/files")
        lp.get_by_label("Username", exact=True).fill(config["limited_username"])
        lp.get_by_label("Password", exact=True).fill(password)
        lp.get_by_role("button", name="Sign in", exact=True).click()
        lp.get_by_role("status").filter(has_text="files.read").wait_for()
        check("limited account is told files.read is required", lp.get_by_role("status").filter(has_text="files.read").count() > 0)
        check("limited session has no files API 200", not limited_ok)
        limited_text = lp.locator("body").inner_text()
        check("limited session never sees entry names", "readme.txt" not in limited_text and "inner.txt" not in limited_text)
        check("no page script errors", not errors)
        limited.close()
        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(json.dumps({"result": "pass", "checks": checks, "source": "Labelled synthetic jail only; not live host browse; not M2 close."}, indent=2))
    print(f"RESULT {len(checks)} passed, 0 failed", flush=True)
except Exception as exc:
    message = str(exc)
    if password:
        message = message.replace(password, "[REDACTED]")
    (evidence / "FAILURE.txt").write_text(message)
    print("FAIL " + message, flush=True)
    sys.exit(1)
finally:
    (runtime / "stop").touch()
