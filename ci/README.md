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
| check | docker | `ci/check.sh` in the toolchain image | binaries, `.deb`, inventory and lintian files; Vitest JUnit; Go coverage |
| release dry run | docker | `ci/release-dry-run.sh`: two `release-bundle.sh` runs in fresh containers, then the `verify` steps | `dist/candidate`, `dist/notes` |
| install matrix | docker | `tests/packaging/install-test.sh` in each image of ci.yml's install-matrix (read from ci.yml) | – |
| browser e2e | docker | `tests/integration/run-hc-e2e.sh` in the e2e image (Playwright, Chromium, Firefox, WebKit) | – |

- **Order:** the later stages need the `check` stage's build, passed with
  `stash`/`unstash`. The docker agent has one executor, so the later stages run one after
  another. With more executors they run in parallel without changes.
- **Pull requests from forks:** the pipeline stops on them (`CHANGE_FORK`). The job must also
  not discover them (see below).
- **Failures:** each stage keeps its exit status, and the build ends FAILURE when any stage
  fails.

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

## Results for n8n or a coding agent

Each build archives `ci-result.json`, with these fields:
- `repository`, `branch`, `pullRequest`, `pullRequestTarget`, `commit`;
- `result`, `failedStages`, `buildUrl`, `durationMillis`.

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
