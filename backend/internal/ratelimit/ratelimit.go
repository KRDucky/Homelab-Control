// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ratelimit provides process-local, in-memory rate limiters for the
// API layer. They are deliberately fail-open: when internal maps are reset to
// bound memory, they allow the request rather than ever denying a legitimate
// one. State resets on restart (documented limitation).
package ratelimit

import (
	"sync"
	"time"
)

const defaultMaxKeys = 50_000

// TokenBucket is a per-key token bucket with a fixed burst and a linear
// refill of one token per Refill interval.
type TokenBucket struct {
	mu      sync.Mutex
	Burst   int
	Refill  time.Duration
	maxKeys int
	state   map[string]*bucketState
	now     func() time.Time
}

type bucketState struct {
	tokens float64
	last   time.Time
}

// NewTokenBucket returns a bucket with the given burst and refill interval.
func NewTokenBucket(burst int, refill time.Duration) *TokenBucket {
	return &TokenBucket{
		Burst: burst, Refill: refill, maxKeys: defaultMaxKeys,
		state: map[string]*bucketState{}, now: time.Now,
	}
}

// Allow consumes one token for key. When not ok, retryAfter is the time until
// the next token is available (never more than Refill).
func (b *TokenBucket) Allow(key string) (ok bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	st := b.state[key]
	if st == nil {
		if len(b.state) >= b.maxKeys {
			b.prune(now)
		}
		st = &bucketState{tokens: float64(b.Burst), last: now}
		b.state[key] = st
	}
	st.tokens += float64(now.Sub(st.last)) / float64(b.Refill)
	if st.tokens > float64(b.Burst) {
		st.tokens = float64(b.Burst)
	}
	st.last = now
	if st.tokens >= 1 {
		st.tokens--
		return true, 0
	}
	d := time.Duration(float64(b.Refill) * (1 - st.tokens))
	return false, d
}

// prune drops fully-refilled, idle entries. If the map is still full it is
// reset: the bucket fails open rather than evicting at random or growing
// without bound.
func (b *TokenBucket) prune(now time.Time) {
	for k, st := range b.state {
		if now.Sub(st.last) >= time.Duration(b.Burst)*b.Refill {
			delete(b.state, k)
		}
	}
	if len(b.state) >= b.maxKeys {
		b.state = map[string]*bucketState{}
	}
}

// Refund returns one token to key (capped at Burst). It is used when a
// request that consumed a token is rejected later, for example by global
// concurrency saturation, so the rejection does not drain the caller's quota.
func (b *TokenBucket) Refund(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[key]
	if st == nil {
		return
	}
	if st.tokens < float64(b.Burst) {
		st.tokens++
	}
}

// Keys reports the number of tracked keys (tests/metrics).
func (b *TokenBucket) Keys() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.state)
}

// Preauth limits unauthenticated audit events by source IP and globally, and
// aggregates suppressed events into a summary flushed at most once per
// SummaryInterval. The suppressed counter is a single global int and is not
// lost when the per-IP map is pruned or reset.
type Preauth struct {
	mu              sync.Mutex
	PerIP           time.Duration
	Global          time.Duration
	SummaryInterval time.Duration
	perIP           map[string]time.Time
	lastGlobal      time.Time
	suppressed      int
	lastSummary     time.Time
	maxKeys         int
	now             func() time.Time
}

// NewPreauth returns a limiter with the given per-IP, global and summary
// intervals.
func NewPreauth(perIP, global, summaryInterval time.Duration) *Preauth {
	return &Preauth{
		PerIP: perIP, Global: global, SummaryInterval: summaryInterval,
		perIP: map[string]time.Time{}, maxKeys: defaultMaxKeys, now: time.Now,
	}
}

// Allow reports whether an unauthenticated event may be written now. When
// false the event is counted as suppressed.
func (p *Preauth) Allow(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if now.Sub(p.lastGlobal) < p.Global {
		p.suppressed++
		return false
	}
	if last, ok := p.perIP[ip]; ok && now.Sub(last) < p.PerIP {
		p.suppressed++
		return false
	}
	if len(p.perIP) >= p.maxKeys {
		p.prune(now)
	}
	p.perIP[ip] = now
	p.lastGlobal = now
	return true
}

// Summary returns the accumulated suppressed count (and resets it) once the
// summary interval has elapsed since the previous flush, when there is
// something to report.
func (p *Preauth) Summary() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.suppressed == 0 || now.Sub(p.lastSummary) < p.SummaryInterval {
		return 0, false
	}
	n := p.suppressed
	p.suppressed = 0
	p.lastSummary = now
	return n, true
}

// prune drops stale per-IP entries. The global window and the suppressed
// counter are independent and survive a reset, so the global 1-per-interval
// cap and the summary are not defeated by key churn.
func (p *Preauth) prune(now time.Time) {
	for ip, t := range p.perIP {
		if now.Sub(t) >= p.PerIP {
			delete(p.perIP, ip)
		}
	}
	if len(p.perIP) >= p.maxKeys {
		p.perIP = map[string]time.Time{}
	}
}

// Keys reports the number of tracked source IPs (tests/metrics).
func (p *Preauth) Keys() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.perIP)
}

// OncePer reports true at most once per key per Interval (a fixed window),
// used to bound summary events.
type OncePer struct {
	mu       sync.Mutex
	Interval time.Duration
	last     map[string]time.Time
	maxKeys  int
	now      func() time.Time
}

// NewOncePer returns a fixed window with the given interval.
func NewOncePer(interval time.Duration) *OncePer {
	return &OncePer{Interval: interval, last: map[string]time.Time{}, maxKeys: defaultMaxKeys, now: time.Now}
}

// Allow reports whether key has not been seen within the interval.
func (o *OncePer) Allow(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	if t, ok := o.last[key]; ok && now.Sub(t) < o.Interval {
		return false
	}
	if len(o.last) >= o.maxKeys {
		o.prune(now)
	}
	o.last[key] = now
	return true
}

func (o *OncePer) prune(now time.Time) {
	for k, t := range o.last {
		if now.Sub(t) >= o.Interval {
			delete(o.last, k)
		}
	}
	if len(o.last) >= o.maxKeys {
		o.last = map[string]time.Time{}
	}
}

// CeilSeconds returns d as a positive integer number of seconds (at least 1)
// for a Retry-After header.
func CeilSeconds(d time.Duration) int {
	s := (d + time.Second - 1) / time.Second
	if s < 1 {
		s = 1
	}
	return int(s)
}
