# Contributing to Homelab Control Extensions

Extensions default to **GPL-3.0-or-later**. MIT and Apache-2.0 are also permitted
where the applicable files and their provenance are explicitly recorded in
`REUSE.toml` or SPDX headers. Preserve existing copyright and licence notices;
do not relicense copied code. Licence texts are in `LICENSES/`.

The sample manifest and its README remain MIT. The extracted
`plugins/sample-readonly/ui/SampleOverviewCard.tsx` remains GPL-3.0-or-later;
the manifest's `license` field does not relicense that component. Use precise
file annotations rather than an MIT glob covering the whole sample directory.

## Developer Certificate of Origin

Every contributor must certify their own commits under the
[Developer Certificate of Origin 1.1](https://developercertificate.org/).
After reviewing the contribution and confirming the right to submit it, the
human contributor adds their own `Signed-off-by: Name <email>` trailer, for
example with `git commit -s`. No tool or other person may sign off on behalf
of the owner or contributor. AI-assisted changes must be reviewed by the human
submitter and identify the tool with an `Assisted-by:` trailer.

Keep changes focused. Describe provenance, licensing and validation in the PR.
Run `reuse lint`, validate manifests with the current core PDK, and check copied
components against their recorded source revision. Work remains in draft PRs
until the owner reviews it; this repository does not authorize privileged
operations, package writes, installation, deployment or releases.
