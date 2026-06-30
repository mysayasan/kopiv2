import { Ico, useT } from '@shared';

// DashboardTab shows control-plane status and the signed-in session, mirroring the
// metric-grid + work-panel layout language used across the product.
export function DashboardTab({ session }) {
  const t = useT();
  return (
    <section className="workspace">
      <div className="machine-metrics">
        <div className="machine-metric-card">
          <dt>{t('dash.controlStatus')}</dt>
          <dd><strong className="status-pill online">{t('dash.ready')}</strong></dd>
        </div>
        <div className="machine-metric-card">
          <dt>{t('dash.resourceApp')}</dt>
          <dd><strong className="status-pill">mymatasan</strong></dd>
        </div>
        <div className="machine-metric-card">
          <dt>{t('dash.identityProvider')}</dt>
          <dd><strong className="status-pill">{session?.issuer || 'myidsan'}</strong></dd>
        </div>
      </div>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="user" /> {t('dash.session')}</span></h2></header>
        <p className="settings-hint">
          {session
            ? t('dash.signedInThrough', { name: session.name || session.email || t('dash.authenticatedUser'), issuer: session.issuer || 'myidsan' })
            : t('dash.loadingAccount')}
        </p>
        {session ? (
          <pre className="code-block">{JSON.stringify(session, null, 2)}</pre>
        ) : null}
      </section>
    </section>
  );
}
