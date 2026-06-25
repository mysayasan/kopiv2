import { Ico } from './icons';

// DashboardTab shows control-plane status and the signed-in session, mirroring the
// metric-grid + work-panel layout language used across the product.
export function DashboardTab({ session }) {
  return (
    <section className="workspace">
      <div className="machine-metrics">
        <div className="machine-metric-card">
          <dt>Control status</dt>
          <dd><strong className="status-pill online">Ready</strong></dd>
        </div>
        <div className="machine-metric-card">
          <dt>Resource app</dt>
          <dd><strong className="status-pill">mymatasan</strong></dd>
        </div>
        <div className="machine-metric-card">
          <dt>Identity provider</dt>
          <dd><strong className="status-pill">{session?.issuer || 'myidsan'}</strong></dd>
        </div>
      </div>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="user" /> Session</span></h2></header>
        <p className="settings-hint">
          {session
            ? `${session.name || session.email || 'Authenticated user'} is signed in through ${session.issuer || 'myidsan'}.`
            : 'Loading current account…'}
        </p>
        {session ? (
          <pre className="code-block">{JSON.stringify(session, null, 2)}</pre>
        ) : null}
      </section>
    </section>
  );
}
