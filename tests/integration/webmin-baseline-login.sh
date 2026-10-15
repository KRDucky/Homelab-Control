#!/usr/bin/env bash
# ok/bad always return 0, so `cond && ok || bad` cannot run both branches.
# shellcheck disable=SC2015
# Baseline startup + login flow test against a running *disposable* upstream
# Webmin instance (see dev/disposable/README.md). Never point this at production.
#
# Env: WEBMIN_URL (default http://127.0.0.1:10000), WEBMIN_USER, WEBMIN_PASS
set -euo pipefail
url=${WEBMIN_URL:-http://127.0.0.1:10000}
user=${WEBMIN_USER:?set WEBMIN_USER}
pass=${WEBMIN_PASS:?set WEBMIN_PASS}
jar=$(mktemp); trap 'rm -f "$jar"' EXIT
pass_n=0; fail_n=0
ok()   { echo "PASS  $*"; pass_n=$((pass_n+1)); }
bad()  { echo "FAIL  $*"; fail_n=$((fail_n+1)); }

# 1. Unauthenticated request shows the login form.
body=$(curl -sS "$url/")
grep -q 'session_login.cgi' <<<"$body" && ok "unauthenticated / serves login form" || bad "login form not served"

# 2. Wrong password is rejected and issues no session.
code=$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -b 'testing=1' -e "$url/" \
  --data-urlencode "user=$user" --data-urlencode "pass=wrong-$RANDOM" "$url/session_login.cgi")
if grep -qE 'sid\s+[0-9a-f]{16,}' "$jar"; then bad "bad password produced a session (HTTP $code)"; else ok "bad password rejected (HTTP $code, no sid)"; fi
: > "$jar"

# 3. Correct password returns a redirect and an HttpOnly sid cookie.
hdrs=$(curl -sS -D - -o /dev/null -c "$jar" -b 'testing=1' -e "$url/" \
  --data-urlencode "user=$user" --data-urlencode "pass=$pass" "$url/session_login.cgi")
grep -qiE '^HTTP/[0-9.]+ 302' <<<"$hdrs" && ok "valid login redirects (302)" || bad "valid login did not redirect"
grep -qiE '^set-cookie: sid=[0-9a-f]+;.*httpOnly' <<<"$hdrs" && ok "sid cookie is HttpOnly" || bad "sid cookie missing or not HttpOnly"
grep -qiE '^set-cookie: sid=.*SameSite=Lax' <<<"$hdrs" && ok "sid cookie has SameSite=Lax" || bad "sid cookie lacks SameSite"

# 4. Authenticated index lists modules.
body=$(curl -sS -b "$jar" -e "$url/" "$url/")
grep -qE 'session_login.cgi' <<<"$body" && bad "still on login page after login" || ok "authenticated index served"
# Framed themes (gray-theme, the in-tree default without Authentic) list modules in left.cgi.
grep -qi '<frame' <<<"$body" && body=$(curl -sS -b "$jar" -e "$url/" "$url/left.cgi")
grep -qE '/(useradmin|proc|mount)/' <<<"$body" && ok "module links present for admin" || bad "no module links in authenticated index"

# 5. Referer check blocks a foreign-origin navigation into a module (CSRF defence).
body=$(curl -sS -b "$jar" -e "https://attacker.example/" "$url/proc/index_tree.cgi")
grep -qiE 'referer|referrer' <<<"$body" && ok "foreign Referer refused by upstream referer check" || bad "foreign Referer not refused"

# 6. Logout invalidates the session.
curl -sS -o /dev/null -b "$jar" -e "$url/" "$url/session_login.cgi?logout=1"
body=$(curl -sS -b "$jar" -e "$url/" "$url/")
grep -q 'session_login.cgi' <<<"$body" && ok "logout invalidates session" || bad "session still valid after logout"

echo "webmin-baseline-login: $pass_n passed, $fail_n failed"
[ "$fail_n" -eq 0 ]
