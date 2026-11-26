#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Static, fail-closed guard for the read-only storage helper (hc-storage-probe:
# lsblk, smartctl, pvs/lvs): its two systemd units, the SELinux module source,
# the AppArmor profile and the packaging scripts. It reads files only: it never
# starts a unit, loads a policy, or touches a device.
#
# Owner 2026-11-25 (ADR 0008 Amendment 12): the package ships the opt-in marker
# and an administrator may enable the socket; the package never starts or
# enables it. The helper runs as root with CAP_SYS_RAWIO only.
#
# What it enforces:
#   - keys are a closed set (any new key needs a deliberate edit here);
#   - every key except DeviceAllow/SystemCallFilter is assigned exactly once,
#     so systemd's "last assignment wins" cannot hide a conflicting later line;
#   - DeviceAllow is non-empty and is exactly the reviewed block groups, each
#     with "r" only (no w, no m, no char-*, no /dev paths, no wildcards, no
#     duplicates, no empty reset);
#   - RootImage=/MountImages=/ExtensionImages= are banned (systemd adds
#     block-blkext and block-loop with rwm for them, systemd.exec(5));
#   - the service has no [Install] section; the socket's only install rule is
#     WantedBy=sockets.target; no packaging script enables, starts or presets it;
#   - the service runs as root (no User=, DynamicUser=, SupplementaryGroups=)
#     with exactly CAP_SYS_RAWIO and CAP_SYS_ADMIN (smartctl: SATA pass-through
#     and NVMe admin commands) in the bounding and ambient sets;
#   - the SELinux and AppArmor sources contain no unconfined/permissive/write
#     shortcut, and the names used by the units match the policy sources.
#
# Usage: tests/packaging/storage-probe-policy-check.sh [--no-self-test] [REPO_ROOT]
# Exit 0 when everything holds. Needs bash, awk, grep, sed, sort (no systemd).
set -u

selftest=1
if [ "${1:-}" = "--no-self-test" ]; then selftest=0; shift; fi
root=${1:-$(cd "$(dirname "$0")/../.." && pwd)}

REVIEWED_CLASSES="sd blkext virtblk md"
MARKER=/etc/homelab-control/storage-probe.enabled
EXEC=/usr/lib/homelab-control/hc-storage-probe
SEL_TYPE=hc_storage_probe_t
AA_PROFILE=hc-storage-probe
SVC_NAME=homelab-control-storage-probe@.service
SOCK_NAME=homelab-control-storage-probe.socket
DENY_SYSCALLS='~@resources @mount @swap @reboot @raw-io @module @obsolete @debug @cpu-emulation @clock'
INACCESSIBLE='-/etc/homelab-control -/var/lib/homelab-control -/var/log/homelab-control -/var/log/homelab-control-broker -/dev/shm -/dev/mqueue'

FAILS=0
P=""
bad() { FAILS=$((FAILS + 1)); echo "  - $*" >&2; }

# parse_unit FILE -> TSV "section<TAB>key<TAB>value"; headers as key __section__.
parse_unit() {
  awk '
    /^[[:space:]]*($|[#;])/ { next }
    /^[[:space:]]*\[/ { s=$0; gsub(/[[:space:]]/, "", s); sec=s; print s "\t__section__\t"; next }
    {
      line=$0; sub(/^[[:space:]]+/, "", line); i=index(line, "=")
      if (i == 0) { print sec "\t__noassign__\t" line; next }
      k=substr(line, 1, i-1); v=substr(line, i+1)
      gsub(/[[:space:]]+$/, "", k); sub(/^[[:space:]]+/, "", v); sub(/[[:space:]]+$/, "", v)
      print sec "\t" k "\t" v
    }' "$1"
}
vals() { printf '%s\n' "$P" | awk -F'\t' -v s="$1" -v k="$2" '$1==s && $2==k {print $3}'; }
count() { printf '%s\n' "$P" | awk -F'\t' -v s="$1" -v k="$2" '$1==s && $2==k {n++} END {print n+0}'; }
single() { # section key expected-value
  local n v
  n=$(count "$1" "$2")
  if [ "$n" != 1 ]; then bad "$1 $2 is assigned $n times (must be exactly once)"; return; fi
  v=$(vals "$1" "$2")
  [ "$v" = "$3" ] || bad "$1 $2='$v' (expected '$3')"
}
in_list() {
  local x=$1 list=$2
  case " $list " in *" $x "*) return 0 ;; *) return 1 ;; esac
}

# check_keys FILE "allowed sections" "unit keys" "section keys" "multi-valued keys"
check_keys() {
  local f=$1 secs=$2 ukeys=$3 skeys=$4 multi=$5 sec key n
  grep -qE '\\[[:space:]]*$' "$f" && bad "line continuation is not allowed in guarded units"
  while IFS=$'\t' read -r sec key _; do
    [ -n "$sec" ] || continue
    if [ "$key" = "__noassign__" ]; then bad "line without '=' in $sec"; continue; fi
    in_list "$sec" "$secs" || { bad "section $sec is not allowed"; continue; }
    [ "$key" = "__section__" ] && continue
    case "$sec" in
      "[Unit]") in_list "$key" "$ukeys" || bad "key $key is not allowed in [Unit]" ;;
      *)        in_list "$key" "$skeys" || bad "key $key is not allowed in $sec" ;;
    esac
    n=$(count "$sec" "$key")
    if [ "$n" -gt 1 ] && ! in_list "$key" "$multi"; then bad "$sec $key is assigned $n times"; fi
  done < <(printf '%s\n' "$P" | sort -u)
  # The only capabilities are CAP_SYS_RAWIO and CAP_SYS_ADMIN, and only in the
  # two capability keys (checked by value in check_service).
  if grep -vE '^[[:space:]]*[#;]' "$f" | grep 'CAP_' | grep -vqxE '(CapabilityBoundingSet|AmbientCapabilities)=CAP_SYS_RAWIO CAP_SYS_ADMIN'; then
    bad "a capability set other than 'CAP_SYS_RAWIO CAP_SYS_ADMIN', or in another key, appears in an assignment"
  fi
}

check_service() {
  local f=$1 v c n d missing
  FAILS=0; P=$(parse_unit "$f")
  [ -n "$P" ] || { bad "unit is empty or unreadable: $f"; return 1; }
  check_keys "$f" "[Unit] [Service]" \
    "Description Documentation Requires After ConditionPathExists RefuseManualStart" \
    "Type ExecStart StandardInput StandardOutput StandardError PrivateDevices DevicePolicy DeviceAllow CapabilityBoundingSet AmbientCapabilities NoNewPrivileges ProtectSystem ProtectHome PrivateTmp PrivateNetwork PrivateIPC IPAddressDeny ProtectKernelTunables ProtectKernelModules ProtectKernelLogs ProtectControlGroups ProtectClock ProtectHostname ProtectProc RestrictNamespaces RestrictRealtime RestrictSUIDSGID LockPersonality MemoryDenyWriteExecute RestrictAddressFamilies SystemCallArchitectures SystemCallFilter SystemCallErrorNumber InaccessiblePaths UMask SELinuxContext AppArmorProfile TimeoutStartSec MemoryMax TasksMax LimitCORE" \
    "DeviceAllow SystemCallFilter"
  single "[Unit]" ConditionPathExists "$MARKER"
  single "[Unit]" RefuseManualStart yes
  single "[Unit]" Requires "$SOCK_NAME"
  single "[Unit]" After "$SOCK_NAME"
  single "[Service]" Type oneshot
  # Exec lines run confined by the helper's MAC policy, which cannot exec grep or a shell.
  for c in ExecStartPre ExecStartPost ExecStop ExecStopPost ExecReload ExecCondition; do
    [ "$(count "[Service]" "$c")" = 0 ] || bad "$c is not allowed (the startup self-check belongs to the helper)"
  done
  single "[Service]" ExecStart "$EXEC"
  single "[Service]" StandardInput socket
  single "[Service]" StandardOutput socket
  single "[Service]" StandardError journal
  single "[Service]" PrivateDevices no
  single "[Service]" DevicePolicy closed
  # Root (LVM refuses to scan as any other user) with smartctl's two capabilities.
  single "[Service]" CapabilityBoundingSet "CAP_SYS_RAWIO CAP_SYS_ADMIN"
  single "[Service]" AmbientCapabilities "CAP_SYS_RAWIO CAP_SYS_ADMIN"
  single "[Service]" NoNewPrivileges yes
  single "[Service]" ProtectSystem strict
  single "[Service]" ProtectHome yes
  single "[Service]" PrivateTmp yes
  single "[Service]" PrivateNetwork yes
  single "[Service]" PrivateIPC yes
  single "[Service]" IPAddressDeny any
  for c in ProtectKernelTunables ProtectKernelModules ProtectKernelLogs ProtectControlGroups ProtectClock ProtectHostname RestrictNamespaces RestrictRealtime RestrictSUIDSGID LockPersonality MemoryDenyWriteExecute; do
    single "[Service]" "$c" yes
  done
  single "[Service]" ProtectProc invisible
  single "[Service]" RestrictAddressFamilies AF_UNIX
  single "[Service]" SystemCallArchitectures native
  single "[Service]" SystemCallErrorNumber EPERM
  single "[Service]" InaccessiblePaths "$INACCESSIBLE"
  single "[Service]" UMask 0077
  single "[Service]" SELinuxContext "system_u:system_r:${SEL_TYPE}:s0"
  single "[Service]" AppArmorProfile "$AA_PROFILE"
  single "[Service]" TimeoutStartSec 60
  single "[Service]" MemoryMax 128M
  single "[Service]" TasksMax 16
  single "[Service]" LimitCORE 0
  # SystemCallFilter: the allow group plus the deny groups, nothing else.
  n=$(count "[Service]" SystemCallFilter)
  v=$(vals "[Service]" SystemCallFilter | sort | tr '\n' '|')
  if [ "$n" != 2 ] || [ "$v" != "$(printf '%s\n%s\n' "$DENY_SYSCALLS" '@system-service' | sort | tr '\n' '|')" ]; then
    bad "SystemCallFilter must be exactly '@system-service' and '$DENY_SYSCALLS'"
  fi
  # DeviceAllow: non-empty, only reviewed block groups, "r" only, exactly the reviewed set.
  n=$(count "[Service]" DeviceAllow)
  [ "$n" -ge 1 ] || bad "DeviceAllow allow-list is empty (fail closed: the unit needs the reviewed groups)"
  d=$(vals "[Service]" DeviceAllow)
  # command substitution drops a trailing empty value, so count them explicitly
  [ "$(printf '%s\n' "$P" | awk -F'\t' '$1=="[Service]" && $2=="DeviceAllow" && $3=="" {n++} END {print n+0}')" = 0 ] \
    || bad "empty DeviceAllow assignment (resets the list)"
  while IFS= read -r v; do
    [ -n "$v" ] || { bad "empty DeviceAllow assignment (resets the list)"; continue; }
    ok=0
    for c in $REVIEWED_CLASSES; do [ "$v" = "block-$c r" ] && ok=1; done
    # The OpenZFS control node: libzfs opens it read-write even for zpool list.
    [ "$v" = "/dev/zfs rw" ] && ok=1
    [ "$ok" = 1 ] || bad "DeviceAllow='$v' is not a reviewed read-only block group (allowed: $REVIEWED_CLASSES, mode 'r') or /dev/zfs rw"
  done <<< "$d"
  [ -z "$(printf '%s\n' "$d" | sort | uniq -d)" ] || bad "duplicate DeviceAllow entries"
  missing=""
  for c in $REVIEWED_CLASSES; do printf '%s\n' "$d" | grep -qx "block-$c r" || missing="$missing $c"; done
  [ -z "$missing" ] || bad "reviewed block group(s) missing from DeviceAllow:$missing"
  printf '%s\n' "$d" | grep -qx "/dev/zfs rw" || bad "DeviceAllow lacks the OpenZFS control node /dev/zfs rw"
  return $((FAILS > 0))
}

check_socket() {
  local f=$1
  FAILS=0; P=$(parse_unit "$f")
  [ -n "$P" ] || { bad "unit is empty or unreadable: $f"; return 1; }
  check_keys "$f" "[Unit] [Socket] [Install]" \
    "Description Documentation ConditionPathExists" \
    "ListenStream Accept MaxConnections SocketMode SocketUser SocketGroup DirectoryMode RemoveOnStop WantedBy" ""
  single "[Unit]" ConditionPathExists "$MARKER"
  single "[Socket]" ListenStream /run/homelab-control/storage-probe.sock
  single "[Socket]" Accept yes
  single "[Socket]" MaxConnections 1
  single "[Socket]" SocketMode 0600
  single "[Socket]" SocketUser root
  single "[Socket]" SocketGroup root
  single "[Socket]" DirectoryMode 0755
  single "[Socket]" RemoveOnStop yes
  # An administrator may enable the socket; nothing else may pull it in.
  single "[Install]" WantedBy sockets.target
  return $((FAILS > 0))
}

# strip_policy FILE: drop comment-only lines and trailing '#' comments.
strip_policy() { sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$1"; }

check_selinux() { # te fc
  local te=$1 fc=$2 body fc_body
  FAILS=0
  if [ ! -r "$te" ] || [ ! -r "$fc" ]; then bad "SELinux sources are missing"; return 1; fi
  body=$(strip_policy "$te")
  for need in "policy_module(homelab_control_storage_probe" "type ${SEL_TYPE};" \
              "init_daemon_domain(${SEL_TYPE}, hc_storage_probe_exec_t)" \
              "allow ${SEL_TYPE} fixed_disk_device_t:blk_file { getattr open read ioctl };" \
              "allow ${SEL_TYPE} cgroup_t:dir search;" \
              "neverallow ${SEL_TYPE} self:capability { sys_admin sys_rawio sys_module mknod dac_override dac_read_search sys_ptrace net_admin net_raw };" \
              "neverallow ${SEL_TYPE} fixed_disk_device_t:blk_file { write append create unlink rename setattr relabelfrom relabelto };" \
              "neverallow ${SEL_TYPE} filesystem_type:filesystem { mount remount unmount };"; do
    printf '%s\n' "$body" | grep -qF "$need" || bad "SELinux source lacks: $need"
  done
  # Everything except the neverallow assertions must avoid shortcuts.
  local allow
  allow=$(printf '%s\n' "$body" | grep -v '^neverallow')
  for tok in unconfined permissive domain_unconfined bin_t lib_t libexec_t lvm_exec_t sys_admin sys_rawio sys_module \
             storage_raw_write storage_raw_read removable_device_t corecmd_exec_shell corecmd_exec_bin \
             tcp_socket udp_socket netlink mounton :capability dac_override; do
    printf '%s\n' "$allow" | grep -qF -- "$tok" && bad "SELinux source uses forbidden token '$tok'"
  done
  printf '%s\n' "$allow" | grep -E 'blk_file[^;]*(write|append|create|unlink|rename|setattr|lock|map)' \
    | grep -q . && bad "SELinux source grants more than read access on a block device"
  fc_body=$(strip_policy "$fc")
  [ "$(printf '%s\n' "$fc_body" | grep -c '')" = 1 ] || bad "file-context source must contain exactly one rule"
  printf '%s\n' "$fc_body" | grep -qE "^${EXEC//\//\\/}[[:space:]]+--[[:space:]]+gen_context\(system_u:object_r:hc_storage_probe_exec_t,s0\)$" \
    || bad "file-context rule must label $EXEC as hc_storage_probe_exec_t (no libexec, no bin_t)"
  return $((FAILS > 0))
}

check_apparmor() { # profile
  local f=$1 body line perms
  FAILS=0
  [ -r "$f" ] || { bad "AppArmor profile is missing"; return 1; }
  body=$(strip_policy "$f")
  printf '%s\n' "$body" | grep -qxF "profile ${AA_PROFILE} ${EXEC} flags=(enforce) {" || bad "profile header must be: profile ${AA_PROFILE} ${EXEC} flags=(enforce) {"
  for need in "capability sys_rawio," "capability sys_admin," "deny mount," "deny ptrace," "deny network inet," "deny network inet6," \
              "deny network netlink," "deny network packet," "deny network bluetooth," "deny network vsock," \
              "unix (receive, send, getattr, shutdown) type=stream," '@{PROC}/@{pid}/cgroup r,'; do
    printf '%s\n' "$body" | grep -qxF "  $need" || printf '%s\n' "$body" | grep -qxF "$need" || bad "AppArmor profile lacks: $need"
  done
  # A blanket network rule (allow or deny) is wrong: "deny network," also matches
  # AF_UNIX and overrides the unix rule, so the confined helper cannot read its
  # socket (Debian 13 audit: operation=recvmsg class=net family=unix denied).
  if printf '%s\n' "$body" | grep -qE '^[[:space:]]*(deny[[:space:]]+)?(audit[[:space:]]+)?network[[:space:]]*,'; then
    bad "blanket 'network,' rule (allow or deny) breaks AF_UNIX: use per-family denies"
  fi
  # Exactly two capability rules, and only reviewed tools may be executed (ix).
  [ "$(printf '%s\n' "$body" | grep -cE '^[[:space:]]*capability[[:space:]]')" = 2 ] || bad "AppArmor profile must allow exactly sys_rawio and sys_admin"
  printf '%s\n' "$body" | grep -E '[[:space:]]ix,' | grep -vxF -e '  /{usr/,}{s,}bin/lsblk ix,' -e '  /{usr/,}{s,}bin/smartctl ix,' -e '  /{usr/,}{s,}bin/{lvm,pvs,lvs,vgs} ix,' -e '  /{usr/,}{s,}bin/{zpool,zfs} ix,' -e '  /{usr/,}{s,}bin/btrfs ix,' -e '  /{usr/,}{s,}bin/mdadm ix,' \
    | grep -q . && bad "AppArmor profile executes an unreviewed program"
  while IFS= read -r line; do
    line=${line#"${line%%[![:space:]]*}"}
    case "$line" in
      deny\ *) continue ;;
      "capability sys_rawio,"|"capability sys_admin,") ;;
      capability*|network*|mount*|umount*|pivot_root*|ptrace*) bad "AppArmor rule must be a deny: $line" ;;
      owner\ /dev/*|/dev/*)
        perms=${line##* }; perms=${perms%,}
        [ "$perms" = r ] || [ "$line" = "/dev/zfs rw," ] || bad "device rule must be read-only: $line" ;;
    esac
    case "$line" in
      *\ [uUpPcC]x,*|*\ [uUpPcC]x|*\ pux,*|*\ Pux,*|*\ PUx,*|*\ cux,*|*\ Cux,*) bad "AppArmor exec transition other than ix: $line" ;;
      *unconfined*|*flags=\(*complain*) bad "AppArmor shortcut: $line" ;;
      /usr/bin/sh*|/bin/sh*|*/bash*|*/dash*) bad "shell exec in AppArmor profile: $line" ;;
    esac
  done <<< "$body"
  # Write access exists only for /dev/zfs (libzfs ioctls) and btrfs scrub state (exclusive lock; the unit's
  # ProtectSystem=strict keeps /var/lib read-only).
  printf '%s\n' "$body" | sed 's/^[[:space:]]*//' | grep -vE '^(deny|#|capability|network|unix|profile|\})' \
    | grep -E '^[/@].* [a-z]*w[a-z]*,$' | grep -vxF -e '/dev/zfs rw,' -e '/var/lib/btrfs/scrub.status.* rwk,' \
    | grep -q . && bad "AppArmor profile grants write outside /dev/zfs and the btrfs scrub state"
  # /dev/**/ and /**/ (directories only: LVM's device cache, btrfs mountpoints) are allowed; a file glob is not.
  printf '%s\n' "$body" | sed 's/^[[:space:]]*//' | grep -E '^/\*\*/ ' | grep -qvxF '/**/ r,' && bad "the directory rule /**/ must be read-only: /**/ r,"
  printf '%s\n' "$body" | sed 's/^[[:space:]]*//' | grep -qE '^/dev/\*\*([^/]|$)|^/\*\* ' && bad "AppArmor profile grants a broad path"
  return $((FAILS > 0))
}

check_packaging() { # root
  local r=$1 hits
  FAILS=0
  hits=$(grep -rnE 'systemctl[^#]*(enable|start|restart|preset|unmask|reload-or-restart|try-restart)[^#]*storage-probe' \
           "$r/packaging" "$r/scripts" 2>/dev/null | grep -v 'storage-probe-policy-check' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#')
  [ -z "$hits" ] || bad "a packaging script starts or enables the storage probe: $hits"
  hits=$(grep -rln 'storage-probe\.enabled' "$r/packaging" 2>/dev/null | grep -v "/common/${SVC_NAME}\$" | grep -v "/common/${SOCK_NAME}\$" | grep -v "/debian/build-deb.sh\$")
  [ -z "$hits" ] || bad "the opt-in marker is referenced outside the two units and the .deb build: $hits"
  hits=$(find "$r/packaging" -iname '*preset*' 2>/dev/null)
  [ -z "$hits" ] || bad "preset files must not exist for the storage probe: $hits"
  if grep -rqlE 'WantedBy=.*storage-probe|storage-probe.*WantedBy=' "$r/packaging" 2>/dev/null; then
    bad "WantedBy references the storage probe"
  fi
  # Package scripts install the helper from /usr/lib/homelab-control (matches the SELinux file context).
  grep -rqn 'libexec.*hc-storage-probe' "$r/packaging" 2>/dev/null && bad "hc-storage-probe must not be installed under libexec"
  # Cross-checks between the units and the policy sources.
  grep -q "type ${SEL_TYPE};" "$r/packaging/fedora/selinux/homelab_control_storage_probe.te" 2>/dev/null || bad "SELinuxContext type is not declared by the module"
  grep -q "profile ${AA_PROFILE} " "$r/packaging/debian/apparmor/usr.lib.homelab-control.hc-storage-probe" 2>/dev/null || bad "AppArmorProfile name is not defined by the profile"
  return $((FAILS > 0))
}

run_all() { # root -> 0 if all guards hold
  local r=$1 rc=0
  check_service "$r/packaging/common/$SVC_NAME" || { echo "FAIL  service unit" >&2; rc=1; }
  check_socket "$r/packaging/common/$SOCK_NAME" || { echo "FAIL  socket unit" >&2; rc=1; }
  check_selinux "$r/packaging/fedora/selinux/homelab_control_storage_probe.te" "$r/packaging/fedora/selinux/homelab_control_storage_probe.fc" || { echo "FAIL  SELinux source" >&2; rc=1; }
  check_apparmor "$r/packaging/debian/apparmor/usr.lib.homelab-control.hc-storage-probe" || { echo "FAIL  AppArmor profile" >&2; rc=1; }
  check_packaging "$r" || { echo "FAIL  packaging" >&2; rc=1; }
  return $rc
}

# --- self-test: every mutation must be rejected, the pristine tree must pass ---
self_test() {
  local tmp n=0 fail=0 name kind expr target rc
  tmp=$(mktemp -d) || return 1
  # shellcheck disable=SC2064
  trap "rm -rf '$tmp'" RETURN
  mutate() { # kind name sed-expr
    kind=$1; name=$2; expr=$3
    rm -rf "$tmp/t"; mkdir -p "$tmp/t"
    cp -r "$root/packaging" "$tmp/t/packaging"; mkdir -p "$tmp/t/scripts"
    case "$kind" in
      service) target="$tmp/t/packaging/common/$SVC_NAME" ;;
      socket)  target="$tmp/t/packaging/common/$SOCK_NAME" ;;
      te)      target="$tmp/t/packaging/fedora/selinux/homelab_control_storage_probe.te" ;;
      fc)      target="$tmp/t/packaging/fedora/selinux/homelab_control_storage_probe.fc" ;;
      aa)      target="$tmp/t/packaging/debian/apparmor/usr.lib.homelab-control.hc-storage-probe" ;;
      deb)     target="$tmp/t/packaging/debian/build-deb.sh" ;;
    esac
    sed -i -e "$expr" "$target"
    n=$((n + 1))
    if run_all "$tmp/t" >/dev/null 2>&1; then echo "SELF-TEST FAIL  not rejected: $name" >&2; fail=$((fail + 1)); fi
  }
  rm -rf "$tmp/t"; mkdir -p "$tmp/t"; cp -r "$root/packaging" "$tmp/t/packaging"; mkdir -p "$tmp/t/scripts"
  run_all "$tmp/t" >/dev/null 2>&1 || { echo "SELF-TEST FAIL  pristine copy was rejected" >&2; run_all "$tmp/t" >&2; return 1; }
  mutate service "block-sd with write"            's/^DeviceAllow=block-sd r$/DeviceAllow=block-sd rw/'
  mutate service "block-md with mknod"            's/^DeviceAllow=block-md r$/DeviceAllow=block-md rm/'
  mutate service "wildcard group"                 's/^DeviceAllow=block-sd r$/DeviceAllow=block-* r/'
  mutate service "loop group"                     's/^DeviceAllow=block-sd r$/DeviceAllow=block-loop r/'
  mutate service "char device group"              's/^DeviceAllow=block-sd r$/DeviceAllow=char-pts rw/'
  mutate service "zfs node with mknod"            's#^DeviceAllow=/dev/zfs rw$#DeviceAllow=/dev/zfs rwm#'
  mutate service "zfs node replaced"              's#^DeviceAllow=/dev/zfs rw$#DeviceAllow=/dev/mem rw#'
  mutate service "zfs node removed"               '\#^DeviceAllow=/dev/zfs rw$#d'
  mutate service "explicit device path"           's#^DeviceAllow=block-sd r$#DeviceAllow=/dev/sda r#'
  mutate service "extra char group appended"      '/^DeviceAllow=block-md r$/a DeviceAllow=char-misc r'
  mutate service "empty allow-list"               '/^DeviceAllow=/d'
  mutate service "empty reset assignment"         '/^DeviceAllow=block-md r$/a DeviceAllow='
  mutate service "missing reviewed group"         '/^DeviceAllow=block-virtblk r$/d'
  mutate service "duplicate DeviceAllow"          '/^DeviceAllow=block-md r$/a DeviceAllow=block-md r'
  mutate service "private devices yes"            's/^PrivateDevices=no$/PrivateDevices=yes/'
  mutate service "late conflicting PrivateDevices" '/^LimitCORE=0$/a PrivateDevices=yes'
  mutate service "late conflicting DevicePolicy"  '/^LimitCORE=0$/a DevicePolicy=auto'
  mutate service "device policy auto"             's/^DevicePolicy=closed$/DevicePolicy=auto/'
  mutate service "RootImage"                      '/^LimitCORE=0$/a RootImage=/var/lib/x.img'
  mutate service "MountImages"                    '/^LimitCORE=0$/a MountImages=/var/lib/x.img:/mnt'
  mutate service "ExtensionImages"                '/^LimitCORE=0$/a ExtensionImages=/var/lib/x.raw'
  mutate service "extra bounding capability"      's/^CapabilityBoundingSet=CAP_SYS_RAWIO CAP_SYS_ADMIN$/CapabilityBoundingSet=CAP_SYS_RAWIO CAP_SYS_ADMIN CAP_DAC_OVERRIDE/'
  mutate service "extra ambient capability"       's/^AmbientCapabilities=CAP_SYS_RAWIO CAP_SYS_ADMIN$/AmbientCapabilities=CAP_SYS_RAWIO CAP_SYS_ADMIN CAP_NET_ADMIN/'
  mutate service "no capability line"             '/^CapabilityBoundingSet=/d'
  mutate service "inverted bounding set"          's/^CapabilityBoundingSet=CAP_SYS_RAWIO CAP_SYS_ADMIN$/CapabilityBoundingSet=~CAP_SYS_MODULE/'
  mutate service "mount syscalls allowed"         's/ @mount//'
  mutate service "module syscalls allowed"        's/ @module//'
  mutate service "install section"                "\$a [Install]\nWantedBy=multi-user.target"
  mutate service "WantedBy only"                  '/^LimitCORE=0$/a WantedBy=multi-user.target'
  mutate service "User set"                       '/^LimitCORE=0$/a User=nobody'
  mutate service "extra group"                    '/^LimitCORE=0$/a SupplementaryGroups=disk'
  mutate service "dynamic user"                   '/^LimitCORE=0$/a DynamicUser=yes'
  mutate service "no SELinuxContext"              '/^SELinuxContext=/d'
  mutate service "wrong SELinux type"             's/hc_storage_probe_t/unconfined_service_t/'
  mutate service "no AppArmorProfile"             '/^AppArmorProfile=/d'
  mutate service "no opt-in marker"               '/^ConditionPathExists=/d'
  mutate service "manual start allowed"           's/^RefuseManualStart=yes$/RefuseManualStart=no/'
  mutate service "ExecStartPost"                  '/^ExecStart=/a ExecStartPost=/bin/true'
  mutate service "ExecStartPre"                   '/^ExecStart=/i ExecStartPre=/bin/true'
  mutate service "different ExecStart"            's#^ExecStart=.*#ExecStart=/usr/libexec/homelab-control/hc-storage-probe#'
  mutate service "network allowed"                's/^PrivateNetwork=yes$/PrivateNetwork=no/'
  mutate service "inet sockets"                   's/^RestrictAddressFamilies=AF_UNIX$/RestrictAddressFamilies=AF_UNIX AF_INET/'
  mutate service "raw-io not denied"              's/ @raw-io//'
  mutate service "no new privileges off"          's/^NoNewPrivileges=yes$/NoNewPrivileges=no/'
  mutate service "unknown key (BindPaths)"        '/^LimitCORE=0$/a BindPaths=/dev'
  mutate service "line continuation"              's#^LimitCORE=0$#LimitCORE=0 \\#'
  mutate socket  "socket wanted by multi-user"    's/^WantedBy=sockets.target$/WantedBy=multi-user.target/'
  mutate socket  "socket required by"             '/^WantedBy=sockets.target$/a RequiredBy=homelab-control.service'
  mutate socket  "socket install alias"           '/^WantedBy=sockets.target$/a Alias=other.socket'
  mutate socket  "no opt-in marker"               '/^ConditionPathExists=/d'
  mutate socket  "world accessible"               's/^SocketMode=0600$/SocketMode=0666/'
  mutate socket  "unlimited connections"          's/^MaxConnections=1$/MaxConnections=64/'
  mutate socket  "not per-connection"             's/^Accept=yes$/Accept=no/'
  mutate socket  "duplicate listen"               '/^ListenStream=/a ListenStream=0.0.0.0:9999'
  mutate te      "unconfined domain"              '/^allow init_t hc_storage_probe_t:process2/a unconfined_domain(hc_storage_probe_t)'
  mutate te      "permissive domain"              '/^allow init_t hc_storage_probe_t:process2/a permissive hc_storage_probe_t;'
  mutate te      "block device write"             's/blk_file { getattr open read ioctl }/blk_file { getattr open read write ioctl }/'
  mutate te      "capability grant"               '/^allow hc_storage_probe_t self:fifo_file/a allow hc_storage_probe_t self:capability sys_rawio;'
  mutate te      "removable raw read"             '/^allow hc_storage_probe_t self:fifo_file/a storage_raw_read_removable_device(hc_storage_probe_t)'
  mutate te      "shell exec"                     '/^allow hc_storage_probe_t self:fifo_file/a corecmd_exec_shell(hc_storage_probe_t)'
  mutate te      "LVM executable grant"            '/^allow hc_storage_probe_t self:fifo_file/a can_exec(hc_storage_probe_t, lvm_exec_t)'
  mutate te      "cgroup traversal removed"        '/^allow hc_storage_probe_t cgroup_t:dir search;$/d'
  mutate te      "neverallow removed"             '/^neverallow hc_storage_probe_t filesystem_type/d'
  mutate te      "bin_t shortcut"                 '/^allow hc_storage_probe_t self:fifo_file/a allow hc_storage_probe_t bin_t:file { execute };'
  mutate fc      "libexec context"                's#/usr/lib/homelab-control#/usr/libexec/homelab-control#'
  mutate fc      "wrong type"                     's/hc_storage_probe_exec_t/bin_t/'
  mutate aa      "device write"                   's#^  /dev/sd\[a-z\]\* r,#  /dev/sd[a-z]* rw,#'
  mutate aa      "other device write"             's#^  /dev/zfs rw,#  /dev/zfs rw,\n  /dev/mem rw,#'
  mutate aa      "capability widened"             's/^  capability sys_rawio,/  capability sys_module,/'
  mutate aa      "third capability"               's/^  capability sys_admin,/  capability sys_admin,\n  capability dac_override,/'
  mutate aa      "mount deny removed"             '/^  deny mount,/d'
  mutate aa      "blanket network allow"          's/^  deny network inet,/  network,/'
  mutate aa      "blanket deny network (AF_UNIX)" 's/^  deny network inet,/  deny network,/'
  mutate aa      "inet deny removed"              '/^  deny network inet,/d'
  mutate aa      "inet6 deny removed"             '/^  deny network inet6,/d'
  mutate aa      "netlink deny removed"           '/^  deny network netlink,/d'
  mutate aa      "packet deny removed"            '/^  deny network packet,/d'
  mutate aa      "bluetooth deny removed"         '/^  deny network bluetooth,/d'
  mutate aa      "vsock deny removed"             '/^  deny network vsock,/d'
  mutate aa      "unix rule removed"              '/^  unix (receive/d'
  mutate aa      "cgroup read removed"            '/@{PROC}\/@{pid}\/cgroup r,/d'
  mutate aa      "unreviewed exec"                's#^  /{usr/,}{s,}bin/lsblk ix,#  /{usr/,}{s,}bin/lsblk ix,\n  /{usr/,}bin/dd ix,#'
  mutate aa      "unconfined exec"                's#^  /{usr/,}{s,}bin/lsblk ix,#  /{usr/,}{s,}bin/lsblk Ux,#'
  mutate aa      "complain mode"                  's/flags=(enforce)/flags=(complain)/'
  mutate aa      "broad device rule"              's#^  /dev/block/ r,#  /dev/** rw,#'
  mutate aa      "broad device read"              's#^  /dev/block/ r,#  /dev/** r,#'
  mutate aa      "file-wide read"                 's#^  /\*\*/ r,#  /** r,#'
  mutate aa      "directory write"                's#^  /\*\*/ r,#  /**/ rw,#'
  mutate aa      "extra file write"               's#^  /etc/blkid.conf r,#  /etc/blkid.conf rw,#'
  mutate aa      "scrub state widened"            's#^  /var/lib/btrfs/scrub.status.\* rwk,#  /var/lib/btrfs/** rwk,#'
  mutate aa      "profile renamed"                's/^profile hc-storage-probe /profile other /'
  mutate deb     "package starts probe"           "\$a systemctl start homelab-control-storage-probe.socket"
  mutate deb     "package enables probe"          "\$a systemctl enable --now homelab-control-storage-probe.socket"
  mutate deb     "libexec install"                "\$a install -Dm0755 x \"\$stage/usr/libexec/homelab-control/hc-storage-probe\""
  echo "self-test: $n mutations, $fail not rejected"
  [ "$fail" = 0 ]
}

rc=0
run_all "$root" || rc=1
if [ "$rc" = 0 ] && [ "$selftest" = 1 ]; then self_test || rc=1; fi
if [ "$rc" = 0 ]; then echo "OK  storage probe policy guard: units, SELinux, AppArmor and packaging hold"; else echo "FAIL storage probe policy guard" >&2; fi
exit "$rc"
