import { useCallback, useEffect, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage } from '../lib/helpers';
import { ConfirmModal, Field, Panel } from './ui';
import { ActionValue } from './scenes';

// Schedules: fire a scene or a command on the clock, or at sunrise/sunset.
//
// A schedule is the automation a telemetry rule cannot express — its trigger is a TIME, not a
// reading. It fires through the same actuation path everything else does, so authoring one and
// test-firing it are admin-only; a viewer can see the schedules but not fire them.

const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6];
const TRIGGERS = ['clock', 'sunrise', 'sunset'];
const EMPTY_SCHEDULE = {
  name: '', enabled: true, triggerType: 'clock', timeOfDay: '07:30', offsetMinutes: 0,
  days: '', targetType: 'scene', sceneId: 0, deviceId: 0, commandName: '', value: 0,
};

export function SchedulesPage({ onToast, session }) {
  const t = useT();
  const isAdmin = !!session?.isAdmin;
  const [schedules, setSchedules] = useState([]);
  const [scenes, setScenes] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    const [sc, scn] = await Promise.all([api('/api/schedules'), api('/api/scenes')]);
    if (sc.ok) { setSchedules(sc.body?.items || []); setError(''); }
    else setError(errorMessage(sc, t('sched.loadFailed')));
    if (scn.ok) setScenes(scn.body?.items || []);
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);

  async function runNow(sched) {
    const r = await api(`/api/schedules/${sched.id}/run`, { method: 'POST', body: '{}' });
    if (!r.ok) { onToast(errorMessage(r, t('sched.runFailed')), 'error'); return; }
    onToast(t('sched.fired', { name: sched.name }), 'success');
  }

  async function remove(sched) {
    setDeleting(true);
    const r = await api(`/api/schedules/${sched.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) { onToast(errorMessage(r, t('sched.deleteFailed')), 'error'); return; }
    onToast(t('sched.deleted', { name: sched.name }), 'success');
    load();
  }

  if (editing !== null) {
    return (
      <ScheduleEditor
        schedule={editing}
        scenes={scenes}
        onBack={() => setEditing(null)}
        onSaved={() => { setEditing(null); load(); }}
        onToast={onToast}
      />
    );
  }

  const triggerText = (row) => {
    if (row.triggerType === 'clock') return t('sched.atTime', { time: row.timeOfDay });
    const off = row.offsetMinutes;
    const base = t(`sched.trigger.${row.triggerType}`);
    if (!off) return base;
    return off > 0 ? t('sched.afterEvent', { min: off, event: base }) : t('sched.beforeEvent', { min: -off, event: base });
  };

  const columns = [
    { key: 'name', label: t('sched.name'), render: (v, row) => (
      <button type="button" className="quiet iot-link-cell" onClick={() => setEditing(row)}>{v}</button>
    ) },
    { key: 'trigger', label: t('sched.trigger'), filterable: false, render: (_v, row) => triggerText(row) },
    { key: 'targetType', label: t('sched.target'), render: (v, row) => (
      v === 'scene'
        ? (scenes.find((s) => s.id === row.sceneId)?.name || t('sched.aScene'))
        : `${row.commandName}`
    ) },
    { key: 'enabled', label: t('sched.enabled'), filterType: 'boolean', render: (v) => (
      v ? <span className="status-pill resolved">{t('sched.on')}</span> : <span className="status-pill">{t('sched.off')}</span>
    ) },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => (
      <div className="table-actions">
        {isAdmin ? (
          <button type="button" className="quiet" onClick={() => runNow(row)}>
            <span className="btn-icon"><Ico n="send" sz={14} /> {t('sched.testFire')}</span>
          </button>
        ) : null}
        <button type="button" className="quiet" onClick={() => setEditing(row)}>{t('common.edit')}</button>
        <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}>{t('common.delete')}</button>
      </div>
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="clock"
        title={t('page.schedules')}
        hint={t('page.schedulesHint')}
        actions={(
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" onClick={() => setEditing(EMPTY_SCHEDULE)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('sched.add')}</span>
            </button>
          </>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        {!isAdmin ? <p className="field-hint">{t('sched.viewerHint')}</p> : null}
        <DataTable rows={schedules} columns={columns} busy={busy} pageSize={10} emptyText={t('sched.empty')} />
      </Panel>

      {isAdmin ? <LocationPanel onToast={onToast} /> : null}

      {confirmDelete ? (
        <ConfirmModal
          title={t('sched.deleteTitle')}
          body={t('sched.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')}
          busy={deleting}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => remove(confirmDelete)}
        />
      ) : null}
    </section>
  );
}

function ScheduleEditor({ schedule, scenes, onBack, onSaved, onToast }) {
  const t = useT();
  const [form, setForm] = useState({ ...EMPTY_SCHEDULE, ...schedule });
  const [devices, setDevices] = useState([]);
  const [decls, setDecls] = useState([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const loadCommands = useCallback(async (deviceId) => {
    if (!deviceId) { setDecls([]); return; }
    const r = await api(`/api/devices/${deviceId}/commands`);
    setDecls(r.ok ? (r.body?.items || []) : []);
  }, []);

  useEffect(() => {
    (async () => {
      const dv = await api('/api/devices');
      if (dv.ok) setDevices(dv.body?.items || []);
    })();
    if (schedule?.deviceId) loadCommands(schedule.deviceId);
  }, [schedule, loadCommands]);

  function toggleDay(day) {
    const cur = new Set((form.days || '').split(',').map((s) => s.trim()).filter(Boolean));
    cur.has(String(day)) ? cur.delete(String(day)) : cur.add(String(day));
    set({ days: [...cur].sort().join(',') });
  }
  const dayOn = (day) => (form.days || '').split(',').includes(String(day));

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const payload = {
      name: form.name,
      enabled: !!form.enabled,
      triggerType: form.triggerType,
      timeOfDay: form.triggerType === 'clock' ? form.timeOfDay : '',
      offsetMinutes: form.triggerType === 'clock' ? 0 : Number(form.offsetMinutes) || 0,
      days: form.days || '',
      targetType: form.targetType,
      sceneId: form.targetType === 'scene' ? Number(form.sceneId) || 0 : 0,
      deviceId: form.targetType === 'command' ? Number(form.deviceId) || 0 : 0,
      commandName: form.targetType === 'command' ? form.commandName : '',
      value: Number(form.value) || 0,
    };
    const r = form.id
      ? await api(`/api/schedules/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      : await api('/api/schedules', { method: 'POST', body: JSON.stringify(payload) });
    setBusy(false);
    if (!r.ok) { setError(errorMessage(r, t('sched.saveFailed'))); return; }
    onToast(t('sched.saved', { name: payload.name }), 'success');
    onSaved();
  }

  const decl = decls.find((d) => d.name === form.commandName);

  return (
    <section className="workspace">
      <Panel icon="clock" title={form.id ? t('sched.editTitle') : t('sched.newTitle')}
        actions={<button type="button" className="quiet" onClick={onBack}><span className="btn-icon"><Ico n="arrow-left" sz={14} /> {t('common.back')}</span></button>}>
        {error ? <div className="status-line">{error}</div> : null}
        <form onSubmit={save} className="iot-form">
          <div className="form-grid">
            <Field label={t('sched.name')}>
              <input value={form.name || ''} required onChange={(e) => set({ name: e.target.value })} />
            </Field>
            <Field label={t('sched.enabled')}>
              <label className="iot-checkbox">
                <input type="checkbox" checked={!!form.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
                <span>{t('sched.enabledHint')}</span>
              </label>
            </Field>
          </div>

          <h3 className="iot-subhead">{t('sched.when')}</h3>
          <div className="form-grid">
            <Field label={t('sched.trigger')}>
              <select value={form.triggerType} onChange={(e) => set({ triggerType: e.target.value })}>
                {TRIGGERS.map((tr) => <option key={tr} value={tr}>{t(`sched.trigger.${tr}`)}</option>)}
              </select>
            </Field>
            {form.triggerType === 'clock' ? (
              <Field label={t('sched.timeOfDay')}>
                <input type="time" value={form.timeOfDay || ''} onChange={(e) => set({ timeOfDay: e.target.value })} />
              </Field>
            ) : (
              <Field label={t('sched.offset')} hint={t('sched.offsetHint')}>
                <input type="number" step="1" value={form.offsetMinutes ?? 0} onChange={(e) => set({ offsetMinutes: e.target.value })} />
              </Field>
            )}
          </div>
          <div className="schedule-days">
            {WEEKDAYS.map((d) => (
              <button key={d} type="button" className={dayOn(d) ? 'active' : 'quiet'} onClick={() => toggleDay(d)}>
                {t(`day.${d}`)}
              </button>
            ))}
          </div>
          <p className="field-hint">{t('sched.daysHint')}</p>

          <h3 className="iot-subhead">{t('sched.doWhat')}</h3>
          <div className="form-grid">
            <Field label={t('sched.target')}>
              <select value={form.targetType} onChange={(e) => set({ targetType: e.target.value })}>
                <option value="scene">{t('sched.aScene')}</option>
                <option value="command">{t('sched.aCommand')}</option>
              </select>
            </Field>
            {form.targetType === 'scene' ? (
              <Field label={t('sched.scene')}>
                <select value={form.sceneId || ''} onChange={(e) => set({ sceneId: Number(e.target.value) })}>
                  <option value="">{t('sched.pickScene')}</option>
                  {scenes.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                </select>
              </Field>
            ) : (
              <>
                <Field label={t('sched.device')}>
                  <select value={form.deviceId || ''} onChange={(e) => { set({ deviceId: Number(e.target.value), commandName: '', value: 0 }); loadCommands(Number(e.target.value)); }}>
                    <option value="">{t('sched.pickDevice')}</option>
                    {devices.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
                  </select>
                </Field>
                <Field label={t('sched.command')}>
                  <select value={form.commandName || ''} disabled={!form.deviceId} onChange={(e) => set({ commandName: e.target.value, value: 0 })}>
                    <option value="">{t('sched.pickCommand')}</option>
                    {decls.map((d) => <option key={d.name} value={d.name}>{d.label || d.name}</option>)}
                  </select>
                </Field>
                <Field label={t('sched.value')}>
                  <ActionValue decl={decl} value={form.value} onChange={(v) => set({ value: v })} />
                </Field>
              </>
            )}
          </div>

          <div className="modal-actions">
            <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
          </div>
        </form>
      </Panel>
    </section>
  );
}

// LocationPanel sets the site latitude/longitude sunrise/sunset schedules need. Without it, a sun
// schedule refuses to save — the server has no way to compute the sun's times.
function LocationPanel({ onToast }) {
  const t = useT();
  const [lat, setLat] = useState('');
  const [lon, setLon] = useState('');
  const [set, setSet] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api('/api/settings/location').then((r) => {
      if (r.ok) { setSet(!!r.body?.set); if (r.body?.set) { setLat(String(r.body.latitude)); setLon(String(r.body.longitude)); } }
    });
  }, []);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    const r = await api('/api/settings/location', { method: 'PUT', body: JSON.stringify({ latitude: Number(lat), longitude: Number(lon) }) });
    setBusy(false);
    if (!r.ok) { onToast(errorMessage(r, t('sched.locationFailed')), 'error'); return; }
    setSet(true);
    onToast(t('sched.locationSaved'), 'success');
  }

  return (
    <Panel icon="map-pin" title={t('sched.location')} hint={t('sched.locationHint')}>
      <form onSubmit={save} className="iot-form">
        <div className="form-grid">
          <Field label={t('sched.latitude')}>
            <input type="number" step="any" min="-90" max="90" value={lat} onChange={(e) => setLat(e.target.value)} required />
          </Field>
          <Field label={t('sched.longitude')}>
            <input type="number" step="any" min="-180" max="180" value={lon} onChange={(e) => setLon(e.target.value)} required />
          </Field>
        </div>
        <div className="modal-actions">
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : (set ? t('sched.updateLocation') : t('sched.setLocation'))}</button>
        </div>
      </form>
    </Panel>
  );
}
