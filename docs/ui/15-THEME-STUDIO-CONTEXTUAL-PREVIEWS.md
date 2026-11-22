# Contextual Theme Studio previews

Theme Studio provides three isolated, interactive scenes for testing a draft theme before activation:

- **Dashboard:** Example CPU, memory and storage widgets, meters and chart lines using HCIS theme tokens.
- **Navigation:** A mock grouped sidebar with focus, hover, selection and unavailable-module states. Navigation remains inside the preview and never changes the real application's route.
- **Module page:** A mock services table, status labels, filter input, disabled control and safety message. The filter modifies only hard-coded example data.

The existing **Desktop, Tablet and Phone** preview widths use the same nested component, so container queries adapt layouts based on the chosen preview width rather than the browser window alone. The entire preview is read-only with respect to the host: no capability checks, host API calls, system service actions or writes to browser theme preferences are initiated by preview-scene interactions.

Changes to a draft are applied only to the preview subtree through `applyTheme(draft, previewElement)`. The separate **Preview on application** action still requires contrast validation and an explicit 15-second confirmation before activation; Escape or **Revert** restores the prior appearance.

## Validation

`frontend/app/src/theme-preview.test.tsx` verifies scene switching, sample service filtering, local navigation, draft colour propagation, responsive width state, no global theme mutation and continued activation confirmation. Existing TDK tests retain the create/save, contrast and activation flows.

These previews illustrate the UI styling and do not imply live data, full capability coverage or a complete accessibility audit.
