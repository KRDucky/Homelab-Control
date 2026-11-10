# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
import os
import sys
import tempfile
import unittest

# Importable both from this directory and from the repository root
# (python3 -m unittest dev/docker-metadata-test/test_broker_identity_audit.py).
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import broker_identity_audit as audit  # noqa: E402

GROUPS = "root:x:0:\nadm:x:4:\nsudo:x:27:\nusers:x:100:\nlxd:x:101:\ndocker:x:977:\nsvc-meta:x:2201:\nmedia:x:1500:\n"


def fixture(status):
    d = tempfile.mkdtemp()
    os.makedirs(os.path.join(d, "proc", "4242"))
    with open(os.path.join(d, "proc", "4242", "status"), "w") as f:
        f.write(status)
    with open(os.path.join(d, "group"), "w") as f:
        f.write(GROUPS)
    return os.path.join(d, "proc"), os.path.join(d, "group")


class BrokerIdentityAudit(unittest.TestCase):
    def test_current_test_broker_shape_is_not_production_ready(self):
        proc, grp = fixture("Uid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
                            "Groups:\t4 27 100 101 977 1000 1500\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n")
        r = audit.audit(4242, proc, grp)
        names = sorted(f.get("name") for f in r["findings"] if f["code"] == "privileged_group")
        self.assertEqual(names, ["adm", "docker", "lxd", "sudo"])
        self.assertFalse(r["productionReady"])
        self.assertEqual(audit.main(["4242", "--proc-root", proc, "--group-file", grp]), 1)

    def test_dedicated_identity_is_production_ready(self):
        proc, grp = fixture("Uid:\t2201\t2201\t2201\t2201\nGid:\t2201\t2201\t2201\t2201\n"
                            "Groups:\t2201\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n")
        r = audit.audit(4242, proc, grp)
        self.assertTrue(r["productionReady"], r["findings"])
        self.assertEqual(audit.main(["4242", "--proc-root", proc, "--group-file", grp, "--json"]), 0)

    def test_root_capabilities_and_missing_hardening_are_findings(self):
        proc, grp = fixture("Uid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\nGroups:\t\nCapEff:\t000001ffffffffff\nNoNewPrivs:\t0\nSeccomp:\t0\n")
        codes = {f["code"] for f in audit.audit(4242, proc, grp)["findings"]}
        self.assertEqual(codes, {"root_uid", "effective_capabilities", "new_privileges_allowed", "no_seccomp_filter"})

    def test_pid_option_form(self):
        proc, grp = fixture("Uid:\t2201\t2201\t2201\t2201\nGid:\t2201\t2201\t2201\t2201\n"
                            "Groups:\t2201\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n")
        self.assertEqual(audit.main(["--pid", "4242", "--proc-root", proc, "--group-file", grp]), 0)
        self.assertEqual(audit.main(["--proc-root", proc, "--pid", "4242", "--group-file", grp, "--json"]), 0)
        self.assertEqual(audit.main(["--pid"]), 2)
        self.assertEqual(audit.main(["--pid", "4242", "4243", "--proc-root", proc]), 2)

    def test_bad_input_fails_without_reading(self):
        self.assertEqual(audit.main([]), 2)
        self.assertEqual(audit.main(["../1"]), 2)
        self.assertEqual(audit.main(["999999999", "--proc-root", "/nonexistent"]), 2)


if __name__ == "__main__":
    unittest.main()
