#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Preview-only /host-settings Playwright + axe against the opt-in fixture.

Run TestHostSettingsBrowserFixture with a private runtime directory first. No
browser request is mocked. The only observations are the labelled synthetic
testdata/hostsettings tree (fixture-static, Fixture Lab (example),
fixture-kernel-name, Europe/Berlin). Preview never posts plan or apply and
never runs hostnamectl or timedatectl. Runtime credentials/state MUST NOT be
copied into evidence or Git. Not live host apply, not M2 close, not acceptance.

Usage: host_settings_browser.py RUNTIME_DIR EVIDENCE_DIR AXE_MIN_JS
"""
import json
import os
from pathlib import Path
import sys
import time
from urllib.parse import urlsplit

from playwright.sync_api import sync_playwright

runtime = Path(sys.argv[1])
evidence = Path(sys.argv[2])
axe = Path(sys.argv[3])
checks = []
password = ""
HOST_PREFIX = "/api/v1/host/settings"
WRITE_METHODS = {"POST", "PUT", "PATCH", "DELETE"}
PLAN_APPLY = ("/host/settings/plan", "/host/settings/apply")


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    checks.append(name)
    print("PASS " + name, flush=True)


def is_host_api(url):
    return urlsplit(url).path.startswith(HOST_PREFIX)


def capture_host(bucket):
    def on_request(req):
        if not is_host_api(req.url):
            return
        bucket.append({"method": req.method.upper(), "url": req.url, "headers": dict(req.headers)})

    return on_request


def page_origin(url):
    parts = urlsplit(url)
    return f"{parts.scheme}://{parts.netloc}"


def path_of(url):
    return urlsplit(url).path


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
    preview_user = config.get("username") or "preview"
    origin = page_origin(url)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"), headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors = []
        host_log = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.on("request", capture_host(host_log))
        page.goto(url + "/host-settings")
        page.get_by_label("Username", exact=True).fill(preview_user)
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.get_by_role("heading", name="Host settings", exact=True).wait_for()
        page.get_by_text("fixture-static", exact=True).wait_for()
        check("real login and host settings heading", page.get_by_role("heading", name="Host settings", exact=True).is_visible())
        check("current static host name", page.get_by_text("fixture-static", exact=True).is_visible())
        check("current pretty host name", page.get_by_text("Fixture Lab (example)", exact=True).is_visible())
        check("current kernel host name", page.get_by_text("fixture-kernel-name", exact=True).is_visible())
        check("current time zone", page.get_by_text("Europe/Berlin", exact=True).is_visible())
        check("preview form is offered", page.get_by_role("button", name="Preview (does not apply)", exact=True).is_visible())
        check("apply is not offered before preview", page.get_by_role("button", name="Apply these settings…").count() == 0)
        gets = [r for r in host_log if r["method"] == "GET" and path_of(r["url"]).endswith("/host/settings")]
        check("GET /api/v1/host/settings observed", len(gets) >= 1)
        page.get_by_label("Static host name", exact=True).fill("nas-01")
        page.get_by_label("Time zone", exact=True).fill("Etc/UTC")
        before_preview = len(host_log)
        page.get_by_role("button", name="Preview (does not apply)", exact=True).click()
        page.get_by_text("Preview result — not applied", exact=True).wait_for()
        check("preview result labelled not applied", page.get_by_text("Preview result — not applied", exact=True).is_visible())
        check("preview says nothing was changed", page.get_by_text("Nothing was changed on this host", exact=False).count() > 0)
        check("preview would-change is shown", page.get_by_text("Would change", exact=True).count() > 0)
        check("apply stays hidden without host.settings.apply", page.get_by_role("button", name="Apply these settings…").count() == 0)
        check("apply stays hidden without apply_available", page.get_by_role("button", name="Apply host settings").count() == 0)
        after_preview = host_log[before_preview:]
        posts = [r for r in after_preview if r["method"] == "POST"]
        check("preview POST observed", len(posts) == 1 and path_of(posts[0]["url"]).endswith("/host/settings/preview"))
        check("X-CSRF-Token on preview POST", bool(posts[0]["headers"].get("x-csrf-token")))
        check(
            "no plan or apply requests",
            not any(path_of(r["url"]).endswith(PLAN_APPLY) for r in host_log),
        )
        check(
            "host-settings writes are preview POST only",
            all(r["method"] not in WRITE_METHODS or path_of(r["url"]).endswith("/host/settings/preview") for r in host_log),
        )
        check("current static still fixture-static after preview", page.get_by_text("fixture-static", exact=True).count() > 0)
        check("current time zone still Europe/Berlin after preview", page.get_by_text("Europe/Berlin", exact=True).count() > 0)
        refused = context.request.post(
            url + "/api/v1/host/settings/preview",
            data='{"static_hostname":"nas-01"}',
            headers={"Content-Type": "application/json", "Origin": origin},
        )
        body = {}
        try:
            body = refused.json()
        except Exception:
            body = {}
        check(
            "cookie-only preview refused (CSRF token required)",
            refused.status == 403 and body.get("error") == "csrf_rejected",
        )
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
        limited_host = []
        limited_ok = []
        lp.on("request", capture_host(limited_host))
        lp.on(
            "response",
            lambda resp: limited_ok.append(resp.url)
            if is_host_api(resp.url) and path_of(resp.url).endswith("/host/settings/preview") and resp.status == 200
            else None,
        )
        lp.goto(url + "/host-settings")
        lp.get_by_label("Username", exact=True).fill(config["limited_username"])
        lp.get_by_label("Password", exact=True).fill(password)
        lp.get_by_role("button", name="Sign in", exact=True).click()
        lp.get_by_role("heading", name="Host settings", exact=True).wait_for()
        lp.get_by_text("fixture-static", exact=True).wait_for()
        check("limited account still sees current settings", lp.get_by_text("fixture-static", exact=True).is_visible())
        check(
            "limited account is told host.settings.preview is required",
            lp.get_by_text("host.settings.preview", exact=False).count() > 0,
        )
        check("limited account has no preview button", lp.get_by_role("button", name="Preview (does not apply)").count() == 0)
        check("limited session has no preview POST 200", not limited_ok)
        check(
            "limited session never posts plan or apply",
            not any(path_of(r["url"]).endswith(PLAN_APPLY) for r in limited_host),
        )
        check("no page script errors", not errors)
        limited.close()
        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(
        json.dumps(
            {
                "result": "pass",
                "checks": checks,
                "source": "Labelled synthetic hostsettings testdata only; preview-only; not live hostnamectl/timedatectl; not M2 close.",
            },
            indent=2,
        )
    )
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
