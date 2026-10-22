/* ======================================================================
 * HCIS v2 — Adaptive Glass Linux Admin. Rewritten shell, reactive states,
 * risk classes, and the expanded component catalogue. Concatenated after
 * hcis.jsx (shares its scope: React hooks, cx, Icon, Button, …).
 * ==================================================================== */

/* ---------- Reactive state ---------- */
const LIVE = {
  live: { tone: 'ok', word: 'Live' }, refreshing: { tone: 'info', word: 'Refreshing' },
  stale: { tone: 'warn', word: 'Stale' }, paused: { tone: 'idle', word: 'Paused' }, disconnected: { tone: 'crit', word: 'Disconnected' },
};
function LiveIndicator({ state = 'live', updated, onRetry }) {
  const m = LIVE[state];
  return (
    <span className={'hc-live hc-live-' + state} role="status" aria-live="polite" aria-atomic="true">
      {state === 'refreshing' ? <span className="hc-spinner" aria-hidden="true" /> : <StateDot tone={m.tone} pulse={state === 'live'} />}
      <span className="hc-live-word">{m.word}</span>
      {updated && <span className="hc-live-time">· {state === 'live' ? 'updated ' : 'last data '}{updated}</span>}
      {state === 'disconnected' && onRetry && <Button size="sm" variant="ghost" icon="restart" onClick={onRetry}>Retry</Button>}
    </span>
  );
}
function StatusIndicator({ state = 'unknown', label, detail }) {
  const tone = state === 'notice' ? 'info' : state === 'unknown' ? 'idle' : state;
  return (
    <span className={'hc-statusind hc-statusind-' + state}>
      <Icon name={state === 'notice' ? 'info' : TONE_ICON[state] || 'unknown'} size="sm" />
      <span>{label}</span>{detail && <span className="hc-muted"> {detail}</span>}
      <span className="hc-vh"> ({state})</span>
    </span>
  );
}

/* ---------- Production: Section (ui.tsx) + live/glass ---------- */
function Section({ title, meta, children, id, actions, level = 2, live, glass }) {
  const auto = useId();
  const hid = 'h-' + (id || auto.replace(/:/g, ''));
  const H = 'h' + level;
  const m = meta || { status: 'ok', sources: [] };
  const unavailableNotes = m.status === 'unavailable' ? m.notes || [] : [];
  return (
    <section className={cx('hc-section', glass && 'hc-glass')} aria-labelledby={hid} aria-busy={live && live.state === 'refreshing' ? true : undefined}>
      <header className="hc-section-head">
        <H id={hid}>{title}</H>
        {m.status !== 'ok' && <StatusPill tone={m.status === 'partial' ? 'warn' : 'crit'} label={statusText[m.status]} />}
        {live && <LiveIndicator {...live} />}
        {actions && <span className="hc-spacer" />}
        {actions}
      </header>
      {unavailableNotes.length > 0 && <p className="hc-empty">{unavailableNotes.join(' ')}</p>}
      <div className={cx(live && live.state === 'stale' && 'hc-stale')}>{children}</div>
      {m.sources.length > 0 && <Sources meta={m} />}
    </section>
  );
}

/* ---------- Data table (responsive: stacks into cards in narrow containers) ---------- */
function DataTable({ caption, columns, rows, rowKey, selectable, onSelectionChange, rowTone, bulkActions, initialSort, emptyState, captionHidden, onRowClick, activeKey }) {
  const [sort, setSort] = useState(initialSort || null);
  const [sel, setSel] = useState([]);
  const sorted = useMemo(() => {
    if (!sort) return rows;
    const col = columns.find((c) => c.id === sort.id);
    const sv = col.sortValue || ((r) => r[col.id]);
    return [...rows].sort((a, b) => { const x = sv(a), y = sv(b); return (x > y ? 1 : x < y ? -1 : 0) * (sort.dir === 'asc' ? 1 : -1); });
  }, [rows, sort]);
  const keys = rows.map(rowKey);
  useEffect(() => { const live = sel.filter((k) => keys.includes(k)); if (live.length !== sel.length) setSel(live); }, [rows]);
  const setS = (s) => { setSel(s); onSelectionChange && onSelectionChange(s); };
  const all = sel.length === keys.length && keys.length > 0;
  const text = (h) => (typeof h === 'string' ? h : '');
  return (
    <div className="hc-dt">
      {selectable && sel.length > 0 && bulkActions && (
        <div className="hc-bulkbar" role="region" aria-label="Bulk actions">
          <strong>{sel.length} selected</strong>{bulkActions(sel)}<span className="hc-spacer" />
          <Button variant="ghost" size="sm" onClick={() => setS([])}>Clear</Button>
        </div>
      )}
      <div className="hc-table-wrap">
        <table className="hc-table">
          <caption className={captionHidden ? 'hc-vh' : undefined}>{caption}</caption>
          <thead>
            <tr>
              {selectable && <th className="hc-sel" scope="col"><Checkbox label={<span className="hc-vh">Select all</span>} checked={all}
                indeterminate={sel.length > 0 && !all} onChange={() => setS(all ? [] : keys)} /></th>}
              {columns.map((c) => {
                const active = sort && sort.id === c.id;
                const ariaSort = active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : c.sortable === false ? undefined : 'none';
                return (
                  <th key={c.id} scope="col" aria-sort={ariaSort} className={c.numeric ? 'hc-num' : undefined}>
                    {c.sortable === false ? c.header : (
                      <button type="button" className="hc-sort" onClick={() => setSort({ id: c.id, dir: active && sort.dir === 'asc' ? 'desc' : 'asc' })}>
                        {c.header}<Icon name={active ? (sort.dir === 'asc' ? 'sort-asc' : 'sort-desc') : 'sort'} size="sm" />
                      </button>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {sorted.map((r) => {
              const k = rowKey(r); const on = sel.includes(k);
              return (
                <tr key={k} aria-selected={selectable ? on : undefined} data-active={activeKey === k || undefined}
                  className={cx(rowTone && rowTone(r) === 'crit' && 'hc-row-crit', onRowClick && 'hc-row-click')} onClick={onRowClick ? () => onRowClick(r) : undefined}>
                  {selectable && <td className="hc-sel"><Checkbox label={<span className="hc-vh">Select {k}</span>} checked={on}
                    onChange={() => setS(on ? sel.filter((x) => x !== k) : [...sel, k])} onClick={(e) => e.stopPropagation()} /></td>}
                  {columns.map((c, i) => {
                    const v = c.cell ? c.cell(r) : r[c.id];
                    return i === 0 ? <th key={c.id} scope="row" data-label={text(c.header)}>{v}</th> :
                      <td key={c.id} data-label={text(c.header)} className={cx(c.numeric && 'hc-num', c.wrap && 'hc-wrap')}>{v}</td>;
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
        {sorted.length === 0 && <div className="hc-dt-empty">{emptyState || <EmptyState title="Nothing to show" />}</div>}
      </div>
    </div>
  );
}
function FilterBar({ query, onQuery, placeholder = 'Filter', facets = [], onFacet, count, total, actions }) {
  const active = facets.filter((f) => f.value && f.value !== 'all');
  return (
    <div className="hc-filterbar" role="search">
      <SearchField label={placeholder} placeholder={placeholder} value={query} onChange={(e) => onQuery(e.target.value)} />
      {facets.map((f) => (
        <label key={f.id} className="hc-facet"><span className="hc-vh">{f.label}</span>
          <select className="hc-select" value={f.value} onChange={(e) => onFacet(f.id, e.target.value)}>
            <option value="all">{f.label}: all</option>{f.options.map((o) => <option key={o} value={o}>{o}</option>)}
          </select>
        </label>
      ))}
      {active.map((f) => (
        <button key={f.id} type="button" className="hc-chip" onClick={() => onFacet(f.id, 'all')} aria-label={`Remove filter ${f.label}: ${f.value}`}>
          {f.label}: {f.value}<Icon name="close" size="sm" />
        </button>
      ))}
      <span className="hc-spacer" />
      {count !== undefined && <span className="hc-count" aria-live="polite">{count} of {total}</span>}
      {actions}
    </div>
  );
}

/* ---------- Tree ---------- */
function ResourceTree({ nodes, selected, onSelect, label = 'Resources', defaultExpanded }) {
  const [open, setOpen] = useState(() => new Set(defaultExpanded || nodes.map((n) => n.id)));
  const flat = [];
  const walk = (ns, depth, parent) => ns.forEach((n) => { flat.push({ n, depth, parent }); if (n.children && open.has(n.id)) walk(n.children, depth + 1, n.id); });
  walk(nodes, 0, null);
  const [focus, setFocus] = useState(selected || (flat[0] && flat[0].n.id));
  const ref = useRef(null);
  const move = (id) => { setFocus(id); const el = ref.current && ref.current.querySelector(`[data-id="${CSS.escape(id)}"]`); el && el.focus(); };
  const onKey = (e) => {
    const i = flat.findIndex((f) => f.n.id === focus); if (i < 0) return; const cur = flat[i];
    const tog = (v) => { const s = new Set(open); v ? s.add(cur.n.id) : s.delete(cur.n.id); setOpen(s); };
    if (e.key === 'ArrowDown' && flat[i + 1]) { e.preventDefault(); move(flat[i + 1].n.id); }
    else if (e.key === 'ArrowUp' && flat[i - 1]) { e.preventDefault(); move(flat[i - 1].n.id); }
    else if (e.key === 'ArrowRight' && cur.n.children) { e.preventDefault(); if (!open.has(cur.n.id)) tog(true); else move(cur.n.children[0].id); }
    else if (e.key === 'ArrowLeft') { e.preventDefault(); if (cur.n.children && open.has(cur.n.id)) tog(false); else if (cur.parent) move(cur.parent); }
    else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect && onSelect(cur.n); }
    else if (e.key === 'Home') { e.preventDefault(); move(flat[0].n.id); }
    else if (e.key === 'End') { e.preventDefault(); move(flat[flat.length - 1].n.id); }
  };
  return (
    <ul className="hc-tree" role="tree" aria-label={label} ref={ref} onKeyDown={onKey}>
      {flat.map(({ n, depth }) => (
        <li key={n.id} role="treeitem" data-id={n.id} aria-level={depth + 1} aria-expanded={n.children ? open.has(n.id) : undefined}
          aria-selected={selected === n.id} tabIndex={focus === n.id ? 0 : -1} style={{ '--hc-depth': depth }}
          onClick={() => { setFocus(n.id); onSelect && onSelect(n); }} onFocus={() => setFocus(n.id)}>
          <span className="hc-tree-row">
            {n.children ? <span className="hc-tree-twist" onClick={(e) => { e.stopPropagation(); const s = new Set(open); s.has(n.id) ? s.delete(n.id) : s.add(n.id); setOpen(s); }}>
              <Icon name={open.has(n.id) ? 'chevron-down' : 'chevron-right'} size="sm" /></span> : <span className="hc-tree-twist" />}
            {n.icon && <Icon name={n.icon} size="sm" />}<span className="hc-tree-label">{n.label}</span>
            {n.status && <StateDot tone={n.status} />}{n.meta && <span className="hc-tree-meta">{n.meta}</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}

/* ---------- Panels & tiles ---------- */
function DetailPanel({ kind, title, onClose, actions, children, floating }) {
  return (
    <aside className={cx('hc-detail', floating && 'hc-glass hc-detail-floating')} aria-label={(kind ? kind + ': ' : '') + title}>
      <header className="hc-detail-head">
        <div>{kind && <div className="hc-reshead-kind">{kind}</div>}<strong>{title}</strong></div><span className="hc-spacer" />
        {actions}{onClose && <IconButton icon="close" label="Close details" onClick={onClose} />}
      </header>
      <div className="hc-detail-body">{children}</div>
    </aside>
  );
}
function MetricTile({ label, value, unit, tone, trend, detail, icon }) {
  return (
    <div className={cx('hc-mtile', tone && 'hc-mtile-' + tone)}>
      <div className="hc-mtile-head">{icon && <Icon name={icon} size="sm" />}<span>{label}</span>
        {tone && tone !== 'ok' && <StatusPill tone={tone} label={{ warn: 'High', crit: 'Critical', info: 'Info', unknown: 'Unknown' }[tone]} />}</div>
      <div className="hc-mtile-val"><span className="hc-metric">{value}</span>{unit && <small>{unit}</small>}</div>
      {trend && <Sparkline values={trend} label={`${label} trend, latest ${value}${unit || ''}`} />}
      {detail && <div className="hc-meter-detail">{detail}</div>}
    </div>
  );
}
function CapacityBar({ label, segments, total, unit = 'TiB' }) {
  const used = segments.reduce((a, s) => a + s.value, 0);
  const free = Math.max(0, total - used);
  const pct = (v) => (total > 0 ? (v / total) * 100 : 0);
  return (
    <figure className="hc-capbar">
      <figcaption className="hc-meter-head"><span className="hc-meter-label">{label}</span><span>{used.toFixed(1)} of {total} {unit} · {Math.round(pct(used))}% used</span></figcaption>
      <div className="hc-fsbar" role="img" aria-label={`${label}: ${segments.map((s) => `${s.label} ${s.value} ${unit}`).join(', ')}, free ${free.toFixed(1)} ${unit}`}>
        {segments.map((s, i) => <span key={s.label} className={'hc-c' + (s.series || i + 1)} style={{ width: pct(s.value) + '%' }} />)}
      </div>
      <ul className="hc-legend">{segments.map((s, i) => <li key={s.label}><span className={'hc-swatch hc-c' + (s.series || i + 1)} />{s.label} {s.value} {unit}</li>)}<li><span className="hc-swatch hc-free" />Free {free.toFixed(1)} {unit}</li></ul>
    </figure>
  );
}
function chartSummary(series, labels, unit) {
  return series.map((s) => {
    const max = Math.max(...s.values), min = Math.min(...s.values), last = s.values[s.values.length - 1];
    return `${s.name}: now ${last}${unit}, range ${min}–${max}${unit} (peak at ${labels[s.values.indexOf(max)]})`;
  }).join('. ') + '.';
}
function TimeSeriesChart(props) {
  return <div className="hc-stack"><MetricChart {...props} /><p className="hc-chart-summary">{chartSummary(props.series, props.labels, props.unit || '')}</p></div>;
}
function HealthSummary({ counts, lastScan, scanning, onScan, unchecked = 0 }) {
  const items = [['crit', 'Critical'], ['warn', 'Warning'], ['notice', 'Notice'], ['ok', 'Healthy'], ['unknown', 'Unknown']];
  const worst = counts.crit ? 'crit' : counts.warn ? 'warn' : counts.notice ? 'notice' : unchecked || counts.unknown ? 'unknown' : 'ok';
  const headline = { crit: `${counts.crit} critical problem${counts.crit > 1 ? 's' : ''}`, warn: `${counts.warn} warning${counts.warn > 1 ? 's' : ''} to review`, notice: 'No problems, some notices', unknown: 'Not fully checked', ok: 'All checks passed' }[worst];
  return (
    <section className="hc-hsum hc-glass" aria-label="Health summary">
      <div className="hc-hsum-head">
        <StatusIndicator state={worst} label={headline} />
        <span className="hc-spacer" />
        <span className="hc-count">{scanning ? 'Scanning…' : 'Last scan ' + lastScan}</span>
        {onScan && <Button size="sm" variant="secondary" icon="restart" busy={scanning} onClick={onScan}>{scanning ? 'Scanning' : 'Scan now'}</Button>}
      </div>
      <ul className="hc-hsum-counts">
        {items.map(([k, w]) => (
          <li key={k} className={'hc-hsum-' + k}><Icon name={k === 'notice' ? 'info' : TONE_ICON[k]} size="sm" /><strong>{counts[k] || 0}</strong><span>{w}</span></li>
        ))}
      </ul>
      {unchecked > 0 && <p className="hc-help">{unchecked} module{unchecked > 1 ? 's were' : ' was'} not checked. Unchecked areas are shown as Unknown, never as healthy.</p>}
    </section>
  );
}

/* ---------- Dependency graph ---------- */
function DependencyGraph({ levels, links, selected, onSelect, label = 'Relationships' }) {
  const wrap = useRef(null); const [paths, setPaths] = useState([]);
  const draw = useCallback(() => {
    const w = wrap.current; if (!w) return; const b = w.getBoundingClientRect();
    setPaths(links.map(([a, z]) => {
      const A = w.querySelector(`[data-node="${CSS.escape(a)}"]`), Z = w.querySelector(`[data-node="${CSS.escape(z)}"]`);
      if (!A || !Z) return null; const ra = A.getBoundingClientRect(), rz = Z.getBoundingClientRect();
      const x1 = ra.right - b.left, y1 = ra.top + ra.height / 2 - b.top, x2 = rz.left - b.left, y2 = rz.top + rz.height / 2 - b.top, mx = (x1 + x2) / 2;
      return { k: a + '>' + z, d: `M${x1} ${y1} C${mx} ${y1} ${mx} ${y2} ${x2} ${y2}`, on: selected && (a === selected || z === selected) };
    }).filter(Boolean));
  }, [links, selected]);
  useLayoutEffect(() => { draw(); const ro = new ResizeObserver(draw); wrap.current && ro.observe(wrap.current); return () => ro.disconnect(); }, [draw]);
  return (
    <div className="hc-dep" ref={wrap} role="group" aria-label={label}>
      <svg className="hc-dep-lines" aria-hidden="true">{paths.map((p) => <path key={p.k} d={p.d} className={p.on ? 'hc-on' : undefined} />)}</svg>
      {levels.map((lv) => (
        <div key={lv.kind} className="hc-dep-col">
          <div className="hc-nav-group">{lv.kind}</div>
          {lv.nodes.map((n) => (
            <button key={n.id} type="button" data-node={n.id} className="hc-dep-node" aria-pressed={selected === n.id} onClick={() => onSelect && onSelect(n)}>
              <Icon name={n.icon || 'server'} size="sm" /><span className="hc-dep-label">{n.label}</span>{n.status && <StateDot tone={n.status} />}
            </button>
          ))}
        </div>
      ))}
      <p className="hc-vh">{links.map(([a, z]) => `${a} → ${z}`).join('; ')}</p>
    </div>
  );
}

/* ---------- Inputs ---------- */
function NumericInput({ label, value, onChange, min, max, step = 1, unit, help, error, id }) {
  const auto = useId(); const fid = id || auto;
  const clamp = (v) => Math.min(max ?? Infinity, Math.max(min ?? -Infinity, v));
  const n = Number(value);
  return (
    <div className="hc-field">
      {label && <label className="hc-label" htmlFor={fid}>{label}</label>}
      <div className="hc-num-input">
        <button type="button" className="hc-btn hc-btn-secondary hc-iconbtn" aria-label={'Decrease ' + label} tabIndex={-1} onClick={() => onChange(clamp(n - step))} disabled={min !== undefined && n <= min}>−</button>
        <input id={fid} className="hc-input hc-input-mono" inputMode="numeric" value={value} aria-invalid={error ? 'true' : undefined}
          onChange={(e) => { const v = e.target.value.replace(/[^\d-]/g, ''); onChange(v === '' || v === '-' ? v : clamp(Number(v))); }}
          onKeyDown={(e) => { if (e.key === 'ArrowUp') { e.preventDefault(); onChange(clamp(n + step)); } if (e.key === 'ArrowDown') { e.preventDefault(); onChange(clamp(n - step)); } }} />
        <button type="button" className="hc-btn hc-btn-secondary hc-iconbtn" aria-label={'Increase ' + label} tabIndex={-1} onClick={() => onChange(clamp(n + step))} disabled={max !== undefined && n >= max}>+</button>
        {unit && <span className="hc-count">{unit}</span>}
      </div>
      {help && <span className="hc-help">{help}</span>}
      {error && <span className="hc-error"><Icon name="critical" size="sm" />{error}</span>}
    </div>
  );
}
function Combobox({ label, options, value, onChange, placeholder, help, allowCustom, customHint, mono }) {
  const id = useId(); const [open, setOpen] = useState(false); const [act, setAct] = useState(0);
  const cur = options.find((o) => o.value === value);
  const [q, setQ] = useState(cur ? cur.label : value || '');
  useEffect(() => { const c = options.find((o) => o.value === value); setQ(c ? c.label : value || ''); }, [value]);
  const res = options.filter((o) => (o.label + ' ' + (o.detail || '')).toLowerCase().includes(q.toLowerCase()) || !q || (cur && q === cur.label));
  const custom = allowCustom && q && !options.some((o) => o.label === q) ? allowCustom(q) : null;
  const all = custom ? [...res, custom] : res;
  const pick = (o) => { onChange(o.value); setQ(o.label); setOpen(false); };
  return (
    <div className="hc-field hc-combo">
      <label className="hc-label" htmlFor={id}>{label}</label>
      <input id={id} className={cx('hc-input', mono && 'hc-input-mono')} role="combobox" aria-expanded={open} aria-controls={id + 'l'} aria-autocomplete="list"
        aria-activedescendant={open && all[act] ? id + 'o' + act : undefined} placeholder={placeholder} value={q}
        onChange={(e) => { setQ(e.target.value); setOpen(true); setAct(0); }} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 120)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') { e.preventDefault(); setOpen(true); setAct(Math.min(all.length - 1, act + 1)); }
          if (e.key === 'ArrowUp') { e.preventDefault(); setAct(Math.max(0, act - 1)); }
          if (e.key === 'Enter' && open && all[act]) { e.preventDefault(); pick(all[act]); }
          if (e.key === 'Escape') setOpen(false);
        }} />
      {open && all.length > 0 && (
        <ul className="hc-menu hc-combo-list" role="listbox" id={id + 'l'} aria-label={label}>
          {all.map((o, i) => (
            <li key={o.value + i} id={id + 'o' + i} role="option" aria-selected={i === act} className="hc-menu-item" onMouseDown={(e) => { e.preventDefault(); pick(o); }}>
              <span className={mono ? 'hc-mono' : undefined}>{o.label}</span>{o.detail && <span className="hc-hint">{o.detail}</span>}
            </li>
          ))}
        </ul>
      )}
      {help && <span className="hc-help">{help}</span>}
      {customHint && q && !cur && <span className="hc-help">{customHint}</span>}
    </div>
  );
}
function RadioGroup({ legend, options, value, onChange, name }) {
  const auto = useId();
  return (
    <fieldset className="hc-radios">
      <legend className="hc-label">{legend}</legend>
      {options.map((o) => (
        <label key={o.id} className={cx('hc-radio', value === o.id && 'on')}>
          <input type="radio" name={name || auto} value={o.id} checked={value === o.id} onChange={() => onChange(o.id)} />
          <span><span className="hc-radio-label">{o.label}</span>{o.description && <span className="hc-help">{o.description}</span>}</span>
        </label>
      ))}
    </fieldset>
  );
}
const PROTECTED = ['/', '/bin', '/boot', '/dev', '/etc', '/home', '/lib', '/proc', '/root', '/run', '/sbin', '/sys', '/usr', '/var', '/var/lib/docker'];
function isProtected(p) { return PROTECTED.includes(p.replace(/\/+$/, '') || '/'); }
function PathPicker({ label, value, onChange, tree, help }) {
  const [open, setOpen] = useState(false); const [pick, setPick] = useState(null);
  const prot = value && isProtected(value);
  const toNodes = (ns, base) => ns.map((n) => ({ id: base + '/' + n.name, label: n.name, icon: 'folder', children: n.children && toNodes(n.children, base + '/' + n.name) }));
  return (
    <div className="hc-field">
      <span className="hc-label">{label}</span>
      <div className="hc-row" style={{ flexWrap: 'nowrap' }}>
        <input aria-label={label} className="hc-input hc-input-mono" value={value} onChange={(e) => onChange(e.target.value)} spellCheck={false} />
        <Button variant="secondary" icon="folder" onClick={() => setOpen(true)}>Browse…</Button>
      </div>
      {prot ? <span className="hc-error"><Icon name="critical" size="sm" />{value} is a protected system path and can't be selected.</span> : help && <span className="hc-help">{help}</span>}
      <Dialog open={open} onClose={() => setOpen(false)} title={'Choose ' + label.toLowerCase()} footer={<>
        <span className="hc-count hc-mono" style={{ marginRight: 'auto' }}>{pick || 'Nothing selected'}</span>
        <Button variant="secondary" onClick={() => setOpen(false)}>Cancel</Button>
        <Button disabledReason={pick && isProtected(pick) ? 'Protected system path' : pick ? undefined : 'Select a folder'} onClick={() => { onChange(pick); setOpen(false); }}>Choose</Button></>}>
        <div className="hc-dialog-body"><ResourceTree label="Folders" nodes={toNodes(tree, '')} selected={pick} onSelect={(n) => setPick(n.id)} /></div>
      </Dialog>
    </div>
  );
}
function UserGroupPicker({ kind = 'user', label, entries, value, onChange }) {
  const opts = entries.map((e) => ({ value: String(e.id), label: e.name, detail: (kind === 'user' ? 'UID ' : 'GID ') + e.id }));
  const match = entries.find((e) => String(e.id) === String(value));
  return (
    <div className="hc-stack" style={{ gap: 'var(--hc-space-1)' }}>
      <Combobox label={label} options={opts} value={String(value)} onChange={onChange} mono
        placeholder={kind === 'user' ? 'Username or UID' : 'Group name or GID'}
        allowCustom={(q) => /^\d+$/.test(q) ? { value: q, label: q, detail: `${kind === 'user' ? 'UID' : 'GID'} ${q} (no local ${kind})` } : null} />
      <span className="hc-help">{match ? `${kind === 'user' ? 'UID' : 'GID'} ${match.id} = ${match.name}` : value !== '' && /^\d+$/.test(String(value)) ? `Numeric ${kind === 'user' ? 'UID' : 'GID'} ${value}: no local ${kind} has this ID (common for container users).` : 'Type a name or a number.'}</span>
    </div>
  );
}

/* ---------- Feedback ---------- */
function Alert({ tone = 'info', title, children }) {
  return (
    <div className={'hc-alert hc-alert-' + tone} role={tone === 'crit' ? 'alert' : 'note'}>
      <Icon name={TONE_ICON[tone] || 'info'} size="sm" /><div>{title && <strong>{title} </strong>}{children}</div>
    </div>
  );
}
function ToastRegion({ toasts, onDismiss }) {
  return (
    <div className="hc-toasts" role="region" aria-label="Notifications" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={'hc-toast hc-glass hc-toast-' + (t.tone || 'info')} role={t.tone === 'crit' ? 'alert' : 'status'}>
          <Icon name={TONE_ICON[t.tone || 'info']} /><div><strong>{t.title}</strong>{t.body && <p>{t.body}</p>}</div>
          {t.action}<IconButton icon="close" label="Dismiss" onClick={() => onDismiss(t.id)} />
        </div>
      ))}
    </div>
  );
}
function EmptyState({ icon = 'folder', title, children, action }) {
  return <div className="hc-state"><span className="hc-state-icon"><Icon name={icon} /></span><h3>{title}</h3>{children && <p>{children}</p>}{action}</div>;
}
function ErrorState({ title, children, detail, onRetry }) {
  return (
    <div className="hc-state hc-state-error" role="alert">
      <span className="hc-state-icon"><Icon name="critical" /></span><h3>{title}</h3>{children && <p>{children}</p>}
      {detail && <details className="hc-sources"><summary>Technical detail</summary><pre className="hc-diff">{detail}</pre></details>}
      {onRetry && <Button variant="secondary" icon="restart" onClick={onRetry}>Try again</Button>}
    </div>
  );
}
function LoadingState({ label = 'Loading', rows = 4 }) {
  return (
    <div className="hc-skel" aria-busy="true" role="status"><span className="hc-vh">{label}…</span>
      {Array.from({ length: rows }, (_, i) => <div key={i} className="hc-skel-row"><span /><span /><span /></div>)}
    </div>
  );
}
function Tooltip({ text, label = 'More information', children }) {
  const [show, setShow] = useState(false); const id = useId();
  return (
    <span className="hc-tip" onMouseEnter={() => setShow(true)} onMouseLeave={() => setShow(false)}>
      {children || <button type="button" className="hc-tip-btn" aria-label={label} aria-describedby={id} onFocus={() => setShow(true)} onBlur={() => setShow(false)}
        onKeyDown={(e) => e.key === 'Escape' && setShow(false)} onClick={() => setShow(!show)}><Icon name="info" size="sm" /></button>}
      <span role="tooltip" id={id} className={cx('hc-tip-body hc-glass-elev', show && 'on')}>{text}</span>
    </span>
  );
}

/* ---------- Risk & operations ---------- */
const RISK = {
  'read-only': { icon: 'eye', word: 'Read-only', note: 'Does not modify the host.' },
  reversible: { icon: 'restart', word: 'Reversible', note: 'Can generally be undone.' },
  disruptive: { icon: 'warning', word: 'Disruptive', note: 'May interrupt services or availability.' },
  destructive: { icon: 'critical', word: 'Destructive', note: 'May cause irreversible data loss.' },
};
const LEVEL_TO_CLASS = { low: 'reversible', medium: 'disruptive', high: 'destructive' };
function RiskBadge({ riskClass }) {
  const r = RISK[riskClass];
  return <span className={'hc-risk-badge hc-risk-' + riskClass}><Icon name={r.icon} size="sm" />{r.word}</span>;
}
function RiskCallout({ riskClass, level, title, items = [] }) {
  const rc = riskClass || LEVEL_TO_CLASS[level] || 'disruptive';
  const r = RISK[rc];
  return (
    <div className={cx('hc-risk', rc === 'destructive' && 'hc-risk-high', rc === 'read-only' && 'hc-risk-safe', rc === 'reversible' && 'hc-risk-low')} role="note">
      <Icon name={r.icon} />
      <div>
        <div className="hc-risk-level">{r.word} · <span className="hc-risk-note">{r.note}</span></div>
        {title && <strong>{title}</strong>}
        {items.length > 0 && <ul>{items.map((i) => <li key={i}>{i}</li>)}</ul>}
      </div>
    </div>
  );
}
function OperationPreview({ title, riskClass = 'disruptive', risk, target, changes, dependencies, consequences, counts, summary, onApply, onCancel, applyLabel = 'Continue to confirmation…' }) {
  const rc = risk ? LEVEL_TO_CLASS[risk] : riskClass;
  return (
    <section className="hc-plan" aria-label={'Preview: ' + title}>
      <div className="hc-plan-mode" role="status"><Icon name="eye" size="sm" /><strong>Preview</strong> · nothing has been changed yet</div>
      <div className="hc-row"><h3 className="hc-card-title">{title}</h3><RiskBadge riskClass={rc} /></div>
      {target && <KeyValueList items={Array.isArray(target) ? target : [{ term: 'Target', value: target.canonical, mono: true }, ...(target.items || [])]} />}
      {summary && <p className="hc-help" style={{ margin: 0 }}>{summary}</p>}
      {counts && <ul className="hc-plan-counts">{counts.map((c) => <li key={c.label}><strong>{c.value}</strong>{c.label}</li>)}</ul>}
      <ul className="hc-plan-list">
        {changes.map((c, i) => (
          <li key={i} className={'hc-plan-' + c.op}>
            <span className="hc-plan-op">{{ add: '+ ADD', change: '~ CHANGE', remove: '− REMOVE', skip: '· SKIP' }[c.op]}</span>
            <div><span>{c.label}</span>
              {c.diff && <pre className="hc-diff">{c.diff.map((d, j) => <div key={j} className={d[0] === '-' ? 'hc-del' : d[0] === '+' ? 'hc-ins' : undefined}>{d}</div>)}</pre>}</div>
          </li>
        ))}
      </ul>
      {dependencies && dependencies.length > 0 && <Alert tone="warn" title="Also affected:">{dependencies.join(' · ')}</Alert>}
      <RiskCallout riskClass={rc} items={consequences} />
      <div className="hc-row"><span className="hc-spacer" />{onCancel && <Button variant="secondary" onClick={onCancel}>Cancel</Button>}
        <Button variant={rc === 'destructive' ? 'danger' : 'primary'} onClick={onApply} disabledReason={onApply ? undefined : 'Design preview: operations never run here'}>{applyLabel}</Button></div>
    </section>
  );
}
function ConfirmDialog({ open, onClose, onConfirm, title, riskClass, risk = 'medium', consequences = [], confirmLabel = 'Confirm', typeToConfirm, reauth, children }) {
  const [typed, setTyped] = useState(''); const [pw, setPw] = useState('');
  useEffect(() => { if (!open) { setTyped(''); setPw(''); } }, [open]);
  const rc = riskClass || LEVEL_TO_CLASS[risk];
  const missing = typeToConfirm && typed !== typeToConfirm ? `Type ${typeToConfirm} to enable` : reauth && !pw ? 'Enter your password to enable' : undefined;
  return (
    <Dialog open={open} onClose={onClose} title={title} className={rc === 'destructive' ? 'hc-dialog-critical' : undefined} icon={<Icon name={RISK[rc].icon} />}
      footer={<>
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button variant={rc === 'destructive' ? 'danger' : 'primary'} disabledReason={missing} onClick={onConfirm}>{confirmLabel}</Button>
      </>}>
      <div className="hc-dialog-body">
        <RiskBadge riskClass={rc} />
        {children}
        <RiskCallout riskClass={rc} items={consequences} />
        {typeToConfirm && <TextField label={<>Type <code>{typeToConfirm}</code> to confirm</>} mono value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} />}
        {reauth && <TextField label="Your password" type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} help="Privileged operations ask you to sign in again." />}
      </div>
    </Dialog>
  );
}
function JobProgress({ title, state = 'running', steps = [], done, total, started, onCancel, onOpen, result }) {
  const verified = result && result.state === 'verified';
  const label = state === 'succeeded' ? (verified ? 'Succeeded · verified' : 'Finished · awaiting verification') : state[0].toUpperCase() + state.slice(1);
  const tone = { queued: 'neutral', running: 'info', succeeded: verified ? 'ok' : 'warn', failed: 'crit', cancelled: 'neutral' }[state];
  return (
    <section className="hc-job" aria-label={title} aria-busy={state === 'running'}>
      <div className="hc-job-head">
        <strong>{title}</strong><StatusPill tone={tone} label={label} />
        <span className="hc-spacer" />{started && <span className="hc-count">{started}</span>}
        {state === 'running' && onCancel && <Button variant="ghost" size="sm" onClick={onCancel}>Cancel</Button>}
        {onOpen && <Button variant="secondary" size="sm" onClick={onOpen}>Details</Button>}
      </div>
      {(state === 'running' || state === 'queued') && <ProgressBar label={title} value={done || 0} max={total || 1} indeterminate={state === 'running' && !total} />}
      {total && state === 'running' && <span className="hc-count">{done} of {total} steps</span>}
      {steps.length > 0 && (
        <ol className="hc-job-steps">
          {steps.map((s) => <li key={s.label} className={'hc-step-' + s.state}><Icon name={STEP_ICON[s.state]} />{s.label}<span className="hc-vh"> — {s.state}</span></li>)}
        </ol>
      )}
      {result && <div className="hc-job-result"><span>Observed result: {result.summary}</span>{result.auditRef && <span className="hc-count">Audit <code>{result.auditRef}</code></span>}</div>}
    </section>
  );
}
function BackgroundJobPanel({ jobs, title = 'Background operations' }) {
  const running = jobs.filter((j) => j.state === 'running').length;
  return (
    <section className="hc-jobpanel hc-glass" aria-label={title}>
      <header className="hc-section-head"><h3>{title}</h3><StatusPill tone={running ? 'info' : 'neutral'} label={running ? `${running} running` : 'Idle'} /></header>
      <div className="hc-stack">{jobs.map((j) => <JobProgress key={j.title} {...j} />)}</div>
    </section>
  );
}

/* ---------- Diagnostics ---------- */
const SEV = { crit: ['crit', 'Critical'], warn: ['warn', 'Warning'], notice: ['info', 'Notice'] };
function DiagnosticFinding({ f, onAck, onSuppress, defaultOpen }) {
  const [open, setOpen] = useState(!!defaultOpen); const id = useId();
  return (
    <article className={cx('hc-finding', 'hc-finding-' + f.severity, f.acknowledged && 'hc-finding-ack')}>
      <header className="hc-finding-head">
        <StatusPill tone={SEV[f.severity][0]} label={SEV[f.severity][1]} icon={f.severity === 'notice' ? 'info' : undefined} />
        <span className={'hc-conf ' + (f.confidence === 'confirmed' ? 'hc-conf-verified' : 'hc-conf-inferred')}>
          <Icon name={f.confidence === 'confirmed' ? 'check' : 'unknown'} size="sm" />{f.confidence === 'confirmed' ? 'Confirmed' : 'Suspected'}</span>
        <button type="button" className="hc-finding-title" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size="sm" />{f.title}</button>
        <span className="hc-spacer" />
        <span className="hc-count">{f.module}</span>
      </header>
      <p className="hc-finding-sum">{f.summary}</p>
      <div className="hc-finding-meta">First seen {f.firstSeen} · last seen {f.lastSeen}{f.acknowledged && ' · acknowledged'}
        {f.verification && <> · <StatusIndicator state={f.verification === 'resolved' ? 'ok' : 'unknown'} label={f.verification === 'resolved' ? 'Resolved (verified by re-scan)' : 'Not yet verified'} /></>}</div>
      <div id={id} hidden={!open} className="hc-finding-body">
        {f.evidence && <><h4>Evidence</h4><pre className="hc-log hc-log-plain">{f.evidence.map((e, i) => <div key={i}>{e}</div>)}</pre></>}
        {f.caveat && <Alert tone="info">{f.caveat}</Alert>}
        {f.investigate && <><h4>Investigate</h4><ol className="hc-steps-plain">{f.investigate.map((s) => <li key={s}>{s}</li>)}</ol></>}
        {f.solution && <><h4>Suggested solution</h4><p>{f.solution}</p></>}
        <div className="hc-row">
          {f.fix && <Button size="sm" icon="eye" variant="secondary">{f.fix}</Button>}
          {onAck && !f.acknowledged && <Button size="sm" variant="ghost" onClick={() => onAck(f.id)}>Acknowledge</Button>}
          {onSuppress && <Button size="sm" variant="ghost" onClick={() => onSuppress(f.id)}>Suppress…</Button>}
        </div>
      </div>
    </article>
  );
}
function ResolutionGuide({ title = 'Suggested recovery steps', steps, docs }) {
  const [copied, setCopied] = useState(null);
  const copy = (c, i) => { try { navigator.clipboard.writeText(c); } catch (e) {} setCopied(i); setTimeout(() => setCopied(null), 1500); };
  return (
    <section className="hc-guide" aria-label={title}>
      <header className="hc-section-head"><h3>{title}</h3></header>
      <Alert tone="info">Nothing here runs automatically. Each step is a suggestion; run commands yourself or use an action that opens a preview.</Alert>
      <ol className="hc-guide-steps">
        {steps.map((s, i) => (
          <li key={i} className={'hc-guide-' + (s.kind || 'fix')}>
            <span className="hc-guide-kind">{{ investigate: 'Investigate', fix: 'Fix', verify: 'Verify' }[s.kind || 'fix']}</span>
            <div>
              <strong>{s.title}</strong>{s.body && <p>{s.body}</p>}
              {s.command && <div className="hc-cmd"><code>{s.command}</code><Button size="sm" variant="ghost" icon={copied === i ? 'check' : 'copy'} onClick={() => copy(s.command, i)}>{copied === i ? 'Copied' : 'Copy'}</Button></div>}
            </div>
          </li>
        ))}
      </ol>
      {docs && <p className="hc-help">Documentation: {docs.map((d, i) => <React.Fragment key={d.href}>{i > 0 && ' · '}<a href={d.href} target="_blank" rel="noreferrer">{d.label}</a></React.Fragment>)}</p>}
    </section>
  );
}
function TerminalViewer({ title = 'Terminal', lines, host = 'host' }) {
  return (
    <section className="hc-term" aria-label={title}>
      <header className="hc-term-head"><Icon name="terminal" size="sm" /><span>{title}</span><span className="hc-spacer" /><StatusPill label="Read-only transcript" icon="eye" /></header>
      <pre className="hc-term-body" tabIndex={0}>{lines.map((l, i) => l.cmd !== undefined
        ? <div key={i}><span className="hc-term-prompt">{l.prompt || host + ':~$'} </span><span className="hc-term-cmd">{l.cmd}</span></div>
        : <div key={i} className={l.err ? 'hc-term-err' : undefined}>{l.out}</div>)}</pre>
    </section>
  );
}

/* ---------- Shell: sidebar, rail, header, appearance, workspaces ---------- */
function Sidebar({ nav = NAV, current, onNavigate, counts = {}, unavailable = {}, hideUnavailable, rail }) {
  return (
    <nav className={cx('hc-sidebar hc-glass', rail && 'hc-rail')} aria-label="Main">
      {nav.map((g, gi) => (
        <div key={gi}>
          {g.group && <div className="hc-nav-group" aria-hidden="true">{g.group}</div>}
          <ul className="hc-nav" aria-label={g.group || 'Home'}>
            {g.items.filter((it) => !(hideUnavailable && unavailable[it.id])).map((it) => unavailable[it.id] ? (
              <li key={it.id}><span className="hc-nav-off" title={`${it.label}: ${unavailable[it.id]}`}><Icon name={it.icon} /><span className="hc-nav-text">{it.label}<span className="hc-nav-why">{unavailable[it.id]}</span></span></span></li>
            ) : (
              <li key={it.id}>
                <a href={'#' + it.id} aria-current={current === it.id ? 'page' : undefined} title={it.label}
                  onClick={(e) => { e.preventDefault(); onNavigate && onNavigate(it.id); }}>
                  <Icon name={it.icon} /><span className="hc-nav-text">{it.label}</span>
                  {counts[it.id] && <span className="hc-nav-count">{counts[it.id]}</span>}
                </a>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}
function NavigationRail(props) { return <Sidebar {...props} rail />; }
const THEMES = [{ id: 'midnight', label: 'Midnight', description: 'Dark, frosted glass (default)' }, { id: 'daybreak', label: 'Daybreak', description: 'Light, Nordic' }, { id: 'graphite', label: 'Graphite', description: 'High-contrast dark' }, { id: 'oled', label: 'OLED', description: 'True black for OLED panels' }];
function AppearanceMenu({ prefs, onChange, align = 'end' }) {
  const [open, setOpen] = useState(false); const ref = useRef(null); const id = useId();
  useOutside(ref, open, () => setOpen(false));
  const set = (k, v) => onChange(Object.assign({}, prefs, { [k]: v }));
  return (
    <div className="hc-menu-wrap" ref={ref} onKeyDown={(e) => { if (e.key === 'Escape') { setOpen(false); ref.current.querySelector('button').focus(); } }}>
      <button type="button" className="hc-btn hc-btn-ghost hc-iconbtn" aria-label="Appearance" title="Appearance" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}><Icon name="eye" /></button>
      {open && (
        <div className={cx('hc-menu hc-popover hc-glass-elev', align === 'end' && 'hc-menu-end')} id={id} role="dialog" aria-label="Appearance">
          <RadioGroup legend="Theme" value={prefs.theme} onChange={(v) => set('theme', v)} options={THEMES} />
          <div className="hc-row hc-popover-row"><Switch label="Transparency" checked={prefs.transparency !== 'reduced'} onChange={(v) => set('transparency', v ? 'full' : 'reduced')} /></div>
          <SegmentedControl legend="Density" value={prefs.density} onChange={(v) => set('density', v)} options={[{ id: 'spacious', label: 'Spacious' }, { id: 'standard', label: 'Standard' }, { id: 'compact', label: 'Compact' }]} />
          <SegmentedControl legend="Motion" value={prefs.motion} onChange={(v) => set('motion', v)} options={[{ id: 'full', label: 'Full' }, { id: 'reduced', label: 'Reduced' }, { id: 'none', label: 'None' }]} />
          <p className="hc-help" style={{ margin: 0 }}>Theme changes colours only. Density, transparency and motion are your layout preferences and stay when you switch themes.</p>
        </div>
      )}
    </div>
  );
}
function WorkspaceSwitcher({ workspaces, current, onChange, onSave }) {
  return <Dropdown variant="ghost" label={(workspaces.find((w) => w.id === current) || workspaces[0]).label}
    items={[...workspaces.map((w) => ({ id: w.id, label: w.label + (w.id === current ? ' (current)' : ''), icon: w.icon || 'overview', onSelect: () => onChange && onChange(w.id) })), '-',
      { label: 'Save layout as workspace…', icon: 'plus', onSelect: onSave }, { label: 'Manage workspaces…', icon: 'settings' }]} />;
}
function GlobalHeader({ hosts, host, onHost, onToggleSidebar, collapsed, onMenu, onSearch, notifications = 0, operations = [], user = 'admin', health, onHealth, prefs, onPrefs, workspaces, workspace, onWorkspace }) {
  const running = operations.filter((o) => o.state === 'running').length;
  return (
    <header className="hc-header hc-glass">
      <span className="hc-show-narrow"><IconButton icon="menu" label="Open navigation" onClick={onMenu} /></span>
      <span className="hc-hide-narrow"><IconButton icon="sidebar" label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-expanded={!collapsed} onClick={onToggleSidebar} /></span>
      <span className="hc-brand"><img src={window.HCIS_MARK || ''} alt="" /><span className="hc-hide-narrow">Homelab Control</span></span>
      <label className="hc-vh" htmlFor="hc-host">Active host</label>
      <select id="hc-host" className="hc-select hc-hostsel" value={host} onChange={(e) => onHost && onHost(e.target.value)}>
        {hosts.map((h) => <option key={h}>{h}</option>)}
      </select>
      {health && <button type="button" className={'hc-healthchip hc-healthchip-' + health.tone} onClick={onHealth} aria-label={'Host health: ' + health.label}>
        <Icon name={TONE_ICON[health.tone] || 'unknown'} size="sm" /><span className="hc-hide-narrow">{health.label}</span></button>}
      {workspaces && <span className="hc-hide-mid"><WorkspaceSwitcher workspaces={workspaces} current={workspace} onChange={onWorkspace} /></span>}
      <div className="hc-input-wrap hc-header-search hc-hide-narrow" onClick={onSearch}>
        <Icon name="search" size="sm" />
        <input className="hc-input" readOnly placeholder="Search or run a command…" aria-label="Open command palette" aria-haspopup="dialog"
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onSearch && onSearch(); }} />
        <kbd className="hc-kbd" aria-hidden="true">Ctrl K</kbd>
      </div>
      <span className="hc-spacer" />
      <span className="hc-show-narrow"><IconButton icon="search" label="Search" onClick={onSearch} /></span>
      <Dropdown variant="ghost" label={`Background jobs, ${running} running`} icon="activity" iconOnly align="end"
        items={operations.length ? operations.map((o) => ({ id: o.id, label: `${o.title} — ${o.state}`, icon: o.state === 'running' ? 'activity' : 'check' })) : [{ label: 'No background jobs' }]} />
      <span className="hc-row hc-bell" style={{ gap: 0 }}>
        <IconButton icon="bell" label={`Notifications, ${notifications} unread`} />
        {notifications > 0 && <Badge count={notifications}>unread</Badge>}
      </span>
      {prefs && <AppearanceMenu prefs={prefs} onChange={onPrefs} />}
      <Dropdown variant="ghost" label={user} align="end" items={[{ label: 'Preferences', icon: 'settings' }, { label: 'My audit log', icon: 'audit' }, '-', { label: 'Sign out', icon: 'external' }]} />
    </header>
  );
}
function AppShell({ children, current, onNavigate, hosts = ['pve-node-01'], density, counts, unavailable, operations, notifications, onSearch, health, workspaces, initialPrefs, applyThemeTo = 'document' }) {
  const [collapsed, setCollapsed] = useState(false);
  const [drawer, setDrawer] = useState(false);
  const [host, setHost] = useState(hosts[0]);
  const [ws, setWs] = useState(workspaces && workspaces[0].id);
  const [prefs, setPrefs] = useState(Object.assign({ theme: document.documentElement.getAttribute('data-theme') || 'midnight', transparency: 'full', density: density || 'standard', motion: 'full' }, initialPrefs));
  useEffect(() => { if (density) setPrefs((p) => Object.assign({}, p, { density })); }, [density]);
  useEffect(() => { if (applyThemeTo === 'document') document.documentElement.setAttribute('data-theme', prefs.theme); }, [prefs.theme]);
  useEffect(() => { if (!drawer) return; const f = (e) => e.key === 'Escape' && setDrawer(false); window.addEventListener('keydown', f); return () => window.removeEventListener('keydown', f); }, [drawer]);
  return (
    <div className="hc hc-cq" data-hc-density={prefs.density} data-hc-transparency={prefs.transparency} data-hc-motion={prefs.motion}>
      <div className="hc-shell" data-collapsed={collapsed} data-drawer={drawer}>
        <a className="hc-skip" href="#hc-main">Skip to content</a>
        <GlobalHeader hosts={hosts} host={host} onHost={setHost} collapsed={collapsed} onToggleSidebar={() => setCollapsed(!collapsed)} onMenu={() => setDrawer(true)}
          operations={operations} notifications={notifications} onSearch={onSearch} health={health} onHealth={() => onNavigate && onNavigate('health')}
          prefs={prefs} onPrefs={setPrefs} workspaces={workspaces} workspace={ws} onWorkspace={setWs} />
        <div className="hc-drawer-scrim" onClick={() => setDrawer(false)} aria-hidden="true" />
        <Sidebar current={current} onNavigate={(id) => { setDrawer(false); onNavigate && onNavigate(id); }} counts={counts} unavailable={unavailable} />
        <main id="hc-main" className="hc-workspace" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}

/* ---------- Showcase: System Overview ---------- */
function OverviewDashboard({ live = 'live', updated = '2 s ago', metrics, chart, alerts, jobs, counts, onRetry }) {
  return (
    <div className="hc-page">
      <div className="hc-pagehead"><div><h1 className="hc-title-1">Overview</h1><p className="hc-help" style={{ margin: 0 }}>pve-node-01 · Debian 13 · up 12 d 4 h</p></div><span className="hc-spacer" />
        <LiveIndicator state={live} updated={updated} onRetry={onRetry} /></div>
      {live === 'disconnected' && <Banner tone="crit" title="Lost connection to hc-api">Showing the last data received 1 min ago. Values are frozen until the connection returns.</Banner>}
      <div className={cx('hc-tilegrid', live !== 'live' && live !== 'refreshing' && 'hc-stale')}>{metrics.map((m) => <MetricTile key={m.label} {...m} />)}</div>
      <div className="hc-dash">
        <Section title="CPU and memory, last hour" level={2} meta={{ status: 'ok', sources: ['/proc/stat', '/proc/meminfo'] }}><TimeSeriesChart {...chart} /></Section>
        <div className="hc-stack">
          <HealthSummary counts={counts} lastScan="4 min ago" unchecked={1} />
          {alerts.map((a) => <Banner key={a.title} tone={a.tone} title={a.title} action={<Button size="sm" variant="secondary">{a.action}</Button>}>{a.body}</Banner>)}
        </div>
      </div>
      <BackgroundJobPanel jobs={jobs} />
    </div>
  );
}

/* ---------- Showcase: Systemd Diagnose & Resolve ---------- */
function ServiceDetail({ unit, description, state, sub, enabled, since, pid, memory, exitCode, restarts, failure, deps = [], logs = [], unitFile, steps = [], docs, history = [] }) {
  const tone = state === 'failed' ? 'crit' : state === 'active' ? 'ok' : 'unknown';
  const tabs = [{ id: 'overview', label: 'Overview', icon: 'overview' }, failure && { id: 'diagnose', label: 'Diagnose & resolve', icon: 'wrench', badge: 1 },
    { id: 'logs', label: 'Logs', icon: 'logs' }, unitFile && { id: 'configuration', label: 'Configuration', icon: 'settings' },
    deps.length && { id: 'dependencies', label: 'Dependencies', icon: 'network' }, history.length && { id: 'history', label: 'History', icon: 'history' }].filter(Boolean);
  const [tab, setTab] = useState(failure ? 'diagnose' : 'overview');
  return (
    <div className="hc-stack">
      <ResourceHeader kind="systemd service" name={unit} icon="system"
        status={<StatusPill tone={tone} label={state} detail={sub} />}
        actions={<><Button variant="secondary" icon="restart">Restart…</Button><Dropdown label="More" items={[{ label: 'Stop…', icon: 'stop' }, { label: enabled ? 'Disable at boot…' : 'Enable at boot…', icon: 'settings' }, '-', { label: 'Copy unit name', icon: 'copy' }]} /></>}
        facts={[{ term: 'Description', value: description }, { term: 'Exit code', value: exitCode || '—', mono: true }, { term: 'Restarts', value: restarts ?? '—' },
          { term: 'At boot', value: enabled ? 'enabled' : 'disabled' }, { term: 'Since', value: since }, { term: 'Main PID', value: pid || '—' }, { term: 'Memory', value: memory }]} />
      <Tabs tabs={tabs} value={tab} onChange={setTab} label="Service sections">
        {(t) => t === 'diagnose' ? (
          <div className="hc-detail-grid">
            <div className="hc-stack">
              <Section title="Why it failed" level={3} meta={{ status: 'ok', sources: ['systemctl show ' + unit, 'journalctl -u ' + unit + ' -b -n 200'] }}>
                <div className="hc-row"><StatusPill tone="crit" label={failure.category} /><span className={'hc-conf ' + (failure.confidence === 'confirmed' ? 'hc-conf-verified' : 'hc-conf-inferred')}>
                  <Icon name={failure.confidence === 'confirmed' ? 'check' : 'unknown'} size="sm" />{failure.confidence === 'confirmed' ? 'Matched in logs' : 'Suspected'}</span></div>
                <p>{failure.summary}</p>
                <h4 className="hc-subhead">Evidence</h4>
                <pre className="hc-log hc-log-plain">{failure.evidence.map((e, i) => <div key={i}>{e}</div>)}</pre>
              </Section>
              <ResolutionGuide steps={steps} docs={docs} />
            </div>
            <div className="hc-stack">
              <Section title="Dependencies" level={3}>{deps.map((d) => <div key={d.unit} className="hc-vdev-disk"><StatusIndicator state={d.state === 'active' ? 'ok' : d.state === 'failed' ? 'crit' : 'unknown'} label={d.unit} detail={d.relation} /></div>)}</Section>
              <Section title="Other failure patterns HCIS recognizes" level={3}>
                <ul className="hc-steps-plain">{['Port already in use', 'Permission denied', 'Missing executable', 'Missing directory', 'Invalid configuration', 'Dependency failure', 'Start-limit reached', 'Timeout', 'Out-of-memory kill'].map((x) => <li key={x}>{x}</li>)}</ul>
              </Section>
            </div>
          </div>
        ) : t === 'overview' ? <KeyValueList items={[{ term: 'Unit file', value: '/usr/lib/systemd/system/' + unit, mono: true }, { term: 'Type', value: 'notify' }, { term: 'Result', value: sub }]} />
          : t === 'logs' ? <LogViewer lines={logs} title={unit + ' journal'} />
          : t === 'configuration' ? <TerminalViewer title={'systemctl cat ' + unit} host="nas-01" lines={[{ cmd: 'systemctl cat ' + unit }, ...unitFile.split('\n').map((out) => ({ out }))]} />
          : t === 'dependencies' ? <DataTable caption="Dependencies" captionHidden rowKey={(d) => d.unit} rows={deps} columns={[{ id: 'unit', header: 'Unit', cell: (d) => <span className="hc-mono">{d.unit}</span> }, { id: 'relation', header: 'Relation' }, { id: 'state', header: 'State', cell: (d) => <StatusIndicator state={d.state === 'active' ? 'ok' : d.state === 'failed' ? 'crit' : 'unknown'} label={d.state} /> }]} />
          : <AuditTimeline events={history} />}
      </Tabs>
    </div>
  );
}

/* ---------- Showcase: Health & Diagnostics ---------- */
function HealthDashboard({ findings: initial, modules = [], scans = [], lastScan = '4 min ago', onScan }) {
  const [findings, setFindings] = useState(initial);
  const [sev, setSev] = useState('all'); const [group, setGroup] = useState('severity'); const [showAck, setShowAck] = useState(false);
  const [scanning, setScanning] = useState(false);
  const counts = { crit: 0, warn: 0, notice: 0, ok: modules.filter((m) => m.state === 'ok').length, unknown: modules.filter((m) => m.state === 'unknown').length };
  findings.forEach((f) => { if (!f.acknowledged) counts[f.severity]++; });
  const shown = findings.filter((f) => (sev === 'all' || f.severity === sev) && (showAck || !f.acknowledged));
  const order = { crit: 0, warn: 1, notice: 2 };
  const groups = group === 'module' ? [...new Set(shown.map((f) => f.module))].map((m) => [m, shown.filter((f) => f.module === m)]) : [['', [...shown].sort((a, b) => order[a.severity] - order[b.severity])]];
  const ack = (id) => setFindings(findings.map((f) => f.id === id ? Object.assign({}, f, { acknowledged: true }) : f));
  return (
    <div className="hc-stack">
      <div className="hc-pagehead"><div><h1 className="hc-title-1">Health &amp; Diagnostics</h1><p className="hc-help" style={{ margin: 0 }}>Fix common problems across the whole system. Scans are read-only.</p></div></div>
      <HealthSummary counts={counts} lastScan={lastScan} scanning={scanning} unchecked={counts.unknown}
        onScan={() => { setScanning(true); setTimeout(() => setScanning(false), 1800); onScan && onScan(); }} />
      <div className="hc-detail-grid">
        <Section title="Findings" actions={<span className="hc-row">
          <SegmentedControl legend="Severity" value={sev} onChange={setSev} options={[{ id: 'all', label: 'All' }, { id: 'crit', label: 'Critical', count: counts.crit }, { id: 'warn', label: 'Warning', count: counts.warn }, { id: 'notice', label: 'Notice', count: counts.notice }]} />
          <Select aria-label="Group by" value={group} onChange={(e) => setGroup(e.target.value)} options={[{ value: 'severity', label: 'Group: severity' }, { value: 'module', label: 'Group: module' }]} />
        </span>}>
          <Checkbox label="Show acknowledged" checked={showAck} onChange={() => setShowAck(!showAck)} />
          {groups.map(([g, fs]) => (
            <div key={g || 'all'} className="hc-stack">
              {g && <div className="hc-nav-group">{g}</div>}
              {fs.map((f, i) => <DiagnosticFinding key={f.id} f={f} onAck={ack} onSuppress={() => {}} defaultOpen={i === 0 && !g} />)}
            </div>
          ))}
          {shown.length === 0 && <EmptyState icon="check" title="No findings match">Clear the filter to see everything. Unchecked modules are listed on the right.</EmptyState>}
        </Section>
        <div className="hc-stack">
          <Section title="Modules" level={3}>
            <ul className="hc-modlist">{modules.map((m) => <li key={m.name}><StatusIndicator state={m.state} label={m.name} />{m.note && <span className="hc-count">{m.note}</span>}</li>)}</ul>
          </Section>
          <Section title="Scan history" level={3}>
            <DataTable caption="Scan history" captionHidden rowKey={(s) => s.time} rows={scans} columns={[{ id: 'time', header: 'Started' }, { id: 'checks', header: 'Checks', numeric: true }, { id: 'found', header: 'Findings', numeric: true }, { id: 'duration', header: 'Took' }]} />
          </Section>
        </div>
      </div>
    </div>
  );
}

/* ---------- Showcase: Docker ---------- */
function ContainerManager({ stacks, images = [], networks = [], volumes = [], engine }) {
  const [tab, setTab] = useState('containers');
  const [open, setOpen] = useState(() => Object.fromEntries(stacks.map((s) => [s.name, true])));
  const [view, setView] = useState('stacks'); const [q, setQ] = useState(''); const [stateF, setStateF] = useState('all');
  const tone = (s) => s === 'running' ? 'ok' : s === 'exited' ? 'crit' : s === 'restarting' ? 'warn' : 'unknown';
  const match = (c) => (!q || (c.name + c.image).toLowerCase().includes(q.toLowerCase())) && (stateF === 'all' || c.state === stateF);
  const cols = [
    { id: 'name', header: 'Container', cell: (c) => <span className="hc-mono">{c.name}</span> },
    { id: 'state', header: 'State', cell: (c) => <span className="hc-row" style={{ flexWrap: 'nowrap' }}><StatusPill tone={tone(c.state)} label={c.state} detail={c.health && `· ${c.health}`} />{c.restarts > 3 && <StatusPill tone="warn" label={`Crash loop · ${c.restarts} restarts`} />}</span> },
    { id: 'image', header: 'Image', cell: (c) => <span className="hc-mono hc-muted">{c.image}</span> },
    { id: 'ports', header: 'Ports', cell: (c) => <span className="hc-mono">{c.ports || '—'}</span>, sortable: false },
    { id: 'cpu', header: 'CPU', numeric: true, cell: (c) => c.cpu + '%' },
    { id: 'mem', header: 'Memory', numeric: true },
    { id: 'act', header: <span className="hc-vh">Actions</span>, sortable: false, cell: (c) => <Dropdown iconOnly variant="ghost" label={'Actions for ' + c.name} align="end"
      items={[{ label: 'Logs', icon: 'logs' }, { label: 'Inspect', icon: 'eye' }, { label: 'Open shell…', icon: 'terminal' }, '-', { label: c.state === 'running' ? 'Stop…' : 'Start…', icon: c.state === 'running' ? 'stop' : 'play' }, { label: 'Restart…', icon: 'restart' }, { label: 'Remove…', icon: 'close', danger: true }]} /> },
  ];
  const all = stacks.flatMap((s) => s.containers.map((c) => Object.assign({ stack: s.name }, c))).filter(match);
  const total = stacks.reduce((a, s) => a + s.containers.length, 0);
  return (
    <div className="hc-stack">
      <Tabs value={tab} onChange={setTab} label="Docker" tabs={[{ id: 'containers', label: 'Containers', icon: 'containers' }, { id: 'images', label: 'Images', icon: 'packages' }, { id: 'networks', label: 'Networks', icon: 'network' }, { id: 'volumes', label: 'Volumes', icon: 'storage' }, { id: 'engine', label: 'Engine', icon: 'settings' }]}>
        {(t) => t === 'containers' ? (
          <div className="hc-stack">
            <FilterBar query={q} onQuery={setQ} placeholder="Filter by name or image" count={all.length} total={total}
              facets={[{ id: 'state', label: 'State', value: stateF, options: ['running', 'exited', 'restarting'] }]} onFacet={(_, v) => setStateF(v)}
              actions={<><SegmentedControl legend="Group by" value={view} onChange={setView} options={[{ id: 'stacks', label: 'Compose' }, { id: 'all', label: 'Flat' }]} /><Button icon="plus" variant="secondary">Deploy stack…</Button></>} />
            {view === 'all' ? <DataTable caption="Containers" captionHidden columns={cols} rows={all} rowKey={(c) => c.name} selectable rowTone={(c) => c.state === 'exited' ? 'crit' : null}
              bulkActions={() => <><Button size="sm" variant="secondary" icon="restart">Restart…</Button><Button size="sm" variant="secondary" icon="stop">Stop…</Button></>} /> :
              stacks.map((s) => {
                const cs = s.containers.filter(match); if (!cs.length) return null;
                const up = s.containers.filter((c) => c.state === 'running').length;
                return (
                  <section key={s.name} className="hc-stack-card" aria-label={'Compose project ' + s.name}>
                    <div className="hc-stack-head">
                      <button type="button" className="hc-disclose" aria-expanded={open[s.name]} onClick={() => setOpen(Object.assign({}, open, { [s.name]: !open[s.name] }))}>
                        <Icon name="chevron-down" size="sm" /><Icon name="containers" size="sm" />{s.name}
                      </button>
                      <StatusPill tone={up === s.containers.length ? 'ok' : up === 0 ? 'crit' : 'warn'} label={`${up}/${s.containers.length} running`} />
                      <span className="hc-count hc-mono hc-hide-narrow">{s.path}</span><span className="hc-spacer" />
                      <Button size="sm" variant="ghost" icon="settings">Edit compose</Button>
                      <Button size="sm" variant="secondary" icon="download">Pull &amp; redeploy…</Button>
                    </div>
                    {open[s.name] && <DataTable caption={'Containers in ' + s.name} captionHidden columns={cols} rows={cs} rowKey={(c) => c.name} rowTone={(c) => c.state === 'exited' ? 'crit' : null} />}
                  </section>
                );
              })}
          </div>
        ) : t === 'images' ? <DataTable caption="Images" captionHidden rowKey={(i) => i.tag} rows={images} selectable bulkActions={() => <Button size="sm" variant="secondary" icon="close">Remove…</Button>}
            columns={[{ id: 'tag', header: 'Repository:tag', cell: (i) => <span className="hc-mono">{i.tag}</span> }, { id: 'id', header: 'ID', cell: (i) => <span className="hc-mono hc-muted">{i.id}</span> }, { id: 'size', header: 'Size', numeric: true }, { id: 'used', header: 'Used by', cell: (i) => i.used || <StatusPill label="Unused" /> }, { id: 'created', header: 'Created' }]} />
          : t === 'networks' ? <DataTable caption="Networks" captionHidden rowKey={(n) => n.name} rows={networks} columns={[{ id: 'name', header: 'Network', cell: (n) => <span className="hc-mono">{n.name}</span> }, { id: 'driver', header: 'Driver' }, { id: 'subnet', header: 'Subnet', cell: (n) => <span className="hc-mono">{n.subnet}</span> }, { id: 'containers', header: 'Containers', numeric: true }]} />
          : t === 'volumes' ? <DataTable caption="Volumes" captionHidden rowKey={(v) => v.name} rows={volumes} columns={[{ id: 'name', header: 'Volume', cell: (v) => <span className="hc-mono">{v.name}</span> }, { id: 'driver', header: 'Driver' }, { id: 'size', header: 'Size', numeric: true }, { id: 'used', header: 'Mounted by', cell: (v) => v.used || <StatusPill tone="warn" label="Dangling" /> }]} />
          : <KeyValueList items={engine || []} />}
      </Tabs>
    </div>
  );
}

/* ---------- Showcase: Docker Permissions Manager ---------- */
function SafePermissionsManager({ tree, directories: initial, savedLists = [], users = [], groups = [], history = [], onExecute }) {
  const [dirs, setDirs] = useState(initial);
  const [add, setAdd] = useState(''); const [list, setList] = useState('');
  const [uid, setUid] = useState('1000'); const [gid, setGid] = useState('1000');
  const [recursive, setRecursive] = useState(true); const [dirMode, setDirMode] = useState('0775'); const [fileMode, setFileMode] = useState('0664');
  const [setDir, setSetDir] = useState(true); const [setFile, setSetFile] = useState(false);
  const [stage, setStage] = useState('edit');
  const files = dirs.reduce((a, d) => a + (recursive ? d.files : 0), 0), subdirs = dirs.reduce((a, d) => a + (recursive ? d.dirs : 1), 0);
  const warn = dirs.filter((d) => d.flags && d.flags.length);
  const change = () => setStage('edit');
  return (
    <div className="hc-stack">
      <div className={'hc-modebar hc-modebar-' + (stage === 'preview' ? 'preview' : 'edit')} role="status">
        <Icon name={stage === 'preview' ? 'eye' : 'settings'} size="sm" />
        {stage === 'preview' ? <><strong>Dry-run preview</strong> · nothing on disk has changed</> : <><strong>Editing selection</strong> · nothing runs until you preview and confirm</>}
      </div>
      <div className="hc-detail-grid">
        <Section title="1 · Directories" level={3} actions={savedLists.length > 0 && <Select aria-label="Saved directory lists" value={list}
          onChange={(e) => { setList(e.target.value); const l = savedLists.find((x) => x.name === e.target.value); if (l) { setDirs(l.directories); change(); } }}
          options={[{ value: '', label: 'Saved lists…' }, ...savedLists.map((l) => ({ value: l.name, label: l.name }))]} />}>
          <div className="hc-row" style={{ alignItems: 'end', flexWrap: 'nowrap' }}>
            <div style={{ flex: 1 }}><PathPicker label="Add directory" value={add} onChange={setAdd} tree={tree} /></div>
            <Button variant="secondary" icon="plus" disabledReason={!add ? undefined : isProtected(add) ? 'Protected path' : undefined}
              onClick={() => { if (add && !isProtected(add) && !dirs.some((d) => d.path === add)) { setDirs([...dirs, { path: add, owner: '?', mode: '?', files: 0, dirs: 1, usedBy: '—' }]); setAdd(''); change(); } }}>Add</Button>
          </div>
          <ul className="hc-dirlist" aria-label="Selected directories">
            {dirs.map((d) => (
              <li key={d.path} className="hc-dir2">
                <Icon name="folder" size="sm" />
                <span className="hc-dir2-main"><span className="hc-mono">{d.path}</span>
                  <span className="hc-help">bind-mounted by {d.usedBy} · now <span className="hc-mono">{d.owner} {d.mode}</span> · {d.files.toLocaleString()} files</span>
                  {d.flags && d.flags.map((f) => <span key={f} className="hc-flag"><Icon name="warning" size="sm" />{f}</span>)}</span>
                <IconButton icon="close" label={'Remove ' + d.path} onClick={() => { setDirs(dirs.filter((x) => x.path !== d.path)); change(); }} />
              </li>
            ))}
          </ul>
          <Button size="sm" variant="ghost" icon="plus">Save as list…</Button>
        </Section>
        <Section title="2 · Owner and modes" level={3}>
          <div className="hc-stack">
            <div className="hc-ids">
              <UserGroupPicker kind="user" label="Owner" entries={users} value={uid} onChange={(v) => { setUid(v); change(); }} />
              <UserGroupPicker kind="group" label="Group" entries={groups} value={gid} onChange={(v) => { setGid(v); change(); }} />
            </div>
            <Alert tone="info">Many images (for example linuxserver.io) honour PUID/PGID; others run as a fixed user or root. Check the image's documentation before choosing an owner.</Alert>
            <Switch label="Apply recursively" checked={recursive} onChange={(v) => { setRecursive(v); change(); }} />
            <div className="hc-modes">
              <div className="hc-stack"><Switch label="Set directory mode" checked={setDir} onChange={(v) => { setSetDir(v); change(); }} />{setDir && <PermissionEditor label="Directories" value={dirMode} onChange={(v) => { setDirMode(v); change(); }} />}</div>
              <div className="hc-stack"><Switch label="Set file mode" checked={setFile} onChange={(v) => { setSetFile(v); change(); }} />{setFile && <PermissionEditor label="Files" value={fileMode} onChange={(v) => { setFileMode(v); change(); }} />}</div>
            </div>
            {uid === '0' && <RiskCallout riskClass="disruptive" title="UID 0 is root" items={['Containers writing as root can modify every file under these paths.']} />}
            <Button icon="eye" disabledReason={dirs.length ? undefined : 'Add at least one directory'} onClick={() => setStage('preview')}>Preview changes</Button>
          </div>
        </Section>
      </div>
      {stage === 'preview' && (
        <Section title="3 · Dry-run result" level={3}>
          <OperationPreview title={`chown ${uid}:${gid}${setDir ? ' · dirs ' + dirMode : ''}${setFile ? ' · files ' + fileMode : ''}`} riskClass="reversible"
            target={dirs.map((d) => ({ term: d.usedBy, value: d.path, mono: true }))}
            counts={[{ label: 'directories', value: subdirs.toLocaleString() }, { label: 'files', value: files.toLocaleString() }, { label: 'symlinks skipped (not followed)', value: 37 }, { label: 'other filesystems skipped', value: 1 }]}
            changes={[...dirs.map((d) => ({ op: 'change', label: d.path + (recursive ? ' (recursive)' : ''), diff: ['- ' + d.owner + '  ' + d.mode, '+ ' + uid + ':' + gid + '  ' + (setDir ? dirMode : d.mode)] })),
              { op: 'skip', label: '/srv/appdata/jellyfin/cache/transcodes → /mnt/scratch (mount boundary, not crossed)' },
              { op: 'add', label: 'Ownership backup → /var/lib/homelab-control/backups/perm-2026-10-21T05-58.json' }]}
            dependencies={warn.length ? warn.map((d) => d.path + ': ' + d.flags.join(', ')) : undefined}
            consequences={['Containers running as a different UID lose write access to these paths.', 'Ownership and modes can be restored from the backup; file contents are untouched.', 'POSIX ACLs on 2 directories are kept but may now disagree with the new owner.']}
            onCancel={change} onApply={onExecute} applyLabel="Execute…" />
        </Section>
      )}
      <Section title="Audit history" level={3}><AuditTimeline events={history} /></Section>
    </div>
  );
}

/* ---------- Showcase: Physical Drive Layout Designer ---------- */
const TYPE_ICON = { hdd: 'drive', ssd: 'hardware', nvme: 'system' };
function DriveBayGrid({ layouts: initialLayouts, drives, assignments: initialAssign, onLocate }) {
  const [layouts, setLayouts] = useState(initialLayouts);
  const [lid, setLid] = useState(initialLayouts[0].id);
  const [asg, setAsg] = useState(initialAssign);
  const [sel, setSel] = useState(null); const [held, setHeld] = useState(null);
  const [view, setView] = useState('grid'); const [edit, setEdit] = useState(false); const [zoom, setZoom] = useState(100);
  const [io, setIo] = useState(null); const [imp, setImp] = useState(''); const [impErr, setImpErr] = useState(null);
  const [msg, setMsg] = useState('');
  const L = layouts.find((l) => l.id === lid);
  const byId = Object.fromEntries(drives.map((d) => [d.id, d]));
  const assigned = new Set(Object.values(asg));
  const unassigned = drives.filter((d) => !assigned.has(d.id) && d.status !== 'missing');
  const slot = L.slots.find((s) => s.id === sel);
  const assign = (slotId, driveId) => {
    const n = Object.assign({}, asg); Object.keys(n).forEach((k) => n[k] === driveId && delete n[k]);
    if (driveId) n[slotId] = driveId; else delete n[slotId];
    setAsg(n); setHeld(null); setMsg(driveId ? `${byId[driveId].model} (${byId[driveId].serial}) assigned to ${L.slots.find((s) => s.id === slotId).label}.` : 'Bay cleared.');
  };
  const updSlot = (patch) => setLayouts(layouts.map((l) => l.id !== lid ? l : Object.assign({}, l, { slots: l.slots.map((s) => s.id === sel ? Object.assign({}, s, patch) : s) })));
  const setDim = (k, v) => setLayouts(layouts.map((l) => l.id !== lid ? l : Object.assign({}, l, { [k]: v })));
  const activate = (s) => { if (held && !s.reserved) assign(s.id, held); else setSel(sel === s.id ? null : s.id); };
  const health = (d) => d.status === 'missing' ? ['crit', 'Missing'] : d.status === 'replaced' ? ['warn', 'Replaced'] : [d.tone, d.health];
  const exportJson = JSON.stringify({ hcisDriveLayout: 1, name: L.name, chassis: L.chassis, rows: L.rows, cols: L.cols, slots: L.slots, assignments: Object.fromEntries(Object.entries(asg).filter(([k]) => L.slots.some((s) => s.id === k))) }, null, 2);
  const doImport = () => {
    try {
      const j = JSON.parse(imp); const errs = [];
      if (j.hcisDriveLayout !== 1) errs.push('hcisDriveLayout must be 1');
      if (!j.name) errs.push('name is required');
      if (!Array.isArray(j.slots)) errs.push('slots must be a list');
      else j.slots.forEach((s, i) => { if (!s.id || !s.label) errs.push(`slots[${i}] needs id and label`); });
      if (errs.length) return setImpErr(errs);
      const nl = { id: 'imp-' + Date.now(), name: j.name + ' (imported)', chassis: j.chassis || '', rows: j.rows || 1, cols: j.cols || 4, slots: j.slots };
      setLayouts([...layouts, nl]); setLid(nl.id); setIo(null); setImp(''); setImpErr(null); setMsg('Imported layout ' + nl.name + '. Drive assignments were not imported; assign by serial/WWN.');
    } catch (e) { setImpErr(['Not valid JSON: ' + e.message]); }
  };
  const Bay = ({ s }) => {
    const d = asg[s.id] && byId[asg[s.id]];
    const [t, w] = d ? health(d) : [null, null];
    return (
      <button type="button" className={cx('hc-bay', s.reserved && 'hc-bay-reserved', !d && !s.reserved && 'hc-bay-empty', t === 'crit' && 'hc-bay-crit', t === 'warn' && 'hc-bay-warn', held && !s.reserved && 'hc-bay-target')}
        style={{ gridRow: `${s.row} / span ${s.rowSpan || 1}`, gridColumn: `${s.col} / span ${s.colSpan || 1}` }} aria-pressed={sel === s.id}
        aria-label={`${s.label}${s.reserved ? ', reserved' : d ? `: ${d.model}, ${d.size}, ${w}, location ${d.confidence}` : ', empty'}${held && !s.reserved ? '. Press to assign the held drive here' : ''}`}
        onClick={() => activate(s)} onDragOver={(e) => { if (!s.reserved) e.preventDefault(); }} onDrop={(e) => { e.preventDefault(); const id = e.dataTransfer.getData('text/plain'); id && assign(s.id, id); }}
        draggable={!!d && !edit} onDragStart={(e) => d && e.dataTransfer.setData('text/plain', d.id)}>
        <span className="hc-bay-num">{s.label}<Icon name={TYPE_ICON[s.type || 'hdd']} size="sm" /></span>
        <span>{s.reserved ? <span className="hc-bay-sub">Reserved</span> : d ? <><span className="hc-bay-name">{d.size} · {d.pool || 'unassigned'}</span><span className="hc-bay-sub">{d.status === 'missing' ? 'Missing · ' + d.serial : d.model}</span></> : <span className="hc-bay-sub">Empty</span>}</span>
        <span className={cx('hc-bay-led', d && (d.locating ? 'hc-locate' : 'hc-' + t))} aria-hidden="true" />
        {d && <span className="hc-bay-temp">{d.temp}</span>}
      </button>
    );
  };
  return (
    <div className="hc-stack hc-bays-wrap">
      <div className="hc-toolbar" style={{ margin: 0 }}>
        <Select aria-label="Saved layout" value={lid} onChange={(e) => { setLid(e.target.value); setSel(null); }} options={layouts.map((l) => ({ value: l.id, label: l.name }))} />
        <SegmentedControl legend="View" value={view} onChange={setView} options={[{ id: 'grid', label: 'Bays', icon: 'overview' }, { id: 'list', label: 'List', icon: 'logs' }]} />
        {view === 'grid' && <label className="hc-row hc-count">Zoom<input type="range" min="60" max="100" step="10" value={zoom} onChange={(e) => setZoom(Number(e.target.value))} aria-label="Zoom" /></label>}
        <span className="hc-spacer" />
        <Switch label="Edit layout" checked={edit} onChange={setEdit} />
        <Dropdown label="Layout" items={[{ label: 'Export JSON…', icon: 'download', onSelect: () => setIo('export') }, { label: 'Import JSON…', icon: 'plus', onSelect: () => setIo('import') }, { label: 'New layout…', icon: 'plus' }]} align="end" />
      </div>
      <p className="hc-vh" role="status" aria-live="polite">{msg}</p>
      {edit && (
        <div className="hc-edit-strip">
          <NumericInput label="Rows" value={L.rows} min={1} max={12} onChange={(v) => setDim('rows', v)} />
          <NumericInput label="Columns" value={L.cols} min={1} max={24} onChange={(v) => setDim('cols', v)} />
          <span className="hc-help">Select a bay to rename it, change its type, span or reserve it. Irregular chassis: give bays a row/column span.</span>
        </div>
      )}
      <div className="hc-bays-layout">
        <div className="hc-stack" style={{ minWidth: 0 }}>
          {view === 'grid' ? (
            <div className="hc-bays-scroll" tabIndex={0} aria-label={`${L.name} chassis, scrollable`}>
              <div className="hc-bays-chassis" role="group" aria-label={L.name + ' — ' + L.chassis}
                style={{ gridTemplateColumns: `repeat(${L.cols}, minmax(${Math.round(132 * zoom / 100)}px, 1fr))`, gridTemplateRows: `repeat(${L.rows}, auto)`, fontSize: zoom < 80 ? '0.85em' : undefined }}>
                {L.slots.map((s) => <Bay key={s.id} s={s} />)}
              </div>
            </div>
          ) : (
            <DataTable caption={L.name + ' bays'} captionHidden rowKey={(s) => s.id} rows={L.slots} onRowClick={(s) => setSel(s.id)} activeKey={sel}
              columns={[{ id: 'label', header: 'Bay' }, { id: 'type', header: 'Type', cell: (s) => (s.type || 'hdd').toUpperCase() },
                { id: 'drive', header: 'Drive', cell: (s) => { const d = byId[asg[s.id]]; return s.reserved ? 'Reserved' : d ? <span className="hc-mono">{d.model} · {d.serial}</span> : 'Empty'; } },
                { id: 'health', header: 'Health', cell: (s) => { const d = byId[asg[s.id]]; if (!d) return '—'; const [t, w] = health(d); return <StatusPill tone={t} label={w} />; } },
                { id: 'loc', header: 'Location', cell: (s) => { const d = byId[asg[s.id]]; return d ? <Confidence level={d.confidence} /> : '—'; } }]} />
          )}
          <div className="hc-legend"><span className="hc-row"><span className="hc-bay-led hc-ok" />Healthy</span><span className="hc-row"><span className="hc-bay-led hc-warn" />Warning / replaced</span><span className="hc-row"><span className="hc-bay-led hc-crit" />Failing / missing</span><span className="hc-row"><span className="hc-bay-led hc-locate" />Locating</span></div>
          <Section title={`Unassigned drives (${unassigned.length})`} level={3}>
            <p className="hc-help" style={{ marginTop: 0 }}>Drag a drive onto a bay, or press <strong>Hold</strong> and then press a bay. Identity is the WWN/serial, never <span className="hc-mono">/dev/sdX</span>.</p>
            <ul className="hc-tray">{unassigned.map((d) => (
              <li key={d.id} draggable onDragStart={(e) => e.dataTransfer.setData('text/plain', d.id)} className={cx('hc-traydrive', held === d.id && 'on')}>
                <Icon name={TYPE_ICON[d.type]} size="sm" /><span><strong>{d.size} {d.type.toUpperCase()}</strong> <span className="hc-mono hc-muted">{d.model} · {d.serial}</span><span className="hc-help" style={{ display: 'block' }}>{d.hba} port {d.port}</span></span>
                <Button size="sm" variant={held === d.id ? 'primary' : 'secondary'} aria-pressed={held === d.id} onClick={() => setHeld(held === d.id ? null : d.id)}>{held === d.id ? 'Holding' : 'Hold'}</Button>
              </li>))}
              {unassigned.length === 0 && <li className="hc-help">Every detected drive has a bay.</li>}</ul>
          </Section>
        </div>
        {slot ? (
          <DetailPanel kind={L.name} title={slot.label} onClose={() => setSel(null)}>
            {edit && <div className="hc-stack">
              <TextField label="Bay label" value={slot.label} onChange={(e) => updSlot({ label: e.target.value })} />
              <Select label="Bay type" value={slot.type || 'hdd'} onChange={(e) => updSlot({ type: e.target.value })} options={[{ value: 'hdd', label: '3.5" HDD' }, { value: 'ssd', label: '2.5" SSD' }, { value: 'nvme', label: 'NVMe / M.2' }]} />
              <div className="hc-ids"><NumericInput label="Row span" value={slot.rowSpan || 1} min={1} max={4} onChange={(v) => updSlot({ rowSpan: v })} /><NumericInput label="Column span" value={slot.colSpan || 1} min={1} max={4} onChange={(v) => updSlot({ colSpan: v })} /></div>
              <Switch label="Reserved (no drive)" checked={!!slot.reserved} onChange={(v) => updSlot({ reserved: v })} />
            </div>}
            {(() => { const d = byId[asg[slot.id]]; if (!d) return slot.reserved ? <p className="hc-help">Reserved position.</p> : <Combobox label="Assign drive" value="" onChange={(v) => assign(slot.id, v)} options={unassigned.map((x) => ({ value: x.id, label: x.serial, detail: `${x.size} ${x.model}` }))} placeholder="Serial or model" mono />;
              const [t, w] = health(d);
              return <div className="hc-stack">
                {d.status === 'missing' && <Alert tone="crit" title="Drive missing.">Assigned drive {d.serial} was not detected in the last scan. Check the bay and cabling before replacing.</Alert>}
                {d.status === 'replaced' && <Alert tone="warn" title="Drive replaced?">A different serial ({d.serial}) now reports on the port this bay used ({d.prevSerial}). Confirm the new assignment.</Alert>}
                <KeyValueList layout="rows" items={[{ term: 'Health', value: <StatusPill tone={t} label={w} /> }, { term: 'Location', value: <Confidence level={d.confidence} /> }, { term: 'Model', value: d.model },
                  { term: 'Serial', value: d.serial, mono: true }, { term: d.type === 'nvme' ? 'NVMe EUI' : 'WWN', value: d.wwn, mono: true }, { term: 'Controller', value: `${d.hba} · port ${d.port}` },
                  { term: 'Pool', value: d.pool || '—' }, { term: 'Temperature', value: d.temp }, { term: 'Current node', value: <span className="hc-muted">{d.dev} (changes on reboot)</span> }]} />
                <div className="hc-row"><Button size="sm" variant="secondary" onClick={() => assign(slot.id, null)}>Unassign</Button>
                  <Button size="sm" variant="secondary" icon="locate" disabledReason="Bay LED control needs enclosure support and is not authorized yet (M2+)">Blink LED…</Button></div>
              </div>; })()}
          </DetailPanel>
        ) : <DetailPanel kind="Drive locator" title="Select a bay"><p className="hc-help">Select a bay to inspect its drive, or hold an unassigned drive and press a bay to place it. Locations established by enclosure data are Verified; ones set by hand or by port order are Inferred.</p></DetailPanel>}
      </div>
      <Dialog open={io === 'export'} onClose={() => setIo(null)} title={'Export ' + L.name} footer={<Button variant="secondary" onClick={() => setIo(null)}>Close</Button>}>
        <div className="hc-dialog-body"><p className="hc-help">Layout and bay assignments by serial/WWN. Import it on another host or keep it as a backup.</p><textarea className="hc-input hc-input-mono hc-textarea" readOnly value={exportJson} aria-label="Layout JSON" rows={12} /></div>
      </Dialog>
      <Dialog open={io === 'import'} onClose={() => { setIo(null); setImpErr(null); }} title="Import layout" footer={<><Button variant="secondary" onClick={() => setIo(null)}>Cancel</Button><Button onClick={doImport} disabledReason={imp.trim() ? undefined : 'Paste a layout first'}>Validate and import</Button></>}>
        <div className="hc-dialog-body"><textarea className="hc-input hc-input-mono hc-textarea" value={imp} onChange={(e) => setImp(e.target.value)} aria-label="Layout JSON to import" rows={10} placeholder='{"hcisDriveLayout": 1, "name": "…", "slots": […]}' />
          {impErr && <Alert tone="crit" title="Can't import:">{impErr.join('; ')}</Alert>}</div>
      </Dialog>
    </div>
  );
}

/* ---------- Showcase: Storage overview ---------- */
function StorageOverview({ pools, disks, relations }) {
  const [sel, setSel] = useState(relations.defaultSelected);
  const total = pools.reduce((a, p) => a + p.size, 0);
  return (
    <div className="hc-stack">
      <div className="hc-pagehead"><div><h1 className="hc-title-1">Storage</h1><p className="hc-help" style={{ margin: 0 }}>Inspection is read-only. Actions that change storage are marked with their risk class.</p></div><span className="hc-spacer" />
        <span className="hc-row"><RiskBadge riskClass="read-only" /><RiskBadge riskClass="reversible" /><RiskBadge riskClass="disruptive" /><RiskBadge riskClass="destructive" /></span></div>
      <Section title="Capacity" glass><CapacityBar label="All pools" total={Math.round(total)} segments={pools.map((p, i) => ({ label: p.name, value: p.used, series: i + 1 }))} /></Section>
      <StoragePools pools={pools} />
      <div className="hc-stack">
        <Section title="Disk health" meta={{ status: 'partial', sources: ['smartctl -a', 'nvme smart-log'], notes: ['1 USB bridge does not pass SMART data.'] }}>
          <DataTable caption="Disks" captionHidden rowKey={(d) => d.serial} rows={disks} rowTone={(d) => d.tone === 'crit' ? 'crit' : null} columns={[
            { id: 'bay', header: 'Bay' }, { id: 'model', header: 'Model' }, { id: 'serial', header: 'Serial', cell: (d) => <span className="hc-mono">{d.serial}</span> },
            { id: 'health', header: 'SMART', cell: (d) => <StatusPill tone={d.tone} label={d.health} /> }, { id: 'temp', header: 'Temp', numeric: true }, { id: 'pool', header: 'Pool' }]} />
        </Section>
        <Section title="Relationships" level={2}>
          <DependencyGraph levels={relations.levels} links={relations.links} selected={sel} onSelect={(n) => setSel(n.id)} />
          <p className="hc-help">Selected: <strong>{sel}</strong>. Modules stay independent; this view only reads their relationships.</p>
        </Section>
      </div>
    </div>
  );
}

/* ---------- Showcase: Resource Explorer ---------- */
function ResourceExplorer({ tree, resources, levels, links }) {
  const [sel, setSel] = useState(Object.keys(resources)[0]);
  const r = resources[sel];
  const flat = []; const walk = (ns) => ns.forEach((n) => { flat.push(n); n.children && walk(n.children); }); walk(tree);
  return (
    <div className="hc-explorer">
      <nav className="hc-explorer-tree" aria-label="All resources"><ResourceTree nodes={tree} selected={sel} onSelect={(n) => resources[n.id] && setSel(n.id)} label="Resources by type" /></nav>
      <div className="hc-explorer-main">
        <Breadcrumbs items={(r.path || [r.label]).map((l) => ({ label: l }))} />
        <DependencyGraph levels={levels} links={links} selected={sel} onSelect={(n) => resources[n.id] && setSel(n.id)} label={'Relationships of ' + r.label} />
      </div>
      <DetailPanel kind={r.kind} title={r.label} floating actions={<Button size="sm" variant="secondary" icon="external">Open</Button>}>
        <KeyValueList layout="rows" items={r.props} />
        {r.related && <><h4 className="hc-subhead">Related</h4><ul className="hc-related">{r.related.map((x) => <li key={x.id}><button type="button" className="hc-linkbtn" onClick={() => resources[x.id] && setSel(x.id)}><Icon name={x.icon} size="sm" />{x.label}</button><span className="hc-count">{x.relation}</span></li>)}</ul></>}
      </DetailPanel>
    </div>
  );
}

/* ---------- Showcase: Theme Manager ---------- */
const THEME_TIP = 'All custom themes must follow HCIS requirements for responsive layouts, accessibility, readable contrast, component consistency, and security. Themes may customize appearance but cannot alter functionality, permissions, safety controls, or application layout.';
const THEME_REQS = ['Preserve responsive behavior.', 'Meet applicable HCIS accessibility requirements.', 'Maintain readable contrast.', 'Preserve component usability.', 'Support keyboard navigation and focus visibility.', 'Respect reduced-motion settings.', 'Provide usable fallback surfaces if transparency or blur is unavailable.', 'Preserve safety warnings and destructive-operation visibility.', 'Remain compatible with density and layout preferences.', 'Avoid changing application behavior or authorization.', 'Use validated .hltheme JSON values.', 'Avoid arbitrary JavaScript or unrestricted CSS injection.'];
const FORBIDDEN_KEYS = ['layout', 'navigation', 'density', 'dashboard', 'script', 'css', 'javascript', 'permissions', 'rbac', '__proto__', 'constructor', 'prototype'];
function lum(h) { const c = [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255).map((x) => (x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4)); return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]; }
function contrast(a, b) { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); }
function validateHltheme(text) {
  const errors = [], warnings = [];
  let j; try { j = JSON.parse(text); } catch (e) { return { errors: ['Not valid JSON: ' + e.message], warnings }; }
  if (j.schemaVersion !== 1) errors.push('schemaVersion: must be 1');
  if (!j.id || !/^[a-z0-9.-]+$/.test(j.id)) errors.push('id: required, reverse-DNS style (a-z, 0-9, ., -)');
  if (!j.name || String(j.name).length > 80) errors.push('name: required, at most 80 characters');
  if (!j.modes || typeof j.modes !== 'object') errors.push('modes: required (dark and/or light)');
  const scan = (o, path) => { if (o && typeof o === 'object') Object.keys(o).forEach((k) => {
    if (FORBIDDEN_KEYS.includes(k.toLowerCase())) errors.push(`${path}${k}: not allowed — themes control appearance only (ADR 0010)`);
    const v = o[k]; if (typeof v === 'string' && /color/.test(path + k) && !/^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$/.test(v)) errors.push(`${path}${k}: "${v}" is not #RRGGBB or #RRGGBBAA`);
    if (typeof v === 'string' && /(url\(|var\(|expression|<|javascript:)/i.test(v)) errors.push(`${path}${k}: functions, URLs and markup are not allowed`);
    scan(v, path + k + '.'); }); };
  scan(j, '');
  Object.entries(j.modes || {}).forEach(([m, set]) => {
    const c = set && set.color; if (!c || !c.surface || !c.text) { errors.push(`modes.${m}.color: surface and text are required`); return; }
    const pairs = [['text.primary', 'surface.base', 4.5], ['text.secondary', 'surface.base', 4.5], ['text.primary', 'surface.raised', 4.5], ['focus.ring', 'surface.base', 3]];
    pairs.forEach(([f, b, min]) => { const fv = f.split('.').reduce((o, k) => o && o[k], c), bv = b.split('.').reduce((o, k) => o && o[k], c);
      if (/^#[0-9a-f]{6}/i.test(fv || '') && /^#[0-9a-f]{6}/i.test(bv || '')) { const r = contrast(fv.slice(0, 7), bv.slice(0, 7)); if (r < min) errors.push(`modes.${m}.color.${f} on ${b}: ${r.toFixed(2)}:1, needs ${min}:1`); }
      else warnings.push(`modes.${m}.color.${f} or ${b} missing: inherits the built-in value`); });
  });
  return { errors, warnings, theme: j };
}
const SAMPLE_BAD = '{\n  "schemaVersion": 1,\n  "id": "com.example.neon",\n  "name": "Neon Nights",\n  "layout": { "sidebar": "right" },\n  "modes": { "dark": { "color": {\n    "surface": { "base": "#05010a", "raised": "#120a1f" },\n    "text": { "primary": "#ff2bd6", "secondary": "#5b2a6e" },\n    "focus": { "ring": "#1a0f2b" },\n    "accent": { "base": "rgb(0,255,200)" }\n  } } }\n}';
function ThemeManager({ installed, current: initial = 'midnight', onApply }) {
  const [current, setCurrent] = useState(initial); const [preview, setPreview] = useState(null); const [left, setLeft] = useState(0);
  const [reqs, setReqs] = useState(false); const [imp, setImp] = useState(false); const [text, setText] = useState(SAMPLE_BAD); const [result, setResult] = useState(null);
  const [inspect, setInspect] = useState(null);
  const apply = (id) => document.documentElement.setAttribute('data-theme', id);
  useEffect(() => {
    if (!preview) return; setLeft(15); apply(preview);
    const t = setInterval(() => setLeft((s) => { if (s <= 1) { clearInterval(t); setPreview(null); apply(current); return 0; } return s - 1; }), 1000);
    const esc = (e) => { if (e.key === 'Escape') { setPreview(null); apply(current); } };
    window.addEventListener('keydown', esc);
    return () => { clearInterval(t); window.removeEventListener('keydown', esc); };
  }, [preview]);
  const keep = () => { setCurrent(preview); setPreview(null); onApply && onApply(preview); };
  const revert = () => { setPreview(null); apply(current); };
  const T = installed.find((t) => t.id === inspect);
  return (
    <div className="hc-stack">
      <div className="hc-pagehead"><div><Breadcrumbs items={[{ label: 'Settings' }, { label: 'Appearance' }, { label: 'Themes' }]} /><h1 className="hc-title-1">Themes</h1></div><span className="hc-spacer" />
        <span className="hc-row"><Button variant="secondary" icon="plus" onClick={() => { setImp(true); setResult(null); }}>Import .hltheme…</Button><Tooltip text={THEME_TIP} label="Custom theme requirements" />
          <Button variant="ghost" onClick={() => setReqs(true)}>View theme requirements</Button></span></div>
      {preview && (
        <div className="hc-previewbar" role="alertdialog" aria-label="Theme preview" aria-describedby="hc-pv-msg">
          <Icon name="eye" /><span id="hc-pv-msg"><strong>Previewing {installed.find((t) => t.id === preview).name}.</strong> Reverting to {installed.find((t) => t.id === current).name} in {left} s unless you keep it. Esc reverts now.</span>
          <span className="hc-spacer" /><Button variant="secondary" onClick={revert}>Revert</Button><Button onClick={keep}>Keep theme</Button>
        </div>
      )}
      <div className="hc-themegrid">
        {installed.map((t) => (
          <article key={t.id} className={cx('hc-themecard', current === t.id && 'on')} aria-label={t.name}>
            <div className="hc-themeswatch" data-theme={t.builtin ? t.id : undefined} aria-hidden="true" style={!t.builtin && t.swatch ? Object.fromEntries(Object.entries(t.swatch).map(([k, v]) => ["--hc-color-" + k, v])) : undefined}>
              <span className="sw-base" /><span className="sw-raised"><i className="sw-text" /><i className="sw-text2" /><i className="sw-accent" /></span><span className="sw-glass" />
            </div>
            <div className="hc-row"><strong>{t.name}</strong>{t.builtin ? <StatusPill tone="accent" label="Built-in" icon={null} /> : <StatusPill label="Custom" icon={null} />}
              {current === t.id && <StatusPill tone="ok" label="Active" />}</div>
            <div className="hc-help">{t.description}</div>
            <div className="hc-row hc-count">v{t.version} · {t.author}{t.compat && <> · <StatusIndicator state={t.compat.ok ? 'ok' : 'crit'} label={t.compat.label} /></>}</div>
            <div className="hc-row">
              <Button size="sm" variant="secondary" disabledReason={!t.builtin ? 'Fix validation errors first' : current === t.id ? undefined : undefined} onClick={() => t.builtin && current !== t.id && setPreview(t.id)}>{current === t.id ? 'Active' : 'Preview'}</Button>
              <Button size="sm" variant="ghost" onClick={() => setInspect(t.id)}>Details</Button>
            </div>
          </article>
        ))}
      </div>
      {current !== 'midnight' && <Button variant="ghost" icon="restart" onClick={() => { setCurrent('midnight'); apply('midnight'); }}>Revert to built-in Midnight</Button>}
      <Dialog open={reqs} onClose={() => setReqs(false)} title="Theme requirements" footer={<Button onClick={() => setReqs(false)}>Close</Button>}>
        <div className="hc-dialog-body"><p className="hc-help">{THEME_TIP}</p><ol className="hc-steps-plain">{THEME_REQS.map((r) => <li key={r}>{r}</li>)}</ol>
          <Alert tone="info">Built-in themes meet the same requirements. Automated validation catches many problems but is not a guarantee of full accessibility compliance.</Alert></div>
      </Dialog>
      <Dialog open={imp} onClose={() => setImp(false)} title={<span className="hc-row">Import a theme <Tooltip text={THEME_TIP} label="Custom theme requirements" /></span>}
        footer={<><Button variant="secondary" onClick={() => setImp(false)}>Cancel</Button><Button icon="check" onClick={() => setResult(validateHltheme(text))}>Validate</Button>
          <Button disabledReason={result && result.errors.length === 0 ? undefined : 'Validate without errors first'}>Install</Button></>}>
        <div className="hc-dialog-body">
          <label className="hc-field"><span className="hc-label">.hltheme file</span><input type="file" accept=".hltheme,application/json" className="hc-input"
            onChange={(e) => { const f = e.target.files[0]; if (f) f.text().then((t) => { setText(t); setResult(null); }); }} /></label>
          <label className="hc-field"><span className="hc-label">Or paste JSON</span><textarea className="hc-input hc-input-mono hc-textarea" rows={10} value={text} onChange={(e) => { setText(e.target.value); setResult(null); }} spellCheck={false} /></label>
          {result && (result.errors.length ? <Alert tone="crit" title={`${result.errors.length} problem${result.errors.length > 1 ? 's' : ''}:`}><ul className="hc-steps-plain">{result.errors.map((e) => <li key={e}><code>{e}</code></li>)}</ul></Alert>
            : <Alert tone="ok" title="Valid.">Structure, value formats and key contrast pairs pass. Preview it before keeping it; automated checks are not a full accessibility audit.</Alert>)}
          {result && result.warnings.length > 0 && <Alert tone="warn" title="Notes:">{result.warnings.join(' · ')}</Alert>}
        </div>
      </Dialog>
      <Dialog open={!!T} onClose={() => setInspect(null)} title={T ? T.name : ''} footer={<Button onClick={() => setInspect(null)}>Close</Button>}>
        {T && <div className="hc-dialog-body"><KeyValueList layout="rows" items={[{ term: 'ID', value: T.id, mono: true }, { term: 'Version', value: T.version }, { term: 'Author', value: T.author }, { term: 'License', value: T.license }, { term: 'Engine', value: '>=1.0.0 <2.0.0', mono: true }, { term: 'Modes', value: T.modes }, { term: 'Compatibility', value: T.compat ? T.compat.label : '—' }]} />
          {T.issues && <Alert tone="crit" title="Validation errors:"><ul className="hc-steps-plain">{T.issues.map((i) => <li key={i}><code>{i}</code></li>)}</ul></Alert>}</div>}
      </Dialog>
    </div>
  );
}

/* ---------- Exports ---------- */
window.HCIS_MARK = window.HCIS_MARK || '/_blob/be29571775936884e4fb328964096a41';
window.HCIS = {
  Icon, Example, Section, Sources, StateDot, Meter, Button, IconButton, Dropdown, TextField, SearchField, Select, Checkbox, Switch, SegmentedControl,
  StatusPill, Badge, Banner, CheckList, Confidence, DataTable, KeyValueList, ResourceCard, Sparkline, MetricChart, ProgressBar,
  Breadcrumbs, Tabs, ResourceHeader, Sidebar, GlobalHeader, AppShell, Dialog, ConfirmDialog, RiskCallout, CommandPalette,
  useCommandPaletteShortcut, LogViewer, AuditTimeline, JobProgress, OperationPreview, ResourceInspector, ServiceDetail, HealthScore,
  HealthDashboard, ContainerManager, SafePermissionsManager, PermissionEditor, FileBrowser, DriveBayGrid, StoragePools, VMList,
  NetworkInterfaces, PackageList, ShareList,
  LiveIndicator, StatusIndicator, FilterBar, ResourceTree, DetailPanel, MetricTile, CapacityBar, TimeSeriesChart, HealthSummary, DependencyGraph,
  NumericInput, Combobox, RadioGroup, PathPicker, UserGroupPicker, Alert, ToastRegion, EmptyState, ErrorState, LoadingState, Tooltip,
  RiskBadge, BackgroundJobPanel, DiagnosticFinding, ResolutionGuide, TerminalViewer, NavigationRail, AppearanceMenu, WorkspaceSwitcher,
  OverviewDashboard, StorageOverview, ResourceExplorer, ThemeManager, validateHltheme,
  /* catalogue aliases (spec names → HCIS components) */
  TabBar: Tabs, PropertyList: KeyValueList, StatusDot: StateDot, ProgressIndicator: ProgressBar, ConfirmationDialog: ConfirmDialog,
  RiskConfirmation: ConfirmDialog, PermissionMatrix: PermissionEditor, FileModeEditor: PermissionEditor, DirectoryBrowser: FileBrowser,
  ServiceDetails: ServiceDetail, DockerPermissionsManager: SafePermissionsManager, DriveLayoutDesigner: DriveBayGrid, Toast: ToastRegion,
  RESOURCE_TABS, NAV, THEMES, RISK,
};
