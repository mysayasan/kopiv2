import { useState, useEffect, useRef, useCallback } from 'react';
import { Ico } from './icons';
import { Tabs } from '@shared/Tabs';
import { DataTable } from '@shared/DataTable';
import { useT } from '@shared/i18n';
import { FormBusyOverlay, FieldTitle, AccordionList, AccordionItem } from './ui';
import { ConsoleLog } from './console';
import { PasswordField } from './layout';
import { defaultYoloConfig, bestYoloDefaults, defaultCaptureConfig, captureModeOptions, defaultAlertNotificationConfig, alertNotificationFields, alertFieldDataKeys, builtinPayloadKeys, notificationCategories, notificationTemplateTokens, defaultDestination, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings } from '../lib/constants';
import {iceUrlsText,textToIceUrls,decoderTransportOptions,decoderHWAccelOptions,apiBase } from '../lib/helpers';

// SETTINGS_TABS drives the top tab bar: order, icon, label, and the contextual
// one-line description shown in the header band under the active tab.
const SETTINGS_TABS = [
  { id: 'runtime', labelKey: 'st.navRuntime', descKey: 'st.tabDesc.runtime', icon: 'sliders' },
  { id: 'ai', labelKey: 'st.navAi', descKey: 'st.tabDesc.ai', icon: 'cpu' },
  { id: 'notifications', labelKey: 'st.navNotifications', descKey: 'st.tabDesc.notifications', icon: 'bell' },
  { id: 'health', labelKey: 'st.navCameraHealth', descKey: 'st.tabDesc.health', icon: 'wifi' },
  { id: 'machine', labelKey: 'st.navMachineHealth', descKey: 'st.tabDesc.machine', icon: 'cpu' },
  { id: 'users', labelKey: 'st.navUsers', descKey: 'st.tabDesc.users', icon: 'user' },
  { id: 'pairing', labelKey: 'st.navConnectivity', descKey: 'st.tabDesc.pairing', icon: 'shield' },
  { id: 'backup', labelKey: 'st.navBackup', descKey: 'st.tabDesc.backup', icon: 'key' },
  { id: 'system', labelKey: 'st.navVersionHealth', descKey: 'st.tabDesc.system', icon: 'monitor' },
];

// stockModelHints describes the speed/accuracy trade-off of each base model.
const stockModelHints = {
  'yolo11n.pt': 'st.modelHintNano',
  'yolo11s.pt': 'st.modelHintSmall',
  'yolo11m.pt': 'st.modelHintMedium',
  'yolo11l.pt': 'st.modelHintLarge',
  'yolo11x.pt': 'st.modelHintXl',
};

// StockModelPanel picks the always-on base detection model. Known variants are
// downloaded from the net by ultralytics; a custom path can also be used.
// useRoles loads the roles an admin may assign. Authorization is a ROLE now, not an
// "administrator" checkbox: viewer (watch live), operator (+ review footage, acknowledge,
// PTZ, talk) and admin (everything). The middle rung is the point — an operator who was
// present at an incident cannot delete the footage of it.
function useRoles(authHeader) {
  const [roles, setRoles] = useState([]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const headers = {};
        if (authHeader) headers.Authorization = authHeader;
        const resp = await fetch(`${apiBase()}/api/settings/roles`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const payload = await resp.json();
        const list = payload?.data?.result ?? payload?.result ?? [];
        if (!cancelled && Array.isArray(list)) setRoles(list);
      } catch (_) { /* the picker just falls back to empty; the API still accepts a role id */ }
    })();
    return () => { cancelled = true; };
  }, [authHeader]);
  return roles;
}

// roleLabel maps a role name to its translated label. The names are stable server-side
// identifiers ("superadmin" / "operator" / "viewer"); only the label is localised.
function roleLabel(t, name) {
  switch (name) {
    case 'superadmin': return t('st.roleAdmin');
    case 'operator': return t('st.roleOperator');
    case 'viewer': return t('st.roleViewer');
    default: return name;
  }
}

function RoleSelect({ roles, value, onChange, disabled, label }) {
  const t = useT();
  return (
    <label>
      {label || t('st.role')}
      <select value={value || ''} onChange={(event) => onChange(Number(event.target.value))} disabled={disabled} aria-label={label || t('st.role')}>
        <option value="">{t('st.roleUnassigned')}</option>
        {roles.map((role) => (
          <option key={role.id} value={role.id}>{roleLabel(t, role.name)}</option>
        ))}
      </select>
    </label>
  );
}

// --- Users & Roles --------------------------------------------------------------------------
//
// A scannable TABLE of accounts (initials avatar + name + @username, a colour-coded role badge,
// a status pill), with add/edit behind a modal rather than a wall of inline inputs — the standard
// admin user-management pattern: a table to compare at a glance, focused editing behind a dialog.
// Self-contained like StockModelPanel above (its own fetch + state); ported from myiotsan so both
// appliances present users the same way.

const AVATAR_COLORS = ['#3b82f6', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981', '#06b6d4', '#ef4444', '#6366f1', '#0ea5e9', '#d946ef'];

function userInitials(u) {
  const src = (u.displayName || u.username || '?').trim();
  const parts = src.split(/\s+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return src.slice(0, 2).toUpperCase();
}
function avatarColor(seed) {
  let h = 0;
  for (const ch of String(seed || '')) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}
function roleTone(role) {
  if (!role) return '';
  if (role.isSuperadmin) return 'is-admin';
  return /operator/i.test(role.name) ? 'is-operator' : 'is-viewer';
}

// usersApi mirrors the panel-local fetch helper used elsewhere in this file (cf. StockModelPanel):
// it carries the admin auth header and unwraps the API envelope, throwing on a non-2xx.
async function usersApi(authHeader, path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (authHeader) headers.Authorization = authHeader;
  if (options.body) headers['Content-Type'] = 'application/json';
  const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
  const text = await resp.text();
  let payload = null;
  if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
  if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
  return payload?.data?.result ?? payload?.result ?? payload;
}

function UsersManager({ authHeader, onMessage, focusUsername }) {
  const t = useT();
  const roles = useRoles(authHeader);
  const [users, setUsers] = useState([]);
  const [busy, setBusy] = useState(true);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState(null);
  const [confirmDel, setConfirmDel] = useState(null);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const result = await usersApi(authHeader, '/api/settings/users?limit=100&offset=0');
      setUsers(Array.isArray(result) ? result : result?.items || []);
    } catch (err) {
      onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }, [authHeader, onMessage]);
  useEffect(() => { load(); }, [load]);

  // When a login-security notification deep-links here, open that account's editor.
  useEffect(() => {
    if (!focusUsername || busy) return;
    const u = users.find((x) => x.username === focusUsername);
    if (u) setEditing(u);
  }, [focusUsername, busy, users]);

  const roleOf = (id) => roles.find((x) => x.id === id);

  async function remove(u) {
    setConfirmDel(null);
    try {
      await usersApi(authHeader, `/api/settings/users/${u.id}`, { method: 'DELETE' });
      onMessage(t('app.userDeleted'));
      load();
    } catch (err) { onMessage(err.message, 'error'); }
  }

  const columns = [
    {
      key: 'displayName', label: t('st.user'),
      render: (_v, u) => (
        <div className="user-cell">
          <span className="user-avatar" style={{ background: avatarColor(u.username || u.id) }}>{userInitials(u)}</span>
          <div className="user-idcol">
            <button type="button" className="quiet user-link" onClick={() => setEditing(u)}>{u.displayName || u.username}</button>
            <span className="user-sub">@{u.username}</span>
          </div>
        </div>
      ),
    },
    {
      key: 'roleId', label: t('st.role'),
      render: (_v, u) => { const role = roleOf(u.roleId); return role ? <span className={`role-badge ${roleTone(role)}`}>{roleLabel(t, role.name)}</span> : <span className="user-sub">—</span>; },
    },
    {
      key: 'isActive', label: t('st.status'), filterType: 'boolean',
      render: (_v, u) => (
        <span className="user-status-cell">
          <span className={`user-pill ${u.isActive ? 'is-on' : 'is-off'}`}>{u.isActive ? t('common.active') : t('common.inactive')}</span>
          {u.mustChangePassword ? <span className="user-pill is-warn" title={t('st.pwChangePending')}><Ico n="key" sz={11} /></span> : null}
        </span>
      ),
    },
    {
      key: 'actions', label: '', filterable: false,
      render: (_v, u) => (
        <div className="table-actions">
          <button type="button" className="quiet" onClick={() => setEditing(u)}><span className="btn-icon"><Ico n="edit-2" sz={14} /> {t('common.edit')}</span></button>
          <button type="button" className="quiet danger-text" onClick={() => setConfirmDel(u)}><Ico n="trash" sz={14} /></button>
        </div>
      ),
    },
  ];

  return (
    <div className="settings-layout">
      <section className="settings-panel span-two">
        <header>
          <h2>{t('st.users')}</h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={busy}><span className="btn-icon"><Ico n="refresh" /> {t('common.reload')}</span></button>
            <button type="button" onClick={() => setAdding(true)}><span className="btn-icon"><Ico n="user-plus" /> {t('st.addUser')}</span></button>
          </div>
        </header>
        <p className="settings-hint">{t('st.usersHint')}</p>
        <DataTable rows={users} columns={columns} busy={busy} pageSize={10} pageSizeOptions={[10, 25, 50]} emptyText={t('st.noUsers')} />
      </section>

      {adding ? <UserModal roles={roles} authHeader={authHeader} onClose={() => setAdding(false)} onSaved={() => { setAdding(false); load(); }} onMessage={onMessage} /> : null}
      {editing ? <UserModal user={editing} roles={roles} authHeader={authHeader} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} onMessage={onMessage} /> : null}
      {confirmDel ? (
        <div className="modal-backdrop" onClick={() => setConfirmDel(null)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()} role="alertdialog" aria-modal="true">
            <h2 className="user-modal-title">{t('st.deleteUserTitle')}</h2>
            <p>{t('st.deleteUserBody', { name: confirmDel.username })}</p>
            <div className="modal-actions">
              <button type="button" className="quiet" onClick={() => setConfirmDel(null)}>{t('common.cancel')}</button>
              <button type="button" className="danger-solid" onClick={() => remove(confirmDel)}>{t('common.delete')}</button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

// UserModal creates a new account or edits an existing one — the focused editing surface the
// table links into. On an existing user it also carries the reset-password action.
function UserModal({ user, roles, authHeader, onClose, onSaved, onMessage }) {
  const t = useT();
  const isNew = !user;
  const [form, setForm] = useState(() => ({
    username: user?.username || '',
    displayName: user?.displayName || '',
    roleId: user?.roleId || 0,
    isActive: isNew ? true : !!user?.isActive,
    password: '',
    newPassword: '',
  }));
  const [busy, setBusy] = useState(false);
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));
  const role = roles.find((r) => r.id === Number(form.roleId));

  async function submit(e) {
    e.preventDefault();
    if (!form.roleId) { onMessage(t('st.roleUnassigned'), 'error'); return; }
    if (isNew && (form.password || '').length < 8) { onMessage(t('app.passwordMin'), 'error'); return; }
    setBusy(true);
    try {
      if (isNew) {
        const body = { username: form.username, password: form.password, displayName: form.displayName, roleId: Number(form.roleId), isAdmin: !!role?.isSuperadmin, isActive: form.isActive, mustChangePassword: true };
        await usersApi(authHeader, '/api/settings/users', { method: 'POST', body: JSON.stringify(body) });
        onMessage(t('app.userCreated'));
      } else {
        const body = { username: form.username, displayName: form.displayName, roleId: Number(form.roleId), isAdmin: !!role?.isSuperadmin, isActive: form.isActive };
        await usersApi(authHeader, `/api/settings/users/${user.id}`, { method: 'PUT', body: JSON.stringify(body) });
        onMessage(t('app.userSaved'));
      }
      onSaved();
    } catch (err) {
      onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function resetPw() {
    const password = form.newPassword.trim();
    if (password.length < 8) { onMessage(t('app.passwordMin'), 'error'); return; }
    setBusy(true);
    try {
      await usersApi(authHeader, `/api/settings/users/${user.id}/password`, { method: 'POST', body: JSON.stringify({ password }) });
      set({ newPassword: '' });
      onMessage(t('app.passwordReset'));
    } catch (err) {
      onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onClick={busy ? undefined : onClose}>
      <div className="modal-card user-modal-card" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <h2 className="user-modal-title">{isNew ? t('st.addUser') : t('st.editUser', { name: user.displayName || user.username })}</h2>
        <form onSubmit={submit} className="user-form">
          <div className="settings-field-grid">
            <label>
              {t('common.username')}
              <input value={form.username} required autoComplete="off" onChange={(e) => set({ username: e.target.value })} />
            </label>
            <label>
              {t('st.displayName')}
              <input value={form.displayName} autoComplete="off" onChange={(e) => set({ displayName: e.target.value })} />
            </label>
            {isNew ? (
              <label>
                {t('common.password')}
                <PasswordField value={form.password} onChange={(p) => set({ password: p })} autoComplete="new-password" />
              </label>
            ) : null}
            <RoleSelect roles={roles} value={form.roleId} onChange={(roleId) => set({ roleId })} disabled={busy} />
          </div>
          <label className="check-row"><input type="checkbox" checked={form.isActive} onChange={(e) => set({ isActive: e.target.checked })} /> {t('st.activeAccount')}</label>

          {!isNew ? (
            <>
              <hr className="user-modal-sep" />
              <p className="settings-hint">{t('st.resetPwHint')}</p>
              <div className="user-pw-row">
                <PasswordField value={form.newPassword} onChange={(p) => set({ newPassword: p })} autoComplete="new-password" placeholder={t('st.newPassword')} />
                <button type="button" className="quiet" onClick={resetPw} disabled={busy || !form.newPassword.trim()}><span className="btn-icon"><Ico n="key" sz={14} /> {t('st.resetPassword')}</span></button>
              </div>
            </>
          ) : null}

          <div className="modal-actions">
            <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
            <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
          </div>
        </form>
      </div>
    </div>
  );
}

function StockModelPanel({ authHeader, onMessage }) {
  const t = useT();
  const [info, setInfo] = useState({ current: 'yolo11n.pt', options: [] });
  const [choice, setChoice] = useState('');
  const [customPath, setCustomPath] = useState('');
  const [busy, setBusy] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function load() {
    try {
      const result = await api('/api/training/stock-model');
      setInfo({ current: result?.current || 'yolo11n.pt', options: Array.isArray(result?.options) ? result.options : [] });
      setChoice(result?.current || 'yolo11n.pt');
    } catch (_) { /* best effort */ }
  }
  useEffect(() => { load(); }, [authHeader]);

  async function apply() {
    const model = choice === '__custom__' ? customPath.trim() : choice;
    if (!model) { if (onMessage) onMessage(t('st.chooseModelOrPath'), 'error'); return; }
    setBusy(true);
    if (onMessage) onMessage(t('st.applyingBaseModel'), 'info');
    try {
      const result = await api('/api/training/stock-model', { method: 'POST', body: JSON.stringify({ model }) });
      setInfo({ current: result?.current || model, options: info.options });
      if (onMessage) onMessage(t('st.baseModelSet', { model: result?.current || model }));
    } catch (err) {
      if (onMessage) onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  const dirty = choice !== '__custom__' ? choice !== info.current : customPath.trim() !== '';

  return (
    <section className="settings-panel span-two">
      <header>
        <h2>
          <FieldTitle info={t('st.stockInfo')}>
            {t('st.stockTitle')}
          </FieldTitle>
        </h2>
        <span className="status-pill">{info.current}</span>
      </header>
      <p className="settings-hint">
        {t('st.stockHint')}
      </p>
      <div className="settings-field-grid">
        <label>
          {t('st.model')}
          <select value={choice} onChange={(e) => setChoice(e.target.value)} disabled={busy}>
            {info.options.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
            <option value="__custom__">{t('st.customPathOpt')}</option>
          </select>
          <span className="field-hint">{choice === '__custom__' ? t('st.customPathHint') : (stockModelHints[choice] ? t(stockModelHints[choice]) : '')}</span>
        </label>
        {choice === '__custom__' ? (
          <label>
            {t('st.customPtPath')}
            <input value={customPath} onChange={(e) => setCustomPath(e.target.value)} placeholder="/path/to/model.pt" disabled={busy} />
          </label>
        ) : null}
      </div>
      <div className="settings-actions">
        <button type="button" onClick={apply} disabled={busy || !dirty}>
          <span className="btn-icon"><Ico n="download" /> {t('st.downloadApply')}</span>
        </button>
      </div>
    </section>
  );
}

// LPRModelPanel manages the optional license-plate (LPR) detector — a separate
// second-stage model from the stock/custom general detectors. It offers both the
// "automated" path (a curated catalog or any https URL the app downloads) and the
// "manual" path (upload a .pt). Plate models are third-party, so unlike the stock
// picker they download from an explicit URL rather than by ultralytics name.
function LPRModelPanel({ authHeader, onMessage }) {
  const t = useT();
  const [info, setInfo] = useState({ current: '', path: '', options: [], ocrReady: false });
  const [choice, setChoice] = useState('');
  const [customValue, setCustomValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [installingOcr, setInstallingOcr] = useState(false);
  const [ocrLog, setOcrLog] = useState('');
  const fileRef = useRef(null);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function load() {
    try {
      const result = await api('/api/training/lpr-model');
      setInfo({ current: result?.current || '', path: result?.path || '', options: Array.isArray(result?.options) ? result.options : [], ocrReady: !!result?.ocrReady });
      setChoice(result?.options?.[0]?.name || '__url__');
    } catch (_) { /* best effort */ }
  }
  useEffect(() => { load(); }, [authHeader]);

  // Install the OCR dependency (easyocr) in-app, streaming the install log just
  // like the GPU dependency installer. Polls the shared installer status endpoint.
  async function installOcr() {
    if (installingOcr) return;
    setInstallingOcr(true);
    if (onMessage) onMessage(t('st.installingOcr'), 'info');
    try {
      const st = await api('/api/training/lpr-model/install-deps', { method: 'POST' });
      setOcrLog(st?.log || '');
    } catch (err) {
      setInstallingOcr(false);
      if (onMessage) onMessage(err.message);
    }
  }
  useEffect(() => {
    if (!installingOcr) return undefined;
    const id = window.setInterval(async () => {
      try {
        const st = await api('/api/training/setup-deps');
        setOcrLog(st?.log || '');
        if (!st?.running) {
          window.clearInterval(id);
          setInstallingOcr(false);
          await load();
          if (onMessage) onMessage(st?.status === 'done'
            ? t('st.ocrInstalled')
            : t('st.ocrInstallErr'), st?.status === 'done' ? 'success' : 'error');
        }
      } catch (_) { /* keep polling */ }
    }, 3000);
    return () => window.clearInterval(id);
  }, [installingOcr]); // eslint-disable-line react-hooks/exhaustive-deps

  async function apply() {
    const model = choice === '__url__' || choice === '__path__' ? customValue.trim() : choice;
    if (!model) { if (onMessage) onMessage(t('st.chooseCatalogOrUrl'), 'error'); return; }
    setBusy(true);
    if (onMessage) onMessage(t('st.applyingPlateModel'), 'info');
    try {
      const result = await api('/api/training/lpr-model', { method: 'POST', body: JSON.stringify({ model }) });
      setInfo({ current: result?.current || '', path: result?.path || '', options: info.options });
      if (onMessage) onMessage(t('st.plateModelSet', { model: result?.current || model }));
    } catch (err) {
      if (onMessage) onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function upload(file) {
    if (!file) return;
    setBusy(true);
    if (onMessage) onMessage(t('st.uploadingName', { name: file.name }), 'info');
    try {
      const form = new FormData();
      form.append('file', file);
      form.append('name', file.name);
      const result = await api('/api/training/lpr-model/import', { method: 'POST', body: form });
      setInfo({ current: result?.current || file.name, path: result?.path || '', options: info.options });
      if (onMessage) onMessage(t('st.plateModelUploaded', { name: result?.current || file.name }));
    } catch (err) {
      if (onMessage) onMessage(err.message, 'error');
    } finally {
      setBusy(false);
      if (fileRef.current) fileRef.current.value = '';
    }
  }

  async function deactivate() {
    setBusy(true);
    try {
      const result = await api('/api/training/lpr-model/deactivate', { method: 'POST' });
      setInfo({ current: result?.current || '', path: result?.path || '', options: info.options });
      if (onMessage) onMessage(t('st.plateModelDisabled'));
    } catch (err) {
      if (onMessage) onMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  const active = !!info.current;
  const selectedOpt = info.options.find((o) => o.name === choice);

  return (
    <section className="settings-panel span-two">
      <header>
        <h2>
          <FieldTitle info={t('st.lprInfo')}>
            {t('st.lprTitle')}
          </FieldTitle>
        </h2>
        <span className="status-pill">{active ? info.current : t('st.disabled')}</span>
      </header>
      <p className="settings-hint">
        {t('st.lprHint')}
      </p>
      <div className="settings-field-grid">
        <label>
          {t('st.plateModel')}
          <select value={choice} onChange={(e) => setChoice(e.target.value)} disabled={busy}>
            {info.options.map((opt) => <option key={opt.name} value={opt.name}>{opt.name}</option>)}
            <option value="__url__">{t('st.downloadFromUrl')}</option>
            <option value="__path__">{t('st.localPtPath')}</option>
          </select>
          <span className="field-hint">
            {choice === '__url__' ? t('st.pasteUrlHint')
              : choice === '__path__' ? t('st.absPathHint')
              : (selectedOpt?.description || '')}
          </span>
        </label>
        {(choice === '__url__' || choice === '__path__') ? (
          <label>
            {choice === '__url__' ? t('st.modelUrl') : t('st.serverPtPath')}
            <input value={customValue} onChange={(e) => setCustomValue(e.target.value)} placeholder={choice === '__url__' ? 'https://…/best.pt' : '/path/to/plate.pt'} disabled={busy} />
          </label>
        ) : null}
      </div>
      <div className="settings-actions">
        <button type="button" onClick={apply} disabled={busy}>
          <span className="btn-icon"><Ico n="download" /> {t('st.downloadApply')}</span>
        </button>
        <button type="button" className="quiet" onClick={() => fileRef.current && fileRef.current.click()} disabled={busy}>
          <span className="btn-icon"><Ico n="plus" /> {t('st.uploadPt')}</span>
        </button>
        {active ? (
          <button type="button" className="quiet danger" onClick={deactivate} disabled={busy}>
            <span className="btn-icon"><Ico n="x" /> {t('st.disableLpr')}</span>
          </button>
        ) : null}
        <input ref={fileRef} type="file" accept=".pt" style={{ display: 'none' }} onChange={(e) => upload(e.target.files?.[0])} />
      </div>
      <div className="install-deps" style={{ marginTop: '12px' }}>
        <div className="metadata-row" style={{ alignItems: 'center', gap: '10px' }}>
          <span className={`status-pill ${info.ocrReady ? 'ok' : 'offline'}`}>
            {t('st.ocrEngine', { state: info.ocrReady ? t('st.ocrInstalledTag') : t('st.ocrNotInstalled') })}
          </span>
          <button type="button" className="quiet" onClick={installOcr} disabled={installingOcr || busy}>
            <span className="btn-icon"><Ico n="download" /> {info.ocrReady ? t('st.reinstallOcr') : t('st.installOcr')}</span>
          </button>
        </div>
        <span className="field-hint">
          {t('st.ocrHint')}
        </span>
        {ocrLog ? <ConsoleLog title={t('st.ocrInstallTitle')} log={ocrLog} running={installingOcr} /> : null}
      </div>
    </section>
  );
}

// modelClasses decodes a model row's JSON class list (best-effort).
function modelClasses(model) {
  try {
    const parsed = JSON.parse(model?.classes || '[]');
    return Array.isArray(parsed) ? parsed : [];
  } catch (_) { return []; }
}

// DetectionModelsPanel is the custom-model registry (relocated from the retired
// Training page): import a trained best.pt, activate/deactivate it (hot-swapped
// alongside the stock model), and install GPU training dependencies. Deliberately
// form-free — it renders inside the runtime settings <form>.
function DetectionModelsPanel({ authHeader, onMessage, onModelActivated }) {
  const t = useT();
  const [models, setModels] = useState([]);
  const [capability, setCapability] = useState(null);
  const [modelDraft, setModelDraft] = useState({ name: '', classes: '' });
  const [installing, setInstalling] = useState(false);
  const [installLog, setInstallLog] = useState('');
  const [busy, setBusy] = useState(false);
  const fileRef = useRef(null);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  const notify = (msg, kind) => { if (onMessage) onMessage(msg, kind); };

  async function loadModels() {
    try {
      const result = await api('/api/training/models');
      setModels(Array.isArray(result?.items) ? result.items : []);
    } catch (_) { /* best effort */ }
  }
  async function loadCapability() {
    try { setCapability(await api('/api/training/capability')); } catch (_) { /* best effort */ }
  }
  useEffect(() => { loadModels(); loadCapability(); }, [authHeader]);

  // Poll while any model is training so status/progress stays live (a run kicked
  // off elsewhere — e.g. the Teach wizard — surfaces here too).
  const trainingActive = models.some((m) => m.status === 'pending' || m.status === 'running');
  useEffect(() => {
    if (!trainingActive) return undefined;
    const id = window.setInterval(() => { loadModels(); }, 4000);
    return () => window.clearInterval(id);
  }, [trainingActive, authHeader]);

  async function run(action, okMessage) {
    setBusy(true);
    try {
      await action();
      if (okMessage) notify(okMessage);
    } catch (err) {
      notify(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  // Kick off the in-app GPU dependency install, then poll status until it ends.
  function installDeps() {
    run(async () => {
      const st = await api('/api/training/setup-deps', { method: 'POST' });
      setInstallLog(st?.log || '');
      setInstalling(true);
    }, t('tr.installingGpu'));
  }
  useEffect(() => {
    if (!installing) return undefined;
    const id = window.setInterval(async () => {
      try {
        const st = await api('/api/training/setup-deps');
        setInstallLog(st?.log || '');
        if (!st?.running) {
          window.clearInterval(id);
          setInstalling(false);
          await loadCapability();
          notify(st?.status === 'done' ? t('tr.gpuInstalled') : t('tr.gpuInstallErr'), st?.status === 'done' ? 'success' : 'error');
        }
      } catch (_) { /* keep polling */ }
    }, 3000);
    return () => window.clearInterval(id);
  }, [installing, authHeader]);

  function importModel() {
    const file = fileRef.current?.files?.[0];
    if (!file) { notify(t('tr.choosePt'), 'error'); return; }
    run(async () => {
      const form = new FormData();
      form.append('file', file);
      form.append('name', modelDraft.name);
      form.append('classes', modelDraft.classes);
      const model = await api('/api/training/models/import', { method: 'POST', body: form });
      setModelDraft({ name: '', classes: '' });
      if (fileRef.current) fileRef.current.value = '';
      await loadModels();
      const cls = modelClasses(model);
      notify(cls.length ? t('tr.modelImportedCls', { cls: cls.join(', ') }) : t('tr.modelImported'));
    });
  }

  function activateModel(id) {
    run(async () => {
      const model = await api(`/api/training/models/${id}/activate`, { method: 'POST' });
      await loadModels();
      // Refresh the app-level class registry so the model's classes appear in the
      // rule "Detect" picker (and the Object Classes list) without a reload.
      if (onModelActivated) onModelActivated();
      const cls = modelClasses(model);
      notify(cls.length ? t('tr.modelActivatedCls', { cls: cls.join(', ') }) : t('tr.modelActivated'));
    });
  }

  function deactivateModel() {
    run(async () => {
      await api('/api/training/models/deactivate', { method: 'POST' });
      await loadModels();
      if (onModelActivated) onModelActivated();
    }, t('tr.modelDeactivated'));
  }

  function deleteModel(id) {
    run(async () => {
      await api(`/api/training/models/${id}`, { method: 'DELETE' });
      await loadModels();
    }, t('tr.modelDeleted'));
  }

  const active = models.find((m) => m.isActive);
  const activeCls = active ? modelClasses(active) : [];

  return (
    <section className="settings-panel span-two">
      <header>
        <h2>
          <FieldTitle info={t('tr.modelsHint')}>
            {t('st.customModels')}
          </FieldTitle>
        </h2>
        {capability ? (
          <span className={`status-pill ${capability.cuda ? 'ok' : ''}`}>
            {capability.cuda ? t('tr.gpuReady') : capability.available ? t('tr.cpuOnly') : t('tr.unavailable')}
          </span>
        ) : null}
      </header>
      {active ? (
        <div className="training-active-banner">
          <Ico n="check-ok" /> {t('tr.activeRunning', { name: active.name })}
          {activeCls.length ? t('tr.activeUseClasses', { cls: activeCls.join(', ') }) : null}
        </div>
      ) : null}
      {capability && capability.canInstall && !capability.cuda ? (
        <div className="install-deps">
          <div className="action-row">
            <button type="button" onClick={installDeps} disabled={busy || installing}>
              <span className="btn-icon">
                <Ico n={installing ? 'refresh' : 'download'} /> {installing ? t('tr.installingGpuBtn') : t('tr.installGpuBtn')}
              </span>
            </button>
            <span className="field-hint">{t('tr.detectedGpu', { gpu: capability.gpu })}</span>
          </div>
          {installing ? <span className="field-hint">{t('tr.downloadingInstalling')}</span> : null}
          {installLog ? <ConsoleLog title={t('tr.gpuInstallTitle')} log={installLog} running={installing} /> : null}
        </div>
      ) : null}
      <ul className="class-registry-list">
        {models.map((m) => (
          <li key={m.id} className="class-registry-row">
            <div>
              <strong>{m.name}</strong>
              {m.isActive ? <span className="status-pill ok"> {t('tr.activePill')}</span> : null}
              <span className="field-hint">
                {' '}{m.status}{m.status === 'running' ? ` · ${m.progress || 0}%` : ''} · {modelClasses(m).join(', ') || t('tr.noClasses')}
                {(() => { try { const mt = JSON.parse(m.metrics || '{}'); return mt.map50 ? ` · mAP50 ${(mt.map50 * 100).toFixed(0)}%` : (mt.error ? ` · ${mt.error}` : ''); } catch (_) { return ''; } })()}
              </span>
              {m.status === 'running' ? (
                <div className="training-progress"><div className="training-progress-bar" style={{ width: `${m.progress || 0}%` }} /></div>
              ) : null}
            </div>
            <div className="class-registry-actions">
              {!m.isActive && m.status !== 'running' && m.status !== 'pending' ? (
                <button type="button" onClick={() => activateModel(m.id)} disabled={busy || !m.filePath}>{t('tr.activate')}</button>
              ) : null}
              {m.isActive ? (
                <button type="button" className="quiet" onClick={deactivateModel} disabled={busy} title={t('tr.deactivateTitle')}>{t('tr.deactivate')}</button>
              ) : null}
              <button type="button" className="quiet danger" onClick={() => deleteModel(m.id)} disabled={busy || m.isActive || m.status === 'running'} title={m.isActive ? t('tr.deactivateFirst') : t('tr.deleteModel')}>{t('common.delete')}</button>
            </div>
          </li>
        ))}
        {models.length === 0 ? <li className="field-hint">{t('st.noModelsImport')}</li> : null}
      </ul>
      <div className="vision-rule-form">
        <header><h3>{t('tr.importTrainedModel')}</h3></header>
        <div className="metadata-row">
          <label>
            {t('common.name')}
            <input value={modelDraft.name} onChange={(e) => setModelDraft({ ...modelDraft, name: e.target.value })} placeholder={t('tr.phCouriersV1')} disabled={busy} />
          </label>
          <label>
            {t('tr.classesOptional')}
            <input value={modelDraft.classes} onChange={(e) => setModelDraft({ ...modelDraft, classes: e.target.value })} placeholder={t('tr.phAutoDetected')} disabled={busy} />
          </label>
        </div>
        <label>
          {t('tr.weightsFile')}
          <input ref={fileRef} type="file" accept=".pt" disabled={busy} />
          <span className="field-hint">{t('tr.weightsHint')}</span>
        </label>
        <div className="action-row">
          <button type="button" onClick={importModel} disabled={busy || !modelDraft.name.trim()}>
            <span className="btn-icon"><Ico n="save" /> {t('tr.importModel')}</span>
          </button>
        </div>
      </div>
    </section>
  );
}

// FileBrowserModal is a server-side directory picker. It navigates the host
// filesystem via GET /settings/fs/browse, lets the user drill into folders and
// click a file to select it, and returns the chosen absolute path. Used to pick the
// ffmpeg binary without typing a path. Admin-gated server-side.
function FileBrowserModal({ authHeader, initialPath, onSelect, onClose }) {
  const t = useT();
  const [listing, setListing] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function browse(path) {
    setLoading(true);
    setError(null);
    try {
      const headers = {};
      if (authHeader) headers.Authorization = authHeader;
      const resp = await fetch(`${apiBase()}/api/settings/fs/browse?path=${encodeURIComponent(path || '')}`, { credentials: 'include', headers });
      const text = await resp.text();
      let payload = null;
      if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
      if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
      setListing(payload?.data?.result ?? payload?.result ?? payload);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }
  // Seed from the directory of the current value so the user starts somewhere useful.
  useEffect(() => { browse(initialPath || ''); /* eslint-disable-next-line */ }, []);

  const entries = listing?.entries || [];
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card file-browser" onClick={(e) => e.stopPropagation()}>
        <header className="file-browser-head">
          <h2><span className="btn-icon"><Ico n="folder" /> {t('st.chooseFfmpeg')}</span></h2>
          <button type="button" className="icon-btn" aria-label={t('common.close')} onClick={onClose}><Ico n="x" /></button>
        </header>
        <div className="file-browser-path" title={listing?.path || ''}>{listing?.path || t('st.allowedLocations')}</div>
        <div className="file-browser-list">
          {loading ? (
            <p className="field-hint">{t('common.loading')}</p>
          ) : error ? (
            <p className="field-hint danger-text">{error}</p>
          ) : (
            <ul>
              {listing && listing.parent !== undefined ? (
                <li>
                  <button type="button" className="file-row" onClick={() => browse(listing.parent)} disabled={!listing.path}>
                    <span className="btn-icon"><Ico n="arr-up" /> ..</span>
                  </button>
                </li>
              ) : null}
              {entries.map((item) => (
                <li key={item.path}>
                  <button
                    type="button"
                    className={`file-row${item.dir ? ' is-dir' : ''}`}
                    onClick={() => (item.dir ? browse(item.path) : onSelect(item.path))}
                  >
                    <span className="btn-icon"><Ico n={item.dir ? 'folder' : 'film'} /> {item.name}</span>
                  </button>
                </li>
              ))}
              {entries.length === 0 ? <li><span className="field-hint">{t('st.emptyFolder')}</span></li> : null}
            </ul>
          )}
        </div>
        <p className="field-hint file-browser-hint">{t('st.browserHint')}</p>
      </div>
    </div>
  );
}

// FfmpegInstallControl shows whether a usable ffmpeg is available and, when it is
// not found, offers an in-app download (the same background installer the first-run
// wizard uses). ffmpeg is required for live view, recording, and AI frame capture,
// so this lets an operator fix a missing video engine without leaving Settings.
// Endpoints: GET /decoder/status, POST /decoder/ffmpeg/install (+ /install/status).
function FfmpegInstallControl({ authHeader, value, onChangePath, onMessage, onRestart, onInstalledPath }) {
  const t = useT();
  const [status, setStatus] = useState(undefined); // undefined = loading
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState(false);
  const [failure, setFailure] = useState(null);
  const [browsing, setBrowsing] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function check() {
    try { setStatus(await api('/api/settings/decoder/status')); }
    catch (_) { setStatus(null); }
  }
  useEffect(() => { check(); }, [authHeader]);

  async function download() {
    setInstalling(true);
    setFailure(null);
    if (onMessage) onMessage(t('st.downloadingFfmpeg'), 'info');
    try {
      await api('/api/settings/decoder/ffmpeg/install', { method: 'POST' });
      // Poll the background job until it finishes.
      const deadline = Date.now() + 180000;
      let state = null;
      for (;;) {
        state = await api('/api/settings/decoder/ffmpeg/install/status');
        if (state?.status === 'done' || state?.status === 'failed') break;
        if (Date.now() > deadline) { state = { status: 'failed', log: 'Timed out.' }; break; }
        await new Promise((r) => setTimeout(r, 1500));
      }
      if (state?.status === 'done') {
        setInstalled(true);
        if (state.path && onInstalledPath) onInstalledPath(state.path);
        if (onMessage) onMessage(t('st.ffmpegInstalled'));
        await check();
      } else {
        setFailure(state || { status: 'failed' });
        if (onMessage) onMessage(t('st.ffmpegInstallFailed', { log: state?.log || 'unknown error' }), 'error');
      }
    } catch (err) {
      setFailure({ status: 'failed', log: err.message });
      if (onMessage) onMessage(err.message);
    } finally {
      setInstalling(false);
    }
  }

  // Status icon shown between the input and Check button. The detail (version,
  // path, or why it matters) lives in a hover/focus balloon so the row stays tidy.
  const found = status?.found;
  const tip = status === undefined ? t('st.checkingFfmpeg')
    : found ? `${t('st.ffmpegFound')}${status.version ? ` — ${status.version}` : status.path ? ` — ${status.path}` : ''}${installed ? t('st.restartToApply') : ''}`
    : t('st.ffmpegNotFound');
  const iconState = status === undefined ? 'pending' : found ? 'ok' : 'bad';

  return (
    <>
      <div className="ffmpeg-input-row">
        <div className="input-with-icon">
          <input
            value={value}
            onChange={(e) => onChangePath(e.target.value)}
            placeholder="ffmpeg"
            autoComplete="off"
          />
          <button
            type="button"
            className="input-icon-btn"
            onClick={() => setBrowsing(true)}
            title={t('st.browseFfmpeg')}
            aria-label={t('st.browseFfmpeg')}
          >
            <Ico n="folder" sz={15} />
          </button>
        </div>
        <span
          className={`ffmpeg-status-icon ${iconState}`}
          data-tip={tip}
          tabIndex={0}
          role="img"
          aria-label={tip}
        >
          <Ico n={status === undefined ? 'reload' : found ? 'check-ok' : 'x'} sz={15} />
        </span>
        <button type="button" className="quiet ffmpeg-check-btn" onClick={check} disabled={installing}>
          <span className="btn-icon"><Ico n="reload" /> {t('st.check')}</span>
        </button>
      </div>
      {(status && !status.found && !installed) || (installed && onRestart) || failure ? (
        <div className="ffmpeg-status-actions">
          {status && !status.found && !installed ? (
            <button type="button" onClick={download} disabled={installing}>
              <span className="btn-icon"><Ico n="download" /> {installing ? t('rec.downloading') : t('st.downloadFfmpeg')}</span>
            </button>
          ) : null}
          {installed && onRestart ? (
            <button type="button" onClick={() => onRestart()} disabled={installing}>
              <span className="btn-icon"><Ico n="reload" /> {t('st.restartNow')}</span>
            </button>
          ) : null}
          {failure ? (
            <p className="field-hint danger-text ffmpeg-status-error">
              {failure.supported === false
                ? t('st.dlUnavailablePlatform')
                : t('st.dlFailed')}
            </p>
          ) : null}
        </div>
      ) : null}
      {browsing ? (
        <FileBrowserModal
          authHeader={authHeader}
          initialPath={value}
          onSelect={(path) => { onChangePath(path); setBrowsing(false); }}
          onClose={() => setBrowsing(false)}
        />
      ) : null}
    </>
  );
}

// AiRuntimeInstallControl installs a self-contained AI Python runtime (Python +
// GPU/CPU torch + ultralytics) in-app — the heavy dependency that isn't bundled.
// It mirrors the ffmpeg installer: a button starts a background job and the live
// pip log streams into a scrolling box (the torch download can be 200MB–2.5GB).
// Endpoints: GET /vision/ai-runtime/status, POST /vision/ai-runtime/install (+ /install/status).
function AiRuntimeInstallControl({ authHeader, onMessage, onRestart }) {
  const t = useT();
  const [status, setStatus] = useState(undefined); // undefined = loading
  const [installing, setInstalling] = useState(false);
  const [log, setLog] = useState('');
  const [done, setDone] = useState(false);
  const [supported, setSupported] = useState(true);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function check() {
    try { setStatus(await api('/api/settings/vision/ai-runtime/status')); }
    catch (_) { setStatus(null); }
  }
  useEffect(() => { check(); }, [authHeader]);

  async function install() {
    setInstalling(true);
    setDone(false);
    setLog('');
    if (onMessage) onMessage(t('st.aiRtInstalling'), 'info');
    try {
      const start = await api('/api/settings/vision/ai-runtime/install', { method: 'POST' });
      if (start && start.supported === false) { setSupported(false); throw new Error(t('st.dlUnavailablePlatform')); }
      // Long-running (torch is large): poll up to 45 min, streaming the log.
      const deadline = Date.now() + 45 * 60 * 1000;
      let state = null;
      for (;;) {
        state = await api('/api/settings/vision/ai-runtime/install/status');
        setLog(state?.log || '');
        if (state?.status === 'done' || state?.status === 'failed') break;
        if (Date.now() > deadline) { state = { status: 'failed', log: (state?.log || '') + '\nTimed out.' }; break; }
        await new Promise((r) => setTimeout(r, 2500));
      }
      if (state?.status === 'done') {
        setDone(true);
        if (onMessage) onMessage(t('st.aiRtInstalled'));
        await check();
      } else if (onMessage) {
        onMessage(t('st.aiRtInstallFailed'), 'error');
      }
    } catch (err) {
      setLog((prev) => prev + '\n' + err.message);
      if (onMessage) onMessage(err.message, 'error');
    } finally {
      setInstalling(false);
    }
  }

  const found = status?.found;
  return (
    <div className="ai-runtime-control">
      <div className="ai-runtime-row">
        <span className={`ffmpeg-status-icon ${status === undefined ? 'pending' : found ? 'ok' : 'bad'}`}>
          <Ico n={status === undefined ? 'reload' : found ? 'check-ok' : 'x'} sz={15} />
        </span>
        <span className="ai-runtime-summary">
          {status === undefined
            ? t('st.aiRtChecking')
            : found
              ? t('st.aiRtReady', { torch: status.torch || '?', accel: status.cuda ? 'CUDA' : 'CPU' })
              : t('st.aiRtMissing')}
        </span>
        <button type="button" className="quiet" onClick={check} disabled={installing}>
          <span className="btn-icon"><Ico n="reload" /> {t('st.check')}</span>
        </button>
      </div>
      {!found && supported ? (
        <p className="field-hint">{t('st.aiRtHint')}</p>
      ) : null}
      <div className="ffmpeg-status-actions">
        {!found && supported ? (
          <button type="button" onClick={install} disabled={installing}>
            <span className="btn-icon"><Ico n="download" /> {installing ? t('rec.downloading') : t('st.aiRtInstall')}</span>
          </button>
        ) : null}
        {done && onRestart ? (
          <button type="button" onClick={() => onRestart()} disabled={installing}>
            <span className="btn-icon"><Ico n="reload" /> {t('st.restartNow')}</span>
          </button>
        ) : null}
      </div>
      {log ? <pre className="install-output ai-runtime-log">{log}</pre> : null}
    </div>
  );
}

// UpdatePanel surfaces the self-update state: current vs latest version (checked on a
// schedule server-side), and — on portable/installer installs — a one-click apply that
// downloads, verifies, swaps, and restarts. Package/Docker installs get guidance
// instead. Endpoints: GET /system/update, POST /system/update/check, POST /system/update/apply.
function UpdatePanel({ authHeader, onRestart }) {
  const t = useT();
  const [info, setInfo] = useState(undefined); // undefined = loading
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [log, setLog] = useState('');

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function load() { try { setInfo(await api('/api/system/update')); } catch (_) { setInfo(null); } }
  useEffect(() => { load(); }, [authHeader]);

  async function check() {
    setChecking(true);
    try { setInfo(await api('/api/system/update/check', { method: 'POST' })); }
    catch (_) { /* keep prior */ }
    finally { setChecking(false); }
  }

  async function apply() {
    setApplying(true);
    setLog('');
    try {
      await api('/api/system/update/apply', { method: 'POST' });
      // Poll until done/failed. On success the server restarts (requests then fail).
      const deadline = Date.now() + 10 * 60 * 1000;
      for (;;) {
        let state;
        try { state = await api('/api/system/update'); }
        catch (_) { setLog((p) => p + '\nServer restarting…'); break; }
        setLog(state?.applyLog || '');
        if (state?.applyStatus === 'done') { if (onRestart) onRestart(); break; }
        if (state?.applyStatus === 'failed') break;
        if (Date.now() > deadline) break;
        await new Promise((r) => setTimeout(r, 2000));
      }
    } catch (err) {
      setLog((p) => p + '\n' + err.message);
    } finally {
      setApplying(false);
    }
  }

  if (info === undefined) return null;

  const available = info && info.updateAvailable;
  const managedNote = info?.managed === 'package'
    ? t('st.updManagedPackage')
    : info?.managed === 'docker'
      ? t('st.updManagedDocker')
      : t('st.updManualNote');

  return (
    <section className="settings-panel span-two">
      <header>
        <h2><span className="btn-icon"><Ico n="download" /> {t('st.updates')}</span></h2>
        <button type="button" className="quiet" onClick={check} disabled={checking || applying}>
          <span className="btn-icon"><Ico n="reload" /> {checking ? t('st.updChecking') : t('st.updCheck')}</span>
        </button>
      </header>
      {!info ? (
        <p className="settings-hint">{t('st.updUnavailable')}</p>
      ) : available ? (
        <>
          <p className="update-available">
            <strong className="status-pill online">{t('st.updAvailable', { version: info.latest })}</strong>
            <span className="field-hint"> {t('st.updCurrent', { version: info.current })}</span>
          </p>
          {info.canSelfUpdate ? (
            <div className="ffmpeg-status-actions">
              <button type="button" onClick={apply} disabled={applying}>
                <span className="btn-icon"><Ico n="download" /> {applying ? t('st.updApplying') : t('st.updApply', { version: info.latest })}</span>
              </button>
            </div>
          ) : (
            <p className="settings-hint">{managedNote}{info.htmlUrl ? <> <a href={info.htmlUrl} target="_blank" rel="noreferrer">{t('st.updReleaseNotes')}</a></> : null}</p>
          )}
          {log ? <pre className="install-output ai-runtime-log">{log}</pre> : null}
        </>
      ) : (
        <p className="settings-hint">
          <strong className="status-pill online">{t('st.updUpToDate')}</strong>
          <span className="field-hint"> v{info.current}</span>
        </p>
      )}
    </section>
  );
}

// SystemStatusPanel shows the running software version and live service health by
// polling the public version/health/liveness/readiness endpoints. It is read-only:
// it confirms what build is running and whether the API, database, cache, and the
// app's own monitors report healthy. Endpoints:
//   GET /api/version  — runtime version (SendResult envelope)
//   GET /api/health   — API namespaces health  {"ok": true}
//   GET /health       — service liveness        {"alive": true}
//   GET /ready        — service readiness        {"ok", "db", "cache", ...advisory}
function SystemStatusPanel({ authHeader, onRestart }) {
  const t = useT();
  const [restarting, setRestarting] = useState(false);
  const [version, setVersion] = useState(null);
  const [apiHealth, setApiHealth] = useState(null);
  const [live, setLive] = useState(null);
  const [ready, setReady] = useState(null);
  const [busy, setBusy] = useState(false);
  const [checkedAt, setCheckedAt] = useState(null);

  async function probe(path) {
    const headers = {};
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, status: resp.status, body };
  }

  async function load() {
    setBusy(true);
    const fail = (e) => ({ ok: false, status: 0, body: { message: e?.message || 'unreachable' } });
    const [v, h, l, r] = await Promise.all([
      probe('/api/version').catch(fail),
      probe('/api/health').catch(fail),
      probe('/health').catch(fail),
      probe('/ready').catch(fail),
    ]);
    setVersion(v); setApiHealth(h); setLive(l); setReady(r);
    setCheckedAt(new Date());
    setBusy(false);
  }
  useEffect(() => { load(); }, [authHeader]);

  function pill(ok, label) {
    return <strong className={`status-pill ${ok ? 'online' : 'offline'}`}>{label}</strong>;
  }
  // readyExtras surfaces the advisory keys (db, cache, machine, cameras, …) that
  // /ready merges in beyond the ok verdict, so operators see what's degraded.
  const readyExtras = ready?.body && typeof ready.body === 'object'
    ? Object.entries(ready.body).filter(([k]) => k !== 'ok')
    : [];
  const ver = version?.ok && version?.body && typeof version.body === 'object' ? version.body : null;

  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> {t('st.softwareVersion')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={busy || restarting}>
              <span className="btn-icon"><Ico n="reload" /> {t('common.refresh')}</span>
            </button>
            {onRestart ? (
              <button type="button" className="quiet" onClick={() => { setRestarting(true); onRestart(); }} disabled={restarting}>
                <span className="btn-icon"><Ico n="reload" /> {restarting ? t('st.restarting') : t('st.restartApp')}</span>
              </button>
            ) : null}
          </div>
        </header>
        {ver ? (
          <div className="machine-metrics">
            <div className="machine-metric-card">
              <dt>{t('st.application')}</dt>
              <dd><strong className="status-pill">{ver.app || '—'}</strong></dd>
              <span className="field-hint">v{ver.appVersion || '—'}</span>
            </div>
            <div className="machine-metric-card">
              <dt>{t('st.sharedCore')}</dt>
              <dd><strong className="status-pill">v{ver.coreVersion || '—'}</strong></dd>
            </div>
            {ver.commit ? (
              <div className="machine-metric-card">
                <dt>{t('st.commit')}</dt>
                <dd><strong className="status-pill">{String(ver.commit).slice(0, 12)}</strong></dd>
              </div>
            ) : null}
            {ver.updatedAt ? (
              <div className="machine-metric-card">
                <dt>{t('st.built')}</dt>
                <dd><strong className="status-pill">{ver.updatedAt}</strong></dd>
              </div>
            ) : null}
          </div>
        ) : (
          <p className="settings-hint">{t('st.versionUnavailable')}{version?.body?.message ? ` — ${version.body.message}` : ''}.</p>
        )}
      </section>

      <UpdatePanel authHeader={authHeader} onRestart={onRestart} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> {t('st.serviceHealth')}</span></h2>
          {checkedAt ? <span className="field-hint">{t('st.checkedAt', { time: checkedAt.toLocaleTimeString() })}</span> : null}
        </header>
        <p className="settings-hint">
          {t('st.healthHint')}
        </p>
        <div className="machine-metrics">
          <div className="machine-metric-card">
            <dt>{t('st.apiNamespaces')}</dt>
            <dd>{pill(apiHealth?.ok && apiHealth?.body?.ok !== false, apiHealth?.ok ? t('st.ok') : t('st.down'))}</dd>
            <span className="field-hint">/api/health</span>
          </div>
          <div className="machine-metric-card">
            <dt>{t('st.liveness')}</dt>
            <dd>{pill(live?.ok && live?.body?.alive !== false, live?.ok ? t('st.alive') : t('st.down'))}</dd>
            <span className="field-hint">/health</span>
          </div>
          <div className="machine-metric-card">
            <dt>{t('st.readiness')}</dt>
            <dd>{pill(ready?.ok && ready?.body?.ok === true, ready?.ok && ready?.body?.ok === true ? t('st.ready') : t('st.notReady'))}</dd>
            <span className="field-hint">/ready</span>
          </div>
          {readyExtras.map(([key, val]) => (
            <div className="machine-metric-card" key={key}>
              <dt>{key.charAt(0).toUpperCase() + key.slice(1)}</dt>
              <dd>{pill(String(val).toLowerCase() === 'up' || String(val).toLowerCase() === 'ok', String(val))}</dd>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

// PairingPanel manages the node's relationship to a myseliasan control plane:
// setting the shared fleet key, generating a short-lived claim code an operator
// enters in the control plane to adopt this node, and unpairing (self-drop) once
// adopted. A node is discoverable on the LAN only while unpaired AND a fleet key
// is set; adoption locks it to a single parent and silences discovery.
function PairingPanel({ authHeader }) {
  const t = useT();
  const [status, setStatus] = useState(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState(null);
  const [fleetKey, setFleetKey] = useState('');
  const [claim, setClaim] = useState(null);
  const [claimCopied, setClaimCopied] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, status: resp.status, body };
  }

  async function load(quiet) {
    if (!quiet) setBusy(true);
    const r = await api('/api/pairing/status').catch(() => ({ ok: false }));
    if (r.ok) {
      setStatus((prev) => {
        // Surface the moment the control plane adopts (or releases) this node so
        // an operator watching this page sees it without a manual refresh.
        if (prev && prev.paired !== r.body?.paired) {
          if (r.body?.paired) {
            setMsg({ kind: 'ok', text: t('st.adoptedBy', { parent: r.body.parentName || r.body.parentId || t('st.aControlPlane') }) });
            setClaim(null);
          } else {
            setMsg({ kind: 'ok', text: t('st.releasedDiscoverable') });
          }
        }
        return r.body;
      });
    }
    if (!quiet) setBusy(false);
  }
  // Load on mount, then poll quietly so adoption/release initiated from the control
  // plane is reflected here automatically (the page "refreshes" itself).
  useEffect(() => {
    load();
    const id = window.setInterval(() => load(true), 4000);
    return () => window.clearInterval(id);
    /* eslint-disable-next-line */
  }, [authHeader]);

  async function saveFleetKey() {
    if (fleetKey.trim().length < 16) { setMsg({ kind: 'error', text: t('st.fleetKeyMin') }); return; }
    setBusy(true);
    const r = await api('/api/pairing/fleet-key', { method: 'PUT', body: JSON.stringify({ key: fleetKey.trim() }) });
    setBusy(false);
    if (r.ok) { setMsg({ kind: 'ok', text: t('st.fleetKeySaved') }); setFleetKey(''); load(); }
    else setMsg({ kind: 'error', text: r.body?.message || t('st.fleetKeySaveFail') });
  }

  async function generateClaim() {
    setBusy(true);
    const r = await api('/api/pairing/claim-code', { method: 'POST' });
    setBusy(false);
    if (r.ok) { setClaim(r.body); setMsg(null); }
    else setMsg({ kind: 'error', text: r.body?.message || t('st.claimGenFail') });
  }

  async function copyClaim() {
    if (!claim?.code) return;
    try {
      await navigator.clipboard.writeText(claim.code);
      setClaimCopied(true);
      setTimeout(() => setClaimCopied(false), 2000);
    } catch (_) {
      setMsg({ kind: 'error', text: t('st.copyManually') });
    }
  }

  async function unpair() {
    if (!window.confirm(t('st.unpairConfirm'))) return;
    setBusy(true);
    const r = await api('/api/pairing/unpair', { method: 'POST' });
    setBusy(false);
    if (r.ok) { setMsg({ kind: 'ok', text: t('st.unpaired') }); setClaim(null); load(); }
    else setMsg({ kind: 'error', text: r.body?.message || t('st.unpairFail') });
  }

  const paired = !!status?.paired;
  const fleetKeySet = !!status?.fleetKeySet;
  const claimExpiry = claim?.expiresAt ? new Date(claim.expiresAt * 1000).toLocaleTimeString() : null;

  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> {t('st.controlPlane')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('common.refresh')}</span>
            </button>
          </div>
        </header>
        {msg ? <p className={msg.kind === 'error' ? 'settings-hint danger-text' : 'settings-hint'}>{msg.text}</p> : null}
        <div className="machine-metrics">
          <div className="machine-metric-card">
            <dt>{t('common.status')}</dt>
            <dd><strong className={`status-pill ${paired ? 'online' : ''}`}>{paired ? t('st.paired') : t('st.unpairedPill')}</strong></dd>
          </div>
          <div className="machine-metric-card">
            <dt>{t('st.discoverable')}</dt>
            <dd><strong className={`status-pill ${status?.discoverable ? 'online' : 'offline'}`}>{status?.discoverable ? t('common.yes') : t('common.no')}</strong></dd>
            <span className="field-hint">{paired ? t('st.silenced') : (fleetKeySet ? t('st.answeringProbes') : t('st.setFleetFirst'))}</span>
          </div>
          <div className="machine-metric-card">
            <dt>{t('st.nodeId')}</dt>
            <dd><strong className="status-pill">{status?.nodeId ? String(status.nodeId).slice(0, 8) : '—'}</strong></dd>
            <span className="field-hint">{status?.name || ''}</span>
          </div>
        </div>
      </section>

      {paired ? (
        <section className="settings-panel span-two">
          <header><h2><span className="btn-icon"><Ico n="shield" /> {t('st.pairedParent')}</span></h2></header>
          <p className="settings-hint">
            {t('st.pairedParentHint')}
          </p>
          <div className="machine-metrics">
            <div className="machine-metric-card">
              <dt>{t('st.parent')}</dt>
              <dd><strong className="status-pill">{status?.parentName || status?.parentId || '—'}</strong></dd>
              <span className="field-hint">{status?.parentBaseUrl || ''}</span>
            </div>
            {status?.pairedAt ? (
              <div className="machine-metric-card">
                <dt>{t('st.pairedSince')}</dt>
                <dd><strong className="status-pill">{new Date(status.pairedAt * 1000).toLocaleString()}</strong></dd>
              </div>
            ) : null}
          </div>
          <div className="settings-actions">
            <button type="button" className="danger" onClick={unpair} disabled={busy}>
              <span className="btn-icon"><Ico n="x" /> {t('st.unpairSelfDrop')}</span>
            </button>
          </div>
        </section>
      ) : (
        <>
          <section className="settings-panel span-two">
            <header><h2><span className="btn-icon"><Ico n="key" /> {t('st.fleetKey')}</span></h2></header>
            <p className="settings-hint">
              {t('st.fleetKeyHint')}{fleetKeySet ? t('st.fleetKeyReplaceHint') : ''}
            </p>
            <div className="settings-field-grid">
              <label>
                {fleetKeySet ? t('st.fleetKeySetLabel') : t('st.fleetKeyNotSetLabel')}
                <input
                  type="password"
                  value={fleetKey}
                  onChange={(e) => setFleetKey(e.target.value)}
                  placeholder={fleetKeySet ? t('st.fleetKeyPhSet') : t('st.fleetKeyPhUnset')}
                  disabled={busy}
                  autoComplete="off"
                />
              </label>
            </div>
            <div className="settings-actions">
              <button type="button" onClick={saveFleetKey} disabled={busy || fleetKey.trim().length < 16}>
                <span className="btn-icon"><Ico n="save" /> {t('st.saveFleetKey')}</span>
              </button>
            </div>
          </section>

          <section className="settings-panel span-two">
            <header><h2><span className="btn-icon"><Ico n="check-ok" /> {t('st.claimCode')}</span></h2></header>
            <p className="settings-hint">
              {t('st.claimHint')}
            </p>
            {claim ? (
              <div className="machine-metrics">
                <div className="machine-metric-card">
                  <dt>{t('st.claimCode')}</dt>
                  <dd style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <strong className="status-pill online" style={{ letterSpacing: 2, fontSize: '1.2em' }}>{claim.code}</strong>
                    <button
                      type="button"
                      className="quiet"
                      onClick={copyClaim}
                      title={t('st.copyClaim')}
                      aria-label={t('st.copyClaim')}
                      style={{ padding: 4, lineHeight: 0 }}
                    >
                      <Ico n={claimCopied ? 'check-ok' : 'copy'} sz={15} />
                    </button>
                  </dd>
                  {claimExpiry ? <span className="field-hint">{t('st.expiresAt', { time: claimExpiry })}</span> : null}
                </div>
              </div>
            ) : null}
            <div className="settings-actions">
              <button type="button" onClick={generateClaim} disabled={busy || !fleetKeySet}>
                <span className="btn-icon"><Ico n="refresh" /> {t('st.generateClaim')}</span>
              </button>
              {!fleetKeySet ? <span className="field-hint">{t('st.setFleetFirstShort')}</span> : null}
            </div>
          </section>
        </>
      )}
    </div>
  );
}

export function SettingsTab({
  settingsNav,
  settings,
  authHeader,
  onMessage,
  busy,
  hasChanges,
  onChange,
  onSettingsNav,
  onSave,
  onDiscard,
  onReset,
  onAutoTune,
  autoTuneResult,
  onRestart,
  onCaptureAutoConfig,
  gpuDevices,
  onCheckVisionTool,
  visionToolStatus,
  onInstallPackages,
  visionInstallResult,
  focusUsername,
  notificationSettings,
  savedNotificationSettings,
  onNotificationChange,
  onSaveDestination,
  onDeleteDestination,
  onSaveRetention,
  onTestNotification,
  onPurgeNotifications,
  healthSettings,
  healthHasChanges,
  onHealthChange,
  onSaveHealth,
  onDiscardHealth,
  machineHealthSettings,
  machineHealthHasChanges,
  onMachineHealthChange,
  onSaveMachineHealth,
  onDiscardMachineHealth,
  machineMetrics,
  onRefreshMachineMetrics,
  capacity,
  onEstimateCapacity,
  onCalibrateCapacity,
  resetAllowed,
  onSecureWipe,
  onModelActivated,
  visionClasses,
  visionLabels,
  activeModelClasses,
  onSaveClass,
  onDeleteClass,
}) {
  const t = useT();
  const iceServers = settings.stream.webrtc.iceServers || [];
  const capture = { ...defaultCaptureConfig, ...(settings.vision?.capture || {}),
    standalone: { ...defaultCaptureConfig.standalone, ...(settings.vision?.capture?.standalone || {}) },
    siphon: { ...defaultCaptureConfig.siphon, ...(settings.vision?.capture?.siphon || {}) } };
  const captureAuto = capture.mode === 'auto';
  const gpuDeviceOptions = Array.isArray(gpuDevices?.devices) ? gpuDevices.devices : [];
  const selectedGpuDeviceIndex = gpuDeviceOptions.findIndex(
    (item) =>
      item.value === settings.decoder.ffmpeg.hwaccelDevice &&
      (!item.hwaccel || item.hwaccel === settings.decoder.ffmpeg.hwaccel || !settings.decoder.ffmpeg.hwaccelDevice)
  );
  const gpuDeviceSelectValue =
    settings.decoder.ffmpeg.hwaccelDevice === '' ? '__default__' : selectedGpuDeviceIndex >= 0 ? String(selectedGpuDeviceIndex) : '__manual__';
  const [showManualGpuInput, setShowManualGpuInput] = useState(() => gpuDeviceSelectValue === '__manual__');
  useEffect(() => {
    if (gpuDeviceSelectValue === '__manual__') {
      setShowManualGpuInput(true);
    }
  }, [gpuDeviceSelectValue]);
  const effectiveGpuSelectValue = showManualGpuInput ? '__manual__' : gpuDeviceSelectValue;
  function update(mutator) {
    onChange(mutator(settings));
  }
  function updateIceServer(index, patch) {
    update((current) => {
      const nextServers = [...(current.stream.webrtc.iceServers || [])];
      nextServers[index] = { ...nextServers[index], ...patch };
      return {
        ...current,
        stream: {
          ...current.stream,
          webrtc: { ...current.stream.webrtc, iceServers: nextServers },
        },
      };
    });
  }
  function updateMJPEGDecoder(patch) {
    update((current) => ({
      ...current,
      decoder: {
        ...current.decoder,
        mjpeg: { ...current.decoder.mjpeg, ...patch },
      },
    }));
  }
  function updateYolo(patch) {
    update((current) => ({
      ...current,
      vision: {
        ...current.vision,
        yolo: { ...(current.vision?.yolo || defaultYoloConfig), ...patch },
      },
    }));
  }
  function updateCapture(patch) {
    update((current) => ({
      ...current,
      vision: {
        ...current.vision,
        capture: { ...(current.vision?.capture || defaultCaptureConfig), ...patch },
      },
    }));
  }
  function updateCaptureStandalone(patch) {
    update((current) => {
      const capture = current.vision?.capture || defaultCaptureConfig;
      return {
        ...current,
        vision: {
          ...current.vision,
          capture: { ...capture, standalone: { ...capture.standalone, ...patch } },
        },
      };
    });
  }
  function updateCaptureSiphon(patch) {
    update((current) => {
      const capture = current.vision?.capture || defaultCaptureConfig;
      return {
        ...current,
        vision: {
          ...current.vision,
          capture: { ...capture, siphon: { ...capture.siphon, ...patch } },
        },
      };
    });
  }
  function updateFFmpegDecoder(patch) {
    update((current) => ({
      ...current,
      decoder: {
        ...current.decoder,
        ffmpeg: { ...current.decoder.ffmpeg, ...patch },
      },
    }));
  }
  function updateRecordingStorage(patch) {
    update((current) => ({
      ...current,
      recording: {
        ...(current.recording || {}),
        storage: { ...((current.recording && current.recording.storage) || {}), ...patch },
      },
    }));
  }
  function selectGPUDevice(value) {
    if (value === '__default__') {
      updateFFmpegDecoder({ hwaccelDevice: '' });
      setShowManualGpuInput(false);
      return;
    }
    if (value === '__manual__') {
      setShowManualGpuInput(true);
      return;
    }
    setShowManualGpuInput(false);
    const option = gpuDeviceOptions[Number(value)];
    if (!option) {
      return;
    }
    updateFFmpegDecoder({
      hwaccelDevice: option.value || '',
      ...(option.hwaccel ? { hwaccel: option.hwaccel } : {}),
    });
  }

  const activeTab = SETTINGS_TABS.find((tab) => tab.id === settingsNav) || SETTINGS_TABS[0];

  return (
    <section className="workspace settings-tabbed">
      <Tabs
        ariaLabel={t('st.settingsAria')}
        active={settingsNav}
        onChange={onSettingsNav}
        tabs={SETTINGS_TABS.map((tab) => ({ id: tab.id, label: t(tab.labelKey), icon: tab.icon }))}
      />

      <header className="settings-tab-head">
        <span className="settings-tab-head-icon"><Ico n={activeTab.icon} sz={22} /></span>
        <div className="settings-tab-head-text">
          <h2>{t(activeTab.labelKey)}</h2>
          <p>{t(activeTab.descKey)}</p>
        </div>
      </header>

      <div className="settings-content">
        {(settingsNav === 'runtime' || settingsNav === 'ai') ? (
          <form className="settings-layout" onSubmit={onSave}>
            <FormBusyOverlay busy={busy} />
        {settingsNav === 'runtime' && (<>
        <section className="settings-panel span-two">
          <header>
            <h2>{t('st.decoder')}</h2>
            <button type="button" className="quiet" onClick={onAutoTune} disabled={busy}>
              <span className="btn-icon"><Ico n="wand" /> {t('st.autoTune')}</span>
            </button>
          </header>
          {autoTuneResult ? (
            <div className="auto-tune-result">
              <strong>{autoTuneResult.summary}</strong>
              {Array.isArray(autoTuneResult.observations) && autoTuneResult.observations.length > 0 ? (
                <ul>
                  {autoTuneResult.observations.map((item, index) => (
                    <li key={`auto-tune-${index}`}>{item}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
          <div className="settings-field-grid">
          <label className="field-span-two">
            <FieldTitle info="Executable used for RTSP-to-MJPEG fallback and RTSP frame capture. Leave as ffmpeg to resolve from PATH, or use an absolute service-safe path.">
              {t('st.ffmpegPath')}
            </FieldTitle>
            <FfmpegInstallControl
              authHeader={authHeader}
              value={settings.decoder.mjpeg.ffmpegPath}
              onChangePath={(path) => updateMJPEGDecoder({ ffmpegPath: path })}
              onMessage={onMessage}
              onRestart={onRestart}
              onInstalledPath={(path) => updateMJPEGDecoder({ ffmpegPath: path })}
            />
          </label>
          <label>
            <FieldTitle info="RTSP transport passed to ffmpeg. TCP is most reliable on unstable camera networks; UDP can reduce latency when packet loss is low.">
              {t('st.rtspTransport')}
            </FieldTitle>
            <select value={settings.decoder.ffmpeg.rtspTransport} onChange={(event) => updateFFmpegDecoder({ rtspTransport: event.target.value })}>
              {decoderTransportOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label>
            <FieldTitle info="Hardware acceleration mode for ffmpeg decoding — applies to live view, standalone AI capture, and the continuous detection/recording siphon decode (offloading it from the CPU). None uses CPU software decode; auto lets ffmpeg choose; platform-specific modes need matching ffmpeg build, drivers, and hardware.">
              {t('st.hardwareDecode')}
            </FieldTitle>
            <select value={settings.decoder.ffmpeg.hwaccel} onChange={(event) => updateFFmpegDecoder({ hwaccel: event.target.value })}>
              {decoderHWAccelOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label className="field-span-two">
            <FieldTitle info="Optional hardware device or GPU index passed to ffmpeg hwaccel_device, such as 0, 1, or /dev/dri/renderD128 depending on platform.">
              {t('st.gpuDevice')}
            </FieldTitle>
            <select value={effectiveGpuSelectValue} onChange={(event) => selectGPUDevice(event.target.value)}>
              <option value="__default__">{t('st.gpuDefault')}</option>
              {gpuDeviceOptions.map((item, index) => (
                <option key={`${item.kind || 'gpu'}-${index}-${item.value}`} value={String(index)}>
                  {item.label}
                </option>
              ))}
              <option value="__manual__">
                {settings.decoder.ffmpeg.hwaccelDevice && selectedGpuDeviceIndex < 0
                  ? t('st.gpuManual', { dev: settings.decoder.ffmpeg.hwaccelDevice })
                  : t('st.gpuManualEntry')}
              </option>
            </select>
            {Array.isArray(gpuDevices?.observations) && gpuDevices.observations.length > 0 ? (
              <span className="field-hint">{gpuDevices.observations[0]}</span>
            ) : null}
            {showManualGpuInput ? (
              <input
                value={settings.decoder.ffmpeg.hwaccelDevice}
                onChange={(event) => updateFFmpegDecoder({ hwaccelDevice: event.target.value })}
                placeholder={t('st.manualDeviceValue')}
                autoComplete="off"
              />
            ) : null}
          </label>
          <label>
            <FieldTitle info="Optional ffmpeg init_hw_device value for advanced setups, for example vaapi=va:/dev/dri/renderD128 or d3d11va=cam:1.">
              {t('st.initHwDevice')}
            </FieldTitle>
            <input
              value={settings.decoder.ffmpeg.initHwDevice}
              onChange={(event) => updateFFmpegDecoder({ initHwDevice: event.target.value })}
              placeholder="vaapi=va:/dev/dri/renderD128"
              autoComplete="off"
            />
          </label>
          <label>
            <FieldTitle info="Optional decoder name passed as ffmpeg -c:v before the input, such as h264_cuvid or hevc_cuvid. Leave empty for ffmpeg auto-selection.">
              {t('st.videoDecoder')}
            </FieldTitle>
            <input
              value={settings.decoder.ffmpeg.videoDecoder}
              onChange={(event) => updateFFmpegDecoder({ videoDecoder: event.target.value })}
              placeholder="auto"
              autoComplete="off"
            />
          </label>
          <label>
            <FieldTitle info="MJPEG output quality. Lower numbers are higher quality and more CPU/bandwidth; 7 is a balanced live-view default.">
              {t('st.mjpegQuality')}
            </FieldTitle>
            <input
              type="number"
              min="2"
              max="31"
              value={settings.decoder.mjpeg.quality}
              onChange={(event) => updateMJPEGDecoder({ quality: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Thread count used by ffmpeg while writing MJPEG output. Keep this low on small devices to protect the rest of the app.">
              {t('st.mjpegThreads')}
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="16"
              value={settings.decoder.mjpeg.threads}
              onChange={(event) => updateMJPEGDecoder({ threads: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Bytes ffmpeg may probe before decoding. Larger values can help unusual streams but slow startup.">
              {t('st.probeSize')}
            </FieldTitle>
            <input
              type="number"
              min="32000"
              max="50000000"
              step="1000"
              value={settings.decoder.ffmpeg.probeSize}
              onChange={(event) => updateFFmpegDecoder({ probeSize: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Microseconds ffmpeg may analyze stream metadata. Larger values can help odd cameras but increase first-frame delay.">
              {t('st.analyzeDuration')}
            </FieldTitle>
            <input
              type="number"
              min="0"
              max="30000000"
              step="1000"
              value={settings.decoder.ffmpeg.analyzeDuration}
              onChange={(event) => updateFFmpegDecoder({ analyzeDuration: Number(event.target.value) })}
            />
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.decoder.ffmpeg.lowDelay}
              onChange={(event) => updateFFmpegDecoder({ lowDelay: event.target.checked })}
            />
            <FieldTitle info="Passes ffmpeg low_delay flags for lower latency. Disable only when a camera behaves badly with low-latency decoding.">
              {t('st.lowDelay')}
            </FieldTitle>
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.decoder.ffmpeg.noBuffer}
              onChange={(event) => updateFFmpegDecoder({ noBuffer: event.target.checked })}
            />
            <FieldTitle info="Passes ffmpeg nobuffer flags to reduce live-view lag. Disable if the stream becomes unstable or drops too many frames.">
              {t('st.noBuffer')}
            </FieldTitle>
          </label>
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="How recorded NVR segments are stored on disk. 'Copy' keeps the camera's native codec with no re-encode (default; existing installs unchanged). 'H.264'/'H.265' re-encode each segment once at remux time on the GPU (NVENC) to shrink it — live capture and event clips always stay stream-copy. For the smallest footage with zero host cost, set the camera's own encoder to H.265 in the camera's Recording tab instead.">
                {t('st.recStorageTitle')}
              </FieldTitle>
            </h2>
          </header>
          <div className="settings-grid">
            <label>
              <FieldTitle info="At-rest video codec. Copy: store the camera's codec unchanged (no host CPU/GPU). H.265 (HEVC): ~40-60% smaller, re-encoded on the GPU once per segment; browsers that can't decode HEVC are transcoded to H.264 on playback. H.264: smaller than copy for high-bitrate cameras, plays everywhere.">
                {t('st.storageCodec')}
              </FieldTitle>
              <select
                value={(settings.recording && settings.recording.storage && settings.recording.storage.codec) || 'copy'}
                onChange={(event) => updateRecordingStorage({ codec: event.target.value })}
              >
                <option value="copy">{t('st.codecCopy')}</option>
                <option value="hevc">{t('st.codecHevc')}</option>
                <option value="h264">{t('st.codecH264')}</option>
              </select>
            </label>
            <label>
              <FieldTitle info="NVENC constant-quality (CQ) target used when re-encoding. Lower = better quality and larger files; 23-28 is typical. 0 uses the built-in default (26). Ignored in Copy mode.">
                {t('st.reEncodeQuality')}
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="51"
                value={(settings.recording && settings.recording.storage && settings.recording.storage.quality) || 0}
                onChange={(event) => updateRecordingStorage({ quality: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="Maximum simultaneous GPU (NVENC) encode sessions shared by remux-time re-encoding and playback transcode. Match your GPU's session cap so it is never oversubscribed. 0 uses the default (2).">
                {t('st.maxGpuEncodes')}
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="8"
                value={(settings.recording && settings.recording.storage && settings.recording.storage.maxConcurrentEncodes) || 0}
                onChange={(event) => updateRecordingStorage({ maxConcurrentEncodes: Number(event.target.value) })}
              />
            </label>
          </div>
          <label className="check-row" style={{marginTop:'8px'}}>
            <input
              type="checkbox"
              checked={!(settings.recording && settings.recording.storage && settings.recording.storage.fallbackToCopy === false)}
              onChange={(event) => updateRecordingStorage({ fallbackToCopy: event.target.checked })}
            />
            <FieldTitle info="When the GPU re-encode can't run — no compatible NVIDIA GPU/driver, or a re-encode fails at runtime — store the segment as plain stream-copy (the camera's native codec) instead of dropping it. Checked once on this host and cached. Strongly recommended: without it, a re-encode codec on a machine with no usable GPU records nothing. Ignored in Copy mode.">
              {t('st.fallbackToCopy')}
            </FieldTitle>
          </label>
          <p className="field-hint" style={{marginTop:'8px'}}>
            {t('st.recStorageHint')}
          </p>
        </section>
        </>)}

        {settingsNav === 'ai' && (<>
        <StockModelPanel authHeader={authHeader} onMessage={onMessage} />
        <LPRModelPanel authHeader={authHeader} onMessage={onMessage} />
        <DetectionModelsPanel authHeader={authHeader} onMessage={onMessage} onModelActivated={onModelActivated} />
        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="YOLO inference parameters sent to the AI worker on every frame. 0 or disabled means the worker uses its own env-var default. Changes take effect immediately without a restart.">
                {t('st.yoloTuning')}
              </FieldTitle>
            </h2>
            <button
              type="button"
              className="quiet"
              title={t('st.bestCalibrationTitle')}
              onClick={() => updateYolo(bestYoloDefaults)}
            >
              <span className="btn-icon"><Ico n="wand" /> {t('st.bestCalibration')}</span>
            </button>
          </header>
          <div className="settings-field-grid">
            <label>
              <FieldTitle info="YOLO confidence threshold override (0–1). 0 uses the worker default (MYMATASAN_YOLO_CONF env var, usually 0.25). Lower values detect more objects at the cost of more false positives. Recommended: 0.15–0.20 for hard-to-detect poses like back-facing persons.">
                {t('st.confOverride')}
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1"
                step="0.01"
                value={settings.vision?.yolo?.conf ?? 0}
                onChange={(event) => updateYolo({ conf: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="NMS IOU threshold override (0–1). 0 uses the YOLO default (0.45). Lower values keep more overlapping bounding boxes, reducing suppression of back-facing or partially-occluded persons. Recommended: 0.3–0.4.">
                {t('st.iouOverride')}
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1"
                step="0.01"
                value={settings.vision?.yolo?.iou ?? 0}
                onChange={(event) => updateYolo({ iou: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="Inference image size override in pixels (0 uses env default, e.g. 640). Larger sizes improve detection of small or distant objects but are slower. Raspberry Pi 4 (1–4 GB RAM): use 320 or 480 — sizes above 640 may run out of memory. Jetson Nano: 480–640 is safe. x86/desktop: 640–1280.">
                {t('st.imgszOverride')}
              </FieldTitle>
              <select
                value={settings.vision?.yolo?.imgsz ?? 0}
                onChange={(event) => updateYolo({ imgsz: Number(event.target.value) })}
              >
                <option value={0}>{t('st.defaultEnvVar')}</option>
                <option value={320}>320</option>
                <option value={416}>416</option>
                <option value={480}>480</option>
                <option value={640}>640</option>
                <option value={960}>960</option>
                <option value={1280}>1280</option>
              </select>
            </label>
            <label>
              <FieldTitle info="Maximum detections per image (0 uses YOLO default of 300). Increase if you expect many objects in the frame.">
                {t('st.maxDetOverride')}
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1000"
                step="10"
                value={settings.vision?.yolo?.maxDet ?? 0}
                onChange={(event) => updateYolo({ maxDet: Number(event.target.value) })}
              />
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.vision?.yolo?.augment === true}
                onChange={(event) => updateYolo({ augment: event.target.checked })}
              />
              <FieldTitle info="Enable test-time augmentation (TTA): YOLO runs inference with flipped and scaled copies of the image and merges the results. This is the single most effective setting for detecting back-facing, crouching, or partially-occluded persons. Roughly doubles inference time. Raspberry Pi 4: adds ~10–30 s per frame — only enable if accuracy matters more than speed. Jetson/x86: typically adds 1–3 s.">
                {t('st.augmentLabel')}
              </FieldTitle>
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.vision?.yolo?.half === true}
                onChange={(event) => updateYolo({ half: event.target.checked })}
              />
              <FieldTitle info="Use FP16 half-precision inference. Only effective on CUDA GPUs (Jetson, NVIDIA desktop). Automatically ignored on CPU — will not crash on Raspberry Pi or other ARM/CPU-only devices. Reduces memory usage and can increase throughput on GPU, but may slightly reduce detection accuracy.">
                {t('st.halfLabel')}
              </FieldTitle>
            </label>
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="Controls how the AI detector sources frames per camera. Auto and Siphon keep a continuous decoded stream feeding the detector — off the recorder when recording is on, or off a dedicated detection-only stream when recording is off — so detection stays immediate. Standalone opens a fresh one-frame RTSP grab each interval (slower; ~2–3s per frame). For critical/industrial use pick Auto (default) or Siphon.">
                {t('st.capture')}
              </FieldTitle>
              {capture.mode === 'auto' ? <span className="auto-badge">{t('st.auto')}</span> : null}
            </h2>
            <button
              type="button"
              className="quiet"
              title={t('st.autoConfigTitle')}
              onClick={onCaptureAutoConfig}
              disabled={busy}
            >
              <span className="btn-icon"><Ico n="wand" /> {t('st.autoConfig')}</span>
            </button>
          </header>
          <div className="settings-field-grid">
            <label>
              <FieldTitle info="Frame source. Auto: read fresh decoded frames off the continuous stream (recorder when recording is on, else a dedicated detection-only stream), falling back to a one-shot grab only if no frame is ready yet. Siphon: always read off the continuous stream. Standalone: AI opens its own one-frame RTSP grab each interval (slower). Auto/Siphon give immediate detection even with recording off.">
                {t('st.mode')}
              </FieldTitle>
              <select value={capture.mode} onChange={(event) => updateCapture({ mode: event.target.value })}>
                {captureModeOptions.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label>
              <FieldTitle info="Per-camera sampling interval in milliseconds — how often each camera is sampled for detection. Lower values detect faster but cost more CPU/GPU. In Auto mode these are set by Auto Config.">
                {t('st.intervalMs')}
              </FieldTitle>
              <input
                type="number"
                min="250"
                max="60000"
                step="250"
                value={capture.intervalMs}
                onChange={(event) => updateCapture({ intervalMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Downscaled frame width in pixels fed to detection. 640 is a good accuracy/speed balance; smaller is faster.">
                {t('st.frameWidthPx')}
              </FieldTitle>
              <input
                type="number"
                min="160"
                max="1920"
                step="32"
                value={capture.frameWidth}
                onChange={(event) => updateCapture({ frameWidth: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Standalone only. Deadline in milliseconds for a self-opened one-frame RTSP grab before it is abandoned.">
                {t('st.standaloneTimeout')}
              </FieldTitle>
              <input
                type="number"
                min="500"
                max="60000"
                step="500"
                value={capture.standalone.captureTimeoutMs}
                onChange={(event) => updateCaptureStandalone({ captureTimeoutMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Siphon only. Frames-per-second the recorder tee produces for the detector. Higher gives fresher frames at more CPU cost.">
                {t('st.siphonFps')}
              </FieldTitle>
              <input
                type="number"
                min="1"
                max="30"
                value={capture.siphon.fps}
                onChange={(event) => updateCaptureSiphon({ fps: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Siphon only. How old (ms) a siphoned frame may be before Auto mode falls back to a standalone grab. Typically about twice the interval.">
                {t('st.siphonStale')}
              </FieldTitle>
              <input
                type="number"
                min="500"
                max="120000"
                step="500"
                value={capture.siphon.staleLimitMs}
                onChange={(event) => updateCaptureSiphon({ staleLimitMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
          </div>
          {captureAuto ? (
            <p className="settings-hint">{t('st.captureAutoHint')}</p>
          ) : null}
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="Checks the configured AI detector command, Python packages, worker script, model file, and whether native fallback can keep non-AI detection available.">
                {t('st.aiTool')}
              </FieldTitle>
            </h2>
            <button type="button" className="quiet" onClick={onCheckVisionTool} disabled={busy}>
              <span className="btn-icon"><Ico n="check-ok" /> {t('st.checkAiTool')}</span>
            </button>
          </header>
          {visionToolStatus ? (
            <div className="auto-tune-result">
              <strong>{visionToolStatus.summary}</strong>
              <dl className="tool-status-grid">
                <div>
                  <dt>{t('st.mode')}</dt>
                  <dd>{visionToolStatus.mode || 'motion'}</dd>
                </div>
                <div>
                  <dt>{t('st.aiReady')}</dt>
                  <dd>{visionToolStatus.available ? t('common.yes') : t('common.no')}</dd>
                </div>
                <div>
                  <dt>{t('st.python')}</dt>
                  <dd>{visionToolStatus.pythonVersion || t('st.notDetected')}</dd>
                </div>
                <div>
                  <dt>{t('st.aiPackages')}</dt>
                  <dd>{(() => {
                    if (visionToolStatus.packagesAvailable) return '✓ ultralytics  ✓ cv2  ✓ torch';
                    const missing = visionToolStatus.missingPackages || [];
                    if (missing.length) {
                      return ['ultralytics', 'cv2', 'torch'].map((p) => `${missing.includes(p) ? '✗' : '✓'} ${p}`).join('  ');
                    }
                    return visionToolStatus.packageError || t('st.notCheckedShort');
                  })()}</dd>
                </div>
                <div>
                  <dt>{t('st.nativeFallback')}</dt>
                  <dd>{visionToolStatus.nativeFallback ? t('st.fallbackAvailable') : t('st.fallbackDisabled')}</dd>
                </div>
                <div>
                  <dt>{t('st.bytetrack')}</dt>
                  <dd>{visionToolStatus.trackerAvailable ? t('st.fallbackAvailable') : t('st.trackerNotInstalled')}</dd>
                </div>
                <div>
                  <dt>{t('st.command')}</dt>
                  <dd>{visionToolStatus.commandPath || t('st.notFound')}</dd>
                </div>
                <div>
                  <dt>{t('st.worker')}</dt>
                  <dd>{visionToolStatus.workerPath || t('st.notConfigured')}</dd>
                </div>
                <div>
                  <dt>{t('st.model')}</dt>
                  <dd>{visionToolStatus.modelPath || t('st.notConfigured')}</dd>
                </div>
              </dl>
              {Array.isArray(visionToolStatus.observations) && visionToolStatus.observations.length > 0 ? (
                <ul>
                  {visionToolStatus.observations.map((item, index) => (
                    <li key={`vision-tool-${index}`}>{item}</li>
                  ))}
                </ul>
              ) : null}
              {Array.isArray(visionToolStatus.installHints) && visionToolStatus.installHints.length > 0 ? (
                <div className="install-hints">
                  <p><strong>{t('st.missingPkgsFix')}</strong></p>
                  <ul>
                    {visionToolStatus.installHints.map((hint) => (
                      <li key={hint.importName}>
                        <code>{hint.importName}</code>
                        {hint.manual ? (
                          <span>{t('st.manualInstall', { note: hint.note })}</span>
                        ) : (
                          <span>
                            {' — '}
                            <code>{hint.command}</code>
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                  {visionToolStatus.installHints.some((h) => !h.manual) ? (
                    <button type="button" className="quiet" onClick={onInstallPackages} disabled={busy}>
                      <span className="btn-icon"><Ico n="download" /> {t('st.installMissing')}</span>
                    </button>
                  ) : null}
                  {visionInstallResult ? (
                    <div className="install-result">
                      <strong>{visionInstallResult.success ? t('st.installSucceeded') : t('st.installFailed')}</strong>
                      {Array.isArray(visionInstallResult.observations) && visionInstallResult.observations.length > 0 ? (
                        <ul>
                          {visionInstallResult.observations.map((item, index) => (
                            <li key={`install-obs-${index}`}>{item}</li>
                          ))}
                        </ul>
                      ) : null}
                      {visionInstallResult.output ? (
                        <pre className="install-output">{visionInstallResult.output}</pre>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
          <AiRuntimeInstallControl authHeader={authHeader} onMessage={onMessage} onRestart={onRestart} />
        </section>
        </>)}

        {settingsNav === 'runtime' && (<>
        <section className="settings-panel span-two">
          <header>
            <h2>{t('st.liveStream')}</h2>
          </header>
          <div className="check-row-inline">
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.stream.webrtc.enabled}
                onChange={(event) =>
                  update((current) => ({
                    ...current,
                    stream: {
                      ...current.stream,
                      webrtc: { ...current.stream.webrtc, enabled: event.target.checked },
                    },
                  }))
                }
              />
              {t('st.webrtc')}
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.stream.mjpegFallback.enabled}
                onChange={(event) =>
                  update((current) => ({
                    ...current,
                    stream: {
                      ...current.stream,
                      mjpegFallback: { enabled: event.target.checked },
                    },
                  }))
                }
              />
              {t('st.mjpegFallback')}
            </label>
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>{t('st.iceServers')}</h2>
            <button
              type="button"
              className="quiet"
              onClick={() =>
                update((current) => ({
                  ...current,
                  stream: {
                    ...current.stream,
                    webrtc: {
                      ...current.stream.webrtc,
                      iceServers: [...(current.stream.webrtc.iceServers || []), { urls: [], username: '', credential: '' }],
                    },
                  },
                }))
              }
              disabled={busy}
            >
              {t('st.addServer')}
            </button>
          </header>
          <div className="ice-list">
            {iceServers.length === 0 ? <p className="empty">{t('st.noIceServers')}</p> : null}
            {iceServers.map((server, index) => (
              <div className="ice-row" key={`ice-${index}`}>
                <label>
                  {t('st.urls')}
                  <textarea
                    value={iceUrlsText(server)}
                    onChange={(event) => updateIceServer(index, { urls: textToIceUrls(event.target.value) })}
                    placeholder="stun:stun.example.com:3478"
                  />
                </label>
                <label>
                  {t('common.username')}
                  <input
                    value={server.username || ''}
                    onChange={(event) => updateIceServer(index, { username: event.target.value })}
                    autoComplete="off"
                  />
                </label>
                <label>
                  {t('st.credential')}
                  <PasswordField
                    value={server.credential || ''}
                    onChange={(credential) => updateIceServer(index, { credential })}
                    autoComplete="off"
                  />
                </label>
                <button
                  type="button"
                  className="quiet danger-text"
                  onClick={() =>
                    update((current) => ({
                      ...current,
                      stream: {
                        ...current.stream,
                        webrtc: {
                          ...current.stream.webrtc,
                          iceServers: (current.stream.webrtc.iceServers || []).filter((_, itemIndex) => itemIndex !== index),
                        },
                      },
                    }))
                  }
                  disabled={busy}
                >
                  {t('common.remove')}
                </button>
              </div>
            ))}
          </div>
        </section>
        </>)}

        <div className="settings-actions">
          <button type="submit" disabled={busy || !hasChanges}>
            <span className="btn-icon"><Ico n="save" /> {t('st.saveSettings')}</span>
          </button>
          <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
            <span className="btn-icon"><Ico n="undo" /> {t('st.discardChanges')}</span>
          </button>
          <button type="button" className="quiet" onClick={onReset} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> {t('st.resetDefaults')}</span>
          </button>
        </div>
          </form>
        ) : null}

        {settingsNav === 'users' ? (
          <UsersManager authHeader={authHeader} onMessage={onMessage} focusUsername={focusUsername} />
        ) : null}

        {settingsNav === 'notifications' ? (
          <NotificationSettingsPanel
            settings={notificationSettings}
            savedSettings={savedNotificationSettings}
            busy={busy}
            onChange={onNotificationChange}
            onSaveDestination={onSaveDestination}
            onDeleteDestination={onDeleteDestination}
            onSaveRetention={onSaveRetention}
            onTest={onTestNotification}
            onPurgeExpired={onPurgeNotifications}
          />
        ) : null}

        {settingsNav === 'health' ? (
          <HealthSettingsPanel
            settings={healthSettings}
            busy={busy}
            hasChanges={healthHasChanges}
            onChange={onHealthChange}
            onSave={onSaveHealth}
            onDiscard={onDiscardHealth}
          />
        ) : null}

        {settingsNav === 'machine' ? (
          <MachineHealthSettingsPanel
            settings={machineHealthSettings}
            busy={busy}
            hasChanges={machineHealthHasChanges}
            metrics={machineMetrics}
            onChange={onMachineHealthChange}
            onSave={onSaveMachineHealth}
            onDiscard={onDiscardMachineHealth}
            onRefreshMetrics={onRefreshMachineMetrics}
            capacity={capacity}
            onEstimateCapacity={onEstimateCapacity}
            onCalibrateCapacity={onCalibrateCapacity}
          />
        ) : null}

        {settingsNav === 'pairing' ? (
          <PairingPanel authHeader={authHeader} />
        ) : null}

        {settingsNav === 'backup' ? (
          <div className="settings-stack">
            <BackupRestorePanel authHeader={authHeader} onRestart={onRestart} />
            <RecoveryKeyPanel authHeader={authHeader} />
            <SecureWipePanel resetAllowed={resetAllowed} onSecureWipe={onSecureWipe} />
          </div>
        ) : null}

        {settingsNav === 'system' ? (
          <SystemStatusPanel authHeader={authHeader} onRestart={onRestart} />
        ) : null}
      </div>
    </section>
  );
}

export const SEVERITY_OPTIONS = [
  { value: 'info', label: 'Info and above' },
  { value: 'warning', label: 'Warning and above' },
  { value: 'critical', label: 'Critical only' },
];

export function NotificationSettingsPanel({ settings, savedSettings, busy, onChange, onSaveDestination, onDeleteDestination, onSaveRetention, onTest, onPurgeExpired }) {
  const t = useT();
  const retention = settings.retention || defaultNotificationSettings.retention;
  const destinations = Array.isArray(settings.destinations) ? settings.destinations : [];
  // Last-saved snapshot, used to scope each section's own Save/Discard: a section's
  // buttons enable only when THAT section differs from what's persisted.
  const savedRetention = savedSettings?.retention || defaultNotificationSettings.retention;
  const savedDestinations = Array.isArray(savedSettings?.destinations) ? savedSettings.destinations : [];
  const retentionChanged = JSON.stringify(retention) !== JSON.stringify(savedRetention);
  // A destination is "changed" when it has no saved counterpart yet (newly added)
  // or differs from its saved snapshot at the same position.
  const destChanged = (index) =>
    index >= savedDestinations.length ||
    JSON.stringify(destinations[index]) !== JSON.stringify(savedDestinations[index]);
  // Which destination's form is expanded (accordion — one open at a time).
  // Tracked by index since new destinations have no id until saved.
  const [openIndex, setOpenIndex] = useState(null);
  function patch(section, values) {
    onChange({ ...settings, [section]: { ...settings[section], ...values } });
  }
  function setDestinations(next) {
    onChange({ ...settings, destinations: next });
  }
  function addDestination(type) {
    setDestinations([...destinations, defaultDestination(type)]);
    setOpenIndex(destinations.length); // open the newly added row
  }
  function updateDestination(index, values) {
    setDestinations(destinations.map((d, i) => (i === index ? { ...d, ...values } : d)));
  }
  function removeDestination(index) {
    setDestinations(destinations.filter((_, i) => i !== index));
    setOpenIndex(null);
  }
  // A destination is persisted once it has an id that exists in the saved
  // snapshot; removing that must go to the server (per-destination DELETE), while
  // a never-saved row is just dropped from local state.
  const isPersisted = (index) =>
    index < savedDestinations.length && !!destinations[index]?.id &&
    savedDestinations.some((d) => d.id === destinations[index].id);
  function removeOrDelete(index) {
    if (isPersisted(index)) {
      onDeleteDestination(destinations[index].id);
      setOpenIndex(null);
      return;
    }
    removeDestination(index);
  }
  // Discard one destination's unsaved edits: revert to its saved snapshot, or drop
  // it entirely when it was never saved.
  function discardDestination(index) {
    if (index >= savedDestinations.length) {
      removeDestination(index);
      return;
    }
    setDestinations(destinations.map((d, i) => (i === index ? savedDestinations[index] : d)));
  }
  // Each section persists independently via its own endpoint: saving one
  // destination or the retention policy never touches the others' stored config.
  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="send" /> {t('st.deliveryDest')}</span></h2>
          <button type="button" className="quiet" onClick={() => onTest('destinations')} disabled={busy}>
            <span className="btn-icon"><Ico n="send" /> {t('st.sendTest')}</span>
          </button>
        </header>
        <p className="settings-hint">
          {t('st.notifHint')}
        </p>
        {destinations.length === 0 ? (
          <p className="settings-hint">{t('st.noDest')}</p>
        ) : null}
        {destinations.length > 0 ? (
          <AccordionList>
            {destinations.map((dest, index) => (
              <DestinationItem
                key={dest.id || `new-${index}`}
                dest={dest}
                busy={busy}
                changed={destChanged(index)}
                open={openIndex === index}
                onToggleOpen={() => setOpenIndex(openIndex === index ? null : index)}
                onChange={(values) => updateDestination(index, values)}
                onSave={() => onSaveDestination(index, destinations[index])}
                onDiscard={() => discardDestination(index)}
                onRemove={() => removeOrDelete(index)}
              />
            ))}
          </AccordionList>
        ) : null}
        <div className="action-row">
          <button type="button" className="quiet" onClick={() => addDestination('webhook')} disabled={busy}>
            <span className="btn-icon"><Ico n="wifi" /> {t('st.addWebhook')}</span>
          </button>
          <button type="button" className="quiet" onClick={() => addDestination('telegram')} disabled={busy}>
            <span className="btn-icon"><Ico n="send" /> {t('st.addTelegram')}</span>
          </button>
          <button type="button" className="quiet" onClick={() => addDestination('mqtt')} disabled={busy}>
            <span className="btn-icon"><Ico n="wifi" /> {t('st.addMqtt')}</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="trash" /> {t('st.retention')}</span></h2>
          {onPurgeExpired ? (
            <button
              type="button"
              className="quiet"
              disabled={busy || !(retention.days > 0)}
              title={retention.days > 0
                ? t('st.purgeTitleActive', { days: retention.days, read: retention.onlyRead ? t('st.purgeReadSuffix') : '' })
                : t('st.purgeTitleDisabled')}
              onClick={() => {
                if (window.confirm(t('st.purgeConfirm', { days: retention.days, read: retention.onlyRead ? t('st.purgeConfirmReadSuffix') : '' }))) {
                  onPurgeExpired({ days: retention.days, onlyRead: !!retention.onlyRead });
                }
              }}
            >
              <span className="btn-icon"><Ico n="trash" /> {t('st.purgeExpiredNow')}</span>
            </button>
          ) : null}
        </header>
        <p className="settings-hint">{t('st.retentionHint')}</p>
        <div className="settings-grid">
          <label>
            {t('st.keepForDays')}
            <input
              type="number"
              min="0"
              value={retention.days}
              onChange={(event) => patch('retention', { days: Number(event.target.value) })}
            />
            <span className="settings-hint">{t('st.zeroDisablesPurge')}</span>
          </label>
          <label>
            {t('st.purgeInterval')}
            <input
              type="number"
              min="1"
              value={retention.intervalHours}
              onChange={(event) => patch('retention', { intervalHours: Number(event.target.value) })}
            />
            <span className="settings-hint">{t('st.intervalAfterRestart')}</span>
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={retention.onlyRead}
              onChange={(event) => patch('retention', { onlyRead: event.target.checked })}
            />
            {t('st.onlyPurgeRead')}
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" onClick={() => onSaveRetention(retention)} disabled={busy || !retentionChanged}>
            <span className="btn-icon"><Ico n="save" /> {t('st.saveRetention')}</span>
          </button>
          <button type="button" className="quiet" onClick={() => onChange({ ...settings, retention: savedRetention })} disabled={busy || !retentionChanged}>
            <span className="btn-icon"><Ico n="undo" /> {t('st.discardChanges')}</span>
          </button>
        </div>
      </section>
    </div>
  );
}

// WEBHOOK_PAYLOAD_SAMPLE is an illustrative webhook body shown in the destination
// card so integrators see the shape without leaving the app. Snapshot/base64 and
// the per-destination data.* keys appear only when that destination enables them.
const WEBHOOK_PAYLOAD_SAMPLE = `{
  "id": "9f2c1a7e-…",
  "category": "vision.alert",
  "severity": "critical",
  "title": "Fire detected — Kitchen",
  "body": "Front Gate • fire • 92% confidence\\nsite: Front Gate",
  "source": "vision-monitor",
  "cameraId": 3,
  "refType": "alert_event",
  "refId": 481,
  "link": "/api/vision/alerts/481/snapshot",
  "data": {
    "alertId": 481,
    "ruleId": 12,
    "cameraName": "Front Gate",
    "detectionType": "presence",
    "ruleName": "Fire detected — Kitchen",
    "label": "fire",
    "confidence": 0.92,
    "boundingBox": "[0.41,0.22,0.18,0.30]",
    "zonePolygon": "[[0.1,0.1],[0.9,0.1],[0.9,0.9],[0.1,0.9]]",
    "snapshotPath": "recordings/cam3/snapshots/481.jpg",
    "site": "Front Gate"
  },
  "createdAt": 1781779140,
  "snapshotBase64": "/9j/4AAQSkZJRgABAQAA…",
  "snapshotContentType": "image/jpeg",
  "snapshotFilename": "alert-481.jpg"
}`;

// destinationTarget renders a one-line summary of where a destination delivers,
// shown in the collapsed accordion row so the target is visible without opening
// the form.
function destinationTarget(dest, t) {
  if (dest.type === 'mqtt') {
    const mqtt = dest.mqtt || {};
    if (mqtt.brokerUrl) return `${mqtt.brokerUrl}${mqtt.topic ? ` → ${mqtt.topic}` : ''}`;
    return t('st.noBrokerSet');
  }
  if (dest.type === 'telegram') {
    return dest.chatId ? t('st.chatN', { id: dest.chatId }) : t('st.noChatSet');
  }
  return dest.url || t('st.noUrlSet');
}

// DestinationItem is one accordion row in the destinations list: a compact,
// always-visible summary (name, type, target, enabled toggle, remove) that
// expands to the full editing form (DestinationCard) when clicked. Built on the
// shared AccordionItem so it matches every other list editor.
function DestinationItem({ dest, busy, changed, open, onToggleOpen, onChange, onSave, onRemove, onDiscard }) {
  const t = useT();
  const enabled = dest.enabled !== false;
  const summary = (
    <>
      <span className="accordion-title">{dest.name || titleizeType(dest.type)}</span>
      <span className={`class-source-badge ${dest.type}`}>{dest.type}</span>
      <span className="accordion-muted">{destinationTarget(dest, t)}</span>
      {!enabled ? <span className="accordion-tag">{t('st.disabled')}</span> : null}
    </>
  );
  const actions = (
    <>
      <label className="check-row compact" title={t('st.enableDeliveryTitle')}>
        <input
          type="checkbox"
          checked={enabled}
          onChange={(event) => onChange({ enabled: event.target.checked })}
          disabled={busy}
        />
        {t('common.enabled')}
      </label>
      <button type="button" className="quiet danger" onClick={onRemove} disabled={busy}>{t('common.remove')}</button>
    </>
  );
  return (
    <AccordionItem open={open} onToggle={onToggleOpen} summary={summary} actions={actions}>
      <DestinationCard dest={dest} busy={busy} onChange={onChange} />
      <div className="settings-actions destination-actions">
        <button type="button" onClick={onSave} disabled={busy || !changed}>
          <span className="btn-icon"><Ico n="save" /> {t('st.saveDestination')}</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !changed}>
          <span className="btn-icon"><Ico n="undo" /> {t('st.discardChanges')}</span>
        </button>
      </div>
    </AccordionItem>
  );
}

// titleizeType gives a readable fallback name for a destination with no name set.
function titleizeType(type) {
  if (type === 'telegram') return 'Telegram';
  if (type === 'mqtt') return 'MQTT';
  return 'Webhook';
}

// DestinationCard edits one notification destination: its channel/target,
// severity floor, category subscription, per-destination detection fields, and
// static custom fields. When a custom field key collides with a built-in/AI
// field, that field's toggle is disabled so the user sees the stock field is
// bypassed (custom wins).
function DestinationCard({ dest, busy, onChange }) {
  const t = useT();
  const isMqtt = dest.type === 'mqtt';
  const isTelegram = dest.type === 'telegram';
  const fields = dest.fields || defaultAlertNotificationConfig;
  const categories = Array.isArray(dest.categories) ? dest.categories : [];
  const customFields = Array.isArray(dest.customFields) ? dest.customFields : [];
  const mqtt = dest.mqtt || {};
  const disabled = busy || dest.enabled === false;
  // Payload keys claimed by custom fields, used to disable the matching toggles.
  const customKeys = new Set(customFields.map((f) => String(f.key || '').trim()).filter(Boolean));
  const reservedKeys = new Set([...builtinPayloadKeys, ...Object.values(alertFieldDataKeys)]);

  function setField(key, value) {
    onChange({ fields: { ...fields, [key]: value } });
  }
  function setMqtt(values) {
    onChange({ mqtt: { ...mqtt, ...values } });
  }
  function toggleCategory(cat, on) {
    const next = on
      ? Array.from(new Set([...categories, cat]))
      : categories.filter((c) => c !== cat);
    onChange({ categories: next });
  }
  function setCustom(index, values) {
    onChange({ customFields: customFields.map((f, i) => (i === index ? { ...f, ...values } : f)) });
  }

  return (
    <div className="dest-card">
      <label className="dest-name-field">
        {t('common.name')}
        <input
          className="dest-name"
          value={dest.name || ''}
          onChange={(event) => onChange({ name: event.target.value })}
          placeholder={t('st.destNamePh')}
          disabled={busy}
        />
      </label>

      {isMqtt ? (
        <>
          <div className="settings-grid">
            <label>
              {t('st.brokerUrl')}
              <input value={mqtt.brokerUrl || ''} onChange={(event) => setMqtt({ brokerUrl: event.target.value })} placeholder="ssl://broker.example.com:8883" autoComplete="off" disabled={disabled} />
            </label>
            <label>
              <FieldTitle info="Publish topic. Supports {{token}} placeholders from the payload data (cameraName, alertId, ruleId, detectionType, and label/confidence/ruleName when those fields are enabled; plate/vehicleType/color on license-plate alerts) plus cameraId, category, severity. A token that resolves to nothing collapses its level (e.g. .../{{cameraId}} on a Test → no trailing slash).">
                {t('st.topic')}
              </FieldTitle>
              <input value={mqtt.topic || ''} onChange={(event) => setMqtt({ topic: event.target.value })} placeholder="matasan/alerts/{'{{cameraName}}'}" autoComplete="off" disabled={disabled} />
            </label>
            <label>
              {t('st.clientId')}
              <input value={mqtt.clientId || ''} onChange={(event) => setMqtt({ clientId: event.target.value })} placeholder={t('st.clientIdPh')} autoComplete="off" disabled={disabled} />
            </label>
            <label>
              {t('st.qos')}
              <select value={Number(mqtt.qos ?? 1)} onChange={(event) => setMqtt({ qos: Number(event.target.value) })} disabled={disabled}>
                <option value={0}>{t('st.qos0')}</option>
                <option value={1}>{t('st.qos1')}</option>
                <option value={2}>{t('st.qos2')}</option>
              </select>
            </label>
            <label>
              {t('common.username')}
              <input value={mqtt.username || ''} onChange={(event) => setMqtt({ username: event.target.value })} autoComplete="off" disabled={disabled} />
            </label>
            <label>
              {t('common.password')}
              <PasswordField value={mqtt.password || ''} onChange={(password) => setMqtt({ password })} autoComplete="off" disabled={disabled} />
            </label>
          </div>
          <label className="check-row">
            <input type="checkbox" checked={Boolean(mqtt.retain)} onChange={(event) => setMqtt({ retain: event.target.checked })} disabled={disabled} />
            {t('st.retainMsg')}
          </label>
          <fieldset className="dest-group">
            <legend>
              <FieldTitle info="TLS for ssl:// brokers. Paste PEM contents. CA verifies the broker; client certificate + key enable mutual-TLS (client-certificate auth).">
                {t('st.tlsClientCert')}
              </FieldTitle>
            </legend>
            <label>
              {t('st.caCert')}
              <textarea rows="3" value={mqtt.caCert || ''} onChange={(event) => setMqtt({ caCert: event.target.value })} placeholder="-----BEGIN CERTIFICATE-----" disabled={disabled} />
            </label>
            <div className="settings-grid">
              <label>
                {t('st.clientCert')}
                <textarea rows="3" value={mqtt.clientCert || ''} onChange={(event) => setMqtt({ clientCert: event.target.value })} placeholder="-----BEGIN CERTIFICATE-----" disabled={disabled} />
              </label>
              <label>
                {t('st.clientKey')}
                <textarea rows="3" value={mqtt.clientKey || ''} onChange={(event) => setMqtt({ clientKey: event.target.value })} placeholder="-----BEGIN PRIVATE KEY-----" disabled={disabled} />
              </label>
            </div>
            <label className="check-row">
              <input type="checkbox" checked={Boolean(mqtt.insecureSkipVerify)} onChange={(event) => setMqtt({ insecureSkipVerify: event.target.checked })} disabled={disabled} />
              {t('st.skipVerify')}
            </label>
          </fieldset>
          <details className="dest-sample">
            <summary>{t('st.samplePayload')}</summary>
            <pre className="install-output">{WEBHOOK_PAYLOAD_SAMPLE}</pre>
          </details>
        </>
      ) : isTelegram ? (
        <div className="settings-grid">
          <label>
            {t('st.botToken')}
            <PasswordField value={dest.botToken || ''} onChange={(botToken) => onChange({ botToken })} placeholder="123456:ABC-DEF..." autoComplete="off" disabled={disabled} />
          </label>
          <label>
            {t('st.chatId')}
            <input value={dest.chatId || ''} onChange={(event) => onChange({ chatId: event.target.value })} placeholder="-1001234567890" autoComplete="off" disabled={disabled} />
          </label>
        </div>
      ) : (
        <>
          <label>
            {t('st.webhookUrl')}
            <input value={dest.url || ''} onChange={(event) => onChange({ url: event.target.value })} type="url" placeholder="https://hooks.example.com/..." autoComplete="off" disabled={disabled} />
          </label>
          <details className="dest-sample">
            <summary>{t('st.samplePayload')}</summary>
            <pre className="install-output">{WEBHOOK_PAYLOAD_SAMPLE}</pre>
          </details>
        </>
      )}

      <label>
        {t('st.minSeverity')}
        <select value={dest.minSeverity || 'warning'} onChange={(event) => onChange({ minSeverity: event.target.value })} disabled={disabled}>
          {SEVERITY_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
      </label>

      <fieldset className="dest-group">
        <legend>{t('st.receives')}</legend>
        {notificationCategories.map(([value, label, help]) => (
          <label className="check-row" key={value} title={help}>
            <input
              type="checkbox"
              checked={categories.length === 0 || categories.includes(value)}
              onChange={(event) => toggleCategory(value, event.target.checked)}
              disabled={busy}
            />
            {label}
          </label>
        ))}
        <span className="field-hint">{t('st.noneAllTypes')}</span>
      </fieldset>

      <fieldset className="dest-group">
        <legend>{t('st.detectionFields')}</legend>
        {alertNotificationFields.map(([key, label, help]) => {
          const overridden = customKeys.has(alertFieldDataKeys[key]);
          return (
            <label className="check-row" key={key} title={overridden ? `Overridden by a custom field named "${alertFieldDataKeys[key]}"` : help}>
              <input
                type="checkbox"
                checked={!overridden && fields[key] !== false}
                onChange={(event) => setField(key, event.target.checked)}
                disabled={busy || overridden}
              />
              {label}{overridden ? t('st.overriddenSuffix') : ''}
            </label>
          );
        })}
        <span className="field-hint">
          {t('st.lprFieldsHint')}
        </span>
      </fieldset>

      {!customKeys.has(alertFieldDataKeys.includeSnapshot) && fields.includeSnapshot !== false ? (
        <label>
          {t('st.snapshotDelivery')}
          <select value={dest.snapshotMode === 'link' ? 'link' : 'inline'} onChange={(event) => onChange({ snapshotMode: event.target.value })} disabled={busy}>
            <option value="inline">{t('st.snapInline')}</option>
            <option value="link">{t('st.snapLink')}</option>
          </select>
          <span className="field-hint">{t('st.snapHint')}</span>
        </label>
      ) : null}

      <fieldset className="dest-group">
        <legend>
          <FieldTitle info="Key/value pairs added to the payload. A custom field overrides a built-in field of the same key. Values may use templates: {{ruleName}}, {{cameraName}}, {{label}}, {{confidence}}, {{detectionType}}, {{alertId}}, {{ruleId}}, {{cameraId}}, and for license-plate alerts {{plate}}, {{vehicleType}}, {{color}}, {{watchlisted}}.">
            {t('st.customFields')}
          </FieldTitle>
        </legend>
        {customFields.map((field, index) => {
          const collides = reservedKeys.has(String(field.key || '').trim());
          return (
            <div className="dest-custom-row" key={index}>
              <input value={field.key || ''} onChange={(event) => setCustom(index, { key: event.target.value })} placeholder={t('st.keyPh')} disabled={busy} />
              <input value={field.value || ''} onChange={(event) => setCustom(index, { value: event.target.value })} placeholder={t('st.valuePh')} disabled={busy} />
              <button type="button" className="quiet danger" onClick={() => onChange({ customFields: customFields.filter((_, i) => i !== index) })} disabled={busy} aria-label={t('st.removeField')}>✕</button>
              {collides ? <span className="field-hint">{t('st.overridesBuiltin', { key: String(field.key).trim() })}</span> : null}
            </div>
          );
        })}
        <button type="button" className="quiet" onClick={() => onChange({ customFields: [...customFields, { key: '', value: '' }] })} disabled={busy}>
          <span className="btn-icon"><Ico n="plus" /> {t('st.addField')}</span>
        </button>
        <TemplateTokenHelp />
      </fieldset>
    </div>
  );
}

// TemplateTokenHelp renders the available {{token}} placeholders so users don't
// have to guess them. Clicking a token copies "{{token}}" to the clipboard and
// shows brief inline feedback (self-contained — no parent props needed).
function TemplateTokenHelp() {
  const t = useT();
  const [copied, setCopied] = useState('');
  const groups = notificationTemplateTokens.reduce((acc, tk) => {
    (acc[tk.group] = acc[tk.group] || []).push(tk);
    return acc;
  }, {});
  async function copy(token) {
    const text = `{{${token}}}`;
    try { await navigator.clipboard.writeText(text); } catch (_) { /* clipboard may be blocked */ }
    setCopied(token);
    window.setTimeout(() => setCopied(''), 1200);
  }
  return (
    <div className="template-token-help">
      <span className="field-hint">{t('st.placeholdersHint')}</span>
      {Object.entries(groups).map(([group, tokens]) => (
        <div key={group} className="token-group">
          <strong className="token-group-label">{group}:</strong>
          {tokens.map((tk) => (
            <button
              key={tk.token}
              type="button"
              className={`token-chip${copied === tk.token ? ' copied' : ''}`}
              title={tk.desc}
              onClick={() => copy(tk.token)}
            >
              {copied === tk.token ? t('st.copied') : `{{${tk.token}}}`}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}

export function HealthSettingsPanel({ settings, busy, hasChanges, onChange, onSave, onDiscard }) {
  const t = useT();
  const value = { ...defaultHealthSettings, ...(settings || {}) };
  function patch(values) {
    onChange({ ...value, ...values });
  }
  // Interval and timeout are stored in milliseconds but edited in seconds for clarity.
  const intervalSec = Math.round(value.intervalMs / 1000);
  const timeoutSec = Math.round(value.timeoutMs / 1000);
  const detectionSec = intervalSec * Math.max(1, value.failureThreshold);
  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> {t('st.cameraHealthMonitor')}</span></h2>
          <label className="check-row">
            <input
              type="checkbox"
              checked={value.enabled}
              onChange={(event) => patch({ enabled: event.target.checked })}
            />
            {t('common.enabled')}
          </label>
        </header>
        <p className="settings-hint">
          {t('st.cameraHealthHint')}
        </p>
        <div className="settings-grid">
          <label>
            <FieldTitle info="How often every camera is checked. Lower values detect outages sooner but probe the network more frequently. Minimum 5 seconds.">
              {t('st.checkInterval')}
            </FieldTitle>
            <input
              type="number"
              min="5"
              step="5"
              value={intervalSec}
              onChange={(event) => patch({ intervalMs: Math.max(5, Number(event.target.value) || 0) * 1000 })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Per-probe deadline for the TCP dial and RTSP check. Cameras on slow links may need a higher value. 1–60 seconds.">
              {t('st.probeTimeout')}
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="60"
              value={timeoutSec}
              onChange={(event) => patch({ timeoutMs: Math.min(60, Math.max(1, Number(event.target.value) || 0)) * 1000 })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Consecutive failed checks before a camera is declared offline. Higher values avoid false alarms from brief network blips.">
              {t('st.failureThreshold')}
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="10"
              value={value.failureThreshold}
              onChange={(event) => patch({ failureThreshold: Math.max(1, Number(event.target.value) || 0) })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Consecutive successful checks before a camera is declared back online. Higher values avoid flapping when a camera is intermittently reachable.">
              {t('st.recoveryThreshold')}
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="10"
              value={value.recoveryThreshold}
              onChange={(event) => patch({ recoveryThreshold: Math.max(1, Number(event.target.value) || 0) })}
              disabled={!value.enabled}
            />
          </label>
        </div>
        <p className="settings-hint">
          {t('st.outageHint', { n: detectionSec, thr: value.failureThreshold, int: intervalSec })}
        </p>
      </section>

      <div className="settings-actions">
        <button type="submit" disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="save" /> {t('st.saveSettings')}</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="undo" /> {t('st.discardChanges')}</span>
        </button>
      </div>
    </form>
  );
}

function formatBytes(n) {
  const bytes = Number(n) || 0;
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${(bytes / Math.pow(1024, i)).toFixed(i >= 3 ? 1 : 0)} ${units[i]}`;
}

function machineLevelClass(percent, warn, critical) {
  if (percent >= critical) return 'offline';
  if (percent >= warn) return 'unknown';
  return 'online';
}

const capacityLimitLabels = { cpu: 'st.capCpu', gpu: 'st.capGpu', disk: 'st.capDisk', memory: 'st.capMemory' };

// formatRetentionDays renders an estimated retention span readably: minutes/hours
// below a day, otherwise days.
export function formatRetentionDays(days) {
  if (!days || days <= 0) return '—';
  if (days < 1) {
    const hours = days * 24;
    if (hours < 1) return `${Math.round(hours * 60)} min`;
    return `${hours.toFixed(1)} hours`;
  }
  return `${days < 10 ? days.toFixed(1) : Math.round(days)} days`;
}

// CapacityRetentionNote shows the recording retention achievable at the estimated
// camera count — disk shortens retention rather than blocking cameras.
export function CapacityRetentionNote({ capacity }) {
  const t = useT();
  if (!capacity || !capacity.configuredRetentionDays) return null;
  const constrained = capacity.retentionConstrained;
  return (
    <div className={`capacity-retention${constrained ? ' capacity-retention--warn' : ''}`}>
      <span className="btn-icon"><Ico n={constrained ? 'warning' : 'trash'} /></span>
      <span>
        {t('st.capRetentionNote', { max: capacity.estimatedMax, span: formatRetentionDays(capacity.achievableRetentionDays), target: capacity.configuredRetentionDays })}
        {constrained ? t('st.capRetentionConstrained') : ''}
      </span>
    </div>
  );
}

// BackupRestorePanel exports and restores a portable, passphrase-encrypted bundle of
// the app's configuration (cameras incl. credentials, AI detection, notifications, and
// app settings) so a fresh install can be recovered without reconfiguring. The file
// carries plaintext secrets, so it is always protected by a user passphrase and never
// leaves without one. See services/backup.go.
function BackupRestorePanel({ authHeader, onRestart }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState(null);
  // Export
  const [sections, setSections] = useState([]);
  const [selected, setSelected] = useState({});
  const [pass, setPass] = useState('');
  const [confirmPass, setConfirmPass] = useState('');
  // Restore
  const [fileData, setFileData] = useState('');
  const [restorePass, setRestorePass] = useState('');
  const [manifest, setManifest] = useState(null);
  const [mode, setMode] = useState('replace');
  const [result, setResult] = useState(null);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, status: resp.status, body };
  }

  useEffect(() => {
    let alive = true;
    api('/api/settings/backup/sections').then((r) => {
      if (!alive || !r.ok || !Array.isArray(r.body)) return;
      setSections(r.body);
      // Default: include every section that currently has data.
      const init = {};
      r.body.forEach((s) => { init[s.id] = s.count > 0; });
      setSelected(init);
    }).catch(() => {});
    return () => { alive = false; };
    /* eslint-disable-next-line */
  }, [authHeader]);

  const sectionLabel = (id) => t(`st.bkupSection.${id}`);
  const chosen = sections.filter((s) => selected[s.id]).map((s) => s.id);

  async function createBackup() {
    setMsg(null);
    if (chosen.length === 0) { setMsg({ kind: 'error', text: t('st.bkupSelectSection') }); return; }
    if (pass.length < 8) { setMsg({ kind: 'error', text: t('st.recPassMin') }); return; }
    if (pass !== confirmPass) { setMsg({ kind: 'error', text: t('st.recPassMismatch') }); return; }
    setBusy(true);
    try {
      const r = await api('/api/settings/backup/export', { method: 'POST', body: JSON.stringify({ sections: chosen, passphrase: pass }) });
      if (!r.ok || !r.body?.dataBase64) { setMsg({ kind: 'error', text: r.body?.message || t('st.bkupCreateFail') }); return; }
      const raw = Uint8Array.from(atob(r.body.dataBase64), (c) => c.charCodeAt(0));
      const blob = new Blob([raw], { type: 'application/octet-stream' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = r.body.filename || 'mymatasan-backup.mmbackup';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      setMsg({ kind: 'ok', text: t('st.bkupCreateOk') });
      setPass(''); setConfirmPass('');
    } catch (err) {
      setMsg({ kind: 'error', text: err?.message || t('st.bkupCreateFail') });
    } finally {
      setBusy(false);
    }
  }

  async function onPickFile(e) {
    const file = e.target.files && e.target.files[0];
    setManifest(null); setResult(null); setMsg(null);
    if (!file) { setFileData(''); return; }
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      setFileData(btoa(bin));
    } catch (_) {
      setFileData('');
      setMsg({ kind: 'error', text: t('st.bkupReadFail') });
    }
  }

  async function preview() {
    setMsg(null); setResult(null);
    if (!fileData) { setMsg({ kind: 'error', text: t('st.bkupNeedFile') }); return; }
    if (!restorePass) { setMsg({ kind: 'error', text: t('st.bkupNeedPass') }); return; }
    setBusy(true);
    try {
      const r = await api('/api/settings/backup/preview', { method: 'POST', body: JSON.stringify({ dataBase64: fileData, passphrase: restorePass }) });
      if (!r.ok || !r.body?.app) { setMsg({ kind: 'error', text: r.body?.message || t('st.bkupPreviewFail') }); setManifest(null); return; }
      setManifest(r.body);
    } catch (err) {
      setMsg({ kind: 'error', text: err?.message || t('st.bkupPreviewFail') });
    } finally {
      setBusy(false);
    }
  }

  async function restore() {
    setMsg(null);
    setBusy(true);
    try {
      const r = await api('/api/settings/backup/restore', { method: 'POST', body: JSON.stringify({ dataBase64: fileData, passphrase: restorePass, sections: manifest?.sections || [], mode }) });
      if (!r.ok || !r.body?.restored) { setMsg({ kind: 'error', text: r.body?.message || t('st.bkupRestoreFail') }); return; }
      setResult(r.body);
      setMsg({ kind: 'ok', text: t('st.bkupDoneTitle') });
    } catch (err) {
      setMsg({ kind: 'error', text: err?.message || t('st.bkupRestoreFail') });
    } finally {
      setBusy(false);
    }
  }

  const manifestSections = Array.isArray(manifest?.sections) ? manifest.sections : [];

  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="save" /> {t('st.bkupTitle')}</span></h2></header>
        <p className="settings-hint">{t('st.bkupIntro')}</p>
        {msg ? <p className={msg.kind === 'error' ? 'settings-hint danger-text' : 'settings-hint'}>{msg.text}</p> : null}

        <FieldTitle info={t('st.bkupExportInfo')}>{t('st.bkupExportHeading')}</FieldTitle>
        <p className="field-hint">{t('st.bkupExportHint')}</p>
        <div className="backup-sections">
          {sections.map((s) => (
            <label key={s.id} className={`backup-section${s.count === 0 ? ' is-empty' : ''}`}>
              <input
                type="checkbox"
                checked={!!selected[s.id]}
                disabled={s.count === 0}
                onChange={() => setSelected((prev) => ({ ...prev, [s.id]: !prev[s.id] }))}
              />
              <span className="backup-section-name">{sectionLabel(s.id)}</span>
              <span className="status-pill">{s.count}</span>
            </label>
          ))}
        </div>
        <div className="field-grid">
          <label className="field">
            <span>{t('st.recPassphrase')}</span>
            <PasswordField value={pass} onChange={setPass} autoComplete="new-password" />
          </label>
          <label className="field">
            <span>{t('st.recConfirm')}</span>
            <PasswordField value={confirmPass} onChange={setConfirmPass} autoComplete="new-password" />
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" className="primary" onClick={createBackup} disabled={busy}>
            <span className="btn-icon"><Ico n="download" /> {t('st.bkupCreateBtn')}</span>
          </button>
        </div>

        <hr className="settings-divider" />

        <FieldTitle info={t('st.bkupRestoreInfo')}>{t('st.bkupRestoreHeading')}</FieldTitle>
        <p className="field-hint">{t('st.bkupRestoreHint')}</p>
        <div className="field-grid">
          <label className="field">
            <span>{t('st.bkupFile')}</span>
            <input type="file" accept=".mmbackup,application/octet-stream" onChange={onPickFile} />
          </label>
          <label className="field">
            <span>{t('st.recPassphrase')}</span>
            <PasswordField value={restorePass} onChange={setRestorePass} autoComplete="off" />
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" className="quiet" onClick={preview} disabled={busy}>
            <span className="btn-icon"><Ico n="search" /> {t('st.bkupPreviewBtn')}</span>
          </button>
        </div>

        {manifest ? (
          <div className="auto-tune-result">
            <strong>{t('st.bkupManifestTitle')}</strong>
            <ul>
              <li>{t('st.bkupMadeWith', { version: manifest.appVersion || '—' })}</li>
              {manifestSections.map((id) => (
                <li key={id}>{sectionLabel(id)} — {(manifest.counts && manifest.counts[id]) || 0}</li>
              ))}
            </ul>
            <label className="field">
              <span>{t('st.bkupMode')}</span>
              <select value={mode} onChange={(e) => setMode(e.target.value)}>
                <option value="replace">{t('st.bkupModeReplace')}</option>
                <option value="merge">{t('st.bkupModeMerge')}</option>
              </select>
            </label>
            <p className="settings-hint danger-text"><span className="btn-icon"><Ico n="warning" /></span> {t('st.bkupRestoreWarn')}</p>
            <div className="settings-actions">
              <button type="button" className="danger-solid" onClick={restore} disabled={busy}>
                <span className="btn-icon"><Ico n="reload" /> {t('st.bkupRestoreBtn')}</span>
              </button>
            </div>
          </div>
        ) : null}

        {result ? (
          <div className="auto-tune-result">
            <strong>{t('st.bkupDoneTitle')}</strong>
            <ul>
              {Object.keys(result.restored || {}).map((id) => (
                <li key={id}>{sectionLabel(id)} — {result.restored[id]}{result.skipped && result.skipped[id] ? ` (${result.skipped[id]} skipped)` : ''}</li>
              ))}
              {result.schemaWarning ? <li className="danger-text">{result.schemaWarning}</li> : null}
            </ul>
            {onRestart ? (
              <div className="settings-actions">
                <button type="button" className="primary" onClick={() => onRestart()}>
                  <span className="btn-icon"><Ico n="refresh" /> {t('st.bkupRestartBtn')}</span>
                </button>
              </div>
            ) : null}
          </div>
        ) : null}
      </section>
    </div>
  );
}

// RecoveryKeyPanel lets an admin export a passphrase-protected copy of the encryption-at-
// rest master key (a "recovery escrow") and verify a saved copy still works. It matters
// most for host-bound protectors (DPAPI/systemd-creds): their key can't be unwrapped on
// another machine, so without this escrow a hardware failure or reimage would render all
// recordings permanently unreadable. The actual restore is a documented offline step —
// when a host-bound key is unusable the app won't boot, so there's no live button for it.
function RecoveryKeyPanel({ authHeader }) {
  const t = useT();
  const [state, setState] = useState(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState(null);
  const [pass, setPass] = useState('');
  const [confirmPass, setConfirmPass] = useState('');
  const [passErr, setPassErr] = useState('');
  const [confirmErr, setConfirmErr] = useState('');
  const [verifyPass, setVerifyPass] = useState('');
  const [verifyData, setVerifyData] = useState('');

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, status: resp.status, body };
  }

  useEffect(() => {
    let alive = true;
    api('/api/system/recovery/state').then((r) => { if (alive && r.ok) setState(r.body); }).catch(() => {});
    return () => { alive = false; };
    /* eslint-disable-next-line */
  }, [authHeader]);

  async function exportKey() {
    // Field-level validation so the message sits next to the offending input, not adrift at
    // the top of the panel where it reads like a section label.
    setPassErr(''); setConfirmErr('');
    if (pass.length < 8) { setPassErr(t('st.recPassMin')); return; }
    if (pass !== confirmPass) { setConfirmErr(t('st.recPassMismatch')); return; }
    setBusy(true);
    try {
      const r = await api('/api/system/recovery/export', { method: 'POST', body: JSON.stringify({ passphrase: pass }) });
      if (!r.ok || !r.body?.keyBase64) { setMsg({ kind: 'error', text: r.body?.message || t('st.recExportFail') }); return; }
      // Download the raw key bytes so the file is a drop-in: restoring is just placing this
      // file at the key path (recovery.atrestkey) — no manual decoding step.
      const raw = Uint8Array.from(atob(r.body.keyBase64), (c) => c.charCodeAt(0));
      const blob = new Blob([raw], { type: 'application/octet-stream' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = r.body.filename || 'mymatasan-recovery.atrestkey';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      setMsg({ kind: 'ok', text: t('st.recExportOk') });
      setPass(''); setConfirmPass('');
    } catch (err) {
      setMsg({ kind: 'error', text: err?.message || t('st.recExportFail') });
    } finally {
      setBusy(false);
    }
  }

  async function onPickFile(e) {
    const file = e.target.files && e.target.files[0];
    if (!file) return;
    try {
      // The recovery file is raw bytes; base64-encode them for the JSON verify request.
      const bytes = new Uint8Array(await file.arrayBuffer());
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      setVerifyData(btoa(bin));
      setMsg(null);
    } catch (_) { setMsg({ kind: 'error', text: t('st.recReadFail') }); }
  }

  async function verifyKey() {
    if (!verifyData || !verifyPass) { setMsg({ kind: 'error', text: t('st.recVerifyNeed') }); return; }
    setBusy(true);
    try {
      const r = await api('/api/system/recovery/verify', { method: 'POST', body: JSON.stringify({ passphrase: verifyPass, keyBase64: verifyData }) });
      if (!r.ok) { setMsg({ kind: 'error', text: r.body?.message || t('st.recVerifyFail') }); return; }
      if (!r.body?.valid) { setMsg({ kind: 'error', text: r.body?.error || t('st.recVerifyBad') }); return; }
      setMsg({ kind: 'ok', text: r.body.matchesCurrent ? t('st.recVerifyMatch') : t('st.recVerifyValidNoMatch') });
    } catch (err) {
      setMsg({ kind: 'error', text: err?.message || t('st.recVerifyFail') });
    } finally {
      setBusy(false);
    }
  }

  if (state && !state.enabled) {
    return (
      <div className="settings-layout">
        <section className="settings-panel span-two">
          <header><h2><span className="btn-icon"><Ico n="key" /> {t('st.recTitle')}</span></h2></header>
          <p className="settings-hint">{t('st.recDisabled')}</p>
        </section>
      </div>
    );
  }

  const protector = state?.protector || '';
  const hostBound = !!state?.hostBound;

  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="key" /> {t('st.recTitle')}</span></h2></header>
        <p className="settings-hint">{t('st.recIntro')}</p>
        {protector ? (
          <div className="machine-metrics">
            <div className="machine-metric-card">
              <dt>{t('st.recProtector')}</dt>
              <dd><strong className="status-pill">{protector}</strong></dd>
              <span className="field-hint">{hostBound ? t('st.recHostBound') : t('st.recPortable')}</span>
            </div>
          </div>
        ) : null}
        {hostBound ? (
          <p className="settings-hint danger-text"><span className="btn-icon"><Ico n="warning" /></span> {t('st.recHostBoundWarn')}</p>
        ) : null}
        {msg ? <p className={msg.kind === 'error' ? 'settings-hint danger-text' : 'settings-hint'}>{msg.text}</p> : null}

        <FieldTitle info={t('st.recExportInfo')}>{t('st.recExportHeading')}</FieldTitle>
        <p className="field-hint">{t('st.recExportHint')}</p>
        <div className="field-grid">
          <label className="field">
            <span>{t('st.recPassphrase')}</span>
            <PasswordField value={pass} onChange={(v) => { setPass(v); if (passErr) setPassErr(''); }} autoComplete="new-password" error={!!passErr} />
            {passErr ? <span className="field-hint danger-text">{passErr}</span> : null}
          </label>
          <label className="field">
            <span>{t('st.recConfirm')}</span>
            <PasswordField value={confirmPass} onChange={(v) => { setConfirmPass(v); if (confirmErr) setConfirmErr(''); }} autoComplete="new-password" error={!!confirmErr} />
            {confirmErr ? <span className="field-hint danger-text">{confirmErr}</span> : null}
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" className="primary" onClick={exportKey} disabled={busy}>
            <span className="btn-icon"><Ico n="download" /> {t('st.recExportBtn')}</span>
          </button>
        </div>

        <hr className="settings-divider" />

        <FieldTitle info={t('st.recVerifyInfo')}>{t('st.recVerifyHeading')}</FieldTitle>
        <p className="field-hint">{t('st.recVerifyHint')}</p>
        <div className="field-grid">
          <label className="field">
            <span>{t('st.recFile')}</span>
            <input type="file" accept=".atrestkey,application/octet-stream" onChange={onPickFile} />
          </label>
          <label className="field">
            <span>{t('st.recPassphrase')}</span>
            <PasswordField value={verifyPass} onChange={setVerifyPass} autoComplete="off" />
          </label>
        </div>
        <div className="settings-actions">
          <button type="button" className="quiet" onClick={verifyKey} disabled={busy}>
            <span className="btn-icon"><Ico n="shield" /> {t('st.recVerifyBtn')}</span>
          </button>
        </div>
        <p className="field-hint">{t('st.recRestoreNote')}</p>
      </section>
    </div>
  );
}

// SecureWipePanel is the factory-reset "Danger Zone". It lives in the Backup & Recovery tab
// (alongside the recovery key) since wipe/reset is the destructive counterpart to backup. It
// only renders when the deployment permits reset (bootstrap.allowReset → resetAllowed).
function SecureWipePanel({ resetAllowed, onSecureWipe }) {
  const t = useT();
  if (!resetAllowed || !onSecureWipe) return null;
  return (
    <div className="settings-layout">
      <section className="settings-panel span-two danger-zone">
        <header>
          <h2><span className="btn-icon"><Ico n="warning" /> {t('st.dangerZone')}</span></h2>
        </header>
        <p className="settings-hint">{t('st.dangerHint')}</p>
        <button type="button" className="danger-solid" onClick={onSecureWipe}>
          <span className="btn-icon"><Ico n="warning" /> {t('st.secureWipe')}</span>
        </button>
      </section>
    </div>
  );
}

export function MachineHealthSettingsPanel({ settings, busy, hasChanges, metrics, onChange, onSave, onDiscard, onRefreshMetrics, capacity, onEstimateCapacity, onCalibrateCapacity }) {
  const t = useT();
  const d = defaultMachineHealthSettings;
  const value = {
    ...d,
    ...(settings || {}),
    cpu: { ...d.cpu, ...(settings?.cpu || {}) },
    memory: { ...d.memory, ...(settings?.memory || {}) },
    disk: { ...d.disk, ...(settings?.disk || {}), paths: Array.isArray(settings?.disk?.paths) ? settings.disk.paths : [] },
    mitigation: { ...d.mitigation, ...(settings?.mitigation || {}) },
  };
  function patch(values) { onChange({ ...value, ...values }); }
  function patchCpu(v) { onChange({ ...value, cpu: { ...value.cpu, ...v } }); }
  function patchMem(v) { onChange({ ...value, memory: { ...value.memory, ...v } }); }
  function patchDisk(v) { onChange({ ...value, disk: { ...value.disk, ...v } }); }
  function patchMit(v) { onChange({ ...value, mitigation: { ...value.mitigation, ...v } }); }
  const intervalSec = Math.round(value.intervalMs / 1000);
  const disks = Array.isArray(metrics?.disks) ? metrics.disks : [];

  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="cpu" /> {t('st.capacityEstimate')}</span></h2>
          <div className="capacity-actions">
            <button type="button" className="quiet" onClick={() => onCalibrateCapacity && onCalibrateCapacity()} disabled={busy} title={t('st.runCalibrationTitle')}>
              <span className="btn-icon"><Ico n="wand" /> {t('st.runCalibration')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => onEstimateCapacity && onEstimateCapacity()} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('st.estimate')}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">
          {t('st.capacityHint', { extra: capacity?.confidence === 'measured' ? t('st.capExtraMeasured') : capacity?.confidence === 'calibrated' ? t('st.capExtraCalibrated') : '' })}
        </p>
        {capacity ? (
          <>
            <div className="capacity-headline">
              <span className="capacity-number">{capacity.estimatedMax}</span>
              <div className="capacity-headline-meta">
                <span className="capacity-caption">{t('st.estimatedCameras')}</span>
                <span className={`capacity-badge capacity-badge--${capacity.confidence}`}>
                  {capacity.confidence === 'measured' ? t('st.confMeasured') : capacity.confidence === 'calibrated' ? t('st.confCalibrated') : t('st.confBallpark')}
                </span>
                {capacity.limitingWorkload ? (
                  <span className="field-hint">{t('st.limitedBy', { x: (() => { const k = capacityLimitLabels[(capacity.workloads || []).find((w) => w.name === capacity.limitingWorkload)?.limit]; return k ? t(k) : capacity.limitingWorkload; })() })}</span>
                ) : null}
              </div>
            </div>
            <CapacityRetentionNote capacity={capacity} />
            <div className="capacity-grid">
              {(capacity.workloads || []).map((wl) => (
                <div className={`capacity-card${wl.name === capacity.limitingWorkload ? ' capacity-card--limit' : ''}`} key={wl.name}>
                  <dt>{wl.label}{!wl.continuous ? ' *' : ''}</dt>
                  <dd><strong>{wl.maxCameras}</strong> {t('st.cameras')}</dd>
                  <span className="field-hint">{wl.note}</span>
                </div>
              ))}
            </div>
            {Array.isArray(capacity.assumptions) && capacity.assumptions.length > 0 ? (
              <details className="capacity-assumptions">
                <summary>{t('st.assumptionsMethod')}</summary>
                <ul>{capacity.assumptions.map((a, i) => <li key={i}>{a}</li>)}</ul>
              </details>
            ) : null}
          </>
        ) : (
          <p className="empty">{t('st.clickEstimate')}</p>
        )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="cpu" /> {t('st.machineHealthMonitor')}</span></h2>
          <label className="check-row">
            <input type="checkbox" checked={value.enabled} onChange={(e) => patch({ enabled: e.target.checked })} />
            {t('common.enabled')}
          </label>
        </header>
        <p className="settings-hint">
          {t('st.machineHealthHint')}
        </p>

        <div className="machine-metrics">
          <div className="machine-metric-card">
            <dt>{t('st.capCpu')}</dt>
            <dd><strong className={`status-pill ${machineLevelClass(metrics?.cpuPercent ?? 0, value.cpu.warnPercent, value.cpu.criticalPercent)}`}>{metrics ? `${metrics.cpuPercent}%` : '—'}</strong></dd>
          </div>
          <div className="machine-metric-card">
            <dt>{t('st.memoryMetric')}</dt>
            <dd><strong className={`status-pill ${machineLevelClass(metrics?.memoryPercent ?? 0, value.memory.warnPercent, value.memory.criticalPercent)}`}>{metrics ? `${metrics.memoryPercent}%` : '—'}</strong></dd>
            {metrics ? <span className="field-hint">{formatBytes(metrics.memoryUsedBytes)} / {formatBytes(metrics.memoryTotalBytes)}</span> : null}
          </div>
          {disks.map((disk) => (
            <div className="machine-metric-card" key={disk.mountpoint}>
              <dt>{t('st.diskMount', { mount: disk.mountpoint })}</dt>
              <dd><strong className={`status-pill ${machineLevelClass(disk.usedPercent, value.disk.warnPercent, value.disk.criticalPercent)}`}>{disk.usedPercent}%</strong></dd>
              <span className="field-hint">{formatBytes(disk.freeBytes)} free of {formatBytes(disk.totalBytes)}</span>
            </div>
          ))}
          {metrics?.recordingPaused ? (
            <div className="machine-metric-card">
              <dt>{t('st.recording')}</dt>
              <dd><strong className="status-pill offline">{t('st.pausedDisk')}</strong></dd>
            </div>
          ) : null}
          <div className="machine-metrics-action">
            <button type="button" className="quiet" onClick={() => onRefreshMetrics && onRefreshMetrics()} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('st.checkNow')}</span>
            </button>
          </div>
        </div>

        <div className="settings-grid">
          <label>
            <FieldTitle info="How often host metrics are sampled. Minimum 5 seconds.">{t('st.sampleInterval')}</FieldTitle>
            <input type="number" min="5" step="5" value={intervalSec} onChange={(e) => patch({ intervalMs: Math.max(5, Number(e.target.value) || 0) * 1000 })} disabled={!value.enabled} />
          </label>
          <label>
            <FieldTitle info="Consecutive breaching samples before an alert fires (debounce against transient spikes).">{t('st.sustainedSamples')}</FieldTitle>
            <input type="number" min="1" max="60" value={value.sustainedSamples} onChange={(e) => patch({ sustainedSamples: Math.max(1, Number(e.target.value) || 0) })} disabled={!value.enabled} />
          </label>
          <label>
            <FieldTitle info="Consecutive normal samples before a recovery notice fires.">{t('st.recoverySamples')}</FieldTitle>
            <input type="number" min="1" max="60" value={value.recoverySamples} onChange={(e) => patch({ recoverySamples: Math.max(1, Number(e.target.value) || 0) })} disabled={!value.enabled} />
          </label>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header><h2>{t('st.thresholds')}</h2></header>
        <p className="settings-hint">{t('st.thresholdsHint')}</p>
        <div className="settings-field-grid">
          <label><FieldTitle info="CPU usage % that raises a Warning.">{t('st.cpuWarn')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.cpu.warnPercent} onChange={(e) => patchCpu({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="CPU usage % that raises a Critical.">{t('st.cpuCrit')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.cpu.criticalPercent} onChange={(e) => patchCpu({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Memory usage % that raises a Warning.">{t('st.memWarn')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.memory.warnPercent} onChange={(e) => patchMem({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Memory usage % that raises a Critical.">{t('st.memCrit')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.memory.criticalPercent} onChange={(e) => patchMem({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Disk fullness % that raises a Warning.">{t('st.diskWarn')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.disk.warnPercent} onChange={(e) => patchDisk({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Disk fullness % that raises a Critical.">{t('st.diskCrit')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.disk.criticalPercent} onChange={(e) => patchDisk({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
        </div>
        <label>
          <FieldTitle info="Extra disk paths/volumes to monitor, one per line. The volumes the app writes to (recordings, logs, working dir) are monitored automatically.">
            {t('st.extraDiskPaths')}
          </FieldTitle>
          <textarea
            rows="2"
            value={(value.disk.paths || []).join('\n')}
            onChange={(e) => patchDisk({ paths: e.target.value.split(/\r?\n/).map((p) => p.trim()).filter(Boolean) })}
            placeholder={'C:\\\nE:\\media'}
            disabled={!value.enabled}
          />
        </label>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> {t('st.diskMitigation')}</span></h2>
          <label className="check-row">
            <input type="checkbox" checked={value.mitigation.enabled} onChange={(e) => patchMit({ enabled: e.target.checked })} disabled={!value.enabled} />
            {t('common.enabled')}
          </label>
        </header>
        <p className="settings-hint">
          {t('st.diskMitigationHint')}
        </p>
        <div className="settings-grid">
          <label>
            <FieldTitle info="When a monitored disk reaches this fullness, immediately purge expired recordings (only deletes footage already past its retention).">{t('st.earlyPurgeAt')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.mitigation.purgeAtPercent} onChange={(e) => patchMit({ purgeAtPercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
          <label>
            <FieldTitle info="When a monitored disk reaches this fullness, pause NVR recording to stop the volume filling completely.">{t('st.pauseRecordingAt')}</FieldTitle>
            <input type="number" min="1" max="100" value={value.mitigation.pauseRecordingAtPercent} onChange={(e) => patchMit({ pauseRecordingAtPercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
          <label>
            <FieldTitle info="Recording resumes once the disk drops below this fullness (hysteresis). Must be below the pause threshold.">{t('st.resumeBelow')}</FieldTitle>
            <input type="number" min="1" max="99" value={value.mitigation.resumePercent} onChange={(e) => patchMit({ resumePercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={!!value.mitigation.overwriteOldest} onChange={(e) => patchMit({ overwriteOldest: e.target.checked })} disabled={!value.enabled || !value.mitigation.enabled} />
          {t('st.overwriteOldest')}
        </label>
        {value.mitigation.overwriteOldest ? (
          <div className="settings-grid">
            <label>
              <FieldTitle info="Safety floor for overwrite mode: footage newer than this many days is never auto-deleted. If the disk fills up with only footage inside this window, recording pauses as a last resort.">{t('st.overwriteKeepDays')}</FieldTitle>
              <input type="number" min="1" max="365" value={value.mitigation.overwriteMinKeepDays} onChange={(e) => patchMit({ overwriteMinKeepDays: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
            </label>
          </div>
        ) : null}
      </section>

      <div className="settings-actions">
        <button type="submit" disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="save" /> {t('st.saveSettings')}</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="undo" /> {t('st.discardChanges')}</span>
        </button>
      </div>
    </form>
  );
}

