import { Fragment } from 'react';
import { useT } from '../i18n';
import './charts.css';

// Default sequential hue (dataviz reference blue, step 450). Intensity is encoded
// as alpha over the themed chart surface, so the lightest cells recede toward the
// surface (near-zero) and the busiest reach the full hue — a one-hue, light→dark
// sequential ramp that adapts to both light and dark themes.
const DEFAULT_ACCENT = '#2a78d6';

// hexToRgba turns a #rrggbb hue into an rgba() string at the given alpha.
function hexToRgba(hex, alpha) {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

const HOURS = Array.from({ length: 24 }, (_, h) => h);
const DAYS = [0, 1, 2, 3, 4, 5, 6];

// Heatmap renders an activity-rhythm grid: 7 day-of-week rows × 24 hour-of-day
// columns, each cell shaded by its event count. `cells` is a sparse list of
// { day, hour, count } (day 0=Sunday, local); `max` scales the color ramp;
// `dayLabels` are the 7 localized short weekday names (index 0=Sunday);
// `cellTitle(day, hour, count)` builds a cell's hover/aria text.
export function Heatmap({ cells, max, dayLabels, cellTitle, hourTickEvery = 6, emptyText, accent = DEFAULT_ACCENT }) {
  const t = useT();
  const data = cells || [];
  const peak = max || 0;

  if (data.length === 0 || peak <= 0) {
    return <div className="chart-empty">{emptyText != null ? emptyText : t('common.noData')}</div>;
  }

  const days = dayLabels && dayLabels.length === 7 ? dayLabels : ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const titleFor = cellTitle || ((d, h, c) => `${days[d]} ${String(h).padStart(2, '0')}:00 · ${c}`);

  // Sparse count lookup keyed by day*24 + hour.
  const counts = new Map();
  data.forEach((c) => counts.set(c.day * 24 + c.hour, c.count));

  // Perceptual ramp: sqrt spreads the low end so light activity stays visible,
  // with a floor so any non-zero cell reads above the empty track.
  const fill = (count) => {
    if (count <= 0) return null;
    const alpha = 0.12 + 0.88 * Math.sqrt(count / peak);
    return hexToRgba(accent, alpha);
  };

  return (
    <div className="hm-wrap">
      <div className="hm-grid" role="img" aria-label={t('common.total')}>
        <div className="hm-corner" />
        {HOURS.map((h) => (
          <div key={`tick-${h}`} className="hm-col-tick">
            {h % hourTickEvery === 0 ? String(h).padStart(2, '0') : ''}
          </div>
        ))}
        {DAYS.map((d) => (
          <Fragment key={`row-${d}`}>
            <div className="hm-row-label">{days[d]}</div>
            {HOURS.map((h) => {
              const count = counts.get(d * 24 + h) || 0;
              const bg = fill(count);
              return (
                <div
                  key={`cell-${d}-${h}`}
                  className={`hm-cell${bg ? '' : ' is-empty'}`}
                  style={bg ? { backgroundColor: bg } : undefined}
                  title={titleFor(d, h, count)}
                  aria-label={titleFor(d, h, count)}
                />
              );
            })}
          </Fragment>
        ))}
      </div>
      <div className="hm-legend">
        <span className="hm-legend-cap">{t('common.less')}</span>
        <span className="hm-legend-ramp" style={{ background: `linear-gradient(90deg, ${hexToRgba(accent, 0.12)}, ${accent})` }} />
        <span className="hm-legend-cap">{t('common.more')}</span>
        <span className="hm-legend-peak">{t('common.max')} {peak}</span>
      </div>
    </div>
  );
}
