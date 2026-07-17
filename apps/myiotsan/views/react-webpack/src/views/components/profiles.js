import { useCallback, useEffect, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage } from '../lib/helpers';
import { ConfirmModal, EmptyState, Field, FormBusyOverlay, Panel } from './ui';

const DATA_TYPES = ['number', 'bool', 'string'];
const PAYLOAD_FORMATS = ['json', 'raw'];
// How a device of this type is talked to. "" / "mqtt" = the device PUBLISHES to the broker (the
// default). "modbus" = the app POLLS it over Modbus TCP.
const TRANSPORTS = ['mqtt', 'modbus'];
// SunSpec is self-describing (the driver discovers the keys); a register map is a custom vendor
// device where every key names its register explicitly.
const MODBUS_MODES = ['sunspec', 'regmap'];
const REG_KINDS = ['u16', 'i16', 'u32', 'i32', 'f32'];

const EMPTY_KEY = {
  key: '', label: '', unit: '', dataType: 'number', jsonPath: '',
  deadband: 0, heartbeatSeconds: 900, min: 0, max: 0,
  register: 0, regKind: 'u16', scaleFactor: 1, wordSwap: false, regInput: false,
};

const COMMAND_KINDS = ['switch', 'setpoint', 'dimmer', 'position', 'cct', 'mode', 'color'];

// A command a device of this type can be TOLD to do. It starts as a switch with no bounds and no
// confirmation key, and the editor warns about both — because both are how a command ends up
// either refusing every value or never being confirmable.
const EMPTY_COMMAND = {
  name: '', label: '', kind: 'switch', topicTemplate: '', payloadTemplate: '',
  min: 0, max: 0, confirmKey: '',
  register: 0, regKind: 'u16', scaleFactor: 1,
};

const EMPTY_PROFILE = {
  slug: '', name: '', vendor: '', description: '',
  topicTemplate: 'zigbee2mqtt/{deviceKey}', payloadFormat: 'json',
  transport: '', modbusMode: 'regmap', modbusBase: 0, pollSeconds: 5,
};

// ProfilesPage is the device-type catalog: what a class of device reports, where it
// publishes, and how each of its datapoints is decoded and — the field that matters most —
// DEADBANDED. A profile is written once and every device of that type inherits it, which is
// the difference between onboarding a hundred door sensors and onboarding one, a hundred times.
export function ProfilesPage({ onToast }) {
  const t = useT();
  const [profiles, setProfiles] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [openId, setOpenId] = useState(0);
  const [creating, setCreating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    const r = await api('/api/profiles');
    if (r.ok) {
      setProfiles(r.body?.items || []);
      setError('');
    } else {
      setError(errorMessage(r, t('profiles.loadFailed')));
    }
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);

  async function remove(profile) {
    setDeleting(true);
    const r = await api(`/api/profiles/${profile.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) {
      // A builtin refuses to be deleted (400). That message is the explanation — a site
      // cannot break its own onboarding by tidying up the shipped catalog.
      onToast(errorMessage(r, t('profiles.deleteFailed')), 'error');
      return;
    }
    onToast(t('profiles.deleted', { name: profile.name }), 'success');
    if (openId === profile.id) setOpenId(0);
    load();
  }

  if (openId || creating) {
    return (
      <ProfileEditor
        profileId={creating ? 0 : openId}
        onBack={() => { setOpenId(0); setCreating(false); }}
        onSaved={() => { load(); }}
        onToast={onToast}
      />
    );
  }

  const columns = [
    { key: 'name', label: t('profiles.name'), render: (v, row) => (
      <button type="button" className="quiet iot-link-cell" onClick={() => setOpenId(row.id)}>{v}</button>
    ) },
    { key: 'slug', label: t('profiles.slug') },
    { key: 'vendor', label: t('profiles.vendor') },
    { key: 'topicTemplate', label: t('profiles.topic') },
    { key: 'payloadFormat', label: t('profiles.payload') },
    { key: 'builtin', label: t('profiles.builtin'), filterType: 'boolean', render: (v) => (
      v ? <span className="status-pill resolved">{t('profiles.shipped')}</span> : <span className="status-pill">{t('profiles.custom')}</span>
    ) },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => (
      <div className="table-actions">
        <button type="button" className="quiet" onClick={() => setOpenId(row.id)}>{t('profiles.open')}</button>
        <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}>{t('common.delete')}</button>
      </div>
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="box"
        title={t('page.profiles')}
        hint={t('page.profilesHint')}
        actions={(
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" onClick={() => setCreating(true)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('profiles.add')}</span>
            </button>
          </>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        <DataTable
          rows={profiles}
          columns={columns}
          busy={busy}
          pageSize={10}
          pageSizeOptions={[10, 25, 50]}
          emptyText={t('profiles.empty')}
        />
      </Panel>

      {confirmDelete ? (
        <ConfirmModal
          title={t('profiles.deleteTitle')}
          body={t('profiles.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')}
          busy={deleting}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => remove(confirmDelete)}
        />
      ) : null}
    </section>
  );
}

function ProfileEditor({ profileId, onBack, onSaved, onToast }) {
  const t = useT();
  const [profile, setProfile] = useState(EMPTY_PROFILE);
  const [keys, setKeys] = useState([]);
  const [commands, setCommands] = useState([]);
  const [builtin, setBuiltin] = useState(false);
  const [busy, setBusy] = useState(!!profileId);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!profileId) {
      setProfile(EMPTY_PROFILE);
      setKeys([{ ...EMPTY_KEY }]);
      // A new device type declares NO commands. Read-only is the default here too: a type
      // starts out unable to be told to do anything, and somebody has to decide otherwise.
      setCommands([]);
      return;
    }
    let live = true;
    setBusy(true);
    api(`/api/profiles/${profileId}`).then((r) => {
      if (!live) return;
      if (r.ok) {
        setProfile(r.body?.profile || EMPTY_PROFILE);
        setBuiltin(!!r.body?.profile?.builtin);
        setKeys((r.body?.keys || []).map((k) => ({ ...k })));
        setCommands((r.body?.commands || []).map((c) => ({ ...c })));
      } else {
        setError(errorMessage(r, t('profiles.loadFailed')));
      }
      setBusy(false);
    });
    return () => { live = false; };
  }, [profileId, t]);

  const setP = (patch) => setProfile((p) => ({ ...p, ...patch }));
  const setKey = (i, patch) => setKeys((list) => list.map((k, idx) => (idx === i ? { ...k, ...patch } : k)));
  const addKey = () => setKeys((list) => [...list, { ...EMPTY_KEY }]);
  const removeKey = (i) => setKeys((list) => list.filter((_, idx) => idx !== i));
  const setCmd = (i, patch) => setCommands((list) => list.map((c, idx) => (idx === i ? { ...c, ...patch } : c)));
  const addCmd = () => setCommands((list) => [...list, { ...EMPTY_COMMAND }]);
  const removeCmd = (i) => setCommands((list) => list.filter((_, idx) => idx !== i));

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const isModbus = profile.transport === 'modbus';
    const payload = {
      slug: profile.slug,
      name: profile.name,
      vendor: profile.vendor || '',
      description: profile.description || '',
      topicTemplate: profile.topicTemplate || '',
      payloadFormat: profile.payloadFormat || 'json',
      transport: profile.transport || '',
      modbusMode: isModbus ? (profile.modbusMode || 'regmap') : '',
      modbusBase: Number(profile.modbusBase) || 0,
      pollSeconds: Number(profile.pollSeconds) || 0,
      keys: keys.map((k) => ({
        key: k.key,
        label: k.label || '',
        unit: k.unit || '',
        dataType: k.dataType || 'number',
        jsonPath: k.jsonPath || '',
        deadband: Number(k.deadband) || 0,
        heartbeatSeconds: Number(k.heartbeatSeconds) || 0,
        min: Number(k.min) || 0,
        max: Number(k.max) || 0,
        register: Number(k.register) || 0,
        regKind: k.regKind || '',
        scaleFactor: Number(k.scaleFactor) || 0,
        wordSwap: !!k.wordSwap,
        regInput: !!k.regInput,
      })),
      // The commands, replaced wholesale like the keys. min/max ride along for every kind; the
      // server only enforces them on a setpoint. A Modbus profile's commands are register WRITES —
      // they inherit the profile's transport so the command path routes them to the driver.
      commands: commands.map((c) => ({
        name: c.name,
        label: c.label || '',
        kind: c.kind || 'switch',
        topicTemplate: c.topicTemplate || '',
        payloadTemplate: c.payloadTemplate || '',
        min: Number(c.min) || 0,
        max: Number(c.max) || 0,
        confirmKey: c.confirmKey || '',
        transport: isModbus ? 'modbus' : '',
        register: Number(c.register) || 0,
        regKind: c.regKind || '',
        scaleFactor: Number(c.scaleFactor) || 0,
      })),
    };
    const r = profileId
      ? await api(`/api/profiles/${profileId}`, { method: 'PUT', body: JSON.stringify(payload) })
      : await api('/api/profiles', { method: 'POST', body: JSON.stringify(payload) });
    setBusy(false);
    if (!r.ok) {
      setError(errorMessage(r, t('profiles.saveFailed')));
      return;
    }
    onToast(profileId ? t('profiles.updated') : t('profiles.created'), 'success');
    onSaved();
    onBack();
  }

  // A Modbus profile edits register bindings instead of MQTT topics/JSON paths; a register-map
  // (regmap) profile also binds each key to a register, while SunSpec discovers them.
  const isModbus = profile.transport === 'modbus';
  const isRegmap = isModbus && (profile.modbusMode || 'regmap') === 'regmap';

  return (
    <section className="workspace">
      <Panel
        icon="box"
        title={profileId ? (profile.name || t('profiles.edit')) : t('profiles.addTitle')}
        hint={t('profiles.editHint')}
        actions={(
          <button type="button" className="quiet" onClick={onBack}>
            <span className="btn-icon"><Ico n="arr-left" sz={14} /> {t('profiles.backToList')}</span>
          </button>
        )}
      >
        <form className="device-edit-form" onSubmit={save}>
          <FormBusyOverlay busy={busy} />
          {error ? <div className="status-line">{error}</div> : null}
          {builtin ? <p className="settings-hint">{t('profiles.builtinNote')}</p> : null}

          <div className="settings-field-grid">
            <Field label={t('profiles.name')} required>
              <input value={profile.name} onChange={(e) => setP({ name: e.target.value })} />
            </Field>
            <Field label={t('profiles.slug')} hint={t('profiles.slugHint')} required>
              <input value={profile.slug} onChange={(e) => setP({ slug: e.target.value })} />
            </Field>
            <Field label={t('profiles.vendor')}>
              <input value={profile.vendor || ''} onChange={(e) => setP({ vendor: e.target.value })} />
            </Field>
            <Field label={t('profiles.transport')} hint={t('profiles.transportHint')}>
              <select value={profile.transport || 'mqtt'} onChange={(e) => setP({ transport: e.target.value === 'mqtt' ? '' : e.target.value })}>
                {TRANSPORTS.map((tp) => <option key={tp} value={tp}>{t(`transport.${tp}`)}</option>)}
              </select>
            </Field>

            {isModbus ? (
              <>
                <Field label={t('profiles.modbusMode')} hint={t('profiles.modbusModeHint')}>
                  <select value={profile.modbusMode || 'regmap'} onChange={(e) => setP({ modbusMode: e.target.value })}>
                    {MODBUS_MODES.map((m) => <option key={m} value={m}>{t(`modbusMode.${m}`)}</option>)}
                  </select>
                </Field>
                {(profile.modbusMode || 'regmap') === 'sunspec' ? (
                  <Field label={t('profiles.modbusBase')} hint={t('profiles.modbusBaseHint')}>
                    <input type="number" value={profile.modbusBase ?? 0} onChange={(e) => setP({ modbusBase: e.target.value })} />
                  </Field>
                ) : null}
                <Field label={t('profiles.pollSeconds')} hint={t('profiles.pollSecondsHint')}>
                  <input type="number" min="1" value={profile.pollSeconds ?? 5} onChange={(e) => setP({ pollSeconds: e.target.value })} />
                </Field>
              </>
            ) : (
              <>
                <Field label={t('profiles.payload')} hint={t('profiles.payloadHint')}>
                  <select value={profile.payloadFormat || 'json'} onChange={(e) => setP({ payloadFormat: e.target.value })}>
                    {PAYLOAD_FORMATS.map((f) => <option key={f} value={f}>{t(`payload.${f}`)}</option>)}
                  </select>
                </Field>
                <Field label={t('profiles.topic')} hint={t('profiles.topicHint')} span>
                  <input value={profile.topicTemplate || ''} onChange={(e) => setP({ topicTemplate: e.target.value })} />
                </Field>
              </>
            )}
            <Field label={t('common.description')} span>
              <input value={profile.description || ''} onChange={(e) => setP({ description: e.target.value })} />
            </Field>
          </div>
          {isModbus && !isRegmap ? <p className="settings-hint">{t('profiles.sunspecNote')}</p> : null}

          <div className="toolbar">
            <div>
              <h3 className="section-subtitle">{t('profiles.keysTitle')}</h3>
              <p className="settings-hint">{t('profiles.keysHint')}</p>
            </div>
            <button type="button" className="quiet" onClick={addKey}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('profiles.addKey')}</span>
            </button>
          </div>

          {keys.length === 0 ? (
            <EmptyState text={t('profiles.noKeys')} />
          ) : keys.map((k, i) => (
            <fieldset className="iot-fieldset" key={k.id || `new-${i}`}>
              <legend>{k.label || k.key || t('profiles.newKey')}</legend>
              <div className="settings-field-grid">
                <Field label={t('profiles.key')} hint={t('profiles.keyHint')} required>
                  <input value={k.key} onChange={(e) => setKey(i, { key: e.target.value })} />
                </Field>
                <Field label={t('profiles.label')}>
                  <input value={k.label || ''} onChange={(e) => setKey(i, { label: e.target.value })} />
                </Field>
                <Field label={t('profiles.unit')}>
                  <input value={k.unit || ''} onChange={(e) => setKey(i, { unit: e.target.value })} />
                </Field>
                <Field label={t('profiles.dataType')} hint={t('profiles.dataTypeHint')}>
                  <select value={k.dataType || 'number'} onChange={(e) => setKey(i, { dataType: e.target.value })}>
                    {DATA_TYPES.map((d) => <option key={d} value={d}>{t(`dataType.${d}`)}</option>)}
                  </select>
                </Field>
                {!isModbus ? (
                  <Field label={t('profiles.jsonPath')} hint={t('profiles.jsonPathHint')}>
                    <input value={k.jsonPath || ''} onChange={(e) => setKey(i, { jsonPath: e.target.value })} />
                  </Field>
                ) : isRegmap ? (
                  <>
                    <Field label={t('profiles.register')} hint={t('profiles.registerHint')}>
                      <input type="number" value={k.register ?? 0} onChange={(e) => setKey(i, { register: e.target.value })} />
                    </Field>
                    <Field label={t('profiles.regKind')} hint={t('profiles.regKindHint')}>
                      <select value={k.regKind || 'u16'} onChange={(e) => setKey(i, { regKind: e.target.value })}>
                        {REG_KINDS.map((rk) => <option key={rk} value={rk}>{rk}</option>)}
                      </select>
                    </Field>
                    <Field label={t('profiles.scaleFactor')} hint={t('profiles.scaleFactorHint')}>
                      <input type="number" step="any" value={k.scaleFactor ?? 1} onChange={(e) => setKey(i, { scaleFactor: e.target.value })} />
                    </Field>
                    <Field label={t('profiles.wordSwap')} hint={t('profiles.wordSwapHint')}>
                      <label className="check-row"><input type="checkbox" checked={!!k.wordSwap} onChange={(e) => setKey(i, { wordSwap: e.target.checked })} /> {t('profiles.wordSwapLabel')}</label>
                    </Field>
                    <Field label={t('profiles.regInput')} hint={t('profiles.regInputHint')}>
                      <label className="check-row"><input type="checkbox" checked={!!k.regInput} onChange={(e) => setKey(i, { regInput: e.target.checked })} /> {t('profiles.regInputLabel')}</label>
                    </Field>
                  </>
                ) : null}

                {/* The deadband is the whole storage design, so it is explained where it is
                    set — not in a manual nobody will read. */}
                <Field label={t('profiles.deadband')} hint={t('profiles.deadbandHint')}>
                  <input
                    type="number"
                    step="any"
                    min="0"
                    value={k.deadband ?? 0}
                    onChange={(e) => setKey(i, { deadband: e.target.value })}
                  />
                </Field>
                <Field label={t('profiles.heartbeat')} hint={t('profiles.heartbeatHint')}>
                  <input
                    type="number"
                    min="0"
                    value={k.heartbeatSeconds ?? 0}
                    onChange={(e) => setKey(i, { heartbeatSeconds: e.target.value })}
                  />
                </Field>
                <Field label={t('profiles.min')} hint={t('profiles.rangeHint')}>
                  <input type="number" step="any" value={k.min ?? 0} onChange={(e) => setKey(i, { min: e.target.value })} />
                </Field>
                <Field label={t('profiles.max')}>
                  <input type="number" step="any" value={k.max ?? 0} onChange={(e) => setKey(i, { max: e.target.value })} />
                </Field>
              </div>
              <div className="action-row iot-actions-end">
                <button type="button" className="quiet danger-text" onClick={() => removeKey(i)}>
                  <span className="btn-icon"><Ico n="trash" sz={14} /> {t('profiles.removeKey')}</span>
                </button>
              </div>
            </fieldset>
          ))}

          {k0Warning(keys) ? <p className="field-hint">{t('profiles.zeroDeadbandNote')}</p> : null}

          {/* --- Commands: what a device of this type can be TOLD to do ---------------------
              This is the other half of a profile, and the dangerous half. A device can be sent
              exactly what is declared here and nothing else — there is no generic "publish this
              payload to that topic" escape hatch, which would be a remote shell for the
              building's electrics. Everything declared here is a physical action somewhere. */}
          <div className="toolbar">
            <div>
              <h3 className="section-subtitle">{t('profiles.commandsTitle')}</h3>
              <p className="settings-hint">{t('profiles.commandsHint')}</p>
            </div>
            <button type="button" className="quiet" onClick={addCmd}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('profiles.addCommand')}</span>
            </button>
          </div>

          {commands.length === 0 ? (
            <EmptyState text={t('profiles.noCommands')} />
          ) : commands.map((c, i) => {
            const kind = (c.kind || 'switch').toLowerCase();
            // Both min and max at zero is not "unbounded" — the server treats it as an omission
            // and refuses EVERY value. Say so here, where it can still be fixed, rather than
            // letting an admin ship a command that mysteriously never works.
            const noRange = (kind === 'setpoint' || kind === 'cct') && Number(c.min || 0) === 0 && Number(c.max || 0) === 0;
            return (
              <fieldset className="iot-fieldset" key={c.id || `new-cmd-${i}`}>
                <legend>{c.label || c.name || t('profiles.newCommand')}</legend>
                <div className="settings-field-grid">
                  <Field label={t('profiles.cmdName')} hint={t('profiles.cmdNameHint')} required>
                    <input value={c.name} onChange={(e) => setCmd(i, { name: e.target.value })} />
                  </Field>
                  <Field label={t('profiles.cmdLabel')}>
                    <input value={c.label || ''} onChange={(e) => setCmd(i, { label: e.target.value })} />
                  </Field>
                  <Field label={t('profiles.cmdKind')} hint={t('profiles.cmdKindHint')}>
                    <select value={kind} onChange={(e) => setCmd(i, { kind: e.target.value })}>
                      {COMMAND_KINDS.map((k) => <option key={k} value={k}>{t(`cmdKind.${k}`)}</option>)}
                    </select>
                  </Field>
                  {!isModbus ? (
                    <>
                      <Field label={t('profiles.cmdConfirmKey')} hint={t('profiles.cmdConfirmKeyHint')}>
                        <input value={c.confirmKey || ''} onChange={(e) => setCmd(i, { confirmKey: e.target.value })} />
                      </Field>
                      <Field label={t('profiles.cmdTopic')} hint={t('profiles.cmdTopicHint')} span>
                        <input value={c.topicTemplate || ''} onChange={(e) => setCmd(i, { topicTemplate: e.target.value })} />
                      </Field>
                      <Field label={t('profiles.cmdPayload')} hint={t('profiles.cmdPayloadHint')} span>
                        <input value={c.payloadTemplate || ''} onChange={(e) => setCmd(i, { payloadTemplate: e.target.value })} />
                      </Field>
                    </>
                  ) : (
                    <>
                      <Field label={t('profiles.register')} hint={t('profiles.cmdRegisterHint')}>
                        <input type="number" value={c.register ?? 0} onChange={(e) => setCmd(i, { register: e.target.value })} />
                      </Field>
                      <Field label={t('profiles.regKind')} hint={t('profiles.cmdRegKindHint')}>
                        <select value={c.regKind || 'u16'} onChange={(e) => setCmd(i, { regKind: e.target.value })}>
                          {['u16', 'i16'].map((rk) => <option key={rk} value={rk}>{rk}</option>)}
                        </select>
                      </Field>
                      <Field label={t('profiles.scaleFactor')} hint={t('profiles.scaleFactorHint')}>
                        <input type="number" step="any" value={c.scaleFactor ?? 1} onChange={(e) => setCmd(i, { scaleFactor: e.target.value })} />
                      </Field>
                    </>
                  )}
                  {(kind === 'setpoint' || kind === 'cct') ? (
                    <>
                      <Field label={t('profiles.cmdMin')} hint={t('profiles.cmdRangeHint')}>
                        <input type="number" step="any" value={c.min ?? 0} onChange={(e) => setCmd(i, { min: e.target.value })} />
                      </Field>
                      <Field label={t('profiles.cmdMax')}>
                        <input type="number" step="any" value={c.max ?? 0} onChange={(e) => setCmd(i, { max: e.target.value })} />
                      </Field>
                    </>
                  ) : null}
                  {kind === 'mode' ? (
                    <Field label={t('profiles.cmdOptions')} hint={t('profiles.cmdOptionsHint')} span>
                      <input value={c.options || ''} onChange={(e) => setCmd(i, { options: e.target.value })}
                        placeholder='[{"value":0,"label":"Off"},{"value":2,"label":"Cool"}]' />
                    </Field>
                  ) : null}
                </div>

                {noRange ? (
                  <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('profiles.cmdNoRangeWarn')}</p>
                ) : null}
                {/* Without a confirmation key an MQTT command can only ever be "sent" — and "sent"
                    is not "it happened". A Modbus command needs no confirm key: WriteConfirm reads
                    the register back, so it confirms inline. */}
                {!isModbus && !c.confirmKey ? (
                  <p className="iot-cmd-warn"><Ico n="warning" sz={13} /> {t('profiles.cmdNoConfirmWarn')}</p>
                ) : null}

                <div className="action-row iot-actions-end">
                  <button type="button" className="quiet danger-text" onClick={() => removeCmd(i)}>
                    <span className="btn-icon"><Ico n="trash" sz={14} /> {t('profiles.removeCommand')}</span>
                  </button>
                </div>
              </fieldset>
            );
          })}

          <div className="action-row iot-actions-end">
            <button type="button" className="quiet" onClick={onBack} disabled={busy}>{t('common.cancel')}</button>
            <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
          </div>
        </form>
      </Panel>
    </section>
  );
}

// k0Warning is true when a numeric key stores every single sample. That is correct for a
// door contact (every transition matters) and expensive for a temperature probe, so the
// editor says so rather than letting a site discover it as a full disk.
function k0Warning(keys) {
  return keys.some((k) => (k.dataType || 'number') === 'number' && Number(k.deadband || 0) === 0);
}
