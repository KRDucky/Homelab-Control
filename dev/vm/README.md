# Disposable VM (Vagrant) — unverified in M0

```bash
cd dev/vm
vagrant up --provider=libvirt     # or --provider=virtualbox
vagrant ssh
cd /src && sudo WEBMIN_PASS='throwaway' dev/disposable/webmin-baseline.sh
```
Tear down: `vagrant destroy -f`. The two extra 2 GiB virtual disks are for future storage fixtures.

Status: this definition was written in M0 but **not run** (the build environment had no KVM or
VirtualBox). Running it on your workstation and saving the output to `docs/evidence/` is an M1 entry task.
