import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { Ico, useT } from '@shared';
import { StatCard, DonutChart, BarChart, TimeSeriesChart, Heatmap, ChartCard } from '@shared/charts';
import { api } from '../lib/helpers';

// NodeDashboard mirrors the mymatasan app's own landing dashboard, but for a managed
// node: it renders the exact same KPI tiles and charts (@shared/charts) and reads the
// exact same node endpoints — only routed over the control tunnel via the commander
// proxy (/api/nodes/{id}/proxy/...) instead of same-origin. This keeps the node's
// dashboard identical whether viewed on the node itself or from the control plane.

// Label maps copied from the node app so category/source names read the same here.
const notificationSourceLabels = {
  'vision-monitor': 'AI Detection',
  'camera-health-monitor': 'Camera Health',
  'machine-health-monitor': 'Machine Health',
  'local-auth': 'Login Security',
  settings: 'Settings',
};
const notificationCategoryLabels = {
  'vision.alert': 'AI Detection',
  'health.check': 'Camera Health',
  system: 'System',
};
function cameraTitle(device) {
  return device?.name || device?.model || device?.host || 'Camera';
}

// The selectable time ranges. Each resolves to a concrete [from, to] window and a
// timeseries bucket size at fetch time (Today buckets by hour, longer ranges by day).
const RANGES = [
  { id: 'today', labelKey: 'dash.rangeToday', bucket: 'hour' },
  { id: '7d', labelKey: 'dash.range7d', bucket: 'day' },
  { id: '30d', labelKey: 'dash.range30d', bucket: 'day' },
];

// resolveWindow turns a range id into { from, to, bucket } in unix seconds, anchored
// to the viewer's local midnight so daily buckets are whole local days.
function resolveWindow(rangeId) {
  const now = Math.floor(Date.now() / 1000);
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const startOfToday = Math.floor(midnight.getTime() / 1000);
  if (rangeId === 'today') {
    return { from: startOfToday, to: now, bucket: 'hour' };
  }
  const days = rangeId === '30d' ? 30 : 7;
  return { from: startOfToday - (days - 1) * 86400, to: now, bucket: 'day' };
}

// Toggle is a small sliding on/off switch (an accessible styled checkbox).
function Toggle({ checked, disabled, onChange, label }) {
  return (
    <label className="switch-row">
      <span className="switch">
        <input type="checkbox" checked={checked} disabled={disabled} onChange={onChange} />
        <span className="switch-slider" />
      </span>
      {label ? <span className="switch-label">{label}</span> : null}
    </label>
  );
}

// InfoTip is a small muted info icon with a hover/focus tooltip explaining a control.
function InfoTip({ text }) {
  return (
    <span className="info-tip" tabIndex={0} role="img" aria-label={text} title={text}>
      <Ico n="info" sz={14} />
    </span>
  );
}

export function NodeDashboard({ node, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  const [range, setRange] = useState('7d');
  const [stats, setStats] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [heatCamera, setHeatCamera] = useState(0);
  const [heatmap, setHeatmap] = useState(null);
  const [baseline, setBaseline] = useState(null);
  const [anomalyCfg, setAnomalyCfg] = useState(null);
  const [anomalyFindings, setAnomalyFindings] = useState(null);
  const [anomalyScanning, setAnomalyScanning] = useState(false);
  const [reliability, setReliability] = useState(null);
  const [noise, setNoise] = useState(null);
  const [cams, setCams] = useState([]);

  // Hold onToast in a ref so the fetch callbacks never list it as a dependency.
  // onToast (App's pushToast) is a fresh function every render, and a fetch failure
  // calls it → App re-renders → new onToast → callbacks would rebuild → effects would
  // refetch → infinite request storm. The ref breaks that loop.
  const onToastRef = useRef(onToast);
  useEffect(() => { onToastRef.current = onToast; }, [onToast]);
  const toast = useCallback((msg) => { if (onToastRef.current) onToastRef.current(msg); }, []);
  // Keep t out of the fetch callbacks' dependency lists — listing it made fetchStats
  // rebuild on the setLoading(true) re-render, re-running its effect and hammering
  // /notifications/stats until the node rate-limited it (429 storm). tRef is always
  // current, so the error toast still localizes without destabilizing the callback.
  const tRef = useRef(t);
  tRef.current = t;

  // px routes a node-relative path over the commander proxy and returns the api()
  // result ({ ok, status, body }); body is already envelope-unwrapped to the result.
  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  // The node's saved cameras — for camera-name mapping and the heatmap camera picker.
  useEffect(() => {
    let cancelled = false;
    px('/api/cameras?limit=200').then((r) => {
      if (!cancelled && r.ok) setCams(Array.isArray(r.body) ? r.body : (r.body?.items || []));
    });
    return () => { cancelled = true; };
  }, [px]);

  const fetchStats = useCallback(async () => {
    setLoading(true);
    setError(false);
    const { from, to, bucket } = resolveWindow(range);
    const tzOffset = -new Date().getTimezoneOffset();
    const params = new URLSearchParams({ from: String(from), to: String(to), bucket, tzOffset: String(tzOffset) });
    const r = await px(`/api/notifications/stats?${params}`);
    setLoading(false);
    if (r.ok) setStats(r.body);
    else { setError(true); toast(tRef.current('dash.loadFailed')); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range, px]);

  const fetchHeatmap = useCallback(async () => {
    const to = Math.floor(Date.now() / 1000);
    const from = to - 28 * 86400;
    const tzOffset = -new Date().getTimezoneOffset();
    const params = new URLSearchParams({ from: String(from), to: String(to), tzOffset: String(tzOffset) });
    if (heatCamera > 0) params.set('cameraId', String(heatCamera));
    const r = await px(`/api/notifications/heatmap?${params}`);
    setHeatmap(r.ok ? r.body : null);
  }, [heatCamera, px]);

  const fetchBaseline = useCallback(async () => {
    const { from, to, bucket } = resolveWindow(range);
    const tzOffset = -new Date().getTimezoneOffset();
    const params = new URLSearchParams({ from: String(from), to: String(to), bucket, tzOffset: String(tzOffset) });
    const r = await px(`/api/notifications/baseline?${params}`);
    setBaseline(r.ok ? r.body : null);
  }, [range, px]);

  const fetchAnomalyCfg = useCallback(async () => {
    const r = await px('/api/anomaly/settings');
    setAnomalyCfg(r.ok ? r.body : null);
  }, [px]);

  const fetchReliability = useCallback(async () => {
    const to = Math.floor(Date.now() / 1000);
    const from = to - 7 * 86400;
    const r = await px(`/api/notifications/reliability?from=${from}&to=${to}`);
    setReliability(r.ok ? r.body : null);
  }, [px]);

  const fetchNoise = useCallback(async () => {
    const to = Math.floor(Date.now() / 1000);
    const from = to - 7 * 86400;
    const r = await px(`/api/notifications/noise?from=${from}&to=${to}`);
    setNoise(r.ok ? r.body : null);
  }, [px]);

  useEffect(() => { fetchStats(); }, [fetchStats]);
  useEffect(() => { fetchHeatmap(); }, [fetchHeatmap]);
  useEffect(() => { fetchBaseline(); }, [fetchBaseline]);
  useEffect(() => { fetchAnomalyCfg(); }, [fetchAnomalyCfg]);
  useEffect(() => { fetchReliability(); }, [fetchReliability]);
  useEffect(() => { fetchNoise(); }, [fetchNoise]);

  const refreshAll = useCallback(() => {
    fetchStats(); fetchHeatmap(); fetchBaseline(); fetchReliability(); fetchNoise();
  }, [fetchStats, fetchHeatmap, fetchBaseline, fetchReliability, fetchNoise]);

  // Persist a partial change to the anomaly config (optimistic; re-syncs from the
  // normalized server response). Writes ride the tunnel as the operator's role.
  const saveAnomalyCfg = useCallback(async (patch) => {
    if (!anomalyCfg) return;
    const next = { ...anomalyCfg, ...patch };
    setAnomalyCfg(next);
    const r = await px('/api/anomaly/settings', { method: 'PUT', body: JSON.stringify(next) });
    if (r.ok) setAnomalyCfg(r.body || next);
    else toast(t('dash.anomalySaveFailed'));
  }, [anomalyCfg, px, toast, t]);

  const runAnomalyScan = useCallback(async () => {
    setAnomalyScanning(true);
    const tzOffset = -new Date().getTimezoneOffset();
    const r = await px(`/api/anomaly/scan?tzOffset=${tzOffset}`);
    setAnomalyScanning(false);
    if (r.ok) setAnomalyFindings(r.body?.findings || []);
    else { setAnomalyFindings([]); toast(t('dash.anomalyScanFailed')); }
  }, [px, toast, t]);

  const cameraNames = useMemo(() => {
    const map = new Map();
    (cams || []).forEach((c) => map.set(Number(c.id), cameraTitle(c)));
    return map;
  }, [cams]);

  const bucket = stats?.bucket || 'day';
  const formatX = useCallback((unixSec) => {
    const d = new Date(unixSec * 1000);
    if (bucket === 'hour') return `${String(d.getHours()).padStart(2, '0')}:00`;
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }, [bucket]);

  const categoryLabel = useCallback((key) => notificationCategoryLabels[key] || key, []);
  const sourceLabel = useCallback((key) => notificationSourceLabels[key] || key, []);
  const severityLabel = useCallback((key) => t(`dash.sev.${key}`, {}) || key, [t]);
  const cameraLabel = useCallback((id) => cameraNames.get(Number(id)) || t('dash.cameraN', { id }), [cameraNames, t]);
  const capitalize = useCallback((s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s), []);

  const formatDuration = useCallback((seconds) => {
    if (!seconds || seconds <= 0) return '—';
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds % 3600) / 60);
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }, []);

  const reliabilityRows = useMemo(() => {
    const byId = new Map((reliability?.cameras || []).map((c) => [Number(c.cameraId), c]));
    const ids = (cams && cams.length) ? cams.map((c) => Number(c.id)) : [...byId.keys()];
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
  }, [reliability, cams, cameraNames, t]);

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

  const seriesCategories = useMemo(() => {
    const known = ['vision.alert', 'health.check', 'system'];
    const present = new Set();
    (stats?.timeseries || []).forEach((b) => Object.keys(b.byCategory || {}).forEach((k) => present.add(k)));
    (stats?.byCategory || []).forEach((c) => present.add(c.key));
    const ordered = known.filter((k) => present.has(k));
    present.forEach((k) => { if (!ordered.includes(k)) ordered.push(k); });
    return ordered;
  }, [stats]);

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
          <h2 className="section-title">{node.name || nodeId}</h2>
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
          <button type="button" className="quiet" onClick={refreshAll} disabled={loading}>
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
                {(cams || []).map((c) => (
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
              title={<>{t('dash.anomalyTitle')} <InfoTip text={t('dash.anomalyInfo')} /></>}
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
