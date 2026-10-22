# ConfirmDialog

The explicit authorization step (the spec's ConfirmationDialog / RiskConfirmation).

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: `CLAUDE.md write-action rules; docs/ui/06 §6.4`

## When to use

Open after an OperationPreview. Give `riskClass`, concrete `consequences`; `typeToConfirm` for irreversible actions; `reauth` where policy requires re-authentication.

## Rules

- Destructive dialogs are opaque (never glass), ringed in critical, over a heavier scrim.
- Focus starts on Close, never on the destructive button.

Props: `ConfirmDialogProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
