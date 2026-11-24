// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package aptmirror

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/aptapply"
)

// release builds a clear-signed InRelease shaped like Ubuntu's (test data).
func release(codename, date, validUntil string) string {
	s := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nOrigin: Ubuntu\nSuite: " + codename +
		"\nCodename: " + codename + "\nDate: " + date + "\n"
	if validUntil != "" {
		s += "Valid-Until: " + validUntil + "\n"
	}
	return s + "SHA256:\n 0123 100 main/binary-amd64/Packages\n-----BEGIN PGP SIGNATURE-----\nxx\n"
}

const now = "Tue, 24 Nov 2026 10:00:00 UTC"

func server(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL + "/ubuntu/"
}

func allowAll(string) bool { return true }

func TestProbeRanksAndClassifies(t *testing.T) {
	fast := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ubuntu/dists/noble/InRelease" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(release("noble", now, "")))
	})
	slow := server(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Write([]byte(release("noble", now, "")))
	})
	stale := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(release("noble", "Mon, 23 Nov 2026 08:00:00 UTC", "")))
	})
	wrong := server(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(release("jammy", now, ""))) })
	expired := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(release("noble", now, "Tue, 24 Nov 2026 09:00:00 UTC")))
	})
	missing := server(t, http.NotFound)
	p := Prober{Client: NewClient(allowAll), Now: func() time.Time { return time.Date(2026, 11, 24, 10, 30, 0, 0, time.UTC) }}
	cands := []Candidate{{slow, SourceCurrent}, {fast, SourceOfficial}, {stale, SourceOfficial}, {wrong, SourceOfficial}, {expired, SourceOfficial}, {missing, SourceOfficial}}
	rs := p.Probe(context.Background(), cands, "noble")
	byURI := map[string]Result{}
	for _, r := range rs {
		byURI[r.URI] = r
	}
	if rs[0].URI != fast || rs[0].Rank != 1 || rs[1].URI != slow || rs[1].Rank != 2 || !rs[1].CurrentInUse {
		t.Fatalf("ranking: %+v", rs[:2])
	}
	for uri, want := range map[string]string{stale: StatusStale, wrong: StatusMismatch, expired: StatusExpired, missing: StatusUnreachable} {
		if got := byURI[uri]; got.Status != want || got.Rank != 0 || got.Reason == "" {
			t.Errorf("%s: %+v, want %s", uri, got, want)
		}
	}
}

func TestProbeRefusesPrivateAddressesAndBigBodies(t *testing.T) {
	ok := server(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(release("noble", now, ""))) })
	rs := Prober{Client: NewClient(nil)}.Probe(context.Background(), []Candidate{{ok, SourceOfficial}}, "noble")
	if rs[0].Status != StatusUnreachable {
		t.Fatalf("a loopback mirror from the official list was fetched: %+v", rs[0])
	}
	big := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(release("noble", now, "")))
		w.Write([]byte(strings.Repeat("x", maxIndexBytes+1)))
	})
	rs = Prober{Client: NewClient(allowAll)}.Probe(context.Background(), []Candidate{{big, SourceOfficial}}, "noble")
	if rs[0].Status != StatusUnreachable || !strings.Contains(rs[0].Reason, "too large") {
		t.Fatalf("oversize: %+v", rs[0])
	}
	loop := server(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, r.URL.String(), http.StatusFound) })
	rs = Prober{Client: NewClient(allowAll)}.Probe(context.Background(), []Candidate{{loop, SourceOfficial}}, "noble")
	if rs[0].Status != StatusUnreachable {
		t.Fatalf("redirect loop: %+v", rs[0])
	}
}

func TestCandidates(t *testing.T) {
	list := func(_ context.Context, u string) ([]byte, error) {
		if u != "http://mirrors.ubuntu.com/US.txt" {
			return nil, errors.New("unexpected " + u)
		}
		// Fixture: shaped like mirrors.ubuntu.com/US.txt (test data only).
		return []byte("http://ubuntu.example.org/ubuntu/\nhttp://mirror.example.edu/ubuntu\nhttp://u:p@bad.example/ubuntu/\nnot a url\n"), nil
	}
	cs, err := Candidates(context.Background(), aptapply.Distro{ID: "ubuntu", Codename: "noble"}, "US", []string{"http://us.archive.ubuntu.com/ubuntu/"}, list)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range cs {
		got = append(got, c.Source+" "+c.URI)
	}
	want := "current http://us.archive.ubuntu.com/ubuntu/,official http://ubuntu.example.org/ubuntu/,official http://mirror.example.edu/ubuntu/"
	if strings.Join(got, ",") != want {
		t.Fatalf("%v", got)
	}
	cs, _ = Candidates(context.Background(), aptapply.Distro{ID: "debian", Codename: "trixie"}, "DE", nil, list)
	if len(cs) != 3 || cs[2].URI != "http://ftp.de.debian.org/debian/" {
		t.Fatalf("debian: %+v", cs)
	}
	if _, err := Candidates(context.Background(), aptapply.Distro{ID: "ubuntu"}, "usa", nil, list); err == nil {
		t.Fatal("bad country accepted")
	}
}

func TestCountryForZone(t *testing.T) {
	tab := "# comment\nUS\t+404251-0740023\tAmerica/New_York\tEastern\nDE\t+5230+01322\tEurope/Berlin\n"
	if CountryForZone(tab, "Europe/Berlin") != "DE" || CountryForZone(tab, "Mars/Base") != "" {
		t.Fatal("zone lookup")
	}
}
