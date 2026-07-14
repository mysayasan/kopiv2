import { useCallback, useEffect, useMemo, useState } from 'react';
import { DataTable, Ico, Tabs, useT } from '@shared';
import { ChartCard, LineChart } from '@shared/charts';
import { accessError, api, forbidden, formatAgo, formatClock, formatNumber, formatTimestamp } from './lib/helpers';
import { AccessDenied, CheckRow, ConfirmModal, CopyButton, EmptyState, Field, FormBusyOverlay, HealthPill, Modal, Panel } from './ui';
import { DeviceControl } from './commands';

const PROTOCOLS = ['mqtt', 'http'];

// RANGES are the chart lookbacks, in seconds. A sensor estate is read at two very
// different zooms — "what is it doing right now" and "did it drift over the week" — so
// both are one click away rather than a date picker.
const RANGES = [
  { id: '1h', secs: 3600 },
  { id: '6h', secs: 6 * 3600 },
  { id: '24h', secs: 24 * 3600 },
  { id: '7d', secs: 7 * 24 * 3600 },
];

// DevicesPage is the inventory: the estate as a table, and one device at a time as a
// detail view with its live values and its history.
export function DevicesPage({ onToast }) {
  const t = useT();
  const [devices, setDevices] = useState([]);
  const [profiles, setProfiles] = useState([]);
  const [busy, setBusy] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [denied, setDenied] = useState(false);
  const [openId, setOpenId] = useState(0);
  const [creating, setCreating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);
  // credential is the one-time broker password: shown once, on create or rotate, and
  // never again — the node keeps only a bcrypt hash of it.
  const [credential, setCredential] = useState(null);

  const load = useCallback(async () => {
    setBusy(true);
    const [dr, pr] = await Promise.all([
      api('/api/devices?limit=1000'),
      api('/api/profiles'),
    ]);
    if (dr.ok) {
      setDevices(dr.body?.items || []);
      setLoadError('');
      setDenied(false);
    } else if (forbidden(dr)) {
      // Not "no devices" — "not for you". Say which.
      setDenied(true);
      setLoadError('');
    } else {
      setDenied(false);
      setLoadError(accessError(dr, t, t('devices.loadFailed')));
    }
    if (pr.ok) setProfiles(pr.body?.items || []);
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);

  const profileName = useCallback((id) => {
    const p = profiles.find((x) => x.id === id);
    return p ? p.name : (id ? t('devices.unknownProfile') : t('devices.noProfile'));
  }, [profiles, t]);

  async function removeDevice(device) {
    setDeleting(true);
    const r = await api(`/api/devices/${device.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) {
      // Creating and deleting devices is admin-at-the-node. An operator who tries it is told
      // so plainly, in the same words everywhere in this embed.
      onToast(accessError(r, t, t('devices.deleteFailed')), 'error');
      return;
    }
    onToast(t('devices.deleted', { name: device.name }), 'success');
    if (openId === device.id) setOpenId(0);
    load();
  }

  const openDevice = devices.find((d) => d.id === openId);
  if (openId && openDevice) {
    return (
      <DeviceDetail
        device={openDevice}
        profiles={profiles}
        onBack={() => setOpenId(0)}
        onToast={onToast}
        onChanged={load}
        onCredential={setCredential}
        credential={credential}
        onCredentialClose={() => setCredential(null)}
      />
    );
  }

  const columns = [
    { key: 'name', label: t('devices.name'), render: (v, row) => (
      <button type="button" className="quiet iot-link-cell" onClick={() => setOpenId(row.id)}>{v}</button>
    ) },
    { key: 'deviceKey', label: t('devices.deviceKey') },
    { key: 'profileId', label: t('devices.profile'), filterType: 'number', render: (v) => profileName(v) },
    { key: 'tag', label: t('devices.tag') },
    { key: 'location', label: t('devices.location') },
    { key: 'health', label: t('devices.health'), render: (v) => <HealthPill health={v} /> },
    { key: 'lastSeenAt', label: t('devices.lastSeen'), filterType: 'number', render: (v) => (
      <span title={formatTimestamp(v)}>{formatAgo(v, t)}</span>
    ) },
    { key: 'enabled', label: t('devices.enabled'), filterType: 'boolean' },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => (
      <div className="table-actions">
        <button type="button" className="quiet" onClick={() => setOpenId(row.id)}>{t('devices.open')}</button>
        <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}>{t('common.delete')}</button>
      </div>
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="cpu"
        title={t('page.devices')}
        hint={t('page.devicesHint')}
        actions={denied ? null : (
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" onClick={() => setCreating(true)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('devices.add')}</span>
            </button>
          </>
        )}
      >
        {denied ? <AccessDenied /> : (
          <>
            {loadError ? <div className="status-line">{loadError}</div> : null}
            <DataTable
              rows={devices}
              columns={columns}
              busy={busy}
              pageSize={10}
              pageSizeOptions={[10, 25, 50, 100]}
              emptyText={t('devices.empty')}
            />
          </>
        )}
      </Panel>

      {creating ? (
        <CreateDeviceModal
          profiles={profiles}
          onClose={() => setCreating(false)}
          onCreated={(result) => {
            setCreating(false);
            setCredential({ password: result.password, name: result.device?.name, rotated: false });
            load();
          }}
          onToast={onToast}
        />
      ) : null}

      {credential ? (
        <CredentialModal credential={credential} onClose={() => setCredential(null)} />
      ) : null}

      {confirmDelete ? (
        <ConfirmModal
          title={t('devices.deleteTitle')}
          body={t('devices.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')}
          busy={deleting}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => removeDevice(confirmDelete)}
        />
      ) : null}
    </section>
  );
}

// CredentialModal is the one-time credential. The password in it exists in exactly one
// place in the world — this dialog — because the node stored only its bcrypt hash. So
// the dialog says so, in plain words, and makes copying it the obvious next act.
//
// Exported because adoption (discovery.js) mints exactly the same thing under exactly the
// same contract: a device adopted from a candidate gets a real, permanent credential shown
// once and never again. It must look and behave identically wherever it is minted.
export function CredentialModal({ credential, onClose }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  return (
    <Modal title={credential.rotated ? t('devices.credRotatedTitle') : t('devices.credTitle')} onClose={onClose}>
      <div className="iot-cred-warning">
        <Ico n="warning" sz={18} />
        <p>{t('devices.credWarning')}</p>
      </div>
      <p className="settings-hint">{t('devices.credFor', { name: credential.name || '' })}</p>
      <div className="iot-cred-value">
        <code>{credential.password}</code>
      </div>
      <div className="modal-actions">
        <CopyButton value={credential.password} label={t('devices.credCopy')} onCopied={() => setCopied(true)} />
        <button type="button" onClick={onClose} disabled={!copied} title={copied ? '' : t('devices.credMustCopy')}>
          {t('devices.credAck')}
        </button>
      </div>
      {!copied ? <p className="field-hint iot-cred-gate">{t('devices.credMustCopy')}</p> : null}
    </Modal>
  );
}

const EMPTY_DEVICE = {
  name: '', deviceKey: '', protocol: 'mqtt', profileId: 0, password: '',
  tag: '', location: '', vendor: '', model: '', enabled: true, actuationEnabled: false,
};

function CreateDeviceModal({ profiles, onClose, onCreated, onToast }) {
  const t = useT();
  const [form, setForm] = useState(EMPTY_DEVICE);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const r = await api('/api/devices', {
      method: 'POST',
      body: JSON.stringify({ ...form, profileId: Number(form.profileId) || 0 }),
    });
    setBusy(false);
    if (!r.ok) {
      // The node owns validation (a duplicate device key, a missing name) AND authorization
      // (creating a device is admin-at-the-node). Show what it said rather than guessing.
      setError(accessError(r, t, t('devices.createFailed')));
      return;
    }
    onToast(t('devices.created', { name: r.body?.device?.name || form.name }), 'success');
    onCreated(r.body || {});
  }

  return (
    <Modal title={t('devices.addTitle')} onClose={busy ? undefined : onClose} wide>
      <form className="device-edit-form" onSubmit={submit}>
        <FormBusyOverlay busy={busy} />
        {error ? <div className="status-line">{error}</div> : null}
        <div className="settings-field-grid">
          <Field label={t('devices.name')} required>
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} autoFocus />
          </Field>
          <Field label={t('devices.deviceKey')} hint={t('devices.deviceKeyHint')} required>
            <input value={form.deviceKey} onChange={(e) => set({ deviceKey: e.target.value })} />
          </Field>
          <Field label={t('devices.protocol')}>
            <select value={form.protocol} onChange={(e) => set({ protocol: e.target.value })}>
              {PROTOCOLS.map((p) => <option key={p} value={p}>{t(`protocol.${p}`)}</option>)}
            </select>
          </Field>
          <Field label={t('devices.profile')} hint={t('devices.profileHint')}>
            <select value={form.profileId} onChange={(e) => set({ profileId: Number(e.target.value) })}>
              <option value={0}>{t('devices.noProfile')}</option>
              {profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </Field>
          <Field label={t('devices.password')} hint={t('devices.passwordHint')} span>
            <input
              type="text"
              value={form.password}
              onChange={(e) => set({ password: e.target.value })}
              placeholder={t('devices.passwordPlaceholder')}
              autoComplete="off"
            />
          </Field>
          <Field label={t('devices.tag')} hint={t('devices.tagHint')}>
            <input value={form.tag} onChange={(e) => set({ tag: e.target.value })} />
          </Field>
          <Field label={t('devices.location')}>
            <input value={form.location} onChange={(e) => set({ location: e.target.value })} />
          </Field>
          <Field label={t('devices.vendor')}>
            <input value={form.vendor} onChange={(e) => set({ vendor: e.target.value })} />
          </Field>
          <Field label={t('devices.model')}>
            <input value={form.model} onChange={(e) => set({ model: e.target.value })} />
          </Field>
        </div>
        <CheckRow checked={form.enabled} onChange={(v) => set({ enabled: v })} label={t('devices.enabled')} />
        <CheckRow
          checked={form.actuationEnabled}
          onChange={(v) => set({ actuationEnabled: v })}
          label={t('devices.actuation')}
          hint={t('devices.actuationHint')}
        />
        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('devices.create')}</button>
        </div>
      </form>
    </Modal>
  );
}

// DeviceDetail is one device: what it is reporting right now, what it can be TOLD to do, and
// what has been done to it.
//
// Control is its own tab rather than a strip on the readings page, and deliberately: reading a
// sensor and firing a relay are different acts, and the second one deserves a place you have to
// mean to go to. That is true from the control plane too — arguably more so, because the person
// clicking it may be a building away from the relay.
function DeviceDetail({ device, profiles, onBack, onToast, onChanged, credential, onCredential, onCredentialClose }) {
  const t = useT();
  const [tab, setTab] = useState('readings');
  const profile = profiles.find((p) => p.id === device.profileId) || null;

  return (
    <section className="workspace">
      <Panel
        icon="cpu"
        title={device.name}
        hint={[profile ? profile.name : t('devices.noProfile'), device.location, device.tag].filter(Boolean).join(' · ')}
        actions={(
          <>
            <HealthPill health={device.health} />
            <button type="button" className="quiet" onClick={onBack}>
              <span className="btn-icon"><Ico n="arr-left" sz={14} /> {t('devices.backToList')}</span>
            </button>
          </>
        )}
      >
        <Tabs
          tabs={[
            { id: 'readings', label: t('devices.tabReadings'), icon: 'monitor' },
            // Control is shown to every role: the history and the twin on it are readable by
            // everyone with access to the node, and only the issuing controls inside it are
            // gated — by the NODE, which is the only thing that can gate them truthfully.
            { id: 'control', label: t('devices.tabControl'), icon: 'send' },
            { id: 'settings', label: t('devices.tabSettings'), icon: 'sliders' },
          ]}
          active={tab}
          onChange={setTab}
          ariaLabel={t('devices.tabsLabel')}
        />
        {tab === 'readings' ? <DeviceReadings device={device} /> : null}
        {tab === 'control' ? <DeviceControl device={device} onToast={onToast} /> : null}
        {tab === 'settings' ? (
          <DeviceSettings
            device={device}
            profiles={profiles}
            onToast={onToast}
            onChanged={onChanged}
            onCredential={onCredential}
            onBack={onBack}
          />
        ) : null}
      </Panel>
      {credential ? <CredentialModal credential={credential} onClose={onCredentialClose} /> : null}
    </section>
  );
}

// DeviceReadings shows the CURRENT value of every key the device reports, and the
// history of each numeric one. Nothing is drawn that was not measured: a device that has
// never published shows an empty state, not a flat line at zero.
//
// The two halves are separately authorized at the node, and that is not an accident of the
// matrix — it is the design. A VIEWER may see what a sensor says right now; the HISTORY (where
// the building was empty, when the cold room warmed) needs operator. So when the series calls
// come back 403 the current-value strip stays exactly as it is, and the charts are replaced by
// a plain statement of why they are not there. A blank chart would read as a broken sensor.
function DeviceReadings({ device }) {
  const t = useT();
  const [latest, setLatest] = useState({});
  const [keys, setKeys] = useState([]);
  const [series, setSeries] = useState({});
  const [range, setRange] = useState('24h');
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [latestDenied, setLatestDenied] = useState(false);
  const [historyDenied, setHistoryDenied] = useState(false);

  const rangeSecs = useMemo(() => (RANGES.find((r) => r.id === range) || RANGES[2]).secs, [range]);

  const loadLatest = useCallback(async () => {
    const r = await api(`/api/devices/${device.id}/latest`);
    if (r.ok) {
      setLatest(r.body || {});
      setLatestDenied(false);
      setError('');
    } else if (forbidden(r)) {
      setLatestDenied(true);
      setError('');
    } else {
      setError(accessError(r, t, t('devices.readingsFailed')));
    }
  }, [device.id, t]);

  // The profile declares the keys: their labels, units and — for the charts — which of
  // them are numbers. Without it we can still show whatever arrived, just unlabelled.
  const loadKeys = useCallback(async () => {
    if (!device.profileId) {
      setKeys([]);
      return;
    }
    const r = await api(`/api/profiles/${device.profileId}`);
    if (r.ok) setKeys(r.body?.keys || []);
  }, [device.profileId]);

  const loadSeries = useCallback(async (keyList) => {
    const to = Math.floor(Date.now() / 1000);
    const from = to - rangeSecs;
    const numeric = keyList.filter((k) => k.dataType !== 'string');
    const results = await Promise.all(numeric.map(async (k) => {
      const r = await api(`/api/devices/${device.id}/readings?key=${encodeURIComponent(k.key)}&from=${from}&to=${to}`);
      return [k.key, r];
    }));
    // One 403 is all of them: the grant is per-node, not per-key.
    setHistoryDenied(results.some(([, r]) => forbidden(r)));
    setSeries(Object.fromEntries(results.map(([key, r]) => [key, r.ok ? (r.body?.items || []) : []])));
  }, [device.id, rangeSecs]);

  const reload = useCallback(async () => {
    setBusy(true);
    await Promise.all([loadLatest(), loadKeys()]);
    setBusy(false);
  }, [loadLatest, loadKeys]);

  useEffect(() => { reload(); }, [reload]);

  // Chartable keys: the profile's, or — when the device has no profile — whatever it has
  // actually reported (synthesised from the latest values, so the page still works).
  const chartKeys = useMemo(() => {
    if (keys.length > 0) return keys;
    return Object.keys(latest).map((k) => ({ key: k, label: k, unit: '', dataType: 'number' }));
  }, [keys, latest]);

  useEffect(() => {
    if (chartKeys.length === 0) return;
    loadSeries(chartKeys);
  }, [chartKeys, loadSeries]);

  // Poll the live values (not the history) so the current-value strip stays current — but not
  // once the node has refused us. A refusal will not change while the page is open, and every
  // retry is another 403 that the commander AUDITS; a denied viewer idling here must not write
  // an audit row every fifteen seconds.
  useEffect(() => {
    if (latestDenied) return undefined;
    const id = setInterval(loadLatest, 15000);
    return () => clearInterval(id);
  }, [loadLatest, latestDenied]);

  const latestKeys = Object.keys(latest);
  const withDate = rangeSecs > 24 * 3600;

  if (latestDenied) {
    return <AccessDenied />;
  }

  return (
    <div className="iot-readings">
      <div className="toolbar">
        <p className="settings-hint">{t('devices.readingsHint')}</p>
        <div className="toolbar-actions">
          {historyDenied ? null : (
            <div className="segmented">
              {RANGES.map((r) => (
                <button
                  key={r.id}
                  type="button"
                  className={r.id === range ? 'active' : 'quiet'}
                  onClick={() => setRange(r.id)}
                >
                  {t(`range.${r.id}`)}
                </button>
              ))}
            </div>
          )}
          <button type="button" className="quiet" onClick={reload} disabled={busy}>
            <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
          </button>
        </div>
      </div>

      {error ? <div className="status-line">{error}</div> : null}

      {latestKeys.length === 0 && !busy ? (
        <EmptyState text={t('devices.noReadings')} />
      ) : (
        <div className="iot-latest-grid">
          {latestKeys.map((key) => {
            const reading = latest[key];
            const meta = chartKeys.find((k) => k.key === key);
            return (
              <div key={key} className={`iot-latest-card${reading.suspect ? ' is-suspect' : ''}`}>
                <span className="iot-latest-label">{meta?.label || key}</span>
                <span className="iot-latest-value">
                  {readingText(reading, meta, t)}
                  {meta?.unit ? <span className="iot-latest-unit">{meta.unit}</span> : null}
                </span>
                <span className="iot-latest-time" title={formatTimestamp(reading.ts)}>
                  {formatAgo(Math.floor(reading.ts / 1000), t)}
                </span>
                {reading.suspect ? (
                  <span className="iot-latest-suspect">
                    <Ico n="warning" sz={12} /> {t('devices.suspect')}
                  </span>
                ) : null}
              </div>
            );
          })}
        </div>
      )}

      {historyDenied ? (
        <AccessDenied text={t('niot.historyForbidden')} hint={t('niot.historyForbiddenHint')} />
      ) : (
        chartKeys
          .filter((k) => k.dataType !== 'string')
          .map((k) => {
            const points = (series[k.key] || []).map((r) => ({ t: r.ts, v: r.num, suspect: r.suspect }));
            return (
              <ChartCard
                key={k.key}
                title={k.label || k.key}
                subtitle={k.unit ? t('devices.chartSubtitleUnit', { unit: k.unit, range: t(`range.${range}`) }) : t('devices.chartSubtitle', { range: t(`range.${range}`) })}
              >
                <LineChart
                  points={points}
                  unit={k.unit || ''}
                  formatX={(ms) => formatClock(ms, withDate)}
                  emptyText={t('devices.noSeries')}
                />
              </ChartCard>
            );
          })
      )}
    </div>
  );
}

// readingText renders a value the way its declared type means it: a boolean key is a
// state (a door is open or closed), not the number 1.
function readingText(reading, meta, t) {
  if (!reading) return '—';
  if (meta?.dataType === 'bool') return reading.num ? t('common.yes') : t('common.no');
  if (meta?.dataType === 'string' || (reading.str && !reading.num)) return reading.str || '—';
  return formatNumber(reading.num);
}

function DeviceSettings({ device, profiles, onToast, onChanged, onCredential, onBack }) {
  const t = useT();
  const [form, setForm] = useState({
    name: device.name,
    profileId: device.profileId,
    tag: device.tag || '',
    location: device.location || '',
    vendor: device.vendor || '',
    model: device.model || '',
    enabled: !!device.enabled,
    actuationEnabled: !!device.actuationEnabled,
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const r = await api(`/api/devices/${device.id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...form, profileId: Number(form.profileId) || 0 }),
    });
    setBusy(false);
    if (!r.ok) {
      setError(accessError(r, t, t('devices.saveFailed')));
      return;
    }
    onToast(t('devices.saved'), 'success');
    onChanged();
  }

  async function rotate() {
    setBusy(true);
    const r = await api(`/api/devices/${device.id}/password`, { method: 'POST' });
    setBusy(false);
    setConfirmRotate(false);
    if (!r.ok) {
      onToast(accessError(r, t, t('devices.rotateFailed')), 'error');
      return;
    }
    onCredential({ password: r.body?.password, name: device.name, rotated: true });
  }

  async function remove() {
    setBusy(true);
    const r = await api(`/api/devices/${device.id}`, { method: 'DELETE' });
    setBusy(false);
    setConfirmDelete(false);
    if (!r.ok) {
      onToast(accessError(r, t, t('devices.deleteFailed')), 'error');
      return;
    }
    onToast(t('devices.deleted', { name: device.name }), 'success');
    onChanged();
    onBack();
  }

  return (
    <form className="device-edit-form" onSubmit={save}>
      <FormBusyOverlay busy={busy} />
      {error ? <div className="status-line">{error}</div> : null}
      <div className="settings-field-grid">
        <Field label={t('devices.name')} required>
          <input value={form.name} onChange={(e) => set({ name: e.target.value })} />
        </Field>
        <Field label={t('devices.deviceKey')} hint={t('devices.deviceKeyLocked')}>
          <input value={device.deviceKey} readOnly disabled />
        </Field>
        <Field label={t('devices.profile')} hint={t('devices.profileHint')}>
          <select value={form.profileId} onChange={(e) => set({ profileId: Number(e.target.value) })}>
            <option value={0}>{t('devices.noProfile')}</option>
            {profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </Field>
        <Field label={t('devices.tag')} hint={t('devices.tagHint')}>
          <input value={form.tag} onChange={(e) => set({ tag: e.target.value })} />
        </Field>
        <Field label={t('devices.location')}>
          <input value={form.location} onChange={(e) => set({ location: e.target.value })} />
        </Field>
        <Field label={t('devices.vendor')}>
          <input value={form.vendor} onChange={(e) => set({ vendor: e.target.value })} />
        </Field>
        <Field label={t('devices.model')}>
          <input value={form.model} onChange={(e) => set({ model: e.target.value })} />
        </Field>
        <Field label={t('devices.protocol')}>
          <input value={t(`protocol.${device.protocol}`)} readOnly disabled />
        </Field>
      </div>
      <CheckRow checked={form.enabled} onChange={(v) => set({ enabled: v })} label={t('devices.enabled')} />
      <CheckRow
        checked={form.actuationEnabled}
        onChange={(v) => set({ actuationEnabled: v })}
        label={t('devices.actuation')}
        hint={t('devices.actuationHint')}
      />

      <div className="meta-grid iot-meta">
        <div><span>{t('devices.serial')}</span><strong>{device.serial || '—'}</strong></div>
        <div><span>{t('devices.firmware')}</span><strong>{device.firmware || '—'}</strong></div>
        <div><span>{t('devices.lastSeen')}</span><strong>{device.lastSeenAt ? formatTimestamp(device.lastSeenAt) : t('time.never')}</strong></div>
        <div><span>{t('devices.createdAt')}</span><strong>{formatTimestamp(device.createdAt)}</strong></div>
      </div>

      <div className="action-row iot-actions-end">
        <button type="button" className="quiet" onClick={() => setConfirmRotate(true)} disabled={busy}>
          <span className="btn-icon"><Ico n="key" sz={14} /> {t('devices.rotate')}</span>
        </button>
        <button type="button" className="danger-solid" onClick={() => setConfirmDelete(true)} disabled={busy}>
          <span className="btn-icon"><Ico n="trash" sz={14} /> {t('common.delete')}</span>
        </button>
        <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
      </div>

      {confirmRotate ? (
        <ConfirmModal
          title={t('devices.rotateTitle')}
          body={t('devices.rotateBody', { name: device.name })}
          confirmLabel={t('devices.rotate')}
          busy={busy}
          danger={false}
          onCancel={() => setConfirmRotate(false)}
          onConfirm={rotate}
        />
      ) : null}
      {confirmDelete ? (
        <ConfirmModal
          title={t('devices.deleteTitle')}
          body={t('devices.deleteBody', { name: device.name })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onCancel={() => setConfirmDelete(false)}
          onConfirm={remove}
        />
      ) : null}
    </form>
  );
}
