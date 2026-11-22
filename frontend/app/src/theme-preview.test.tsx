// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { ThemePreview } from './pages/ThemePreview';
import { ThemeStudio } from './pages/ThemeStudio';
import { DEFAULT_PREFERENCES, applyPreferences } from './preferences';
import { applyTheme, newTheme, resolvedColors } from './theme';

beforeEach(() => {
  localStorage.clear();
  applyTheme(null);
  applyPreferences(DEFAULT_PREFERENCES);
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  applyTheme(null);
  applyPreferences(DEFAULT_PREFERENCES);
});

describe('contextual Theme Studio preview', () => {
  it('shows dashboard readings and follows draft colours without changing application appearance', () => {
    const draft = newTheme('daybreak');
    const { rerender } = render(<ThemePreview draft={draft} width="desktop" />);
    const preview = screen.getByLabelText('Theme preview with example data');
    expect(preview.getAttribute('data-preview-width')).toBe('desktop');
    expect(within(preview).getByText('CPU usage')).toBeTruthy();
    expect(within(preview).getByText('Resource trends')).toBeTruthy();
    expect(preview.style.getPropertyValue('--hc-color-surface-base')).toBe(
      resolvedColors(draft)['surface-base'],
    );

    const modified = {
      ...draft,
      colors: { ...draft.colors, 'accent-base': '#123456' },
    };
    rerender(<ThemePreview draft={modified} width="phone" />);
    expect(preview.getAttribute('data-preview-width')).toBe('phone');
    expect(preview.style.getPropertyValue('--hc-color-accent-base')).toBe('#123456');
    expect(document.documentElement.style.getPropertyValue('--hc-color-accent-base')).toBe('');
  });

  it('switches scenes and navigation entirely inside the isolated preview', () => {
    render(<ThemePreview draft={newTheme('midnight')} width="tablet" />);
    fireEvent.click(screen.getByRole('tab', { name: 'Navigation' }));
    expect(screen.getByRole('tab', { name: 'Navigation' }).getAttribute('aria-selected')).toBe('true');
    const nav = screen.getByRole('navigation', { name: 'Example module navigation' });
    fireEvent.click(within(nav).getByRole('button', { name: 'Storage' }));
    expect(within(nav).getByRole('button', { name: 'Storage' }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('heading', { name: 'Storage' })).toBeTruthy();
    expect(within(nav).getByRole('button', { name: 'Unavailable module' }).hasAttribute('disabled')).toBe(true);
    expect(window.location.pathname).not.toBe('/storage');

    fireEvent.click(screen.getByRole('tab', { name: 'Module page' }));
    expect(screen.getByText('backup.service')).toBeTruthy();
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter example services' }), {
      target: { value: 'media' },
    });
    expect(screen.queryByText('backup.service')).toBeNull();
    expect(screen.getByText('media.service')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filter' }));
    expect(screen.getByText('backup.service')).toBeTruthy();
    expect(localStorage.length).toBe(0);
  });

  it('retains theme validation and timed application controls around the new preview', () => {
    render(<ThemeStudio />);
    expect(screen.getByRole('tab', { name: 'Dashboard' })).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: 'Navigation' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Module page' }));
    expect(screen.getByRole('button', { name: 'Preview on application' }).hasAttribute('disabled')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Preview on application' }));
    expect(screen.getByRole('region', { name: 'Confirm theme preview' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    expect(screen.queryByRole('region', { name: 'Confirm theme preview' })).toBeNull();
  });
});
