# ResourceExplorer

Cross-module explorer: resource tree, relationship graph and a floating detail panel with related links.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: none in the repository.

## When to use

Example path: host → drive bay → pool → dataset → bind mount → container.

## Rules

- Reads relationships only; modules remain independent.

Props: `ResourceExplorerProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
