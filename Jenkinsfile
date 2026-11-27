// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Jenkins pipeline for Homelab Control (ci/README.md). It runs the same gates as
// .github/workflows/ci.yml, which stays the required CI. Every gate is an existing Make target or
// script; ci/run.sh runs each one in a throwaway container on the agent's rootless Docker daemon,
// with the toolchain pinned by packaging/release/toolchain.env. Nothing here publishes, tags,
// signs, deploys or needs a credential. Tests that need root and real systemd (systemd-ubuntu,
// systemd-ubuntu-2604, upgrade-ubuntu) run only on GitHub's disposable runner VMs for now.

def failedStages = []
def gitCommit = null

// Full history and tags: VERSION (git describe), repro-check and the release dry run need them.
// The source's own extensions (for a pull request, the merge with its target) are kept.
def checkoutFull() {
  return checkout([$class: 'GitSCM', branches: scm.branches, userRemoteConfigs: scm.userRemoteConfigs,
    extensions: scm.extensions + [
      [$class: 'CloneOption', noTags: false, shallow: false, depth: 0, reference: '', timeout: 30],
      [$class: 'CleanBeforeCheckout', deleteUntrackedNestedRepositories: true],
    ]])
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
    stage('check') {
      agent { label 'docker' }
      options { timeout(time: 90, unit: 'MINUTES') }
      steps {
        script {
          // Pull requests from forks are untrusted code; they never run on these persistent agents.
          if (env.CHANGE_FORK) {
            error("Pull request ${env.CHANGE_ID} comes from the fork ${env.CHANGE_FORK}; it is not built here.")
          }
        }
        script { gitCommit = checkoutFull().GIT_COMMIT }
        sh 'ci/run.sh --out build --out reports --env HC_CI -- ci/check.sh'
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
        failure { script { failedStages << env.STAGE_NAME } }
      }
    }
    // The docker agent has one executor, so these run one after another; each is independent.
    stage('after check') {
      parallel {
        stage('release dry run') {
          agent { label 'docker' }
          options { timeout(time: 90, unit: 'MINUTES') }
          steps {
            checkoutFull()
            sh 'ci/release-dry-run.sh'
          }
          post {
            always {
              archiveArtifacts artifacts: 'dist/candidate/**,dist/notes/**', allowEmptyArchive: true, fingerprint: true
            }
            failure { script { failedStages << env.STAGE_NAME } }
          }
        }
        stage('install matrix') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          steps {
            checkoutFull()
            unstash 'build'
            script {
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
            }
          }
          post { failure { script { failedStages << env.STAGE_NAME } } }
        }
        stage('browser e2e') {
          agent { label 'docker' }
          options { timeout(time: 45, unit: 'MINUTES') }
          environment { HC_CSP_BROWSERS = 'chromium,firefox,webkit' }
          steps {
            checkoutFull()
            unstash 'build'
            sh '''
              ci/run.sh --image e2e --env HC_CSP_BROWSERS -- sh -ec '
                (cd frontend/app && npm ci --ignore-scripts --no-fund)
                chmod +x build/hc-api build/hlctl
                tests/integration/run-hc-e2e.sh'
            '''
          }
          post { failure { script { failedStages << env.STAGE_NAME } } }
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
            branch: env.BRANCH_NAME,
            pullRequest: env.CHANGE_ID ?: null,
            pullRequestTarget: env.CHANGE_TARGET ?: null,
            commit: gitCommit,
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
