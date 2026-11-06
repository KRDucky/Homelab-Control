<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# Managing systemd services

Homelab Control can **start**, **stop** and **restart** systemd `.service` units from the Services
page. Every operation is reviewed before it runs, confirmed by you, carried out by the privileged
helper (`hc-broker`), recorded, and reported with the state systemd shows afterwards.

On the M2-3 development branch (not yet accepted by the owner) it can also **enable or disable a
service at boot**, and shows **timers**, **sockets** and **unit details** read-only; see
[Boot-time enable/disable and unit details](#boot-time-enabledisable-and-unit-details-m2-3) below.

Not available: masking, editing or creating unit files, `daemon-reload`, starting or stopping other
unit types (sockets, timers, mounts…), and host shutdown or reboot.

## Installed units and runtime state

The Services inventory combines systemd's runtime list with its installed unit-file list.
Installed units absent from the runtime list appear as **not-loaded**, with **unknown** active
and substate and their reported enablement state. This includes aliases and template files;
their presence does not prove they are running, inactive, or usable as service instances.
An alias is listed under its own name even when the unit it points to is running, so for example a
`dbus-org.freedesktop.*` alias shows **not-loaded**/**unknown** next to its running target.
Runtime observations take precedence when a unit appears in both lists. Inventory does not load
units or query every unit individually. Installed-only entries have no Start/Stop/Restart or Logs
buttons (logs are read only for units systemd currently lists); existing operation authorization and
protected-unit checks remain unchanged. If the list would exceed 5,000 units, runtime units are kept
before installed-only entries.
If unit-file listing fails, the runtime list is still shown, but installed-only units are then
**omitted** and the “At boot” column shows “—”: the list is not complete in that case.

## Who can use it

The permission is the scope **`services.operate`**. It is never granted by default, including on
upgrade. An administrator grants it on the server:

```sh
sudo hlctl user scopes --file /etc/homelab-control/users.json --username alice \
  --scopes system.read,services.read,services.logs,services.operate,audit.read
sudo systemctl restart homelab-control.service
```

Sign in again afterwards: a session keeps the scopes it had at sign-in, minus any that were removed
since. Removing `services.operate` takes effect on existing sessions at their next request.

Without the scope the Services page stays read-only and says why. The server refuses the operation
too; hiding the buttons is only a convenience.

## The flow

1. **Choose an action.** Running units offer *Restart* and *Stop*; stopped or failed units offer
   *Start*. Units that are changing state offer nothing until they settle.
2. **Review.** The dialog says *Preview · nothing has been changed yet* and shows:
   - the canonical unit (if you clicked an alias, the real unit is shown);
   - its current state;
   - the risk class: **Reversible** (start) or **Disruptive** (stop, restart, or a start that stops
     conflicting units);
   - what will happen, other units systemd stops, restarts or starts with it, and the note that
     there is no automatic rollback.
   The review is valid for **2 minutes** and can be used **once**.
3. **Confirm.** Disruptive operations ask for your **password** again. A correct password is
   remembered for **5 minutes** for this browser session only; signing out, revoking the session or
   restarting Homelab Control forgets it. Failed attempts count towards the sign-in limit.
4. **Result.** The helper runs `systemctl`, then reads the unit state back:
   - **Done** — systemd reports the state you asked for (for example `active (running)`);
   - **Failed** — `systemctl` failed, or the service failed after systemd accepted the request;
   - **Not verified** — the result could not be confirmed (for example the service is still
     starting); the observed state is shown as it is.
   On failure the dialog shows what to do next, including *Show logs*. The operation ID links to the
   Audit page.

## Why an operation can be refused

| Message | Meaning |
|---|---|
| *… is protected* | The built-in policy covers this unit (see below). |
| *would also stop or restart …, which is protected* | A unit that depends on this one, directly or further down the chain, is protected. |
| *could pull in … (shutdown, reboot or recovery mode)* | Starting it could reach a shutdown, reboot, rescue or emergency unit. |
| *sets FailureAction=… / SuccessAction=…* | The unit (or one stopped or started with it) can reboot or power off the host. |
| *The unit changed since you reviewed it* | Its state, unit file, aliases or affected units changed. Nothing was done; review again. |
| *This review is no longer valid* | It expired, was already used, or was made in another session. Nothing was done. |
| *Another operation on this unit is in progress* | Only one operation per unit runs at a time. |
| *Re-enter your password* | The 5-minute confirmation lapsed. |
| *The result is unknown* | The helper's answer was lost. The operation may or may not have run: check the unit's state and *Privileged operations* on the Audit page. |

### Protected units

A fixed policy, enforced by the privileged helper, protects units whose interruption could lock you
out or harm the host: Homelab Control itself, SSH, D-Bus and polkit, core `systemd-*` units, console
logins (getty), user sessions, network managers and network configuration (including Arch `netctl`
and Open vSwitch), firewalls, VPNs and remote-access tunnels (Tailscale, WireGuard, ZeroTier,
Cloudflare Tunnel, OpenVPN, strongSwan/IPsec, Nebula), display managers, storage infrastructure (ZFS,
LVM, md, multipath, iSCSI), login and identity services (SSSD, LDAP, Winbind), `auditd`, and power
and recovery units — whatever their unit type (`ssh.socket` counts as SSH). Units that systemd itself
marks `RefuseManualStart`/`RefuseManualStop` are refused as well.

The policy also applies through aliases, template instances (`getty@tty1.service`) and dependencies:
units stopped, restarted or started along with the one you chose are checked too.

The list is not exhaustive. A service that matters to you but is not on it — a reverse proxy in front
of Homelab Control, a container runtime, a hypervisor — can be stopped by anyone with
`services.operate`, so grant that permission only to people who should be able to.
It cannot be changed from the interface or a configuration file in this version. Manage these units
on the host when you need to (`sudo systemctl …`).

## Records

- **Audit page → Privileged operations:** every operation with its outcome, the user, the observed
  state and the operation ID; operations interrupted by a helper restart are marked *Interrupted*
  (they are never retried automatically). Needs `audit.read`.
- **Audit events:** `services.op.plan` and `services.op.apply` (requested, then the outcome or the
  refusal).
- **On the server:** the helper's journal `/var/log/homelab-control-broker/operations.jsonl`
  (root only). Check its integrity with
  `sudo hlctl audit verify --file /var/log/homelab-control-broker/operations.jsonl`.

Passwords, session cookies and review tokens are never written to any of these.

## If something goes wrong

There is **no automatic rollback**: starting, stopping and restarting are runtime actions. If a
service fails to start, read its logs on the Services page or run `systemctl status <unit>` and
`journalctl -u <unit>` on the host. If the result is *Not verified* or *Interrupted*, the Services
page and the operation journal show the state systemd reports now.

## Boot-time enable/disable and unit details (M2-3)

> **Status:** implemented on the M2-3 branch for owner review; not accepted, not released, and not to be
> enabled in a real deployment until the owner decides so.

### Boot-time setting vs. running state
*Enable at boot* and *Disable at boot* change **only** whether systemd starts the service when the host
boots. They never start or stop it now (Homelab Control never uses `--now`): use Start or Stop for
that. They also do **not** reload systemd (`--no-reload`). The setting is written to disk at once. systemd
uses it at the next boot, but also **earlier** if it reloads its configuration for another reason
(package installs and updates commonly do) or loads a target that wanted the service for the first
time; after that, starting such a target can start a newly enabled service (or still start a disabled
one until the reload). Until then `systemctl status` on the host may still show the old setting and
report that the unit file changed on disk. Homelab Control reads the setting from disk (`systemctl is-enabled`), so the Services
page shows the new value at once.

### Who can use it
The scope **`services.configure`**, never granted by default and not implied by `services.operate`:

```sh
sudo hlctl user scopes --file /etc/homelab-control/users.json --username alice \
  --scopes system.read,services.read,services.configure,audit.read
sudo systemctl restart homelab-control.service
```

### The flow
1. The Services page offers *Enable at boot* for units reported **disabled** and *Disable at boot* for
   units reported **enabled**. Static, indirect, generated, masked, runtime-only and template units
   get no button, and protected units (SSH, Homelab Control itself, D-Bus, networking, firewalls, …) are
   labelled instead.
2. **Review:** the dialog lists the links systemd will create or remove under `/etc/systemd/system`
   (the targets that want the service and its alias names), what the boot-time start would also start
   or stop, the risk class **Disruptive** and that there is no automatic rollback.
3. **Confirm with your password** (always required for boot-time changes; valid 5 minutes).
4. **Result:** verified by the privileged helper independently of `systemctl`'s own report: the
   setting read back from disk, every expected link checked (and where it points), and a fresh search
   for any other link to the service. If only some links changed, or other links appeared, the result
   is **Failed** and says what was observed; nothing is repaired automatically.

To undo a change, review the opposite action.

### What is refused, and why
The privileged helper refuses rather than guesses. Refused: protected units by any name (including
aliases and alias names in the unit's `[Install]` section) or when a protected unit wants it; units
that would conflict with a protected unit or pull in shutdown, reboot or recovery at boot; `Also=`,
`RequiredBy=`, `UpheldBy=` and `DefaultInstance=`; specifiers (`%n`, …), escapes, line continuations
or resets in `[Install]`; units generated from SysV init scripts or by generators; transient, linked,
masked and runtime-enabled units; template instances; unit files reached through a symbolic link or
outside the standard unit directories; a unit that changed between review and confirmation; a unit
whose files on disk differ from the copy systemd has loaded (for example a drop-in added without
`systemctl daemon-reload`; review it again after the next boot or reload); a unit with links to it that
its `[Install]` section does not describe (`systemctl disable` would remove those too), with links in
`/run/systemd/system`, or with links pointing elsewhere.

**Enable after an unreloaded change.** *Enable at boot* is refused while any unit file, drop-in or link
under the unit directories changed after systemd last loaded its configuration — for example a drop-in or
override written without `systemctl daemon-reload`, a `systemctl --no-reload enable` run on the host, or a
package that installed a unit without reloading. systemd would use such a change at the next boot, but the
review checks the boot-time effect against what systemd has loaded, so it cannot evaluate it. The message
lists the changed paths. Run the review again after the next boot or after systemd reloads its
configuration (`sudo systemctl daemon-reload`, if that is appropriate on the host). Homelab Control's own
boot-time changes do not block the next one, and *Disable at boot* stays available. The same applies to *Disable at
boot*: it is refused while unit files changed without a reload, and also when the service is the only thing
that starts a protected unit at boot — for example a custom unit that pulls in a WireGuard, Tailscale or SSH
unit through `Wants=`, `Requires=`, `BindsTo=` or `Upholds=`, directly, through other units or through an
alias. Disabling it would leave that protected unit stopped after the next reboot. If another enabled unit,
or a socket, timer or path unit, also starts it, the disable is allowed, and the review lists the units that
will no longer start at boot. A disable is also refused if a protected unit's files declare `Requisite=` on
the service.

**Runtime configuration** (files under `/run/systemd/system`, for example netplan's on Ubuntu) is not assumed
to be there after a reboot. It does not block anything unless it concerns a unit the review evaluates: then a
unit masked only until the next boot, a runtime unit file that replaces a persistent one, a runtime alias, or
a runtime drop-in that hides a persistent drop-in of the same name is refused with an explanation, and a
protected unit that only runtime configuration starts at boot does not count as started by something else.

**Unit search path.** Both boot-time actions are refused if systemd reports a unit search path other than the
documented one (for example when `SYSTEMD_UNIT_PATH` is set for the system manager); the message shows the
reported path.

**What runs now does not count for the next boot.** *Enable at boot* is judged against the next boot, not
against what is running at the moment: a dependency that runs now (for example because someone started it by
hand) is checked like one that does not, socket, timer and path units the service pulls in are followed to
the services they activate, and a conflict with a unit that is stopped now but starts at boot (or with a unit
the service itself starts) is checked as if that unit were running. The review therefore lists units that
"also start at boot" even if they are already running, and refuses more often than *Start* does for the same
service.

### Unit details, timers and sockets
- **Details** (any unit, `services.read`): the canonical name and aliases, state, the setting at boot,
  the unit file and drop-ins, the service type, automatic restarts since the manager started, the last
  exit of the main process, and the dependencies systemd reports. Missing values are shown as missing;
  for an installed but unloaded unit, systemd loads its definition only to answer the query; templates
  have no runtime state.
- **Unit file** (`services.files`, read on request and recorded in the audit log): the unit file and
  drop-ins as the service account can read them. Files only root can read are not shown. Values that
  look like secrets are replaced by `[redacted]`. This is heuristic and **not a secrecy guarantee**:
  secrets passed as positional arguments (`-u user:pass`, `-pSECRET`), inside quoted header values or
  URL query strings, or under setting names without words such as "password", "secret" or "token" are
  not detected. **This permission can disclose sensitive configuration**: grant `services.files` only to
  people who may read the host's unit files. It is never granted by default and is not included in
  `services.logs` (journal entries), `services.read`, `services.operate` or `services.configure`; grant it
  explicitly, for example `sudo hlctl user scopes --file /etc/homelab-control/users.json --username alice
  --scopes system.read,services.read,services.files`.
- **Downgrading** to a build without `services.files` (any build before this one): remove the scope from every
  account first (`hlctl user scopes …` without it). A users file naming a scope the running build does not
  know is rejected at start-up, and `hc-api` then does not start (`users file: … unknown scope`). The same
  applies to `services.configure` for builds before M2-3.
- **Timers** and **Sockets** tabs: schedule, last and next run, listen addresses and activated units,
  as systemd reports them.

### Turning the Services module off (module registry)
An administrator can disable the Services module on a host:

```sh
echo '{"version":1,"disabled":["services"]}' | sudo tee /etc/homelab-control/modules.json
sudo chmod 0644 /etc/homelab-control/modules.json   # root-owned, not group/world-writable
sudo systemctl restart homelab-control.service homelab-control-broker.service
```

Its navigation entry disappears, its API routes answer 404 like unknown routes, and the privileged
helper refuses its operations. An invalid file disables every module. Delete the file and restart
both units to re-enable it. There is no way to change this from the web interface.
