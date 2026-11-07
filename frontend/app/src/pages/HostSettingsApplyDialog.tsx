// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import {
  ApiError,
  applyHostSettings,
  planHostSettings,
  type HostSetting,
  type HostSettingsObserved,
  type HostSettingsOutcome,
  type HostSettingsPlan,
  type HostSettingsPreviewRequest,
} from '../api';
import { linkHandler } from '../router';
import { StatusPill } from '../ui';
import { explain } from '../useData';
import { RiskBadge } from './ServiceActionDialog';

// HCIS privileged-operation flow: preview (nothing changed yet) → confirm
// (reversible: confirmation required; this op does not use password
// re-entry) → progress → backend-verified result. Success is hostnamectl/
// timedatectl read-back, never the kernel name seen by hc-api. A plan with
// step_up_required is refused: that path needs an ADR, not a Services copy.

const labels: Record<'static_hostname' | 'pretty_hostname' | 'timezone', string> = {
  static_hostname: 'Static host name',
  pretty_hostname: 'Pretty host name',
  timezone: 'Time zone',
};

const outcomeText = {
  unchanged: 'Same as now',
  'would-change': 'Would change',
  invalid: 'Not accepted',
  'cannot-compare': 'Current value unknown',
} as const;

function observedText(status: HostSetting['status'], value?: string): ReactNode {
  if (status === 'observed') return <span className="mono">{value}</span>;
  return <span className="muted">{status === 'not-set' ? 'Not set' : 'Unavailable'}</span>;
}

function ObservedFacts({ observed }: { observed: HostSettingsObserved }) {
  return (
    <dl className="kv">
      <dt>Static host name</dt>
      <dd>{observedText(observed.static_status, observed.static_hostname)}</dd>
      <dt>Pretty host name</dt>
      <dd>{observedText(observed.pretty_status, observed.pretty_hostname)}</dd>
      <dt>Time zone</dt>
      <dd>{observedText(observed.timezone_status, observed.timezone)}</dd>
    </dl>
  );
}

function Modal({
  labelledBy,
  describedBy,
  onEscape,
  children,
  initialFocus,
  returnFocus,
}: {
  labelledBy: string;
  describedBy?: string;
  onEscape: () => void;
  children: ReactNode;
  initialFocus: React.RefObject<HTMLElement | null>;
  returnFocus: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = document.getElementById('root');
    if (root) root.inert = true;
    initialFocus.current?.focus();
    return () => {
      if (root) root.inert = false;
      returnFocus();
    };
    // Run once per mount: focus placement and restoration.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onEscape();
      return;
    }
    if (e.key !== 'Tab' || !box.current) return;
    const items = [
      ...box.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), a[href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ];
    const first = items[0];
    const last = items[1] !== undefined ? items[items.length - 1] : first;
    if (!first || !last) return;
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };
  return createPortal(
    <div className="modal-scrim">
      <div
        ref={box}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        onKeyDown={onKeyDown}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}

type Stage =
  | { kind: 'planning' }
  | { kind: 'preview'; plan: HostSettingsPlan }
  | { kind: 'running'; plan: HostSettingsPlan }
  | { kind: 'result'; outcome: HostSettingsOutcome }
  | { kind: 'refused'; title: string; message: string; retry: boolean };

function opExplain(err: unknown): string {
  if (err instanceof ApiError) {
    if ((err.status >= 400 && err.status < 500) || err.status === 503 || err.code === 'outcome_unknown')
      return err.message;
    return explain(err);
  }
  return err instanceof Error ? explain(err) : String(err);
}

const refusedBeforeRun = new Set([
  'plan_invalid',
  'invalid_token',
  'conflict',
  'busy',
  'policy_denied',
  'bad_request',
  'forbidden',
  'step_up_required',
]);
const selfDescribing = new Set(['unavailable', 'audit_unavailable', 'outcome_unknown']);

function secondsLeft(iso: string, now: number): number {
  return Math.max(0, Math.floor((Date.parse(iso) - now) / 1000));
}

export function HostSettingsApplyDialog({
  request,
  csrf,
  onClose,
  onUnauthorized,
  auditAvailable,
  returnFocus,
}: {
  request: HostSettingsPreviewRequest;
  csrf: string;
  onClose: (changed: boolean) => void;
  onUnauthorized: () => void;
  auditAvailable: boolean;
  returnFocus: () => void;
}) {
  const [stage, setStage] = useState<Stage>({ kind: 'planning' });
  const [now, setNow] = useState(() => Date.now());
  const [executed, setExecuted] = useState(false);
  const closeRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const descId = useId();

  const review = useCallback(async () => {
    setStage({ kind: 'planning' });
    try {
      const plan = await planHostSettings(request, csrf);
      if (plan.step_up_required) {
        setStage({
          kind: 'refused',
          title: 'Cannot apply host settings',
          message:
            'This review requires a password confirmation. Host-settings apply does not use that path.',
          retry: false,
        });
        return;
      }
      setStage({ kind: 'preview', plan });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return onUnauthorized();
      const e = err instanceof ApiError ? err : null;
      setStage({
        kind: 'refused',
        title: e?.code === 'policy_denied' ? 'Not allowed' : 'Cannot apply host settings',
        message: opExplain(err),
        retry: e?.code !== 'policy_denied' && e?.status !== 403 && e?.status !== 404,
      });
    }
  }, [request, csrf, onUnauthorized]);

  useEffect(() => {
    const t = window.setTimeout(() => void review(), 0);
    return () => window.clearTimeout(t);
  }, [review]);

  useEffect(() => {
    if (stage.kind !== 'preview') return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [stage.kind]);

  useEffect(() => {
    if (stage.kind !== 'running') closeRef.current?.focus();
  }, [stage.kind]);

  async function confirm(plan: HostSettingsPlan) {
    if (plan.step_up_required) {
      setStage({
        kind: 'refused',
        title: 'Cannot apply host settings',
        message:
          'This review requires a password confirmation. Host-settings apply does not use that path.',
        retry: false,
      });
      return;
    }
    setStage({ kind: 'running', plan });
    try {
      const outcome = await applyHostSettings(request, plan.token, csrf);
      setExecuted(true);
      setStage({ kind: 'result', outcome });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return onUnauthorized();
      const e = err instanceof ApiError ? err : null;
      const titles: Record<string, string> = {
        plan_invalid: 'This review is no longer valid',
        invalid_token: 'This review is no longer valid',
        conflict: 'Host settings changed since you reviewed them',
        busy: 'Another host-settings operation is in progress',
        policy_denied: 'Not allowed',
        outcome_unknown: 'The result is unknown',
        step_up_required: 'Cannot apply host settings',
      };
      setStage({
        kind: 'refused',
        title: titles[e?.code ?? ''] ?? 'Could not apply host settings',
        message:
          opExplain(err) +
          (refusedBeforeRun.has(e?.code ?? '')
            ? ' Nothing was changed.'
            : selfDescribing.has(e?.code ?? '')
              ? ''
              : ' The operation may or may not have run: check Host settings and the operation journal.'),
        retry: e?.code !== 'policy_denied',
      });
    }
  }

  const running = stage.kind === 'running';
  const close = () => {
    if (!running) onClose(executed);
  };

  let body: ReactNode;
  let footer: ReactNode = null;
  if (stage.kind === 'planning') {
    body = (
      <p role="status" className="muted">
        Reviewing the change: reading hostnamectl and timedatectl, then issuing a plan token. Nothing is
        changed yet.
      </p>
    );
  } else if (stage.kind === 'preview' || stage.kind === 'running') {
    const plan = stage.plan;
    const left = secondsLeft(plan.expires_at, now);
    const expired = left === 0;
    body = (
      <>
        <p className="preview-bar">Preview · nothing has been changed yet</p>
        <dl className="kv">
          <dt>Risk</dt>
          <dd>
            <RiskBadge risk={plan.risk} />{' '}
            <span className="muted">Can generally be undone. Confirmation is required.</span>
          </dd>
        </dl>
        <p id={descId}>{plan.effect}</p>
        <ObservedFacts observed={plan.observed} />
        <table>
          <caption className="muted">Requested changes — not applied</caption>
          <thead>
            <tr>
              <th scope="col">Setting</th>
              <th scope="col">Now</th>
              <th scope="col">Requested</th>
              <th scope="col">Result</th>
            </tr>
          </thead>
          <tbody>
            {plan.changes.map((c) => (
              <tr key={c.field}>
                <th scope="row">{labels[c.field]}</th>
                <td>{c.current ? <span className="mono">{c.current}</span> : <span className="muted">Not set</span>}</td>
                <td>
                  <span className="mono">{c.requested}</span>
                </td>
                <td>
                  <StatusPill
                    tone={
                      c.outcome === 'invalid' ? 'crit' : c.outcome === 'would-change' ? 'notice' : 'info'
                    }
                    label={outcomeText[c.outcome]}
                  />
                  {c.reason && <span className="muted"> {c.reason}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <ul className="consequences">
          {plan.consequences.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        {plan.warnings.map((w) => (
          <p key={w} className="banner" role="note">
            {w}
          </p>
        ))}
        {(plan.observed.notes ?? []).map((n) => (
          <p key={n} className="footnote" role="note">
            {n}
          </p>
        ))}
        <p className="footnote" aria-live="off">
          {expired
            ? 'This review has expired. Review the operation again to continue.'
            : `This review is valid for ${Math.floor(left / 60)} min ${left % 60} s.`}
        </p>
        {running && (
          <p role="status" className="progress-note" aria-busy="true">
            Applying host settings… waiting for hostnamectl and timedatectl, then reading them back to
            verify the result.
          </p>
        )}
      </>
    );
    footer = (
      <>
        <button type="button" ref={closeRef} className="ghost" onClick={close} disabled={running}>
          {running ? 'Working…' : 'Close'}
        </button>
        {expired && !running ? (
          <button type="button" onClick={() => void review()}>
            Review again
          </button>
        ) : (
          <button type="button" disabled={running} onClick={() => void confirm(plan)}>
            Apply host settings
          </button>
        )}
      </>
    );
  } else if (stage.kind === 'result') {
    const o = stage.outcome;
    const label = { succeeded: 'Done', failed: 'Failed', uncertain: 'Not verified' }[o.outcome];
    body = (
      <div role="status">
        <p className={`op-result op-${o.outcome}`}>
          <strong>{label}</strong> — {o.message}
        </p>
        {o.observed && <ObservedFacts observed={o.observed} />}
        {(o.observed?.notes ?? []).map((n) => (
          <p key={n} className="footnote" role="note">
            {n}
          </p>
        ))}
        {o.recovery && o.recovery.length > 0 && (
          <div className="recovery">
            <p>
              <strong>What to do next</strong>
            </p>
            <ul>
              {o.recovery.map((r) => (
                <li key={r}>{r}</li>
              ))}
            </ul>
          </div>
        )}
        <p className="footnote">
          Operation <code>{o.operation_id.slice(0, 12)}</code>, recorded in the operation journal
          {auditAvailable ? (
            <>
              {' '}
              (
              <a href="/audit" onClick={linkHandler('/audit')}>
                view in Audit
              </a>
              )
            </>
          ) : null}
          .
        </p>
      </div>
    );
    footer = (
      <button type="button" ref={closeRef} className="ghost" onClick={close}>
        Close
      </button>
    );
  } else {
    body = (
      <p className="banner" role="alert">
        <strong>{stage.title}.</strong> {stage.message}
      </p>
    );
    footer = (
      <>
        <button type="button" ref={closeRef} className="ghost" onClick={close}>
          Close
        </button>
        {stage.retry && (
          <button type="button" onClick={() => void review()}>
            Review again
          </button>
        )}
      </>
    );
  }

  return (
    <Modal
      labelledBy={titleId}
      describedBy={descId}
      onEscape={close}
      initialFocus={closeRef}
      returnFocus={returnFocus}
    >
      <div className="modal-head">
        <h2 id={titleId}>Apply host settings</h2>
      </div>
      <div className="modal-body">{body}</div>
      <div className="modal-foot">
        {footer ?? (
          <button type="button" ref={closeRef} className="ghost" onClick={close}>
            Close
          </button>
        )}
      </div>
    </Modal>
  );
}
