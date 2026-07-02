import './charts.css';

// StatCard is a single KPI tile: a big value, a caption, an optional icon, and
// an optional delta vs the previous period (▲ up / ▼ down, colored by whether
// up is good). Tone tints the value color for emphasis (e.g. critical → danger).
export function StatCard({ label, value, icon, delta, deltaGood, tone = 'default', hint }) {
  const hasDelta = typeof delta === 'number' && isFinite(delta);
  const up = hasDelta && delta > 0;
  const down = hasDelta && delta < 0;
  // deltaGood tells us whether "up" is a good thing (default: neutral). When the
  // direction is good → success color, bad → danger, flat/unknown → muted.
  let deltaClass = 'stat-delta';
  if (hasDelta && delta !== 0 && typeof deltaGood === 'boolean') {
    deltaClass += (up === deltaGood) ? ' is-good' : ' is-bad';
  }
  return (
    <div className={`stat-card stat-tone-${tone}`}>
      <div className="stat-card-head">
        <span className="stat-label">{label}</span>
        {icon ? <span className="stat-icon">{icon}</span> : null}
      </div>
      <div className="stat-value">{value}</div>
      <div className="stat-foot">
        {hasDelta ? (
          <span className={deltaClass}>
            {up ? '▲' : down ? '▼' : '±'} {Math.abs(delta)}%
          </span>
        ) : null}
        {hint ? <span className="stat-hint">{hint}</span> : null}
      </div>
    </div>
  );
}
