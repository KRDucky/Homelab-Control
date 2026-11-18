# M2-3 KVM evidence: PR #119 Phase A1/A2 (partial)

**Status:** partial evidence only; not M2-3 acceptance and not production readiness.
Captured 2026-11-18 from the existing disposable guests in the authorized
`claude-vms-zfs` pool. The guests had autostart disabled and their
`pre-m2-3-phase-a` snapshots were retained. The system was accessed over SSH
with strict host-key checking. No host package, production service, or SELinux
policy was changed.

## Exact source and CI artifact

- PR #119 head: `04185f2894b78a4a55d87019b924d773a72d1ba6`
- Exact source merge commit: `ffec9a42f1a9eecf79763c5bb9a22ba8baa16fb3`
- CI run: [44317837412](https://github.com/KRDucky/Homelab-Control/actions/runs/44317837412), 21/21 checks succeeded.
- GitHub release-candidate artifact digest: `d88afd5535d193f4afcc3c8f9e3419ff62a8eeb58be95f5e979692d8df3b2ece`
- RPM archive SHA-256: `021a9f62933039696e1df54d0c9ba7014750eed0b4b83305910eb88dbc87a46c`
- Debian archive SHA-256: `01eba6ac510b22da27971fc8a97c8d84226e5269681fd33ef84fe2f0da6e2e33`
- Test-source archive SHA-256: `bd60a00c646e0ce2eb6915aca28e24dc940cfa712e43b7aa181363c211c87b14`
- The GitHub API confirmed the PR head and merge commit have identical trees.

The CI job builds RPM payload binaries that differ from the generic Linux
tarball. The hashes below are therefore package-specific and were computed by
reading the exact RPM's gzip-compressed cpio payload; the Fedora guest's
installed-file hashes matched them. The package archive itself also matched
its pinned SHA-256.

| RPM path | SHA-256 | bytes |
|---|---|---:|
| `/usr/bin/hc-api` | `439d1de79420c9017c1b1b54ac7e499e010ae815877b7037e42dc8bf20cf5e75` | 14,382,448 |
| `/usr/lib/homelab-control/hc-broker` | `e5ea6e5824d838b1535c270b931828652bbeeb38a3eeea392303870d6cfb7962` | 4,476,528 |
| `/usr/bin/hlctl` | `e4f156e3c2bd35e395fc1de0a8425dec0b37567b3a113044d522a8d89aa580c2` | 8,594,768 |

## Results

| Guest | Systemd / kernel | Policy | Phase A1 | Phase A2 |
|---|---|---|---|---|
| Ubuntu 26.04.1 | 259.9 / 7.0.0-43-generic | AppArmor enabled | 308 PASS, 0 FAIL, 1 expected Phase B skip; exit 0 | 16 PASS, 0 FAIL; exit 0 |
| Ubuntu 22.04.5 | 249.11 / 5.15.0-203-generic | AppArmor enabled | 308 PASS, 0 FAIL, 1 expected Phase B skip; exit 0 | 16 PASS, 0 FAIL; exit 0 |
| Fedora 44 | 259.5 / 6.19.10-300.fc44 | SELinux Enforcing (targeted) | Not run | Not run |

For both Ubuntu guests, A1 breakdown was 89 service-operation checks, 145
enablement checks, and 74 systemd smoke checks. Long expiry waits (125 seconds
for the plan token and 305 seconds for step-up expiry) passed. The Webmin fixture
passed. A2 was a plan-only disable survey; no plan was applied. Results:

- Ubuntu 26.04.1: 30 allowed, 17 refused of 47 enabled services; largest plan
  took 829 ms.
- Ubuntu 22.04.5: 27 allowed, 18 refused of 45; largest plan took 755 ms.
- The fixture with `Wants=network-online.target` was allowed on both guests;
  its temporary enablement was undone and the pre-existing
  `systemd-networkd-wait-online` state was restored.
- Review the refused-service reasons in the retained raw logs before deciding
  whether the disable policy over-refuses.

The expected single A1 skip is the broker interruption scenario, reserved for
Phase B. Neither A3/browser, real reboot checks, nor Phase B has run on these
guests.

## Fedora first attempt and resumption

The first Fedora package probe installed the exact RPM and then purged it. The
archive hash matched. Its three installed executable hashes matched the
package-specific RPM payload hashes above, but not the generic tarball hashes.
No Fedora A1/A2 test ran before the stop. This was a manifest omission, not a
product failure. The exact payload hashes are now recorded so a new authorized
job can resume with an explicit per-package comparison.

The first Fedora A1 retry on the existing guest stopped before the test checks:
the prior run had left an `admin` account and state database, so the harness's
fresh-account setup refused to continue. The RPM was removed again and the
existing data and snapshot were preserved. No A2, A3/A4/A5 or Phase B work ran
in that attempt. A fresh, uniquely named Fedora guest is being used for the
next A1/A2 attempt; its result will be recorded separately before acceptance is
claimed.

## Reproducible local evidence

Raw logs and per-guest command output remain in the authorized VM-pool artifact
bundle `m2-3-pr119-04185f28-inputs/phase-a-20261118/run-jONjjO/`; the unredacted
files are retained locally and are not committed. The relevant files are
`u2604/m2-3-phase-a.log`,
`u2604/m2-3-disable-survey.log`, `u2204/m2-3-phase-a.log`,
`u2204/m2-3-disable-survey.log`, guest package/binary hash records, and
`f44/BINARY-HASH-MISMATCH.md`. SHA-256 checksums of the four Ubuntu raw logs:

```text
abb5c0f33bda1ccb57abd998eb6d0f444cc06cbf4454079d0bfb3f0f819def76  u2604/m2-3-phase-a.log
8c2cc4882ff78215366145308200d8ffeefb9c8d5378ca4c4038606540852a87  u2604/m2-3-disable-survey.log
f946da2edb3d00c42ecc54f4fba0377225409abfd26a6f66dc75b102c6d35e84  u2204/m2-3-phase-a.log
fd7192cb2f35d96b924bd807270e11d9799359f9a49668f7defbbc854795e2bd  u2204/m2-3-disable-survey.log
```

This sanitized summary omits guest UUIDs, machine IDs, addresses, usernames,
and credentials. Original snapshots and raw logs are retained.
