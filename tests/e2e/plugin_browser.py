#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Read-only sample plugin load/unload through the Appearance popover + axe.

Drives the opt-in TestStorageBrowserFixture (real HTTP, auth, sessions; storage
sources are the labelled synthetic fixture only). Plugin enable/disable is
browser-local (`hc.plugins.v1`); no request is mocked and no host operation is
run. Flow: sign in, confirm the Overview page shows no sample card, enable
"Read-only sample plugin" in the Appearance popover, confirm the labelled
Overview card appears, run desktop + mobile axe **while the card is loaded**,
disable the plugin, confirm the card is gone, and confirm Theme Studio
(`/appearance/themes`) import/export is unchanged.

Accessibility: the whole-document run fails on **violations only**, matching the
sibling fixtures; incomplete (skipped) results are persisted to
`axe-incomplete.json` / `axe-mobile-incomplete.json` and their ids printed, so
known chrome incompletes do not fail the run. The sample-card subtree itself is
scoped separately and fails closed on violations **or** incomplete (the card is
the only UI this checkpoint owns). Empty axe JSON on pass. Not M3 acceptance,
not production.

Usage: plugin_browser.py RUNTIME_DIR EVIDENCE_DIR AXE_MIN_JS
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
CARD_SCOPE = "document.getElementById('sample-plugin-card-title').closest('section')"


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


def run_axe(page, scope_js):
    page.evaluate(axe.read_text() + "\n;0")
    raw = page.evaluate(f"async () => await axe.run({scope_js}, {AXE_OPTIONS})")
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

        page.goto(url + "/")
        page.get_by_label("Username", exact=True).fill(username)
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.locator(".faceplate-host").wait_for()
        check("overview page rendered", page.locator(".faceplate-host").is_visible())
        check("sample card hidden before load", page.get_by_role("heading", name="Read-only sample plugin", exact=True).count() == 0)

        # Load: enable the sample plugin in the Appearance popover, then close it.
        page.get_by_role("button", name="Appearance", exact=True).click()
        page.get_by_role("checkbox", name="Read-only sample plugin").check()
        page.keyboard.press("Escape")
        check("sample card visible after load", page.get_by_role("heading", name="Read-only sample plugin", exact=True).is_visible())
        check("sample card is labelled example only", page.get_by_text("Sample plugin · example only", exact=True).is_visible())

        # Desktop axe while the sample card is loaded.
        page.screenshot(path=str(evidence / "desktop.png"), full_page=True)
        desktop = run_axe(page, "document")
        (evidence / "axe.json").write_text(json.dumps(desktop["violations"], indent=2))
        (evidence / "axe-incomplete.json").write_text(json.dumps(desktop["incomplete"], indent=2))
        report_incomplete("desktop", desktop["incomplete"])
        check("desktop accessibility", not desktop["violations"])

        # The sample-card subtree is the UI this checkpoint owns: fail closed on it.
        card = run_axe(page, CARD_SCOPE)
        (evidence / "axe-card.json").write_text(json.dumps(card, indent=2))
        check("sample card accessibility", not card["violations"] and not card["incomplete"])

        # Mobile axe while still loaded.
        page.set_viewport_size({"width": 390, "height": 844})
        page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
        page.screenshot(path=str(evidence / "mobile.png"), full_page=True)
        mobile = run_axe(page, "document")
        (evidence / "axe-mobile.json").write_text(json.dumps(mobile["violations"], indent=2))
        (evidence / "axe-mobile-incomplete.json").write_text(json.dumps(mobile["incomplete"], indent=2))
        report_incomplete("mobile", mobile["incomplete"])
        check("mobile accessibility", not mobile["violations"])

        # Unload: disable it; the card must disappear.
        page.get_by_role("button", name="Appearance", exact=True).click()
        page.get_by_role("checkbox", name="Read-only sample plugin").uncheck()
        page.keyboard.press("Escape")
        check("sample card hidden after unload", page.get_by_role("heading", name="Read-only sample plugin", exact=True).count() == 0)

        # Theme Studio is unchanged by the plugin half.
        page.goto(url + "/appearance/themes")
        page.get_by_role("heading", name="Theme library", exact=True).wait_for()
        check("Theme Studio import still offered", page.locator("input[type='file'][accept*='.hltheme']").count() == 1)
        check("Theme Studio export still offered", page.get_by_role("button", name="Export .hltheme", exact=True).is_visible())
        check("no page script errors", not errors)

        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(
        json.dumps(
            {
                "result": "pass",
                "checks": checks,
                "source": "Opt-in TestStorageBrowserFixture only; browser-local sample plugin; not M3 acceptance, not production.",
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
