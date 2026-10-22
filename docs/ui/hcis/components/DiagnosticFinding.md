# DiagnosticFinding

One Health finding: severity, confirmed vs suspected, module, first/last seen, evidence, investigation, solution, verification, acknowledge/suppress.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: none in the repository.

## Rules

- Suspected findings say why they are only suspected (for example high RAM use alone is not a leak).
- Resolution is verified by a re-scan, never assumed.

Props: `DiagnosticFindingProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
