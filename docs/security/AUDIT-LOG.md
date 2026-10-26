<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# Audit logs: hash chaining, checkpoints and their limits

Status: implemented in M2 PR-1b (chaining, checkpoints) and PR-1a (API trail in SQLite). There are two logs:

- **API audit trail:** table `audit_events` in `/var/lib/homelab-control/state.db` (`hc-api`, owned by
  `homelab-control`, 0600, `synchronous=FULL`). Shown on the Audit page (scope `audit.read`), exported
  with `hlctl audit export --db`. Before 0.2 it was `/var/log/homelab-control/audit.jsonl`; that file
  is kept, no longer written, and still verifies (its records are reported as `legacy`).
- **Broker log:** `/var/log/homelab-control-broker/broker-audit.jsonl` (`hc-broker`, owned by root,
  0600). Not shown in the UI; verified with `hlctl audit verify --file`.
- **Operation journal (M2-2, ADR 0011):** `/var/log/homelab-control-broker/operations.jsonl`
  (`hc-broker`, root, 0600). The authoritative record of privileged operations: `op.intent` (written
  and fsync'ed before anything runs), `op.outcome` (observed result) and `op.interrupted` (recorded at
  broker start for an intent without an outcome; never retried). Records carry a structured `data`
  field (compact JSON, ≤ 8 KiB) covered by the hash; it holds the plan-token **ID**, never the token.
  Shown on the Audit page under *Privileged operations* (scope `audit.read`); verified with
  `hlctl audit verify --file`. If the journal cannot be opened, the broker disables write operations.

**The logs are tamper-evident, not tamper-proof.** Anyone who can write a log file can still change it.
The design only makes certain changes detectable, and only under the conditions listed below.

## Record format

Each record (a JSON line, or a table row exported as one) is:

| Field | Meaning |
|---|---|
| `seq` | 1, 2, 3, … within a chain segment |
| `time`, `event`, `user`, `remote`, `outcome`, `detail` | What happened (control characters are stripped and fields are capped at 128 bytes) |
| `prev` | `hash` of the previous record; 64 zeros for the first record of a segment |
| `hash` | SHA-256 of the record's JSON encoding without `hash` |

- **Writes:** each record is fsync'ed before the request continues.
- **Failing closed:** if the broker cannot write a record, it withholds the operation's result and
  returns `unavailable`. If `hc-api` cannot record a service-log read, it returns 503 and no log
  content. Other API events, such as logins, are recorded on a best-effort basis so that a full disk
  does not lock every administrator out; each failed write is reported in hc-api's journal
  (`audit write failed`).
- **Opening the log:** the first record of a new segment is an `audit.chain` event. Reopening an intact log continues the chain with an `audit.open` event.
- **Bad last line:** if the last line is unreadable or fails verification (a crash mid-write, or tampering), the logger starts a new segment. It does not silently continue the chain. Verification reports the break as a warning, and reports the bad line itself as a problem.
- **Older records:** records written before chaining existed (no `seq`) are counted as `legacy`.

## Checkpoints

The logger reports the chain head through journald as a separate, append-only record kept by another
system service. It does this when the log is opened, every 100 records, and when the service stops:

```
audit checkpoint log=/var/log/homelab-control-broker/broker-audit.jsonl seq=1234 hash=<64 hex>
```

To read the latest checkpoint and verify the log against it:

```sh
cp=$(journalctl -u homelab-control-broker.service -o cat | grep 'audit checkpoint' | tail -1 \
     | sed -E 's/.* seq=([0-9]+) hash=([0-9a-f]{64}).*/\1:\2/')
sudo hlctl audit verify --file /var/log/homelab-control-broker/broker-audit.jsonl --checkpoint "$cp"
```

For the API trail, use `homelab-control.service` and `--db /var/lib/homelab-control/state.db` instead of
`--file`. `hlctl` opens the database read-only, so it can run while `hc-api` is up.
`--checkpoint` can be repeated. `hlctl audit verify` exits non-zero if any problem is found.

## What is detected

| Change | Detected? | How |
|---|---|---|
| A field of a record edited | Yes | The record's `hash` no longer matches |
| A record edited and its hash recomputed | Yes | The next record's `prev` link breaks |
| Records reordered, deleted from the middle or inserted | Yes | `seq` gap or broken `prev` link |
| Garbage or non-JSON lines | Yes | Reported per line |
| The tail truncated cleanly (last N records removed) | **Only with a checkpoint** at or beyond the removed records | Checkpoint `seq` is beyond the head, or missing |
| The file replaced with an older copy (rollback) | **Only with a checkpoint** newer than the copy | Same |
| The whole file rewritten with a fresh, internally consistent chain | **Only with a checkpoint** | Checkpoint hash differs, or its `seq` is missing |
| Records appended by the attacker | **No** | An appended, correctly chained record looks legitimate |
| A crash mid-write | Shown as a new segment (warning) with the torn line reported | |

The tests are in `backend/internal/audit/audit_test.go`, `backend/internal/state/state_test.go` (the
database trail: edited and deleted rows, a tampered last row) and `backend/cmd/hlctl/main_test.go`. The
real-systemd checks in `tests/systemd/m1-checks.sh` exercise modification, truncation and live journal
checkpoints.

## Threat model and limitations

- **Who can write the logs:** root can write both logs. The `homelab-control` user, and therefore a
  compromised `hc-api`, can write the API log. A compromised `hc-api` cannot write the broker log.
  Anyone who can write a log can produce a consistent chain. Chaining only raises the bar: they also
  have to defeat the checkpoints.
- **Root and the journal:** root can also delete or rewrite journald files and rotate them away.
  Checkpoints only protect against an attacker who controls the log file but not the journal, for
  example a compromised `hc-api` against its own log. They also protect against tampering done
  carelessly.
- **Protection against root:** forward the journal off the host (for example `systemd-journal-upload`,
  syslog forwarding to another machine, or periodic copies of the checkpoint lines). Homelab Control
  does not configure forwarding for you.
- **Time between checkpoints:** a checkpoint covers only what was written before it. Up to 99 records
  after the last checkpoint can be truncated undetectably. A clean shutdown emits a final checkpoint.
  A crash or kill does not.
- **What a record proves:** the hash is not a signature. It proves internal consistency, not who
  wrote the record. Nothing is keyed or signed, by design. There are no signing keys (ADR 0009).
- **Rotation:** none is configured. If an administrator rotates or deletes a log, the next record
  starts a new segment, and checkpoints for the old file no longer match the new one.
- **Size:** no retention yet; both logs grow until an administrator prunes them (a few hundred bytes
  per event). Deleting old records breaks the chain at that point, which verification reports.
- **What is never logged:** passwords, session IDs, CSRF tokens and log content. A
  `services.unit.logs` read records the unit and line count only, never the messages returned. The
  broker records an argument only after it passes validation; anything else is recorded as
  `invalid args`.
