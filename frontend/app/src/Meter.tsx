import { level, ratio } from './format';

const SEGMENTS = 24;

interface MeterProps {
  label: string;
  used: number | undefined;
  total: number | undefined;
  detail: string;
}

/**
 * Segmented capacity meter. Fill is expressed purely with classes (no inline
 * styles) so the page works under a strict `style-src 'self'` CSP. When the
 * ratio is unknown the meter renders empty and says so.
 */
export function Meter({ label, used, total, detail }: MeterProps) {
  const r = ratio(used, total);
  const filled = r === undefined ? 0 : Math.round(r * SEGMENTS);
  const lvl = r === undefined ? 'unknown' : level(r);
  const pct = r === undefined ? undefined : Math.round(r * 100);
  return (
    <div className="meter">
      <div className="meter-head">
        <span className="meter-label">{label}</span>
        <span className="meter-value">{pct === undefined ? 'Unknown' : `${pct}%`}</span>
      </div>
      <div
        className={`meter-track meter-${lvl}`}
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={pct ?? 0}
        aria-valuetext={pct === undefined ? 'Unknown' : `${pct}% — ${detail}`}
      >
        {Array.from({ length: SEGMENTS }, (_, i) => (
          <span key={i} className={i < filled ? 'seg seg-on' : 'seg'} />
        ))}
      </div>
      <div className="meter-detail">{detail}</div>
    </div>
  );
}
