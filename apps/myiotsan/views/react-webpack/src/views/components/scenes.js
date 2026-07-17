import { useCallback, useEffect, useMemo, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage, formatNumber } from '../lib/helpers';
import { ConfirmModal, EmptyState, Field, Modal, Panel } from './ui';
import { CommandStatusPill, describeValue, hexToInt, intToHex, parseOptions } from './commands';

// Scenes: named groups of device commands run together.
//
// A scene is convenience, not authority. Running one calls the SAME command endpoint a manual
// command does, once per action, so the server re-checks every gate (actuation enabled, admin only,
// declared bounds, rate limit) per action. This page never asserts a scene "succeeded" — it shows
// the per-action report the server returns, because a scene can partly fail and hiding that would be
// the same lie the whole actuation UI exists to avoid.

const EMPTY_SCENE = { name: '', description: '', enabled: true, actions: [] };

export function ScenesPage({ onToast, session }) {
  const t = useT();
  const isAdmin = !!session?.isAdmin;
  const [scenes, setScenes] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // scene id, 0 for new, null for list
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);
  const [runResult, setRunResult] = useState(null);
  const [running, setRunning] = useState(0);

  const load = useCallback(async () => {
    setBusy(true);
    const r = await api('/api/scenes');
    if (r.ok) { setScenes(r.body?.items || []); setError(''); }
    else setError(errorMessage(r, t('scenes.loadFailed')));
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);

  async function runScene(scene) {
    setRunning(scene.id);
    const r = await api(`/api/scenes/${scene.id}/run`, { method: 'POST', body: '{}' });
    setRunning(0);
    if (!r.ok) { onToast(errorMessage(r, t('scenes.runFailed')), 'error'); return; }
    setRunResult({ scene, result: r.body });
  }

  async function remove(scene) {
    setDeleting(true);
    const r = await api(`/api/scenes/${scene.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) { onToast(errorMessage(r, t('scenes.deleteFailed')), 'error'); return; }
    onToast(t('scenes.deleted', { name: scene.name }), 'success');
    load();
  }

  if (editing !== null) {
    return (
      <SceneEditor
        sceneId={editing}
        onBack={() => setEditing(null)}
        onSaved={() => { setEditing(null); load(); }}
        onToast={onToast}
      />
    );
  }

  const columns = [
    { key: 'name', label: t('scenes.name'), render: (v, row) => (
      <button type="button" className="quiet iot-link-cell" onClick={() => setEditing(row.id)}>{v}</button>
    ) },
    { key: 'description', label: t('scenes.description') },
    { key: 'enabled', label: t('scenes.enabled'), filterType: 'boolean', render: (v) => (
      v ? <span className="status-pill resolved">{t('scenes.on')}</span> : <span className="status-pill">{t('scenes.off')}</span>
    ) },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => (
      <div className="table-actions">
        {isAdmin ? (
          <button type="button" disabled={!row.enabled || running === row.id} onClick={() => runScene(row)}>
            <span className="btn-icon"><Ico n="send" sz={14} /> {running === row.id ? t('scenes.running') : t('scenes.run')}</span>
          </button>
        ) : null}
        <button type="button" className="quiet" onClick={() => setEditing(row.id)}>{t('common.edit')}</button>
        <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}>{t('common.delete')}</button>
      </div>
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="play"
        title={t('page.scenes')}
        hint={t('page.scenesHint')}
        actions={(
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" onClick={() => setEditing(0)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('scenes.add')}</span>
            </button>
          </>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        {!isAdmin ? <p className="field-hint">{t('scenes.viewerHint')}</p> : null}
        <DataTable rows={scenes} columns={columns} busy={busy} pageSize={10} emptyText={t('scenes.empty')} />
      </Panel>

      {confirmDelete ? (
        <ConfirmModal
          title={t('scenes.deleteTitle')}
          body={t('scenes.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')}
          busy={deleting}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => remove(confirmDelete)}
        />
      ) : null}

      {runResult ? <RunResultModal {...runResult} onClose={() => setRunResult(null)} /> : null}
    </section>
  );
}

// SceneEditor authors a scene: its name and its ordered actions. Each action picks a device, then
// one of that device's declared commands, then a value rendered as what that command IS.
function SceneEditor({ sceneId, onBack, onSaved, onToast }) {
  const t = useT();
  const [scene, setScene] = useState(EMPTY_SCENE);
  const [devices, setDevices] = useState([]);
  const [commandsByDevice, setCommandsByDevice] = useState({}); // deviceId -> [decl]
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');

  // Load a device's declared commands once, on demand, and cache them.
  const loadCommands = useCallback(async (deviceId) => {
    if (!deviceId) return;
    const r = await api(`/api/devices/${deviceId}/commands`);
    if (r.ok) setCommandsByDevice((m) => ({ ...m, [deviceId]: r.body?.items || [] }));
  }, []);

  useEffect(() => {
    let live = true;
    (async () => {
      setBusy(true);
      const dv = await api('/api/devices');
      if (live && dv.ok) setDevices(dv.body?.items || []);
      if (sceneId) {
        const r = await api(`/api/scenes/${sceneId}`);
        if (live && r.ok) {
          const sc = r.body?.scene || EMPTY_SCENE;
          const actions = (r.body?.actions || []).map((a) => ({ deviceId: a.deviceId, commandName: a.commandName, value: a.value }));
          setScene({ ...sc, actions });
          [...new Set(actions.map((a) => a.deviceId))].forEach(loadCommands);
        }
      } else {
        setScene(EMPTY_SCENE);
      }
      if (live) setBusy(false);
    })();
    return () => { live = false; };
  }, [sceneId, loadCommands]);

  const setActions = (fn) => setScene((s) => ({ ...s, actions: fn(s.actions) }));
  const addAction = () => setActions((list) => [...list, { deviceId: 0, commandName: '', value: 0 }]);
  const removeAction = (i) => setActions((list) => list.filter((_, idx) => idx !== i));
  const setAction = (i, patch) => setActions((list) => list.map((a, idx) => (idx === i ? { ...a, ...patch } : a)));

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const payload = {
      name: scene.name,
      description: scene.description || '',
      enabled: !!scene.enabled,
      actions: (scene.actions || []).filter((a) => a.deviceId && a.commandName)
        .map((a) => ({ deviceId: Number(a.deviceId), commandName: a.commandName, value: Number(a.value) || 0 })),
    };
    const r = sceneId
      ? await api(`/api/scenes/${sceneId}`, { method: 'PUT', body: JSON.stringify(payload) })
      : await api('/api/scenes', { method: 'POST', body: JSON.stringify(payload) });
    setBusy(false);
    if (!r.ok) { setError(errorMessage(r, t('scenes.saveFailed'))); return; }
    onToast(t('scenes.saved', { name: payload.name }), 'success');
    onSaved();
  }

  return (
    <section className="workspace">
      <Panel icon="play" title={sceneId ? t('scenes.editTitle') : t('scenes.newTitle')}
        actions={<button type="button" className="quiet" onClick={onBack}><span className="btn-icon"><Ico n="arrow-left" sz={14} /> {t('common.back')}</span></button>}>
        {error ? <div className="status-line">{error}</div> : null}
        <form onSubmit={save} className="iot-form">
          <div className="form-grid">
            <Field label={t('scenes.name')}>
              <input value={scene.name || ''} required onChange={(e) => setScene((s) => ({ ...s, name: e.target.value }))} />
            </Field>
            <Field label={t('scenes.enabled')}>
              <label className="iot-checkbox">
                <input type="checkbox" checked={!!scene.enabled} onChange={(e) => setScene((s) => ({ ...s, enabled: e.target.checked }))} />
                <span>{t('scenes.enabledHint')}</span>
              </label>
            </Field>
            <Field label={t('scenes.description')} span>
              <input value={scene.description || ''} onChange={(e) => setScene((s) => ({ ...s, description: e.target.value }))} />
            </Field>
          </div>

          <h3 className="iot-subhead">{t('scenes.actions')}</h3>
          {(scene.actions || []).length === 0 ? <p className="field-hint">{t('scenes.noActions')}</p> : null}
          {(scene.actions || []).map((a, i) => (
            <ActionRow
              key={i}
              action={a}
              devices={devices}
              decls={commandsByDevice[a.deviceId] || []}
              onDevice={(id) => { setAction(i, { deviceId: id, commandName: '', value: 0 }); loadCommands(id); }}
              onCommand={(name) => setAction(i, { commandName: name, value: 0 })}
              onValue={(v) => setAction(i, { value: v })}
              onRemove={() => removeAction(i)}
            />
          ))}
          <div className="action-row">
            <button type="button" className="quiet" onClick={addAction}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('scenes.addAction')}</span>
            </button>
          </div>

          <div className="modal-actions">
            <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
          </div>
        </form>
      </Panel>
    </section>
  );
}

function ActionRow({ action, devices, decls, onDevice, onCommand, onValue, onRemove }) {
  const t = useT();
  const decl = decls.find((d) => d.name === action.commandName);
  return (
    <fieldset className="iot-action-row">
      <Field label={t('scenes.device')}>
        <select value={action.deviceId || ''} onChange={(e) => onDevice(Number(e.target.value))}>
          <option value="">{t('scenes.pickDevice')}</option>
          {devices.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
        </select>
      </Field>
      <Field label={t('scenes.command')}>
        <select value={action.commandName || ''} disabled={!action.deviceId} onChange={(e) => onCommand(e.target.value)}>
          <option value="">{t('scenes.pickCommand')}</option>
          {decls.map((d) => <option key={d.name} value={d.name}>{d.label || d.name}</option>)}
        </select>
      </Field>
      <Field label={t('scenes.value')}>
        <ActionValue decl={decl} value={action.value} onChange={onValue} />
      </Field>
      <button type="button" className="quiet danger-text iot-action-del" onClick={onRemove}>
        <Ico n="trash" sz={14} />
      </button>
    </fieldset>
  );
}

// ActionValue renders the value editor for a chosen command, by its kind — the same vocabulary as
// the live control, but producing a static value to store in a scene or a schedule.
export function ActionValue({ decl, value, onChange }) {
  const t = useT();
  const kind = String(decl?.kind || 'switch').toLowerCase();
  const options = useMemo(() => parseOptions(decl?.options), [decl?.options]);
  if (!decl) return <input disabled value="" />;
  if (kind === 'switch') {
    return (
      <select value={Number(value) === 1 ? '1' : '0'} onChange={(e) => onChange(Number(e.target.value))}>
        <option value="1">{t('scenes.on')}</option>
        <option value="0">{t('scenes.off')}</option>
      </select>
    );
  }
  if (kind === 'mode') {
    return (
      <select value={String(value ?? '')} onChange={(e) => onChange(Number(e.target.value))}>
        {options.map((o) => <option key={o.value} value={o.value}>{o.label || o.value}</option>)}
      </select>
    );
  }
  if (kind === 'color') {
    return <input type="color" value={intToHex(Number(value) || 0)} onChange={(e) => onChange(hexToInt(e.target.value))} />;
  }
  const min = kind === 'dimmer' || kind === 'position' ? 0 : (Number(decl.min) || undefined);
  const max = kind === 'dimmer' || kind === 'position' ? 100 : (Number(decl.max) || undefined);
  return <input type="number" step="any" min={min} max={max} value={value ?? 0} onChange={(e) => onChange(Number(e.target.value))} />;
}

// RunResultModal shows the per-action outcome the server reported — the honest picture of what a
// scene actually did, including any action it refused.
function RunResultModal({ scene, result, onClose }) {
  const t = useT();
  const results = result?.results || [];
  return (
    <Modal title={t('scenes.runResultTitle', { name: scene.name })} onClose={onClose}>
      {results.length === 0 ? (
        <EmptyState text={t('scenes.runNoActions')} />
      ) : (
        <ul className="iot-run-list">
          {results.map((r, i) => (
            <li key={i} className="iot-run-item">
              <span className="iot-run-what">{r.commandName} = {formatNumber(Number(r.value))}</span>
              <CommandStatusPill status={r.status} />
              {r.error ? <span className="iot-run-err">{r.error}</span> : null}
            </li>
          ))}
        </ul>
      )}
      <div className="modal-actions">
        <button type="button" onClick={onClose}>{t('common.close')}</button>
      </div>
    </Modal>
  );
}
