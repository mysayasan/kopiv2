import { useT } from '../i18n';
import { colorFor } from './palette';
import './charts.css';

// BarChart is a horizontal ranked bar list from [{ key, count }] data: a label
// column, a proportional bar, and the value. Ideal for "top N" breakdowns
// (cameras, detected objects) where names are variable-length. `label` maps a
// raw key to a display string; `accent` forces a single bar color (else the
// categorical ramp is used).
export function BarChart({ data, label, accent, kind = 'plain', emptyText }) {
  const t = useT();
  const items = (data || []).filter((d) => d.count > 0);
  if (items.length === 0) {
    return <div className="chart-empty">{emptyText != null ? emptyText : t('common.noData')}</div>;
  }
  const max = Math.max(...items.map((d) => d.count));

  return (
    <ul className="hbar-list">
      {items.map((d, i) => {
        const pct = max > 0 ? (d.count / max) * 100 : 0;
        const color = accent || colorFor(d.key, i, kind);
        return (
          <li key={d.key} className="hbar-row">
            <span className="hbar-label" title={label ? label(d.key) : d.key}>
              {label ? label(d.key) : d.key}
            </span>
            <span className="hbar-track">
              <span className="hbar-fill" style={{ width: `${Math.max(pct, 2)}%`, background: color }} />
            </span>
            <span className="hbar-value">{d.count}</span>
          </li>
        );
      })}
    </ul>
  );
}
