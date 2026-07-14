import { useCallback, useEffect, useState } from 'react';
import { Ico, useT } from '@shared';
import { ChartCard, DonutChart, StatCard } from '@shared/charts';
import { accessError, api, forbidden, formatAgo, formatCount, formatNumber, formatTimestamp } from './lib/helpers';
import { AccessDenied, EmptyState, Panel, SeverityPill } from './ui';

// The sensor hub at a glance, over the tunnel: how many devices are talking, what has fired
// recently, and — the panel an operator learns to read first — what the ingest path is doing
// with everything the sensors send.
export function DashboardPage({ onNavigate }) {
  const t = useT();
  const [devices, setDevices] = useState([]);
  const [stats, setStats] = useState(null);
  const [alerts, setAlerts] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [denied, setDenied] = useState(false);

  const load = useCallback(async () => {
    const [dr, sr, ar] = await Promise.all([
      api('/api/devices?limit=1000'),
      api('/api/devices/stats'),
      api('/api/alerts?limit=6'),
    ]);
    if (dr.ok) setDevices(dr.body?.items || []);
    if (sr.ok) setStats(sr.body || null);
    if (ar.ok) setAlerts(ar.body?.items || []);
    // A refusal is not an empty estate. If the node will not show us its devices, say that —
    // do not render zeroes that read as "this hub has nothing on it".
    setDenied(forbidden(dr));
    setError(!dr.ok && !forbidden(dr) ? accessError(dr, t, t('iot.dash.loadFailed')) : '');
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);
  // Stop polling once the node has refused us. The answer cannot change while the page is
  // open, and every retry is another 403 — each of which the commander AUDITS. A denied
  // operator idling on this tab must not quietly write an audit row every fifteen seconds.
  useEffect(() => {
    if (denied) return undefined;
    const id = setInterval(load, 15000);
    return () => clearInterval(id);
  }, [load, denied]);

  const online = devices.filter((d) => d.health === 'online').length;
  const offline = devices.filter((d) => d.health === 'offline').length;
  const unknown = devices.length - online - offline;
  const unacked = alerts.filter((a) => !a.ackedAt).length;

  const healthData = [
    { key: 'online', count: online },
    { key: 'offline', count: offline },
    { key: 'unknown', count: unknown },
  ];

  if (denied) {
    return (
      <section className="workspace">
        <Panel icon="monitor" title={t('page.dashboard')}>
          <AccessDenied />
        </Panel>
      </section>
    );
  }

  return (
    <section className="workspace">
      <Panel
        icon="monitor"
        title={t('page.dashboard')}
        hint={t('page.dashboardHint')}
        actions={(
          <button type="button" className="quiet" onClick={load} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
          </button>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        <div className="settings-grid">
          <StatCard label={t('dash.devices')} value={devices.length} icon={<Ico n="cpu" sz={16} />} />
          <StatCard label={t('health.online')} value={online} tone="success" />
          <StatCard
            label={t('health.offline')}
            value={offline}
            tone={offline > 0 ? 'danger' : 'default'}
            hint={offline > 0 ? t('dash.offlineHint') : ''}
          />
          <StatCard label={t('health.unknown')} value={unknown} hint={unknown > 0 ? t('dash.unknownHint') : ''} />
        </div>
      </Panel>

      <div className="iot-cols-2">
        <ChartCard title={t('dash.healthTitle')} subtitle={t('dash.healthSubtitle')}>
          <DonutChart
            data={healthData}
            kind="health"
            label={(k) => t(`health.${k}`)}
            emptyText={t('dash.noDevices')}
          />
        </ChartCard>

        <ChartCard
          title={t('dash.recentAlerts')}
          subtitle={unacked > 0 ? t('dash.unacked', { n: unacked }) : t('dash.allAcked')}
          actions={onNavigate ? (
            <button type="button" className="quiet compact-button" onClick={() => onNavigate('alerts')}>
              {t('dash.viewAll')}
            </button>
          ) : null}
        >
          {alerts.length === 0 ? (
            <EmptyState text={t('dash.noAlerts')} icon="check-ok" />
          ) : (
            <ul className="iot-alert-feed">
              {alerts.map((a) => (
                <li key={a.id} className={a.ackedAt ? 'is-acked' : ''}>
                  <SeverityPill severity={a.severity} />
                  <div className="iot-alert-feed-body">
                    <strong>{a.ruleName}</strong>
                    <span>{a.message}</span>
                  </div>
                  <span className="iot-alert-feed-meta" title={formatTimestamp(a.createdAt)}>
                    {a.deviceName}
                    <em>{formatAgo(a.createdAt, t)}</em>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </ChartCard>
      </div>

      <IngestPanel stats={stats} />
    </section>
  );
}

// IngestPanel is the storage design, made visible.
//
// `suppressed` vs `stored` is the deadband earning its keep: a building sensor reports the
// same value over and over, and persisting only the changes is what keeps a hundred devices
// inside one SQLite file. A suppression ratio near 98% is normal and GOOD.
//
// `dropped` is the opposite: it means the writer's queue overflowed and readings were
// thrown away because the disk could not keep up. It is the one number here that must never
// be non-zero, so it gets its own alarm rather than a slot in a row of counters.
export function IngestPanel({ stats }) {
  const t = useT();
  if (!stats) {
    return (
      <Panel icon="server" title={t('ingest.title')} hint={t('ingest.hint')}>
        <EmptyState text={t('ingest.noStats')} />
      </Panel>
    );
  }

  const considered = (stats.stored || 0) + (stats.suppressed || 0);
  const suppressionPct = considered > 0 ? (stats.suppressed / considered) * 100 : 0;
  const dropped = stats.dropped || 0;

  return (
    <Panel icon="server" title={t('ingest.title')} hint={t('ingest.hint')}>
      {dropped > 0 ? (
        <div className="iot-ingest-alarm">
          <Ico n="warning" sz={18} />
          <p>{t('ingest.droppedAlarm', { n: formatCount(dropped) })}</p>
        </div>
      ) : (
        <p className="iot-ingest-ok">
          <Ico n="check-ok" sz={14} /> {t('ingest.droppedNone')}
        </p>
      )}

      <div className="settings-grid">
        <StatCard label={t('ingest.received')} value={formatCount(stats.received)} hint={t('ingest.receivedHint')} />
        <StatCard label={t('ingest.stored')} value={formatCount(stats.stored)} hint={t('ingest.storedHint')} />
        <StatCard
          label={t('ingest.suppressed')}
          value={formatCount(stats.suppressed)}
          tone="success"
          hint={t('ingest.suppressedHint')}
        />
        <StatCard
          label={t('ingest.dropped')}
          value={formatCount(dropped)}
          tone={dropped > 0 ? 'danger' : 'default'}
          hint={t('ingest.droppedHint')}
        />
      </div>

      {considered > 0 ? (
        <div className="iot-suppression">
          <div className="iot-suppression-bar" role="img" aria-label={t('ingest.ratioLabel', { pct: formatNumber(suppressionPct) })}>
            <span className="iot-suppression-fill" style={{ width: `${Math.min(100, suppressionPct)}%` }} />
          </div>
          <p className="iot-suppression-text">
            {t('ingest.explain', {
              pct: formatNumber(suppressionPct),
              stored: formatCount(stats.stored),
              considered: formatCount(considered),
            })}
          </p>
          {suppressionPct < 50 ? <p className="field-hint">{t('ingest.lowSuppression')}</p> : null}
        </div>
      ) : (
        <p className="settings-hint">{t('ingest.noSamples')}</p>
      )}

      <div className="meta-grid iot-meta">
        <div><span>{t('ingest.decoded')}</span><strong>{formatCount(stats.decoded)}</strong></div>
        <div><span>{t('ingest.written')}</span><strong>{formatCount(stats.written)}</strong></div>
        <div><span>{t('ingest.queued')}</span><strong>{formatCount(stats.queued)}</strong></div>
        <div><span>{t('ingest.series')}</span><strong>{formatCount(stats.series)}</strong></div>
      </div>
      <p className="settings-hint">{t('ingest.counterNote')}</p>
    </Panel>
  );
}
