import { useState } from 'react';
import { useT } from '../i18n';
import { CATEGORICAL } from './palette';
import './charts.css';

// LineChart plots a MEASURED VALUE over time: [{ t, v, suspect }] where `t` is unix
// milliseconds and `v` the reading. It is the counterpart to TimeSeriesChart, which
// stacks COUNTS of events into bars from zero — the right shape for "how many alerts
// this hour" and the wrong one for "what is the temperature". A sensor series has a
// baseline (a room does not start at 0 °C, and a freezer is negative), so this chart
// scales its y-axis to the data's own range instead of anchoring it at zero, and draws
// a continuous line rather than bars.
//
// Props:
//   points     [{ t (unix ms), v (number), suspect? (bool) }], any order
//   unit       appended to the value in the tooltip / y-axis (e.g. '°C', '%')
//   formatX    (unix ms) => axis tick label
//   color      line colour; defaults to the first categorical ramp colour
//   emptyText  shown when there is nothing to plot — never invent a line
//
// Points flagged `suspect` (outside the sensor's declared plausible range) are marked:
// they are real readings and hiding them would hide a failing sensor.
export function LineChart({ points, unit = '', formatX, color, emptyText }) {
  const t = useT();
  const [hover, setHover] = useState(-1);

  const data = (points || [])
    .filter((p) => p && typeof p.v === 'number' && isFinite(p.v))
    .slice()
    .sort((a, b) => a.t - b.t);

  if (data.length === 0) {
    return <div className="chart-empty">{emptyText != null ? emptyText : t('common.noData')}</div>;
  }

  const W = 640;
  const H = 200;
  const padL = 46;
  const padR = 10;
  const padT = 12;
  const padB = 26;
  const plotW = W - padL - padR;
  const plotH = H - padT - padB;
  const stroke = color || CATEGORICAL[0];

  // Y range follows the DATA, not zero. A flat line still needs a band to sit in, so a
  // zero-spread series gets an arbitrary but stable ±1 (or ±5%) envelope.
  let lo = Math.min(...data.map((p) => p.v));
  let hi = Math.max(...data.map((p) => p.v));
  if (hi === lo) {
    const pad = Math.max(Math.abs(hi) * 0.05, 1);
    lo -= pad;
    hi += pad;
  } else {
    const pad = (hi - lo) * 0.08;
    lo -= pad;
    hi += pad;
  }

  const t0 = data[0].t;
  const t1 = data[data.length - 1].t;
  const span = t1 - t0 || 1;

  const xFor = (ms) => padL + ((ms - t0) / span) * plotW;
  const yFor = (v) => padT + plotH - ((v - lo) / (hi - lo)) * plotH;

  const path = data.map((p, i) => `${i === 0 ? 'M' : 'L'} ${xFor(p.t).toFixed(2)} ${yFor(p.v).toFixed(2)}`).join(' ');
  const area = `${path} L ${xFor(t1).toFixed(2)} ${padT + plotH} L ${xFor(t0).toFixed(2)} ${padT + plotH} Z`;

  // 5 horizontal grid lines across the value range.
  const gridLines = [0, 0.25, 0.5, 0.75, 1].map((f) => ({
    y: padT + plotH - f * plotH,
    v: lo + f * (hi - lo),
  }));

  // 4 time ticks along the x-axis (first and last always land on real samples).
  const xTicks = [0, 0.33, 0.66, 1].map((f) => t0 + f * span);

  // Only dot individual samples when they are sparse enough to be readable.
  const showDots = data.length <= 60;

  // Nearest-sample hover: the pointer's x is mapped back to a time, then to the closest
  // sample, so hovering anywhere in the plot picks the reading a human means to inspect.
  function onMove(e) {
    const rect = e.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const svgX = ((e.clientX - rect.left) / rect.width) * W;
    const ms = t0 + ((svgX - padL) / plotW) * span;
    let best = 0;
    let bestGap = Infinity;
    for (let i = 0; i < data.length; i += 1) {
      const gap = Math.abs(data[i].t - ms);
      if (gap < bestGap) {
        bestGap = gap;
        best = i;
      }
    }
    setHover(best);
  }

  const hovered = hover >= 0 ? data[hover] : null;
  const fmtValue = (v) => `${round(v)}${unit ? ` ${unit}` : ''}`;

  return (
    <div className="ln-wrap">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="ln-svg"
        role="img"
        preserveAspectRatio="none"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(-1)}
      >
        {gridLines.map((g) => (
          <g key={g.y}>
            <line x1={padL} y1={g.y} x2={W - padR} y2={g.y} className="ln-grid" />
            <text x={padL - 6} y={g.y + 3} className="ln-ytick" textAnchor="end">{round(g.v)}</text>
          </g>
        ))}
        {xTicks.map((ms, i) => (
          <text
            key={ms}
            x={Math.min(Math.max(xFor(ms), padL + 12), W - padR - 12)}
            y={H - 8}
            className="ln-xtick"
            textAnchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
          >
            {formatX ? formatX(ms) : ''}
          </text>
        ))}
        <path d={area} className="ln-area" style={{ fill: stroke }} />
        <path d={path} className="ln-path" style={{ stroke }} />
        {showDots ? data.map((p) => (
          <circle
            key={p.t}
            cx={xFor(p.t)}
            cy={yFor(p.v)}
            r={2.4}
            className={p.suspect ? 'ln-dot ln-suspect' : 'ln-dot'}
            style={p.suspect ? undefined : { fill: stroke }}
          />
        )) : data.filter((p) => p.suspect).map((p) => (
          <circle key={p.t} cx={xFor(p.t)} cy={yFor(p.v)} r={3} className="ln-dot ln-suspect" />
        ))}
        {hovered ? (
          <g>
            <line x1={xFor(hovered.t)} y1={padT} x2={xFor(hovered.t)} y2={padT + plotH} className="ln-cursor" />
            <circle cx={xFor(hovered.t)} cy={yFor(hovered.v)} r={3.6} className="ln-dot ln-dot-hover" style={{ fill: stroke }} />
          </g>
        ) : null}
      </svg>
      {hovered ? (
        <div
          className="ln-tooltip"
          style={{ left: `${Math.min(Math.max((xFor(hovered.t) / W) * 100, 8), 92)}%` }}
        >
          <div className="ln-tt-title">{formatX ? formatX(hovered.t) : ''}</div>
          <div className="ln-tt-val">{fmtValue(hovered.v)}</div>
          {hovered.suspect ? <div className="ln-tt-suspect">{t('charts.suspect')}</div> : null}
        </div>
      ) : null}
    </div>
  );
}

// round trims float noise (20.100000000000001) without forcing decimals onto integers.
function round(v) {
  const abs = Math.abs(v);
  const digits = abs >= 100 ? 1 : abs >= 1 ? 2 : 3;
  return Number(v.toFixed(digits));
}
