import { Ico, useT } from '@shared';

// P0 ships the shell only: every section is a real page in the standard workspace /
// settings-panel shape, but its body is an honest empty state rather than mock data.
// Each phase replaces one of these bodies with the feature.
function PlaceholderPage({ icon, title, hint }) {
  const t = useT();
  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n={icon} /> {title}</span></h2>
        </header>
        <p className="settings-hint">{hint}</p>
        <div className="phase-empty">
          <Ico n="info" sz={18} />
          <p>{t('page.later')}</p>
        </div>
      </section>
    </section>
  );
}

export function DashboardPage() {
  const t = useT();
  return <PlaceholderPage icon="monitor" title={t('page.dashboard')} hint={t('page.dashboardHint')} />;
}

export function DevicesPage() {
  const t = useT();
  return <PlaceholderPage icon="cpu" title={t('page.devices')} hint={t('page.devicesHint')} />;
}

export function AlertsPage() {
  const t = useT();
  return <PlaceholderPage icon="bell" title={t('page.alerts')} hint={t('page.alertsHint')} />;
}

export function SettingsPage() {
  const t = useT();
  return <PlaceholderPage icon="sliders" title={t('page.settings')} hint={t('page.settingsHint')} />;
}
