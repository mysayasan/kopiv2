import { useState } from 'react';
import { useT } from '../i18n';
import { colorFor, niceMax } from './palette';
import './charts.css';

// TimeSeriesChart renders stacked bars of event counts over time. Each bucket is
// { start, total, byCategory } and every category becomes a colored band, so the
// bar height is the bucket total and the composition shows the mix. Hovering a
// bar reveals a tooltip with the per-category breakdown. `categories` is the
// ordered key list (stacking order); `formatX` renders a bucket start (unix s)
// as an axis tick; `label` maps a category key to a display name.
export function TimeSeriesChart({ buckets, categories, formatX, label, emptyText }) {
  const t = useT();
  const [hover, setHover] = useState(-1);
  const data = buckets || [];
  const cats = categories && categories.length ? categories : ['total'];
  const maxTotal = data.reduce((m, b) => Math.max(m, b.total || 0), 0);

  if (data.length === 0 || maxTotal === 0) {
    return <div className="chart-empty">{emptyText != null ? emptyText : t('common.noData')}</div>;
  }

  // viewBox coordinate space; the SVG scales to its container width.
  const W = 640;
  const H = 240;
  const padL = 34;
  const padR = 10;
  const padT = 12;
  const padB = 28;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;
  const yMax = niceMax(maxTotal);
  const n = data.length;
  const slot = plotW / n;
  const barW = Math.max(2, Math.min(slot * 0.7, 40));

  const yFor = (v) => padT + plotH - (v / yMax) * plotH;

  // Y grid: 4 evenly spaced lines.
  const gridLines = [0, 0.25, 0.5, 0.75, 1].map((f) => ({
    y: padT + plotH - f * plotH,
    v: Math.round(yMax * f),
  }));

  // Thin x-axis ticks so labels don't collide: aim for ~8 ticks max.
  const tickEvery = Math.max(1, Math.ceil(n / 8));

  const hovered = hover >= 0 ? data[hover] : null;

  return (
    <div className="ts-wrap">
      <svg viewBox={`0 0 ${W} ${H}`} className="ts-svg" preserveAspectRatio="none" role="img">
        {gridLines.map((g) => (
          <g key={g.y}>
            <line x1={padL} y1={g.y} x2={W - padR} y2={g.y} className="ts-grid" />
            <text x={padL - 6} y={g.y + 3} className="ts-ytick" textAnchor="end">{g.v}</text>
          </g>
        ))}
        {data.map((b, i) => {
          const cx = padL + slot * i + slot / 2;
          const x = cx - barW / 2;
          let yCursor = padT + plotH;
          return (
            <g
              key={b.start}
              onMouseEnter={() => setHover(i)}
              onMouseLeave={() => setHover(-1)}
            >
              {/* Full-slot hit area so hover works even on empty buckets. */}
              <rect x={padL + slot * i} y={padT} width={slot} height={plotH} fill="transparent" />
              {cats.map((cat, ci) => {
                const v = (b.byCategory && b.byCategory[cat]) || 0;
                if (v <= 0) return null;
                const h = (v / yMax) * plotH;
                yCursor -= h;
                return (
                  <rect
                    key={cat}
                    x={x}
                    y={yCursor}
                    width={barW}
                    height={h}
                    fill={colorFor(cat, ci, 'category')}
                    opacity={hover === -1 || hover === i ? 1 : 0.4}
                    rx={1}
                  />
                );
              })}
              {i % tickEvery === 0 ? (
                <text x={cx} y={H - 8} className="ts-xtick" textAnchor="middle">{formatX(b.start)}</text>
              ) : null}
            </g>
          );
        })}
      </svg>
      {hovered ? (
        <div
          className="ts-tooltip"
          style={{ left: `${((padL + slot * hover + slot / 2) / W) * 100}%` }}
        >
          <div className="ts-tt-title">{formatX(hovered.start)}</div>
          {cats
            .map((cat, ci) => ({ cat, ci, v: (hovered.byCategory && hovered.byCategory[cat]) || 0 }))
            .filter((r) => r.v > 0)
            .map((r) => (
              <div key={r.cat} className="ts-tt-row">
                <span className="legend-swatch" style={{ background: colorFor(r.cat, r.ci, 'category') }} />
                <span className="ts-tt-key">{label ? label(r.cat) : r.cat}</span>
                <span className="ts-tt-val">{r.v}</span>
              </div>
            ))}
          <div className="ts-tt-total">{t('common.total')} {hovered.total}</div>
        </div>
      ) : null}
      <ul className="chart-legend ts-legend">
        {cats.map((cat, ci) => (
          <li key={cat}>
            <span className="legend-swatch" style={{ background: colorFor(cat, ci, 'category') }} />
            <span className="legend-key">{label ? label(cat) : cat}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
