# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
import contextlib
import hashlib
import importlib.util
import io
import json
import stat
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('provision_fixture', Path(__file__).with_name('provision.py'))
provision = importlib.util.module_from_spec(spec)
spec.loader.exec_module(provision)


class ProvisionSafety(unittest.TestCase):
    def test_root_is_owner_approved_location(self):
        # Owner-approved 2026-11-09: parent must not be the group-writable Projects dir.
        self.assertEqual(provision.ROOT, Path('/home/ian/.homecontrol-metadata-test'))
        self.assertEqual(provision.ROOT.parent, Path('/home/ian'))

    def test_default_is_no_write_plan(self):
        out = io.StringIO()
        with patch.object(sys, 'argv', ['provision.py']), patch.object(Path, 'mkdir', side_effect=AssertionError('write prohibited')), patch.object(provision.subprocess, 'run', side_effect=AssertionError('command prohibited')), contextlib.redirect_stdout(out):
            provision.main()
        self.assertIn('PLAN ONLY', json.loads(out.getvalue())['mode'])

    def test_execute_rejects_invalid_digest_before_writes(self):
        with patch.object(sys, 'argv', ['provision.py', '--execute-approved', '--artifact-sha256', 'invalid']), patch.object(Path, 'mkdir', side_effect=AssertionError('write prohibited')):
            with self.assertRaises(RuntimeError):
                provision.main()

    def test_scoped_new_temp_resources_and_only_two_mocked_acls(self):
        # Only temporary test files. No actual ACL command, daemon or service.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / 'new'
            source = Path(tmp) / 'fixture-artifact'
            source.write_bytes(b'controlled fixture binary')
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            with patch.object(provision, 'ROOT', root), patch.object(provision, 'SOURCE', source), patch.object(provision, 'verify_map'), patch.object(provision.os, 'getuid', return_value=1000), patch.object(provision.os, 'getgid', return_value=1000), patch.object(provision.os, 'umask'), patch.object(provision.shutil, 'which', return_value='/usr/bin/setfacl'), patch.object(provision.subprocess, 'run') as command, patch.object(sys, 'argv', ['provision.py', '--execute-approved', '--artifact-sha256', digest]), contextlib.redirect_stdout(io.StringIO()):
                provision.main()
                self.assertEqual(command.call_count, 2)
                # Exactly the two pre-bind ACLs in the reviewed deployment proposal.
                self.assertEqual([c.args[0] for c in command.call_args_list], [
                    ['setfacl', '-m', 'u:100999:--x,m::--x', str(root / 'transport')],
                    ['setfacl', '-m', 'u:100999:r--,m::r--', str(root / 'credentials/broker_token')],
                ])
                self.assertTrue(all(c.kwargs == {'check': True} for c in command.call_args_list))
                # Broker/CORE token loaders strip one trailing newline and require a 32-256 char token.
                self.assertRegex((root / 'credentials/broker_token').read_text(), r'\A[0-9a-f]{64}\n\Z')
                self.assertEqual(stat.S_IMODE((root / 'credentials/broker_token').stat().st_mode), 0o600)
                self.assertEqual(stat.S_IMODE((root / 'bin/hc-docker-metadata').stat().st_mode), 0o555)
                self.assertEqual((root / 'bin/hc-docker-metadata').read_bytes(), source.read_bytes())
                self.assertFalse((root / 'transport/broker.sock').exists())
                with self.assertRaises(RuntimeError):
                    provision.main()


if __name__ == '__main__':
    unittest.main(verbosity=2)
