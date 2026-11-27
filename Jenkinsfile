// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Jenkins pipeline for Homelab Control (ci/README.md). It runs the same gates as
// .github/workflows/ci.yml, which stays the required CI. Every gate is an existing Make target or
// script; ci/run.sh runs each one in a throwaway container on the agent's rootless Docker daemon,
// with the toolchain pinned by packaging/release/toolchain.env. Nothing here publishes, tags,
// signs, deploys or needs a credential. Tests that need root and real systemd (systemd-ubuntu,
// systemd-ubuntu-2604, upgrade-ubuntu) run only on GitHub's disposable runner VMs for now.
//
// The same file runs the pre-push job Homelab-Control-prepush (ci/README.md "Pre-push testing"):
// when the job's HC_PREPUSH_SHA parameter is set, every stage tests that unpushed commit, fetched
// from n8n as a git bundle and merged with main, instead of the branch or pull request.

def failedStages = []
def gitCommit = null // the head commit (see checkoutSource)
def testedTree = null // the tree every stage tests
def prepushBase = null // pre-push mode: the main commit every stage merges with
def graphify = [status: 'not run', indexedCommit: null]

def prepushMode() { return (params.HC_PREPUSH_SHA ?: '') != '' }

// Full history and tags: VERSION (git describe), repro-check and the release dry run need them.
// The source's own extensions (for a pull request, the merge with its target) are kept.
def checkoutFull() {
  return checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs,
    extensions: scm.extensions + [
      [$class: 'CloneOption', noTags: false, shallow: false, depth: 0, reference: '', timeout: 30],
      [$class: 'CleanBeforeCheckout', deleteUntrackedNestedRepositories: true],
    ]])
}

// The commit to test: the branch or pull request as Jenkins checks it out, or in pre-push mode the
// bundle's head merged with main (ci/prepush-checkout.sh). Returns the head commit (for a pull
// request, its head; Jenkins tests it merged with the target), HEAD, HEAD's tree and the pinned main.
// A pull request's merge commit is made anew at each checkout, so stages compare trees, not commits.
def checkoutSource(String base) {
  def head = checkoutFull().GIT_COMMIT
  if (prepushMode()) {
    head = params.HC_PREPUSH_SHA
    withEnv(["HC_PREPUSH_SHA=${params.HC_PREPUSH_SHA}", "HC_PREPUSH_BUNDLE_URL=${params.HC_PREPUSH_BUNDLE_URL}",
             "HC_PREPUSH_BASE=${base ?: ''}"]) {
      def out = sh(returnStdout: true, script: 'ci/prepush-checkout.sh').trim()
      if (!base) { base = out.readLines().last().replaceFirst('^base ', '').trim() }
    }
  }
  def ids = sh(returnStdout: true, script: 'git rev-parse HEAD HEAD^{tree}').trim().readLines()
  return [head: head, commit: ids[0], tree: ids[1], base: base]
}

// Runs body; when body itself fails, names the stage in failedStages and fails it. A stage's
// post { failure } also fires after another stage has failed the build, so it cannot be used.
def tracked(List failedStages, String stage, Closure body) {
  try {
    body()
  } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
    throw e // aborted or superseded: not a failure of this stage
  } catch (e) {
    failedStages << stage
    throw e
  }
}

pipeline {
  agent none
  options {
    timestamps()
    timeout(time: 4, unit: 'HOURS')
    disableConcurrentBuilds(abortPrevious: true)
    buildDiscarder(logRotator(numToKeepStr: '30', artifactNumToKeepStr: '5'))
    skipDefaultCheckout()
  }
  environment {
    // CI mode for scripts/security-check.sh: every scanner is required, RustSec is fetched.
    HC_CI = '1'
    HC_CI_CPUS = '6'
    HC_CI_MEMORY = '12g'
  }
  stages {
    // Graphify impact review of the commit under test (docs/project/08-TESTING-AND-ACCEPTANCE.md).
    // An index that misses a changed source file makes the build UNSTABLE, not green.
    stage('graphify') {
      agent { label 'docker' }
      steps {
        // In the steps, not the stage options: a stage option also counts the wait for the single
        // docker executor, which can be longer than the review itself.
        timeout(time: 20, unit: 'MINUTES') { script {
          tracked(failedStages, env.STAGE_NAME) {
            if (env.CHANGE_FORK) {
              error("Pull request ${env.CHANGE_ID} comes from the fork ${env.CHANGE_FORK}; it is not built here.")
            }
            def src = checkoutSource(null)
            prepushBase = src.base
            gitCommit = src.head
            testedTree = src.tree
            def code = 0
            withEnv(["HC_TESTED_COMMIT=${src.commit}", "HC_GRAPHIFY_BASE=${src.base ?: ''}"]) {
              code = sh(returnStatus: true, script:
                'ci/run.sh --out reports --env HC_TESTED_COMMIT --env HC_GRAPHIFY_BASE --env CHANGE_TARGET -- ci/graphify-stage.sh')
            }
            graphify.status = fileExists('reports/graphify/STATUS') ? readFile('reports/graphify/STATUS').trim() : 'error'
            graphify.indexedCommit = fileExists('reports/graphify/INDEXED_SHA') ? readFile('reports/graphify/INDEXED_SHA').trim() : null
            if (code == 1) {
              unstable('Graphify: a changed source file is not in the index; see reports/graphify/IMPACT.md')
            } else if (code != 0) {
              error("Graphify review failed (exit ${code})")
            } else if (graphify.indexedCommit != src.commit) {
              error("Graphify indexed ${graphify.indexedCommit}, not the commit under test ${src.commit}")
            }
          }
        } }
      }
      post {
        always {
          archiveArtifacts artifacts: 'reports/graphify/**', allowEmptyArchive: true
        }
      }
    }
    stage('check') {
      agent { label 'docker' }
      options { timeout(time: 90, unit: 'MINUTES') }
      steps {
        script {
          tracked(failedStages, env.STAGE_NAME) {
            // Pull requests from forks are untrusted code; they never run on these persistent agents.
            if (env.CHANGE_FORK) {
              error("Pull request ${env.CHANGE_ID} comes from the fork ${env.CHANGE_FORK}; it is not built here.")
            }
            def src = checkoutSource(prepushBase)
            if (testedTree && src.tree != testedTree) {
              error("check would test tree ${src.tree}, but the graphify stage reviewed tree ${testedTree}")
            }
            gitCommit = src.head
            testedTree = src.tree
            sh 'ci/run.sh --out build --out reports --env HC_CI -- ci/check.sh'
          }
        }
      }
      post {
        always {
          archiveArtifacts artifacts: 'build/hc-*,build/hlctl,build/SHA256SUMS,build/*.deb,build/*.inventory.txt,build/*.lintian.txt',
            allowEmptyArchive: true, fingerprint: true
          junit testResults: 'reports/vitest-junit.xml', allowEmptyResults: true
          recordCoverage(tools: [[parser: 'GO_COV', pattern: 'reports/go-coverage.out']],
            name: 'Go coverage', sourceCodeRetention: 'NEVER', enabledForFailure: true, skipPublishingChecks: true)
        }
        success { stash name: 'build', includes: 'build/**' }
      }
    }
    // The docker agent has one executor, so these run one after another; each is independent.
    stage('after check') {
      parallel {
        stage('release dry run') {
          agent { label 'docker' }
          options { timeout(time: 90, unit: 'MINUTES') }
          steps {
            script {
              tracked(failedStages, env.STAGE_NAME) {
                checkoutSource(prepushBase)
                sh 'ci/release-dry-run.sh'
              }
            }
          }
          post {
            always {
              archiveArtifacts artifacts: 'dist/candidate/**,dist/notes/**', allowEmptyArchive: true, fingerprint: true
            }
          }
        }
        stage('install matrix') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            script { tracked(failedStages, env.STAGE_NAME) {
              checkoutSource(prepushBase)
              unstash 'build'
              // The images and their preparation come from ci.yml's install-matrix, as written there.
              def rows = sh(returnStdout: true, script: '''
                sed -n "s/.*{ image: '\\([^']*\\)', prep: '\\([^']*\\)' }.*/\\1|\\2/p" .github/workflows/ci.yml
              ''').trim().readLines()
              if (rows.isEmpty()) { error('no install-matrix images found in .github/workflows/ci.yml') }
              def runs = [:]
              for (row in rows) {
                def parts = row.split('\\|', 2)
                def image = parts[0]
                def prep = parts[1]
                runs[image] = {
                  // Root only inside the throwaway container; the marker file is created there.
                  withEnv(["HC_IMAGE=${image}", "HC_PREP=${prep}", "HC_CI_CPUS=2", "HC_CI_MEMORY=4g"]) {
                    sh '''
                      ci/run.sh --image "$HC_IMAGE" --root --env HC_IMAGE --env HC_PREP -- sh -ec '
                        sh -c "$HC_PREP"
                        touch /etc/hc-disposable
                        HC_DISTRO="$HC_IMAGE" tests/packaging/install-test.sh "$(ls build/*.deb)"'
                    '''
                  }
                }
              }
              parallel runs
            } }
          }
        }
        stage('browser e2e') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          environment { HC_CSP_BROWSERS = 'chromium,firefox,webkit' }
          steps {
            script { tracked(failedStages, env.STAGE_NAME) {
              checkoutSource(prepushBase)
              unstash 'build'
              sh '''
              ci/run.sh --image e2e --env HC_CI --env HC_CSP_BROWSERS -- sh -ec '
                (cd frontend/app && npm ci --ignore-scripts --no-fund)
                chmod +x build/hc-api build/hlctl
                tests/integration/run-hc-e2e.sh'
              '''
            } }
          }
        }
      }
    }
  }
  post {
    always {
      // Machine-readable summary for n8n or a coding agent (ci/README.md). The Jenkins REST API
      // holds the rest: test results, stages and artifacts.
      node('docker') {
        script {
          def summary = [
            repository: 'KRDucky/Homelab-Control',
            mode: prepushMode() ? 'prepush' : (env.CHANGE_ID ? 'pull-request' : 'branch'),
            branch: prepushMode() ? params.HC_PREPUSH_BRANCH : env.BRANCH_NAME,
            pullRequest: env.CHANGE_ID ?: null,
            pullRequestTarget: env.CHANGE_TARGET ?: null,
            // The head commit: for a pull request or a pre-push run, Jenkins tests it merged with main.
            commit: gitCommit,
            testedTree: testedTree,
            mainCommit: prepushBase,
            prepushRun: prepushMode() ? params.HC_PREPUSH_RUN : null,
            graphify: graphify,
            result: currentBuild.currentResult,
            failedStages: failedStages,
            buildUrl: env.BUILD_URL,
            durationMillis: currentBuild.duration,
          ]
          writeFile file: 'ci-result.json', text: groovy.json.JsonOutput.prettyPrint(groovy.json.JsonOutput.toJson(summary))
          archiveArtifacts artifacts: 'ci-result.json'
        }
      }
    }
  }
}
