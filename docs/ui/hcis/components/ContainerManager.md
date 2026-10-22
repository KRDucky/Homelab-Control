# ContainerManager

Docker: containers grouped by Compose project or flat, images, networks, volumes and engine details.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: none in the repository.

## Rules

- Crash loops are called out with the restart count.
- Every lifecycle action opens a preview.

Props: `ContainerManagerProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
