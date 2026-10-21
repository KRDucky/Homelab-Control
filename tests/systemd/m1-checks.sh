#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M1 checks, sourced by systemd-smoke.sh after hc-api is up (uses check/code,
# HC_SID). Requires the broker socket unit to be started. Optionally installs
# upstream Webmin from $HC_WEBMIN_SRC into the standard /etc/webmin location
# (disposable hosts only) to exercise legacy detection against real files.
# shellcheck disable=SC2154,SC2034
api=http://127.0.0.1:9180
get() { curl -s -H "Cookie: __Host-hc_session=$HC_SID" "$api$1"; }
getc() { curl -s -o /dev/null -w '%{http_code}' -H "Cookie: __Host-hc_session=$HC_SID" "$api$1"; }
jget() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }

echo "== M1: broker"
check "broker socket active" active "$(systemctl is-active homelab-control-broker.socket)"
check "socket mode/owner" "660 root:homelab-control" "$(stat -c '%a %U:%G' /run/homelab-control/broker.sock)"
info=$(hlctl broker info 2>&1)
check "hlctl broker info (root peer)" 1 "$(grep -c 'services.systemd.list' <<<"$info")"
echo "$info"
check "broker started by socket activation" active "$(systemctl is-active homelab-control-broker.service)"
bpid=$(systemctl show -p MainPID --value homelab-control-broker.service)
check "broker runs as root" 0 "$(awk '/^Uid:/{print $2}' /proc/"$bpid"/status)"
check "broker CapEff = CAP_SETUID|CAP_SETGID only" 00000000000000c0 "$(awk '/^CapEff/{print $2}' /proc/"$bpid"/status)"
check "broker NoNewPrivs" 1 "$(awk '/^NoNewPrivs/{print $2}' /proc/"$bpid"/status)"
deny=$(runuser -u nobody -- hlctl broker info 2>&1 || true)
check "other local users cannot reach the broker" 1 "$(grep -ci 'unavailable\|denied' <<<"$deny")"

if [ -n "${HC_WEBMIN_SRC:-}" ] && [ ! -e /etc/webmin ]; then
  echo "== installing upstream Webmin from $HC_WEBMIN_SRC into /etc/webmin (disposable host)"
  [ -x /usr/local/bin/perl ] || ln -s /usr/bin/perl /usr/local/bin/perl
  ( cd "$HC_WEBMIN_SRC" && config_dir=/etc/webmin var_dir=/var/webmin perl=/usr/bin/perl port=10000 \
      login=admin password="webmin-smoke-$RANDOM-pw" ssl=0 atboot=0 nostart=1 nouninstall=1 \
      nopostinstall=1 noperlpath=1 autoos=1 autothird=1 ./setup.sh >/tmp/webmin-setup.log 2>&1 )
  /etc/webmin/start >/dev/null 2>&1 || true
  sleep 2
fi

echo "== M1: API"
capj=$(get /api/v1/capabilities)
for id in services hardware legacy; do
  check "capability $id available" available "$(jget "[c['status'] for c in d['capabilities'] if c['id']=='$id'][0]" <<<"$capj")"
done
svc=$(get /api/v1/services)
check "services: homelab-control.service listed active" active "$(jget "[u['active'] for u in d['units'] if u['name']=='homelab-control.service'][0]" <<<"$svc")"
check "services: unit-file state merged" enabled-or-disabled "$(jget "'enabled-or-disabled' if [u.get('unit_file_state') for u in d['units'] if u['name']=='homelab-control.service'][0] in ('enabled','disabled') else 'missing'" <<<"$svc")"
echo "   $(jget "d.get('systemd_version')" <<<"$svc"); units=$(jget "len(d['units'])" <<<"$svc"); failed=$(jget "d['counts'].get('failed_total',0)" <<<"$svc")"
# Catch the helper process while it runs to prove it is not root.
seen=$(for _ in $(seq 40); do get /api/v1/services >/dev/null & ps -o user= -C systemctl 2>/dev/null; done; wait)
check "systemctl helper never runs as root" 0 "$(grep -c '^root' <<<"$seen" || true)"
echo "   helper users observed: $(sort -u <<<"$seen" | tr '\n' ' ')"
hw=$(get /api/v1/hardware)
check "hardware: inventory returned" 1 "$(jget "1 if 'disks' in d['inventory'] else 0" <<<"$hw")"
echo "   disks=$(jget "[(x['kernel_name'],x['identity_confidence']) for x in d['inventory']['disks']['devices']]" <<<"$hw") identity=$(jget "d['identity']['status']" <<<"$hw") dmi_available=$(jget "(d['identity'].get('data') or {}).get('available')" <<<"$hw")"
lg=$(get /api/v1/legacy/webmin)
check "legacy: answered by broker" broker "$(jget "d['source']" <<<"$lg")"
if [ -e /etc/webmin/miniserv.conf ]; then
  check "legacy: real Webmin detected" True "$(jget "d['status']['installed']" <<<"$lg")"
  check "legacy: version read" "$(cat /etc/webmin/version)" "$(jget "d['status']['version']" <<<"$lg")"
  check "legacy: running miniserv detected" True "$(jget "d['status'].get('running')" <<<"$lg")"
  check "legacy: no secrets in response" 0 "$(grep -ciE 'pass|hash|[$]6[$]|[$]y[$]|PRIVATE' <<<"$lg" || true)"
  echo "   checks: $(jget "[(c['id'],c['level']) for c in d['status']['checks']]" <<<"$lg")"
fi
check "broker audit log written (root 0600)" "600 root" "$(stat -c '%a %U' /var/log/homelab-control-broker/broker-audit.jsonl)"
echo "   last broker audit: $(tail -1 /var/log/homelab-control-broker/broker-audit.jsonl)"
echo "== systemd-analyze security (broker)"; systemd-analyze security --no-pager homelab-control-broker.service 2>/dev/null | tail -1 || true

echo "== M2 PR-1b: service logs (read-only, services.logs scope)"
check "capability service_logs available" available "$(jget "[c['status'] for c in d['capabilities'] if c['id']=='service_logs'][0]" <<<"$capj")"
lj=$(get '/api/v1/services/units/homelab-control.service/logs?lines=20')
check "logs: entries returned for homelab-control.service" 1 "$(jget "1 if d['unit']=='homelab-control.service' and 0 < len(d['entries']) <= 20 else 0" <<<"$lj")"
echo "   last entry: $(jget "d['entries'][-1]['message'][:120]" <<<"$lj")"
check "logs: lines above 500 rejected" 400 "$(getc '/api/v1/services/units/homelab-control.service/logs?lines=501')"
check "logs: option-like unit rejected" 400 "$(getc '/api/v1/services/units/--output=export.service/logs')"
check "logs: encoded path traversal rejected" 400 "$(getc '/api/v1/services/units/..%2F..%2Fetc%2Fpasswd.service/logs')"
check "logs: unknown unit is 404" 404 "$(getc '/api/v1/services/units/hc-no-such-unit.service/logs')"
seen=$(for _ in $(seq 40); do get '/api/v1/services/units/homelab-control.service/logs?lines=500' >/dev/null & ps -o user= -C journalctl 2>/dev/null; done; wait)
check "journalctl helper never runs as root" 0 "$(grep -c '^root' <<<"$seen" || true)"
echo "   journalctl users observed: $(sort -u <<<"$seen" | tr '\n' ' ')"
bal=/var/log/homelab-control-broker/broker-audit.jsonl
check "broker audited the logs read (unit and lines only)" 1 "$(grep -c '"detail":"services.unit.logs unit=homelab-control.service lines=20 ' "$bal" | awk '{print ($1>0)}')"
check "log content is not copied into the broker audit log" 0 "$(grep -c 'Started\|Starting' "$bal" || true)"
check "API audited the logs read" 1 "$(grep -c '"event":"services.logs.read"' /var/log/homelab-control/audit.jsonl | awk '{print ($1>0)}')"

echo "== M2 PR-1b: hash-chained audit logs verify, with journal checkpoints"
# A clean stop writes a final checkpoint (= the head); the socket restarts the broker on demand.
systemctl stop homelab-control-broker.service
cp_line=$(journalctl -u homelab-control-broker.service -o cat --no-pager | grep 'audit checkpoint' | tail -1)
cp=$(sed -E 's/.* seq=([0-9]+) hash=([0-9a-f]{64}).*/\1:\2/' <<<"$cp_line")
echo "   journal checkpoint: $cp"
check "broker audit chain + journal checkpoint verify" 0 "$(hlctl audit verify --file "$bal" --checkpoint "$cp" >/tmp/av.txt 2>&1; echo $?)"
sed 's/^/   /' /tmp/av.txt
check "API audit chain verifies" 0 "$(hlctl audit verify --file /var/log/homelab-control/audit.jsonl >/dev/null 2>&1; echo $?)"
# Tamper on copies (the live logs stay untouched).
tmpd=$(mktemp -d)
head -n -1 "$bal" >"$tmpd/trunc.jsonl"
cpseq=${cp%%:*}
lastseq=$(tail -1 "$bal" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')
check "shutdown checkpoint is the log head" "$lastseq" "$cpseq"
check "truncation after the checkpoint is detected" 1 "$(hlctl audit verify --file "$tmpd/trunc.jsonl" --checkpoint "$cp" >/dev/null 2>&1; echo $?)"
check "truncation without a checkpoint is not detectable (documented limit)" 0 "$(hlctl audit verify --file "$tmpd/trunc.jsonl" >/dev/null 2>&1; echo $?)"
cp "$bal" "$tmpd/reorder.jsonl"; python3 - "$tmpd/reorder.jsonl" <<'PY'
import sys
p = sys.argv[1]; l = open(p).read().splitlines(True); l[2], l[3] = l[3], l[2]; open(p, "w").writelines(l)
PY
check "reordered records are detected" 1 "$(hlctl audit verify --file "$tmpd/reorder.jsonl" >/dev/null 2>&1; echo $?)"
sed '2s/"outcome":"[a-z]*"/"outcome":"forged"/' "$bal" >"$tmpd/mod.jsonl"
check "modified record is detected" 1 "$(hlctl audit verify --file "$tmpd/mod.jsonl" >/dev/null 2>&1; echo $?)"
rm -rf "$tmpd"

echo "== M1: overview filesystems describe the host, not hc-api's sandbox"
# hc-api runs with ProtectSystem=strict/PrivateTmp/LogsDirectory; its own mount
# view is read-only with extra bind mounts. The API must report the host's table.
ov=$(get /api/v1/host/overview)
check "filesystems taken from the host mount table" host "$(jget "d['filesystems'].get('view')" <<<"$ov")"
hostro=$(awk '$2=="/"{print ($4 ~ /(^|,)ro(,|$)/) ? "True" : "False"; exit}' /proc/1/mounts)
check "/ read-only flag matches the host" "$hostro" "$(jget "[f['read_only'] for f in d['filesystems']['items'] if f['mount_point']=='/'][0]" <<<"$ov")"
check "no mount points that the host does not have" 0 "$(jget "len([f for f in d['filesystems']['items'] if f['mount_point'] not in {l.split()[1] for l in open('/proc/1/mounts')}])" <<<"$ov")"

echo "== M1: broker stopped → graceful degradation"
systemctl stop homelab-control-broker.socket homelab-control-broker.service
sleep 6   # API caches broker state for 5 s
capj=$(get /api/v1/capabilities)
check "services unavailable without broker" unavailable "$(jget "[c['status'] for c in d['capabilities'] if c['id']=='services'][0]" <<<"$capj")"
check "hardware degraded without broker" degraded "$(jget "[c['status'] for c in d['capabilities'] if c['id']=='hardware'][0]" <<<"$capj")"
check "services endpoint 503" 503 "$(getc /api/v1/services)"
check "service logs unavailable without broker" unavailable "$(jget "[c['status'] for c in d['capabilities'] if c['id']=='service_logs'][0]" <<<"$capj")"
check "logs endpoint 503 without broker" 503 "$(getc '/api/v1/services/units/homelab-control.service/logs')"
check "legacy falls back to unprivileged detection" unprivileged "$(jget "d['source']" <<<"$(get /api/v1/legacy/webmin)")"
check "overview still works" 200 "$(getc /api/v1/host/overview)"
systemctl start homelab-control-broker.socket
if [ -x /etc/webmin/stop ]; then /etc/webmin/stop >/dev/null 2>&1 || true; fi
