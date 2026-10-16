#!/usr/bin/env bash
# Disposable libvirt test VMs for Homelab Control (host side).
#
# Hard limits, enforced below rather than by convention:
#   - connection qemu:///system only; libvirt network "default" only
#   - VM and volume names must start with "hc-test-"
#   - every disk is a volume in the existing "hc-test" storage pool; no host
#     block devices, no paths outside that pool (the pool is never created,
#     changed or deleted by this script)
#   - destroy only acts on domains carrying this script's metadata marker,
#     and only after an explicit --yes
#   - images are downloaded only from cloud-images.ubuntu.com and are used only
#     after SHA256SUMS passes a GPG check against the pinned Ubuntu cloud-image
#     signing key fingerprint
# No sudo is used: the calling user needs access to qemu:///system (libvirt group).
#
# Usage:
#   dev/vm/hc-vm.sh preflight
#   dev/vm/hc-vm.sh fetch-image <release>          # 26.04 | 24.04
#   dev/vm/hc-vm.sh create <name> <release>        # e.g. hc-test-u2604 26.04
#   dev/vm/hc-vm.sh wait <name>                    # wait for DHCP + SSH + cloud-init
#   dev/vm/hc-vm.sh ip <name>
#   dev/vm/hc-vm.sh ssh <name> [command...]
#   dev/vm/hc-vm.sh copy <name> <local-path>... -- <remote-dir>
#   dev/vm/hc-vm.sh status
#   dev/vm/hc-vm.sh diagnose <name>                # read-only: state, leases, disks, boot
#   dev/vm/hc-vm.sh destroy <name> --yes
set -euo pipefail

CONN=qemu:///system
POOL=hc-test
NET=default
PREFIX=hc-test-
MARK="created-by=homelab-control/dev/vm/hc-vm.sh"
# Ubuntu Cloud Image signing key ("UEC Image Automatic Signing Key <cdimage@ubuntu.com>").
UBUNTU_CLOUD_KEY=D2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81
root=$(cd "$(dirname "$0")/../.." && pwd)
state=${HC_VM_STATE:-$root/.dev/vm}          # .dev/ is git-ignored
mkdir -p "$state/images" "$state/seeds"
chmod 700 "$state"

v() { virsh --connect "$CONN" "$@"; }
die() { echo "hc-vm: $*" >&2; exit 1; }
need_name() { [[ "${1:-}" =~ ^hc-test-[a-z0-9][a-z0-9-]{0,40}$ ]] || die "VM name must match ^hc-test-[a-z0-9-]+\$ (got '${1:-}')"; }

codename() {
  case "$1" in
    26.04) echo resolute ;;
    24.04) echo noble ;;
    *) die "unsupported release '$1' (26.04 or 24.04)" ;;
  esac
}

preflight() {
  local miss=()
  for c in virsh virt-install qemu-img gpg sha256sum curl ssh ssh-keygen scp; do command -v "$c" >/dev/null || miss+=("$c"); done
  command -v cloud-localds >/dev/null || command -v xorriso >/dev/null || command -v mkisofs >/dev/null \
    || command -v genisoimage >/dev/null || miss+=("cloud-localds|xorriso|mkisofs")
  ((${#miss[@]} == 0)) || die "missing host tools: ${miss[*]}"
  v uri >/dev/null || die "cannot connect to $CONN (is the user in the libvirt group?)"
  v pool-info "$POOL" >/dev/null 2>&1 || die "storage pool '$POOL' not found (create it yourself; this script never does)"
  [ "$(v pool-info "$POOL" | awk '/^State:/{print $2}')" = running ] || die "storage pool '$POOL' is not active"
  v net-info "$NET" >/dev/null 2>&1 || die "network '$NET' not found"
  [ "$(v net-info "$NET" | awk '/^Active:/{print $2}')" = yes ] || die "network '$NET' is not active"
  [ -e /dev/kvm ] || die "/dev/kvm missing (no KVM acceleration)"
  echo "preflight OK: $CONN, pool $POOL ($(v pool-dumpxml "$POOL" | sed -n 's|.*<path>\(.*\)</path>.*|\1|p' | head -1)), network $NET, /dev/kvm"
}

fetch_image() {
  local rel=$1 cn base img dir g
  cn=$(codename "$rel")
  base="https://cloud-images.ubuntu.com/releases/$cn/release"
  dir="$state/images/$cn"; mkdir -p "$dir"
  curl -fsSL --proto '=https' -o "$dir/SHA256SUMS" "$base/SHA256SUMS"
  curl -fsSL --proto '=https' -o "$dir/SHA256SUMS.gpg" "$base/SHA256SUMS.gpg"
  g="$state/gnupg"; mkdir -p "$g"; chmod 700 "$g"
  if ! GNUPGHOME="$g" gpg --batch --list-keys "$UBUNTU_CLOUD_KEY" >/dev/null 2>&1; then
    GNUPGHOME="$g" gpg --batch --keyserver hkps://keyserver.ubuntu.com --recv-keys "$UBUNTU_CLOUD_KEY"
  fi
  # --status-fd: require a VALIDSIG from exactly the pinned fingerprint, not just "a good signature".
  GNUPGHOME="$g" gpg --batch --status-fd 1 --verify "$dir/SHA256SUMS.gpg" "$dir/SHA256SUMS" 2>/dev/null \
    | grep -q "^\[GNUPG:\] VALIDSIG $UBUNTU_CLOUD_KEY " || die "SHA256SUMS signature is not a valid signature by $UBUNTU_CLOUD_KEY"
  img=$(awk '{print $2}' "$dir/SHA256SUMS" | tr -d '*' | grep -E "^ubuntu-$rel-server-cloudimg-amd64\.img$" | head -1)
  [ -n "$img" ] || die "no amd64 server cloud image for $rel listed in SHA256SUMS"
  if [ ! -f "$dir/$img" ] || ! (cd "$dir" && grep -E " \*?$img\$" SHA256SUMS | sha256sum -c --status); then
    curl -fL --proto '=https' -o "$dir/$img.part" "$base/$img"
    mv "$dir/$img.part" "$dir/$img"
  fi
  (cd "$dir" && grep -E " \*?$img\$" SHA256SUMS | sha256sum -c) || die "checksum mismatch for $img"
  {
    echo "release=$rel codename=$cn"
    echo "url=$base/$img"
    echo "sha256=$(grep -E " \*?$img\$" "$dir/SHA256SUMS" | awk '{print $1}')"
    echo "signing_key=$UBUNTU_CLOUD_KEY"
    echo "serial=$(curl -fsSL --proto '=https' "$base/" | grep -oE 'release-[0-9]{8}(\.[0-9]+)?' | sort -u | tail -1 || true)"
    echo "verified_at=$(date -u +%FT%TZ)"
  } > "$dir/VERIFIED"
  echo "$dir/$img"
}

pool_path() { v pool-dumpxml "$POOL" | sed -n 's|.*<path>\(.*\)</path>.*|\1|p' | head -1; }

make_seed() {
  local name=$1 out=$2 d
  d=$(mktemp -d); trap 'rm -rf "$d"' RETURN
  [ -f "$state/id_ed25519" ] || ssh-keygen -q -t ed25519 -N '' -C "hc-vm-test" -f "$state/id_ed25519"
  cat > "$d/user-data" <<EOF
#cloud-config
hostname: $name
users:
  - name: hc
    groups: [sudo]
    shell: /bin/bash
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    lock_passwd: true
    ssh_authorized_keys:
      - $(cat "$state/id_ed25519.pub")
ssh_pwauth: false
disable_root: true
package_update: false
runcmd:
  - [touch, /etc/hc-disposable]
EOF
  printf 'instance-id: %s-%s\nlocal-hostname: %s\n' "$name" "$(date +%s)" "$name" > "$d/meta-data"
  if command -v cloud-localds >/dev/null; then cloud-localds "$out" "$d/user-data" "$d/meta-data"
  elif command -v xorriso >/dev/null; then xorriso -as mkisofs -quiet -o "$out" -V cidata -J -r "$d/user-data" "$d/meta-data"
  else "$(command -v mkisofs || command -v genisoimage)" -quiet -o "$out" -V cidata -J -r "$d/user-data" "$d/meta-data"; fi
}

create() {
  local name=$1 rel=$2 cn img basevol vol seedvol seed fmt
  need_name "$name"; preflight >/dev/null
  v dominfo "$name" >/dev/null 2>&1 && die "domain $name already exists; not touching it"
  cn=$(codename "$rel")
  img=$(fetch_image "$rel" | tail -1)
  basevol="${PREFIX}base-ubuntu-$rel.qcow2"; vol="$name.qcow2"; seedvol="$name-seed.iso"
  for x in "$vol" "$seedvol"; do v vol-info --pool "$POOL" "$x" >/dev/null 2>&1 && die "volume $POOL/$x already exists; not touching it"; done
  # Human output: the image's own "file format:" line comes first (JSON lists child nodes such as
  # "file" before the top-level format on newer qemu-img).
  fmt=$(qemu-img info "$img" | awk -F': *' '/^file format:/{print $2; exit}')
  [ "$fmt" = qcow2 ] || die "unexpected image format '$fmt'"
  if ! v vol-info --pool "$POOL" "$basevol" >/dev/null 2>&1; then
    v vol-create-as "$POOL" "$basevol" "$(stat -c %s "$img")" --format qcow2 >/dev/null
    v vol-upload --pool "$POOL" "$basevol" "$img"
  fi
  v vol-create-as "$POOL" "$vol" 20G --format qcow2 --backing-vol "$basevol" --backing-vol-format qcow2 >/dev/null
  seed="$state/seeds/$name-seed.iso"; make_seed "$name" "$seed"
  v vol-create-as "$POOL" "$seedvol" "$(stat -c %s "$seed")" --format raw >/dev/null
  v vol-upload --pool "$POOL" "$seedvol" "$seed"
  virt-install --connect "$CONN" --name "$name" --memory 4096 --vcpus 2 --cpu host-passthrough \
    --osinfo detect=on,require=off --import \
    --disk "vol=$POOL/$vol,bus=virtio" --disk "vol=$POOL/$seedvol,device=cdrom" \
    --network "network=$NET,model=virtio" --graphics none --noautoconsole \
    --metadata "description=Homelab Control disposable test VM ($rel). $MARK"
  echo "created $name ($rel); waiting for address and SSH..."
  wait_ssh "$name"
}

ip_of() {
  local name=$1 a
  need_name "$name"
  a=$(v domifaddr "$name" --source lease 2>/dev/null | awk '/ipv4/{sub(/\/.*/,"",$4); print $4; exit}')
  [ -n "$a" ] || a=$(v domifaddr "$name" --source arp 2>/dev/null | awk '/ipv4/{sub(/\/.*/,"",$4); print $4; exit}')
  [ -n "$a" ] || return 1
  echo "$a"
}

sshopts() { echo "-i $state/id_ed25519 -o IdentitiesOnly=yes -o UserKnownHostsFile=$state/known_hosts -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -o BatchMode=yes"; }

wait_ssh() {
  local name=$1 a="" stage="no DHCP lease/ARP entry yet" err=""
  for i in $(seq 120); do
    a=$(ip_of "$name" || true)
    if [ -n "$a" ]; then
      stage="address $a, port 22 closed"
      if (exec 3<>"/dev/tcp/$a/22") 2>/dev/null; then
        stage="address $a, SSH up, login failed"
        # shellcheck disable=SC2046
        if err=$(ssh $(sshopts) "hc@$a" 'cloud-init status --wait >/dev/null 2>&1; true' 2>&1); then
          echo "$name ready at $a"; return 0
        fi
      fi
    fi
    [ $((i % 6)) -eq 0 ] && echo "  ... $((i * 5))s: $stage"
    sleep 5
  done
  [ -n "$err" ] && echo "last ssh error: $err" >&2
  die "$name not reachable over SSH within 10 minutes (last state: $stage). Run: dev/vm/hc-vm.sh diagnose $name"
}

# Read-only: state, NIC, leases, disks and boot settings of one harness VM.
diagnose() {
  local name=$1
  need_name "$name"
  echo "== domstate";       v domstate "$name" --reason || true
  echo "== domiflist";      v domiflist "$name" || true
  echo "== domifaddr lease"; v domifaddr "$name" --source lease || true
  echo "== domifaddr arp";   v domifaddr "$name" --source arp || true
  echo "== dhcp leases ($NET)"; v net-dhcp-leases "$NET" || true
  echo "== domblklist";     v domblklist "$name" --details || true
  echo "== os/boot/machine"; v dumpxml "$name" | grep -E '<(type|loader|boot|firmware|cpu mode|interface|model type|serial|console)' | sed 's/^ */  /'
  echo "== seed volume";    v vol-info --pool "$POOL" "$name-seed.iso" 2>&1 || true
  echo "== harness key";    ls -l "$state/id_ed25519" "$state/id_ed25519.pub" 2>&1
  echo
  echo "Next: watch the boot/cloud-init log on the serial console:"
  echo "  virsh -c $CONN console $name     (press Enter; exit with Ctrl+])"
}

do_ssh() { local name=$1 a; shift; a=$(ip_of "$name") || die "no address for $name"
  # Remote command is intentionally interpreted by the guest shell.
  # shellcheck disable=SC2046,SC2029
  ssh $(sshopts) "hc@$a" "$@"; }

do_copy() {
  local name=$1 a; shift; local srcs=()
  while [ $# -gt 0 ] && [ "$1" != -- ]; do srcs+=("$1"); shift; done
  if [ "${1:-}" != -- ] || [ -z "${2:-}" ]; then die "usage: copy <name> <path>... -- <remote-dir>"; fi
  a=$(ip_of "$name") || die "no address for $name"
  # shellcheck disable=SC2046
  scp -r -q $(sshopts) "${srcs[@]}" "hc@$a:$2"
}

status() {
  echo "== domains ($PREFIX*)"; v list --all --name | grep "^$PREFIX" || echo "(none)"
  echo "== volumes in $POOL"; v vol-list "$POOL" | awk 'NR>2 && $1!=""{print $1}'
}

destroy() {
  local name=$1 desc src p
  need_name "$name"; [ "${2:-}" = --yes ] || die "destroy needs --yes"
  v dominfo "$name" >/dev/null 2>&1 || die "no domain $name"
  desc=$(v desc "$name" 2>/dev/null || true)
  [[ "$desc" == *"$MARK"* ]] || die "$name was not created by this harness (marker missing); refusing"
  p=$(pool_path)
  # Every disk must be inside the hc-test pool before anything is removed.
  while read -r src; do
    [ -z "$src" ] && continue
    [[ "$src" == "$p"/* ]] || die "$name has a disk outside pool $POOL ($src); refusing"
  done < <(v domblklist "$name" --details | awk '$1=="file"{print $4}')
  v destroy "$name" >/dev/null 2>&1 || true
  v undefine "$name" --nvram >/dev/null 2>&1 || v undefine "$name" >/dev/null
  for x in "$name.qcow2" "$name-seed.iso"; do v vol-delete --pool "$POOL" "$x" >/dev/null 2>&1 || true; done
  echo "destroyed $name and its volumes (base image volume kept)"
}

cmd=${1:-}; shift || true
case "$cmd" in
  preflight) preflight ;;
  fetch-image) fetch_image "${1:?release}" ;;
  create) create "${1:?name}" "${2:?release}" ;;
  ip) ip_of "${1:?name}" ;;
  ssh) do_ssh "${1:?name}" "${@:2}" ;;
  copy) do_copy "$@" ;;
  status) status ;;
  diagnose) diagnose "${1:?name}" ;;
  wait) need_name "${1:?name}"; wait_ssh "$1" ;;
  destroy) destroy "${1:?name}" "${2:-}" ;;
  *) sed -n '2,25p' "$0"; exit 2 ;;
esac
