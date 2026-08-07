import { useCallback, useEffect, useMemo, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { api, formatTimestamp } from '../lib/helpers';

// FleetRulesPage is the control plane's cross-domain correlation surface — the one thing no
// single node can do for itself. A camera node cannot see your door contacts; a sensor hub
// cannot see your cameras. Only myseliasan, which already receives every node's events in one
// feed, is in a position to notice the CONJUNCTION:
//
//   motion on a camera node AND a door contact on a sensor node AND no badge swipe -> intrusion
//
// The page is deliberately wordy at the top (the pitch) and around the grace delay (the field
// that decides whether this feature is useful or a nuisance) — see the copy in i18n.js.
//
// Reading rules follows the permission matrix; WRITING them is superadmin-only and the server
// enforces it (403). The controls below are hidden for everyone else, which is UX, not security.

const SEVERITIES = ['info', 'warning', 'critical'];
const KINDS = ['', 'camera', 'iot', 'door'];

// Every fleet-rules call passes noRedirect: a 403 here means "not a superadmin" or "this role
// cannot read rules" — both are things to say in place, not a reason to bounce to the SSO login.
const call = (path, options = {}) => api(path, { noRedirect: true, ...options }).catch(() => ({ ok: false }));

// Toggle is the same sliding on/off switch the node dashboard uses (.switch in node-dashboard.css).
function Toggle({ checked, disabled, onChange, label, ariaLabel }) {
  return (
    <label className="switch-row">
      <span className="switch">
        <input type="checkbox" checked={checked} disabled={disabled} onChange={onChange} aria-label={ariaLabel} />
        <span className="switch-slider" />
      </span>
      {label ? <span className="switch-label">{label}</span> : null}
    </label>
  );
}

const blankClause = (mode) => ({ mode, nodeId: '', kind: '', category: '', match: '' });

// normKind collapses a node's stored kind to one of the clause kinds. Empty/unknown = camera:
// every node adopted before the field existed is a mymatasan NVR.
const normKind = (kind) => {
  const k = String(kind || 'camera').toLowerCase();
  return k === 'iot' || k === 'door' ? k : 'camera';
};

// A new rule starts enabled, critical, with a 120s window and a 5s grace — the grace default
// mirrors the server's (graceOf falls back to 5s), so a rule left alone still tolerates a slow
// badge reader instead of crying intrusion on every legitimate entry.
const blankRule = () => ({
  id: 0,
  name: '',
  enabled: true,
  windowSeconds: 120,
  graceSeconds: 5,
  cooldownSeconds: 300,
  severity: 'critical',
  clauses: [blankClause('required'), blankClause('absent')],
});

// toRequest flattens a {rule, clauses} detail from the API into the SaveFleetRuleRequest shape.
const toRequest = (detail) => ({
  id: detail.rule?.id || 0,
  name: detail.rule?.name || '',
  enabled: !!detail.rule?.enabled,
  windowSeconds: Number(detail.rule?.windowSeconds || 0),
  graceSeconds: Number(detail.rule?.graceSeconds || 0),
  cooldownSeconds: Number(detail.rule?.cooldownSeconds || 0),
  severity: detail.rule?.severity || 'critical',
  clauses: (detail.clauses || []).map((c) => ({
    mode: c.mode || 'required',
    nodeId: c.nodeId || '',
    kind: c.kind || '',
    category: c.category || '',
    match: c.match || '',
  })),
});

// kindLabel names a node's type in the operator's language. An empty kind is a camera node:
// every node adopted before the field existed is a mymatasan NVR.
function useKindLabel() {
  const t = useT();
  return useCallback((kind) => {
    switch (String(kind || '').toLowerCase()) {
      case 'iot': return t('fr.kindSensor');
      case 'door': return t('fr.kindDoor');
      default: return t('fr.kindCamera');
    }
  }, [t]);
}

// ClausePhrase renders one condition as plain English: what to look for, and where.
// “door opened” on a sensor node   /   “badge” on Front Lobby   /   any event on any node
function ClausePhrase({ clause, nodeName }) {
  const t = useT();
  const what = clause.match ? `“${clause.match}”` : (clause.category ? clause.category : t('fr.anyEvent'));
  const kind = String(clause.kind || '').toLowerCase();
  const where = clause.nodeId
    ? t('fr.onNode', { name: nodeName || clause.nodeId })
    : kind === 'iot'
      ? t('fr.onSensorNode')
      : kind === 'camera'
        ? t('fr.onCameraNode')
        : kind === 'door'
          ? t('fr.onDoorNode')
          : t('fr.onAnyNode');
  const cat = clause.match && clause.category ? ` ${t('fr.inCategory', { category: clause.category })}` : '';
  return <span className="fr-phrase"><strong>{what}</strong> {where}{cat}</span>;
}

// RuleSummary is the whole rule in one sentence — the reason an operator can trust the list
// without opening the editor. The absences are styled apart because they are the half of the
// rule that expresses innocence.
function RuleSummary({ detail, nodeNames }) {
  const t = useT();
  const clauses = detail.clauses || [];
  const required = clauses.filter((c) => c.mode !== 'absent');
  const absent = clauses.filter((c) => c.mode === 'absent');
  if (clauses.length === 0) return <p className="fr-summary fr-summary--empty">{t('fr.sumNoClauses')}</p>;
  return (
    <p className="fr-summary">
      {required.map((c, i) => (
        <span key={`r${c.id || i}`}>
          {i > 0 ? <span className="fr-and"> {t('fr.sumAnd')} </span> : null}
          <ClausePhrase clause={c} nodeName={nodeNames.get(String(c.nodeId))} />
        </span>
      ))}
      {absent.length > 0 ? (
        <span className="fr-summary-absent">
          {' — '}
          {absent.map((c, i) => (
            <span key={`a${c.id || i}`}>
              {i === 0 ? `${t('fr.sumWithNo')} ` : ` ${t('fr.sumAndNo')} `}
              <ClausePhrase clause={c} nodeName={nodeNames.get(String(c.nodeId))} />
            </span>
          ))}
        </span>
      ) : null}
      <span className="fr-summary-window"> · {t('fr.sumWithin', { n: detail.rule?.windowSeconds || 0 })}</span>
    </p>
  );
}

// ClauseRow is one condition in the editor. An "absent" row is deliberately a different
// creature on screen: it is what stops the rule firing on every legitimate badge entry.
function ClauseRow({ index, clause, nodes, busy, onChange, onRemove }) {
  const t = useT();
  const kindLabel = useKindLabel();
  const absent = clause.mode === 'absent';
  const set = (patch) => onChange({ ...clause, ...patch });
  // Picking a node type narrows the node list; picking a node from another type clears the
  // stale selection so the clause can never say "a camera node — specifically, this sensor hub".
  const selectable = (nodes || []).filter((n) => {
    if (!clause.kind) return true;
    return normKind(n.kind) === clause.kind;
  });
  return (
    <div className={`fr-clause${absent ? ' fr-clause--absent' : ''}`}>
      <div className="fr-clause-head">
        <span className={`fr-clause-badge${absent ? ' fr-clause-badge--absent' : ''}`}>
          <Ico n={absent ? 'eye-off' : 'check-ok'} sz={13} />
          {absent ? t('fr.badgeAbsent') : t('fr.badgeRequired')}
        </span>
        <span className="fr-clause-n">{t('fr.clauseN', { n: index + 1 })}</span>
        <button type="button" className="quiet danger-text fr-clause-remove" onClick={onRemove} disabled={busy}>
          <span className="btn-icon"><Ico n="trash" sz={13} /> {t('fr.remove')}</span>
        </button>
      </div>
      {absent ? <p className="fr-clause-note">{t('fr.absentNote')}</p> : null}
      <div className="settings-field-grid">
        <label>
          {t('fr.mode')}
          <select value={clause.mode} onChange={(e) => set({ mode: e.target.value })} disabled={busy}>
            <option value="required">{t('fr.modeRequired')}</option>
            <option value="absent">{t('fr.modeAbsent')}</option>
          </select>
        </label>
        <label>
          {t('fr.clauseKind')}
          <select
            value={clause.kind}
            onChange={(e) => {
              const kind = e.target.value;
              const keep = !clause.nodeId || (nodes || []).some((n) => {
                if (String(n.nodeId) !== String(clause.nodeId)) return false;
                if (!kind) return true;
                return normKind(n.kind) === kind;
              });
              set({ kind, nodeId: keep ? clause.nodeId : '' });
            }}
            disabled={busy}
          >
            {KINDS.map((k) => (
              <option key={k || 'any'} value={k}>{k ? kindLabel(k) : t('fr.kindAny')}</option>
            ))}
          </select>
        </label>
        <label>
          {t('fr.clauseNode')} <span className="settings-field-opt">{t('fr.optional')}</span>
          <select value={clause.nodeId} onChange={(e) => set({ nodeId: e.target.value })} disabled={busy}>
            <option value="">{t('fr.nodeAny')}</option>
            {selectable.map((n) => (
              <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId} · {kindLabel(n.kind)}</option>
            ))}
          </select>
        </label>
        <label>
          {t('fr.clauseCategory')} <span className="settings-field-opt">{t('fr.optional')}</span>
          <input value={clause.category} onChange={(e) => set({ category: e.target.value })} placeholder={t('fr.categoryPlaceholder')} disabled={busy} />
        </label>
        <label className="field-span-two">
          {t('fr.clauseMatch')}
          <input value={clause.match} onChange={(e) => set({ match: e.target.value })} placeholder={t('fr.matchPlaceholder')} disabled={busy} />
          <span className="fr-field-hint">{t('fr.matchHint')}</span>
        </label>
      </div>
    </div>
  );
}

// RuleEditor replaces the list while an operator writes a rule — the clause builder needs the
// full width, and a half-filled correlation rule is not something to lose to a stray backdrop
// click. Validation is the SERVER's: its refusals say why in words worth reading, so they are
// surfaced verbatim rather than pre-empted here.
function RuleEditor({ initial, nodes, busy, error, onCancel, onSave }) {
  const t = useT();
  const [form, setForm] = useState(initial);
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));
  const num = (v) => (v === '' ? 0 : Math.max(0, parseInt(v, 10) || 0));

  const setClause = (i, next) => setForm((f) => ({ ...f, clauses: f.clauses.map((c, idx) => (idx === i ? next : c)) }));
  const addClause = (mode) => setForm((f) => ({ ...f, clauses: [...f.clauses, blankClause(mode)] }));
  const removeClause = (i) => setForm((f) => ({ ...f, clauses: f.clauses.filter((_, idx) => idx !== i) }));

  return (
    <form
      className="workspace"
      onSubmit={(e) => { e.preventDefault(); onSave(form); }}
    >
      <section className="settings-panel span-two">
        <header>
          <h2>
            <span className="btn-icon">
              <Ico n="grid4" /> {form.id ? t('fr.editorEdit') : t('fr.editorNew')}
            </span>
          </h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={onCancel} disabled={busy}>
              <span className="btn-icon"><Ico n="arr-left" /> {t('fr.back')}</span>
            </button>
          </div>
        </header>

        {error ? <p className="fr-error" role="alert"><Ico n="warning" sz={14} /> {error}</p> : null}

        <div className="settings-field-grid">
          <label className="field-span-two">
            {t('fr.name')}
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} placeholder={t('fr.namePlaceholder')} disabled={busy} autoFocus />
            <span className="fr-field-hint">{t('fr.nameHint')}</span>
          </label>
          <label>
            {t('fr.severity')}
            <select value={form.severity} onChange={(e) => set({ severity: e.target.value })} disabled={busy}>
              {SEVERITIES.map((s) => <option key={s} value={s}>{t(`fr.sev.${s}`)}</option>)}
            </select>
          </label>
          <div className="fr-enable-field">
            <span className="fr-enable-label">{t('fr.enabledLabel')}</span>
            <Toggle
              checked={!!form.enabled}
              disabled={busy}
              onChange={(e) => set({ enabled: e.target.checked })}
              label={form.enabled ? t('fr.enabled') : t('fr.disabled')}
              ariaLabel={t('fr.enabledLabel')}
            />
          </div>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="refresh" /> {t('fr.timingTitle')}</span></h2>
        </header>
        <div className="fr-timing">
          <label className="fr-timing-field">
            <span className="fr-timing-label">{t('fr.window')}</span>
            <input type="number" min="1" value={form.windowSeconds} onChange={(e) => set({ windowSeconds: num(e.target.value) })} disabled={busy} />
            <span className="fr-field-hint">{t('fr.windowHint')}</span>
          </label>

          {/* The grace delay is the subtle one — and the difference between a product and a
              nuisance. It gets a panel of its own copy, not a one-line label. */}
          <label className="fr-timing-field fr-timing-field--grace">
            <span className="fr-timing-label">
              <Ico n="info" sz={13} /> {t('fr.grace')}
            </span>
            <input type="number" min="0" value={form.graceSeconds} onChange={(e) => set({ graceSeconds: num(e.target.value) })} disabled={busy} />
            <span className="fr-field-hint fr-grace-hint">{t('fr.graceHint')}</span>
            <span className="fr-field-hint">{t('fr.graceDefaultHint')}</span>
          </label>

          <label className="fr-timing-field">
            <span className="fr-timing-label">{t('fr.cooldown')}</span>
            <input type="number" min="0" value={form.cooldownSeconds} onChange={(e) => set({ cooldownSeconds: num(e.target.value) })} disabled={busy} />
            <span className="fr-field-hint">{t('fr.cooldownHint')}</span>
          </label>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="list" /> {t('fr.clauses')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={() => addClause('required')} disabled={busy}>
              <span className="btn-icon"><Ico n="plus" /> {t('fr.addRequired')}</span>
            </button>
            <button type="button" className="quiet fr-add-absent" onClick={() => addClause('absent')} disabled={busy}>
              <span className="btn-icon"><Ico n="eye-off" /> {t('fr.addAbsent')}</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">{t('fr.clausesHint')}</p>
        {form.clauses.length === 0 ? (
          <p className="settings-hint">{t('fr.noClauses')}</p>
        ) : (
          <div className="fr-clause-list">
            {form.clauses.map((c, i) => (
              <ClauseRow
                key={i} // eslint-disable-line react/no-array-index-key
                index={i}
                clause={c}
                nodes={nodes}
                busy={busy}
                onChange={(next) => setClause(i, next)}
                onRemove={() => removeClause(i)}
              />
            ))}
          </div>
        )}
        <div className="modal-actions fr-editor-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('fr.cancel')}</button>
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="save" /> {busy ? t('fr.saving') : t('fr.save')}</span>
          </button>
        </div>
      </section>
    </form>
  );
}

// prefill, when set, opens the editor with an agent-suggested draft (the AI
// Insight page's "Create rule" hand-off). It is a SUGGESTION: nothing is saved
// until the operator presses Save; onPrefillConsumed clears the hand-off state
// so leaving and returning shows the normal list.
export function FleetRulesPage({ nodes = [], session, onToast, prefill = null, onPrefillConsumed }) {
  const t = useT();
  const kindLabel = useKindLabel();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(null); // SaveFleetRuleRequest-shaped draft, or null
  const [error, setError] = useState('');
  const canWrite = !!session?.isSuperadmin;

  const nodeNames = useMemo(() => {
    const m = new Map();
    (nodes || []).forEach((n) => m.set(String(n.nodeId), n.name || n.nodeId));
    return m;
  }, [nodes]);

  const load = useCallback(async () => {
    setLoading(true);
    const r = await call('/api/fleet-rules');
    setLoading(false);
    if (r.ok) setItems(Array.isArray(r.body?.items) ? r.body.items : []);
    else if (onToast) onToast(r.message || t('fr.loadFailed'), 'error');
  }, [onToast, t]);
  useEffect(() => { load(); }, [load]);

  // An agent-suggested draft opens the editor pre-filled (superadmin only —
  // rule writes are superadmin-gated server-side anyway).
  useEffect(() => {
    if (!prefill || !canWrite) return;
    setError('');
    setEditing({
      ...blankRule(),
      name: prefill.name || '',
      windowSeconds: prefill.windowSeconds || 120,
      graceSeconds: prefill.graceSeconds || 5,
      cooldownSeconds: prefill.cooldownSeconds || 300,
      clauses: [
        { ...blankClause('required'), nodeId: prefill.nodeId || '', category: prefill.category || '' },
        blankClause('absent'),
      ],
    });
    onPrefillConsumed && onPrefillConsumed();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prefill, canWrite]);

  // save posts the whole rule: clauses are replaced wholesale, so an edit either lands or does
  // not — a correlation rule that half-applied would be worse than one that never saved.
  async function save(form) {
    setBusy(true);
    setError('');
    const r = await call('/api/fleet-rules', { method: 'POST', body: JSON.stringify(form) });
    setBusy(false);
    if (!r.ok) {
      // The server's refusals are written to be read ("a rule made only of absences fires on
      // nothing"). Show them as they are.
      setError(r.message || t('fr.saveFailed'));
      return;
    }
    setEditing(null);
    if (onToast) onToast(t('fr.saved'));
    load();
  }

  // Flipping the switch on the list re-saves the rule with `enabled` toggled — the clauses go
  // back up unchanged, which is exactly what "replaced wholesale" means.
  async function toggleEnabled(detail) {
    const req = toRequest(detail);
    req.enabled = !req.enabled;
    setBusy(true);
    const r = await call('/api/fleet-rules', { method: 'POST', body: JSON.stringify(req) });
    setBusy(false);
    if (r.ok) {
      if (onToast) onToast(req.enabled ? t('fr.ruleEnabled') : t('fr.ruleDisabled'));
      load();
    } else if (onToast) onToast(r.message || t('fr.saveFailed'), 'error');
  }

  async function remove(detail) {
    const name = detail.rule?.name || '';
    if (!window.confirm(t('fr.confirmDelete', { name }))) return;
    setBusy(true);
    const r = await call(`/api/fleet-rules/${detail.rule.id}`, { method: 'DELETE' });
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('fr.deleted')); load(); }
    else if (onToast) onToast(r.message || t('fr.deleteFailed'), 'error');
  }

  if (editing) {
    return (
      <>
        <FormBusyOverlay busy={busy} />
        <RuleEditor
          initial={editing}
          nodes={nodes}
          busy={busy}
          error={error}
          onCancel={() => { setEditing(null); setError(''); }}
          onSave={save}
        />
      </>
    );
  }

  return (
    <section className="workspace">
      <FormBusyOverlay busy={loading || busy} />

      {/* The pitch. Said once, plainly, at the top: what a cross-domain rule is, and why it is
          worth more than any single sensor in the fleet. */}
      <section className="settings-panel span-two fr-pitch">
        <header>
          <h2><span className="btn-icon"><Ico n="grid4" /> {t('fr.pitchTitle')}</span></h2>
        </header>
        <p className="fr-pitch-lead">{t('fr.pitchLead')}</p>
        <p className="fr-pitch-example">
          <Ico n="arr-right" sz={14} /> {t('fr.pitchExample')}
        </p>
        <p className="fr-pitch-noise">{t('fr.pitchNoise')}</p>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="list" /> {t('fr.rulesTitle')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('fr.refresh')}</span>
            </button>
            {canWrite ? (
              <button type="button" onClick={() => { setError(''); setEditing(blankRule()); }} disabled={busy}>
                <span className="btn-icon"><Ico n="plus" /> {t('fr.newRule')}</span>
              </button>
            ) : null}
          </div>
        </header>
        {!canWrite ? <p className="settings-hint">{t('fr.readOnly')}</p> : null}

        {items.length === 0 ? (
          <p className="settings-hint">{canWrite ? t('fr.empty') : t('fr.emptyReadonly')}</p>
        ) : (
          <div className="fr-list">
            {items.map((detail) => {
              const rule = detail.rule || {};
              const sev = String(rule.severity || 'critical').toLowerCase();
              return (
                <article key={rule.id} className={`fr-card${rule.enabled ? '' : ' fr-card--off'}`}>
                  <div className="fr-card-head">
                    <h3 className="fr-card-name">{rule.name}</h3>
                    <span className={`fr-sev fr-sev--${sev}`}>{SEVERITIES.includes(sev) ? t(`fr.sev.${sev}`) : sev}</span>
                    {canWrite ? (
                      <Toggle
                        checked={!!rule.enabled}
                        disabled={busy}
                        onChange={() => toggleEnabled(detail)}
                        label={rule.enabled ? t('fr.enabled') : t('fr.disabled')}
                        ariaLabel={t('fr.enabledLabel')}
                      />
                    ) : (
                      <span className={`status-pill ${rule.enabled ? 'online' : 'offline'}`}>
                        {rule.enabled ? t('fr.enabled') : t('fr.disabled')}
                      </span>
                    )}
                  </div>

                  <RuleSummary detail={detail} nodeNames={nodeNames} />

                  <div className="fr-card-foot">
                    <span className="fr-meta">
                      <Ico n="bell" sz={13} />
                      {rule.lastTriggeredAt ? t('fr.lastFired', { time: formatTimestamp(rule.lastTriggeredAt) }) : t('fr.neverFired')}
                    </span>
                    <span className="fr-meta">
                      <Ico n="refresh" sz={13} />
                      {t('fr.metaTiming', { grace: rule.graceSeconds || 0, cooldown: rule.cooldownSeconds || 0 })}
                    </span>
                    {canWrite ? (
                      <span className="fr-card-actions">
                        <button type="button" className="quiet" onClick={() => { setError(''); setEditing(toRequest(detail)); }} disabled={busy}>
                          <span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('fr.edit')}</span>
                        </button>
                        <button type="button" className="quiet danger-text" onClick={() => remove(detail)} disabled={busy}>
                          <span className="btn-icon"><Ico n="trash" sz={13} /> {t('fr.delete')}</span>
                        </button>
                      </span>
                    ) : null}
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </section>

      {/* The fleet a rule can draw on. A camera node and a sensor hub are not interchangeable,
          so the operator writing a clause can see which of each they actually have. */}
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> {t('fr.fleetTitle')}</span></h2>
        </header>
        <p className="settings-hint">{t('fr.fleetHint')}</p>
        {(nodes || []).length === 0 ? (
          <p className="settings-hint">{t('fr.noNodes')}</p>
        ) : (
          <div className="fr-node-chips">
            {(nodes || []).map((n) => {
              const k = normKind(n.kind);
              const chipIcon = k === 'iot' ? 'cpu' : k === 'door' ? 'door' : 'video';
              return (
                <span key={n.nodeId} className={`fr-node-chip fr-node-chip--${k}`}>
                  <Ico n={chipIcon} sz={13} />
                  {n.name || n.nodeId}
                  <span className="fr-node-chip-kind">{kindLabel(n.kind)}</span>
                </span>
              );
            })}
          </div>
        )}
      </section>
    </section>
  );
}
