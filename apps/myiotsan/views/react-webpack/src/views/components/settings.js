import { useCallback, useEffect, useMemo, useState } from 'react';
import { Ico, PasswordField, Tabs, useT } from '@shared';
import { api, defaultDestination, errorMessage, notificationCategories, notificationSeverityOptions } from '../lib/helpers';
import { AccordionItem, AccordionList, ConfirmModal, CopyButton, Field, Panel } from './ui';

// The Settings page — one tabbed home for everything that configures the hub itself. Admin-only (the
// nav entry is hidden from non-admins; every route below is administrator-gated server-side). It
// follows mymatasan's structure: a shared Tabs bar, a topic band under it (icon + title +
// description), then subtopic cards (settings-panel).

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
  const active = SETTINGS_TABS.find((tb) => tb.id === nav) || SETTINGS_TABS[0];
  const tabs = SETTINGS_TABS.map((tb) => ({ id: tb.id, label: t(`st.nav.${tb.id}`), icon: tb.icon }));

  return (
    <section className="workspace settings-tabbed">
      <Tabs tabs={tabs} active={nav} onChange={setNav} ariaLabel={t('page.settings')} />
      <header className="settings-tab-head">
        <span className="settings-tab-head-icon"><Ico n={active.icon} sz={22} /></span>
        <div className="settings-tab-head-text">
          <h2>{t(`st.nav.${active.id}`)}</h2>
          <p>{t(`st.tabHint.${active.id}`)}</p>
        </div>
      </header>
      <div className="settings-content">
        {nav === 'users' ? <UsersPanel onToast={onToast} /> : null}
        {nav === 'location' ? <LocationPanel onToast={onToast} /> : null}
        {nav === 'notifications' ? <NotificationsPanel onToast={onToast} /> : null}
        {nav === 'telemetry' ? <TelemetryPanel onToast={onToast} onGoSystem={() => setNav('system')} /> : null}
        {nav === 'connectivity' ? <ConnectivityPanel onToast={onToast} /> : null}
        {nav === 'system' ? <SystemPanel onToast={onToast} /> : null}
      </div>
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
    <>
      <Panel icon="user-plus" title={t('st.addUser')} hint={t('st.addUserHint')}>
        <form onSubmit={create}>
          <div className="settings-field-grid">
            <Field label={t('st.username')} required><input value={newUser.username} required autoComplete="off" onChange={(e) => setNewUser({ ...newUser, username: e.target.value })} /></Field>
            <Field label={t('st.displayName')}><input value={newUser.displayName} autoComplete="off" onChange={(e) => setNewUser({ ...newUser, displayName: e.target.value })} /></Field>
            <Field label={t('st.password')} required><PasswordField value={newUser.password} onChange={(p) => setNewUser({ ...newUser, password: p })} autoComplete="new-password" /></Field>
            <Field label={t('st.role')} required><RoleSelect value={newUser.roleId} onChange={(roleId) => setNewUser({ ...newUser, roleId })} /></Field>
          </div>
          <div className="settings-actions">
            <button type="submit" disabled={busy}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('st.addUser')}</span></button>
          </div>
        </form>
      </Panel>

      <Panel icon="user" title={t('st.users')} actions={<button type="button" className="quiet" onClick={load} disabled={busy}><span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.reload')}</span></button>}>
        {users.length === 0 ? <p className="settings-hint">{t('st.noUsers')}</p> : null}
        {users.map((u) => (
          <fieldset className="iot-action-row" key={u.id}>
            <Field label={t('st.username')}><input value={u.username || ''} onChange={(e) => setEdit(u.id, { username: e.target.value })} /></Field>
            <Field label={t('st.displayName')}><input value={u.displayName || ''} onChange={(e) => setEdit(u.id, { displayName: e.target.value })} /></Field>
            <Field label={t('st.role')}><RoleSelect value={u.roleId} onChange={(roleId) => setEdit(u.id, { roleId })} /></Field>
            <Field label={t('st.newPassword')}><PasswordField value={pwDrafts[u.id] || ''} onChange={(p) => setPwDrafts((d) => ({ ...d, [u.id]: p }))} autoComplete="new-password" placeholder={t('st.leaveBlankKeep')} /></Field>
            <label className="check-row"><input type="checkbox" checked={!!u.isActive} onChange={(e) => setEdit(u.id, { isActive: e.target.checked })} /> {t('st.active')}</label>
            {u.mustChangePassword ? <span className="accordion-tag">{t('st.pwChangePending')}</span> : null}
            <div className="user-actions">
              <button type="button" onClick={() => save(u)} disabled={busy}><span className="btn-icon"><Ico n="save" sz={14} /> {t('common.save')}</span></button>
              <button type="button" className="quiet" onClick={() => resetPw(u)} disabled={!(pwDrafts[u.id] || '').trim()}><span className="btn-icon"><Ico n="key" sz={14} /> {t('st.resetPassword')}</span></button>
              <button type="button" className="quiet danger-text" onClick={() => setConfirmDel(u)}><span className="btn-icon"><Ico n="trash" sz={14} /> {t('common.delete')}</span></button>
            </div>
          </fieldset>
        ))}
      </Panel>

      {confirmDel ? (
        <ConfirmModal title={t('st.deleteUserTitle')} body={t('st.deleteUserBody', { name: confirmDel.username })}
          confirmLabel={t('common.delete')} onCancel={() => setConfirmDel(null)} onConfirm={() => remove(confirmDel)} />
      ) : null}
    </>
  );
}

// --- Location -------------------------------------------------------------------------------

function LocationPanel({ onToast }) {
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
    <Panel icon="map-pin" title={t('st.nav.location')} hint={t('st.locationHint')}>
      <form onSubmit={save}>
        <div className="settings-field-grid">
          <Field label={t('st.latitude')} required><input type="number" step="any" min="-90" max="90" value={lat} required onChange={(e) => setLat(e.target.value)} /></Field>
          <Field label={t('st.longitude')} required><input type="number" step="any" min="-180" max="180" value={lon} required onChange={(e) => setLon(e.target.value)} /></Field>
        </div>
        <div className="settings-actions">
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : (isSet ? t('st.updateLocation') : t('st.setLocation'))}</button>
        </div>
      </form>
    </Panel>
  );
}

// --- Notifications: an accordion of delivery destinations -----------------------------------

function NotificationsPanel({ onToast }) {
  const t = useT();
  const [destinations, setDestinations] = useState([]);
  const [saved, setSaved] = useState([]); // server snapshot, to detect unsaved edits + discard
  const [openIndex, setOpenIndex] = useState(null);
  const [busy, setBusy] = useState(true);

  const load = useCallback(async () => {
    setBusy(true);
    const r = await api('/api/settings/notification');
    if (r.ok) { const d = r.body?.destinations || []; setDestinations(d); setSaved(d); }
    setBusy(false);
  }, []);
  useEffect(() => { load(); }, [load]);

  const update = (i, values) => setDestinations((list) => list.map((d, idx) => (idx === i ? { ...d, ...values } : d)));
  const changed = (i) => JSON.stringify(destinations[i]) !== JSON.stringify(saved.find((s) => s.id === destinations[i].id) || {});

  function add(type) {
    setDestinations((list) => { const next = [...list, defaultDestination(type)]; setOpenIndex(next.length - 1); return next; });
  }

  async function saveOne(i) {
    const r = await api('/api/settings/notification/destination', { method: 'PUT', body: JSON.stringify(destinations[i]) });
    if (!r.ok) { onToast(errorMessage(r, t('st.saveFailed')), 'error'); return; }
    const d = r.body?.settings?.destinations || [];
    setDestinations(d); setSaved(d);
    onToast(t('st.notifSaved'), 'success');
  }

  async function removeOne(i) {
    const dest = destinations[i];
    if (!dest.id) { setDestinations((list) => list.filter((_, idx) => idx !== i)); return; } // unsaved draft
    const r = await api(`/api/settings/notification/destination/${dest.id}`, { method: 'DELETE' });
    if (!r.ok) { onToast(errorMessage(r, t('st.deleteFailed')), 'error'); return; }
    const d = r.body?.destinations || [];
    setDestinations(d); setSaved(d);
  }

  function discardOne(i) {
    const snap = saved.find((s) => s.id === destinations[i].id);
    if (snap) update(i, snap); else setDestinations((list) => list.filter((_, idx) => idx !== i));
  }

  async function test() {
    const r = await api('/api/settings/notification/test', { method: 'POST', body: '{}' });
    onToast(r.ok ? t('st.testSent') : errorMessage(r, t('st.testFailed')), r.ok ? 'success' : 'error');
  }

  return (
    <Panel icon="bell" title={t('st.destinations')} hint={t('st.notifHint')}
      actions={<button type="button" className="quiet" onClick={test} disabled={busy}><span className="btn-icon"><Ico n="send" sz={14} /> {t('st.sendTest')}</span></button>}>
      {destinations.length === 0 ? <p className="settings-hint">{t('st.noDestinations')}</p> : null}
      <AccordionList>
        {destinations.map((dest, i) => (
          <DestinationItem key={dest.id || `new-${i}`} dest={dest} busy={busy} changed={changed(i)}
            open={openIndex === i} onToggleOpen={() => setOpenIndex(openIndex === i ? null : i)}
            onChange={(values) => update(i, values)} onSave={() => saveOne(i)} onDiscard={() => discardOne(i)} onRemove={() => removeOne(i)} />
        ))}
      </AccordionList>
      <div className="settings-actions" style={{ justifyContent: 'flex-start' }}>
        <button type="button" className="quiet" onClick={() => add('webhook')}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('st.addWebhook')}</span></button>
        <button type="button" className="quiet" onClick={() => add('telegram')}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('st.addTelegram')}</span></button>
        <button type="button" className="quiet" onClick={() => add('mqtt')}><span className="btn-icon"><Ico n="plus" sz={14} /> {t('st.addMqtt')}</span></button>
      </div>
    </Panel>
  );
}

function destinationTarget(dest) {
  if (dest.type === 'mqtt') return `${dest.mqtt?.brokerUrl || ''}${dest.mqtt?.topic ? ' → ' + dest.mqtt.topic : ''}`;
  if (dest.type === 'telegram') return dest.chatId ? `Chat ${dest.chatId}` : '';
  return dest.url || '';
}

function DestinationItem({ dest, busy, changed, open, onToggleOpen, onChange, onSave, onDiscard, onRemove }) {
  const t = useT();
  const enabled = dest.enabled !== false;
  const summary = (
    <>
      <span className="accordion-title">{dest.name || dest.type}</span>
      <span className={`class-source-badge ${dest.type}`}>{dest.type}</span>
      <span className="accordion-muted">{destinationTarget(dest)}</span>
      {!enabled ? <span className="accordion-tag">{t('common.disabled')}</span> : null}
    </>
  );
  const actions = (
    <>
      <label className="check-row compact"><input type="checkbox" checked={enabled} onChange={(e) => onChange({ enabled: e.target.checked })} disabled={busy} /> {t('common.enabled')}</label>
      <button type="button" className="quiet danger-text" onClick={onRemove} disabled={busy}>{t('common.remove')}</button>
    </>
  );
  return (
    <AccordionItem open={open} onToggle={onToggleOpen} summary={summary} actions={actions}>
      <DestinationCard dest={dest} busy={busy} onChange={onChange} />
      <div className="settings-actions destination-actions">
        <button type="button" onClick={onSave} disabled={busy || !changed}><span className="btn-icon"><Ico n="save" sz={14} /> {t('st.saveDestination')}</span></button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !changed}><span className="btn-icon"><Ico n="undo" sz={14} /> {t('common.discard')}</span></button>
      </div>
    </AccordionItem>
  );
}

function DestinationCard({ dest, busy, onChange }) {
  const t = useT();
  const categories = dest.categories || [];
  const setMqtt = (values) => onChange({ mqtt: { ...dest.mqtt, ...values } });
  const toggleCategory = (value, on) => {
    const set = new Set(categories);
    on ? set.add(value) : set.delete(value);
    onChange({ categories: [...set] });
  };

  return (
    <div className="settings-field-grid">
      <Field label={t('st.destName')} span><input value={dest.name || ''} disabled={busy} onChange={(e) => onChange({ name: e.target.value })} /></Field>

      {dest.type === 'webhook' ? (
        <Field label={t('st.webhookUrl')} span><input type="url" value={dest.url || ''} placeholder="https://" disabled={busy} onChange={(e) => onChange({ url: e.target.value })} /></Field>
      ) : null}

      {dest.type === 'telegram' ? (
        <>
          <Field label={t('st.botToken')}><PasswordField value={dest.botToken || ''} onChange={(v) => onChange({ botToken: v })} autoComplete="off" /></Field>
          <Field label={t('st.chatId')}><input value={dest.chatId || ''} disabled={busy} onChange={(e) => onChange({ chatId: e.target.value })} /></Field>
        </>
      ) : null}

      {dest.type === 'mqtt' ? (
        <>
          <Field label={t('st.brokerUrl')}><input value={dest.mqtt?.brokerUrl || ''} placeholder="tcp://host:1883" disabled={busy} onChange={(e) => setMqtt({ brokerUrl: e.target.value })} /></Field>
          <Field label={t('st.topic')}><input value={dest.mqtt?.topic || ''} disabled={busy} onChange={(e) => setMqtt({ topic: e.target.value })} /></Field>
          <Field label={t('st.qos')}>
            <select value={dest.mqtt?.qos ?? 1} disabled={busy} onChange={(e) => setMqtt({ qos: Number(e.target.value) })}>
              <option value={0}>0</option><option value={1}>1</option><option value={2}>2</option>
            </select>
          </Field>
          <label className="check-row"><input type="checkbox" checked={!!dest.mqtt?.retain} disabled={busy} onChange={(e) => setMqtt({ retain: e.target.checked })} /> {t('st.retain')}</label>
          <Field label={t('st.mqttUser')}><input value={dest.mqtt?.username || ''} disabled={busy} onChange={(e) => setMqtt({ username: e.target.value })} /></Field>
          <Field label={t('st.mqttPass')}><PasswordField value={dest.mqtt?.password || ''} onChange={(v) => setMqtt({ password: v })} autoComplete="off" /></Field>
        </>
      ) : null}

      <Field label={t('st.minSeverity')}>
        <select value={dest.minSeverity || 'warning'} disabled={busy} onChange={(e) => onChange({ minSeverity: e.target.value })}>
          {notificationSeverityOptions.map(([v, label]) => <option key={v} value={v}>{label}</option>)}
        </select>
      </Field>

      <fieldset className="dest-group field-span-two">
        <legend>{t('st.receives')}</legend>
        {notificationCategories.map(([value, label, help]) => (
          <label className="check-row" key={value} title={help}>
            <input type="checkbox" checked={categories.length === 0 || categories.includes(value)} disabled={busy} onChange={(e) => toggleCategory(value, e.target.checked)} />
            {label}
          </label>
        ))}
        <span className="field-hint">{t('st.noneAllTypes')}</span>
      </fieldset>
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

  if (!s) return <Panel icon="sliders" title={t('st.nav.telemetry')}><p className="settings-hint">{t('common.loading')}</p></Panel>;
  const num = (k) => (e) => set({ [k]: Number(e.target.value) });

  return (
    <form onSubmit={save}>
      <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('st.restartNote')} <button type="button" className="link-btn" onClick={onGoSystem}>{t('st.goRestart')}</button></p>

      <Panel icon="sliders" title={t('st.retention')}>
        <div className="settings-field-grid">
          <Field label={t('st.rawDays')} hint={t('st.rawDaysHint')}><input type="number" min="1" value={s.rawRetentionDays} onChange={num('rawRetentionDays')} /></Field>
          <Field label={t('st.rollupDays')} hint={t('st.rollupDaysHint')}><input type="number" min="1" value={s.rollupRetentionDays} onChange={num('rollupRetentionDays')} /></Field>
        </div>
      </Panel>

      <Panel icon="sliders" title={t('st.writeBatcher')}>
        <div className="settings-field-grid">
          <Field label={t('st.batchSize')}><input type="number" min="1" value={s.batchSize} onChange={num('batchSize')} /></Field>
          <Field label={t('st.flushMs')}><input type="number" min="1" value={s.flushMs} onChange={num('flushMs')} /></Field>
          <Field label={t('st.queueSize')}><input type="number" min="1" value={s.queueSize} onChange={num('queueSize')} /></Field>
        </div>
      </Panel>

      <Panel icon="cpu" title={t('st.broker')}>
        <div className="settings-field-grid">
          <Field label={t('st.mqttAddr')} hint={t('st.mqttAddrHint')} span><input value={s.mqttAddr} onChange={(e) => set({ mqttAddr: e.target.value })} /></Field>
        </div>
        <div className="settings-actions">
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
        </div>
      </Panel>
    </form>
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

  if (!status) return <Panel icon="shield" title={t('st.nav.connectivity')}><p className="settings-hint">{t('common.loading')}</p></Panel>;

  return (
    <>
      <Panel icon="shield" title={t('st.fleetStatus')}>
        <div className="iot-status-grid">
          <div><span className="field-label">{t('st.pairedState')}</span><strong>{status.paired ? t('st.paired') : t('st.notPaired')}</strong></div>
          {status.paired ? <div><span className="field-label">{t('st.parent')}</span><strong>{status.parentName || status.parentId}</strong></div> : null}
          <div><span className="field-label">{t('st.fleetKey')}</span><strong>{status.fleetKeySet ? t('st.set') : t('st.notSet')}</strong></div>
          <div><span className="field-label">{t('st.claimCode')}</span><strong>{status.claimCodeActive ? t('st.active') : t('st.inactive')}</strong></div>
        </div>
      </Panel>

      {!status.paired ? (
        <>
          <Panel icon="key" title={t('st.fleetKey')} hint={t('st.fleetKeyHint')}>
            <form onSubmit={saveKey}>
              <div className="settings-field-grid">
                <Field label={t('st.fleetKey')} span><PasswordField value={fleetKey} onChange={setFleetKey} autoComplete="off" /></Field>
              </div>
              <div className="settings-actions"><button type="submit" disabled={!fleetKey.trim()}>{t('st.saveFleetKey')}</button></div>
            </form>
          </Panel>

          <Panel icon="search" title={t('st.claimCode')} hint={t('st.claimHint')}>
            {claim?.code ? <p className="iot-claim-code"><code>{claim.code}</code> <CopyButton value={claim.code} /></p> : null}
            <div className="settings-actions" style={{ justifyContent: 'flex-start' }}>
              <button type="button" className="quiet" onClick={genClaim}><span className="btn-icon"><Ico n="refresh" sz={14} /> {t('st.genClaim')}</span></button>
            </div>
          </Panel>
        </>
      ) : (
        <Panel icon="stop" title={t('st.leaveFleet')}>
          <div className="settings-actions" style={{ justifyContent: 'flex-start' }}>
            <button type="button" className="quiet danger-text" onClick={() => setConfirmUnpair(true)}><span className="btn-icon"><Ico n="stop" sz={14} /> {t('st.unpair')}</span></button>
          </div>
        </Panel>
      )}

      {confirmUnpair ? (
        <ConfirmModal title={t('st.unpairTitle')} body={t('st.unpairBody')} confirmLabel={t('st.unpair')} onCancel={() => setConfirmUnpair(false)} onConfirm={unpair} />
      ) : null}
    </>
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
    <>
      <Panel icon="monitor" title={t('st.version')}>
        <div className="iot-status-grid">
          <div><span className="field-label">{t('st.appVersion')}</span><strong>{version?.appVersion || '—'}</strong></div>
          <div><span className="field-label">{t('st.coreVersion')}</span><strong>{version?.coreVersion || '—'}</strong></div>
          <div><span className="field-label">{t('st.commit')}</span><strong>{version?.commit || '—'}</strong></div>
          <div><span className="field-label">{t('st.health')}</span><strong>{health?.ok ? t('st.healthy') : t('st.unhealthy')}</strong></div>
        </div>
      </Panel>

      <Panel icon="refresh" title={t('st.maintenance')} hint={t('st.restartHint')}>
        <div className="settings-actions" style={{ justifyContent: 'flex-start' }}>
          <button type="button" className="danger-solid" disabled={restarting} onClick={() => setConfirmRestart(true)}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {restarting ? t('st.restarting') : t('st.restart')}</span>
          </button>
        </div>
      </Panel>

      {confirmRestart ? (
        <ConfirmModal title={t('st.restartTitle')} body={t('st.restartBody')} confirmLabel={t('st.restart')} onCancel={() => setConfirmRestart(false)} onConfirm={restart} />
      ) : null}
    </>
  );
}
