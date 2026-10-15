package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery", MinIterations)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$210000$") {
		t.Fatalf("unexpected encoding %s", h)
	}
	if ok, _ := VerifyPassword("correct horse battery", h); !ok {
		t.Fatal("correct password rejected")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery", MinIterations)
	if h == h2 {
		t.Fatal("salt not random")
	}
}

func TestRejectWeakOrMalformedHashes(t *testing.T) {
	if _, err := HashPassword("x", 1000); err == nil {
		t.Fatal("low iteration count accepted")
	}
	for _, bad := range []string{"", "plain", "pbkdf2-sha256$1000$AAAAAAAAAAA$AAAA", "md5$1$a$b", "pbkdf2-sha256$600000$!!$!!"} {
		if _, err := VerifyPassword("x", bad); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%q: expected ErrMalformedHash, got %v", bad, err)
		}
	}
}

func writeUsers(t *testing.T, mode os.FileMode, f *UsersFile) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "users.json")
	if err := SaveUsersFile(p, f); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func sampleUsers(t *testing.T) *UsersFile {
	h, err := HashPassword("a-long-test-password", MinIterations)
	if err != nil {
		t.Fatal(err)
	}
	return &UsersFile{Version: 1, Users: []User{{Username: "admin", PasswordHash: h, Scopes: []string{ScopeSystemRead}}}}
}

func TestUsersFilePermissions(t *testing.T) {
	p := writeUsers(t, 0o600, sampleUsers(t))
	if _, err := LoadUsersFile(p); err != nil {
		t.Fatalf("0600 rejected: %v", err)
	}
	for _, m := range []os.FileMode{0o644, 0o640, 0o602} {
		p := writeUsers(t, m, sampleUsers(t))
		if _, err := LoadUsersFile(p); !errors.Is(err, ErrInsecurePermissions) {
			t.Errorf("mode %#o: expected ErrInsecurePermissions, got %v", m, err)
		}
	}
}

func TestUsersFileRejectsSymlink(t *testing.T) {
	p := writeUsers(t, 0o600, sampleUsers(t))
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUsersFile(link); err == nil {
		t.Fatal("symlinked users file accepted")
	}
}

func TestUsersFileValidation(t *testing.T) {
	f := sampleUsers(t)
	f.Users[0].Scopes = []string{"root.everything"}
	if err := f.Validate(); err == nil {
		t.Fatal("unknown scope accepted")
	}
	f = sampleUsers(t)
	f.Users = append(f.Users, f.Users[0])
	if err := f.Validate(); err == nil {
		t.Fatal("duplicate user accepted")
	}
	for _, bad := range []string{"Root", "a b", "../x", "", strings.Repeat("a", 40), "x\n"} {
		if ValidateUsername(bad) == nil {
			t.Errorf("username %q accepted", bad)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	f := sampleUsers(t)
	h, _ := HashPassword("another-long-password", MinIterations)
	f.Users = append(f.Users, User{Username: "off", PasswordHash: h, Disabled: true})
	s := NewStore(f)
	if _, ok := s.Authenticate("admin", "a-long-test-password"); !ok {
		t.Fatal("valid login rejected")
	}
	if _, ok := s.Authenticate("admin", "nope"); ok {
		t.Fatal("bad password accepted")
	}
	if _, ok := s.Authenticate("ghost", "a-long-test-password"); ok {
		t.Fatal("unknown user accepted")
	}
	if _, ok := s.Authenticate("off", "another-long-password"); ok {
		t.Fatal("disabled user accepted")
	}
}

func TestSessionLifecycle(t *testing.T) {
	m := NewSessionManager()
	now := time.Unix(1_700_000_000, 0)
	m.now = func() time.Time { return now }
	id, s, err := m.Create(User{Username: "admin", Scopes: []string{ScopeSystemRead}}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) < 40 || s.CSRFToken == "" || s.CSRFToken == id {
		t.Fatal("weak or reused tokens")
	}
	for k := range m.sessions {
		if k == id {
			t.Fatal("raw session ID stored server-side")
		}
	}
	if got, ok := m.Get(id); !ok || !got.HasScope(ScopeSystemRead) {
		t.Fatal("session lookup failed")
	}
	now = now.Add(31 * time.Minute)
	if _, ok := m.Get(id); ok {
		t.Fatal("idle session not expired")
	}
	id, _, _ = m.Create(User{Username: "admin"}, "127.0.0.1")
	for i := 0; i < 50; i++ { // keep alive but exceed max lifetime
		now = now.Add(20 * time.Minute)
		m.Get(id)
	}
	if _, ok := m.Get(id); ok {
		t.Fatal("absolute lifetime not enforced")
	}
	id, _, _ = m.Create(User{Username: "admin"}, "127.0.0.1")
	m.DestroyUser("admin")
	if _, ok := m.Get(id); ok {
		t.Fatal("DestroyUser left a session")
	}
	if _, ok := m.Get(strings.Repeat("x", 500)); ok {
		t.Fatal("oversized id accepted")
	}
}

func TestSessionCap(t *testing.T) {
	m := NewSessionManager()
	m.MaxSessions = 3
	for i := 0; i < 10; i++ {
		if _, _, err := m.Create(User{Username: "u"}, "ip"); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.sessions) != 3 {
		t.Fatalf("session table not bounded: %d", len(m.sessions))
	}
}

func TestCSRF(t *testing.T) {
	s := &Session{CSRFToken: "abc"}
	if CheckCSRF(s, "") || CheckCSRF(s, "abd") || !CheckCSRF(s, "abc") {
		t.Fatal("CSRF comparison wrong")
	}
	if CheckCSRF(&Session{}, "") {
		t.Fatal("empty token accepted")
	}
}

func TestThrottle(t *testing.T) {
	th := NewThrottle()
	now := time.Unix(1_700_000_000, 0)
	th.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if !th.Allowed("admin", "10.0.0.1") {
			t.Fatalf("locked too early at %d", i)
		}
		th.Fail("admin", "10.0.0.1")
	}
	if th.Allowed("admin", "10.0.0.1") {
		t.Fatal("not locked after 5 failures")
	}
	if !th.Allowed("admin", "10.0.0.2") {
		t.Fatal("other IP must not be locked by per-user limit")
	}
	now = now.Add(16 * time.Minute)
	if !th.Allowed("admin", "10.0.0.1") {
		t.Fatal("lockout did not expire")
	}
	for i := 0; i < 20; i++ {
		th.Fail("user"+string(rune('a'+i)), "10.0.0.9")
	}
	if th.Allowed("fresh", "10.0.0.9") {
		t.Fatal("per-IP limit not enforced across usernames")
	}
}

func TestThrottleMemoryBounded(t *testing.T) {
	th := NewThrottle()
	now := time.Unix(1_700_000_000, 0)
	th.now = func() time.Time { return now }
	for i := 0; i < maxTracked+500; i++ {
		th.Fail("user", "2001:db8::"+strconv.Itoa(i))
		if i == maxTracked/2 {
			now = now.Add(16 * time.Minute) // first half becomes stale
		}
	}
	if n := th.Tracked(); n > maxTracked+2 {
		t.Fatalf("throttle table unbounded: %d entries", n)
	}
}
