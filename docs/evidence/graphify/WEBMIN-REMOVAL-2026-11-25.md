# Webmin removal: verification (2026-11-25)

Change: ADR 0016 (independent project; `compat/webmin` and the Legacy Webmin feature removed).

## Graphify review
Indexed tree `e95889b1` (the staged change, exported with `git archive`): 661 code files, 7118 nodes,
24718 edges, 317 communities. 0 missing endpoints, no import cycles; the 7 self-loops are recursion.
- No symbol is left with only its definition (repo-wide, and per Go package for unexported names).
  The removal also made the broker's `parseKV` and `types.Check` unused; both were deleted.
- Every frontend `/api/v1` URL matches a route; every broker op the API calls is registered; every
  client deadline key is an op.
- Found and fixed while removing the `legacy` capability: the OpenAPI `Capability.id` enum lacked 9
  existing IDs. New test `TestOpenAPICapabilityEnumMatchesServer` fails without the fix.

## Checks run
- `go vet`, `go test ./...` (non-root): pass.
- Frontend typecheck, ESLint, Prettier: clean. Vitest 312/312.
- `scripts/security-check.sh`: pass. REUSE 3.3 compliant after removing the six licence texts only
  the Webmin tree used. SBOM and notices regenerated and match their inputs. govulncheck, pyspdxtools
  and RustSec are skipped locally (CI runs them).
- Release tooling unit tests: pass. `tests/systemd/test_smoke_repeatability.sh`: 11/11.
- shellcheck v0.9.0 with the gate's file list: clean.

Not run locally: the real-systemd, install-matrix, browser e2e and release dry-run jobs (CI).
