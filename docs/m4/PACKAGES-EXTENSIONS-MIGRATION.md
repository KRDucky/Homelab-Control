<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->

# Packages migration into Extensions: source-based design

Status: design for review; no backend code has moved. Owner priority is packages,
then services, storage and networking. Packages migration follows the working sample
import. The owner expanded the earlier scope to permit preparing this migration;
package writes remain out of scope.

Inspected baseline: core `dd50867c9792a62f0a90c6588e86f41ed97d0afc`, with sample
integration `23aa73451ac46ae3864061f8e1580753cef1aad4` and direct-build verification
`44aed48c` layered above it. Extensions remains pinned to
`d07eed09c07255b7ad1c14e16a66818bd4c4dd7b`, tree
`281fd0afb21aad133f29c58555bc2982d49650b2`. These references establish the inspected
source, not feature acceptance or production evidence.

## Recommendation and ownership

Move the pure package parsers/model and Software page into the compiled Extensions
source. Keep HTTP handling, authentication, authorization, auditing, module policy,
host discovery, source-file reads, operation dispatch and process execution in core.
The first migration moves product parsing/presentation, not the security boundary.
A separate module service or runtime loader is unnecessary for this slice.

| Existing source | Migration destination / responsibility |
| --- | --- |
| `backend/internal/pkgcenter/{pkgcenter,debian,fedora,suse,arch}.go` | Extensions `plugins/packages/pkgcenter/`, preserving GPL headers, parser algorithms, model JSON tags and errors |
| `backend/internal/pkgcenter/*_test.go` | Parser/security tests move beside the parsers, except the host-discovery integration test below |
| `pkgcenter_test.go:TestOSReleaseFamilySelection` | Core `backend/internal/hostinfo/packages_family_test.go`, external `hostinfo_test` package; it imports core hostinfo and the new pure parser package |
| `frontend/app/src/pages/Software.tsx` | Presentation/state logic moves to Extensions `plugins/packages/ui/Software.tsx`; core retains a small wrapper at the existing path |
| `frontend/app/src/software.test.tsx` | Keep wrapper/integration coverage in core; add extension page tests with injected read-only host functions |
| `backend/internal/broker/types/types.go` | Keep result envelopes in core; replace its parser import so `[]pkgcenter.Package`, `[]Update`, `[]Source` use the moved row types |
| `backend/internal/broker/ops/packages.go` | Keep all operation handlers, fixed source paths/globs, argument decoding, cache timestamps, error translation and audit summaries in core; change only its parser import |
| `backend/internal/api/{server,packages,capabilities}.go` | Keep routes, packages.read, CSRF/Origin checks, actor propagation, audit-before-release, capability and family selection in core |
| `backend/internal/broker/{runner/runner.go,ops/ops.go,server/server.go}` | Keep exact command templates, privilege class, time/output bounds, allowlisted binaries, scopes, module enforcement and dispatch in core |
| `backend/internal/{auth,modules,guard}` | Keep policy and its tests in core; expand guards to cover imported code before moving Go files |
| `backend/internal/api/software_browser_test.go` | Keep the opt-in synthetic HTTP/browser fixture in core and update its parser import; it must not become production data |
| `frontend/app/src/App.tsx` and `api.ts` | Keep HTTP/session transport and route approval in core; App continues selecting the core wrapper for module packages |
| `schemas/openapi/homelab-control.v1.yaml` | Keep the existing public API contract in core; migration does not add or change routes |

The five production parser files import only stdlib: strings, errors, fmt, regexp,
unicode, unicode/utf8 and encoding/xml. Their only core dependency is in a test:
`pkgcenter_test.go` imports `internal/hostinfo`. Current parser consumers outside
that folder are `broker/types/types.go`, `broker/ops/packages.go`, and the browser
fixture `api/software_browser_test.go`. This test-only dependency must be split
before the Extensions tests can stand alone.

## Go module boundary and acyclic imports

Core's Go module is `homelab-control/backend`, rooted in `backend/`. The subtree is
its sibling, outside that module. Moving Go source to `extensions/` without a
module arrangement leaves it outside `go test ./...` and normal module resolution.
Go's internal-package rule also prevents a separate Extensions import path from
importing `homelab-control/backend/internal/hostinfo`, `api`, broker types or runner.
Exporting names does not remove that restriction. Do not copy internal core code
into Extensions or use a reverse require/replace to bypass it.

Add a stdlib-only module at Extensions root, with no core dependency:

```go
module github.com/KRDucky/Homelab-Control-Extensions

go 1.26.0
```

Core `backend/go.mod` gains this local, reviewed source dependency:

```go
require github.com/KRDucky/Homelab-Control-Extensions v0.0.0
replace github.com/KRDucky/Homelab-Control-Extensions => ../extensions
```

The local replacement resolves only the imported tree; `extensions.lock`, rather
than v0.0.0, pins its provenance. No remote module fetch is needed for Extensions.
Do not add a root go.work, any dependency from Extensions to core, or additional
third-party dependencies. Check both modules explicitly with GOWORK=off and the
pinned local Go toolchain; core's ./... does not test a nested/replaced module.
Production imports become:

```text
core api ------------------> core broker/types --------> Extensions pkgcenter
core broker/server --------> core broker/ops -----------> Extensions pkgcenter
                                    |
                                    +------------------> core broker/runner
Extensions pkgcenter ----------------------------------> stdlib only
core hostinfo_test ---------> core hostinfo + Extensions pkgcenter
```

`pkgcenter` remains a public package path within the Extensions module, not an
Extensions `internal/` directory. It must not import core result envelopes: that
would be both an internal-package violation and a types -> parser -> types cycle.
Keep the envelope structs in core and the existing row structs in the pure parser
package. No generic broker client, runner interface or plugin registration hook is
passed into this parser. Core chooses the family and invokes its pure functions.

## Frontend host interface and core contribution approval

Today Software imports core `../api`, `../DataTable`, and `../useData` directly.
Do not reproduce those relative imports from the subtree or import the app shell
back into the generated registry. Define an Extensions-owned typed host interface
in `plugins/packages/ui/host.ts`, with only:

- `client.search(q)`, `installed()`, `updates()`, `sources()`: promises of the existing
  JSON result shapes. No caller-supplied URL, HTTP method, scope, command or argv.
- A generic `renderTable<Row>(TableSpec<Row>)` function with the caption, rows,
  rowKey and column renderers used by Software. Core adapts its existing DataTable.
- Error description/classification callbacks and `onUnauthorized`. Core maps its
  existing ApiError class; the page clears stale data for 401/403 as it does now.

The core wrapper binds its existing API functions to the current CSRF token. It
keeps token/cookie handling and request construction in core; Extensions gets no
session store, token, broker handle, generic HTTP client or arbitrary host access.
Memoize the bound client by CSRF token and keep callback identities stable, so an
injected-props refactor does not cause refetch loops or skip session-change reloads.
Do not add filter/sort controls, sortValue callbacks, client-side search rewrites or
new package security classifications. Keep query.trim(), encodeURIComponent and
family-specific search templates exactly as they are. The moved page imports only
React and its local contracts. The dependency direction
is App -> core Software wrapper -> static registry -> Extensions page -> React/local
contracts; the imported page never imports App, the registry or core API modules.
React remains core's single dependency instance; retain Vite dedupe and TypeScript
resolution for the sibling source. Do not add a second frontend package install.

Add a new declarative package manifest only with the reviewed import. Proposed ID
is `dev.homelab-control.packages-readonly`, version 1.0.0, licence
GPL-3.0-or-later, contribution `software.packages-page`. No routes, scopes, commands,
paths or permission declarations are accepted from that manifest. Extend the known
ID enum in core `plugin.ts` and `build-plugin-sdk.mjs`, then regenerate SDK/schema.
Core approves its export, source hashes and a typed module-page registration for
module `packages` at `/software`; Extensions cannot choose that binding.

Keep module-page and overview component types separate in the generator: the new
page needs injected props, whereas the sample card does not. The core overview
renderer and `hc.plugins.v1` browser-local enablement must continue to operate on
overview IDs only. Do not put the Software page through that hook or let an uploaded
manifest activate an API/module. Package route/capability enablement remains core's
existing modules.json and packages.read behavior; adding a declarative page ID does
not grant either. Existing manifest v1 fields suffice for a known static ID; richer
manifest permissions/routes remain a separate schema/ADR decision.

## Behavior to preserve, and limits observed in source

Preserve the four GET endpoints under `/api/v1/packages`: search, installed,
updates and sources, and the same four `packages.*` broker operations. Search is
validated at the API for nonempty length <=64, then at the broker/runner for the
closed ASCII grammar and no leading option. Family selection is core os-release
logic. No API, client or template may install, remove, upgrade, refresh metadata,
change repositories, import keys or acquire root execution for this migration.
`packages.read` is known but not a default or implied scope; packages.manage is
not enabled. Core checks module disablement on routes and broker operations. A missing
modules.json keeps the current default-on behavior; malformed settings or unknown
module IDs fail closed. Do not substitute the sample browser-local plugin store.

Keep core's session checks and package GET CSRF helper, including current handling
of absent and literal null Origin. The helper rejects other disallowed origins.
Do not describe this as rejecting every request without Origin: source intentionally
permits same-origin GETs that omit it. Preserve both access audits (counts/query
length, never query text or package lists) and refusal when audit is unavailable.

Keep all 13 fixed package query templates unchanged: dpkg-query/apt-cache/apt,
rpm/dnf, zypper, pacman. dnf uses -C; zypper uses --no-refresh. Core runner checks the
binary at use time, replaces the environment, uses /dev/null stdin, caps output and
runs queries as the unprivileged service identity. Cache-only intent is encoded by
those commands/flags; source inspection does not prove an OS network sandbox or
real-distro behavior. Fixed source-file reads remain in core. Cache-mtime errors,
unknown families, missing tools, malformed parser output and nonzero tool exits
retain their existing failure semantics and messages.

The existing capability probe checks the family's primary installed-query binary,
not every per-tab binary; a missing apt-cache/dnf may still fail a particular tab.
Source Signed/ThirdParty fields are configuration/hostname classification, not
cryptographic repository verification. Preserve deb822 parsing, signed fail-closed
heuristics, credential stripping, size/line/entry budgets and dedupe identities.
These are implemented source paths with tests; no release matrix result is implied.

Preserve the page's initial no-search state, submit-only search, four mounted
ARIA-associated tabpanels, exact labels/IDs, responsive tables, source flags, error
states, unauthorized callback, query encoding and metadata-refresh timestamp.
The existing UI row keys omit version for installed rows and origin for updates;
parsers can preserve more identities than those UI keys distinguish. Record that
as a parity review risk; do not silently change it during the extraction. Likewise,
core currently treats every nonzero tool exit as unavailable; interpreting dnf's
or pacman's special statuses is a separately tested behavior correction, not part
of a file move. No new write controls may appear, including disabled controls.

## Import restrictions and guard changes required before backend moves

The sample's generator currently rejects Go files, go.mod, additional TS/TSX and
unapproved code bytes. It must continue doing so until core explicitly approves the
packages import. Do not relax to a subtree-wide source glob. Add an exact inventory
of allowed package source/test/contract files and SHA-256 hashes, reviewed in core,
including go.mod. Keep the UI export approval distinct from approval of non-UI Go
source. Root module metadata must declare only the expected module and Go version,
with no extra require/replace/toolchain directives; do not execute imported build
scripts, init registration, cgo, go:generate or linkname directives.

`guard.TestNoProcessExecution` currently walks only backend. Its reachability check
follows only `homelab-control/backend/` imports. Both must cover the fixed Extensions
module root before code moves, without adding Extensions to allowedExecRoots.
Resolve only the two reviewed module prefixes to their known filesystem roots.
Walk all relevant build-tag variants, not merely the active host's selected files.
Parser imports must remain on the observed stdlib allow-list (and tests on their
explicit stdlib test allow-list); core imports, network/file/process APIs, unsafe,
plugin, cgo and external modules are refused. Inspect transitive first-party imports
so hc-api/hlctl cannot hide a dependency on broker ops/runner/server through Extensions.
Extend source ownership/root-exec guards without granting any new execution root.

Negative guard fixtures must demonstrate: reverse core/internal import, a types ->
parser cycle, os/exec or net/http in imported source, an unapproved Go file/build tag,
changed module replacements, unapproved component bytes, unknown contribution ID,
and any new write operation/template. A Go import graph/compiler check is required
in addition to source scans; Go itself catches cycles and internal-package violations.
Preserve GPL attribution throughout; the sample's exact MIT annotations must not
expand over the new GPL package source, page or contracts.

## Sequenced implementation and evidence gates

1. Finish sample verification: SDK/plugin tests, frontend lint/build, generated SDK
   and registry checks, subtree verification and all tampering checks. Current
   dependency-install failure is recorded below; do not call the sample accepted.
2. In core, implement the explicit code inventory, page host contract/adapter and
   expanded guard coverage, with failing negative fixtures before permitting the
   new Go/module files. Keep this commit buildable without migrated code.
3. In an isolated Extensions branch from the reused bootstrap, move the five parser
   sources and parser tests, splitting out the hostinfo integration test; add its
   minimal module, page/contracts, manifest and provenance notes. Move algorithms
   unchanged. Validate the Extensions module independently. No core backend code is
   moved during this design step.
4. Pin the reviewed Extensions commit/tree via subtree --squash and regenerate the
   typed static registry. Update the two production parser imports and the browser
   fixture import. Add the core hostinfo integration test, wrap the imported page,
   remove duplicate parser/page implementations, retain the SDK example and sample.
5. Run both Go module test suites explicitly, vet/build all three core binaries,
   expanded guard/negative tests, API/auth/module tests, UI tests, generated SDK and
   registry checks, subtree verification, lint/build and release/source-bundle checks.
   Include the local replace target in source packaging so a release source tree
   builds offline with the imported module. No new deploy or host package operation.
6. Run the existing opt-in software browser fixture + axe in its disposable setup
   and confirm preserved DOM/behavior. Pair it with operation argv snapshots, audit
   failures and disabled-module tests. Use fixtures/mocks only: do not invoke real
   package managers, install anything, add repositories, refresh host metadata or
   alter host configuration. Publish draft PRs, never merge or add the owner's DCO
   sign-off.

Required commands include `(cd extensions && GOWORK=off GOTOOLCHAIN=local go test
-count=1 ./...)` and the same environment for core `go test -count=1 ./...`, go vet
and builds. Target core packages include internal/guard, auth, modules, hostinfo,
broker/runner, broker/ops and api. Keep `software.test.tsx` and the wrapper's 401/403,
module-disabled, missing-tool/cache and audit refusal coverage. Test the installed
source layout, not a temporary workspace arrangement that hides a missing module.

## Corrected Qwen acceptance review

The owner supplied the corrected checklist during this review. It is review input,
not evidence of implemented features. The following entries come from actual source
and test assertions. Existing Go/Vitest tests were inspected, not executed here:
the toolchain/dependency blockers below apply to every row. No packages code has
changed, so there is no implemented migration to assess for regressions yet.
"Not run" in each regression column is intentional, not a parity pass.

| Criterion | Existing source / coverage inspected | Migration regression status and risk | Necessary missing fixture/mock tests |
| --- | --- | --- | --- |
| C01: search, installed, pending updates, sources | Software has four tabs; api/packages.go and broker ops have four reads. software.test.tsx has four tests for tabs/read-only controls, installed/source flags and update timestamp. API tests exercise installed and search success, plus installed refusal. | Not run; moving only the visible page could drop client wiring for a tab or lose core envelopes. | Successful API envelopes for updates/sources; imported page and wrapper actually invoke each of the four clients; initial Search makes zero calls and submit renders results. Keep all existing assertions. |
| C02: unchanged search semantics; no filters/sorting/classification | onSearch uses query.trim(); api.ts encodes q; core selects unchanged family templates. TestDebianSearch and family parser fixtures assert parser output; TestDecodePackagesArgs and TestValidPackageQuery cover allowed/refused syntax. Software columns have no sortValue. | Not run; a new host adapter might normalize q, add sorting or interpret literal queries differently. | Assert exact trimmed query passed once, encoded URL and CSRF header, same ordered fixture rows for every family, repeated/empty submission behavior and original errors. Assert no new filter/sort/classifier controls or extra requests; do not change matching or dedupe rules. |
| C03: supplied version/architecture; unavailable versus empty | Parser fixtures compare typed rows; TestPackagesInstalledReadsAndAuditsCountsOnly asserts version reaches JSON. Software renders Version/Architecture columns and missing-value dashes. UI test fixtures supply arch but do not assert it. A supplied empty-string version currently renders blank because ?? only handles null/undefined. | Not run; lossy DTO remapping or a fallback to [] could erase fields or convert a failure into empty success. The empty-string display issue is pre-existing. | Assert supplied version/arch in installed/search/update tables; cover missing and empty strings separately. Test [] success separately from undefined/unavailable fields, rejected promises and missing cache/tool responses. Record any needed empty-string display correction as a focused baseline fix, not a parser move. |
| C04: distinguish installed/candidate versions when supplied | Update rows contain current/new; SoftwareUpdates labels Installed/Available and uses a dash when current is unavailable. Debian/Arch/SUSE fixtures include both; Fedora ParseUpdates can supply New without Current. software.test.tsx asserts 1.2.4, not both version labels/values. Search models have one Version, not an invented installed/candidate pair. | Not run; a new model/adapter could collapse current/new or invent the missing current version. | Mock different current/new and assert both labels and values; missing-current case must remain unavailable, not zero/empty-result or copied candidate. Assert arch and existing fields survive transport. Do not invent additional version discovery or a new search model. |
| C05: cached and possibly stale; no auto refresh | Page banner says cache is never refreshed/changed here. Updates show as_of when provided. software.test.tsx checks the update timestamp. TestPackageQueriesAreClosed asserts -C/--no-refresh and refuses known mutating verbs. No explicit may-be-stale wording exists in the current banner. | Not run; changed effect dependencies could issue extra calls or a migration could add refresh behavior. Explicit staleness copy is an existing checklist gap. | Assert zero automatic refresh calls on mount/tab/search/retry; mock old/missing timestamps; keep cache-missing refusal distinct from no updates. Test expected cached/stale explanation if addressed in a separate copy correction. No fixture may touch host metadata. |
| C06: third-party origin and signing separate; signed is not trusted | API has separate signed/third_party booleans. SoftwareSources has Origin and independent unsigned/third-party tags in a shared Flags column. TestThirdPartyHostIsNotSubstringMatch and TestSignedFailsClosed cover existing classification logic. UI test covers only unsigned + third-party; signed=true has no explicit positive status label. No trusted label is rendered. | Not run; injected DTOs might conflate flags or add a new trust classifier. More explicit signing presentation is a baseline review gap, not permission to change classification. | Fixtures for all four signed/third_party combinations, preserving Origin separately; assert signed third-party never becomes trusted/vendor and has no install/use action. Test explicit signing status if separately corrected. Keep existing classification algorithms byte-identical during migration. |
| C07: strip repository userinfo, query and fragment | stripCredentials strips userinfo and ?/#; all family Source.Origin construction calls it. TestStripCredentials covers userinfo/query/plain path; TestDebianDeb822Sources checks parser-level redaction for userinfo/query. The helper test has no fragment case. | Not run; moving models or adding rendering fallback could expose raw parser input or unsanitized source data. | Helper fragment and combined userinfo/query/fragment cases; parser-level fixtures for Debian one-line/deb822, Fedora baseurl/mirrorlist, SUSE XML and Arch sources; API/UI mock propagation asserts sentinel credentials/tokens absent from JSON, DOM, errors and audit records. Do not move redaction responsibility into the renderer alone. |
| C08: packages.read and independent broker module enablement | TestPackagesRequiresSessionAndScope and TestPackagesRequiresCSRFAndOrigin exercise the API. TestPackagesReadNotGrantedByDefaultOrImplied in auth/users_logs_test.go checks known/default/implied scopes. modules_test.go covers packages prefix ownership. Generic disabled-route and broker tests currently target services; App renders an explicit disabled-module page from core capability metadata. | Not run; treating the page like the browser-local sample could bypass server module policy or change defaults. | Parameterize auth/scope/CSRF checks over all four reads; API disabled packages yields 404 without broker call. Independently send each packages.* broker request with packages.read while packages is disabled and assert module_disabled/no runner. Add packages-specific disabled navigation/page mocks; check missing/invalid modules.json defaults unchanged. |
| C09: distinguish denial, disabled, unsupported, missing tool and execution failure; never empty success | Core has 403, module-disabled 404, unsupported/missing/failure 503 and timeout 504. brokerError reuses code unsupported for CodeUnavailable but preserves reason text. App handles disabled metadata and unavailable capabilities; explain preserves 503/504 messages and gives permission text for 403. A raced/unknown 404 inside Software falls through to generic server-failure text. TestPackagesUnsupportedFamilyIsNotEmptySuccess, TestPackagesUnsupportedFamily, TestPackagesMissingTool and TestPackagesUpdatesRequiresCache cover subsets. | Not run; an adapter could swallow a rejection or erase error identity. A mid-session disabled 404 message and generic automatic-retry wording are existing presentation limitations; normal App disabled state is already distinct. | Table-driven API/UI mocks for denial, disabled metadata/direct 404, unsupported, missing tool, missing cache, timeout, execution failure and actual empty success for all four reads. Assert distinct appropriate states/messages, no successful empty rows on failures, 401 callback and 401/403 stale-data removal. Do not weaken deliberate unknown/disabled route indistinguishability at the API; use core module metadata for page explanation. |
| C10: closed argv, bounded input, cache flags, fixed errors/no stderr | TestPackageQueriesAreClosed and TestValidPackageQuery cover template IDs, bad query syntax, flags and mutating-verb refusal. Generic runner tests cover replaced env, output cap and timeout. runPackageQuery emits fixed binary/exit messages, never res.Stderr. packages_test.go has no direct stderr-sentinel/nonzero-output test or exact argv snapshot for every template. | Not run; moving orchestration/templates would risk broadening process access, so they stay core. | Preserve all 13 exact argv snapshots/query positions and op scope/privilege metadata. Mock execution with sentinel stdout/stderr, nonzero exit, timeout, output cap, missing tool and malformed output; assert fixed public errors and no stderr/query/package leaks in API/audit. Add remaining length/unknown-field/trailing-data boundaries. Never call a real package manager. |
| C11: keyboard, labels, loading/error/empty states | Native buttons/input, software-tab/panel IDs, hidden mounted panels, loading text, alert and empty states exist. software.test.tsx checks roles with click events; software_browser.py checks the synthetic page with axe, not a full keyboard flow. No Software arrow-key tab handler is present. | Not run; page extraction/host table injection could break focus, ARIA associations or state transitions. Full tab-pattern keyboard behavior is not proved by native button presence. | Fixture/mock keyboard Tab/Shift+Tab/Enter/Space flows; verify all aria-controls/labelledby pairs, selected/hidden panels, loading, successful-empty, failed and permission states, cancellation on tab switch/unmount and unauthorized callback. Add axe on the imported page. If full arrow-key tab behavior is required, track the existing gap as a separate focused accessibility correction. |

Every test above uses fixture data, fake broker/client responses and temporary test
roots. Do not install packages, add repositories, refresh metadata, read a real host
package cache or change host configuration. No test result for packages is claimed
in this design. Existing checklist gaps are not migration regressions: log them
separately, add tests that expose them, and address necessary baseline corrections
in focused commits without filters, sorting or new security classifications.

At design time, the five SDK tests and all five sample negative checks pass again;
registry and subtree checks pass. Direct npm build's new prebuild check passes and
rejects registry drift before compilation. The compiler/build, frontend tests/lint
and SDK generated-file checks remain blocked: npm ci --ignore-scripts fails with
EAI_AGAIN registry.npmjs.org and TypeScript/Vitest/ESLint are absent. `go version`
fails with command not found. These are incomplete checks, not passes. Backend
migration is not started while the sample completion gate remains unverified.
