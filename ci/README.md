<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# Jenkins CI (MediaMouse)

`Jenkinsfile` runs this repository's existing gates on the self-hosted Jenkins. It is an extra,
parallel CI. `.github/workflows/ci.yml` stays the CI that pull requests are judged by. Jenkins
does not replace it until the comparison below shows equal coverage and the owner decides.

Nothing in the pipeline publishes, tags, signs, deploys or uses a credential. Checkout uses the
`github-jenkins-app` GitHub App credential, held by the job, not by the Jenkinsfile.

## How it runs

- **Gates:** every gate is an existing Make target or script. `ci/check.sh` is the GitHub `check`
  job's step list, in the same order. `ci/release-dry-run.sh` is the dry-run mode of
  `release-build.yml`. Keep both in step with those workflows.
- **Shared changes:** the fuzz commands are now `make fuzz`, and the `.deb` compliance step is
  `scripts/deb-compliance.sh`. GitHub Actions runs both too.
- **Containers:** `ci/run.sh` streams a copy of the checkout, with `.git`, into a throwaway
  container on the Docker agent's **rootless** daemon. It copies the named outputs back,
  removes the container and returns the command's exit status. It refuses any daemon that is
  not rootless. It never bind-mounts a host path or uses `--privileged`. Without `--root`, the
  command runs as an unprivileged user with every capability dropped. `--root` is used only for
  the install test, as root inside a throwaway distro container. CPU, memory and PID limits
  apply to every container.
- **Toolchain image:** `ci/Dockerfile`, built from official images only.
  - Go, Node and Rust versions come from `packaging/release/toolchain.env`.
  - The govulncheck, spdx-tools, REUSE and Playwright versions come from
    `.github/workflows/ci.yml`.
  - Each version is read where it is already pinned; a missing pin stops the build.
  - The image tag is a hash of the Dockerfile and the pins, so an image is built once per
    toolchain change. Every run checks the toolchain with
    `release-tool.py check-toolchain`.
- **No dependency caches:** as on GitHub, dependencies come from the lockfiles on every run.
  Only the toolchain image is reused.
- **CI mode:** `HC_CI=1` turns on CI mode, as `GITHUB_ACTIONS` does:
  - in `scripts/security-check.sh`, every scanner must be installed and the RustSec database is
    fetched. Before this, a run outside GitHub Actions skipped the RustSec check without failing;
  - in `tests/integration/run-hc-e2e.sh`, the browser tests must run. Before this, they were
    skipped with a passing result when Playwright could not be imported, which the first Jenkins
    build showed.

## Stages

| Stage | Agent | Runs | Output |
|---|---|---|---|
| graphify | docker | `ci/graphify-stage.sh`: Graphify impact review of the commit under test (below) | `reports/graphify/` |
| check | docker | `ci/check.sh` in the toolchain image | binaries, `.deb`, inventory and lintian files; Vitest JUnit; Go coverage |
| release dry run | docker | `ci/release-dry-run.sh`: two `release-bundle.sh` runs in fresh containers, then the `verify` steps | `dist/candidate`, `dist/notes` |
| install matrix | docker | `tests/packaging/install-test.sh` in each image of ci.yml's install-matrix (read from ci.yml) | – |
| browser e2e | docker | `tests/integration/run-hc-e2e.sh` in the e2e image (Playwright, Chromium, Firefox, WebKit) | – |

- **Order:** `graphify` runs first; the later stages need the `check` stage's build, passed with
  `stash`/`unstash`. The docker agent has one executor, so the later stages run one after
  another. With more executors they run in parallel without changes.
- **Pull requests from forks:** the pipeline stops on them (`CHANGE_FORK`). The job must also
  not discover them (see below).
- **Failures:** each stage keeps its exit status, and the build ends FAILURE when any stage
  fails.

## Graphify stage

`docs/project/08-TESTING-AND-ACCEPTANCE.md` requires a Graphify review of every code change, with
the indexed commit checked against the commit under test. The `graphify` stage produces the
evidence for it on every build:

- **What:** `ci/graphify_review.py` indexes the commit under test, without `compat/`, with
  Graphify's AST extraction. That needs no model, no network and about 10 seconds. For every file
  changed since the base it lists the definitions in it, the files that use them, the files they
  use and the linked tests.
- **Base:** the pull request's target, as Jenkins merged it. For a branch build it is the first
  parent, so a branch build reviews only its last commit. In pre-push mode it is the pinned `main`.
- **Index check:** the index is of `HEAD` exactly. A modified working tree, or a `HEAD` that is not
  the commit under test, fails the stage. `INDEXED_SHA` and `graph.json.gz` record what was indexed.
  Every later stage checks that it tests the same tree.
- **Result:** `STATUS` is one of:
  - `complete`: every changed Go, TypeScript, JavaScript, Python or Rust file has nodes in the
    graph;
  - `not-applicable`: no such file changed;
  - `incomplete`: one of them has no node. The build is then **UNSTABLE**, not green.
  Changed files of a type Graphify does not parse, such as shell, Markdown or YAML, are listed in
  `IMPACT.md` under "review by reading".
- **Not a verdict:** `IMPACT.md` says what a change touches. A person or agent still reads the
  changed definitions and their dependents, records findings and fixes the confirmed ones. A
  missing edge does not prove that nothing depends on a definition.
- **Pin:** Graphify and every dependency are pinned by hash in `ci/graphify-requirements.txt`, and
  installed in the toolchain image under `/opt/graphify`. Licences are listed in that file. It is
  CI only; nothing of it is shipped.

## Pre-push testing (Jenkins + n8n)

This runs the full Jenkins gate, Graphify first, on a commit **before** it is pushed to GitHub.

```text
git push
  └─ pre-push hook: scripts/prepush-ci.sh hook
       ├─ status?  ──► n8n "HC pre-push" ──► Jenkins API: newest Homelab-Control-prepush build for the commit
       └─ submit   ──► n8n stores <sha>-<nonce>.bundle and starts Homelab-Control-prepush
                           └─ every stage: fetch the bundle from n8n, merge with main (ci/prepush-checkout.sh)
                              graphify ─► check ─► release dry run, install matrix, browser e2e
  the push goes through once the run for that exact commit passed
```

- **Client:** `scripts/prepush-ci.sh` (`submit`, `status`, `wait`, `install-hook`, `hook`).
  - It bundles only the commits that are not on GitHub's `main` (`git bundle ... --not origin/main`).
  - The pre-push hook acts only on pushes to `github.com/KRDucky/Homelab-Control`. It refuses a
    push of commits that have no passing run. A commit with no run is submitted, and the hook
    asks you to push again once it passes.
  - A run passes when Jenkins' pre-push build of that exact commit is SUCCESS, its
    `ci-result.json` names the commit, and Graphify is `complete` or `not-applicable`.
  - `HC_PREPUSH_SKIP=1 git push` or `git push --no-verify` skips the hook. The pull request is
    still judged by GitHub Actions, and Jenkins still builds it as usual.
- **n8n workflow:** `ci/prepush/n8n-workflow.json`, generated by `ci/prepush/gen-n8n-workflow.py`.
  Edit the generator, not the JSON; a test fails when they differ. It has three webhooks:
  - `POST /webhook/hc-prepush/submit?sha=&branch=&nonce=` (header auth): validates the input,
    stores the bundle as `<sha>-<nonce>.bundle`, and starts the Jenkins job with `buildWithParameters`.
  - `GET /webhook/hc-prepush/status?sha=` (header auth): finds the newest build of the job for
    that commit, or its queue item, and returns the state, `ci-result.json` and `pass`.
  - `GET /webhook/hc-prepush/bundle?name=` (no auth): the Jenkins agent fetches the bundle. The
    name must be exactly `<40 hex>-<32 hex>.bundle`. The random nonce, made by the client, is the
    only key, and the name is never used to build any other path.
  - Successful executions are not saved (`saveDataSuccessExecution: none`), so bundles do not
    collect in n8n's database.
- **Jenkins job:** `Homelab-Control-prepush`, from `ci/prepush/jenkins-job.xml`. It runs the same
  root `Jenkinsfile` from GitHub's `main`, cloned anonymously, so it holds no credential and
  publishes no GitHub check. When `HC_PREPUSH_SHA` is set, every stage calls
  `ci/prepush-checkout.sh`. That script:
  - fetches and verifies the bundle;
  - checks that it holds exactly `HC_PREPUSH_SHA`;
  - merges it with `main` as GitHub would merge a pull request. The merge has a fixed author and
    date, so every stage tests the same commit, and `main` is pinned by the first stage;
  - fails on a merge conflict.

  The job keeps `disableConcurrentBuilds(abortPrevious: true)`: a new submission aborts a running
  one, whose status then becomes `ABORTED`.
- **Trust:** the pre-push job runs code that nobody but its author has reviewed, on the persistent
  agents. That is the same trust as a same-repository pull request: only holders of the n8n header
  credential can submit, and fork pull requests are still never built.

### Setup (once, by the owner)

1. **Jenkins job.** As an administrator (the CI agent's token cannot create jobs):

   ```sh
   curl -u ADMIN:TOKEN -H 'Content-Type: application/xml' --data-binary @ci/prepush/jenkins-job.xml \
     "$JENKINS_URL/createItem?name=Homelab-Control-prepush"
   ```

   Give n8n its own Jenkins user with Overall/Read, and Job/Read and Job/Build on this job, and an
   API token. Don't reuse a coding agent's token.
2. **Bundle directory.** `mkdir /containers/appdata/n8n/files/hc-prepush`. It must be the n8n
   container's `/files/hc-prepush`, and n8n's file access must allow it
   (`N8N_RESTRICT_FILE_ACCESS_TO`). If the container maps it elsewhere, change `bundleDir` in the
   generator.
3. **n8n.**
   - Import `ci/prepush/n8n-workflow.json`.
   - Create two credentials and select them in the nodes that show a missing credential:
     - "HC pre-push client": Header Auth, name `X-HC-Prepush-Token`, a long random value;
     - "Jenkins hc-prepush": Basic Auth, the Jenkins user and API token.
   - Activate the workflow. `jenkinsUrl` and `n8nUrl` in the generator are `192.168.1.44`, since
     the agents reach n8n there and not on `localhost`.
4. **Client.** Write `~/.config/hc-prepush/env` (mode 0600) with `HC_PREPUSH_URL` and
   `HC_PREPUSH_TOKEN`, then run `scripts/prepush-ci.sh install-hook` in the checkout.
5. **Cleanup.** Bundles stay in the directory. Remove old ones from time to time:
   `find /containers/appdata/n8n/files/hc-prepush -name '*.bundle' -mtime +7 -delete`.

## Coverage compared with GitHub Actions

| GitHub Actions (ci.yml) | Jenkins | Note |
|---|---|---|
| check: lint, build, Go, Rust, retention cross-check, fuzz, frontend, release tooling, security, repro-check, deb, compliance | check | same targets and order |
| release-dry-run: build a, build b, verify | release dry run | the two runs are separate containers on one host, not two machines |
| install-matrix (Debian stable and oldstable; Ubuntu 22.04, 24.04 and 26.04) | install matrix | same images and preparation, read from ci.yml |
| browser-e2e (Chromium, Firefox, WebKit) | browser e2e | official Playwright image, same version |
| systemd-ubuntu, systemd-ubuntu-2604 | – | needs root and systemd as PID 1 on a disposable machine; see "Disposable VMs" |
| upgrade-ubuntu | – | same |
| release.yml (manual release) | – | never run by Jenkins |

Reports that GitHub Actions does not have:
- Vitest JUnit, shown as Jenkins test results;
- the Go coverage profile, through the Coverage plugin;
- `ci-result.json`, described below.

## Jenkins configuration

The controller already has:
- the multibranch job `Homelab-Control`, using the `github-jenkins-app` credential;
- branch discovery, excluding branches that are also pull requests;
- origin pull requests, merged with their target;
- a scan every 5 minutes;
- the script path `Jenkinsfile`.

Keep it that way:
- **Fork pull requests:** do not add the "Discover pull requests from forks" trait. If you add
  it later, pick "Nobody" as trusted, and still never build forks on these agents.
- **Optional check name:** the GitHub Checks plugin publishes a "Jenkins" check with the stage
  results through the App's Checks permission. To give it a stable, distinct name, add the
  "Status Checks Properties" trait with the name `jenkins/homelab-control`.
- **Tags:** the Jenkinsfile fetches tags itself (`CloneOption`). The job needs no extra trait.

Plugins the Jenkinsfile uses, all already installed and compatible with 2.580.3:

| Plugin | Installed version | Use |
|---|---|---|
| Pipeline (workflow-aggregator) | 608.v67378e9d3db_1 | declarative pipeline, `stash`, `parallel` |
| Git, GitHub Branch Source | 1988.v87ea_66346560 (branch source) | discovery and checkout |
| GitHub Checks | 717.v107022b_fe5fb_ | check runs on GitHub |
| JUnit | 1458.1460.vd53b_087eeb_2c | Vitest results |
| Coverage | 4.3627.v8c2b_efe6d608 | Go coverage (`GO_COV`) |
| Timestamper | 1.30 | `timestamps()` |

No new plugin is needed. Pipeline Utility Steps is not installed and not used; the summary is
written with `groovy.json.JsonOutput`, which the sandbox allows.

## Operations

- **Executors:** the docker agent has one executor, so the later stages wait for each other. A
  build takes about 15 minutes. With 2–3 executors on that agent they run in parallel; every
  container keeps its own CPU and memory limits.
- **Disk:** a build archives about 190 MB (binaries, `.deb`, release candidate) on the
  controller. `artifactNumToKeep` is 5 per branch.
- **Images:** each toolchain change builds new `hc-ci-*` images on the rootless daemon. Remove
  old ones with:

  ```sh
  docker image prune --all --filter label=hc-ci=1 --filter until=336h
  ```

- **Leftover containers:** `docker ps -a --filter label=hc-ci=1` should be empty between builds.
  An aborted build removes its container (`ci/run.sh` traps the signal).

## Results for n8n or a coding agent

Each build archives `ci-result.json`, with these fields:
- `repository`, `branch`, `pullRequest`, `pullRequestTarget`, `commit`. For a pull request,
  `commit` is the head commit; Jenkins builds it merged with the target;
- `mode` (`branch`, `pull-request` or `prepush`), `testedTree` (the tree every stage tested),
  `mainCommit` and `prepushRun` (pre-push mode);
- `graphify`: `status` (`complete`, `not-applicable`, `incomplete`, `error` or `not run`) and
  `indexedCommit`;
- `result`, `failedStages`, `buildUrl`, `durationMillis`.

`failedStages` names only the stages whose own steps failed. A stage stopped by an abort, or
superseded by a newer build, is not listed; `result` is then `ABORTED` or `NOT_BUILT`.

The Jenkins REST API gives the rest. Use a dedicated Jenkins user with an API token and read-only
permissions:

| What | Endpoint (under the job, for example `/job/Homelab-Control/job/PR-183/`) |
|---|---|
| summary | `lastBuild/artifact/ci-result.json` |
| status, commit | `lastBuild/api/json?tree=result,building,number,url,actions[lastBuiltRevision[SHA1]]` |
| test results | `lastBuild/testReport/api/json` |
| artifacts | `lastBuild/api/json?tree=artifacts[relativePath]`, then `lastBuild/artifact/<path>` |
| log | `lastBuild/consoleText`. Each check step starts with a `=====` line, which makes a good excerpt anchor. |

Any automated remediation built on this needs:
- bounded retries;
- changes made through pull requests only;
- a human for the merge.

The pipeline itself only reports.

## Disposable VMs (proposal, not set up)

The systemd and upgrade tests need root, systemd as PID 1 and a machine that is thrown away. The
persistent agents and rootless containers are not that. The proposal:
- **Agent:** a separate agent `vm-agent` with label `vm` and one executor. It runs as a systemd
  service for a dedicated, unprivileged host user `jenkins-vm`, with `CPUQuota=800%` and
  `MemoryMax=16G` on its slice.
- **Access:** that user gets `/dev/kvm` through the `kvm` group and its own **session** libvirt
  (`qemu:///session`). It does not get the system libvirt socket, so it cannot see or change
  existing VMs, and it has no Docker socket and no sudo.
- **Storage:** disk images live in `/mnt/fast/jenkins/vm`, as qcow2 overlays on a read-only,
  checksummed Ubuntu cloud image, deleted after each run.
- **Network:** user-mode networking (passt), with no bridge on the LAN.
- **Test:** the stage boots a fresh overlay, copies in the `.deb` and `tests/`, runs
  `tests/systemd/systemd-smoke.sh` and `tests/packaging/upgrade-test.sh` as root inside the VM,
  collects the logs, and destroys the VM.

This changes the host, so it waits for the owner's approval. Until then, GitHub's runner VMs run
those three jobs.

## Run it locally

```sh
DOCKER_HOST=unix:///run/user/1000/docker.sock ci/run.sh --out build --out reports -- ci/check.sh
```

Use a full clone with tags (`git fetch --tags`), not a worktree: the checkout's `.git` must be a
directory.
