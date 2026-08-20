import { useCallback, useEffect, useMemo, useState } from 'react';
import { Ico, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { api, formatTimestamp } from '../lib/helpers';

// FleetPolicyPage answers a question a fleet could not previously answer about itself:
// is every appliance configured the way we decided it should be?
//
// The control plane could always CHANGE a node's settings — the node screens tunnel to the
// node's own API. What it could not do was say what the settings ought to be. So the
// configuration of a fleet lived only in the fleet: fifty appliances, each authoritative
// about itself, and "is continuity monitoring on everywhere?" meant opening fifty screens.
//
// The comparison, not the push, is the product. Enforcement is off by default on every
// policy — see the note beside the switch in the editor.

// Compliance verdicts, in the order the summary tiles read. Drifted first: it is the only
// one that asks for an action.
const STATUSES = ['drifted', 'unknown', 'compliant', 'unmanaged'];

const SCOPES = ['fleet', 'site', 'node'];

// Every call passes noRedirect: a 403 here means "not a superadmin", which is something to
// say in place rather than a reason to bounce to the SSO login.
const call = (path, options = {}) => api(path, { noRedirect: true, ...options }).catch(() => ({ ok: false }));

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

// tr falls back to the SERVER's English label when a key is missing, rather than to the key
// itself (which is what the shared t() does). The catalog is served by the server, so a
// section added there before its translation lands still reads as words on screen.
const tr = (t, key, fallback) => {
  const s = t(key);
  return s === key ? (fallback || key) : s;
};

const normKind = (kind) => {
  const k = String(kind || 'camera').toLowerCase();
  return k === 'iot' || k === 'door' ? k : 'camera';
};

const blankPolicy = (nodeKind = 'camera') => ({
  id: 0,
  name: '',
  description: '',
  enabled: true,
  scope: 'fleet',
  targetId: '',
  nodeKind,
  // Report-only. A new policy must never start out able to rewrite the estate.
  enforce: false,
  items: [],
});

const toRequest = (detail) => ({
  id: detail.policy?.id || 0,
  name: detail.policy?.name || '',
  description: detail.policy?.description || '',
  enabled: !!detail.policy?.enabled,
  scope: detail.policy?.scope || 'fleet',
  targetId: detail.policy?.targetId || '',
  nodeKind: detail.policy?.nodeKind || 'camera',
  enforce: !!detail.policy?.enforce,
  items: (detail.items || []).map((i) => ({ section: i.section, field: i.field, value: String(i.value ?? '') })),
});

function useKindLabel() {
  const t = useT();
  return useCallback((kind) => {
    switch (String(kind || '').toLowerCase()) {
      case 'iot': return t('fp.kindSensor');
      case 'door': return t('fp.kindDoor');
      default: return t('fp.kindCamera');
    }
  }, [t]);
}

// FieldInput renders the right control for a catalog field's declared type, so a bool is a
// switch and a bounded number carries its own bounds. The server validates regardless — this
// only stops the operator finding out by having a filled-in form rejected.
function FieldInput({ field, value, busy, onChange }) {
  const t = useT();
  if (field.kind === 'bool') {
    return (
      <Toggle
        checked={String(value) === 'true'}
        disabled={busy}
        onChange={(e) => onChange(e.target.checked ? 'true' : 'false')}
        label={String(value) === 'true' ? t('fp.valueOn') : t('fp.valueOff')}
        ariaLabel={field.label}
      />
    );
  }
  return (
    <span className="fp-value-input">
      <input
        type="number"
        value={value}
        min={field.min || undefined}
        max={field.max || undefined}
        step={field.kind === 'float' ? 'any' : 1}
        disabled={busy}
        onChange={(e) => onChange(e.target.value)}
        aria-label={field.label}
      />
      {field.unit ? <span className="fp-unit">{field.unit}</span> : null}
    </span>
  );
}

// PolicyEditor builds a policy out of the SERVER's catalog rather than a hardcoded list, so
// the form can never offer a field the server would refuse.
function PolicyEditor({ initial, catalog, nodes, sites, busy, error, onCancel, onSave }) {
  const t = useT();
  const kindLabel = useKindLabel();
  const [form, setForm] = useState(initial);
  useEffect(() => { setForm(initial); }, [initial]);

  const sections = useMemo(
    () => (catalog.sections || []).filter((s) => (s.nodeKinds || []).includes(normKind(form.nodeKind))),
    [catalog, form.nodeKind],
  );
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const selected = useMemo(() => {
    const m = new Map();
    (form.items || []).forEach((i) => m.set(`${i.section}.${i.field}`, i.value));
    return m;
  }, [form.items]);

  // Ticking a field adds it with the field's own sensible starting point; unticking removes
  // it. A policy states only the settings it names.
  const toggleField = (section, field) => {
    const key = `${section.id}.${field.key}`;
    if (selected.has(key)) {
      set({ items: form.items.filter((i) => `${i.section}.${i.field}` !== key) });
      return;
    }
    const start = field.kind === 'bool' ? 'true' : String(field.min || 1);
    set({ items: [...form.items, { section: section.id, field: field.key, value: start }] });
  };

  const setValue = (section, field, value) => {
    const key = `${section.id}.${field.key}`;
    set({ items: form.items.map((i) => (`${i.section}.${i.field}` === key ? { ...i, value } : i)) });
  };

  // Changing the node kind drops items belonging to sections that kind does not have —
  // otherwise the save is refused for settings the operator can no longer even see.
  const changeKind = (nodeKind) => {
    const allowed = new Set(
      (catalog.sections || []).filter((s) => (s.nodeKinds || []).includes(normKind(nodeKind))).map((s) => s.id),
    );
    setForm((f) => ({ ...f, nodeKind, items: f.items.filter((i) => allowed.has(i.section)) }));
  };

  const targets = form.scope === 'site' ? sites : nodes;

  return (
    <form
      className="workspace"
      onSubmit={(e) => { e.preventDefault(); onSave(form); }}
    >
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="sliders" /> {form.id ? t('fp.editTitle') : t('fp.newTitle')}</span></h2>
        </header>
        {/* An inline, VISIBLE error. `.danger-text` alone is inert on a non-button here —
            that mistake once made ~25 validation errors read as grey hints. */}
        {error ? <p className="fp-error" role="alert"><Ico n="warning" sz={14} /> {error}</p> : null}

        <div className="settings-field-grid">
          <label>
            {t('fp.name')}
            <input value={form.name} onChange={(e) => set({ name: e.target.value })} disabled={busy} required />
          </label>
          <label>
            {t('fp.nodeKind')}
            <select value={form.nodeKind} onChange={(e) => changeKind(e.target.value)} disabled={busy}>
              {(catalog.nodeKinds || ['camera']).map((k) => (
                <option key={k} value={k}>{kindLabel(k)}</option>
              ))}
            </select>
          </label>
          <label>
            {t('fp.scope')}
            <select value={form.scope} onChange={(e) => set({ scope: e.target.value, targetId: '' })} disabled={busy}>
              {SCOPES.map((s) => <option key={s} value={s}>{t(`fp.scope.${s}`)}</option>)}
            </select>
          </label>
          {form.scope !== 'fleet' ? (
            <label>
              {form.scope === 'site' ? t('fp.site') : t('fp.node')}
              <select value={form.targetId} onChange={(e) => set({ targetId: e.target.value })} disabled={busy} required>
                <option value="">{t('fp.choose')}</option>
                {(targets || []).map((x) => (
                  <option key={x.id ?? x.nodeId} value={String(x.id ?? x.nodeId)}>{x.name || x.nodeId}</option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
        <label className="settings-field-wide">
          {t('fp.description')} <span className="settings-field-opt">{t('fp.optional')}</span>
          <input value={form.description} onChange={(e) => set({ description: e.target.value })} disabled={busy} />
        </label>

        <p className="settings-hint">{t('fp.scopeHint')}</p>

        <div className="fp-switches">
          <Toggle
            checked={!!form.enabled}
            disabled={busy}
            onChange={(e) => set({ enabled: e.target.checked })}
            label={form.enabled ? t('fp.enabled') : t('fp.disabled')}
            ariaLabel={t('fp.enabledLabel')}
          />
          <div className="fp-enforce">
            <Toggle
              checked={!!form.enforce}
              disabled={busy}
              onChange={(e) => set({ enforce: e.target.checked })}
              label={form.enforce ? t('fp.enforceOn') : t('fp.enforceOff')}
              ariaLabel={t('fp.enforceLabel')}
            />
            {/* The one warning on this screen worth reading. */}
            <p className={`fp-enforce-note${form.enforce ? ' fp-enforce-note--armed' : ''}`}>
              {form.enforce ? t('fp.enforceWarn') : t('fp.enforceNote')}
            </p>
          </div>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="list" /> {t('fp.settingsTitle')}</span></h2>
        </header>
        <p className="settings-hint">{t('fp.settingsHint')}</p>
        {sections.length === 0 ? <p className="settings-hint">{t('fp.noSections')}</p> : null}
        {sections.map((section) => (
          <div key={section.id} className="fp-section">
            <h3 className="fp-section-name">{tr(t, `fp.section.${section.id}`, section.label)}</h3>
            <div className="fp-field-rows">
              {(section.fields || []).map((field) => {
                const key = `${section.id}.${field.key}`;
                const on = selected.has(key);
                return (
                  <div key={key} className={`fp-field-row${on ? ' fp-field-row--on' : ''}`}>
                    <label className="fp-field-pick">
                      <input type="checkbox" checked={on} disabled={busy} onChange={() => toggleField(section, field)} />
                      <span>{tr(t, `fp.field.${section.id}.${field.key}`, field.label)}</span>
                    </label>
                    {on ? (
                      <FieldInput
                        field={field}
                        value={selected.get(key)}
                        busy={busy}
                        onChange={(v) => setValue(section, field, v)}
                      />
                    ) : null}
                  </div>
                );
              })}
            </div>
          </div>
        ))}

        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('fp.cancel')}</button>
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="save" /> {busy ? t('fp.saving') : t('fp.save')}</span>
          </button>
        </div>
      </section>
    </form>
  );
}

// NodeComplianceCard shows one node's verdict, and — when it disagrees — exactly which
// setting, what the fleet asked for, what the appliance holds, and which policy asked.
// Naming the policy is what makes an unexpected desired value diagnosable.
function NodeComplianceCard({ node }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const problems = [];
  (node.sections || []).forEach((s) => {
    (s.fields || []).forEach((f) => { if (f.status !== 'match') problems.push({ ...f, sectionLabel: s.label }); });
  });
  return (
    <article className={`fp-node fp-node--${node.status}`}>
      <div className="fp-node-head">
        <span className={`fp-badge fp-badge--${node.status}`}>{t(`fp.status.${node.status}`)}</span>
        <h3 className="fp-node-name">{node.nodeName || node.nodeId}</h3>
        {node.driftCount > 0 ? <span className="fp-node-count">{t('fp.nDrifted', { n: node.driftCount })}</span> : null}
        {(node.sections || []).length > 0 ? (
          <button type="button" className="quiet fp-node-toggle" onClick={() => setOpen((v) => !v)}>
            {open ? t('fp.hideDetail') : t('fp.showDetail')}
          </button>
        ) : null}
      </div>
      {node.reason ? <p className="fp-node-reason">{node.reason}</p> : null}
      {problems.length > 0 && !open ? (
        <p className="fp-node-summary">
          {problems.slice(0, 3).map((p) => (
            <span key={`${p.section}.${p.field}`} className="fp-chip">
              {tr(t, `fp.field.${p.section}.${p.field}`, p.label)}: <strong>{p.actual || t('fp.absent')}</strong> → {p.desired}
            </span>
          ))}
          {problems.length > 3 ? <span className="fp-chip fp-chip--more">{t('fp.andMore', { n: problems.length - 3 })}</span> : null}
        </p>
      ) : null}
      {open ? (
        <div className="fp-node-detail">
          {(node.sections || []).map((s) => (
            <div key={s.section} className="fp-detail-section">
              <h4>{tr(t, `fp.section.${s.section}`, s.label)}</h4>
              {s.error ? <p className="fp-detail-error">{s.error}</p> : null}
              {(s.fields || []).length > 0 ? (
                <table className="fp-detail-table">
                  <thead>
                    <tr>
                      <th>{t('fp.colSetting')}</th>
                      <th>{t('fp.colDesired')}</th>
                      <th>{t('fp.colActual')}</th>
                      <th>{t('fp.colPolicy')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {s.fields.map((f) => (
                      <tr key={f.field} className={`fp-row--${f.status}`}>
                        <td>{tr(t, `fp.field.${s.section}.${f.field}`, f.label)}{f.unit ? ` (${f.unit})` : ''}</td>
                        <td>{f.desired}</td>
                        <td>{f.status === 'missing' ? <em>{t('fp.absent')}</em> : f.actual}</td>
                        <td>
                          {f.policyName}
                          <span className="fp-scope-tag">{t(`fp.scope.${f.scope}`)}</span>
                          {f.enforce ? <span className="fp-scope-tag fp-scope-tag--enforce">{t('fp.enforcing')}</span> : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
    </article>
  );
}

export function FleetPolicyPage({ nodes = [], session, onToast }) {
  const t = useT();
  const kindLabel = useKindLabel();
  const [items, setItems] = useState([]);
  const [catalog, setCatalog] = useState({ sections: [], nodeKinds: ['camera'] });
  const [compliance, setCompliance] = useState({ nodes: [], counts: {}, checkedAt: 0 });
  // Sites are fetched here rather than threaded through the app shell: this is the only
  // screen that needs them, and a site policy names a site by id.
  const [sites, setSites] = useState([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');
  const canWrite = !!session?.isSuperadmin;

  const load = useCallback(async () => {
    setLoading(true);
    const [list, cat, comp, siteList] = await Promise.all([
      call('/api/fleet-policies'),
      call('/api/fleet-policies/catalog'),
      call('/api/fleet-policies/compliance'),
      call('/api/sites'),
    ]);
    setLoading(false);
    // A role without site access still gets the rest of the page; only the site-scope
    // picker goes empty, and the server refuses a site policy with no site anyway.
    if (siteList.ok) setSites(Array.isArray(siteList.body) ? siteList.body : (siteList.body?.items || []));
    if (list.ok) setItems(Array.isArray(list.body?.items) ? list.body.items : []);
    else if (onToast) onToast(list.message || t('fp.loadFailed'), 'error');
    if (cat.ok && cat.body) setCatalog({ sections: cat.body.sections || [], nodeKinds: cat.body.nodeKinds || ['camera'] });
    if (comp.ok && comp.body) setCompliance(comp.body || { nodes: [], counts: {} });
  }, [onToast, t]);
  useEffect(() => { load(); }, [load]);

  // A refresh is one tunneled round trip per section per node, so it is a deliberate act
  // with a spinner, not something a page load does behind the operator's back.
  async function refresh() {
    setBusy(true);
    const r = await call('/api/fleet-policies/compliance/refresh', { method: 'POST' });
    setBusy(false);
    if (r.ok && r.body) {
      setCompliance(r.body);
      if (onToast) onToast(t('fp.refreshed'));
      load();
    } else if (onToast) onToast(r.message || t('fp.refreshFailed'), 'error');
  }

  async function save(form) {
    setBusy(true);
    setError('');
    const r = await call('/api/fleet-policies', { method: 'POST', body: JSON.stringify(form) });
    setBusy(false);
    if (!r.ok) {
      // The server's refusals are written to be read ("a policy with no settings states
      // nothing"). Show them as they are.
      setError(r.message || t('fp.saveFailed'));
      return;
    }
    setEditing(null);
    if (onToast) onToast(t('fp.saved'));
    load();
  }

  async function toggleEnabled(detail) {
    const req = toRequest(detail);
    req.enabled = !req.enabled;
    setBusy(true);
    const r = await call('/api/fleet-policies', { method: 'POST', body: JSON.stringify(req) });
    setBusy(false);
    if (r.ok) { if (onToast) onToast(req.enabled ? t('fp.policyEnabled') : t('fp.policyDisabled')); load(); }
    else if (onToast) onToast(r.message || t('fp.saveFailed'), 'error');
  }

  async function remove(detail) {
    const id = detail.policy?.id;
    if (!id) return;
    setBusy(true);
    const r = await call(`/api/fleet-policies/${id}`, { method: 'DELETE' });
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('fp.deleted')); load(); }
    else if (onToast) onToast(r.message || t('fp.deleteFailed'), 'error');
  }

  const nodeNames = useMemo(() => {
    const m = new Map();
    (nodes || []).forEach((n) => m.set(String(n.nodeId), n.name || n.nodeId));
    return m;
  }, [nodes]);
  const siteNames = useMemo(() => {
    const m = new Map();
    (sites || []).forEach((s) => m.set(String(s.id), s.name));
    return m;
  }, [sites]);

  const targetLabel = (policy) => {
    if (policy.scope === 'site') return siteNames.get(String(policy.targetId)) || `#${policy.targetId}`;
    if (policy.scope === 'node') return nodeNames.get(String(policy.targetId)) || policy.targetId;
    return t('fp.wholeFleet');
  };

  if (editing) {
    return (
      <>
        <FormBusyOverlay busy={busy} />
        <PolicyEditor
          initial={editing}
          catalog={catalog}
          nodes={nodes}
          sites={sites}
          busy={busy}
          error={error}
          onCancel={() => { setEditing(null); setError(''); }}
          onSave={save}
        />
      </>
    );
  }

  const counts = compliance.counts || {};
  const driftedNodes = (compliance.nodes || []).filter((n) => n.status === 'drifted' || n.status === 'unknown');
  const otherNodes = (compliance.nodes || []).filter((n) => n.status !== 'drifted' && n.status !== 'unknown');

  return (
    <section className="workspace">
      <FormBusyOverlay busy={loading || busy} />

      <section className="settings-panel span-two fp-pitch">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> {t('fp.pitchTitle')}</span></h2>
        </header>
        <p className="fp-pitch-lead">{t('fp.pitchLead')}</p>
        <p className="fp-pitch-noise">{t('fp.pitchReport')}</p>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> {t('fp.complianceTitle')}</span></h2>
          <div className="settings-header-actions">
            <span className="settings-hint fp-checked">
              {compliance.checkedAt ? t('fp.checkedAt', { time: formatTimestamp(compliance.checkedAt) }) : t('fp.neverChecked')}
            </span>
            {canWrite ? (
              <button type="button" className="quiet" onClick={refresh} disabled={busy}>
                <span className="btn-icon"><Ico n="reload" /> {t('fp.checkNow')}</span>
              </button>
            ) : null}
          </div>
        </header>

        <div className="fp-tiles">
          {STATUSES.map((s) => (
            <div key={s} className={`fp-tile fp-tile--${s}`}>
              <span className="fp-tile-n">{counts[s] || 0}</span>
              <span className="fp-tile-label">{t(`fp.status.${s}`)}</span>
            </div>
          ))}
        </div>
        {/* Said plainly, because it is the distinction the whole report rests on. */}
        <p className="settings-hint">{t('fp.unknownHint')}</p>

        {(compliance.nodes || []).length === 0 ? (
          <p className="settings-hint">{t('fp.noComplianceYet')}</p>
        ) : (
          <div className="fp-node-list">
            {driftedNodes.map((n) => <NodeComplianceCard key={n.nodeId} node={n} />)}
            {otherNodes.map((n) => <NodeComplianceCard key={n.nodeId} node={n} />)}
          </div>
        )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="list" /> {t('fp.policiesTitle')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('fp.refresh')}</span>
            </button>
            {canWrite ? (
              <button type="button" onClick={() => { setError(''); setEditing(blankPolicy(catalog.nodeKinds?.[0] || 'camera')); }} disabled={busy}>
                <span className="btn-icon"><Ico n="plus" /> {t('fp.newPolicy')}</span>
              </button>
            ) : null}
          </div>
        </header>
        {!canWrite ? <p className="settings-hint">{t('fp.readOnly')}</p> : null}

        {items.length === 0 ? (
          <p className="settings-hint">{canWrite ? t('fp.empty') : t('fp.emptyReadonly')}</p>
        ) : (
          <div className="fp-list">
            {items.map((detail) => {
              const policy = detail.policy || {};
              return (
                <article key={policy.id} className={`fp-card${policy.enabled ? '' : ' fp-card--off'}`}>
                  <div className="fp-card-head">
                    <h3 className="fp-card-name">{policy.name}</h3>
                    <span className="fp-scope-tag">{t(`fp.scope.${policy.scope}`)}: {targetLabel(policy)}</span>
                    <span className="fp-scope-tag">{kindLabel(policy.nodeKind)}</span>
                    {policy.enforce ? (
                      <span className="fp-scope-tag fp-scope-tag--enforce">{t('fp.enforcing')}</span>
                    ) : (
                      <span className="fp-scope-tag fp-scope-tag--report">{t('fp.reportOnly')}</span>
                    )}
                    {canWrite ? (
                      <Toggle
                        checked={!!policy.enabled}
                        disabled={busy}
                        onChange={() => toggleEnabled(detail)}
                        label={policy.enabled ? t('fp.enabled') : t('fp.disabled')}
                        ariaLabel={t('fp.enabledLabel')}
                      />
                    ) : (
                      <span className={`status-pill ${policy.enabled ? 'online' : 'offline'}`}>
                        {policy.enabled ? t('fp.enabled') : t('fp.disabled')}
                      </span>
                    )}
                  </div>
                  {policy.description ? <p className="fp-card-desc">{policy.description}</p> : null}
                  <p className="fp-card-items">
                    {(detail.items || []).map((i) => (
                      <span key={`${i.section}.${i.field}`} className="fp-chip">
                        {tr(t, `fp.field.${i.section}.${i.field}`, i.field)} = <strong>{i.value}</strong>
                      </span>
                    ))}
                  </p>
                  <div className="fp-card-foot">
                    <span className="fp-meta">
                      <Ico n="refresh" sz={13} />
                      {policy.lastEvaluatedAt
                        ? t('fp.lastEvaluated', { time: formatTimestamp(policy.lastEvaluatedAt) })
                        : t('fp.neverEvaluated')}
                    </span>
                    {canWrite ? (
                      <span className="fp-card-actions">
                        <button type="button" className="quiet" onClick={() => { setError(''); setEditing(toRequest(detail)); }} disabled={busy}>
                          <span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('fp.edit')}</span>
                        </button>
                        <button type="button" className="quiet danger-text" onClick={() => remove(detail)} disabled={busy}>
                          <span className="btn-icon"><Ico n="trash" sz={13} /> {t('fp.delete')}</span>
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
    </section>
  );
}
