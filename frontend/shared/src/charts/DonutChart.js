import { useState } from 'react';
import { useT } from '../i18n';
import { arcPath, colorFor } from './palette';
import './charts.css';

// DonutChart renders a proportional ring from [{ key, count }] data plus a
// legend. The hovered (or, at rest, the total) value shows in the hole. `kind`
// ('category' | 'severity' | 'plain') picks the semantic color mapping; `label`
// maps a raw key to a display string.
export function DonutChart({ data, kind = 'category', label, emptyText }) {
  const t = useT();
  const [hover, setHover] = useState(-1);
  const items = (data || []).filter((d) => d.count > 0);
  const total = items.reduce((sum, d) => sum + d.count, 0);
  const size = 180;
  const cx = size / 2;
  const cy = size / 2;
  const outerR = 82;
  const innerR = 52;

  if (total === 0) {
    return <div className="chart-empty">{emptyText != null ? emptyText : t('common.noData')}</div>;
  }

  let angle = 0;
  const segments = items.map((d, i) => {
    const start = angle;
    const sweep = (d.count / total) * Math.PI * 2;
    angle += sweep;
    return {
      ...d,
      i,
      color: colorFor(d.key, i, kind),
      // A hair of separation between segments reads cleaner; skip when a single
      // segment would otherwise leave a gap.
      path: arcPath(cx, cy, innerR, outerR, start + (items.length > 1 ? 0.02 : 0), angle),
    };
  });

  const focus = hover >= 0 ? segments[hover] : null;
  const centerValue = focus ? focus.count : total;
  const centerLabel = focus ? (label ? label(focus.key) : focus.key) : t('common.total');
  const centerPct = focus ? Math.round((focus.count / total) * 100) : 100;

  return (
    <div className="donut-wrap">
      <svg viewBox={`0 0 ${size} ${size}`} className="donut-svg" role="img">
        {segments.map((s) => (
          <path
            key={s.key}
            d={s.path}
            fill={s.color}
            opacity={hover === -1 || hover === s.i ? 1 : 0.35}
            onMouseEnter={() => setHover(s.i)}
            onMouseLeave={() => setHover(-1)}
          >
            <title>{`${label ? label(s.key) : s.key}: ${s.count}`}</title>
          </path>
        ))}
        <text x={cx} y={cy - 6} className="donut-center-value" textAnchor="middle">{centerValue}</text>
        <text x={cx} y={cy + 12} className="donut-center-label" textAnchor="middle">{centerLabel}</text>
        <text x={cx} y={cy + 28} className="donut-center-pct" textAnchor="middle">{centerPct}%</text>
      </svg>
      <ul className="chart-legend">
        {segments.map((s) => (
          <li
            key={s.key}
            className={hover === s.i ? 'is-hover' : ''}
            onMouseEnter={() => setHover(s.i)}
            onMouseLeave={() => setHover(-1)}
          >
            <span className="legend-swatch" style={{ background: s.color }} />
            <span className="legend-key">{label ? label(s.key) : s.key}</span>
            <span className="legend-count">{s.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
