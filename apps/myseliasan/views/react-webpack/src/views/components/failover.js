import { useCallback, useEffect, useMemo, useState } from 'react';
import { Ico, useT } from '@shared';
import { HelpButton } from '@shared/Manual';
import { FormBusyOverlay } from './ui';
import { api, formatTimestamp } from '../lib/helpers';

// FailoverPage — N+1 node failover (W3-7).
//
// A recorder is the only thing recording its own cameras. This screen is where somebody
// says which spare picks them up when it stops, and — the part that actually matters —
// where they PROVE it would work, on a quiet afternoon, before it has to.
//
// The screen leads with one question and refuses to soften the answer: if this recorder
// died right now, would this work? A successful copy is not a yes. Copying proves the two
// appliances can talk to each other; it says nothing about whether the spare can reach the
// CAMERAS, which is a different network path with different credentials and the thing that
// actually fails. Only a drill answers it, so a plan that has never been drilled says
// "never tested" in the same place a tested one says "ready".
//
// Every sentence here is composed IN THE BROWSER from a state the server sends. The server
// deliberately returns `readyState` and counts rather than a finished sentence: a sentence
// built on the server arrives in English on an Arabic screen, which is a defect this
// programme has shipped twice and caught twice in the screen pass.

// Ready states, in the order the summary tiles read. The ones asking for an action come
// first — a screen that leads with what is fine buries what is not.
const READY_ORDER = ['not-staged', 'untested', 'partial', 'blind', 'overcommitted', 'standby-down', 'active', 'ready', 'disabled'];

// The states that mean somebody should do something. Used for ordering and for the tile
// emphasis; "active" is not one of them — a takeover in progress is working as intended.
const NEEDS_ATTENTION = new Set(['not-staged', 'untested', 'partial', 'blind', 'overcommitted', 'standby-down']);

const call = (path, options = {}) => api(path, { noRedirect: true, ...options }).catch(() => ({ ok: false }));

const blankPlan = () => ({
  id: 0,
  name: '',
  protectedNodeId: '',
  standbyNodeId: '',
  enabled: true,
  // Off. A new plan must never start out able to move a building's cameras by itself.
  autoActivate: false,
  holdDownSeconds: 300,
});

const toRequest = (view) => ({
  id: view.plan?.id || 0,
  name: view.plan?.name || '',
  protectedNodeId: view.plan?.protectedNodeId || '',
  standbyNodeId: view.plan?.standbyNodeId || '',
  enabled: !!view.plan?.enabled,
  autoActivate: !!view.plan?.autoActivate,
  holdDownSeconds: view.plan?.holdDownSeconds || 300,
});

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

// ReadyBadge is the screen's headline per plan. Its whole job is to never overstate: an
// untested plan and a proved one must not look alike at a glance, which is exactly what a
// green tick for "copied successfully" would produce.
function ReadyBadge({ state }) {
  const t = useT();
  const key = READY_ORDER.includes(state) ? state : 'untested';
  return <span className={`fo-badge fo-badge--${key}`} data-fo-ready={key}>{t(`fo.ready.${key}`)}</span>;
}

// CapacityLine reports the spare's own capacity estimate against everything committed to it.
//
// It is deliberately quiet when the answer is comfortable and loud when it is not: a line
// that always shouts is a line people stop reading, and the one state that has to be read is
// "this spare cannot carry what you have pointed at it".
function CapacityLine({ capacity }) {
  const t = useT();
  const c = capacity || {};
  const state = c.state || 'unknown';
  if (state === 'unknown') {
    // Said, not hidden. "We have not been able to ask" is its own answer, exactly as it is
    // for an untested drill — and an appliance too old to have the endpoint lands here.
    return (
      <p className="fo-capacity fo-capacity--unknown" data-fo-capacity="unknown">
        <Ico n="info" sz={13} /> {t('fo.capacityUnknown')}
      </p>
    );
  }
  const used = (c.ownCameras || 0) + (c.committed || 0) + (c.wanted || 0);
  return (
    <p className={`fo-capacity fo-capacity--${state}`} data-fo-capacity={state}>
      <Ico n={state === 'over' ? 'warning' : state === 'tight' ? 'info' : 'check-ok'} sz={13} />{' '}
      {t(`fo.capacity.${state}`, { used, max: c.estimatedMax || 0 })}
      {c.committed ? ' ' + t('fo.capacityShared', { n: c.committed }) : ''}
    </p>
  );
}

function NodeStatusPill({ status }) {
  const t = useT();
  const s = String(status || '').toLowerCase();
  const cls = s === 'online' ? 'online' : 'offline';
  const label = s ? t(`fo.nodeStatus.${s}`) : t('fo.nodeStatus.unknown');
  return <span className={`status-pill ${cls}`}>{label}</span>;
}

function PlanEditor({ initial, nodes, plans, busy, error, onCancel, onSave }) {
  const t = useT();
  const [form, setForm] = useState(initial);
  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  // Only camera recorders can take part, so the pickers never offer a door controller or a
  // sensor hub. The server refuses those too; offering them would only let somebody fill in
  // a form to be told no.
  const recorders = useMemo(
    () => (nodes || []).filter((n) => !n.kind || String(n.kind).toLowerCase() === 'camera'),
    [nodes],
  );
  // An appliance already spoken for cannot appear in the other picker: failover does not
  // chain, and the reason is easier to show than to explain after the fact.
  const spokenFor = useMemo(() => {
    const m = new Map();
    (plans || []).forEach((v) => {
      const p = v.plan || {};
      if (p.id === form.id) return;
      m.set(String(p.protectedNodeId), 'protected');
      m.set(String(p.standbyNodeId), 'standby');
    });
    return m;
  }, [plans, form.id]);

  const protectedOptions = recorders.filter((n) => !spokenFor.has(String(n.nodeId)) || n.nodeId === form.protectedNodeId);
  const standbyOptions = recorders.filter(
    (n) => n.nodeId !== form.protectedNodeId && (!spokenFor.has(String(n.nodeId)) || n.nodeId === form.standbyNodeId),
  );

  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield-check" /> {form.id ? t('fo.editTitle') : t('fo.newTitle')}</span></h2>
        </header>

        {error ? <p className="fo-error" role="alert"><Ico n="warning" sz={14} /> {error}</p> : null}

        <div className="settings-grid">
          <label className="settings-field">
            <span>{t('fo.fieldName')}</span>
            <input
              type="text" data-fo-field="name" value={form.name} disabled={busy}
              placeholder={t('fo.fieldNameHint')}
              onChange={(e) => set({ name: e.target.value })}
            />
          </label>

          <label className="settings-field">
            <span>{t('fo.fieldProtected')}</span>
            <select data-fo-field="protected" value={form.protectedNodeId} disabled={busy} onChange={(e) => set({ protectedNodeId: e.target.value })}>
              <option value="">{t('fo.choose')}</option>
              {protectedOptions.map((n) => <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>)}
            </select>
            <small className="settings-hint">{t('fo.fieldProtectedHint')}</small>
          </label>

          <label className="settings-field">
            <span>{t('fo.fieldStandby')}</span>
            <select data-fo-field="standby" value={form.standbyNodeId} disabled={busy} onChange={(e) => set({ standbyNodeId: e.target.value })}>
              <option value="">{t('fo.choose')}</option>
              {standbyOptions.map((n) => <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>)}
            </select>
            <small className="settings-hint">{t('fo.fieldStandbyHint')}</small>
          </label>

          <label className="settings-field">
            <span>{t('fo.fieldHoldDown')}</span>
            <input
              type="number" min={120} step={30} value={form.holdDownSeconds} disabled={busy}
              onChange={(e) => set({ holdDownSeconds: Number(e.target.value) || 0 })}
            />
            <small className="settings-hint">{t('fo.fieldHoldDownHint')}</small>
          </label>
        </div>

        <div className="fo-editor-switches">
          <Toggle
            checked={!!form.enabled} disabled={busy}
            onChange={(e) => set({ enabled: e.target.checked })}
            label={t('fo.fieldEnabled')} ariaLabel={t('fo.fieldEnabled')}
          />
          <Toggle
            checked={!!form.autoActivate} disabled={busy}
            onChange={(e) => set({ autoActivate: e.target.checked })}
            label={t('fo.fieldAuto')} ariaLabel={t('fo.fieldAuto')}
          />
          {/* Said next to the switch, not buried in a help page: this is the setting that
              can move a building's cameras with nobody watching. */}
          <p className="settings-hint">{t('fo.fieldAutoHint')}</p>
        </div>

        <div className="settings-actions">
          <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('fo.cancel')}</button>
          <button type="button" data-fo-act="save" onClick={() => onSave(form)} disabled={busy || !form.protectedNodeId || !form.standbyNodeId}>
            <span className="btn-icon"><Ico n="save" /> {t('fo.save')}</span>
          </button>
        </div>
      </section>
    </section>
  );
}

// PlanCard is one arrangement, and the per-camera detail behind it. The camera list is
// fetched only when it is opened: it is one tunneled call to the spare, and doing it for
// every plan on every page load would make the screen as slow as the slowest appliance.
function PlanCard({ view, result, canWrite, busy, onAct, onEdit, onDelete }) {
  const t = useT();
  const plan = view.plan || {};
  const [open, setOpen] = useState(false);
  const [cameras, setCameras] = useState(null);
  const [loadingCams, setLoadingCams] = useState(false);

  const toggleOpen = async () => {
    const next = !open;
    setOpen(next);
    if (!next || cameras !== null) return;
    setLoadingCams(true);
    const r = await call(`/api/failover-plans/${plan.id}`);
    setLoadingCams(false);
    setCameras(r.ok && r.body ? (r.body.cameras || []) : []);
  };

  // What the LAST action actually did, per camera, straight from the appliance. It exists
  // for one moment — the spare computes it while taking over and does not store it — so it
  // is shown immediately and the list opens itself. An operator who has just pressed "Take
  // over" during an outage needs to know which of their cameras is recording, and that is
  // not a thing to go looking for.
  const shown = result && result.length ? result : cameras;
  const listOpen = open || !!(result && result.length);
  const outcomes = (result || []).filter((c) => c.outcome);
  const recording = outcomes.filter((c) => c.outcome === 'recording').length;
  // A HAND-BACK is not a failed takeover, and the summary must not read like one. Every
  // camera reporting "stopped" is the correct outcome of the button the operator just
  // pressed; summarising it as "0 of 1 cameras are recording on the spare", in the amber
  // used for a partial takeover, turns a clean fail-back into an alarm. Found by LOOKING at
  // the Arabic screenshot — every assertion on that run passed.
  const stopped = outcomes.filter((c) => c.outcome === 'stopped').length;
  const handedBack = outcomes.length > 0
    && outcomes.every((c) => c.outcome === 'stopped' || c.outcome === 'stop-failed');
  // The appliance sends a STATE, never a finished sentence; the sentence is composed HERE,
  // in the operator's language. A server-composed one arrives in English on an Arabic
  // screen — the defect W3-4 and W3-6 each shipped once, and this screen pass found again.
  // The DETAIL beside it stays raw: it is a machine's own words about a failure, it cannot
  // be enumerated in advance, and paraphrasing it would help nobody who has to act on it.
  const outcomeText = (c) => {
    if (!c.outcome) return '';
    const said = t(`fo.outcome.${c.outcome}`);
    const label = said === `fo.outcome.${c.outcome}` ? c.outcome : said;
    return c.outcomeDetail ? `${label} — ${c.outcomeDetail}` : label;
  };

  const active = plan.state === 'active';

  return (
    <article className={`fo-card${plan.enabled ? '' : ' fo-card--off'}${active ? ' fo-card--active' : ''}`} data-fo-plan={plan.id}>
      <div className="fo-card-head">
        <h3 className="fo-card-name">{plan.name || view.protectedName || plan.protectedNodeId}</h3>
        <ReadyBadge state={view.readyState} />
        {plan.autoActivate ? <span className="fo-tag fo-tag--auto">{t('fo.tagAuto')}</span> : <span className="fo-tag">{t('fo.tagManual')}</span>}
        {!plan.enabled ? <span className="fo-tag fo-tag--off">{t('fo.tagDisabled')}</span> : null}
      </div>

      <div className="fo-pair">
        <div className="fo-pair-side">
          <span className="fo-pair-role">{t('fo.roleProtected')}</span>
          <span className="fo-pair-name">{view.protectedName || plan.protectedNodeId}</span>
          <NodeStatusPill status={view.protectedStatus} />
        </div>
        <span className="fo-pair-arrow" aria-hidden="true"><Ico n="arr-right" sz={18} /></span>
        <div className="fo-pair-side">
          <span className="fo-pair-role">{t('fo.roleStandby')}</span>
          <span className="fo-pair-name">{view.standbyName || plan.standbyNodeId}</span>
          <NodeStatusPill status={view.standbyStatus} />
        </div>
      </div>

      {/* The sentence the whole screen exists to make honest. Composed here, from the
          state, so it is in the operator's language. */}
      <p className="fo-ready-hint">{t(`fo.readyHint.${READY_ORDER.includes(view.readyState) ? view.readyState : 'untested'}`)}</p>

      {/* WHAT THE SPARE SAID IT CAN CARRY. A drill answers whether the spare can REACH the
          cameras; this answers whether it could encode them, which is a different promise
          and the one W3-7 shipped without. Every number here came from the appliance. */}
      <CapacityLine capacity={view.capacity} />

      {outcomes.length ? (() => {
        const allGood = handedBack ? stopped === outcomes.length : recording === outcomes.length;
        return (
          <p className={`fo-outcome-line${allGood ? ' fo-outcome-line--all' : ''}`}>
            <Ico n={allGood ? 'check-ok' : 'warning'} sz={13} />{' '}
            {handedBack
              ? t('fo.outcomeHandedBack', { ok: stopped, all: outcomes.length })
              : t('fo.outcomeSummary', { ok: recording, all: outcomes.length })}
          </p>
        );
      })() : null}

      {active ? (
        <p className="fo-active-note">
          <Ico n="warning" sz={13} />{' '}
          {view.protectedStatus === 'online' ? t('fo.activeAndBack') : t('fo.activeNote')}
        </p>
      ) : null}

      <div className="fo-facts">
        <span className="fo-fact">
          <Ico n="camera" sz={13} /> {t('fo.factCameras', { n: plan.cameraCount || 0 })}
        </span>
        <span className="fo-fact">
          <Ico n="copy" sz={13} />{' '}
          {plan.lastStagedAt ? t('fo.factStaged', { time: formatTimestamp(plan.lastStagedAt) }) : t('fo.factNeverStaged')}
        </span>
        <span className="fo-fact">
          <Ico n="check-ok" sz={13} />{' '}
          {plan.lastDrillAt
            ? t('fo.factDrilled', { time: formatTimestamp(plan.lastDrillAt), ok: plan.drillReachable || 0, all: plan.drillTotal || 0 })
            : t('fo.factNeverDrilled')}
        </span>
        {plan.activatedAt && active ? (
          <span className="fo-fact">
            <Ico n="zap" sz={13} />{' '}
            {plan.activatedAutomatically
              ? t('fo.factActivatedAuto', { time: formatTimestamp(plan.activatedAt) })
              : t('fo.factActivated', { time: formatTimestamp(plan.activatedAt) })}
          </span>
        ) : null}
      </div>

      {plan.lastStageError ? (
        <p className="fo-stage-error" role="alert">{t('fo.stageError', { reason: plan.lastStageError })}</p>
      ) : null}

      <div className="fo-card-foot">
        <button type="button" className="quiet" onClick={toggleOpen} disabled={busy}>
          <span className="btn-icon"><Ico n={open ? 'chev-up' : 'chev-down'} sz={13} /> {t('fo.showCameras')}</span>
        </button>
        {canWrite ? (
          <span className="fo-card-actions">
            <button type="button" data-fo-act="stage" className="quiet" onClick={() => onAct(plan.id, 'stage')} disabled={busy}>
              <span className="btn-icon"><Ico n="copy" sz={13} /> {t('fo.actStage')}</span>
            </button>
            <button type="button" data-fo-act="drill" onClick={() => onAct(plan.id, 'drill')} disabled={busy || !plan.lastStagedAt}>
              <span className="btn-icon"><Ico n="check-ok" sz={13} /> {t('fo.actDrill')}</span>
            </button>
            {active ? (
              <button type="button" data-fo-act="release" className="quiet" onClick={() => onAct(plan.id, 'release')} disabled={busy}>
                <span className="btn-icon"><Ico n="undo" sz={13} /> {t('fo.actRelease')}</span>
              </button>
            ) : (
              <button type="button" data-fo-act="activate" className="quiet danger-text" onClick={() => onAct(plan.id, 'activate')} disabled={busy || !plan.lastStagedAt}>
                <span className="btn-icon"><Ico n="zap" sz={13} /> {t('fo.actActivate')}</span>
              </button>
            )}
            <button type="button" data-fo-act="edit" className="quiet" onClick={() => onEdit(view)} disabled={busy}>
              <span className="btn-icon"><Ico n="edit-2" sz={13} /> {t('fo.edit')}</span>
            </button>
            <button type="button" data-fo-act="delete" className="quiet danger-text" onClick={() => onDelete(plan.id)} disabled={busy || active}>
              <span className="btn-icon"><Ico n="trash" sz={13} /> {t('fo.delete')}</span>
            </button>
          </span>
        ) : null}
      </div>

      {listOpen ? (
        <div className="fo-cameras">
          {loadingCams ? <p className="settings-hint">{t('fo.loadingCameras')}</p> : null}
          {!loadingCams && (shown || []).length === 0 ? <p className="settings-hint">{t('fo.noCameras')}</p> : null}
          {(shown || []).length ? (
            <table className="fo-camera-table">
              <thead>
                <tr>
                  <th>{t('fo.colCamera')}</th>
                  <th>{t('fo.colAddress')}</th>
                  <th>{t('fo.colDrill')}</th>
                  {outcomes.length ? <th>{t('fo.colOutcome')}</th> : null}
                </tr>
              </thead>
              <tbody>
                {shown.map((c) => (
                  <tr key={`${c.name}-${c.host}`} className={`fo-cam--${c.checkStatus || 'untested'}`}>
                    <td>{c.name}</td>
                    <td>{c.host}</td>
                    <td>
                      {c.checkStatus
                        ? <>{t(`fo.check.${c.checkStatus}`)}{c.checkDetail ? <span className="fo-cam-detail"> — {c.checkDetail}</span> : null}</>
                        : <em>{t('fo.check.untested')}</em>}
                    </td>
                    {outcomes.length ? (
                      <td
                        data-fo-outcome={c.outcome || ''}
                        className={c.outcome === 'recording' ? 'fo-cam-outcome--ok' : 'fo-cam-outcome--bad'}
                      >
                        {outcomeText(c)}
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
        </div>
      ) : null}
    </article>
  );
}

export function FailoverPage({ nodes = [], session, onToast }) {
  const t = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(null);
  const [error, setError] = useState('');
  // Per plan: the per-camera detail the appliance returned from the last action.
  const [results, setResults] = useState({});
  const canWrite = !!session?.isSuperadmin;

  const load = useCallback(async () => {
    setLoading(true);
    const r = await call('/api/failover-plans');
    setLoading(false);
    if (r.ok) setItems(Array.isArray(r.body?.items) ? r.body.items : []);
    else if (onToast) onToast(r.message || t('fo.loadFailed'), 'error');
  }, [onToast, t]);
  useEffect(() => { load(); }, [load]);

  async function save(form) {
    setBusy(true);
    setError('');
    const r = await call('/api/failover-plans', { method: 'POST', body: JSON.stringify(form) });
    setBusy(false);
    if (!r.ok) {
      // The server's refusals are written to be read ("failover does not chain"). Show them.
      setError(r.message || t('fo.saveFailed'));
      return;
    }
    setEditing(null);
    if (onToast) onToast(t('fo.saved'));
    load();
  }

  async function act(id, verb) {
    setBusy(true);
    const r = await call(`/api/failover-plans/${id}/${verb}`, { method: 'POST' });
    setBusy(false);
    // Keep what the appliance reported, per camera. It is a RESULT, not a state — the spare
    // does not store it — so dropping it here means "active" is all an operator ever learns
    // about a takeover. The live bench found exactly that, one layer down.
    setResults((prev) => ({ ...prev, [id]: r.ok && r.body ? (r.body.cameras || []) : [] }));
    if (onToast) onToast(r.ok ? t(`fo.done.${verb}`) : (r.message || t(`fo.failed.${verb}`)), r.ok ? 'info' : 'error');
    load();
  }

  async function remove(id) {
    setBusy(true);
    const r = await call(`/api/failover-plans/${id}`, { method: 'DELETE' });
    setBusy(false);
    if (onToast) onToast(r.ok ? t('fo.deleted') : (r.message || t('fo.deleteFailed')), r.ok ? 'info' : 'error');
    load();
  }

  const counts = useMemo(() => {
    const c = {};
    items.forEach((v) => { c[v.readyState] = (c[v.readyState] || 0) + 1; });
    return c;
  }, [items]);

  // Plans wanting attention first. A screen that opens on the ones that are fine is a
  // screen where the one that is not scrolls off the bottom.
  const ordered = useMemo(() => {
    const rank = (v) => READY_ORDER.indexOf(v.readyState);
    return [...items].sort((a, b) => rank(a) - rank(b));
  }, [items]);

  if (editing) {
    return (
      <>
        <FormBusyOverlay busy={busy} />
        <PlanEditor
          initial={editing}
          nodes={nodes}
          plans={items}
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

      <section className="settings-panel span-two fo-pitch">
        <header>
          {/* The contextual "?" goes on the sentence the whole feature turns on, not on the
              page title: what an operator needs the manual for here is why a copied plan is
              not a tested one. */}
          <h2>
            <span className="btn-icon"><Ico n="shield-check" /> {t('fo.pitchTitle')}</span>
            <HelpButton slug="failover" anchor="tested" />
          </h2>
        </header>
        <p className="fo-pitch-lead">{t('fo.pitchLead')}</p>
        {/* The two things this is NOT. Said on the screen rather than in a manual, because
            the person who needs them is the one reading this at the moment of an outage. */}
        <p className="fo-pitch-limit">{t('fo.pitchNotFootage')}</p>
        <p className="fo-pitch-limit">{t('fo.pitchNotFencing')}</p>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="server" /> {t('fo.plansTitle')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
              <span className="btn-icon"><Ico n="reload" /> {t('fo.refresh')}</span>
            </button>
            {canWrite ? (
              <button type="button" onClick={() => { setError(''); setEditing(blankPlan()); }} disabled={busy}>
                <span className="btn-icon"><Ico n="plus" /> {t('fo.newPlan')}</span>
              </button>
            ) : null}
          </div>
        </header>

        {items.length ? (
          <div className="fo-tiles">
            {READY_ORDER.filter((s) => counts[s]).map((s) => (
              <div key={s} className={`fo-tile fo-tile--${s}${NEEDS_ATTENTION.has(s) ? ' fo-tile--attention' : ''}`}>
                <span className="fo-tile-n">{counts[s]}</span>
                <span className="fo-tile-label">{t(`fo.ready.${s}`)}</span>
              </div>
            ))}
          </div>
        ) : null}

        {!canWrite ? <p className="settings-hint">{t('fo.readOnly')}</p> : null}

        {items.length === 0 ? (
          <p className="settings-hint">{canWrite ? t('fo.empty') : t('fo.emptyReadonly')}</p>
        ) : (
          <div className="fo-list">
            {ordered.map((view) => (
              <PlanCard
                key={view.plan?.id}
                view={view}
                result={results[view.plan?.id]}
                canWrite={canWrite}
                busy={busy}
                onAct={act}
                onEdit={(v) => { setError(''); setEditing(toRequest(v)); }}
                onDelete={remove}
              />
            ))}
          </div>
        )}
      </section>
    </section>
  );
}
