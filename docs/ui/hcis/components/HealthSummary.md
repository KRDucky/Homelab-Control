# HealthSummary

Overall health: headline state, counts for Critical, Warning, Notice, Healthy and Unknown, scan control.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: none in the repository.

## Rules

- Unchecked modules count as Unknown and the headline says “Not fully checked”. An unchecked system is never shown as healthy.

Props: `HealthSummaryProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
