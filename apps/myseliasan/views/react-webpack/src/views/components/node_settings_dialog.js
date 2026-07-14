import { useCallback, useEffect, useState } from 'react';
import { Ico, Tabs, useT } from '@shared';
import { NodeEmbed } from './node_embed';
import { NodeDiscoverPanel } from './node_discover';
import { IconDropdown } from './ui';
import { NODE_ICONS, DEFAULT_NODE_ICON } from './nodes';
import { isSensorNode } from './layout';
import { api } from '../lib/helpers';

// NodeSettingsDialog is the node-level "Settings" surface, opened from the Manage button in
// the Adopted Nodes table. It mirrors mymatasan's Settings page as a modal with tabs, but
// every read/write tunnels to the node over the control-channel proxy (the browser never
// contacts the node directly). Tabs: Details, Cameras (discover/add), Camera Health, Users,
// Backup & Recovery, Version & Health.
const TABS = [
  { id: 'details', labelKey: 'nset.tabDetails', icon: 'sliders' },
  { id: 'cameras', labelKey: 'nset.tabCameras', icon: 'video' },
  { id: 'health', labelKey: 'nset.tabHealth', icon: 'wifi' },
  { id: 'users', labelKey: 'nset.tabUsers', icon: 'user' },
  { id: 'backup', labelKey: 'nset.tabBackup', icon: 'key' },
  { id: 'system', labelKey: 'nset.tabSystem', icon: 'monitor' },
];

// A SENSOR HUB IS NOT AN NVR, and this dialog is the NVR's settings page. Every tab on it but
// Details reads a mymatasan endpoint — camera discovery, CAMERA health, its user store, its
// backup archive, its recorder build. A myiotsan hub serves none of them, so offering the tabs
// would be the exact failure the node `kind` exists to prevent: a fleet UI cheerfully proposing
// to check the camera health of a door contact, and then showing six "unavailable on this node"
// panels when it is taken up on it.
//
// So a hub gets the one tab that is REALLY about the node as myseliasan knows it (its name, its
// icon, its address). Its devices, rules, alerts and provisioning are not missing — they live on
// its own manage surface, the nodeiot embed, which is the sensor equivalent of everything else
// here. Wipe and Release are on the Adopted Nodes row itself and are unaffected.
function tabsFor(node) {
  return isSensorNode(node) ? TABS.filter((tb) => tb.id === 'details') : TABS;
}

export function NodeSettingsDialog({ node, onClose, onToast, onWipe, onNodeUpdated }) {
  const t = useT();
  const [tab, setTab] = useState('details');
  const nodeId = node.nodeId;
  const tabs = tabsFor(node);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card node-settings-dialog" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        {/* NodeEmbed injects mymatasan's (scoped) stylesheet so the dialog's typography and
            settings panels look exactly like the node's own Settings page. */}
        <NodeEmbed className="node-settings-embed">
          <header className="node-settings-head">
            <h2><span className="btn-icon"><Ico n={node.icon || 'monitor'} /> {t('nset.title', { name: node.name || nodeId })}</span></h2>
            <button type="button" className="icon-button" onClick={onClose} aria-label={t('nset.close')}><Ico n="x" sz={16} /></button>
          </header>
          <Tabs
            className="node-settings-tabs"
            active={tab}
            onChange={setTab}
            tabs={tabs.map((tb) => ({ id: tb.id, label: t(tb.labelKey), icon: tb.icon }))}
          />
          <div className="node-settings-body">
            {tab === 'details' ? <NodeDetailsPanel node={node} t={t} onToast={onToast} onNodeUpdated={onNodeUpdated} /> : null}
            {tab === 'cameras' ? <NodeDiscoverPanel px={px} t={t} onToast={onToast} /> : null}
            {tab === 'system' ? <VersionHealthPanel node={node} px={px} t={t} onToast={onToast} /> : null}
            {tab === 'health' ? <CameraHealthPanel px={px} t={t} onToast={onToast} /> : null}
            {tab === 'users' ? <UsersPanel px={px} t={t} onToast={onToast} /> : null}
            {tab === 'backup' ? <BackupPanel node={node} px={px} t={t} onToast={onToast} onWipe={onWipe} /> : null}
          </div>
        </NodeEmbed>
      </div>
    </div>
  );
}

// nodeAddressLink renders the node's own web address as a clickable link (opens the node's
// UI directly on the LAN in a new tab). Falls back to plain text / a dash when there's no
// usable address. This is a deliberate direct-to-node action, separate from the tunnel.
function nodeAddressLink(node) {
  let url = (node.baseUrl || '').trim();
  if (!url && node.ip) url = `https://${node.ip}${node.httpsPort ? `:${node.httpsPort}` : ''}`;
  if (!url) return '—';
  const href = /^https?:\/\//i.test(url) ? url : `https://${url}`;
  return <a href={href} target="_blank" rel="noreferrer">{url}</a>;
}

// NodeDetailsPanel edits the node's operator-facing fields on the control plane (display
// name, description, nav icon) via PUT /api/nodes/{id}. This is a myseliasan-side record —
// NOT tunneled — so it's how you rename/re-icon a node after adoption.
function NodeDetailsPanel({ node, t, onToast, onNodeUpdated }) {
  const [draft, setDraft] = useState({ name: node.name || '', description: node.description || '', icon: node.icon || DEFAULT_NODE_ICON });
  const [busy, setBusy] = useState(false);
  const set = (patch) => setDraft((d) => ({ ...d, ...patch }));
  const changed = draft.name !== (node.name || '') || draft.description !== (node.description || '') || draft.icon !== (node.icon || DEFAULT_NODE_ICON);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}`, {
      method: 'PUT',
      noRedirect: true,
      body: JSON.stringify({ name: draft.name.trim(), description: draft.description.trim(), icon: draft.icon }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('nset.detailsSaved')); if (onNodeUpdated) onNodeUpdated(r.body || { ...node, ...draft }); }
    else if (onToast) onToast(r.message || t('nset.saveFailed'));
  }

  return (
    <form className="settings-layout" onSubmit={save}>
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="sliders" /> {t('nset.detailsTitle')}</span></h2></header>
        <p className="settings-hint">{t('nset.detailsHint')}</p>
        <div className="settings-field-grid">
          <label>{t('nset.nodeName')}<input value={draft.name} onChange={(e) => set({ name: e.target.value })} placeholder={node.nodeId} disabled={busy} /></label>
          <label>{t('nset.nodeIcon')}<IconDropdown value={draft.icon} options={NODE_ICONS} onChange={(name) => set({ icon: name })} disabled={busy} /></label>
          <label className="field-span-two">{t('nset.nodeDescription')}<textarea value={draft.description} onChange={(e) => set({ description: e.target.value })} rows={2} disabled={busy} /></label>
          <div className="node-info-grid field-span-two">
            <div><dt>{t('nset.nodeIdLabel')}</dt><dd>{node.nodeId}</dd></div>
            <div><dt>{t('nset.address')}</dt><dd>{nodeAddressLink(node)}</dd></div>
          </div>
        </div>
        <div className="settings-actions">
          <button type="submit" disabled={busy || !changed}><span className="btn-icon"><Ico n="check-ok" /> {t('nset.save')}</span></button>
        </div>
      </section>
    </form>
  );
}

// Default host-health thresholds (mirror the node's DefaultMachineHealthSettings); used
// until the node's real thresholds load, so the colour bands are sensible from the start.
const MH_THRESHOLD_DEFAULTS = {
  cpu: { warnPercent: 85, criticalPercent: 95 },
  memory: { warnPercent: 85, criticalPercent: 95 },
  disk: { warnPercent: 80, criticalPercent: 90 },
};

// levelClass colours a metric pill: green (online) when healthy, yellow (hh-warn) once it
// crosses the warn threshold, red (offline) at critical — matching mymatasan's machine cards.
const levelClass = (pct, warn, crit) => {
  const p = Number(pct) || 0;
  if (p >= crit) return 'offline';
  if (p >= warn) return 'hh-warn';
  return 'online';
};

const fmtBytes = (n) => {
  const v = Number(n || 0);
  if (!v) return '—';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0; let x = v;
  while (x >= 1024 && i < u.length - 1) { x /= 1024; i += 1; }
  return `${x.toFixed(i ? 1 : 0)} ${u[i]}`;
};

// VersionHealthPanel mirrors mymatasan's Version & Health tab: running software version,
// software updates (check/apply), live service health (API/liveness/readiness), plus the
// node's ffmpeg and host CPU/mem/disk. Every probe tunnels over the proxy.
function VersionHealthPanel({ node, px, t, onToast }) {
  const [version, setVersion] = useState(undefined);
  const [decoder, setDecoder] = useState(null);
  const [update, setUpdate] = useState(undefined);
  const [health, setHealth] = useState(null);
  const [ready, setReady] = useState(null);
  const [metrics, setMetrics] = useState(null);
  const [thresholds, setThresholds] = useState(MH_THRESHOLD_DEFAULTS);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [checkedAt, setCheckedAt] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    // Only /api/* is reachable over the control tunnel, so readiness comes from /api/ready
    // (the node mirrors the root /ready probe there) — /health & /ready aren't tunneled.
    const [v, d, u, h, r, m, s] = await Promise.all([
      px('/api/version'), px('/api/settings/decoder/status'), px('/api/system/update'),
      px('/api/health'), px('/api/ready'), px('/api/settings/machine-health/metrics'),
      px('/api/settings/machine-health'),
    ]);
    setVersion(v.ok ? v.body : null);
    setDecoder(d.ok ? d.body : null);
    setUpdate(u.ok ? u.body : null);
    setHealth(h); setReady(r);
    setMetrics(m.ok ? m.body : null);
    if (s.ok && s.body) setThresholds({
      cpu: { ...MH_THRESHOLD_DEFAULTS.cpu, ...(s.body.cpu || {}) },
      memory: { ...MH_THRESHOLD_DEFAULTS.memory, ...(s.body.memory || {}) },
      disk: { ...MH_THRESHOLD_DEFAULTS.disk, ...(s.body.disk || {}) },
    });
    setCheckedAt(new Date());
    setLoading(false);
  }, [px]);
  useEffect(() => { load(); }, [load]);

  async function checkUpdate() {
    setChecking(true);
    const r = await px('/api/system/update/check', { method: 'POST' });
    setChecking(false);
    if (r.ok) setUpdate(r.body);
  }
  async function applyUpdate() {
    setApplying(true);
    const r = await px('/api/system/update/apply', { method: 'POST' });
    setApplying(false);
    if (onToast) onToast(r.ok ? t('nset.updateStarted') : t('nset.saveFailed'));
  }

  const disks = Array.isArray(metrics?.disks) ? metrics.disks : [];
  const ffmpegOk = decoder?.available || decoder?.found;
  const pill = (ok, label) => <strong className={`status-pill ${ok ? 'online' : 'offline'}`}>{label}</strong>;
  // The node's HTTP is alive if /api/health answered (the tunnel reached its router).
  const alive = !!health?.ok && health?.body?.ok !== false;
  const readyOk = !!ready?.ok && ready?.body?.ok === true;
  const readyExtras = ready?.ok && ready?.body && typeof ready.body === 'object'
    ? Object.entries(ready.body).filter(([k]) => k !== 'ok') : [];
  const COMP_LABELS = { db: 'nset.compDb', cache: 'nset.compCache', machine: 'nset.compMachine', cameras: 'nset.compCameras' };
  const compLabel = (k) => (COMP_LABELS[k] ? t(COMP_LABELS[k]) : k.charAt(0).toUpperCase() + k.slice(1));
  const compOk = (val) => { const s = String(val).toLowerCase(); return s === 'up' || s === 'ok' || s === 'ready'; };

  return (
    <div className="settings-layout">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> {t('nset.softwareVersion')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={loading}>
              <span className="btn-icon"><Ico n="reload" /> {t('nset.refresh')}</span>
            </button>
          </div>
        </header>
        {version ? (
          <div className="machine-metrics">
            <div className="machine-metric-card"><dt>{t('nset.application')}</dt><dd><strong className="status-pill">{version.app || node.name || '—'}</strong></dd><span className="field-hint">v{version.appVersion || node.version || '—'}</span></div>
            <div className="machine-metric-card"><dt>{t('nset.sharedCore')}</dt><dd><strong className="status-pill">v{version.coreVersion || '—'}</strong></dd></div>
            {version.commit ? <div className="machine-metric-card"><dt>{t('nset.commit')}</dt><dd><strong className="status-pill">{String(version.commit).slice(0, 12)}</strong></dd></div> : null}
            {version.updatedAt ? <div className="machine-metric-card"><dt>{t('nset.built')}</dt><dd><strong className="status-pill">{version.updatedAt}</strong></dd></div> : null}
            <div className="machine-metric-card"><dt>{t('nset.ffmpeg')}</dt><dd>{decoder ? (ffmpegOk ? <span className="status-pill online">{decoder.version || t('nset.installed')}</span> : <span className="status-pill offline">{t('nset.missing')}</span>) : '—'}</dd></div>
          </div>
        ) : (
          <p className="settings-hint">{loading ? t('nset.loading') : `${t('nset.nodeVersion')}: v${node.version || '—'}`}</p>
        )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="download" /> {t('nset.updates')}</span></h2>
          <button type="button" className="quiet" onClick={checkUpdate} disabled={checking || applying}>
            <span className="btn-icon"><Ico n="reload" /> {checking ? t('nset.checking') : t('nset.checkUpdates')}</span>
          </button>
        </header>
        {update === undefined ? <p className="settings-hint">{t('nset.loading')}</p>
          : !update ? <p className="settings-hint">{t('nset.updUnavailable')}</p>
          : update.updateAvailable ? (
            <>
              <p className="update-available">{pill(true, t('nset.updAvailable', { version: update.latest }))} <span className="field-hint"> {t('nset.updCurrent', { version: update.current })}</span></p>
              {update.canSelfUpdate ? (
                <div className="settings-actions">
                  <button type="button" onClick={applyUpdate} disabled={applying}><span className="btn-icon"><Ico n="download" /> {applying ? t('nset.applying') : t('nset.applyUpdate', { version: update.latest })}</span></button>
                </div>
              ) : (
                <p className="settings-hint">{update.htmlUrl ? <a href={update.htmlUrl} target="_blank" rel="noreferrer">{t('nset.releaseNotes')}</a> : t('nset.updManual')}</p>
              )}
            </>
          ) : (
            <p className="settings-hint">{pill(true, t('nset.upToDate'))} <span className="field-hint"> v{update.current}</span></p>
          )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> {t('nset.serviceHealth')}</span></h2>
          {checkedAt ? <span className="field-hint">{t('nset.checkedAt', { time: checkedAt.toLocaleTimeString() })}</span> : null}
        </header>
        <div className="machine-metrics">
          <div className="machine-metric-card"><dt>{t('nset.apiNamespaces')}</dt><dd>{pill(alive, alive ? t('nset.ok') : t('nset.down'))}</dd><span className="field-hint">/api/health</span></div>
          <div className="machine-metric-card"><dt>{t('nset.liveness')}</dt><dd>{pill(alive, alive ? t('nset.alive') : t('nset.down'))}</dd></div>
          <div className="machine-metric-card"><dt>{t('nset.readiness')}</dt><dd>{pill(readyOk, readyOk ? t('nset.ready') : t('nset.notReady'))}</dd><span className="field-hint">/api/ready</span></div>
          {readyExtras.map(([k, val]) => (
            <div className="machine-metric-card" key={k}><dt>{compLabel(k)}</dt><dd>{pill(compOk(val), String(val))}</dd></div>
          ))}
        </div>
      </section>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="cpu" /> {t('nset.hostHealth')}</span></h2></header>
        {loading && !metrics ? <p className="settings-hint">{t('nset.loading')}</p> : null}
        {!loading && !metrics ? <p className="settings-hint danger-text">{t('nset.metricsUnavailable')}</p> : null}
        {metrics ? (
          <div className="machine-metrics">
            <div className="machine-metric-card"><dt>{t('nset.cpu')}</dt><dd><strong className={`status-pill ${levelClass(metrics.cpuPercent, thresholds.cpu.warnPercent, thresholds.cpu.criticalPercent)}`}>{metrics.cpuPercent ?? '—'}%</strong></dd></div>
            <div className="machine-metric-card"><dt>{t('nset.memory')}</dt><dd><strong className={`status-pill ${levelClass(metrics.memoryPercent, thresholds.memory.warnPercent, thresholds.memory.criticalPercent)}`}>{metrics.memoryPercent ?? '—'}%</strong></dd><span className="field-hint">{fmtBytes(metrics.memoryUsedBytes)} / {fmtBytes(metrics.memoryTotalBytes)}</span></div>
            {disks.map((d) => (
              <div className="machine-metric-card" key={d.mountpoint}><dt>{t('nset.disk', { mount: d.mountpoint })}</dt><dd><strong className={`status-pill ${levelClass(d.usedPercent, thresholds.disk.warnPercent, thresholds.disk.criticalPercent)}`}>{d.usedPercent}%</strong></dd><span className="field-hint">{fmtBytes(d.freeBytes)} {t('nset.free')} / {fmtBytes(d.totalBytes)}</span></div>
            ))}
          </div>
        ) : null}
      </section>
    </div>
  );
}

// CameraHealthPanel: the node's camera-health monitor settings (GET/PUT /api/settings/health).
const HEALTH_DEFAULTS = { enabled: true, intervalMs: 30000, timeoutMs: 5000, failureThreshold: 3, recoveryThreshold: 2 };
function CameraHealthPanel({ px, t, onToast }) {
  const [val, setVal] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const r = await px('/api/settings/health');
    setVal(r.ok ? { ...HEALTH_DEFAULTS, ...(r.body || {}) } : { ...HEALTH_DEFAULTS });
  }, [px]);
  useEffect(() => { load(); }, [load]);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await px('/api/settings/health', { method: 'PUT', body: JSON.stringify(val) });
    setBusy(false);
    if (onToast) onToast(r.ok ? t('nset.saved') : t('nset.saveFailed'));
  }
  if (!val) return <p className="settings-hint">{t('nset.loading')}</p>;
  const num = (k, factor = 1) => ({ value: Math.round((val[k] || 0) / factor), onChange: (e) => setVal({ ...val, [k]: Math.max(0, Number(e.target.value) || 0) * factor }) });

  return (
    <form className="settings-layout" onSubmit={save}>
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> {t('nset.cameraHealthTitle')}</span></h2>
          <label className="check-row">
            <input type="checkbox" checked={!!val.enabled} onChange={(e) => setVal({ ...val, enabled: e.target.checked })} /> {t('nset.enabled')}
          </label>
        </header>
        <p className="settings-hint">{t('nset.cameraHealthHint')}</p>
        <div className="settings-field-grid">
          <label>{t('nset.checkInterval')}<input type="number" min="5" {...num('intervalMs', 1000)} disabled={busy} /></label>
          <label>{t('nset.timeout')}<input type="number" min="1" {...num('timeoutMs', 1000)} disabled={busy} /></label>
          <label>{t('nset.failureThreshold')}<input type="number" min="1" {...num('failureThreshold')} disabled={busy} /></label>
          <label>{t('nset.recoveryThreshold')}<input type="number" min="1" {...num('recoveryThreshold')} disabled={busy} /></label>
        </div>
        <div className="settings-actions">
          <button type="submit" disabled={busy}><span className="btn-icon"><Ico n="check-ok" /> {t('nset.save')}</span></button>
        </div>
      </section>
    </form>
  );
}

// UsersPanel: list + add/delete/toggle-admin/reset-password of the node's local users.
function UsersPanel({ px, t, onToast }) {
  const [users, setUsers] = useState(null);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ username: '', displayName: '', password: '', isAdmin: false });

  const load = useCallback(async () => {
    const r = await px('/api/settings/users?limit=200');
    setUsers(r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);
  }, [px]);
  useEffect(() => { load(); }, [load]);

  async function add(e) {
    e.preventDefault();
    if (!form.username.trim() || !form.password) { if (onToast) onToast(t('nset.userNeedFields')); return; }
    setBusy(true);
    const r = await px('/api/settings/users', { method: 'POST', body: JSON.stringify({ username: form.username.trim(), displayName: form.displayName.trim(), password: form.password, isAdmin: form.isAdmin, isActive: true, mustChangePassword: false }) });
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('nset.userCreated')); setForm({ username: '', displayName: '', password: '', isAdmin: false }); load(); }
    else if (onToast) onToast(r.message || t('nset.userCreateFailed'));
  }

  async function toggleAdmin(u) {
    setBusy(true);
    const r = await px(`/api/settings/users/${u.id}`, { method: 'PUT', body: JSON.stringify({ isAdmin: !u.isAdmin }) });
    setBusy(false);
    if (r.ok) load(); else if (onToast) onToast(r.message || t('nset.saveFailed'));
  }

  async function resetPassword(u) {
    const pw = window.prompt(t('nset.newPasswordFor', { name: u.username }));
    if (!pw) return;
    setBusy(true);
    const r = await px(`/api/settings/users/${u.id}/password`, { method: 'POST', body: JSON.stringify({ password: pw }) });
    setBusy(false);
    if (onToast) onToast(r.ok ? t('nset.passwordReset') : (r.message || t('nset.saveFailed')));
  }

  async function remove(u) {
    if (!window.confirm(t('nset.confirmDeleteUser', { name: u.username }))) return;
    setBusy(true);
    const r = await px(`/api/settings/users/${u.id}`, { method: 'DELETE' });
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('nset.userDeleted')); load(); }
    else if (onToast) onToast(r.message || t('nset.saveFailed'));
  }

  return (
    <div className="settings-layout">
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="user" /> {t('nset.usersTitle')}</span></h2></header>
        {users === null ? <p className="settings-hint">{t('nset.loading')}</p> : users.length === 0 ? <p className="settings-hint">{t('nset.noUsers')}</p> : (
          <table className="event-table">
            <thead><tr><th>{t('nset.user')}</th><th>{t('nset.role')}</th><th></th></tr></thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id || u.username}>
                  <td>{u.displayName || u.username}<div className="field-hint">{u.username}</div></td>
                  <td>{u.isAdmin ? <span className="status-pill online">{t('nset.admin')}</span> : <span className="status-pill">{t('nset.viewer')}</span>}</td>
                  <td className="node-row-actions">
                    <button type="button" className="quiet" onClick={() => toggleAdmin(u)} disabled={busy}>{u.isAdmin ? t('nset.makeViewer') : t('nset.makeAdmin')}</button>
                    <button type="button" className="quiet" onClick={() => resetPassword(u)} disabled={busy}>{t('nset.resetPassword')}</button>
                    <button type="button" className="quiet danger-text" onClick={() => remove(u)} disabled={busy}>{t('nset.delete')}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="user-plus" /> {t('nset.addUser')}</span></h2></header>
        <form className="settings-field-grid" onSubmit={add}>
          <label>{t('nset.username')}<input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} disabled={busy} /></label>
          <label>{t('nset.displayName')}<input value={form.displayName} onChange={(e) => setForm({ ...form, displayName: e.target.value })} disabled={busy} /></label>
          <label>{t('nset.password')}<input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} disabled={busy} /></label>
          <label className="check-row"><input type="checkbox" checked={form.isAdmin} onChange={(e) => setForm({ ...form, isAdmin: e.target.checked })} /> {t('nset.admin')}</label>
          <div className="settings-actions field-span-two">
            <button type="submit" disabled={busy}><span className="btn-icon"><Ico n="check-ok" /> {t('nset.createUser')}</span></button>
          </div>
        </form>
      </section>
    </div>
  );
}

// BackupPanel: recovery-key state + the factory-reset "Danger Zone" (delegates to the same
// countdown wipe the table uses). Full .mmbackup file transfer is done on the node itself —
// it can exceed the control-channel frame cap, so it isn't proxied here.
function BackupPanel({ node, px, t, onToast, onWipe }) {
  const [recovery, setRecovery] = useState(null);

  const load = useCallback(async () => {
    const r = await px('/api/system/recovery/state');
    setRecovery(r.ok ? r.body : null);
  }, [px]);
  useEffect(() => { load(); }, [load]);

  return (
    <div className="settings-layout">
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="key" /> {t('nset.recoveryTitle')}</span></h2></header>
        <p className="settings-hint">{t('nset.recoveryHint')}</p>
        <dl className="node-info-grid">
          <div><dt>{t('nset.recoveryKey')}</dt><dd>{recovery ? (recovery.configured || recovery.exists ? <span className="status-pill online">{t('nset.configured')}</span> : <span className="status-pill">{t('nset.notConfigured')}</span>) : '—'}</dd></div>
        </dl>
        <p className="settings-hint">{t('nset.backupOnNode')}</p>
      </section>

      <section className="settings-panel span-two danger-zone">
        <header><h2><span className="btn-icon"><Ico n="warning" /> {t('nset.dangerZone')}</span></h2></header>
        <p className="settings-hint">{t('nset.dangerHint')}</p>
        <button type="button" className="danger-solid" onClick={() => onWipe && onWipe(node)}>
          <span className="btn-icon"><Ico n="warning" /> {t('nset.factoryReset')}</span>
        </button>
      </section>
    </div>
  );
}
