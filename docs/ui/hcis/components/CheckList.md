# CheckList

A list of findings, each with a level word, a message and an optional action.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/pages/Legacy.tsx (.checks)`

## When to use

Use for security reviews and diagnostics results.

## Rules

- Level words: OK, Review, Problem, Note.

Props: `CheckListProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
