// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { DataTable } from './DataTable';
import { CommandPalette } from './CommandPalette';
import { ThemeStudio } from './pages/ThemeStudio';
import { DEFAULT_PREFERENCES, applyPreferences } from './preferences';
import { activeTheme } from './theme';

afterEach(() => {
  cleanup();
  localStorage.clear();
  applyPreferences(DEFAULT_PREFERENCES);
  vi.useRealTimers();
});
const rows = [
  { id: 'b', n: 10 },
  { id: 'a', n: 2 },
  { id: 'c', n: 1 },
];
const columns = [
  {
    id: 'id',
    label: 'Name',
    render: (r: (typeof rows)[number]) => r.id,
    sortValue: (r: (typeof rows)[number]) => r.id,
  },
  {
    id: 'n',
    label: 'Count',
    render: (r: (typeof rows)[number]) => r.n,
    sortValue: (r: (typeof rows)[number]) => r.n,
  },
];
describe('shared table', () => {
  it('sorts numeric values and bounds rows per page', () => {
    render(
      <DataTable
        caption="Example resources"
        rows={rows}
        rowKey={(r) => r.id}
        columns={columns}
        pageSize={2}
      />,
    );
    expect(screen.getAllByRole('rowheader').map((n) => n.textContent)).toEqual(['b', 'a']);
    fireEvent.click(screen.getByRole('button', { name: /Count/ }));
    expect(screen.getAllByRole('rowheader').map((n) => n.textContent)).toEqual(['c', 'a']);
    expect(screen.getByRole('columnheader', { name: /Count/ }).getAttribute('aria-sort')).toBe('ascending');
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(screen.getAllByRole('rowheader').map((n) => n.textContent)).toEqual(['b']);
  });
  it('keeps selection across refreshed row objects and filters missing keys from actions', () => {
    const change = vi.fn();
    const { rerender } = render(
      <DataTable
        caption="Example"
        rows={rows}
        rowKey={(r) => r.id}
        columns={columns}
        selectable
        onSelectionChange={change}
        bulkActions={(keys) => <span>{keys.join(',')} action</span>}
      />,
    );
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select b' }));
    rerender(
      <DataTable
        caption="Example"
        rows={rows.map((r) => ({ ...r }))}
        rowKey={(r) => r.id}
        columns={columns}
        selectable
        onSelectionChange={change}
      />,
    );
    expect((screen.getByRole('checkbox', { name: 'Select b' }) as HTMLInputElement).checked).toBe(true);
    expect(change).toHaveBeenCalledWith(['b']);
  });
});
it('palette offers permitted navigation and closes on Escape', () => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
  render(
    <CommandPalette
      pages={[
        {
          id: 'audit',
          label: 'Audit',
          path: '/audit',
          scope: 'audit.read',
          status: 'forbidden',
          reason: 'Not allowed',
        },
      ]}
      accountPath="/account/sessions"
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Search pages' }));
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).queryByRole('option', { name: 'Audit' })).toBeNull();
  expect(document.activeElement).toBe(within(dialog).getByRole('combobox'));
  fireEvent(dialog, new Event('cancel', { bubbles: true, cancelable: true }));
  expect(screen.queryByRole('dialog')).toBeNull();
});
it('studio rejects invalid JSON and keeps unvalidated drafts isolated', () => {
  render(<ThemeStudio />);
  fireEvent.click(screen.getByRole('button', { name: 'Advanced JSON' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Theme JSON' }), {
    target: { value: '{"schemaVersion":1,"schemaVersion":2}' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Validate and apply JSON' }));
  expect(screen.getByText('Duplicate key: schemaVersion.')).toBeTruthy();
  expect(activeTheme()).toBeNull();
  expect(document.documentElement.style.getPropertyValue('--hc-color-text-primary')).toBe('');
});
