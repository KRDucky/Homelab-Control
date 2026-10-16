#!/usr/bin/env bash
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
check "legacy falls back to unprivileged detection" unprivileged "$(jget "d['source']" <<<"$(get /api/v1/legacy/webmin)")"
check "overview still works" 200 "$(getc /api/v1/host/overview)"
systemctl start homelab-control-broker.socket
if [ -x /etc/webmin/stop ]; then /etc/webmin/stop >/dev/null 2>&1 || true; fi
