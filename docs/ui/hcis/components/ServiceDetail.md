# ServiceDetail

systemd service page with the Diagnose & Resolve assistant: status, failure category, exit code, restart count, evidence, dependencies, recovery steps, verification.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: none in the repository.

## When to use

Recognised categories: port in use, permission denied, missing executable, missing directory, invalid configuration, dependency failure, start-limit reached, timeout, out-of-memory kill. Tabs appear only when they have content.

## Rules

- Explanations cite evidence from the journal or systemctl.
- Suggested fixes are never shown as executed.

Props: `ServiceDetailProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
