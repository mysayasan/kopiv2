import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { useT } from '@shared/i18n';
import { StatCard, DonutChart, BarChart, TimeSeriesChart, ChartCard } from '@shared/charts';
import { Ico } from './icons';
import {
  apiBase,
  cameraTitle,
  notificationCategoryLabels,
  notificationSourceLabels,
} from '../lib/helpers';

// The selectable time ranges. Each resolves to a concrete [from, to] window and a
// timeseries bucket size at fetch time (Today buckets by hour, longer ranges by
// day) so the chart granularity always fits the span.
const RANGES = [
  { id: 'today', labelKey: 'dash.rangeToday', bucket: 'hour' },
  { id: '7d', labelKey: 'dash.range7d', bucket: 'day' },
  { id: '30d', labelKey: 'dash.range30d', bucket: 'day' },
];

// resolveWindow turns a range id into { from, to, bucket } in unix seconds. It
// anchors the start to the viewer's local midnight so daily buckets are whole
// local days (a clean axis) rather than partial days straddling the boundary.
function resolveWindow(rangeId) {
  const now = Math.floor(Date.now() / 1000);
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const startOfToday = Math.floor(midnight.getTime() / 1000);
  if (rangeId === 'today') {
    return { from: startOfToday, to: now, bucket: 'hour' };
  }
  const days = rangeId === '30d' ? 30 : 7;
  // Include today + the previous (days-1) whole days = `days` daily buckets.
  return { from: startOfToday - (days - 1) * 86400, to: now, bucket: 'day' };
}

// DashboardTab is the landing surface: KPI tiles plus hand-rolled SVG charts of
// every notification event (AI detections, camera/machine health, login
// security, settings). It owns its own aggregated fetch from
// /api/notifications/stats and refreshes live off the same bell signal the
// notification feed uses.
export function DashboardTab({ authHeader, saved, refreshSignal, onMessage }) {
  const t = useT();
  const [range, setRange] = useState('7d');
  const [stats, setStats] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  const fetchStats = useCallback(async () => {
    setLoading(true);
    setError(false);
    try {
      const { from, to, bucket } = resolveWindow(range);
      // getTimezoneOffset is minutes to ADD to local to reach UTC; the backend
      // wants the inverse (minutes to add to UTC to reach local).
      const tzOffset = -new Date().getTimezoneOffset();
      const params = new URLSearchParams({
        from: String(from),
        to: String(to),
        bucket,
        tzOffset: String(tzOffset),
      });
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/notifications/stats?${params}`, { credentials: 'include', headers, cache: 'no-store' });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setStats(result);
    } catch (_) {
      setError(true);
      if (onMessage) onMessage(t('dash.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [range, authHeader, onMessage, t]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  // Live refresh when the bell feed changes, debounced so a burst of arrivals
  // triggers a single refetch.
  const lastSignalRef = useRef(refreshSignal);
  const debounceRef = useRef(null);
  useEffect(() => {
    if (refreshSignal === lastSignalRef.current) return undefined;
    lastSignalRef.current = refreshSignal;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(fetchStats, 1500);
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [refreshSignal, fetchStats]);

  // Camera id → display name from the saved cameras list.
  const cameraNames = useMemo(() => {
    const map = new Map();
    (saved || []).forEach((c) => map.set(Number(c.id), cameraTitle(c)));
    return map;
  }, [saved]);

  const bucket = stats?.bucket || 'day';
  const formatX = useCallback((unixSec) => {
    const d = new Date(unixSec * 1000);
    if (bucket === 'hour') {
      return `${String(d.getHours()).padStart(2, '0')}:00`;
    }
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }, [bucket]);

  const categoryLabel = useCallback((key) => notificationCategoryLabels[key] || key, []);
  const sourceLabel = useCallback((key) => notificationSourceLabels[key] || key, []);
  const severityLabel = useCallback((key) => t(`dash.sev.${key}`, {}) || key, [t]);
  const cameraLabel = useCallback((id) => cameraNames.get(Number(id)) || t('dash.cameraN', { id }), [cameraNames, t]);
  const capitalize = useCallback((s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s), []);

  // Ordered category keys for the stacked timeseries (well-known first, then any
  // extras present in the data), so band colors stay stable across refreshes.
  const seriesCategories = useMemo(() => {
    const known = ['vision.alert', 'health.check', 'system'];
    const present = new Set();
    (stats?.timeseries || []).forEach((b) => Object.keys(b.byCategory || {}).forEach((k) => present.add(k)));
    (stats?.byCategory || []).forEach((c) => present.add(c.key));
    const ordered = known.filter((k) => present.has(k));
    present.forEach((k) => { if (!ordered.includes(k)) ordered.push(k); });
    return ordered;
  }, [stats]);

  // Delta vs the previous equal-length window, as a signed percentage.
  const delta = useMemo(() => {
    if (!stats || typeof stats.prevTotal !== 'number' || stats.prevTotal <= 0) return null;
    return Math.round(((stats.total - stats.prevTotal) / stats.prevTotal) * 100);
  }, [stats]);

  const cameraData = useMemo(
    () => (stats?.topCameras || []).map((c) => ({ key: String(c.cameraId), count: c.count })),
    [stats],
  );

  const hasData = stats && stats.total > 0;

  return (
    <section className="workspace dashboard-tab">
      <div className="camera-tab-header dashboard-head">
        <div>
          <h2 className="section-title">{t('nav.dashboard')}</h2>
          <p className="section-subtitle">{t('dash.subtitle')}</p>
        </div>
        <div className="dashboard-toolbar">
          <div className="seg-toggle" role="group" aria-label={t('dash.rangeLabel')}>
            {RANGES.map((r) => (
              <button
                key={r.id}
                type="button"
                className={range === r.id ? 'active' : 'quiet'}
                onClick={() => setRange(r.id)}
              >
                {t(r.labelKey)}
              </button>
            ))}
          </div>
          <button type="button" className="quiet" onClick={fetchStats} disabled={loading}>
            <span className="btn-icon"><Ico n="refresh" /> {t('dash.refresh')}</span>
          </button>
        </div>
      </div>

      {!hasData ? (
        <div className="page-placeholder">
          <h2>{error ? t('dash.loadFailed') : t('dash.emptyTitle')}</h2>
          <p>{error ? t('dash.retryHint') : t('dash.emptyHint')}</p>
        </div>
      ) : (
        <div className="dashboard-grid">
          <div className="dashboard-kpis">
            <StatCard
              label={t('dash.kpiTotal')}
              value={stats.total}
              icon={<Ico n="bell" sz={16} />}
              delta={delta}
              deltaGood={false}
              hint={t('dash.vsPrev')}
            />
            <StatCard
              label={t('dash.kpiUnread')}
              value={stats.unread}
              icon={<Ico n="bell" sz={16} />}
              tone={stats.unread > 0 ? 'accent' : 'default'}
              hint={stats.unread > 0 ? t('dash.reviewHint') : ''}
            />
            <StatCard
              label={t('dash.kpiCritical')}
              value={stats.critical}
              icon={<Ico n="warning" sz={16} />}
              tone={stats.critical > 0 ? 'danger' : 'default'}
            />
            <StatCard
              label={t('dash.kpiWarning')}
              value={stats.warning}
              icon={<Ico n="warning" sz={16} />}
              tone={stats.warning > 0 ? 'warning' : 'default'}
            />
          </div>

          <ChartCard
            title={t('dash.eventsOverTime')}
            subtitle={t('dash.eventsOverTimeSub')}
            className="dashboard-span-2"
          >
            <TimeSeriesChart
              buckets={stats.timeseries}
              categories={seriesCategories}
              formatX={formatX}
              label={categoryLabel}
              emptyText={t('dash.noData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.byCategory')}>
            <DonutChart
              data={stats.byCategory}
              kind="category"
              label={categoryLabel}
              emptyText={t('dash.noData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.bySeverity')}>
            <DonutChart
              data={stats.bySeverity}
              kind="severity"
              label={severityLabel}
              emptyText={t('dash.noData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.topCameras')} subtitle={t('dash.topCamerasSub')}>
            <BarChart
              data={cameraData}
              label={cameraLabel}
              accent="#3b82f6"
              emptyText={t('dash.noCameraData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.topObjects')} subtitle={t('dash.topObjectsSub')}>
            <BarChart
              data={stats.topLabels}
              label={capitalize}
              accent="#8b5cf6"
              emptyText={t('dash.noObjectData')}
            />
          </ChartCard>

          <ChartCard title={t('dash.bySource')} subtitle={t('dash.bySourceSub')}>
            <BarChart
              data={stats.bySource}
              label={sourceLabel}
              kind="plain"
              emptyText={t('dash.noData')}
            />
          </ChartCard>
        </div>
      )}
    </section>
  );
}
