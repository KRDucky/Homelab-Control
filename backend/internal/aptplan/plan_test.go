// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later
package aptplan

import (
	"errors"
	"strings"
	"testing"
)

const good = `Reading package lists... Done
Building dependency tree... Done
The following additional packages will be installed:
  libdemo
1 upgraded, 1 newly installed, 1 to remove and 0 not upgraded.
Inst libdemo (1.2-1 Debian:13/stable [amd64])
Inst demo [1.0-1] (2.0-1 Debian:13/stable [amd64])
Remv olddemo [0.1]
Conf libdemo (1.2-1 Debian:13/stable [amd64])
Conf demo (2.0-1 Debian:13/stable [amd64])
`

func TestParseSimulation(t *testing.T) {
	p, err := ParseSimulation(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 5 || p.InstallCount != 2 || p.RemoveCount != 1 || p.ConfigureCount != 2 {
		t.Fatalf("bad counts: %+v", p)
	}
	if c := p.Changes[1]; c.Name != "demo" || c.CandidateVersion != "2.0-1" || c.InstalledVersion != "1.0-1" {
		t.Fatalf("bad upgrade: %+v", c)
	}
}
func TestEmptyTransaction(t *testing.T) {
	p, err := ParseSimulation("0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n")
	if err != nil || len(p.Changes) != 0 {
		t.Fatalf("%+v, %v", p, err)
	}
}
func TestRejectDangerousAndAmbiguousOutput(t *testing.T) {
	cases := []string{
		"", "Inst foo (1.0 Debian)\n", "E: Unsatisfied dependencies\n" + good,
		strings.Replace(good, "Inst demo", "Inst --force", 1),
		good + "W: unauthenticated packages\n",
		good + "Inst evil (1.2)\n",
		strings.Replace(good, "Conf demo", "Purg demo", 1),
		strings.Replace(good, "Inst libdemo", "Inst libdemo", 1) + "Inst libdemo (1.2 Debian)\n",
		strings.Repeat("x", maxOutput+1),
		strings.Replace(good, "Inst libdemo (1.2-1 Debian:13/stable [amd64])", "Inst libdemo (bogus", 1),
	}
	for i, x := range cases {
		if _, err := ParseSimulation(x); !errors.Is(err, ErrUnsafe) {
			t.Errorf("case %d err %v", i, err)
		}
	}
}
