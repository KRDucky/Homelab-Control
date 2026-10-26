<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# Managing systemd services

Homelab Control can **start**, **stop** and **restart** systemd `.service` units from the Services
page. Every operation is reviewed before it runs, confirmed by you, carried out by the privileged
helper (`hc-broker`), recorded, and reported with the state systemd shows afterwards.

Nothing else is available yet: enabling or disabling units at boot, masking, editing unit files,
`daemon-reload`, other unit types (sockets, timers, mounts…) and host shutdown or reboot are not part
of this version.

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
