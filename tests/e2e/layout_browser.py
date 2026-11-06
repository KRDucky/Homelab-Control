#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""M2-5 Stage A: persisted drive-bay layout through the actual API and UI.

Drives the opt-in TestStorageBrowserFixture (real HTTP, auth, sessions and a
private SQLite state database; storage sources are the labelled synthetic
fixture only). No request is mocked. Flow: create a tiny chassis, assign the
one synthetic stable drive, reload and verify the stored layout and its
manual/unverified provenance, unassign, reload. One real server refusal checks
that an error is never shown as a saved change. Runtime credentials/state MUST
NOT be copied into evidence or Git.

Usage: layout_browser.py RUNTIME_DIR EVIDENCE_DIR
"""
import json
import os
from pathlib import Path
import sys
import time
from playwright.sync_api import sync_playwright

runtime = Path(sys.argv[1])
evidence = Path(sys.argv[2])
checks = []
password = ""
CHASSIS = "Example chassis (synthetic)"


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
        page.get_by_label("Username", exact=True).fill(config["layout_username"])
        page.get_by_label("Password", exact=True).fill(password)
        page.get_by_role("button", name="Sign in", exact=True).click()
        page.get_by_role("button", name="Inspect EXAMPLE-SERIAL-A", exact=True).wait_for()
        check("synthetic data explicitly labelled", page.get_by_role("list", name="Inventory limitations").get_by_text("Example data — isolated browser fixture, not live storage or acceptance evidence.", exact=True).is_visible())

        csrf = context.request.get(url + "/api/v1/auth/session").json()["csrf_token"]
        refused = context.request.get(url + "/api/v1/storage/layout")
        check("cookie-only layout read refused (CSRF token required)", refused.status == 403 and refused.json().get("error") == "csrf_rejected")

        def stored():
            r = context.request.get(url + "/api/v1/storage/layout", headers={"X-CSRF-Token": csrf})
            if r.status != 200:
                raise AssertionError(f"layout read {r.status}")
            return r.json()["chassis"]

        def bay(col, state):
            return page.get_by_role("button", name=f"{CHASSIS} → Row 1 → Column {col}: {state}", exact=True)

        form = page.get_by_role("form", name="Add chassis")
        form.get_by_label("Name", exact=True).fill(CHASSIS)
        form.get_by_label("Rows", exact=True).fill("1")
        form.get_by_label("Columns", exact=True).fill("2")
        form.get_by_role("button", name="Add chassis", exact=True).click()
        page.get_by_text(f"Added {CHASSIS}.", exact=True).wait_for()
        check("chassis created through UI and stored", [c["name"] for c in stored()] == [CHASSIS])

        bay(1, "No drive assigned").click()
        # Substring match: the nested <select>'s accessible name also includes its selected option text.
        drive = page.get_by_label("Drive to assign")
        option = drive.locator("option", has_text="EXAMPLE-SERIAL-A")
        drive.select_option(value=option.get_attribute("value"))
        page.get_by_role("button", name="Assign drive", exact=True).click()
        page.get_by_text("Drive assigned (manual, unverified).", exact=True).wait_for()

        page.reload()
        bay(1, "Present").wait_for()
        check("assignment survives reload", bay(1, "Present").is_visible())
        a = stored()[0]["cells"][0]["assignment"]
        check("stored identity is persistent, not a kernel name", a["identity"].startswith(("wwn:", "serial:")) and "sda" not in a["identity"])
        check("stored provenance manual/unverified by bays", a["provenance"] == "manual" and a["confidence"] == "unverified" and a["assigned_by"] == config["layout_username"])
        bay(1, "Present").click()
        check("UI shows manual, unverified provenance", page.get_by_text(f"Manual, unverified — stated by {config['layout_username']}", exact=False).is_visible())
        page.screenshot(path=str(evidence / "assigned-after-reload.png"), full_page=True)

        # A real server refusal (identity already assigned elsewhere) must
        # surface as an error, never as a saved change.
        bay(2, "No drive assigned").click()
        noop = page.get_by_label("Drive to assign")
        check("assigned drive no longer offered", noop.locator("option", has_text="EXAMPLE-SERIAL-A").count() == 0)
        forged = context.request.put(
            url + f"/api/v1/storage/layout/chassis/{stored()[0]['id']}/bays/0/1/assignment",
            headers={"X-CSRF-Token": csrf, "Origin": url, "Content-Type": "application/json"},
            data=json.dumps({"persistent_id": a["identity"]}),
        )
        check("actual API refuses a second bay for one identity", forged.status == 409)
        check("refused change not persisted", stored()[0]["cells"][1].get("assignment") is None)

        bay(1, "Present").click()
        page.get_by_role("button", name="Unassign drive", exact=True).click()
        page.get_by_text("Assignment removed.", exact=True).wait_for()
        page.reload()
        bay(1, "No drive assigned").wait_for()
        check("unassignment survives reload", all(c.get("assignment") is None for c in stored()[0]["cells"]))

        # UI error path: delete the chassis server-side, then act on the stale view.
        bay(1, "No drive assigned").click()
        drive = page.get_by_label("Drive to assign")
        drive.select_option(value=drive.locator("option", has_text="EXAMPLE-SERIAL-A").get_attribute("value"))
        gone = context.request.delete(url + f"/api/v1/storage/layout/chassis/{stored()[0]['id']}", headers={"X-CSRF-Token": csrf, "Origin": url})
        check("chassis deleted via actual API", gone.status == 200 and stored() == [])
        page.get_by_role("button", name="Assign drive", exact=True).click()
        page.get_by_text("No such chassis, bay or assignment.", exact=True).wait_for()
        check("UI shows server error, not success", page.get_by_text("Drive assigned (manual, unverified).", exact=True).count() == 0)
        check("nothing persisted by the failed UI action", stored() == [])
        page.screenshot(path=str(evidence / "error-not-success.png"), full_page=True)
        check("no page script errors", not errors)
        context.close()
        browser.close()
    (evidence / "RESULTS.json").write_text(json.dumps({"result": "pass", "checks": checks, "source": "Actual API/session/SQLite; labelled synthetic storage fixture only. No host, root, native-probe or KVM access."}, indent=2))
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
