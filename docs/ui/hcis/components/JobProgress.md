# JobProgress

Progress of one background job, its steps, the observed result and audit reference.

**Status: Planned.** Documented in the repository's `docs/ui/` planning but not implemented.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: `docs/ui/01 §1.4 OperationBannerProps`

## Rules

- “Succeeded” is shown only when the result is verified by the backend; otherwise “Finished · awaiting verification”.

Props: `JobProgressProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
