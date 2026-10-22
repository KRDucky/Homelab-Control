# OperationPreview

The dry-run plan: mode banner, canonical target, counts, every change with diffs, dependency warnings, risk.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: `CLAUDE.md (preflight, dry-run, explicit authorization)`

## When to use

Every write action shows this first. Apply continues to ConfirmDialog; without `onApply` the button explains that design previews never run operations.

Props: `OperationPreviewProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
