// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
// HCIS fonts (OFL-1.1, docs/ui/hcis/fonts/README.md): self-hosted woff2 files bundled by Vite and served
// by hc-api from its own origin (CSP font-src 'self'). No font CDN.
import '@fontsource-variable/inter/wght.css';
import '@fontsource-variable/jetbrains-mono/wght.css';
import '../../design-system/tokens.css';
import './styles.css';
import './shell.css';
import { App } from './App';
import { applyPreferences, loadPreferences } from './preferences';

// Apply stored appearance preferences before the first render, so the page never flashes the
// default theme or text size (they also apply on the sign-in page).
applyPreferences(loadPreferences());

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
