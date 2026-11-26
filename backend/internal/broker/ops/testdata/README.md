Captured 2026-10-15 from disposable Ubuntu containers (debootstrap minbase) running
systemd 255 (noble) and 249 (jammy) as PID 1, via `systemctl ... --output=json` as user nobody.
Real tool output used as parser fixtures; not data from any user host.

`snapraid-13.0-status*.txt`: captured 2026-11-26 from a disposable Ubuntu 26.04 VM (snapraid 13.0-1)
with a two-file test array: `sudo snapraid status` (stdout and stderr), and the same command while
another process held the array lock with `flock`.
