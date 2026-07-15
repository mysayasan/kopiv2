import { useCallback, useEffect, useState } from 'react';
import { Ico, PasswordField, Tabs, useT } from '@shared';
import { api, errorMessage } from '../lib/helpers';
import { ConfirmModal, CopyButton, Field, Panel } from './ui';

// The Settings page — one tabbed home for everything that configures the hub itself, rather than the
// devices it watches. The whole page is admin-only (the nav entry is hidden from non-admins, and
// every route below is administrator-gated server-side). Tabs, left to right:
//   users        — local accounts and their roles
//   location     — the site latitude/longitude sunrise/sunset schedules need
//   notifications — where alerts are delivered off-box (webhook / telegram)
//   telemetry    — storage retention and the embedded broker (restart-to-apply)
//   connectivity — fleet pairing (adoption into a myseliasan control plane)
//   system       — version, health, and a restart

const SEVERITIES = ['info', 'warning', 'critical'];

const SETTINGS_TABS = [
  { id: 'users', icon: 'user' },
  { id: 'location', icon: 'map-pin' },
  { id: 'notifications', icon: 'bell' },
  { id: 'telemetry', icon: 'sliders' },
  { id: 'connectivity', icon: 'shield' },
  { id: 'system', icon: 'monitor' },
];

export function SettingsPage({ onToast }) {
  const t = useT();
  const [nav, setNav] = useState('users');
  const tabs = SETTINGS_TABS.map((tb) => ({ id: tb.id, label: t(`st.nav.${tb.id}`), icon: tb.icon }));

  return (
    <section className="workspace">
      <Panel icon="sliders" title={t('page.settings')} hint={t(`st.tabHint.${nav}`)}>
        <Tabs tabs={tabs} active={nav} onChange={setNav} ariaLabel={t('page.settings')} />
        {nav === 'users' ? <UsersPanel onToast={onToast} /> : null}
        {nav === 'location' ? <LocationPanel onToast={onToast} /> : null}
        {nav === 'notifications' ? <NotificationsPanel onToast={onToast} /> : null}
        {nav === 'telemetry' ? <TelemetryPanel onToast={onToast} onGoSystem={() => setNav('system')} /> : null}
        {nav === 'connectivity' ? <ConnectivityPanel onToast={onToast} /> : null}
        {nav === 'system' ? <SystemPanel onToast={onToast} /> : null}
      </Panel>
    </section>
  );
}

// --- Users & Roles --------------------------------------------------------------------------

const EMPTY_USER = { username: '', displayName: '', password: '', roleId: 0 };

function UsersPanel({ onToast }) {
  const t = useT();
  const [users, setUsers] = useState([]);
  const [roles, setRoles] = useState([]);
  const [busy, setBusy] = useState(true);
  const [newUser, setNewUser] = useState(EMPTY_USER);
  const [pwDrafts, setPwDrafts] = useState({});
  const [confirmDel, setConfirmDel] = useState(null);

  const load = useCallback(async () => {
    setBusy(true);
    const [u, r] = await Promise.all([api('/api/settings/users'), api('/api/settings/roles')]);
    if (u.ok) setUsers(u.body?.items || []);
    if (r.ok) setRoles((Array.isArray(r.body) ? r.body : r.body?.items) || []);
    setBusy(false);
  }, []);
  useEffect(() => { load(); }, [load]);

  const roleOf = (id) => roles.find((x) => x.id === id);
  const setEdit = (id, patch) => setUsers((list) => list.map((u) => (u.id === id ? { ...u, ...patch } : u)));

  async function create(e) {
    e.preventDefault();
    if (!newUser.roleId) { onToast(t('st.pickRole'), 'error'); return; }
    const body = {
      username: newUser.username, password: newUser.password, displayName: newUser.displayName,
      roleId: Number(newUser.roleId), isAdmin: !!roleOf(Number(newUser.roleId))?.isSuperadmin,
      isActive: true, mustChangePassword: true,
    };
    const r = await api('/api/settings/users', { method: 'POST', body: JSON.stringify(body) });
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    onToast(t('st.userCreated', { name: newUser.username }), 'success');
    setNewUser(EMPTY_USER); load();
  }

  async function save(u) {
    const body = { username: u.username, displayName: u.displayName || '', roleId: Number(u.roleId), isAdmin: !!roleOf(Number(u.roleId))?.isSuperadmin, isActive: !!u.isActive };
    const r = await api(`/api/settings/users/${u.id}`, { method: 'PUT', body: JSON.stringify(body) });
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    onToast(t('st.userSaved', { name: u.username }), 'success'); load();
  }

  async function resetPw(u) {
    const password = (pwDrafts[u.id] || '').trim();
    if (!password) return;
    const r = await api(`/api/settings/users/${u.id}/password`, { method: 'POST', body: JSON.stringify({ password }) });
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    setPwDrafts((d) => ({ ...d, [u.id]: '' }));
    onToast(t('st.pwReset', { name: u.username }), 'success');
  }

  async function remove(u) {
    setConfirmDel(null);
    const r = await api(`/api/settings/users/${u.id}`, { method: 'DELETE' });
    if (!r.ok) { onToast(errorMessage(r, t('st.deleteFailed')), 'error'); return; }
    onToast(t('st.userDeleted', { name: u.username }), 'success'); load();
  }

  const RoleSelect = ({ value, onChange }) => (
    <select value={value || ''} onChange={(e) => onChange(Number(e.target.value))}>
      <option value="">{t('st.pickRole')}</option>
      {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
    </select>
  );

  return (
    <div className="settings-content">
      <form onSubmit={create} className="iot-form">
        <h3 className="iot-subhead">{t('st.addUser')}</h3>
        <div className="form-grid">
          <Field label={t('st.username')}><input value={newUser.username} required autoComplete="off" onChange={(e) => setNewUser({ ...newUser, username: e.target.value })} /></Field>
          <Field label={t('st.displayName')}><input value={newUser.displayName} autoComplete="off" onChange={(e) => setNewUser({ ...newUser, displayName: e.target.value })} /></Field>
          <Field label={t('st.password')}><PasswordField value={newUser.password} onChange={(p) => setNewUser({ ...newUser, password: p })} autoComplete="new-password" /></Field>
          <Field label={t('st.role')}><RoleSelect value={newUser.roleId} onChange={(roleId) => setNewUser({ ...newUser, roleId })} /></Field>
        </div>
        <div className="modal-actions">
          <button type="submit" disabled={busy}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('st.addUser')}</span></button>
        </div>
      </form>

      <h3 className="iot-subhead">{t('st.users')}</h3>
      {users.length === 0 ? <p className="field-hint">{t('st.noUsers')}</p> : null}
      {users.map((u) => (
        <fieldset className="iot-action-row" key={u.id}>
          <Field label={t('st.username')}><input value={u.username || ''} onChange={(e) => setEdit(u.id, { username: e.target.value })} /></Field>
          <Field label={t('st.displayName')}><input value={u.displayName || ''} onChange={(e) => setEdit(u.id, { displayName: e.target.value })} /></Field>
          <Field label={t('st.role')}><RoleSelect value={u.roleId} onChange={(roleId) => setEdit(u.id, { roleId })} /></Field>
          <Field label={t('st.newPassword')}><PasswordField value={pwDrafts[u.id] || ''} onChange={(p) => setPwDrafts((d) => ({ ...d, [u.id]: p }))} autoComplete="new-password" placeholder={t('st.leaveBlankKeep')} /></Field>
          <Field label={t('st.active')}>
            <label className="iot-checkbox"><input type="checkbox" checked={!!u.isActive} onChange={(e) => setEdit(u.id, { isActive: e.target.checked })} /><span>{u.mustChangePassword ? t('st.pwChangePending') : ''}</span></label>
          </Field>
          <div className="user-actions">
            <button type="button" onClick={() => save(u)} disabled={busy}><span className="btn-icon"><Ico n="save" sz={14} /> {t('common.save')}</span></button>
            <button type="button" className="quiet" onClick={() => resetPw(u)} disabled={!(pwDrafts[u.id] || '').trim()}><span className="btn-icon"><Ico n="key" sz={14} /> {t('st.resetPassword')}</span></button>
            <button type="button" className="quiet danger-text" onClick={() => setConfirmDel(u)}><span className="btn-icon"><Ico n="trash" sz={14} /> {t('common.delete')}</span></button>
          </div>
        </fieldset>
      ))}

      {confirmDel ? (
        <ConfirmModal title={t('st.deleteUserTitle')} body={t('st.deleteUserBody', { name: confirmDel.username })}
          confirmLabel={t('common.delete')} onCancel={() => setConfirmDel(null)} onConfirm={() => remove(confirmDel)} />
      ) : null}
    </div>
  );
}

// --- Location (site lat/lon) ----------------------------------------------------------------

export function LocationPanel({ onToast }) {
  const t = useT();
  const [lat, setLat] = useState('');
  const [lon, setLon] = useState('');
  const [isSet, setIsSet] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api('/api/settings/location').then((r) => {
      if (r.ok) { setIsSet(!!r.body?.set); if (r.body?.set) { setLat(String(r.body.latitude)); setLon(String(r.body.longitude)); } }
    });
  }, []);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await api('/api/settings/location', { method: 'PUT', body: JSON.stringify({ latitude: Number(lat), longitude: Number(lon) }) });
    setBusy(false);
    if (!r.ok) { onToast(errorMessage(r, t('st.locationFailed')), 'error'); return; }
    setIsSet(true); onToast(t('st.locationSaved'), 'success');
  }

  return (
    <div className="settings-content">
      <form onSubmit={save} className="iot-form">
        <p className="field-hint">{t('st.locationHint')}</p>
        <div className="form-grid">
          <Field label={t('st.latitude')}><input type="number" step="any" min="-90" max="90" value={lat} required onChange={(e) => setLat(e.target.value)} /></Field>
          <Field label={t('st.longitude')}><input type="number" step="any" min="-180" max="180" value={lon} required onChange={(e) => setLon(e.target.value)} /></Field>
        </div>
        <div className="modal-actions">
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : (isSet ? t('st.updateLocation') : t('st.setLocation'))}</button>
        </div>
      </form>
    </div>
  );
}

// --- Notifications (webhook / telegram) -----------------------------------------------------

function NotificationsPanel({ onToast }) {
  const t = useT();
  const [s, setS] = useState({ webhook: { enabled: false, url: '', minSeverity: 'warning' }, telegram: { enabled: false, botToken: '', chatId: '', minSeverity: 'warning' } });
  const [busy, setBusy] = useState(true);
  const setW = (patch) => setS((v) => ({ ...v, webhook: { ...v.webhook, ...patch } }));
  const setTg = (patch) => setS((v) => ({ ...v, telegram: { ...v.telegram, ...patch } }));

  useEffect(() => {
    api('/api/settings/notification').then((r) => { if (r.ok && r.body) setS(r.body); setBusy(false); });
  }, []);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await api('/api/settings/notification', { method: 'PUT', body: JSON.stringify(s) });
    setBusy(false);
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    onToast(t('st.notifSaved'), 'success');
  }

  async function test() {
    const r = await api('/api/settings/notification/test', { method: 'POST', body: '{}' });
    onToast(r.ok ? t('st.testSent') : errorMessage(r, t('st.testFailed')), r.ok ? 'success' : 'error');
  }

  const SevSelect = ({ value, onChange }) => (
    <select value={value} onChange={(e) => onChange(e.target.value)}>
      {SEVERITIES.map((sv) => <option key={sv} value={sv}>{t(`severity.${sv}`)}</option>)}
    </select>
  );

  return (
    <div className="settings-content">
      <form onSubmit={save} className="iot-form">
        <p className="field-hint">{t('st.notifHint')}</p>

        <h3 className="iot-subhead">{t('st.webhook')}</h3>
        <label className="iot-checkbox"><input type="checkbox" checked={s.webhook.enabled} onChange={(e) => setW({ enabled: e.target.checked })} /><span>{t('st.enableWebhook')}</span></label>
        <div className="form-grid">
          <Field label={t('st.webhookUrl')} span><input type="url" value={s.webhook.url} placeholder="https://" disabled={!s.webhook.enabled} onChange={(e) => setW({ url: e.target.value })} /></Field>
          <Field label={t('st.minSeverity')}><SevSelect value={s.webhook.minSeverity} onChange={(v) => setW({ minSeverity: v })} /></Field>
        </div>

        <h3 className="iot-subhead">{t('st.telegram')}</h3>
        <label className="iot-checkbox"><input type="checkbox" checked={s.telegram.enabled} onChange={(e) => setTg({ enabled: e.target.checked })} /><span>{t('st.enableTelegram')}</span></label>
        <div className="form-grid">
          <Field label={t('st.botToken')}><PasswordField value={s.telegram.botToken} onChange={(v) => setTg({ botToken: v })} autoComplete="off" /></Field>
          <Field label={t('st.chatId')}><input value={s.telegram.chatId} disabled={!s.telegram.enabled} onChange={(e) => setTg({ chatId: e.target.value })} /></Field>
          <Field label={t('st.minSeverity')}><SevSelect value={s.telegram.minSeverity} onChange={(v) => setTg({ minSeverity: v })} /></Field>
        </div>

        <div className="modal-actions">
          <button type="button" className="quiet" onClick={test} disabled={busy}><span className="btn-icon"><Ico n="send" sz={14} /> {t('st.sendTest')}</span></button>
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
        </div>
      </form>
    </div>
  );
}

// --- Telemetry & broker (restart-to-apply) --------------------------------------------------

function TelemetryPanel({ onToast, onGoSystem }) {
  const t = useT();
  const [s, setS] = useState(null);
  const [busy, setBusy] = useState(true);
  const set = (patch) => setS((v) => ({ ...v, ...patch }));

  useEffect(() => { api('/api/settings/telemetry').then((r) => { if (r.ok) setS(r.body); setBusy(false); }); }, []);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await api('/api/settings/telemetry', { method: 'PUT', body: JSON.stringify(s) });
    setBusy(false);
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    onToast(t('st.telemetrySaved'), 'success');
  }

  if (!s) return <p className="field-hint">{t('common.loading')}</p>;
  const num = (k) => (e) => set({ [k]: Number(e.target.value) });

  return (
    <div className="settings-content">
      <form onSubmit={save} className="iot-form">
        <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('st.restartNote')} <button type="button" className="link-btn" onClick={onGoSystem}>{t('st.goRestart')}</button></p>

        <h3 className="iot-subhead">{t('st.retention')}</h3>
        <div className="form-grid">
          <Field label={t('st.rawDays')} hint={t('st.rawDaysHint')}><input type="number" min="1" value={s.rawRetentionDays} onChange={num('rawRetentionDays')} /></Field>
          <Field label={t('st.rollupDays')} hint={t('st.rollupDaysHint')}><input type="number" min="1" value={s.rollupRetentionDays} onChange={num('rollupRetentionDays')} /></Field>
        </div>

        <h3 className="iot-subhead">{t('st.writeBatcher')}</h3>
        <div className="form-grid">
          <Field label={t('st.batchSize')}><input type="number" min="1" value={s.batchSize} onChange={num('batchSize')} /></Field>
          <Field label={t('st.flushMs')}><input type="number" min="1" value={s.flushMs} onChange={num('flushMs')} /></Field>
          <Field label={t('st.queueSize')}><input type="number" min="1" value={s.queueSize} onChange={num('queueSize')} /></Field>
        </div>

        <h3 className="iot-subhead">{t('st.broker')}</h3>
        <div className="form-grid">
          <Field label={t('st.mqttAddr')} hint={t('st.mqttAddrHint')} span><input value={s.mqttAddr} onChange={(e) => set({ mqttAddr: e.target.value })} /></Field>
        </div>

        <div className="modal-actions">
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
        </div>
      </form>
    </div>
  );
}

// --- Connectivity (fleet pairing) -----------------------------------------------------------

function ConnectivityPanel({ onToast }) {
  const t = useT();
  const [status, setStatus] = useState(null);
  const [fleetKey, setFleetKey] = useState('');
  const [claim, setClaim] = useState(null);
  const [confirmUnpair, setConfirmUnpair] = useState(false);

  const load = useCallback(() => { api('/api/pairing/status').then((r) => { if (r.ok) setStatus(r.body); }); }, []);
  useEffect(() => { load(); }, [load]);

  async function saveKey(e) {
    e.preventDefault();
    const r = await api('/api/pairing/fleet-key', { method: 'PUT', body: JSON.stringify({ key: fleetKey }) });
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    setFleetKey(''); onToast(t('st.fleetKeySaved'), 'success'); load();
  }
  async function genClaim() {
    const r = await api('/api/pairing/claim-code', { method: 'POST', body: '{}' });
    if (!r.ok) { onToast(errorMessage(r, t('st.claimFailed')), 'error'); return; }
    setClaim(r.body); load();
  }
  async function unpair() {
    setConfirmUnpair(false);
    const r = await api('/api/pairing/unpair', { method: 'POST', body: '{}' });
    if (!r.ok) { onToast(errorMessage(r, t('st.unpairFailed')), 'error'); return; }
    onToast(t('st.unpaired'), 'success'); setClaim(null); load();
  }

  if (!status) return <p className="field-hint">{t('common.loading')}</p>;

  return (
    <div className="settings-content">
      <div className="iot-status-grid">
        <div><span className="field-label">{t('st.pairedState')}</span><strong>{status.paired ? t('st.paired') : t('st.notPaired')}</strong></div>
        {status.paired ? <div><span className="field-label">{t('st.parent')}</span><strong>{status.parentName || status.parentId}</strong></div> : null}
        <div><span className="field-label">{t('st.fleetKey')}</span><strong>{status.fleetKeySet ? t('st.set') : t('st.notSet')}</strong></div>
        <div><span className="field-label">{t('st.claimCode')}</span><strong>{status.claimCodeActive ? t('st.active') : t('st.inactive')}</strong></div>
      </div>

      {!status.paired ? (
        <>
          <form onSubmit={saveKey} className="iot-form">
            <h3 className="iot-subhead">{t('st.fleetKey')}</h3>
            <p className="field-hint">{t('st.fleetKeyHint')}</p>
            <div className="form-grid">
              <Field label={t('st.fleetKey')} span><PasswordField value={fleetKey} onChange={setFleetKey} autoComplete="off" /></Field>
            </div>
            <div className="modal-actions"><button type="submit" disabled={!fleetKey.trim()}>{t('st.saveFleetKey')}</button></div>
          </form>

          <h3 className="iot-subhead">{t('st.claimCode')}</h3>
          <p className="field-hint">{t('st.claimHint')}</p>
          {claim?.code ? (
            <p className="iot-claim-code"><code>{claim.code}</code> <CopyButton value={claim.code} /></p>
          ) : null}
          <div className="modal-actions"><button type="button" className="quiet" onClick={genClaim}><span className="btn-icon"><Ico n="refresh" sz={14} /> {t('st.genClaim')}</span></button></div>
        </>
      ) : (
        <div className="modal-actions">
          <button type="button" className="quiet danger-text" onClick={() => setConfirmUnpair(true)}><span className="btn-icon"><Ico n="stop" sz={14} /> {t('st.unpair')}</span></button>
        </div>
      )}

      {confirmUnpair ? (
        <ConfirmModal title={t('st.unpairTitle')} body={t('st.unpairBody')} confirmLabel={t('st.unpair')} onCancel={() => setConfirmUnpair(false)} onConfirm={unpair} />
      ) : null}
    </div>
  );
}

// --- System (version / health / restart) ----------------------------------------------------

function SystemPanel({ onToast }) {
  const t = useT();
  const [version, setVersion] = useState(null);
  const [health, setHealth] = useState(null);
  const [confirmRestart, setConfirmRestart] = useState(false);
  const [restarting, setRestarting] = useState(false);

  useEffect(() => {
    api('/api/version').then((r) => { if (r.ok) setVersion(r.body); });
    api('/api/health').then((r) => setHealth({ ok: r.ok, ...(r.body || {}) }));
  }, []);

  async function restart() {
    setConfirmRestart(false);
    setRestarting(true);
    const r = await api('/api/system/restart', { method: 'POST', body: '{}' });
    if (!r.ok) { setRestarting(false); onToast(errorMessage(r, t('st.restartFailed')), 'error'); return; }
    onToast(t('st.restarting'), 'success');
  }

  return (
    <div className="settings-content">
      <h3 className="iot-subhead">{t('st.version')}</h3>
      <div className="iot-status-grid">
        <div><span className="field-label">{t('st.appVersion')}</span><strong>{version?.appVersion || '—'}</strong></div>
        <div><span className="field-label">{t('st.coreVersion')}</span><strong>{version?.coreVersion || '—'}</strong></div>
        <div><span className="field-label">{t('st.commit')}</span><strong>{version?.commit || '—'}</strong></div>
        <div><span className="field-label">{t('st.health')}</span><strong>{health?.ok ? t('st.healthy') : t('st.unhealthy')}</strong></div>
      </div>

      <h3 className="iot-subhead">{t('st.maintenance')}</h3>
      <p className="field-hint">{t('st.restartHint')}</p>
      <div className="modal-actions">
        <button type="button" className="danger-solid" disabled={restarting} onClick={() => setConfirmRestart(true)}>
          <span className="btn-icon"><Ico n="refresh" sz={14} /> {restarting ? t('st.restarting') : t('st.restart')}</span>
        </button>
      </div>

      {confirmRestart ? (
        <ConfirmModal title={t('st.restartTitle')} body={t('st.restartBody')} confirmLabel={t('st.restart')} onCancel={() => setConfirmRestart(false)} onConfirm={restart} />
      ) : null}
    </div>
  );
}
