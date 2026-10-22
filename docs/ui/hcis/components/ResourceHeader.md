# ResourceHeader

The header of a resource page: kind, name, status, key facts and actions.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/Overview.tsx (.faceplate)`

## When to use

One per page, above the tabs. The name uses the `display` style.

## Current implementation vs HCIS

App: the “faceplate” with rack-ear mounting holes, a 62%-width Archivo hostname and a gradient. HCIS keeps the idea of one bold element but drops the gradient and ornament.

Props: `ResourceHeaderProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
