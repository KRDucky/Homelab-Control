#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# Cross-language retention check on a disposable fixture (never a real
# state.db): Go writes real audit records, hc-audit-retention archives and
# prunes them, then the Go verifier checks the ledger, the extracted archive
# and the pruned database (anchored), and refuses the pruned database
# without the anchor.
# Usage: tests/retention/go-cross-check.sh <hc-audit-retention> <hlctl>
set -euo pipefail
bin=$(realpath "${1:?hc-audit-retention binary}")
hlctl=$(realpath "${2:?hlctl binary}")
cd "$(git rev-parse --show-toplevel)"
work=$(mktemp -d); chmod 700 "$work"; trap 'rm -rf "$work"' EXIT
fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS  $1"; else echo "FAIL  $1: expected $2, got $3"; fail=1; fi; }

(cd backend && HC_RETENTION_FIXTURE="$work/state.db" go test -count=1 -run '^TestWriteRetentionFixture$' ./internal/state/ >/dev/null)
chmod 600 "$work/state.db"
check "Go fixture verifies" 0 "$("$hlctl" audit verify --db "$work/state.db" >/dev/null 2>&1; echo $?)"
"$bin" run --dry-run --state-db "$work/state.db" --archive-dir "$work/archive" --config "$work/none.conf" >/dev/null
check "dry run creates no archive directory" 0 "$([ -e "$work/archive" ] && echo 1 || echo 0)"
"$bin" run --state-db "$work/state.db" --archive-dir "$work/archive" --config "$work/none.conf" >"$work/run.out"
check "pruned rows" "pruned_rows: 50" "$(grep '^pruned_rows' "$work/run.out")"
check "hc-audit-retention verify" 0 "$("$bin" verify --state-db "$work/state.db" --archive-dir "$work/archive" >/dev/null 2>&1; echo $?)"
check "Go verifies the ledger" 0 "$("$hlctl" audit verify --file "$work/archive/ledger.jsonl" >/dev/null 2>&1; echo $?)"
mkdir "$work/ex"; tar -xzf "$work"/archive/audit-*.tar.gz -C "$work/ex"
check "Go verifies the archived records" 0 "$("$hlctl" audit verify --file "$work/ex/audit.jsonl" >/dev/null 2>&1; echo $?)"
done_line=$(grep '"event":"retention.prune","outcome":"done"' "$work/archive/ledger.jsonl" | tail -1)
seq=$(sed -n 's/.*"last_seq":\([0-9]*\).*/\1/p' <<<"$done_line"); hash=$(sed -n 's/.*"last_hash":"\([0-9a-f]*\)".*/\1/p' <<<"$done_line")
check "Go verifies the pruned database with the ledger anchor" 0 "$("$hlctl" audit verify --db "$work/state.db" --anchor "$seq:$hash" >/dev/null 2>&1; echo $?)"
check "Go refuses the pruned database without the anchor" 1 "$("$hlctl" audit verify --db "$work/state.db" >/dev/null 2>&1; echo $?)"
exit $fail
