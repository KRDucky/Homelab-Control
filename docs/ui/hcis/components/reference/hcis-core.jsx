/* HCIS — Homelab Control Interface System. Hand-written reference components.
 * Production ports: Section/Sources, StateDot, Meter (frontend/app/src/ui.tsx, Meter.tsx).
 * Everything else is a PROPOSED HCIS component: not yet in the Homelab Control app. */
const React = window.React;
const { useState, useRef, useEffect, useLayoutEffect, useMemo, useId, useCallback } = React;
const cx = (...a) => a.filter(Boolean).join(' ');

/* ---------- Icons: original set, 20px grid, 1.5px stroke, currentColor ---------- */
const P = {
  overview: 'M3 3h6v6H3zM11 3h6v4h-6zM11 9h6v8h-6zM3 11h6v6H3z',
  health: 'M2 10h3.5l2-5 3 10 2-6 1.5 1H18',
  system: 'M6 6h8v8H6zM8 2v3M12 2v3M8 15v3M12 15v3M2 8h3M2 12h3M15 8h3M15 12h3',
  storage: 'M4 5c0-1.1 2.7-2 6-2s6 .9 6 2-2.7 2-6 2-6-.9-6-2zM4 5v10c0 1.1 2.7 2 6 2s6-.9 6-2V5M4 10c0 1.1 2.7 2 6 2s6-.9 6-2',
  containers: 'M3 6.5 10 3l7 3.5v7L10 17l-7-3.5zM3 6.5 10 10l7-3.5M10 10v7',
  vm: 'M2.5 4h15v9h-15zM7 17h6M10 13v4M6 7.5h3',
  network: 'M10 3v4M10 13v4M4 10H3M17 10h-1M10 7a3 3 0 1 1 0 6 3 3 0 0 1 0-6zM5 5l2 2M15 15l-2-2M15 5l-2 2M5 15l2-2',
  share: 'M2.5 5.5c0-.6.4-1 1-1H8l1.5 2h7c.6 0 1 .4 1 1v8c0 .6-.4 1-1 1h-13c-.6 0-1-.4-1-1zM8 12.5h4M10 10.5l2 2-2 2',
  packages: 'M3 6.5 10 3l7 3.5v7L10 17l-7-3.5zM6.5 4.8l7 3.5v3',
  hardware: 'M3 5h14v10H3zM6 15v2M14 15v2M6 8h5M6 11h3M14 8.5v.01',
  audit: 'M5 3h10v14H5zM8 7h4M8 10h4M8 13h2',
  settings: 'M4 5h7M15 5h1M4 10h2M10 10h6M4 15h9M17 15h-1M13 3v4M8 8v4M15 13v4',
  search: 'M8.5 3a5.5 5.5 0 1 1 0 11 5.5 5.5 0 0 1 0-11zM12.5 12.5 17 17',
  bell: 'M5 13V9a5 5 0 0 1 10 0v4l1.5 2h-13zM8 17h4',
  activity: 'M10 3a7 7 0 1 1-7 7M10 6v4l3 2',
  'chevron-right': 'M8 5l5 5-5 5', 'chevron-down': 'M5 8l5 5 5-5', 'chevron-left': 'M12 5l-5 5 5 5',
  check: 'M4 10.5l4 4 8-9',
  warning: 'M10 3 18 17H2zM10 8v4M10 14.5v.01',
  critical: 'M7 2.5h6L17.5 7v6L13 17.5H7L2.5 13V7zM7.5 7.5l5 5M12.5 7.5l-5 5',
  info: 'M10 2.5a7.5 7.5 0 1 1 0 15 7.5 7.5 0 0 1 0-15zM10 9v5M10 6.2v.01',
  unknown: 'M10 2.5a7.5 7.5 0 1 1 0 15 7.5 7.5 0 0 1 0-15zM7.8 8a2.3 2.3 0 0 1 4.4.8c0 1.5-2.2 1.8-2.2 3.2M10 14.3v.01',
  play: 'M6 4l10 6-10 6z', stop: 'M5 5h10v10H5z', restart: 'M15.5 7A6 6 0 1 0 16 12M16 3v4h-4',
  more: 'M5 10v.01M10 10v.01M15 10v.01', 'sort': 'M7 8l3-3 3 3M7 12l3 3 3-3', 'sort-asc': 'M10 15V5M6 9l4-4 4 4', 'sort-desc': 'M10 5v10M6 11l4 4 4-4',
  folder: 'M2.5 5.5c0-.6.4-1 1-1H8l1.5 2h7c.6 0 1 .4 1 1v8c0 .6-.4 1-1 1h-13c-.6 0-1-.4-1-1z',
  file: 'M5 2.5h6.5L15 6v11.5H5zM11.5 2.5V6H15',
  lock: 'M5 9h10v8H5zM7 9V6.5a3 3 0 0 1 6 0V9',
  user: 'M10 3a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7zM3.5 17.5c.8-3 3.4-4.5 6.5-4.5s5.7 1.5 6.5 4.5',
  terminal: 'M2.5 4h15v12h-15zM6 8l3 2-3 2M10.5 12.5H14',
  copy: 'M7 7h10v10H7zM13 7V3H3v10h4',
  filter: 'M3 4h14l-5.5 6.5V16l-3-1.5v-4z',
  plus: 'M10 4v12M4 10h12', close: 'M5 5l10 10M15 5 5 15',
  menu: 'M3 5h14M3 10h14M3 15h14', sidebar: 'M3 4h14v12H3zM8 4v12',
  server: 'M3 3.5h14v5H3zM3 11.5h14v5H3zM6 6h.01M6 14h.01',
  drive: 'M3 6h14v8H3zM13.5 10h.01M6 10h4',
  locate: 'M10 2v3M10 15v3M2 10h3M15 10h3M10 6a4 4 0 1 1 0 8 4 4 0 0 1 0-8z',
  history: 'M3 10a7 7 0 1 0 2-5M3 3v3h3M10 6v4l2.5 1.5',
  logs: 'M3 4h14M3 8h10M3 12h14M3 16h8',
  wrench: 'M12.5 3a4 4 0 0 0-3.7 5.5L3 14.3 5.7 17l5.8-5.8A4 4 0 0 0 17 7.5l-2.4 2.4-2.5-.4-.4-2.5L14.1 4.6A4 4 0 0 0 12.5 3z',
  shield: 'M10 2.5 16 5v5c0 3.6-2.6 6.4-6 7.5-3.4-1.1-6-3.9-6-7.5V5z',
  external: 'M11 3h6v6M17 3l-8 8M14 12v5H3V6h5',
  host: 'M3 4h14v9H3zM7 17h6M10 13v4',
  download: 'M10 3v10M6 9l4 4 4-4M3 16h14',
  key: 'M7 8a4 4 0 1 1 3.5 4L9 13.5H7.5V15H6v1.5H3V14l4.4-4.4A4 4 0 0 1 7 8zM13 6h.01',
  pause: 'M6 4h2.5v12H6zM11.5 4H14v12h-2.5z',
  eye: 'M2 10s3-5.5 8-5.5S18 10 18 10s-3 5.5-8 5.5S2 10 2 10zM10 7.5a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5z',
};
function Icon({ name, size, label, className }) {
  const d = P[name] || P.unknown;
  return (
    <svg className={cx('hc-icon', size === 'sm' && 'hc-icon-sm', className)} viewBox="0 0 20 20" fill="none" stroke="currentColor"
      strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" role={label ? 'img' : undefined}
      aria-label={label} aria-hidden={label ? undefined : 'true'} focusable="false">
      <path d={d} />
    </svg>
  );
}
Icon.names = Object.keys(P);
Icon.paths = P;

const TONE_ICON = { ok: 'check', warn: 'warning', crit: 'critical', info: 'info', unknown: 'unknown', neutral: null, accent: null };
const Example = ({ children }) => <span className="hc-example">{children || 'Example data'}</span>;

/* ---------- Production: Section + Sources (frontend/app/src/ui.tsx) ---------- */
function Sources({ meta }) {
  const problems = [...(meta.errors || []), ...(meta.status === 'unavailable' ? [] : meta.notes || [])];
  return (
    <details className="hc-sources">
      <summary>Where this data comes from</summary>
      <ul>{meta.sources.map((s) => <li key={s}><code>{s}</code></li>)}</ul>
      {problems.length > 0 && <ul className="hc-source-errors">{problems.map((e) => <li key={e}>{e}</li>)}</ul>}
    </details>
  );
}
const statusText = { ok: '', partial: 'Partly available', unavailable: 'Unavailable' };
/* ---------- Production: StateDot ---------- */
function StateDot({ tone = 'idle', pulse }) {
  return <span className={cx('hc-dot', 'hc-dot-' + tone, pulse && 'hc-dot-pulse')} aria-hidden="true" />;
}

/* ---------- Production: Meter (frontend/app/src/Meter.tsx) ---------- */
const SEGMENTS = 24;
const ratio = (u, t) => (u === undefined || t === undefined || t <= 0 ? undefined : Math.min(1, Math.max(0, u / t)));
function levelOf(r, th = { warn: 0.75, crit: 0.9 }) { return r >= th.crit ? 'crit' : r >= th.warn ? 'warn' : 'ok'; }
function Meter({ label, used, total, detail, thresholds, compact, hideHead }) {
  const r = ratio(used, total);
  const filled = r === undefined ? 0 : Math.round(r * SEGMENTS);
  const lvl = r === undefined ? 'unknown' : levelOf(r, thresholds);
  const pct = r === undefined ? undefined : Math.round(r * 100);
  const word = { ok: '', warn: 'High', crit: 'Critical', unknown: '' }[lvl];
  return (
    <div className={cx('hc-meter', compact && 'hc-meter-compact')}>
      {!hideHead && (
        <div className="hc-meter-head">
          <span className="hc-meter-label">{label}</span>
          <span className="hc-meter-value">
            {word && <StatusPill tone={lvl} label={word} />} {pct === undefined ? 'Unknown' : pct + '%'}
          </span>
        </div>
      )}
      <div className={'hc-meter-track hc-meter-' + lvl} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100}
        aria-valuenow={pct || 0} aria-valuetext={pct === undefined ? 'Unknown' : `${pct}% — ${detail || ''}`}>
        {Array.from({ length: SEGMENTS }, (_, i) => <span key={i} className={i < filled ? 'hc-seg hc-seg-on' : 'hc-seg'} />)}
      </div>
      {detail && <div className="hc-meter-detail">{detail}</div>}
    </div>
  );
}

/* ---------- Actions ---------- */
function Button({ variant = 'primary', size = 'md', icon, busy, disabledReason, children, onClick, type = 'button', ...rest }) {
  const rid = useId();
  const disabled = !!disabledReason;
  return (
    <span>
      <button type={type} className={cx('hc-btn', variant !== 'primary' && 'hc-btn-' + variant, size === 'sm' && 'hc-btn-sm')}
        aria-disabled={disabled || undefined} aria-busy={busy || undefined}
        aria-describedby={disabled ? rid : undefined}
        onClick={(e) => { if (disabled || busy) { e.preventDefault(); return; } onClick && onClick(e); }} {...rest}>
        {busy ? <span className="hc-spinner" aria-hidden="true" /> : icon && <Icon name={icon} size="sm" />}
        {children}
      </button>
      {disabled && <span id={rid} className="hc-btn-reason">{disabledReason}</span>}
    </span>
  );
}
function IconButton({ icon, label, variant = 'ghost', onClick, ...rest }) {
  return (
    <button type="button" className={cx('hc-btn hc-iconbtn', 'hc-btn-' + variant)} aria-label={label} title={label} onClick={onClick} {...rest}>
      <Icon name={icon} />
    </button>
  );
}
function useOutside(ref, open, close) {
  useEffect(() => {
    if (!open) return;
    const f = (e) => { if (ref.current && !ref.current.contains(e.target)) close(); };
    document.addEventListener('mousedown', f);
    return () => document.removeEventListener('mousedown', f);
  }, [open]);
}
function Dropdown({ label, icon = 'more', items, align = 'start', variant = 'secondary', iconOnly }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  const menuId = useId();
  useOutside(ref, open, () => setOpen(false));
  useEffect(() => { if (open && ref.current) { const f = ref.current.querySelector('[role=menuitem]'); f && f.focus(); } }, [open]);
  const onKey = (e) => {
    const its = [...ref.current.querySelectorAll('[role=menuitem]')];
    const i = its.indexOf(document.activeElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); its[(i + 1) % its.length].focus(); }
    if (e.key === 'ArrowUp') { e.preventDefault(); its[(i - 1 + its.length) % its.length].focus(); }
    if (e.key === 'Escape') { setOpen(false); ref.current.querySelector('button').focus(); }
  };
  return (
    <div className="hc-menu-wrap" ref={ref} onKeyDown={open ? onKey : undefined}>
      <button type="button" className={cx('hc-btn', 'hc-btn-' + variant, iconOnly && 'hc-iconbtn')} aria-haspopup="menu" aria-expanded={open}
        aria-controls={menuId} aria-label={iconOnly ? label : undefined} onClick={() => setOpen(!open)}>
        {iconOnly ? <Icon name={icon} /> : <>{label}<Icon name="chevron-down" size="sm" /></>}
      </button>
      {open && (
        <ul className={cx('hc-menu', align === 'end' && 'hc-menu-end')} role="menu" id={menuId}>
          {items.map((it, i) => it === '-' ? <li key={i} className="hc-menu-sep" role="separator" /> : (
            <li key={it.id || i} role="none">
              <button type="button" role="menuitem" className={cx('hc-menu-item', it.danger && 'hc-danger')}
                onClick={() => { setOpen(false); it.onSelect && it.onSelect(); }}>
                {it.icon && <Icon name={it.icon} size="sm" />}{it.label}{it.shortcut && <kbd className="hc-kbd">{it.shortcut}</kbd>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/* ---------- Forms ---------- */
function TextField({ label, help, error, mono, icon, id, ...rest }) {
  const auto = useId(); const fid = id || auto; const hid = fid + '-h'; const eid = fid + '-e';
  const input = <input id={fid} className={cx('hc-input', mono && 'hc-input-mono')} aria-invalid={error ? 'true' : undefined}
    aria-describedby={cx(help && hid, error && eid) || undefined} {...rest} />;
  return (
    <div className="hc-field">
      {label && <label className="hc-label" htmlFor={fid}>{label}</label>}
      {icon ? <div className="hc-input-wrap"><Icon name={icon} size="sm" />{input}</div> : input}
      {help && <span id={hid} className="hc-help">{help}</span>}
      {error && <span id={eid} className="hc-error"><Icon name="critical" size="sm" />{error}</span>}
    </div>
  );
}
function SearchField({ label = 'Search', shortcut, ...rest }) {
  return (
    <label className="hc-input-wrap">
      <Icon name="search" size="sm" />
      <span className="hc-vh">{label}</span>
      <input type="search" className="hc-input" {...rest} />
      {shortcut && <kbd className="hc-kbd" aria-hidden="true">{shortcut}</kbd>}
    </label>
  );
}
function Select({ label, options, help, id, ...rest }) {
  const auto = useId(); const fid = id || auto;
  return (
    <div className="hc-field">
      {label && <label className="hc-label" htmlFor={fid}>{label}</label>}
      <select id={fid} className="hc-select" {...rest}>
        {options.map((o) => typeof o === 'string' ? <option key={o} value={o}>{o}</option> : <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
      {help && <span className="hc-help">{help}</span>}
    </div>
  );
}
function Checkbox({ label, indeterminate, ...rest }) {
  const ref = useRef(null);
  useEffect(() => { if (ref.current) ref.current.indeterminate = !!indeterminate; }, [indeterminate]);
  return <label className="hc-check"><input type="checkbox" ref={ref} {...rest} /><span>{label}</span></label>;
}
function Switch({ label, checked, onChange, disabled }) {
  const id = useId();
  return (
    <span className="hc-switch">
      <button type="button" role="switch" id={id} aria-checked={!!checked} disabled={disabled} onClick={() => onChange && onChange(!checked)} />
      <label htmlFor={id}>{label}</label>
      <span className="hc-switch-state" aria-hidden="true">{checked ? 'On' : 'Off'}</span>
    </span>
  );
}
function SegmentedControl({ legend, name, options, value, onChange }) {
  const auto = useId();
  return (
    <fieldset className="hc-segmented">
      <legend className="hc-vh">{legend}</legend>
      {options.map((o) => (
        <label key={o.id} className={value === o.id ? 'on' : ''}>
          <input type="radio" name={name || auto} value={o.id} checked={value === o.id} onChange={() => onChange(o.id)} />
          {o.icon && <Icon name={o.icon} size="sm" />}{o.label}{o.count !== undefined && <span className="hc-count">{o.count}</span>}
        </label>
      ))}
    </fieldset>
  );
}

/* ---------- Status ---------- */
function StatusPill({ tone = 'neutral', label, detail, icon }) {
  const ic = icon === undefined ? TONE_ICON[tone] : icon;
  return (
    <span className={cx('hc-pill', tone !== 'neutral' && 'hc-pill-' + (tone === 'unknown' ? 'neutral' : tone))}>
      {ic && <Icon name={ic} size="sm" />}{label}{detail && <span className="hc-pill-detail">{detail}</span>}
    </span>
  );
}
function Badge({ tone = 'neutral', children, count }) {
  if (count !== undefined) return <span className="hc-badge hc-badge-count" aria-label={count + ' ' + (children || '')}>{count}</span>;
  return <span className={cx('hc-badge', tone !== 'neutral' && 'hc-badge-' + tone)}>{children}</span>;
}
function Banner({ tone = 'info', title, children, action, role }) {
  return (
    <div className={'hc-banner hc-banner-' + tone} role={role || (tone === 'crit' ? 'alert' : 'status')}>
      <Icon name={TONE_ICON[tone] || 'info'} />
      <div>{title && <p className="hc-banner-title">{title}</p>}<p>{children}</p></div>
      {action || <span />}
    </div>
  );
}
const CHECK_TEXT = { ok: 'OK', warn: 'Review', crit: 'Problem', info: 'Note' };
function CheckList({ items }) {
  return (
    <ul className="hc-checks">
      {items.map((c) => (
        <li key={c.id}>
          <StateDot tone={c.level === 'info' ? 'idle' : c.level} />
          <StatusPill tone={c.level} label={CHECK_TEXT[c.level]} />
          <span>{c.message}</span>
          {c.action || <span />}
        </li>
      ))}
    </ul>
  );
}
function Confidence({ level }) {
  const m = { verified: ['check', 'Verified'], inferred: ['warning', 'Inferred'], unknown: ['unknown', 'Unknown'] }[level];
  return <span className={'hc-conf hc-conf-' + level}><Icon name={m[0]} size="sm" />{m[1]}</span>;
}

/* ---------- Data ---------- */
function KeyValueList({ items, layout = 'grid' }) {
  return (
    <dl className={cx('hc-kv', layout === 'rows' && 'hc-kv-rows')}>
      {items.map((it) => <div key={it.term}><dt>{it.term}</dt><dd className={it.mono ? 'hc-mono' : undefined}>{it.value}</dd></div>)}
    </dl>
  );
}
function ResourceCard({ icon, title, subtitle, status, metric, unit, children, href, onClick }) {
  const body = (
    <>
      <div className="hc-card-head">
        {icon && <Icon name={icon} />}
        <div><h3 className="hc-card-title">{title}</h3>{subtitle && <div className="hc-card-sub">{subtitle}</div>}</div>
        <span className="hc-spacer" />{status}
      </div>
      {metric !== undefined && <div className="hc-metric">{metric}<small>{unit}</small></div>}
      {children}
    </>
  );
  if (href) return <a className="hc-card" href={href}>{body}</a>;
  if (onClick) return <button type="button" className="hc-card" onClick={onClick}>{body}</button>;
  return <article className="hc-card">{body}</article>;
}
function Sparkline({ values, label }) {
  const max = Math.max(...values), min = Math.min(...values); const w = 96, h = 24;
  const d = values.map((v, i) => (i ? 'L' : 'M') + ((i / (values.length - 1)) * w).toFixed(1) + ' ' + (h - 2 - ((v - min) / (max - min || 1)) * (h - 4)).toFixed(1)).join(' ');
  return <svg className="hc-spark" viewBox={`0 0 ${w} ${h}`} role="img" aria-label={label}><path d={d} /></svg>;
}
function MetricChart({ title, series, labels, unit = '', max, height = 160 }) {
  const [hover, setHover] = useState(null);
  const [table, setTable] = useState(false);
  const W = 560, H = height, L = 36, B = 20, T = 8;
  const top = max || Math.max(...series.flatMap((s) => s.values)) * 1.1;
  const x = (i) => L + (i / (labels.length - 1)) * (W - L - 4);
  const y = (v) => T + (1 - v / top) * (H - T - B);
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * top);
  const onMove = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * W;
    setHover(Math.max(0, Math.min(labels.length - 1, Math.round(((px - L) / (W - L - 4)) * (labels.length - 1)))));
  };
  return (
    <figure className="hc-stack" style={{ margin: 0 }}>
      <div className="hc-row">
        <figcaption className="hc-label">{title}</figcaption><span className="hc-spacer" />
        {hover !== null && <span className="hc-count">{labels[hover]}: {series.map((s) => `${s.name} ${s.values[hover]}${unit}`).join(' · ')}</span>}
        <Button variant="ghost" size="sm" onClick={() => setTable(!table)} aria-pressed={table}>{table ? 'Chart' : 'Table'}</Button>
      </div>
      {table ? (
        <DataTable caption={title} captionHidden rowKey={(r) => r.t}
          columns={[{ id: 't', header: 'Time' }, ...series.map((s) => ({ id: s.name, header: s.name + (unit ? ` (${unit})` : ''), numeric: true }))]}
          rows={labels.map((t, i) => Object.assign({ t }, ...series.map((s) => ({ [s.name]: s.values[i] }))))} />
      ) : (
        <svg className="hc-chart" viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${title}: ${series.map((s) => s.name).join(', ')}. Use the Table button for values.`}
          onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
          {ticks.map((t) => <g key={t}><line className="hc-grid-line" x1={L} x2={W} y1={y(t)} y2={y(t)} /><text className="hc-axis" x={L - 6} y={y(t) + 4} textAnchor="end">{Math.round(t)}{unit}</text></g>)}
          {labels.map((l, i) => i % Math.ceil(labels.length / 6) === 0 && <text key={l} className="hc-axis" x={x(i)} y={H - 4} textAnchor="middle">{l}</text>)}
          {series.map((s, si) => {
            const d = s.values.map((v, i) => (i ? 'L' : 'M') + x(i).toFixed(1) + ' ' + y(v).toFixed(1)).join(' ');
            return <g key={s.name}>
              {si === 0 && <path className={'hc-area hc-c1'} d={d + ` L${x(labels.length - 1)} ${y(0)} L${L} ${y(0)} Z`} stroke="none" />}
              <path className={'hc-series hc-c' + (si + 1)} d={d} />
            </g>;
          })}
          {hover !== null && <line className="hc-cursor" x1={x(hover)} x2={x(hover)} y1={T} y2={H - B} />}
        </svg>
      )}
      <ul className="hc-legend">{series.map((s, i) => <li key={s.name}><span className={'hc-swatch hc-c' + (i + 1)} />{s.name}</li>)}</ul>
    </figure>
  );
}
function ProgressBar({ value, max = 100, label, indeterminate }) {
  const pct = indeterminate ? undefined : Math.round((value / max) * 100);
  return (
    <div className={cx('hc-progress', indeterminate && 'hc-progress-indeterminate')} role="progressbar" aria-label={label}
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
      <span style={indeterminate ? undefined : { width: pct + '%' }} />
    </div>
  );
}

/* ---------- Navigation & shell ---------- */
function Breadcrumbs({ items }) {
  return (
    <nav className="hc-crumbs" aria-label="Breadcrumb">
      <ol>{items.map((it, i) => (
        <li key={it.label}>
          {i > 0 && <Icon name="chevron-right" size="sm" />}
          {i === items.length - 1 ? <span aria-current="page">{it.label}</span> : <a href={it.href || '#'}>{it.label}</a>}
        </li>
      ))}</ol>
    </nav>
  );
}
function Tabs({ tabs, value, onChange, label = 'Sections', children }) {
  const [inner, setInner] = useState(tabs[0].id);
  const cur = value || inner; const set = onChange || setInner; const base = useId();
  const onKey = (e) => {
    const i = tabs.findIndex((t) => t.id === cur);
    let n = null;
    if (e.key === 'ArrowRight') n = (i + 1) % tabs.length; if (e.key === 'ArrowLeft') n = (i - 1 + tabs.length) % tabs.length;
    if (e.key === 'Home') n = 0; if (e.key === 'End') n = tabs.length - 1;
    if (n !== null) { e.preventDefault(); set(tabs[n].id); e.currentTarget.querySelectorAll('[role=tab]')[n].focus(); }
  };
  return (
    <div>
      <div className="hc-tabs" role="tablist" aria-label={label} onKeyDown={onKey}>
        {tabs.map((t) => (
          <button key={t.id} type="button" role="tab" id={base + t.id} aria-selected={cur === t.id} aria-controls={base + t.id + '-p'}
            tabIndex={cur === t.id ? 0 : -1} className="hc-tab" onClick={() => set(t.id)}>
            {t.icon && <Icon name={t.icon} size="sm" />}{t.label}{t.badge !== undefined && <Badge count={t.badge}>{t.label}</Badge>}
          </button>
        ))}
      </div>
      <div className="hc-tabpanel" role="tabpanel" id={base + cur + '-p'} aria-labelledby={base + cur} tabIndex={0}>
        {typeof children === 'function' ? children(cur) : children}
      </div>
    </div>
  );
}
const RESOURCE_TABS = [
  { id: 'overview', label: 'Overview', icon: 'overview' }, { id: 'configuration', label: 'Configuration', icon: 'settings' },
  { id: 'logs', label: 'Logs', icon: 'logs' }, { id: 'diagnostics', label: 'Diagnostics', icon: 'health' },
  { id: 'history', label: 'History', icon: 'history' }, { id: 'actions', label: 'Actions', icon: 'wrench' },
];
function ResourceHeader({ kind, name, icon = 'server', status, facts, actions }) {
  return (
    <section className="hc-reshead" aria-label={kind}>
      <span className="hc-reshead-icon"><Icon name={icon} /></span>
      <div>
        <div className="hc-reshead-kind">{kind}</div>
        <div className="hc-row"><h1>{name}</h1>{status}</div>
      </div>
      <div className="hc-reshead-actions">{actions}</div>
      {facts && <KeyValueList items={facts} />}
    </section>
  );
}
const NAV = [
  { group: null, items: [{ id: 'overview', label: 'Overview', icon: 'overview' }, { id: 'health', label: 'Health & Diagnostics', icon: 'health' }] },
  { group: 'Manage', items: [{ id: 'system', label: 'System', icon: 'system' }, { id: 'storage', label: 'Storage', icon: 'storage' },
    { id: 'docker', label: 'Docker', icon: 'containers' }, { id: 'virtualization', label: 'Virtualization', icon: 'vm' },
    { id: 'networking', label: 'Networking', icon: 'network' }, { id: 'sharing', label: 'File Sharing', icon: 'share' },
    { id: 'packages', label: 'Packages', icon: 'packages' }, { id: 'hardware', label: 'Hardware', icon: 'hardware' }] },
  { group: 'Govern', items: [{ id: 'audit', label: 'Audit', icon: 'audit' }, { id: 'settings', label: 'Settings', icon: 'settings' }] },
];
/* ---------- Overlays ---------- */
function Dialog({ open, onClose, title, icon, children, footer, className, labelledBy }) {
  const ref = useRef(null); const hid = useId();
  useEffect(() => {
    const d = ref.current; if (!d) return;
    if (open && !d.open) d.showModal(); if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog ref={ref} className={cx('hc-dialog', className)} aria-labelledby={labelledBy || hid} onClose={onClose} onCancel={onClose}>
      {open && <>
        {title && <div className="hc-dialog-head">{icon}<h2 id={hid}>{title}</h2><span className="hc-spacer" /><IconButton icon="close" label="Close" onClick={onClose} /></div>}
        {children}
        {footer && <div className="hc-dialog-foot">{footer}</div>}
      </>}
    </dialog>
  );
}
function CommandPalette({ open, onClose, commands }) {
  const [q, setQ] = useState(''); const [act, setAct] = useState(0); const lid = useId();
  const res = commands.filter((c) => (c.label + ' ' + (c.keywords || '')).toLowerCase().includes(q.toLowerCase()));
  useEffect(() => { setAct(0); }, [q]);
  useEffect(() => { if (!open) setQ(''); }, [open]);
  const groups = [...new Set(res.map((r) => r.group))];
  const run = (c) => { onClose(); c.run && c.run(); };
  return (
    <Dialog open={open} onClose={onClose} className="hc-palette" labelledBy={lid + 'l'}>
      <div className="hc-input-wrap">
        <Icon name="search" />
        <label id={lid + 'l'} className="hc-vh" htmlFor={lid + 'i'}>Search pages, resources and actions</label>
        <input id={lid + 'i'} className="hc-input" autoFocus role="combobox" aria-expanded="true" aria-controls={lid} aria-autocomplete="list"
          aria-activedescendant={res[act] ? lid + res[act].id : undefined} placeholder="Search pages, resources and actions…" value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') { e.preventDefault(); setAct(Math.min(res.length - 1, act + 1)); }
            if (e.key === 'ArrowUp') { e.preventDefault(); setAct(Math.max(0, act - 1)); }
            if (e.key === 'Enter' && res[act]) run(res[act]);
          }} />
      </div>
      <ul className="hc-palette-list" role="listbox" id={lid} aria-label="Results">
        {groups.map((g) => (
          <li key={g} role="presentation">
            <div className="hc-palette-group" aria-hidden="true">{g}</div>
            <ul role="group" aria-label={g} style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {res.filter((r) => r.group === g).map((c) => {
                const i = res.indexOf(c);
                return (
                  <li key={c.id} id={lid + c.id} role="option" aria-selected={i === act} className="hc-palette-opt" onMouseEnter={() => setAct(i)} onClick={() => run(c)}>
                    <Icon name={c.icon || 'chevron-right'} />{c.label}
                    {c.kind === 'operation' && <StatusPill tone="warn" label="Opens a preview" icon={null} />}
                    {c.hint && <span className="hc-hint">{c.hint}</span>}
                  </li>
                );
              })}
            </ul>
          </li>
        ))}
        {res.length === 0 && <li className="hc-empty" style={{ padding: 'var(--hc-space-3)' }}>No matches. Try a unit name, pool, container or “restart”.</li>}
      </ul>
      <div className="hc-palette-foot"><span><kbd className="hc-kbd">↑↓</kbd> move</span><span><kbd className="hc-kbd">Enter</kbd> open</span><span><kbd className="hc-kbd">Esc</kbd> close</span></div>
    </Dialog>
  );
}
function useCommandPaletteShortcut(setOpen) {
  useEffect(() => {
    const f = (e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setOpen(true); } };
    window.addEventListener('keydown', f); return () => window.removeEventListener('keydown', f);
  }, []);
}

/* ---------- Operations ---------- */
function LogViewer({ lines, title = 'Logs', follow: initialFollow = true }) {
  const [q, setQ] = useState(''); const [lvl, setLvl] = useState('all'); const [follow, setFollow] = useState(initialFollow);
  const ref = useRef(null);
  const shown = lines.filter((l) => (lvl === 'all' || l.level === lvl) && (!q || l.msg.toLowerCase().includes(q.toLowerCase())));
  useEffect(() => { if (follow && ref.current) ref.current.scrollTop = ref.current.scrollHeight; }, [shown.length, follow]);
  const hi = (m) => { if (!q) return m; const i = m.toLowerCase().indexOf(q.toLowerCase()); if (i < 0) return m; return <>{m.slice(0, i)}<mark>{m.slice(i, i + q.length)}</mark>{m.slice(i + q.length)}</>; };
  return (
    <div className="hc-stack">
      <div className="hc-toolbar" style={{ margin: 0 }}>
        <SearchField label={'Filter ' + title} placeholder="Filter lines" value={q} onChange={(e) => setQ(e.target.value)} />
        <SegmentedControl legend="Level" value={lvl} onChange={setLvl} options={[{ id: 'all', label: 'All' }, { id: 'err', label: 'Errors' }, { id: 'warn', label: 'Warnings' }, { id: 'info', label: 'Info' }]} />
        <Switch label="Follow" checked={follow} onChange={setFollow} />
        <span className="hc-count" aria-live="polite">{shown.length} of {lines.length} lines</span>
      </div>
      <pre className="hc-log" ref={ref} tabIndex={0} aria-label={title}>
        {shown.map((l, i) => (
          <div key={i} className={'hc-log-line hc-log-' + l.level}>
            <span className="hc-log-ts">{l.ts}</span><span className="hc-log-lvl">{l.level.toUpperCase()}</span><span className="hc-log-msg">{hi(l.msg)}</span>
          </div>
        ))}
      </pre>
    </div>
  );
}
function AuditTimeline({ events }) {
  return (
    <ol className="hc-timeline">
      {events.map((e) => (
        <li key={e.id} className={'hc-timeline-' + (e.outcome || 'info')}>
          <span className="hc-timeline-mark"><Icon name={e.outcome === 'ok' ? 'check' : e.outcome === 'crit' ? 'close' : e.outcome === 'warn' ? 'warning' : e.icon || 'info'} /></span>
          <div>
            <div className="hc-timeline-head"><strong>{e.title}</strong>
              {e.outcome && <StatusPill tone={e.outcome} label={{ ok: 'Succeeded', crit: 'Failed', warn: 'Partial', info: 'Info' }[e.outcome]} />}</div>
            <div className="hc-timeline-meta"><time>{e.time}</time> · {e.actor} · <code>{e.scope}</code></div>
            {e.detail && <div className="hc-help">{e.detail}</div>}
          </div>
        </li>
      ))}
    </ol>
  );
}
const STEP_ICON = { done: 'check', run: 'activity', wait: 'chevron-right', fail: 'critical' };
function ResourceInspector({ title, kind, fields, raw, onClose }) {
  const [view, setView] = useState('fields');
  return (
    <aside className="hc-inspector" aria-label={'Inspector: ' + title}>
      <div className="hc-inspector-head">
        <div><div className="hc-reshead-kind">{kind}</div><strong>{title}</strong></div><span className="hc-spacer" />
        <SegmentedControl legend="View" value={view} onChange={setView} options={[{ id: 'fields', label: 'Fields' }, { id: 'raw', label: 'Raw' }]} />
        {onClose && <IconButton icon="close" label="Close inspector" onClick={onClose} />}
      </div>
      <div className="hc-inspector-body">
        {view === 'fields' ? <KeyValueList layout="rows" items={fields} /> : <pre className="hc-log" style={{ padding: 'var(--hc-space-3)' }}><code>{raw}</code></pre>}
      </div>
    </aside>
  );
}

/* ---------- Domain views ---------- */
function HealthScore({ score, label = 'Health score' }) {
  const r = 30, c = 2 * Math.PI * r; const tone = score >= 85 ? 'ok' : score >= 60 ? 'warn' : 'crit';
  return (
    <svg className={'hc-score-ring hc-' + tone} viewBox="0 0 72 72" role="img" aria-label={`${label}: ${score} of 100, ${{ ok: 'healthy', warn: 'needs attention', crit: 'critical' }[tone]}`}>
      <circle className="hc-ring-track" cx="36" cy="36" r={r} />
      <circle className="hc-ring-val" cx="36" cy="36" r={r} strokeDasharray={`${(score / 100) * c} ${c}`} />
      <text x="36" y="42" textAnchor="middle">{score}</text>
    </svg>
  );
}
function PermissionEditor({ value = '0755', onChange, compact, label = 'Permissions' }) {
  const digits = value.slice(-3).split('').map(Number);
  const who = ['Owner', 'Group', 'Others']; const bits = [['Read', 4], ['Write', 2], ['Execute', 1]];
  const toggle = (w, b) => { const d = [...digits]; d[w] ^= b; onChange && onChange('0' + d.join('')); };
  const sym = digits.map((d) => (d & 4 ? 'r' : '-') + (d & 2 ? 'w' : '-') + (d & 1 ? 'x' : '-')).join('');
  return (
    <div className="hc-stack">
      <table className="hc-perm">
        <caption className={compact ? "hc-vh" : "hc-perm-caption"}>{label}</caption>
        <thead><tr><th scope="col"><span className="hc-vh">Who</span></th>{bits.map(([b]) => <th key={b} scope="col">{b}</th>)}</tr></thead>
        <tbody>{who.map((w, wi) => (
          <tr key={w}><th scope="row">{w}</th>{bits.map(([b, v]) => (
            <td key={b}><Checkbox label={<span className="hc-vh">{w} {b}</span>} checked={!!(digits[wi] & v)} onChange={() => toggle(wi, v)} /></td>
          ))}</tr>
        ))}</tbody>
      </table>
      <div className="hc-row"><span className="hc-octal">{'0' + digits.join('')}</span><span className="hc-mono hc-muted">{sym}</span>
        {!compact && digits[2] & 2 ? <StatusPill tone="warn" label="World-writable" /> : null}</div>
    </div>
  );
}
function FileBrowser({ tree, files, path, onOpen }) {
  const [cur, setCur] = useState(path);
  const [sel, setSel] = useState(null);
  const Node = ({ n, base }) => {
    const p = base + '/' + n.name;
    return (
      <li>
        <button type="button" aria-current={cur === p ? 'true' : undefined} onClick={() => setCur(p)}><Icon name="folder" size="sm" />{n.name}</button>
        {n.children && <ul>{n.children.map((c) => <Node key={c.name} n={c} base={p} />)}</ul>}
      </li>
    );
  };
  const parts = cur.split('/').filter(Boolean);
  return (
    <div className="hc-files">
      <nav className="hc-files-tree" aria-label="Folders"><ul>{tree.map((n) => <Node key={n.name} n={n} base="" />)}</ul></nav>
      <div className="hc-files-main">
        <div className="hc-files-path">
          <Breadcrumbs items={parts.map((p, i) => ({ label: p, href: '#' }))} /><span className="hc-spacer" />
          <IconButton icon="copy" label="Copy path" /><Button size="sm" variant="secondary" icon="plus">New folder…</Button>
        </div>
        <DataTable caption={'Contents of ' + cur} captionHidden rowKey={(f) => f.name} selectable
          columns={[
            { id: 'name', header: 'Name', cell: (f) => <span className="hc-row" style={{ flexWrap: 'nowrap' }}><Icon name={f.dir ? 'folder' : 'file'} size="sm" /><span className="hc-mono">{f.name}</span></span> },
            { id: 'size', header: 'Size', numeric: true, sortValue: (f) => f.bytes || 0 },
            { id: 'owner', header: 'Owner', cell: (f) => <span className="hc-owner">{f.owner}</span> },
            { id: 'mode', header: 'Mode', cell: (f) => <span className="hc-mono">{f.mode}</span> },
            { id: 'modified', header: 'Modified' },
          ]} rows={files}
          bulkActions={() => <><Button size="sm" variant="secondary" icon="lock">Permissions…</Button><Button size="sm" variant="secondary" icon="download">Download</Button></>} />
      </div>
    </div>
  );
}
function StoragePools({ pools }) {
  const kinds = ['All', ...new Set(pools.map((p) => p.kind))];
  const [k, setK] = useState('All');
  return (
    <div className="hc-stack">
      <Tabs tabs={kinds.map((x) => ({ id: x, label: x }))} value={k} onChange={setK} label="Storage technology">
        {() => (
          <div className="hc-grid" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(18rem, 1fr))' }}>
            {pools.filter((p) => k === 'All' || p.kind === k).map((p) => (
              <ResourceCard key={p.name} icon="storage" title={p.name} subtitle={p.kind + ' · ' + p.layout} status={<StatusPill tone={p.tone} label={p.state} />}>
                <Meter label="Used" used={p.used} total={p.size} detail={`${p.used} of ${p.size} TiB`} compact />
                {p.vdevs && <div className="hc-vdev" aria-label="Layout">{p.vdevs.map((v) => <div key={v.name} className="hc-vdev-disk"><StateDot tone={v.tone} /><span className="hc-mono">{v.name}</span><span className="hc-count">{v.note}</span></div>)}</div>}
                <div className="hc-row hc-count">{p.notes}</div>
              </ResourceCard>
            ))}
          </div>
        )}
      </Tabs>
    </div>
  );
}
function VMList({ vms }) {
  const tone = (s) => ({ running: 'ok', stopped: 'unknown', paused: 'warn', crashed: 'crit' }[s]);
  return <DataTable caption="Virtual machines" columns={[
    { id: 'name', header: 'Name', cell: (v) => <span className="hc-row" style={{ flexWrap: 'nowrap' }}><Icon name="vm" size="sm" />{v.name}</span> },
    { id: 'state', header: 'State', cell: (v) => <StatusPill tone={tone(v.state)} label={v.state} /> },
    { id: 'vcpu', header: 'vCPU', numeric: true }, { id: 'ram', header: 'Memory', numeric: true },
    { id: 'cpu', header: 'CPU (1 h)', sortable: false, cell: (v) => v.cpu ? <Sparkline values={v.cpu} label={`${v.name} CPU over the last hour`} /> : '—' },
    { id: 'ip', header: 'Address', cell: (v) => <span className="hc-mono">{v.ip || '—'}</span> },
    { id: 'act', header: <span className="hc-vh">Actions</span>, sortable: false, cell: (v) => <span className="hc-row" style={{ flexWrap: 'nowrap' }}>
      <IconButton icon={v.state === 'running' ? 'pause' : 'play'} label={(v.state === 'running' ? 'Pause ' : 'Start ') + v.name} />
      <IconButton icon="terminal" label={'Console for ' + v.name} /></span> },
  ]} rows={vms} rowKey={(v) => v.name} selectable rowTone={(v) => v.state === 'crashed' ? 'crit' : null}
    bulkActions={() => <Button size="sm" variant="secondary" icon="stop">Shut down…</Button>} />;
}
function NetworkInterfaces({ ifaces }) {
  return <DataTable caption="Network interfaces" columns={[
    { id: 'name', header: 'Interface', cell: (i) => <span className="hc-mono">{i.name}</span> },
    { id: 'kind', header: 'Type' },
    { id: 'state', header: 'Link', cell: (i) => <StatusPill tone={i.state === 'up' ? 'ok' : 'crit'} label={i.state} detail={i.speed} /> },
    { id: 'addr', header: 'Addresses', wrap: true, cell: (i) => <span className="hc-mono">{i.addr.join(', ') || '—'}</span> },
    { id: 'rx', header: 'Traffic', sortable: false, cell: (i) => i.rx ? <Sparkline values={i.rx} label={i.name + ' receive rate'} /> : '—' },
    { id: 'members', header: 'Members', cell: (i) => <span className="hc-mono hc-muted">{i.members || '—'}</span> },
  ]} rows={ifaces} rowKey={(i) => i.name} />;
}
function PackageList({ packages }) {
  const [f, setF] = useState('updates');
  const rows = packages.filter((p) => f === 'all' || (f === 'updates' && p.update) || (f === 'security' && p.security));
  return (
    <div className="hc-stack">
      <div className="hc-toolbar" style={{ margin: 0 }}>
        <SegmentedControl legend="Show" value={f} onChange={setF} options={[{ id: 'updates', label: 'Updates', count: packages.filter((p) => p.update).length }, { id: 'security', label: 'Security', count: packages.filter((p) => p.security).length }, { id: 'all', label: 'Installed' }]} />
        <SearchField label="Search packages" placeholder="Search packages" /><span className="hc-spacer" />
        <Button icon="eye">Preview upgrade…</Button>
      </div>
      <DataTable caption="Packages" captionHidden selectable rowKey={(p) => p.name} rows={rows}
        bulkActions={(s) => <Button size="sm" variant="secondary">Preview {s.length} upgrade{s.length === 1 ? '' : 's'}…</Button>}
        columns={[
          { id: 'name', header: 'Package', cell: (p) => <span className="hc-mono">{p.name}</span> },
          { id: 'installed', header: 'Installed', cell: (p) => <span className="hc-mono hc-muted">{p.installed}</span> },
          { id: 'update', header: 'Available', cell: (p) => p.update ? <span className="hc-mono">{p.update}</span> : '—' },
          { id: 'security', header: 'Kind', cell: (p) => p.security ? <StatusPill tone="warn" label="Security" icon="shield" /> : p.update ? <StatusPill label="Update" /> : '' },
          { id: 'repo', header: 'Repository', cell: (p) => <span className="hc-muted">{p.repo}</span> },
        ]} />
    </div>
  );
}
function ShareList({ shares }) {
  return <DataTable caption="File shares" rowKey={(s) => s.name} rows={shares} columns={[
    { id: 'name', header: 'Share', cell: (s) => <span className="hc-row" style={{ flexWrap: 'nowrap' }}><Icon name="share" size="sm" />{s.name}</span> },
    { id: 'proto', header: 'Protocol', cell: (s) => <span className="hc-row">{s.proto.map((p) => <StatusPill key={p} label={p} tone="accent" icon={null} />)}</span> },
    { id: 'path', header: 'Path', cell: (s) => <span className="hc-mono">{s.path}</span> },
    { id: 'access', header: 'Access', cell: (s) => s.guest ? <StatusPill tone="warn" label="Guest access on" /> : <span>{s.access}</span> },
    { id: 'state', header: 'Status', cell: (s) => <StatusPill tone={s.tone} label={s.state} /> },
  ]} />;
}

