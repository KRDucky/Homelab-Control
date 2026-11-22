// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ThemeStudio } from './pages/ThemeStudio';
import { DEFAULT_PREFERENCES, applyPreferences, loadPreferences } from './preferences';
import { activeTheme, applyTheme, loadThemes, resolvedColors } from './theme';

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

describe('Theme Development Kit in the WebUI', () => {
  it('creates, saves, previews, reverts and activates a valid browser-local theme', () => {
    render(<ThemeStudio />);
    fireEvent.click(screen.getByRole('button', { name: 'Create from daybreak' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Theme name' }), {
      target: { value: 'HomeControl Ocean' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Identifier' }), {
      target: { value: 'local.ocean' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
    expect(loadThemes().find((theme) => theme.id === 'local.ocean')?.name).toBe('HomeControl Ocean');
    expect(activeTheme()).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Preview on application' }));
    expect(screen.getByRole('region', { name: 'Confirm theme preview' })).toBeTruthy();
    expect(activeTheme()).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    expect(screen.queryByRole('region', { name: 'Confirm theme preview' })).toBeNull();
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe('');

    fireEvent.click(screen.getByRole('button', { name: 'Preview on application' }));
    fireEvent.click(screen.getByRole('button', { name: 'Keep theme' }));
    expect(activeTheme()?.id).toBe('local.ocean');
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe(
      resolvedColors(activeTheme()!)['surface-base'],
    );
    expect(loadPreferences()).toEqual(DEFAULT_PREFERENCES);
  });

  it('blocks unreadable themes from application preview and focuses the problem role', () => {
    render(<ThemeStudio />);
    fireEvent.click(screen.getByText('text colours'));
    fireEvent.change(screen.getByRole('textbox', { name: 'text-primary hex colour' }), {
      target: { value: '#000000' },
    });
    expect(screen.getByRole('button', { name: 'Preview on application' }).hasAttribute('disabled')).toBe(
      true,
    );
    const validation = screen.getByRole('region', { name: 'Theme validation' });
    const issue = validation.querySelector('button.issue-link');
    expect(issue).not.toBeNull();
    fireEvent.click(issue!);
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'text-primary hex colour' }));
    expect(activeTheme()).toBeNull();
  });
});
