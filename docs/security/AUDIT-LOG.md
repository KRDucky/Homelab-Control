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

## Statistics

`hlctl audit stats` reports the log's shape without exposing who did what:

```sh
sudo hlctl audit stats --file /var/log/homelab-control-broker/broker-audit.jsonl
sudo hlctl audit stats --db /var/lib/homelab-control/state.db
```

It prints the record count (chained and `legacy`), the byte size of the audit trail (the
file size for `--file`, the exported JSON Lines size for `--db`), the oldest and newest
timestamps, the number of chain segments, and read-event counts grouped by event family
and UTC day. A read event is an event name ending in `.read`; its family is the first
dot-separated component. `stats` never prints `user`, `remote`, `detail` or `data`.

## Anchored verification of a pruned log

A log whose beginning has been deleted (pruned) no longer starts with an `audit.chain`
record, so ordinary `hlctl audit verify` reports it as broken. When an administrator prunes
a log, they can keep a checkpoint of the record immediately before the prune point and later
verify the remainder against it:

```sh
sudo hlctl audit verify --file /var/log/homelab-control-broker/broker-audit.jsonl --anchor SEQ:HASH
```

`--anchor` is the `SEQ:HASH` of the record immediately before the first remaining record.
The first remaining record must continue that chain: its `seq` must be `SEQ+1` and its
`prev` must be `HASH`, otherwise verification fails. A partial log is accepted only when the
anchor is supplied explicitly; without `--anchor`, `verify` stays strict and a pruned or
partial log fails. The anchor is trusted input: it is a checkpoint the operator recorded and
it does not itself prove that the discarded prefix was intact.

## Retention

Status: implemented on the development branch, not accepted (ADR 0006 amendment 4 is Proposed). The worker
`hc-audit-retention` (Rust, SQLx) runs daily from `homelab-control-audit-retention.timer` as the
`homelab-control` user. It never runs as root.

**Settings** (`/etc/homelab-control/audit-retention.conf`, owned by root, mode 0640, group
`homelab-control`; the worker refuses a file that is a symlink, not root-owned, or group/world-writable):

| Setting | Range | Default |
|---|---|---|
| `active_retention_days` | 1–365 | 365: records stay on the Audit page this long |
| `archive_retention_months` | 1–60, at least the live window | 60: archives whose newest record is older are deleted |
| `active_db_threshold_gib` | 1–1024 | 5: above this many GiB of database pages in use, the oldest records are archived early |

The defaults are the owner maxima; administrators may only shorten. Shortening
`archive_retention_months` (which deletes archives sooner) needs one confirmation:
`runuser -u homelab-control -- hc-audit-retention run --dry-run`, then `... run --confirm`.

**What a run does** (`/var/lib/homelab-control/audit-archive/`, mode 0700):

1. Records older than the live window form a contiguous oldest prefix (a later record with an older
   timestamp never pulls newer ones with it). If the pages in use (`page_count − freelist_count`)
   exceed the threshold, more of the oldest records are archived, down to 90 % of the threshold. This
   never happens when the excess is not the audit trail's (the trail alone could not bring the
   database under the threshold), and it stops and records `retention.early blocked` if a prune did
   not reduce the pages in use; `--retry-early-archive` lifts the block. The newest record is never
   pruned, so hc-api always continues the chain.
2. For each archive (at most 100,000 records), every record's Go digest and chain link is verified
   first. The archive (`audit-<from id>-<to id>.tar.gz`: `manifest.json` and `audit.jsonl` in the
   `hlctl audit export` format, deterministic bytes) is written to a private temporary file, fsync'ed,
   renamed and the directory fsync'ed; an existing file of that name is used only if byte-identical.
   It is re-read from disk and verified again.
3. The ledger (`ledger.jsonl`, a hash-chained log in this file's format) records
   `retention.archive created` and `retention.prune intent`; then exactly those rows are deleted in one
   transaction that checks their count, id range and first/last hashes; then `retention.prune done`.
4. Archives whose newest record is older than the total window (calendar months, as Go's
   `AddDate`, and never inside the live window) are expired oldest first:
   `retention.expire intent`, delete, fsync, `retention.expire done`.
5. The ledger head is written to the journal (`audit checkpoint log=…/ledger.jsonl seq=… hash=…`).

Every ledger line is fsync'ed before the next step. After a crash the next run finishes a pending prune
or expiry (after re-verifying the archive) or refuses; a missing archive without a recorded expiry, an
unrecorded archive file that is not the byte-identical leftover of an interrupted run, a database that
does not continue the ledger, an unknown schema version or a modified record stops the run before any
change. `run --dry-run` reads only: no lock file, no directory, no ledger repair, read-only database.
Free pages are reused by SQLite; the file shrinks only where `auto_vacuum` is incremental (no full
`VACUUM` ever runs next to hc-api).

**Verification across archives and the database:** `hc-audit-retention verify` checks the ledger, every
archive (digests and links) and the live database as one chain, and accounts for expired archives by their
ledger records. With Go only: `hlctl audit verify --file ledger.jsonl`, `--file audit.jsonl` of an
extracted archive, and `hlctl audit verify --db state.db --anchor SEQ:HASH` with the `last_seq`/`last_hash`
of the latest `retention.prune done` ledger entry. Without the anchor a pruned database fails, as it
should. `tests/retention/go-cross-check.sh` runs these checks in CI.

**Visibility:** the Audit page and `hlctl audit export --db` show only the live database. Archived
records are in the archive directory; there is no UI for them yet.

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
- **Size:** the API trail is bounded by "Retention" above. The broker log and the operation journal
  still have no retention; they grow until an administrator prunes them (a few hundred bytes per
  event). Deleting old records from those breaks the chain at that point, which verification reports.
  Pruning only the beginning can be verified with `--anchor` (see above).
- **What is never logged:** passwords, session IDs, CSRF tokens and log content. A
  `services.unit.logs` read records the unit and line count only, never the messages returned. The
  broker records an argument only after it passes validation; anything else is recorded as
  `invalid args`.
