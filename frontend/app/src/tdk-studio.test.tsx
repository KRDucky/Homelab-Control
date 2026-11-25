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
      target: { value: 'Homelab Control Ocean' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Identifier' }), {
      target: { value: 'local.ocean' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
    expect(loadThemes().find((theme) => theme.id === 'local.ocean')?.name).toBe('Homelab Control Ocean');
    expect(activeTheme()).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Preview on application' }));
    expect(screen.getByRole('region', { name: 'Confirm theme preview' })).toBeTruthy();
    expect(activeTheme()).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    expect(screen.queryByRole('region', { name: 'Confirm theme preview' })).toBeNull();
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe('');

    fireEvent.click(screen.getByRole('button', { name: 'Preview on application' }));
    fireEvent.click(screen.getByRole('button', { name: 'Keep theme' }));
    const activated = activeTheme();
    expect(activated?.id).toBe('local.ocean');
    if (!activated) throw new Error('Confirmed theme did not activate.');
    expect(document.documentElement.style.getPropertyValue('--hc-color-surface-base')).toBe(
      resolvedColors(activated)['surface-base'],
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
    if (!issue) throw new Error('Expected a contrast issue link.');
    fireEvent.click(issue);
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'text-primary hex colour' }));
    expect(activeTheme()).toBeNull();
  });
  it('switches representative scenes and responsive sizes without modifying the active theme', () => {
    render(<ThemeStudio />);
    const preview = screen.getByLabelText('Theme preview with example data');
    expect(screen.getByLabelText('Dashboard scene')).toBeTruthy();
    expect(preview.getAttribute('data-preview-scene')).toBe('dashboard');
    expect(preview.getAttribute('data-preview-width')).toBe('desktop');
    expect(preview.querySelector('[aria-label="Example sidebar"]')).not.toBeNull();
    expect(preview.textContent).toContain('Resource trends');
    expect(preview.textContent).not.toContain('Webmin');

    fireEvent.click(screen.getByRole('button', { name: 'Navigation' }));
    expect(screen.getByLabelText('Navigation scene')).toBeTruthy();
    expect(preview.textContent).toContain('ZFS pools');
    fireEvent.click(screen.getByRole('button', { name: /ZFS pools/ }));
    expect(preview.textContent).toContain('Selected example module: ZFS pools');
    fireEvent.click(screen.getByRole('button', { name: 'Module detail' }));
    expect(screen.getByLabelText('Module scene')).toBeTruthy();
    expect(preview.textContent).toContain('backup.service');
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter example services' }), {
      target: { value: 'media' },
    });
    expect(preview.textContent).not.toContain('backup.service');
    expect(preview.textContent).toContain('media.service');
    fireEvent.click(screen.getByRole('button', { name: 'Clear filter' }));
    expect(preview.textContent).toContain('backup.service');

    fireEvent.change(screen.getByLabelText('Preview width'), { target: { value: 'phone' } });
    expect(preview.getAttribute('data-preview-width')).toBe('phone');
    expect(preview.querySelector('[aria-label="Example sidebar"]')).toBeNull();
    fireEvent.change(screen.getByLabelText('Preview width'), { target: { value: 'tablet' } });
    expect(preview.querySelector('[aria-label="Example sidebar"]')).not.toBeNull();

    fireEvent.click(screen.getByText('accent colours'));
    fireEvent.change(screen.getByRole('textbox', { name: 'accent-base hex colour' }), {
      target: { value: '#2288aa' },
    });
    expect((preview as HTMLElement).style.getPropertyValue('--hc-color-accent-base')).toBe('#2288aa');
    expect(document.documentElement.style.getPropertyValue('--hc-color-accent-base')).toBe('');
    expect(activeTheme()).toBeNull();
    expect(localStorage.getItem('hc.dashboard.v1')).toBeNull();
    expect(localStorage.getItem('hc.theme.active.v1')).toBeNull();
  });
});
