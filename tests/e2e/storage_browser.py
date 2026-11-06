#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Actual API/session/state browser test with explicitly synthetic storage data.

Run TestStorageBrowserFixture with a private runtime directory first. No browser
request is mocked; neither process reads host storage or invokes privileged ops.
Runtime credentials/state MUST NOT be copied into evidence or Git.
"""
import json
import os
from pathlib import Path
import sys
import time
from playwright.sync_api import sync_playwright

runtime = Path(sys.argv[1])
evidence = Path(sys.argv[2])
axe = Path(sys.argv[3])
checks = []
password = ""


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    checks.append(name)
    print("PASS " + name, flush=True)


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
    with sync_playwright() as pw:
        browser = pw.chromium.launch(executable_path=os.environ.get("HC_CHROMIUM", "/usr/bin/chromium"), headless=True)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(url + "/storage")
        page.get_by_label("Username", exact=True).fill(config["username"])
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.get_by_role("button", name="Inspect EXAMPLE-SERIAL-A", exact=True).wait_for()
        check("real login and storage endpoint", page.get_by_role("heading", name="Storage", exact=True).is_visible())
        check("synthetic data explicitly labelled", page.get_by_role("list", name="Inventory limitations").get_by_text("Example data — isolated browser fixture, not live storage or acceptance evidence.", exact=True).is_visible())
        page.get_by_label("Identify drives by", exact=True).focus()
        page.keyboard.press("Tab")
        check("keyboard access to resource filter", page.get_by_label("Find a resource", exact=True).evaluate("element => element === document.activeElement"))
        page.get_by_label("Identify drives by", exact=True).select_option("wwn")
        page.get_by_role("button", name="Inspect naa.5000000000000a01", exact=True).click()
        relations = page.get_by_role("table", name="Relationships of selected resource", exact=True)
        check("independent relationship table", relations.get_by_text("contains", exact=True).first.is_visible())
        check("physical location remains unknown", page.get_by_text("Unknown — not verified by this inventory", exact=True).is_visible())
        lvm = page.get_by_role("table", name="Observed LVM logical volumes and segments", exact=True)
        check("RAID segment observed from parsed report fixture", lvm.get_by_text("raid1", exact=True).is_visible())
        check("SSD cache mode and policy displayed", lvm.get_by_text("writeback / smq", exact=True).is_visible())
        check("dirty cache blocks displayed", lvm.get_by_text("17", exact=True).is_visible())
        pvs = page.get_by_role("table", name="Observed LVM physical volumes", exact=True)
        check("PV allocation joined by LV UUID", pvs.get_by_text("raid", exact=True).is_visible())
        check("PV device number shown", pvs.get_by_text("8:1", exact=True).is_visible())
        check("PV disk link labelled provisional, not verified", pvs.get_by_text("Provisional, not verified — no on-disk signature observed and no revalidation", exact=True).is_visible())
        page.screenshot(path=str(evidence / "desktop.png"), full_page=True)
        page.evaluate(axe.read_text() + "\n;0")
        violations = page.evaluate("async () => (await axe.run(document, {runOnly: {type:'tag', values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v => ({id:v.id,impact:v.impact,targets:v.nodes.map(n=>n.target)}))")
        (evidence / "axe.json").write_text(json.dumps(violations, indent=2))
        check("desktop accessibility", not violations)
        page.get_by_role("button", name="Refresh inventory", exact=True).click()
        page.get_by_role("heading", name="Selected resource", exact=True).wait_for(state="detached")
        check("fresh snapshot clears snapshot-local selection", True)
        page.reload()
        page.get_by_role("button", name="Inspect naa.5000000000000a01", exact=True).wait_for()
        check("WWN preference survives reload", page.get_by_label("Identify drives by", exact=True).input_value() == "wwn")
        refused = context.request.get(url + "/api/v1/storage")
        check("cookie-only storage read refused (CSRF token required)", refused.status == 403 and refused.json().get("error") == "csrf_rejected")
        csrf = context.request.get(url + "/api/v1/auth/session").json()["csrf_token"]
        response = context.request.get(url + "/api/v1/storage", headers={"X-CSRF-Token": csrf})
        body = response.json()
        check("actual API persists observation", response.status == 200 and body["snapshot_persisted"] and bool(body.get("snapshot_id")))
        page.set_viewport_size({"width":390,"height":844})
        page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
        page.screenshot(path=str(evidence / "mobile.png"), full_page=True)
        overflow = page.evaluate("() => [...document.querySelectorAll('body *')].map(e=>({tag:e.tagName,cls:typeof e.className==='string'?e.className:'',right:e.getBoundingClientRect().right,width:e.getBoundingClientRect().width})).filter(e=>e.right>innerWidth+1).slice(0,30)")
        (evidence / "overflow.json").write_text(json.dumps(overflow, indent=2))
        check("mobile layout has no document overflow", page.evaluate("document.documentElement.scrollWidth <= innerWidth + 1"))
        page.evaluate(axe.read_text() + "\n;0")
        mobile = page.evaluate("async () => (await axe.run(document, {runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v=>({id:v.id,impact:v.impact,targets:v.nodes.map(n=>n.target)}))")
        (evidence / "axe-mobile.json").write_text(json.dumps(mobile, indent=2))
        check("mobile accessibility", not mobile)
        page.get_by_role("button", name="Sign out", exact=True).click()
        page.get_by_label("Username", exact=True).wait_for()
        check("sign out revokes storage access", context.request.get(url + "/api/v1/storage").status == 401)
        # Separate real login with no storage.read permission, no cookie reuse.
        limited = browser.new_context(viewport={"width":1280,"height":900})
        lp = limited.new_page()
        lp.goto(url + "/storage")
        lp.get_by_label("Username", exact=True).fill(config["limited_username"])
        lp.get_by_label("Password", exact=True).fill(password)
        lp.get_by_role("button", name="Sign in", exact=True).click()
        lp.get_by_role("status").filter(has_text="Your account does not have the storage.read permission.").wait_for()
        check("missing scope refused by actual API", limited.request.get(url + "/api/v1/storage").status == 403)
        check("restricted browser never receives drive controls", lp.get_by_role("button", name="Inspect naa.5000000000000a01", exact=True).count() == 0)
        check("no page script errors", not errors)
        limited.close()
        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(json.dumps({"result":"pass","checks":checks,"source":"Actual API/session/database; labelled synthetic kernel/LVM fixture only. No native-probe or KVM acceptance."},indent=2))
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
