import './charts.css';

// ChartCard frames a chart in a titled surface panel that follows the app theme
// via the --ui-* tokens. `actions` renders controls (e.g. a toggle) on the right
// of the header; `subtitle` is optional supporting text.
export function ChartCard({ title, subtitle, actions, children, className = '' }) {
  return (
    <section className={`chart-card ${className}`.trim()}>
      {(title || actions) ? (
        <header className="chart-card-head">
          <div className="chart-card-titles">
            {title ? <h3 className="chart-card-title">{title}</h3> : null}
            {subtitle ? <p className="chart-card-subtitle">{subtitle}</p> : null}
          </div>
          {actions ? <div className="chart-card-actions">{actions}</div> : null}
        </header>
      ) : null}
      <div className="chart-card-body">{children}</div>
    </section>
  );
}
