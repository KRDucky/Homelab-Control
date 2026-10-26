// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import {
  ApiError,
  applyServiceAction,
  planServiceAction,
  stepUp,
  type RiskClass,
  type ServiceAction,
  type ServiceOutcome,
  type ServicePlan,
  type UnitObserved,
} from '../api';
import { linkHandler } from '../router';
import { explain } from '../useData';

// The HCIS privileged-operation flow (BRAND-BOOK › Privileged operation
// safety): preview (nothing changed yet) → confirm, with a password re-entry
// for disruptive operations → progress → the observed, backend-verified
// result → audit reference. Nothing here claims success that the broker did
// not verify by reading the unit state back, and nothing offers a rollback.

const verbs: Record<ServiceAction, { label: string; doing: string }> = {
  start: { label: 'Start', doing: 'Starting' },
  stop: { label: 'Stop', doing: 'Stopping' },
  restart: { label: 'Restart', doing: 'Restarting' },
};

const riskText: Record<RiskClass, { label: string; note: string }> = {
  reversible: { label: 'Reversible', note: 'Can generally be undone.' },
  disruptive: { label: 'Disruptive', note: 'May interrupt services or availability.' },
  destructive: { label: 'Destructive', note: 'May cause irreversible data loss.' },
};

/** Risk class as word + icon; colour only reinforces the word (HCIS RiskBadge). */
export function RiskBadge({ risk }: { risk: RiskClass }) {
  return (
    <span className={`risk-badge risk-${risk}`}>
      <svg viewBox="0 0 20 20" width="14" height="14" aria-hidden="true" focusable="false">
        {risk === 'reversible' ? (
          <path d="M5 8a6 6 0 1 1-.5 5M5 3v5h5" fill="none" stroke="currentColor" strokeWidth="1.6" />
        ) : (
          <path d="M10 3 18 17H2Z M10 8v4 M10 14v.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
        )}
      </svg>
      {riskText[risk].label}
    </span>
  );
}

function stateText(o: UnitObserved): string {
  return `${o.active_state}${o.sub_state ? ` (${o.sub_state})` : ''}`;
}

/** Minimal accessible modal: focus trap, Escape, focus starts on Close and returns afterwards. */
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
  /** Where focus goes when the dialog closes (the opener may be gone by then). */
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
    const last = items[items.length - 1];
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
  | { kind: 'preview'; plan: ServicePlan }
  | { kind: 'running'; plan: ServicePlan }
  | { kind: 'result'; outcome: ServiceOutcome }
  | { kind: 'refused'; title: string; message: string; retry: boolean };

/** User-facing text for an operation error: the server's own message for
 * refusals (policy, conflict, expired review, busy, unavailable). */
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
  'conflict',
  'busy',
  'policy_denied',
  'bad_request',
  'forbidden',
]);

// The server's message already says whether anything ran (journal or audit unavailable).
const selfDescribing = new Set(['unavailable', 'audit_unavailable', 'outcome_unknown']);

function secondsLeft(iso: string, now: number): number {
  return Math.max(0, Math.floor((Date.parse(iso) - now) / 1000));
}

export function ServiceActionDialog({
  unit,
  action,
  csrf,
  stepUpUntil,
  onStepUp,
  onClose,
  onUnauthorized,
  onShowLogs,
  auditAvailable,
  returnFocus,
}: {
  unit: string;
  action: ServiceAction;
  csrf: string;
  stepUpUntil: string | null;
  onStepUp: (until: string) => void;
  /** changed: an operation was executed (the list should refresh). */
  onClose: (changed: boolean) => void;
  onUnauthorized: () => void;
  onShowLogs?: ((unit: string) => void) | undefined;
  auditAvailable: boolean;
  returnFocus: () => void;
}) {
  const [stage, setStage] = useState<Stage>({ kind: 'planning' });
  const [password, setPassword] = useState('');
  const [pwError, setPwError] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [executed, setExecuted] = useState(false);
  const closeRef = useRef<HTMLButtonElement>(null);
  const pwRef = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const descId = useId();
  const pwId = useId();
  const verb = verbs[action];

  const review = useCallback(async () => {
    setStage({ kind: 'planning' });
    try {
      const plan = await planServiceAction(unit, action, csrf);
      setStage({ kind: 'preview', plan });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return onUnauthorized();
      const e = err instanceof ApiError ? err : null;
      setStage({
        kind: 'refused',
        title: e?.code === 'policy_denied' ? 'Not allowed for this unit' : `Cannot ${action} ${unit}`,
        message: opExplain(err),
        retry: e?.code !== 'policy_denied' && e?.status !== 403 && e?.status !== 404,
      });
    }
  }, [unit, action, csrf, onUnauthorized]);

  useEffect(() => {
    const t = window.setTimeout(() => void review(), 0);
    return () => window.clearTimeout(t);
  }, [review]);

  useEffect(() => {
    if (stage.kind !== 'preview') return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [stage.kind]);

  // Each new stage starts with focus on Close (HCIS ConfirmDialog rule), so
  // keyboard and screen-reader users are never left on a removed element.
  useEffect(() => {
    if (stage.kind !== 'running') closeRef.current?.focus();
  }, [stage.kind]);

  const stepUpValid = stepUpUntil !== null && Date.parse(stepUpUntil) - now > 5_000;

  async function confirm(plan: ServicePlan) {
    setPwError('');
    if (plan.step_up_required && !stepUpValid) {
      if (!password) {
        setPwError('Enter your password to confirm this disruptive operation.');
        pwRef.current?.focus();
        return;
      }
      try {
        const r = await stepUp(password, csrf);
        onStepUp(r.step_up_until);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return onUnauthorized();
        setPwError(
          err instanceof ApiError && err.status === 429
            ? 'Too many failed attempts; try again later.'
            : 'The password is not correct.',
        );
        pwRef.current?.focus();
        return;
      } finally {
        setPassword('');
      }
    }
    setStage({ kind: 'running', plan });
    try {
      const outcome = await applyServiceAction(plan.unit, plan.action, plan.token, csrf);
      setExecuted(true);
      setStage({ kind: 'result', outcome });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return onUnauthorized();
      const e = err instanceof ApiError ? err : null;
      if (e?.code === 'step_up_required') {
        onStepUp(new Date(0).toISOString());
        setPwError('Your password confirmation has expired. Enter it again.');
        setStage({ kind: 'preview', plan });
        return;
      }
      const titles: Record<string, string> = {
        plan_invalid: 'This review is no longer valid',
        conflict: 'The unit changed since you reviewed it',
        busy: 'Another operation on this unit is in progress',
        policy_denied: 'Not allowed for this unit',
        outcome_unknown: 'The result is unknown',
      };
      setStage({
        kind: 'refused',
        title: titles[e?.code ?? ''] ?? `Could not ${action} ${unit}`,
        // Only refusals that happen before execution can promise that
        // nothing changed; anything else may have run.
        message:
          opExplain(err) +
          (refusedBeforeRun.has(e?.code ?? '')
            ? ' Nothing was changed.'
            : selfDescribing.has(e?.code ?? '')
              ? ''
              : ' The operation may or may not have run: check the unit state on the Services page and the operation journal.'),
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
        Reviewing {verb.label.toLowerCase()} {unit}: checking the unit, its dependencies and the policy…
      </p>
    );
  } else if (stage.kind === 'preview' || stage.kind === 'running') {
    const plan = stage.plan;
    const left = secondsLeft(plan.expires_at, now);
    const expired = left === 0;
    const needPw = plan.step_up_required && !stepUpValid;
    body = (
      <>
        <p className="preview-bar">Preview · nothing has been changed yet</p>
        <dl className="kv">
          <dt>Unit</dt>
          <dd>
            <code>{plan.unit}</code>
            {plan.requested !== plan.unit && <span className="muted"> (requested as {plan.requested})</span>}
          </dd>
          <dt>Now</dt>
          <dd>
            {stateText(plan.observed)}
            {plan.observed.unit_file_state ? `, ${plan.observed.unit_file_state} at boot` : ''}
          </dd>
          <dt>Risk</dt>
          <dd>
            <RiskBadge risk={plan.risk} /> <span className="muted">{riskText[plan.risk].note}</span>
          </dd>
        </dl>
        <p id={descId}>{plan.effect}</p>
        <ul className="consequences">
          {plan.consequences.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        {plan.also_affected && plan.also_affected.length > 0 && (
          <div className="also">
            <p>
              {plan.action === 'start'
                ? 'Conflicting units that systemd stops:'
                : 'Running units systemd stops or restarts together with it:'}
            </p>
            <ul>
              {plan.also_affected.map((u) => (
                <li key={u}>
                  <code>{u}</code>
                </li>
              ))}
            </ul>
          </div>
        )}
        {plan.also_started && plan.also_started.length > 0 && (
          <div className="also">
            <p>Inactive units systemd starts first because the service depends on them:</p>
            <ul>
              {plan.also_started.map((u) => (
                <li key={u}>
                  <code>{u}</code>
                </li>
              ))}
            </ul>
          </div>
        )}
        {needPw && !running && (
          <div className="field">
            <label htmlFor={pwId}>
              Re-enter your password to confirm (required for disruptive operations)
            </label>
            <input
              ref={pwRef}
              id={pwId}
              type="password"
              autoComplete="current-password"
              value={password}
              aria-invalid={pwError ? true : undefined}
              aria-describedby={pwError ? `${pwId}-err` : undefined}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !expired) void confirm(plan);
              }}
            />
          </div>
        )}
        {pwError && (
          <p id={`${pwId}-err`} className="field-error" role="alert">
            {pwError}
          </p>
        )}
        {plan.step_up_required && stepUpValid && !running && (
          <p className="footnote">Your password was confirmed in the last five minutes.</p>
        )}
        <p className="footnote" aria-live="off">
          {expired
            ? 'This review has expired. Review the operation again to continue.'
            : `This review is valid for ${Math.floor(left / 60)} min ${left % 60} s.`}
        </p>
        {running && (
          <p role="status" className="progress-note" aria-busy="true">
            {verb.doing} {plan.unit}… waiting for systemd and reading the state back to verify the result.
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
          <button
            type="button"
            className={plan.risk === 'reversible' ? '' : 'warn-action'}
            disabled={running}
            onClick={() => void confirm(plan)}
          >
            {verb.label} {plan.unit}
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
        {o.observed && (
          <dl className="kv">
            <dt>Observed state</dt>
            <dd>{stateText(o.observed)}</dd>
            {o.observed.result && o.observed.result !== 'success' && (
              <>
                <dt>Result</dt>
                <dd>{o.observed.result}</dd>
              </>
            )}
            {o.observed.main_pid ? (
              <>
                <dt>Main PID</dt>
                <dd>{o.observed.main_pid}</dd>
              </>
            ) : null}
            {o.exit_code !== undefined && o.exit_code !== 0 && (
              <>
                <dt>systemctl exit status</dt>
                <dd>{o.exit_code}</dd>
              </>
            )}
          </dl>
        )}
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
      <>
        <button type="button" ref={closeRef} className="ghost" onClick={close}>
          Close
        </button>
        {onShowLogs && (
          <button
            type="button"
            onClick={() => {
              onShowLogs(o.unit);
              onClose(true);
            }}
          >
            Show logs
          </button>
        )}
      </>
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
        <h2 id={titleId}>
          {verb.label} <code>{unit}</code>
        </h2>
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
