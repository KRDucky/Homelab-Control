#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Capture real browser screenshots from a disposable, unprivileged hc-api."""
import os
from pathlib import Path
from playwright.sync_api import sync_playwright

base = os.environ["HC_SCREENSHOT_URL"].rstrip("/")
password = os.environ["HC_SCREENSHOT_PASSWORD"]
out = Path("docs/screenshots")
out.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context(viewport={"width": 1440, "height": 960}, device_scale_factor=1)
    page = context.new_page()
    page.goto(base + "/", wait_until="domcontentloaded")
    page.get_by_label("Username", exact=True).fill("screenshots")
    page.get_by_label("Password", exact=True).fill(password)
    page.get_by_role("button", name="Sign in", exact=True).click()
    page.locator("nav#main-nav").wait_for(timeout=30000)
    page.get_by_text("Storage & files", exact=True).wait_for(timeout=30000)
    nav = page.locator("nav#main-nav").inner_text()
    assert "webmin" not in nav.lower(), "Legacy Webmin appears in navigation"
    assert "Storage & files" in nav and "Network & applications" in nav
    page.screenshot(path=str(out / "overview-desktop.png"), full_page=True)

    pages = [
        ("/storage", "storage-desktop.png"),
        ("/services", "services-desktop.png"),
        ("/packages", "software-desktop.png"),
        ("/ports", "network-ports-desktop.png"),
    ]
    for url, name in pages:
        page.goto(base + url, wait_until="domcontentloaded")
        page.locator("nav#main-nav").wait_for()
        page.wait_for_timeout(1500)
        assert "webmin" not in page.locator("nav#main-nav").inner_text().lower()
        page.screenshot(path=str(out / name), full_page=True)

    mobile = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=1)
    phone = mobile.new_page()
    # Independent browser session: use credentials rather than transplant session tokens.
    phone.goto(base + "/", wait_until="domcontentloaded")
    phone.get_by_label("Username", exact=True).fill("screenshots")
    phone.get_by_label("Password", exact=True).fill(password)
    phone.get_by_role("button", name="Sign in", exact=True).click()
    phone.get_by_role("button", name="Open navigation").click()
    phone.locator("nav#main-nav").wait_for()
    assert "webmin" not in phone.locator("nav#main-nav").inner_text().lower()
    phone.screenshot(path=str(out / "navigation-mobile.png"), full_page=True)
    mobile.close()
    context.close()
    browser.close()
print("Captured six real browser PNGs from the current source (disposable CI runner).")
