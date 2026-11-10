#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
"""Future approval-scoped NEW-resource provisioning. Default is a no-write plan."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import stat
import subprocess

ROOT = Path('/home/ian/.homecontrol-metadata-test')
SOURCE = Path('/tmp/hc-traefik-phase7-artifacts/build/hc-docker-metadata')
EXPECTED_MAP = [(0, 1000, 1), (1, 100000, 65536)]


def verify_map():
    matches = []
    for p in Path('/proc').iterdir():
        if not p.name.isdigit():
            continue
        try:
            if p.stat().st_uid == 1000 and (p / 'comm').read_text().strip() == 'dockerd':
                maps = [[tuple(map(int, line.split())) for line in (p / name).read_text().splitlines()]
                        for name in ('uid_map', 'gid_map')]
                if maps == [EXPECTED_MAP, EXPECTED_MAP]:
                    matches.append(p.name)
        except (OSError, ValueError):
            continue
    if len(matches) != 1:
        raise RuntimeError('Rootless identity mapping unverified')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute-approved', action='store_true')
    parser.add_argument('--artifact-sha256', default='')
    args = parser.parse_args()
    if not args.execute_approved:
        print(json.dumps({'mode': 'PLAN ONLY; no files, ACLs or services changed',
                          'root': str(ROOT), 'binarySource': str(SOURCE),
                          'newDirectories': ['bin', 'credentials', 'transport'],
                          'newFiles': ['bin/hc-docker-metadata', 'credentials/broker_token'],
                          'aclUID': 100999}))
        return
    if os.getuid() != 1000 or os.getgid() != 1000 or not re.fullmatch('[a-f0-9]{64}', args.artifact_sha256):
        raise RuntimeError('Approved identity and artifact digest required')
    verify_map()
    if ROOT.exists() or ROOT.is_symlink() or shutil.which('setfacl') is None:
        raise RuntimeError('New root collision or required ACL tool unavailable')
    parent = ROOT.parent.lstat()
    if not stat.S_ISDIR(parent.st_mode) or parent.st_uid != 1000 or parent.st_mode & 0o022:
        raise RuntimeError('Untrusted parent directory')
    fd = os.open(SOURCE, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as source:
        st = os.fstat(source.fileno())
        if not stat.S_ISREG(st.st_mode) or st.st_size > 32 * 1024 * 1024:
            raise RuntimeError('Invalid binary artifact')
        binary = source.read(32 * 1024 * 1024 + 1)
    if hashlib.sha256(binary).hexdigest() != args.artifact_sha256:
        raise RuntimeError('Artifact digest changed')
    os.umask(0o077)
    ROOT.mkdir(mode=0o700)
    for child in ('bin', 'credentials', 'transport'):
        (ROOT / child).mkdir(mode=0o700)
    target = os.open(ROOT / 'bin/hc-docker-metadata', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o500)
    with os.fdopen(target, 'wb') as out:
        out.write(binary)
        out.flush()
        os.fchmod(out.fileno(), 0o555)
    target = os.open(ROOT / 'credentials/broker_token', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(target, 'w') as out:
        out.write(secrets.token_hex(32) + '\n')
    subprocess.run(['setfacl', '-m', 'u:100999:--x,m::--x', str(ROOT / 'transport')], check=True)
    subprocess.run(['setfacl', '-m', 'u:100999:r--,m::r--', str(ROOT / 'credentials/broker_token')], check=True)
    print('Created only approved new resources; no values logged. Broker not started; socket ACL still requires approved post-bind step.')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        raise SystemExit('Provisioning stopped; no automatic cleanup or permission workaround')
