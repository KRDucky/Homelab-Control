// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucketBurstAndRetryAfter(t *testing.T) {
	b := NewTokenBucket(30, 5*time.Second)
	now := time.Unix(0, 0)
	b.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		if ok, _ := b.Allow("u"); !ok {
			t.Fatalf("request %d denied within burst", i+1)
		}
	}
	ok, retry := b.Allow("u")
	if ok {
		t.Fatal("request 31 allowed within burst")
	}
	if retry <= 0 || retry > 5*time.Second {
		t.Fatalf("retryAfter = %v", retry)
	}
}

func TestTokenBucketRefill(t *testing.T) {
	b := NewTokenBucket(2, 5*time.Second)
	now := time.Unix(0, 0)
	b.now = func() time.Time { return now }
	if ok, _ := b.Allow("u"); !ok {
		t.Fatal("first denied")
	}
	if ok, _ := b.Allow("u"); !ok {
		t.Fatal("second denied")
	}
	if ok, _ := b.Allow("u"); ok {
		t.Fatal("empty bucket allowed")
	}
	now = now.Add(5 * time.Second)
	if ok, _ := b.Allow("u"); !ok {
		t.Fatal("refilled token not available")
	}
	if ok, _ := b.Allow("u"); ok {
		t.Fatal("token before refill available")
	}
}

func TestTokenBucketIndependentKeys(t *testing.T) {
	b := NewTokenBucket(1, time.Hour)
	now := time.Unix(0, 0)
	b.now = func() time.Time { return now }
	if ok, _ := b.Allow("a"); !ok {
		t.Fatal("a denied")
	}
	if ok, _ := b.Allow("a"); ok {
		t.Fatal("a allowed twice")
	}
	if ok, _ := b.Allow("b"); !ok {
		t.Fatal("b must be independent of a")
	}
}

func TestTokenBucketBoundedKeys(t *testing.T) {
	b := NewTokenBucket(1, time.Hour)
	b.maxKeys = 2
	b.now = func() time.Time { return time.Unix(0, 0) }
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		b.Allow(k)
	}
	if b.Keys() > 2 {
		t.Fatalf("keys not bounded: %d", b.Keys())
	}
}

func TestTokenBucketRefund(t *testing.T) {
	b := NewTokenBucket(1, time.Hour)
	now := time.Unix(0, 0)
	b.now = func() time.Time { return now }
	if ok, _ := b.Allow("u"); !ok {
		t.Fatal("first denied")
	}
	if ok, _ := b.Allow("u"); ok {
		t.Fatal("second allowed (bucket empty)")
	}
	b.Refund("u")
	b.Refund("u") // capped at burst, no-op
	if ok, _ := b.Allow("u"); !ok {
		t.Fatal("refunded token not available")
	}
	if ok, _ := b.Allow("u"); ok {
		t.Fatal("refund exceeded burst")
	}
}

func TestPreauthIPAndGlobal(t *testing.T) {
	p := NewPreauth(10*time.Second, 10*time.Second, time.Minute)
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }
	if !p.Allow("1.1.1.1") {
		t.Fatal("first event denied")
	}
	if p.Allow("1.1.1.1") {
		t.Fatal("same IP within window allowed")
	}
	if p.Allow("2.2.2.2") {
		t.Fatal("different IP within global window allowed")
	}
	now = now.Add(10 * time.Second)
	if !p.Allow("1.1.1.1") {
		t.Fatal("event after window denied")
	}
}

func TestPreauthSummaryBounded(t *testing.T) {
	p := NewPreauth(10*time.Second, 10*time.Second, time.Minute)
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }
	if !p.Allow("1.1.1.1") {
		t.Fatal("first denied")
	}
	if p.Allow("1.1.1.1") {
		t.Fatal("second should be suppressed")
	}
	if n, ok := p.Summary(); !ok || n != 1 {
		t.Fatalf("first summary = %d ok=%v, want 1 true", n, ok)
	}
	if p.Allow("1.1.1.1") {
		t.Fatal("still within window")
	}
	if _, ok := p.Summary(); ok {
		t.Fatal("summary within interval allowed")
	}
	now = now.Add(time.Minute)
	if !p.Allow("1.1.1.1") {
		t.Fatal("event after interval denied")
	}
	if p.Allow("1.1.1.1") {
		t.Fatal("suppressed expected")
	}
	if n, ok := p.Summary(); !ok || n != 2 {
		t.Fatalf("second summary = %d ok=%v, want 2 true", n, ok)
	}
}

func TestPreauthGlobalCapSurvivesKeyChurn(t *testing.T) {
	p := NewPreauth(10*time.Second, 10*time.Second, time.Minute)
	p.maxKeys = 2
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }
	if !p.Allow("1.1.1.1") {
		t.Fatal("first denied")
	}
	// Many distinct IPs within the global window must all be suppressed even
	// if the per-IP map resets from churn.
	for _, ip := range []string{"2.2.2.2", "3.3.3.3", "4.4.4.4", "5.5.5.5"} {
		if p.Allow(ip) {
			t.Fatalf("ip %s within global window allowed", ip)
		}
	}
	if p.Keys() > 2 {
		t.Fatalf("per-IP map not bounded: %d", p.Keys())
	}
}

func TestOncePer(t *testing.T) {
	o := NewOncePer(time.Minute)
	now := time.Unix(0, 0)
	o.now = func() time.Time { return now }
	if !o.Allow("u") {
		t.Fatal("first not allowed")
	}
	if o.Allow("u") {
		t.Fatal("second within interval allowed")
	}
	now = now.Add(time.Minute)
	if !o.Allow("u") {
		t.Fatal("after interval not allowed")
	}
}

func TestCeilSeconds(t *testing.T) {
	cases := map[time.Duration]int{
		0:                       1,
		time.Second:             1,
		1500 * time.Millisecond: 2,
		5 * time.Second:         5,
	}
	for d, want := range cases {
		if got := CeilSeconds(d); got != want {
			t.Errorf("CeilSeconds(%v) = %d, want %d", d, got, want)
		}
	}
}
