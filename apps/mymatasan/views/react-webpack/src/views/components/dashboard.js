import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';
import { StatCard, DonutChart, BarChart, TimeSeriesChart, Heatmap, ChartCard } from '@shared/charts';
import { Ico } from './icons';
// Toggle lives in ./ui so the People roster uses the SAME switch rather than a second
// copy that drifts from this one.
import { Toggle } from './ui';
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

// InfoTip is a small muted info icon with a hover/focus tooltip explaining a control.
function InfoTip({ text }) {
  return (
    <span className="info-tip" tabIndex={0} role="img" aria-label={text} title={text}>
      <Ico n="info" sz={14} />
    </span>
  );
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
  // The activity heatmap has its own camera scope (0 = all) and a fixed 28-day
  // window: a stable weekly rhythm needs weeks of history regardless of the KPI
  // range above.
  const [heatCamera, setHeatCamera] = useState(0);
  const [heatmap, setHeatmap] = useState(null);
  // Expected-activity band for the events-over-time chart, aligned to the same
  // range/bucket as the KPI stats above.
  const [baseline, setBaseline] = useState(null);
  // Statistical anomaly monitor: its runtime config + an on-demand "scan last hour"
  // preview of what it would flag with the current sensitivity.
  const [anomalyCfg, setAnomalyCfg] = useState(null);
  const [anomalyFindings, setAnomalyFindings] = useState(null);
  const [anomalyScanning, setAnomalyScanning] = useState(false);
  // Per-camera reliability (uptime/outages) over the last 7 days.
  const [reliability, setReliability] = useState(null);
  // Noisiest cameras (AI-alert volume + unreviewed ratio) over the last 7 days.
  const [noise, setNoise] = useState(null);

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

  // The heatmap fetch is independent of the KPI range and camera-scoped. It is
  // supplementary, so a failure is swallowed rather than blocking the dashboard.
  const fetchHeatmap = useCallback(async () => {
    try {
      const to = Math.floor(Date.now() / 1000);
      const from = to - 28 * 86400;
      const tzOffset = -new Date().getTimezoneOffset();
      const params = new URLSearchParams({ from: String(from), to: String(to), tzOffset: String(tzOffset) });
      if (heatCamera > 0) params.set('cameraId', String(heatCamera));
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/notifications/heatmap?${params}`, { credentials: 'include', headers, cache: 'no-store' });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setHeatmap(result);
    } catch (_) {
      setHeatmap(null);
    }
  }, [heatCamera, authHeader]);

  // The expected-activity band tracks the same range/bucket as the KPI stats so it
  // overlays the events-over-time chart cleanly. Supplementary → failure is swallowed.
  const fetchBaseline = useCallback(async () => {
    try {
      const { from, to, bucket } = resolveWindow(range);
      const tzOffset = -new Date().getTimezoneOffset();
      const params = new URLSearchParams({ from: String(from), to: String(to), bucket, tzOffset: String(tzOffset) });
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/notifications/baseline?${params}`, { credentials: 'include', headers, cache: 'no-store' });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setBaseline(result);
    } catch (_) {
      setBaseline(null);
    }
  }, [range, authHeader]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  useEffect(() => {
    fetchHeatmap();
  }, [fetchHeatmap]);

  useEffect(() => {
    fetchBaseline();
  }, [fetchBaseline]);

  const authFetch = useCallback((path, opts = {}) => {
    const headers = { ...(opts.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    return fetch(`${apiBase()}${path}`, { credentials: 'include', cache: 'no-store', ...opts, headers });
  }, [authHeader]);

  const fetchAnomalyCfg = useCallback(async () => {
    try {
      const resp = await authFetch('/api/anomaly/settings');
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      setAnomalyCfg(payload?.data?.result ?? payload?.result ?? payload);
    } catch (_) {
      setAnomalyCfg(null);
    }
  }, [authFetch]);

  useEffect(() => {
    fetchAnomalyCfg();
  }, [fetchAnomalyCfg]);

  const fetchReliability = useCallback(async () => {
    try {
      const to = Math.floor(Date.now() / 1000);
      const from = to - 7 * 86400;
      const resp = await authFetch(`/api/notifications/reliability?from=${from}&to=${to}`);
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      setReliability(payload?.data?.result ?? payload?.result ?? payload);
    } catch (_) {
      setReliability(null);
    }
  }, [authFetch]);

  useEffect(() => {
    fetchReliability();
  }, [fetchReliability]);

  const fetchNoise = useCallback(async () => {
    try {
      const to = Math.floor(Date.now() / 1000);
      const from = to - 7 * 86400;
      const resp = await authFetch(`/api/notifications/noise?from=${from}&to=${to}`);
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      setNoise(payload?.data?.result ?? payload?.result ?? payload);
    } catch (_) {
      setNoise(null);
    }
  }, [authFetch]);

  useEffect(() => {
    fetchNoise();
  }, [fetchNoise]);

  // Persist a partial change to the anomaly config (optimistic; re-syncs from the
  // normalized server response).
  const saveAnomalyCfg = useCallback(async (patch) => {
    if (!anomalyCfg) return;
    const next = { ...anomalyCfg, ...patch };
    setAnomalyCfg(next);
    try {
      const resp = await authFetch('/api/anomaly/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(next),
      });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      setAnomalyCfg(payload?.data?.result ?? payload?.result ?? next);
    } catch (_) {
      if (onMessage) onMessage(t('dash.anomalySaveFailed'), 'error');
    }
  }, [anomalyCfg, authFetch, onMessage, t]);

  const runAnomalyScan = useCallback(async () => {
    setAnomalyScanning(true);
    try {
      const tzOffset = -new Date().getTimezoneOffset();
      const resp = await authFetch(`/api/anomaly/scan?tzOffset=${tzOffset}`);
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setAnomalyFindings(result?.findings || []);
    } catch (_) {
      setAnomalyFindings([]);
      if (onMessage) onMessage(t('dash.anomalyScanFailed'), 'error');
    } finally {
      setAnomalyScanning(false);
    }
  }, [authFetch, onMessage, t]);

  // Live refresh when the bell feed changes, debounced so a burst of arrivals
  // triggers a single refetch.
  const lastSignalRef = useRef(refreshSignal);
  const debounceRef = useRef(null);
  useEffect(() => {
    if (refreshSignal === lastSignalRef.current) return undefined;
    lastSignalRef.current = refreshSignal;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => { fetchStats(); fetchHeatmap(); fetchBaseline(); fetchReliability(); fetchNoise(); }, 1500);
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [refreshSignal, fetchStats, fetchHeatmap, fetchBaseline, fetchReliability, fetchNoise]);

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

  // Localized short weekday names (index 0 = Sunday, matching the backend grid)
  // and the per-cell hover/aria label for the activity heatmap.
  // Human "1h 20m" / "45m" / "—" for an offline-seconds duration.
  const formatDuration = useCallback((seconds) => {
    if (!seconds || seconds <= 0) return '—';
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds % 3600) / 60);
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }, []);

  // Merge the per-camera reliability response with the full saved-camera list so
  // every camera shows (cameras with no health events = 100% / healthy), worst first.
  const reliabilityRows = useMemo(() => {
    const byId = new Map((reliability?.cameras || []).map((c) => [Number(c.cameraId), c]));
    const ids = (saved && saved.length) ? saved.map((c) => Number(c.id)) : [...byId.keys()];
    const rows = ids.map((id) => {
      const r = byId.get(id);
      return {
        id,
        name: cameraNames.get(id) || t('dash.cameraN', { id }),
        uptime: r ? r.uptimePercent : 100,
        offline: r ? r.offlineSeconds : 0,
        incidents: r ? r.incidents : 0,
        down: r ? r.currentlyOffline : false,
        known: !!r,
      };
    });
    rows.sort((a, b) => a.uptime - b.uptime || b.incidents - a.incidents);
    return rows;
  }, [reliability, saved, cameraNames, t]);

  const noiseRows = useMemo(() => (noise?.cameras || []).map((c) => ({
    id: Number(c.cameraId),
    name: cameraNames.get(Number(c.cameraId)) || t('dash.cameraN', { id: c.cameraId }),
    count: c.count,
    unread: c.unread,
    unreadPct: c.count > 0 ? Math.round((c.unread / c.count) * 100) : 0,
  })), [noise, cameraNames, t]);

  const dayLabels = useMemo(() => t('dash.weekdaysShort').split(','), [t]);
  const heatmapCellTitle = useCallback(
    (d, h, c) => `${dayLabels[d]} ${String(h).padStart(2, '0')}:00 · ${c} ${t('dash.eventsWord')}`,
    [dayLabels, t],
  );

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
          <button type="button" className="quiet" onClick={() => { fetchStats(); fetchHeatmap(); fetchBaseline(); fetchReliability(); fetchNoise(); }} disabled={loading}>
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
              band={baseline?.buckets}
            />
          </ChartCard>

          <ChartCard
            title={t('dash.activityHeatmap')}
            subtitle={t('dash.activityHeatmapSub')}
            className="dashboard-span-2"
            actions={(
              <select
                className="quiet heatmap-camera-select"
                value={heatCamera}
                onChange={(e) => setHeatCamera(Number(e.target.value))}
                aria-label={t('dash.allCameras')}
              >
                <option value={0}>{t('dash.allCameras')}</option>
                {(saved || []).map((c) => (
                  <option key={c.id} value={c.id}>{cameraTitle(c)}</option>
                ))}
              </select>
            )}
          >
            <Heatmap
              cells={heatmap?.cells}
              max={heatmap?.max}
              dayLabels={dayLabels}
              cellTitle={heatmapCellTitle}
              emptyText={t('dash.noData')}
            />
          </ChartCard>

          {anomalyCfg ? (
            <ChartCard
              title={<>{t('dash.anomalyTitle')} <InfoTip text={t('dash.anomalyInfo')} /> <HelpButton slug="dashboard" anchor="anomaly" /></>}
              subtitle={t('dash.anomalySub')}
              className="dashboard-span-2 anomaly-card"
              actions={(
                <div className="anomaly-enable">
                  <Toggle checked={!!anomalyCfg.enabled} onChange={(e) => saveAnomalyCfg({ enabled: e.target.checked })} />
                  <span className="anomaly-enable-label">{anomalyCfg.enabled ? t('dash.anomalyOn') : t('dash.anomalyOff')}</span>
                </div>
              )}
            >
              <div className="anomaly-body">
                <div className="anomaly-controls">
                  <label className="anomaly-field">
                    <span>{t('dash.anomalyMode')}</span>
                    <select
                      className="quiet"
                      value={anomalyCfg.mode || 'smart'}
                      onChange={(e) => saveAnomalyCfg({ mode: e.target.value })}
                    >
                      <option value="smart">{t('dash.anomalyModeSmart')}</option>
                      <option value="manual">{t('dash.anomalyModeManual')}</option>
                    </select>
                  </label>
                  {(anomalyCfg.mode || 'smart') === 'manual' ? (
                    <>
                      <label className="anomaly-field">
                        <span>{t('dash.anomalyAbove')}</span>
                        <input type="number" min="0" className="quiet anomaly-num" value={anomalyCfg.manualUpper ?? 0}
                          onChange={(e) => saveAnomalyCfg({ manualUpper: Number(e.target.value) })} />
                      </label>
                      <label className="anomaly-field">
                        <span>{t('dash.anomalyBelow')}</span>
                        <input type="number" min="0" className="quiet anomaly-num" value={anomalyCfg.manualLower ?? 0}
                          onChange={(e) => saveAnomalyCfg({ manualLower: Number(e.target.value) })} />
                      </label>
                    </>
                  ) : (
                    <>
                      <label className="anomaly-field">
                        <span>{t('dash.anomalySensitivity')}</span>
                        <select
                          className="quiet"
                          value={String(anomalyCfg.sensitivity)}
                          onChange={(e) => saveAnomalyCfg({ sensitivity: Number(e.target.value) })}
                        >
                          <option value="2">{t('dash.sensHigh')}</option>
                          <option value="2.5">{t('dash.sensMedium')}</option>
                          <option value="3">{t('dash.sensLow')}</option>
                        </select>
                      </label>
                      <Toggle checked={!!anomalyCfg.detectHigh} onChange={(e) => saveAnomalyCfg({ detectHigh: e.target.checked })} label={t('dash.anomalyDetectHigh')} />
                      <Toggle checked={!!anomalyCfg.detectLow} onChange={(e) => saveAnomalyCfg({ detectLow: e.target.checked })} label={t('dash.anomalyDetectLow')} />
                    </>
                  )}
                  <button type="button" className="quiet" onClick={runAnomalyScan} disabled={anomalyScanning}>
                    <span className="btn-icon"><Ico n="refresh" /> {t('dash.anomalyScan')}</span>
                  </button>
                </div>
                {anomalyFindings === null ? (
                  <p className="anomaly-hint">{t('dash.anomalyScanHint')}</p>
                ) : anomalyFindings.length === 0 ? (
                  <p className="anomaly-hint anomaly-ok">{t('dash.anomalyNone')}</p>
                ) : (
                  <ul className="anomaly-findings">
                    {anomalyFindings.map((f, i) => (
                      <li key={i} className={f.direction === 'high' ? 'is-high' : 'is-low'}>
                        <Ico n="warning" sz={14} />
                        <span className="anomaly-f-name">{f.cameraId ? (f.cameraName || t('dash.cameraN', { id: f.cameraId })) : t('dash.anomalyOverall')}</span>
                        <span className="anomaly-f-desc">
                          {f.direction === 'high'
                            ? (f.cameraId
                              ? t('dash.anomalyHighDesc', { actual: f.actual, hi: Math.round(f.hi) })
                              : t('dash.anomalyAboveDesc', { actual: f.actual, hi: Math.round(f.hi) }))
                            : (f.cameraId
                              ? t('dash.anomalyLowDesc', { actual: f.actual, median: Math.round(f.median) })
                              : t('dash.anomalyBelowDesc', { actual: f.actual, lo: Math.round(f.lo) }))}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </ChartCard>
          ) : null}

          {reliabilityRows.length > 0 ? (
            <ChartCard
              title={t('dash.reliabilityTitle')}
              subtitle={t('dash.reliabilitySub')}
              className="dashboard-span-2 reliability-card"
            >
              <div className="reliability-table-wrap">
                <table className="reliability-table">
                  <thead>
                    <tr>
                      <th>{t('dash.relCamera')}</th>
                      <th>{t('dash.relUptime')}</th>
                      <th>{t('dash.relOfflineTime')}</th>
                      <th>{t('dash.relIncidents')}</th>
                      <th>{t('dash.relStatus')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {reliabilityRows.map((r) => (
                      <tr key={r.id} className={r.down ? 'is-down' : (r.uptime < 99 ? 'is-warn' : '')}>
                        <td className="rel-name">{r.name}</td>
                        <td className="rel-uptime">{r.uptime.toFixed(1)}%</td>
                        <td>{formatDuration(r.offline)}</td>
                        <td>{r.incidents}</td>
                        <td>
                          {r.down
                            ? <span className="rel-badge rel-badge-down">{t('dash.relOff')}</span>
                            : <span className="rel-badge rel-badge-up">{t('dash.relOnline')}</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </ChartCard>
          ) : null}

          {noiseRows.length > 0 ? (
            <ChartCard title={t('dash.noiseTitle')} subtitle={t('dash.noiseSub')} className="noise-card">
              <ul className="noise-list">
                {noiseRows.map((r) => (
                  <li key={r.id}>
                    <span className="noise-name">{r.name}</span>
                    <span className="noise-count">{t('dash.noiseAlerts', { count: r.count })}</span>
                    <span className={`noise-unread${r.unreadPct >= 50 ? ' is-high' : ''}`}>{t('dash.noiseUnread', { pct: r.unreadPct })}</span>
                  </li>
                ))}
              </ul>
            </ChartCard>
          ) : null}

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
