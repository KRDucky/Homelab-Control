#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Read-only Software Center through the actual UI + axe.

Drives the opt-in TestSoftwareBrowserFixture (real HTTP, auth, sessions; package
data is the labelled synthetic table only). No package manager is spawned, no
host is read and no request is mocked. Flow: sign in, open the Software page,
confirm the four read-only tabs, run a search, check the installed/updates/sources
views (including third-party/unsigned flags and the metadata-refresh label),
assert no Install/Remove/Update/Add control exists, and run desktop + mobile axe.

Accessibility fails on violations only (incomplete results are persisted to
axe-incomplete.json / axe-mobile-incomplete.json and their ids printed). Empty
axe JSON on pass. Not M4 acceptance, not production.

Usage: software_browser.py RUNTIME_DIR EVIDENCE_DIR AXE_MIN_JS
"""
import json
import os
import sys
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

runtime = Path(sys.argv[1])
evidence = Path(sys.argv[2])
axe = Path(sys.argv[3])
checks = []
password = ""

AXE_OPTIONS = "{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}"


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    checks.append(name)
    print("PASS " + name, flush=True)


def fmt_axe(raw):
    return {
        "violations": [
            {"id": v["id"], "impact": v["impact"], "targets": [n["target"] for n in v["nodes"]]}
            for v in raw["violations"]
        ],
        "incomplete": [
            {"id": v["id"], "impact": v["impact"], "targets": [n["target"] for n in v["nodes"]]}
            for v in raw["incomplete"]
        ],
    }


def run_axe(page):
    page.evaluate(axe.read_text() + "\n;0")
    raw = page.evaluate(f"async () => await axe.run(document, {AXE_OPTIONS})")
    return fmt_axe(raw)


def report_incomplete(label, incomplete):
    ids = ", ".join(sorted({i["id"] for i in incomplete})) or "none"
    print(f"INCOMPLETE {label} {ids}", flush=True)


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
    username = config.get("username") or "browser"
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"), headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))

        page.goto(url + "/software")
        page.get_by_label("Username", exact=True).fill(username)
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.get_by_role("heading", name="Software", exact=True).wait_for()
        check("software page rendered", page.get_by_role("heading", name="Software", exact=True).is_visible())
        check("read-only label shown", page.get_by_text("Read-only preview", exact=False).is_visible())

        for label in ["Search", "Installed", "Updates", "Sources"]:
            check(f"{label} tab present", page.get_by_role("tab", name=label, exact=True).is_visible())
        for forbidden in ["Install", "Remove", "Update", "Add"]:
            check(f"no {forbidden} control", page.get_by_role("button", name=forbidden, exact=True).count() == 0)

        # Search.
        page.get_by_label("Search packages").fill("nginx")
        page.get_by_role("button", name="Search", exact=True).click()
        check("search result shown", page.get_by_text("nginx", exact=True).count() > 0)

        # Installed.
        page.get_by_role("tab", name="Installed", exact=True).click()
        check("installed package shown", page.get_by_text("nginx", exact=True).count() > 0)
        check("installed flag shown", page.get_by_text("installed", exact=True).count() > 0)

        # Updates.
        page.get_by_role("tab", name="Updates", exact=True).click()
        check("update preview labelled with refresh time", page.get_by_text("As of last metadata refresh", exact=False).is_visible())

        # Sources.
        page.get_by_role("tab", name="Sources", exact=True).click()
        check("third-party source flagged", page.get_by_text("third-party script source", exact=True).is_visible())
        check("unsigned source flagged", page.get_by_text("unsigned", exact=True).is_visible())

        # Axe on the Software page (sources tab).
        page.screenshot(path=str(evidence / "desktop.png"), full_page=True)
        desktop = run_axe(page)
        (evidence / "axe.json").write_text(json.dumps(desktop["violations"], indent=2))
        (evidence / "axe-incomplete.json").write_text(json.dumps(desktop["incomplete"], indent=2))
        report_incomplete("desktop", desktop["incomplete"])
        check("desktop accessibility", not desktop["violations"])

        page.set_viewport_size({"width": 390, "height": 844})
        page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
        page.screenshot(path=str(evidence / "mobile.png"), full_page=True)
        mobile = run_axe(page)
        (evidence / "axe-mobile.json").write_text(json.dumps(mobile["violations"], indent=2))
        (evidence / "axe-mobile-incomplete.json").write_text(json.dumps(mobile["incomplete"], indent=2))
        report_incomplete("mobile", mobile["incomplete"])
        check("mobile accessibility", not mobile["violations"])
        check("no page script errors", not errors)

        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(
        json.dumps(
            {
                "result": "pass",
                "checks": checks,
                "source": "Opt-in TestSoftwareBrowserFixture only; read-only Software Center; no package manager spawned; not M4 acceptance.",
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
