#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Capture the README screenshots from a running Homelab Control instance.

Run it from a workstation against a disposable test VM (never production):

    HC_SCREENSHOT_URL=https://127.0.0.1:9443 HC_SCREENSHOT_USER=demo \\
    HC_SCREENSHOT_PASSWORD_FILE=/path/to/pw HC_SCREENSHOT_OUT=docs/screenshots \\
    python3 scripts/capture-readme-ui.py

It signs in with a temporary read-only test account, visits each module page,
waits for API responses, fonts and loading states to settle, and fails if any
rendered page, link or image mentions Webmin, if a page shows an error overlay,
or if the browser console logs an error. It prints a JSON report on stdout.
The certificate is not verified (the test VM uses a throwaway self-signed one).
Text checks compare case-insensitively: CSS text-transform changes innerText.
"""
import json
import os
import re
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ["HC_SCREENSHOT_URL"].rstrip("/")
USER = os.environ.get("HC_SCREENSHOT_USER", "demo")
PASSWORD = Path(os.environ["HC_SCREENSHOT_PASSWORD_FILE"]).read_text().strip()
OUT = Path(os.environ.get("HC_SCREENSHOT_OUT", "docs/screenshots"))
DESKTOP = {"width": 1440, "height": 900}
MOBILE = {"width": 390, "height": 844}
WEBMIN = re.compile(r"webmin|miniserv", re.I)

# (file stem, path)
PAGES = [
    ("overview", "/"),
    ("services", "/services"),
    ("hardware", "/hardware"),
    ("storage", "/storage"),
    ("software", "/software"),
    ("files", "/files"),
    ("zfs", "/storage/zfs"),
    ("btrfs", "/storage/btrfs"),
    ("lvm", "/storage/lvm"),
    ("mergerfs", "/storage/mergerfs"),
    ("network-ports", "/network/ports"),
    ("network-bonds", "/network/bonds"),
    ("network-firewall", "/network/firewall"),
    ("traefik", "/networking/reverse-proxy/traefik"),
    ("theme-studio", "/appearance/themes"),
]

report = {"base": BASE, "pages": {}, "console_errors": [], "webmin_hits": [], "files": []}


def prefs(theme, sidebar="expanded"):
    return json.dumps({"schemaVersion": 1, "theme": theme, "sidebar": sidebar})


def settle(page):
    page.wait_for_load_state("networkidle")
    page.evaluate("document.fonts.ready")
    # No loading text or busy region may remain before a capture.
    page.wait_for_function(
        "() => !document.body.innerText.includes('Loading…') && !document.querySelector('[aria-busy=\"true\"]')",
        timeout=30000,
    )
    page.wait_for_timeout(600)


def check_page(page, name):
    text = page.evaluate("document.body.innerText")
    refs = page.evaluate(
        "() => [...document.querySelectorAll('[href],[src],[action]')]"
        ".map(e => e.getAttribute('href') || e.getAttribute('src') || e.getAttribute('action'))"
    )
    for hit in [t for t in [text, *refs] if t and WEBMIN.search(t)]:
        report["webmin_hits"].append({"page": name, "text": WEBMIN.search(hit).group(0)})
    if page.locator(".error-boundary").count():
        report["pages"].setdefault(name, {})["error_overlay"] = True
    return text


def sign_in(context):
    page = context.new_page()
    state = {"signed_in": False}
    # Before sign-in the app's session check answers 401 by design; only errors
    # after sign-in count.
    page.on(
        "console",
        lambda m: m.type == "error"
        and state["signed_in"]
        and report["console_errors"].append({"page": page.url, "text": m.text[:300]}),
    )
    page.goto(BASE + "/", wait_until="domcontentloaded")
    page.get_by_label("Username", exact=True).fill(USER)
    page.get_by_label("Password", exact=True).fill(PASSWORD)
    page.get_by_role("button", name="Sign in", exact=True).click()
    try:
        # The sidebar is a hidden drawer on phones, so wait for it to exist, not to show.
        page.wait_for_function("() => !document.title.startsWith('Sign in')", timeout=60000)
        page.locator("nav#main-nav").wait_for(state="attached", timeout=30000)
    except Exception:
        page.screenshot(path=str(OUT / "_signin-failure.png"))
        raise
    state["signed_in"] = True
    return page


def shot(page, stem, full_page=False):
    path = OUT / f"{stem}.png"
    page.screenshot(path=str(path), full_page=full_page, animations="disabled", caret="hide")
    report["files"].append(path.name)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)

        # Desktop, Midnight theme: every module page.
        ctx = browser.new_context(viewport=DESKTOP, device_scale_factor=1, ignore_https_errors=True, color_scheme="dark")
        ctx.add_init_script(f"localStorage.setItem('hc.preferences.v1', {json.dumps(prefs('midnight'))})")
        page = sign_in(ctx)
        nav = page.locator("nav#main-nav")
        for stem, path in PAGES:
            page.goto(BASE + path, wait_until="domcontentloaded")
            nav.wait_for(state="attached")
            settle(page)
            if stem == "storage" and page.get_by_role("button", name="Probe storage").count():
                # SMART, signatures and LVM come from the on-demand read-only probe.
                page.get_by_role("button", name="Probe storage").click()
                page.get_by_text(re.compile("Probe complete")).wait_for(timeout=90000)
                settle(page)
                # Show the drive table (identity, size, use, SMART health), not just the notes.
                # HC_SCREENSHOT_STORAGE_FILTER uses the page's own "Find a resource" box (the
                # test VM's disks share a serial prefix); the caption must say it is filtered.
                if os.environ.get("HC_SCREENSHOT_STORAGE_FILTER"):
                    page.get_by_label("Find a resource").fill(os.environ["HC_SCREENSHOT_STORAGE_FILTER"])
                    page.wait_for_timeout(400)
                page.get_by_role("table", name="Observed storage resources").scroll_into_view_if_needed()
                page.evaluate("window.scrollBy(0, -140)")
                page.wait_for_timeout(300)
            if stem == "software":
                # The installed-package list shows real data; the default Search tab starts empty.
                page.get_by_role("tab", name="Installed").click()
                settle(page)
            text = check_page(page, stem)
            report["pages"][stem] = {"url": path, "title": page.title(), "chars": len(text)}
            shot(page, f"{stem}-desktop")
        # The full sidebar with every module group.
        page.goto(BASE + "/", wait_until="domcontentloaded")
        settle(page)
        report["nav_groups"] = nav.locator("h2.nav-group-title").all_text_contents()
        report["nav_entries"] = [t.strip() for t in nav.locator("a").all_text_contents()]
        nav.screenshot(path=str(OUT / "navigation-desktop.png"), animations="disabled")
        report["files"].append("navigation-desktop.png")
        ctx.close()

        # Daybreak theme: overview and storage.
        ctx = browser.new_context(viewport=DESKTOP, device_scale_factor=1, ignore_https_errors=True, color_scheme="light")
        ctx.add_init_script(f"localStorage.setItem('hc.preferences.v1', {json.dumps(prefs('daybreak'))})")
        page = sign_in(ctx)
        for stem, path in (("theme-daybreak", "/"), ("storage-daybreak", "/storage")):
            page.goto(BASE + path, wait_until="domcontentloaded")
            settle(page)
            check_page(page, stem)
            shot(page, stem)
        ctx.close()

        # Mobile, Midnight: overview and the open navigation drawer.
        ctx = browser.new_context(
            viewport=MOBILE, device_scale_factor=2, ignore_https_errors=True, color_scheme="dark", is_mobile=True, has_touch=True
        )
        ctx.add_init_script(f"localStorage.setItem('hc.preferences.v1', {json.dumps(prefs('midnight'))})")
        page = sign_in(ctx)
        settle(page)
        check_page(page, "overview-mobile")
        shot(page, "overview-mobile")
        page.get_by_role("button", name="Open navigation").click()
        page.wait_for_timeout(500)
        check_page(page, "navigation-mobile")
        shot(page, "navigation-mobile")
        ctx.close()
        browser.close()

    report["ok"] = not report["webmin_hits"] and not report["console_errors"] and not any(
        p.get("error_overlay") for p in report["pages"].values()
    )
    print(json.dumps(report, indent=1))
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
