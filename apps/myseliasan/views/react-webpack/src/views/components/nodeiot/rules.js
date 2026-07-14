import { useCallback, useEffect, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { accessError, api, forbidden, formatAgo, formatNumber } from './lib/helpers';
import { AccessDenied, CheckRow, ConfirmModal, Field, FormBusyOverlay, Modal, Panel, SeverityPill } from './ui';

export const CONDITIONS = ['above', 'below', 'equals', 'delta', 'rate', 'stuck', 'offline'];
const SEVERITIES = ['info', 'warning', 'critical'];
const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6];

// COND_FIELDS is what each condition actually USES. A rule editor that shows every field
// for every condition teaches an operator nothing and invites them to set a hysteresis on
// an offline rule, where it means nothing. Only the fields that matter are shown.
//
//   window     delta/rate/stuck/offline are all defined over a lookback — without one they
//              cannot be evaluated at all, and the API rejects them (windowSeconds > 0).
//   hysteresis only above/below have a threshold to fall back through, so only they can flap.
//   key        every condition watches one datapoint, except offline, which watches the
//              device itself.
const COND_FIELDS = {
  above: { key: true, threshold: true, hysteresis: true, window: false },
  below: { key: true, threshold: true, hysteresis: true, window: false },
  equals: { key: true, threshold: true, hysteresis: false, window: false },
  delta: { key: true, threshold: true, hysteresis: false, window: true },
  rate: { key: true, threshold: true, hysteresis: false, window: true },
  stuck: { key: true, threshold: false, hysteresis: false, window: true },
  offline: { key: false, threshold: false, hysteresis: false, window: true },
};

const EMPTY_RULE = {
  name: '',
  enabled: true,
  deviceId: 0,
  tag: '',
  key: '',
  condition: 'above',
  threshold: 0,
  hysteresis: 0,
  consecutiveSamples: 3,
  windowSeconds: 300,
  cooldownSeconds: 900,
  schedulePolicy: 'always',
  scheduleStart: '',
  scheduleEnd: '',
  scheduleDays: '',
  severity: 'warning',
};

// The node's own alert rules, edited over the tunnel. (Not to be confused with myseliasan's
// FLEET rules, which span nodes; these live on this hub and fire on this hub's telemetry.)
// Editing them is admin-at-the-node — the node says so, and this page relays that verbatim.
export function RulesPage({ onToast }) {
  const t = useT();
  const [rules, setRules] = useState([]);
  const [devices, setDevices] = useState([]);
  const [keys, setKeys] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [denied, setDenied] = useState(false);
  const [editing, setEditing] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    const [rr, dr, pr] = await Promise.all([
      api('/api/rules'),
      api('/api/devices?limit=1000'),
      api('/api/profiles'),
    ]);
    if (rr.ok) {
      setRules(rr.body?.items || []);
      setError('');
      setDenied(false);
    } else if (forbidden(rr)) {
      setDenied(true);
      setError('');
    } else {
      setDenied(false);
      setError(accessError(rr, t, t('rules.loadFailed')));
    }
    if (dr.ok) setDevices(dr.body?.items || []);

    // The telemetry keys an operator can watch are whatever the profiles declare. Pulling
    // them here turns the key field from free text (typo = a rule that silently never
    // fires) into a list of what the estate actually reports.
    if (pr.ok) {
      const profiles = pr.body?.items || [];
      const details = await Promise.all(profiles.map((p) => api(`/api/profiles/${p.id}`)));
      const seen = new Map();
      details.forEach((d) => {
        if (!d.ok) return;
        (d.body?.keys || []).forEach((k) => {
          if (!seen.has(k.key)) seen.set(k.key, { key: k.key, label: k.label || k.key, unit: k.unit || '' });
        });
      });
      setKeys([...seen.values()].sort((a, b) => a.key.localeCompare(b.key)));
    }
    setBusy(false);
  }, [t]);

  useEffect(() => { load(); }, [load]);

  const scopeText = useCallback((rule) => {
    if (rule.deviceId) {
      const d = devices.find((x) => x.id === rule.deviceId);
      return d ? d.name : t('rules.scopeDeviceGone', { id: rule.deviceId });
    }
    if (rule.tag) return t('rules.scopeTag', { tag: rule.tag });
    return t('rules.scopeAny');
  }, [devices, t]);

  async function save(form, id) {
    const payload = {
      name: form.name,
      enabled: !!form.enabled,
      deviceId: Number(form.deviceId) || 0,
      tag: form.tag || '',
      key: form.key || '',
      condition: form.condition,
      threshold: Number(form.threshold) || 0,
      hysteresis: Number(form.hysteresis) || 0,
      consecutiveSamples: Number(form.consecutiveSamples) || 0,
      windowSeconds: Number(form.windowSeconds) || 0,
      cooldownSeconds: Number(form.cooldownSeconds) || 0,
      schedulePolicy: form.schedulePolicy || 'always',
      scheduleStart: form.scheduleStart || '',
      scheduleEnd: form.scheduleEnd || '',
      scheduleDays: form.scheduleDays || '',
      severity: form.severity || 'info',
    };
    const r = id
      ? await api(`/api/rules/${id}`, { method: 'PUT', body: JSON.stringify(payload) })
      : await api('/api/rules', { method: 'POST', body: JSON.stringify(payload) });
    // The rule engine owns the rules of a rule (a window is mandatory for delta/rate/
    // stuck/offline; every condition but offline needs a key). Its message is the one worth
    // reading, so it is shown verbatim rather than second-guessed in the form.
    if (!r.ok) return accessError(r, t, t('rules.saveFailed'));
    onToast(id ? t('rules.updated') : t('rules.created'), 'success');
    setEditing(null);
    load();
    return '';
  }

  async function remove(rule) {
    setDeleting(true);
    const r = await api(`/api/rules/${rule.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) {
      onToast(accessError(r, t, t('rules.deleteFailed')), 'error');
      return;
    }
    onToast(t('rules.deleted', { name: rule.name }), 'success');
    load();
  }

  const columns = [
    { key: 'name', label: t('rules.name') },
    { key: 'enabled', label: t('common.status'), filterType: 'boolean', render: (v) => (
      <span className={`status-pill ${v ? 'online' : ''}`}>{v ? t('common.enabled') : t('common.disabled')}</span>
    ) },
    { key: 'condition', label: t('rules.condition'), render: (v, row) => (
      <span title={t(`cond.${v}.desc`)}>{summarize(row, t)}</span>
    ) },
    { key: 'deviceId', label: t('rules.scope'), filterType: 'number', render: (_v, row) => scopeText(row) },
    { key: 'severity', label: t('rules.severity'), render: (v) => <SeverityPill severity={v} /> },
    { key: 'lastTriggeredAt', label: t('rules.lastFired'), filterType: 'number', render: (v) => (v ? formatAgo(v, t) : t('time.never')) },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => (
      <div className="table-actions">
        <button type="button" className="quiet" onClick={() => setEditing(row)}>{t('common.edit')}</button>
        <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}>{t('common.delete')}</button>
      </div>
    ) },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="wand"
        title={t('page.rules')}
        hint={t('page.rulesHint')}
        actions={denied ? null : (
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" onClick={() => setEditing(EMPTY_RULE)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('rules.add')}</span>
            </button>
          </>
        )}
      >
        {denied ? <AccessDenied /> : (
          <>
            {error ? <div className="status-line">{error}</div> : null}
            <DataTable
              rows={rules}
              columns={columns}
              busy={busy}
              pageSize={10}
              pageSizeOptions={[10, 25, 50]}
              emptyText={t('rules.empty')}
            />
          </>
        )}
      </Panel>

      {editing ? (
        <RuleEditor
          rule={editing}
          devices={devices}
          keys={keys}
          onCancel={() => setEditing(null)}
          onSave={(form) => save(form, editing.id)}
        />
      ) : null}

      {confirmDelete ? (
        <ConfirmModal
          title={t('rules.deleteTitle')}
          body={t('rules.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')}
          busy={deleting}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => remove(confirmDelete)}
        />
      ) : null}
    </section>
  );
}

// summarize renders a rule's condition as the sentence it means, so the list reads like
// what it does ("temperature above 5") instead of a row of columns to reassemble.
function summarize(rule, t) {
  const key = rule.key || t('rules.theDevice');
  switch (rule.condition) {
    case 'above': return t('cond.above.sum', { key, n: formatNumber(rule.threshold) });
    case 'below': return t('cond.below.sum', { key, n: formatNumber(rule.threshold) });
    case 'equals': return t('cond.equals.sum', { key, n: formatNumber(rule.threshold) });
    case 'delta': return t('cond.delta.sum', { key, n: formatNumber(rule.threshold), w: rule.windowSeconds });
    case 'rate': return t('cond.rate.sum', { key, n: formatNumber(rule.threshold) });
    case 'stuck': return t('cond.stuck.sum', { key, w: rule.windowSeconds });
    case 'offline': return t('cond.offline.sum', { w: rule.windowSeconds });
    default: return rule.condition;
  }
}

function RuleEditor({ rule, devices, keys, onCancel, onSave }) {
  const t = useT();
  const [form, setForm] = useState({ ...EMPTY_RULE, ...rule });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const fields = COND_FIELDS[form.condition] || COND_FIELDS.above;
  const scope = form.deviceId ? 'device' : form.tag ? 'tag' : 'any';

  function setScope(next) {
    if (next === 'device') set({ tag: '' });
    else if (next === 'tag') set({ deviceId: 0 });
    else set({ deviceId: 0, tag: '' });
  }

  function toggleDay(day) {
    const current = new Set((form.scheduleDays || '').split(',').map((s) => s.trim()).filter(Boolean));
    const d = String(day);
    if (current.has(d)) current.delete(d); else current.add(d);
    set({ scheduleDays: [...current].sort().join(',') });
  }
  const dayOn = (day) => (form.scheduleDays || '').split(',').includes(String(day));

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    const message = await onSave(form);
    setBusy(false);
    setError(message || '');
  }

  return (
    <Modal title={rule.id ? t('rules.editTitle') : t('rules.addTitle')} onClose={busy ? undefined : onCancel} wide>
      <form className="device-edit-form" onSubmit={submit}>
        <FormBusyOverlay busy={busy} />
        {error ? <div className="status-line">{error}</div> : null}

        <div className="settings-field-grid">
          <Field label={t('rules.name')} required>
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} autoFocus />
          </Field>
          <Field label={t('rules.severity')} hint={t('rules.severityHint')}>
            <select value={form.severity} onChange={(e) => set({ severity: e.target.value })}>
              {SEVERITIES.map((s) => <option key={s} value={s}>{t(`severity.${s}`)}</option>)}
            </select>
          </Field>
        </div>

        {/* --- Scope: which devices this rule watches -------------------------------- */}
        <fieldset className="iot-fieldset">
          <legend>{t('rules.scope')}</legend>
          <p className="settings-hint">{t('rules.scopeHint')}</p>
          <div className="segmented">
            {['any', 'device', 'tag'].map((s) => (
              <button
                key={s}
                type="button"
                className={scope === s ? 'active' : 'quiet'}
                onClick={() => setScope(s)}
              >
                {t(`rules.scope.${s}`)}
              </button>
            ))}
          </div>
          {scope === 'device' ? (
            <Field label={t('rules.device')}>
              <select value={form.deviceId} onChange={(e) => set({ deviceId: Number(e.target.value) })}>
                <option value={0}>{t('rules.pickDevice')}</option>
                {devices.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </Field>
          ) : null}
          {scope === 'tag' ? (
            <Field label={t('rules.tag')} hint={t('rules.tagHint')}>
              <input value={form.tag} onChange={(e) => set({ tag: e.target.value })} list="nodeiot-rule-tags" />
            </Field>
          ) : null}
          <datalist id="nodeiot-rule-tags">
            {[...new Set(devices.map((d) => d.tag).filter(Boolean))].map((tag) => <option key={tag} value={tag} />)}
          </datalist>
        </fieldset>

        {/* --- Condition: what makes it fire ----------------------------------------- */}
        <fieldset className="iot-fieldset">
          <legend>{t('rules.condition')}</legend>
          <div className="settings-field-grid">
            <Field label={t('rules.condition')}>
              <select value={form.condition} onChange={(e) => set({ condition: e.target.value })}>
                {CONDITIONS.map((c) => <option key={c} value={c}>{t(`cond.${c}`)}</option>)}
              </select>
            </Field>
            {fields.key ? (
              <Field label={t('rules.key')} hint={t('rules.keyHint')} required>
                <input value={form.key} onChange={(e) => set({ key: e.target.value })} list="nodeiot-rule-keys" />
              </Field>
            ) : null}
          </div>
          <datalist id="nodeiot-rule-keys">
            {keys.map((k) => <option key={k.key} value={k.key}>{k.unit ? `${k.label} (${k.unit})` : k.label}</option>)}
          </datalist>

          {/* The condition explains itself, in the words an operator would use. */}
          <p className="iot-cond-explain">
            <Ico n="info" sz={14} /> {t(`cond.${form.condition}.desc`)}
          </p>

          <div className="settings-field-grid">
            {fields.threshold ? (
              <Field
                label={form.condition === 'rate' ? t('rules.thresholdRate') : t('rules.threshold')}
                hint={t(`cond.${form.condition}.thresholdHint`)}
                required
              >
                <input
                  type="number"
                  step="any"
                  value={form.threshold}
                  onChange={(e) => set({ threshold: e.target.value })}
                />
              </Field>
            ) : null}
            {fields.hysteresis ? (
              <Field label={t('rules.hysteresis')} hint={t('rules.hysteresisHint')}>
                <input
                  type="number"
                  step="any"
                  value={form.hysteresis}
                  onChange={(e) => set({ hysteresis: e.target.value })}
                />
              </Field>
            ) : null}
            {fields.window ? (
              <Field label={t('rules.window')} hint={t(`cond.${form.condition}.windowHint`)} required>
                {/* Deliberately no `min` constraint. A native min would make the BROWSER
                    refuse the submit with its own bubble, and the rule engine's message
                    ("delta/rate/stuck/offline need a window") is the one worth reading —
                    so the value goes to the node and the node's answer is shown. */}
                <input
                  type="number"
                  value={form.windowSeconds}
                  onChange={(e) => set({ windowSeconds: e.target.value })}
                />
              </Field>
            ) : null}
          </div>
        </fieldset>

        {/* --- Noise control: the difference between an alarm and a nuisance --------- */}
        <fieldset className="iot-fieldset">
          <legend>{t('rules.noise')}</legend>
          <div className="settings-field-grid">
            <Field label={t('rules.debounce')} hint={t('rules.debounceHint')}>
              <input
                type="number"
                min="0"
                value={form.consecutiveSamples}
                onChange={(e) => set({ consecutiveSamples: e.target.value })}
              />
            </Field>
            <Field label={t('rules.cooldown')} hint={t('rules.cooldownHint')}>
              <input
                type="number"
                min="0"
                value={form.cooldownSeconds}
                onChange={(e) => set({ cooldownSeconds: e.target.value })}
              />
            </Field>
          </div>
        </fieldset>

        {/* --- Schedule: when the rule is live -------------------------------------- */}
        <fieldset className="iot-fieldset">
          <legend>{t('rules.schedule')}</legend>
          <p className="settings-hint">{t('rules.scheduleHint')}</p>
          <div className="segmented">
            {['always', 'window'].map((p) => (
              <button
                key={p}
                type="button"
                className={form.schedulePolicy === p ? 'active' : 'quiet'}
                onClick={() => set({ schedulePolicy: p })}
              >
                {t(`rules.policy.${p}`)}
              </button>
            ))}
          </div>
          {form.schedulePolicy === 'window' ? (
            <>
              <div className="settings-field-grid">
                <Field label={t('rules.from')}>
                  <input type="time" value={form.scheduleStart} onChange={(e) => set({ scheduleStart: e.target.value })} />
                </Field>
                <Field label={t('rules.to')}>
                  <input type="time" value={form.scheduleEnd} onChange={(e) => set({ scheduleEnd: e.target.value })} />
                </Field>
              </div>
              <div className="schedule-days">
                {WEEKDAYS.map((d) => (
                  <button
                    key={d}
                    type="button"
                    className={dayOn(d) ? 'active' : 'quiet'}
                    onClick={() => toggleDay(d)}
                  >
                    {t(`day.${d}`)}
                  </button>
                ))}
              </div>
              <p className="field-hint">{t('rules.daysHint')}</p>
            </>
          ) : null}
        </fieldset>

        <CheckRow checked={form.enabled} onChange={(v) => set({ enabled: v })} label={t('rules.enabledLabel')} />

        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('common.cancel')}</button>
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('common.save')}</button>
        </div>
      </form>
    </Modal>
  );
}
