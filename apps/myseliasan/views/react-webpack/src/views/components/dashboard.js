import { useState, useEffect, useMemo, useCallback } from 'react';
import { Ico, useT } from '@shared';
import { StatCard, DonutChart, BarChart, TimeSeriesChart, ChartCard } from '@shared/charts';
import { api, formatTimestamp } from '../lib/helpers';

// DashboardTab is the control plane's landing dashboard. It mirrors the mymatasan app
// dashboard (same @shared/charts + KPI layout), but the unit of aggregation is the NODE
// instead of the camera: it reads myseliasan's OWN notifications table (node-pushed
// events + control-plane events) via /api/notifications/stats and breaks activity down
// per adopted node (Stats.BySource, where every node event carries source "node:<id>").
// No per-node fan-out is needed — the control plane already stores every event.

const notificationCategoryLabels = {
  'vision.alert': 'AI Detection',
  'health.check': 'Camera Health',
  system: 'System',
};

// Selectable time ranges → concrete [from,to] window + a timeseries bucket size.
const RANGES = [
  { id: 'today', labelKey: 'dash.rangeToday', bucket: 'hour' },
  { id: '7d', labelKey: 'dash.range7d', bucket: 'day' },
  { id: '30d', labelKey: 'dash.range30d', bucket: 'day' },
];

// resolveWindow turns a range id into { from, to, bucket } in unix seconds, anchored to
// the viewer's local midnight so daily buckets are whole local days.
function resolveWindow(rangeId) {
  const now = Math.floor(Date.now() / 1000);
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const startOfToday = Math.floor(midnight.getTime() / 1000);
  if (rangeId === 'today') return { from: startOfToday, to: now, bucket: 'hour' };
  const days = rangeId === '30d' ? 30 : 7;
  return { from: startOfToday - (days - 1) * 86400, to: now, bucket: 'day' };
}

const nodeStatusKey = (status) => {
  switch ((status || '').toLowerCase()) {
    case 'online': return 'dash.statusOnline';
    case 'lost': return 'dash.statusLost';
    case 'self-dropped': return 'dash.statusSelfDropped';
    default: return 'dash.statusUnknown';
  }
};

export function DashboardTab({ nodes }) {
  const t = useT();
  const [range, setRange] = useState('7d');
  const [stats, setStats] = useState(null);
  const [fleet, setFleet] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  const fetchStats = useCallback(async () => {
    setLoading(true);
    setError(false);
    const { from, to, bucket } = resolveWindow(range);
    const tzOffset = -new Date().getTimezoneOffset();
    const params = new URLSearchParams({ from: String(from), to: String(to), bucket, tzOffset: String(tzOffset) });
    const [r, f] = await Promise.all([
      api(`/api/notifications/stats?${params}`, { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/nodes/fleet-status', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    setLoading(false);
    if (r.ok) setStats(r.body || null); else setError(true);
    if (f.ok) setFleet(f.body || null);
  }, [range]);
  useEffect(() => { fetchStats(); }, [fetchStats]);

  const nodeList = useMemo(() => (Array.isArray(nodes) ? nodes : []), [nodes]);
  const nodeNames = useMemo(() => {
    const m = new Map();
    nodeList.forEach((n) => m.set(n.nodeId, n.name || n.nodeId));
    return m;
  }, [nodeList]);
  const onlineCount = useMemo(() => nodeList.filter((n) => (n.status || '').toLowerCase() === 'online').length, [nodeList]);
  const certExpiring = fleet?.certsExpiring || 0;
  const certExpired = fleet?.certsExpired || 0;
  const certWarnDays = fleet?.certWarnDays || 7;

  const delta = useMemo(() => {
    if (!stats || typeof stats.prevTotal !== 'number' || stats.prevTotal <= 0) return null;
    return Math.round(((stats.total - stats.prevTotal) / stats.prevTotal) * 100);
  }, [stats]);

  const bucket = stats?.bucket || 'day';
  const formatX = useCallback((unixSec) => {
    const d = new Date(unixSec * 1000);
    if (bucket === 'hour') return `${String(d.getHours()).padStart(2, '0')}:00`;
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }, [bucket]);
  const categoryLabel = useCallback((k) => notificationCategoryLabels[k] || k, []);
  const severityLabel = useCallback((k) => t(`dash.sev.${k}`, {}) || k, [t]);

  // Resolve a bySource key: "node:<id>" → the node's display name; anything else is a
  // control-plane event.
  const sourceNodeLabel = useCallback((key) => {
    if (typeof key === 'string' && key.startsWith('node:')) {
      const id = key.slice(5);
      return nodeNames.get(id) || `${id.slice(0, 8)}…`;
    }
    return t('dash.controlPlane');
  }, [nodeNames, t]);

  // Events-over-time category bands (stable, known-first order).
  const seriesCategories = useMemo(() => {
    const known = ['vision.alert', 'health.check', 'system'];
    const present = new Set();
    (stats?.timeseries || []).forEach((b) => Object.keys(b.byCategory || {}).forEach((k) => present.add(k)));
    (stats?.byCategory || []).forEach((c) => present.add(c.key));
    const ordered = known.filter((k) => present.has(k));
    present.forEach((k) => { if (!ordered.includes(k)) ordered.push(k); });
    return ordered;
  }, [stats]);

  // Per-node event counts (from bySource, node entries only), for the Nodes table.
  const nodeEventCount = useMemo(() => {
    const m = new Map();
    (stats?.bySource || []).forEach((s) => {
      if (typeof s.key === 'string' && s.key.startsWith('node:')) m.set(s.key.slice(5), s.count);
    });
    return m;
  }, [stats]);

  const nodeRows = useMemo(() => nodeList
    .map((n) => ({
      id: n.nodeId,
      name: n.name || n.nodeId,
      icon: n.icon || 'monitor',
      status: (n.status || 'unknown').toLowerCase(),
      lastSeen: n.lastSeenAt || 0,
      events: nodeEventCount.get(n.nodeId) || 0,
    }))
    // Offline/lost first (they need attention), then by event volume.
    .sort((a, b) => (a.status === 'online' ? 1 : 0) - (b.status === 'online' ? 1 : 0) || b.events - a.events),
  [nodeList, nodeEventCount]);

  const hasNodes = nodeList.length > 0;

  return (
    <section className="workspace dashboard-tab">
      <div className="camera-tab-header dashboard-head">
        <div>
          <h2 className="section-title">{t('nav.dashboard')}</h2>
          <p className="section-subtitle">{t('dash.fleetSubtitle')}</p>
        </div>
        <div className="dashboard-toolbar">
          <div className="seg-toggle" role="group" aria-label={t('dash.rangeLabel')}>
            {RANGES.map((r) => (
              <button key={r.id} type="button" className={range === r.id ? 'active' : 'quiet'} onClick={() => setRange(r.id)}>
                {t(r.labelKey)}
              </button>
            ))}
          </div>
          <button type="button" className="quiet" onClick={fetchStats} disabled={loading}>
            <span className="btn-icon"><Ico n="reload" /> {t('dash.refresh')}</span>
          </button>
        </div>
      </div>

      {!hasNodes ? (
        <div className="page-placeholder">
          <h2>{error ? t('dash.loadFailed') : t('dash.noNodesYet')}</h2>
          <p>{error ? t('dash.retryHint') : t('dash.noNodesHint')}</p>
        </div>
      ) : (
        <div className="dashboard-grid">
          <div className="dashboard-kpis">
            <StatCard
              label={t('dash.nodesOnline')}
              value={onlineCount}
              icon={<Ico n="monitor" sz={16} />}
              tone={onlineCount < nodeList.length ? 'warning' : 'default'}
              hint={t('dash.ofAdopted', { total: nodeList.length })}
            />
            <StatCard
              label={t('dash.kpiTotal')}
              value={stats?.total ?? 0}
              icon={<Ico n="bell" sz={16} />}
              delta={delta}
              deltaGood={false}
              hint={t('dash.vsPrev')}
            />
            <StatCard
              label={t('dash.kpiUnread')}
              value={stats?.unread ?? 0}
              icon={<Ico n="bell" sz={16} />}
              tone={(stats?.unread || 0) > 0 ? 'accent' : 'default'}
              hint={(stats?.unread || 0) > 0 ? t('dash.reviewHint') : ''}
            />
            <StatCard
              label={t('dash.kpiCritical')}
              value={stats?.critical ?? 0}
              icon={<Ico n="warning" sz={16} />}
              tone={(stats?.critical || 0) > 0 ? 'danger' : 'default'}
            />
            <StatCard
              label={t('dash.certsExpiring')}
              value={certExpiring + certExpired}
              icon={<Ico n="shield" sz={16} />}
              tone={certExpired > 0 ? 'danger' : (certExpiring > 0 ? 'warning' : 'default')}
              hint={certExpired > 0
                ? t('dash.certsExpiredHint', { count: certExpired })
                : (certExpiring > 0 ? t('dash.certsExpiringHint', { days: certWarnDays }) : t('dash.certsHealthy'))}
            />
          </div>

          <ChartCard title={t('dash.eventsOverTime')} subtitle={t('dash.eventsOverTimeSub')} className="dashboard-span-2">
            <TimeSeriesChart
              buckets={stats?.timeseries}
              categories={seriesCategories}
              formatX={formatX}
              label={categoryLabel}
              emptyText={t('dash.noData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.byNode')} subtitle={t('dash.byNodeSub')} className="dashboard-span-2">
            <BarChart data={stats?.bySource} label={sourceNodeLabel} accent="#3b82f6" emptyText={t('dash.noNodeData')} />
          </ChartCard>

          <ChartCard title={t('dash.byCategory')}>
            <DonutChart data={stats?.byCategory} kind="category" label={categoryLabel} emptyText={t('dash.noData')} />
          </ChartCard>
          <ChartCard title={t('dash.bySeverity')}>
            <DonutChart data={stats?.bySeverity} kind="severity" label={severityLabel} emptyText={t('dash.noData')} />
          </ChartCard>

          <ChartCard title={t('dash.nodesTitle')} subtitle={t('dash.nodesSub')} className="dashboard-span-2">
            <table className="reliability-table">
              <thead>
                <tr>
                  <th>{t('dash.colNode')}</th>
                  <th>{t('dash.colStatus')}</th>
                  <th>{t('dash.colEvents')}</th>
                  <th>{t('dash.colLastSeen')}</th>
                </tr>
              </thead>
              <tbody>
                {nodeRows.map((n) => (
                  <tr key={n.id}>
                    <td><span className="node-name-cell"><Ico n={n.icon} sz={15} /> {n.name}</span></td>
                    <td><span className={`status-pill ${n.status === 'online' ? 'online' : 'offline'}`}>{t(nodeStatusKey(n.status))}</span></td>
                    <td>{n.events}</td>
                    <td>{n.lastSeen ? formatTimestamp(n.lastSeen) : t('dash.never')}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </ChartCard>
        </div>
      )}
    </section>
  );
}
