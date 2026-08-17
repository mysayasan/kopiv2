import { useCallback, useEffect, useState } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/deployment.css';

// The shared deployment-mode UI: "can this install run behind a load balancer, and is
// this instance actually set up for it?".
//
// It exists because most of the answer is NOT in the product. Pointing the cache and the
// lock at Redis is the easy half; the half that silently ruins a deployment is outside —
// a load balancer terminating TLS on the ports where a node's client certificate IS the
// authentication, an encryption key that differs between instances so one cannot read
// what the other sealed, a signing secret each replica invented for itself. None of those
// announce themselves. They present as flaky sign-ins, or as a fleet that looks healthy
// until something reads a row.
//
// So the panel does two things the server cannot do alone: it reports what it CAN verify
// from inside (with the values an operator needs to compare between instances), and it
// lists plainly what only a person can check. Each app passes its own `api` function —
// their request helpers differ — and its own operator steps, because "which ports must be
// L4 passthrough" is a fact about that app, not about deployment in general.

// Reason codes the server returns for an app that cannot be replicated. Mapped here to
// translated sentences; the server deliberately sends a code, not prose, because the UI
// ships in four languages.
const APPLIANCE_REASONS = {
  'serial-bus': 'deploy.appliance.serialBus',
  'local-media': 'deploy.appliance.localMedia',
};

// Default translation keys for the server-verified rows, by check id. An app can add or
// override entries (myseliasan's LLM row) via the `labels` prop.
const CHECK_LABELS = {
  dbEngine: 'deploy.check.dbEngine',
  sharedCache: 'deploy.check.sharedCache',
  sharedLock: 'deploy.check.sharedLock',
  atrestKey: 'deploy.check.atrestKey',
  jwtSecret: 'deploy.check.jwtSecret',
  dbPool: 'deploy.check.dbPool',
};

// DeploymentPanel is the whole feature: the mode question, the verified checklist, the
// operator checklist, and (before the fleet legs are cluster-safe) the caveats an
// operator has to accept before declaring a clustered install.
//
// Used unchanged by the first-run wizard and by Settings — the wizard supplies onSaved
// so it can advance, Settings does not.
export function DeploymentPanel({
  api,
  appLabel,
  operatorSteps = [],
  caveats = [],
  labels = {},
  readOnly = false,
  onToast,
  onSaved,
}) {
  const t = useT();
  const [report, setReport] = useState(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [mode, setMode] = useState(null);
  const [acked, setAcked] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const r = await api('/api/deployment/preflight', { noRedirect: true }).catch(() => ({ ok: false }));
    if (!r || !r.ok || !r.body) { setLoadFailed(true); return; }
    setReport(r.body);
    setMode(r.body.mode || 'standalone');
    // Seeded from what was stored, so somebody returning to this screen sees the
    // acceptance they already gave rather than an empty box implying they never did.
    setAcked(!!r.body.acknowledged);
  }, [api]);

  useEffect(() => { load(); }, [load]);

  const save = useCallback(async (nextMode) => {
    setBusy(true);
    const r = await api('/api/deployment/mode', {
      method: 'POST',
      noRedirect: true,
      body: JSON.stringify({ mode: nextMode, acknowledged: nextMode === 'clustered' ? acked : false }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r || !r.ok) {
      onToast && onToast((r && r.message) || t('deploy.saveFailed'), 'error');
      return;
    }
    onToast && onToast(t('deploy.saved'), 'success');
    await load();
    onSaved && onSaved(nextMode);
  }, [acked, api, load, onSaved, onToast, t]);

  if (loadFailed) {
    return <p className="dep-hint dep-hint--error">{t('deploy.loadFailed')}</p>;
  }
  if (!report) {
    return <div className="dep-loading"><Ico n="reload" sz={16} /><span>{t('common.loading')}</span></div>;
  }

  // An appliance is not a choice to make, so it is not rendered as one. Saying WHY —
  // that the hardware cannot be shared — is the whole content, because an operator who
  // does not know that will keep looking for the setting.
  if (!report.clusterable) {
    const reasonKey = APPLIANCE_REASONS[report.applianceReason];
    return (
      <section className="dep-appliance">
        <h3><span className="dep-inline"><Ico n="server" sz={15} /> {t('deploy.appliance.title', { app: appLabel })}</span></h3>
        <p>{reasonKey ? t(reasonKey) : t('deploy.appliance.generic')}</p>
        <p className="dep-hint">{t('deploy.appliance.advice')}</p>
      </section>
    );
  }

  const clustered = mode === 'clustered';
  const ackSatisfied = !clustered || caveats.length === 0 || acked;
  const needsAck = caveats.length > 0 && clustered && !acked;
  // Saving is offered when EITHER the mode or the acknowledgment differs from what is
  // stored. Requiring a mode change meant an install that was already clustered could
  // never record a fresh acknowledgment — so when the caveats change under an operator,
  // the one action the screen asks them to take was the one it refused to accept.
  const dirty = mode !== report.mode || (clustered && acked !== !!report.acknowledged);
  const canSave = !readOnly && dirty && ackSatisfied;

  return (
    <section className="dep-panel">
      <div className="dep-choice" role="radiogroup" aria-label={t('deploy.modeLegend')}>
        <ModeCard
          selected={!clustered}
          disabled={readOnly || busy}
          icon="server"
          title={t('deploy.mode.standalone')}
          body={t('deploy.mode.standaloneBody')}
          onSelect={() => setMode('standalone')}
        />
        <ModeCard
          selected={clustered}
          disabled={readOnly || busy}
          icon="layers"
          title={t('deploy.mode.clustered')}
          body={t('deploy.mode.clusteredBody')}
          onSelect={() => setMode('clustered')}
        />
      </div>

      {clustered ? (
        <>
          <Checklist report={report} labels={{ ...CHECK_LABELS, ...labels }} t={t} />
          {operatorSteps.length > 0 ? <OperatorSteps steps={operatorSteps} t={t} /> : null}
          {caveats.length > 0 ? (
            <Caveats caveats={caveats} acked={acked} onAck={setAcked} disabled={readOnly || busy} t={t} />
          ) : null}
        </>
      ) : (
        <p className="dep-hint">{t('deploy.standaloneHint')}</p>
      )}

      {readOnly ? null : (
        <div className="dep-actions">
          <button type="button" className="dep-btn" disabled={!canSave || busy} onClick={() => save(mode)}>
            {busy ? t('common.saving') : t('deploy.save')}
          </button>
          {needsAck && !acked ? <span className="dep-hint">{t('deploy.ackRequired')}</span> : null}
        </div>
      )}
    </section>
  );
}

function ModeCard({ selected, disabled, icon, title, body, onSelect }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      className={`dep-card${selected ? ' selected' : ''}`}
      disabled={disabled}
      onClick={onSelect}
    >
      <span className="dep-card-head"><Ico n={icon} sz={16} /> {title}</span>
      <span className="dep-card-body">{body}</span>
    </button>
  );
}

// Checklist renders what the server could verify from inside this instance.
//
// The `detail` is shown verbatim next to every row, and for two of them it is the entire
// point: the at-rest key fingerprint and the resolved connection budget are values an
// operator COMPARES between instances. A row that only said "ok" would hide the one
// number they need.
function Checklist({ report, labels, t }) {
  const rows = report.checks || [];
  return (
    <div className="dep-block">
      <h4>
        <span className="dep-inline">
          <Ico n={report.ready ? 'acknowledge' : 'warning'} sz={15} />
          {report.ready ? t('deploy.checksReady') : t('deploy.checksNotReady')}
        </span>
      </h4>
      <ul className="dep-checks">
        {rows.map((c) => {
          const key = labels[c.id];
          return (
            <li key={c.id} className={`dep-check${c.ok ? ' ok' : ` bad ${c.severity}`}`}>
              <span className="dep-check-mark" aria-hidden="true">
                <Ico n={c.ok ? 'acknowledge' : c.severity === 'warning' ? 'info' : 'x'} sz={14} />
              </span>
              <span className="dep-check-text">
                <span className="dep-check-label">{key ? t(key) : c.id}</span>
                {c.detail ? <code className="dep-check-detail">{c.detail}</code> : null}
                {!c.ok && key ? <span className="dep-check-fix">{t(`${key}Fix`)}</span> : null}
              </span>
              {c.severity === 'warning' && !c.ok ? (
                <span className="dep-badge">{t('deploy.warning')}</span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// OperatorSteps are the things no endpoint can confirm. They are rendered as plain text
// rather than tickboxes on purpose: a tickbox invites clicking through, and the server
// has no way to know whether any of this was actually done.
function OperatorSteps({ steps, t }) {
  return (
    <div className="dep-block">
      <h4><span className="dep-inline"><Ico n="list" sz={15} /> {t('deploy.operatorTitle')}</span></h4>
      <p className="dep-hint">{t('deploy.operatorBody')}</p>
      <ul className="dep-steps">
        {steps.map((s, i) => <li key={i}>{s}</li>)}
      </ul>
    </div>
  );
}

// Caveats list what is not yet safe to replicate. It is an explicit acknowledgment
// rather than a note, because these are silent failures — an operator who skims past
// them finds out from wrong numbers or a missed alert, weeks later.
function Caveats({ caveats, acked, onAck, disabled, t }) {
  return (
    <div className="dep-block dep-caveats">
      <h4><span className="dep-inline"><Ico n="warning" sz={15} /> {t('deploy.caveatsTitle')}</span></h4>
      <p className="dep-hint">{t('deploy.caveatsBody')}</p>
      <ul className="dep-steps">
        {caveats.map((c, i) => <li key={i}>{c}</li>)}
      </ul>
      <label className="dep-ack">
        <input type="checkbox" checked={acked} disabled={disabled} onChange={(e) => onAck(e.target.checked)} />
        <span>{t('deploy.ackLabel')}</span>
      </label>
    </div>
  );
}
