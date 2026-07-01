import { useT } from '@shared/i18n';

// DashboardTab is the landing surface for mymatasan. It is intentionally empty for
// now — the shell (side-nav navigation, workspace chrome) landed first; the metric
// cards and live overview will be built out in a later batch.
export function DashboardTab() {
  const t = useT();
  return (
    <section className="workspace">
      <div className="camera-tab-header">
        <h2 className="section-title">{t('nav.dashboard')}</h2>
        <p className="section-subtitle">{t('dash.subtitle')}</p>
      </div>
      <div className="page-placeholder">
        <h2>{t('dash.emptyTitle')}</h2>
        <p>{t('dash.emptyHint')}</p>
      </div>
    </section>
  );
}
