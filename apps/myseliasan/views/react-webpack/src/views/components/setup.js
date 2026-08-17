import { useState, useEffect, useCallback } from 'react';
import { Ico, useT, LanguageDropdown, DeploymentPanel } from '@shared';
import { useManual } from '@shared/Manual';
import { BrandLogo } from './layout';
import { FormBusyOverlay, Message } from './ui';
import { api } from '../lib/helpers';
import { KIND_ORDER, KIND_ICO, normKind, defaultIconFor, hasPlans, multiPlan } from './site_kinds';

// First-run setup wizard for the control plane. myseliasan carries the heaviest setup
// burden in the suite — sign-in, a site to put on the map, an adopted node, and the
// handoff off the bootstrap account — and until now a new install dropped the operator
// on an empty dashboard with none of it done.
//
// Every step is a thin form over an API that ALREADY EXISTS; the wizard adds sequence
// and explanation, not new capability, and every step is skippable because the same
// work can always be done later from the regular pages. Completion is a single
// server-side flag (POST /api/setup/complete), the same contract mymatasan and myidsan
// use — so dismissal is per-install, not per-browser.
//
// Deliberately NOT here:
//   - restore-from-backup: myseliasan has no backup/restore at all (only mymatasan's
//     .mmbackup and myidsan's .idbackup exist). Separate gap, not folded in.
//   - an alerting step: there is no notification-destination API in this app yet; the
//     feed is a live relay of node-pushed events. A form over nothing would be a lie.
const STEP_KEYS = ['setup.stepWelcome', 'setup.stepDeployment', 'setup.stepSignin', 'setup.stepSite', 'setup.stepNode', 'setup.stepHandoff', 'setup.stepDone'];

// Deployment is asked SECOND, before sign-in and before adopting a node, because the
// answer changes both of those. In a clustered install the SSO redirect URL and the
// address a node dials back on must be the load balancer's, not this machine's — and a
// hostname baked in during setup is not something anyone revisits until the second
// instance is added and nothing works.
//
// The operator checklist and the caveats live HERE rather than in the shared component
// because they are facts about myseliasan specifically: its listener ports, its uploads
// directory, its pairing URL, and which of its own features are not yet cluster-safe.
export function deploymentOperatorSteps(t) {
  return [
    t('setup.deployLbHttps'),
    t('setup.deployLbPassthrough'),
    t('setup.deployKeyFile'),
    t('setup.deployFloorplans'),
    t('setup.deployMediaIps'),
    t('setup.deployVipUrls'),
  ];
}

// What is not yet safe to replicate. Every one of these fails SILENTLY — wrong numbers on
// a chart, an alert that never arrives, a picture that never loads — so they are spelled
// out and acknowledged rather than left in a release note nobody reads at 2am.
//
// This list has to SHRINK as the gaps close, and shrink promptly. A caveat that is no
// longer true is worse than no caveat at all: it teaches an operator that the warnings
// here are stale, and the next time one of them matters they will skim past it.
//
// Removed as each gap closed: node liveness and node-screen access (the owner registry and
// instance forwarding, node_owner.go / node_peer.go); the live notification bell and
// cross-instance fleet rules (the event bus, node_events.go); live camera video (media
// offer forwarding, media_peer.go). What is left is the one thing still genuinely
// per-instance.
export function deploymentCaveats(t) {
  return [t('setup.deployCaveatSettings')];
}

// The manual article behind the wizard's "?", with one anchor per step, positionally
// aligned with STEP_KEYS above. Written out as a literal list rather than derived, so a
// source scan can find and verify every deep link — see apps/myseliasan/manual/manual_test.go.
const SETUP_STEP_HELP = {
  slug: 'setup-wizard',
  anchors: ['welcome', 'signin', 'site', 'node', 'handoff', 'done'],
};

const SSO_BUNDLE_KIND = 'myidsan.sso.client';
const SSO_BUNDLE_MAX_VERSION = 1;
// Bundle leaf -> the leaf's name inside the settings API's `sso` section payload.
// Anything absent from the file is left alone rather than blanked, so a partial
// bundle cannot silently wipe working config. Mirrors settings.js's importer.
const SSO_BUNDLE_FIELDS = [
  'issuer', 'audience', 'clientId', 'clientSecret',
  'providerBaseUrl', 'redirectBaseUrl', 'redirectPath', 'sessionTtlSeconds',
];

export function SetupWizard({ session, lang, onLangChange, onToast, onDone }) {
  const t = useT();
  const manual = useManual();
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  // Per-step "this actually got done" flags, summarised on the final pane.
  // deployMode is also read by the later steps: a clustered install needs the load
  // balancer's address in the SSO redirect and the node's dial-back URL, not this host's.
  const [deployMode, setDeployMode] = useState('standalone');
  const [ssoDone, setSsoDone] = useState(false);
  const [ssoRestartNeeded, setSsoRestartNeeded] = useState(false);
  const [siteDone, setSiteDone] = useState('');
  const [nodeDone, setNodeDone] = useState('');
  const [handoffDone, setHandoffDone] = useState('');

  const next = () => { setMessage(''); setStep((s) => Math.min(STEP_KEYS.length - 1, s + 1)); };
  const back = () => { setMessage(''); setStep((s) => Math.max(0, s - 1)); };

  const finish = useCallback(async () => {
    setBusy(true);
    const r = await api('/api/setup/complete', { method: 'POST', noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) { setMessage(r.message || t('setup.finishFailed')); return; }
    onDone();
  }, [onDone, t]);

  return (
    <main className="login-screen">
      <div className="setup-wizard">
        <FormBusyOverlay busy={busy} />
        <div className="setup-head">
          <BrandLogo size={44} />
          <div className="setup-head-actions">
            <button
              type="button"
              className="login-help"
              onClick={() => manual.openHelp(SETUP_STEP_HELP.slug, SETUP_STEP_HELP.anchors[step])}
            >
              <Ico n="help" sz={15} />
              <span>{t('help.link')}</span>
            </button>
            <LanguageDropdown lang={lang} onLang={onLangChange} />
            <button type="button" className="quiet setup-skip" onClick={finish} disabled={busy}>
              {t('setup.skip')}
            </button>
          </div>
        </div>

        <ol className="setup-steps" aria-label={t('setup.progressAria')}>
          {STEP_KEYS.map((key, i) => (
            <li key={key} className={`setup-step-dot${i === step ? ' active' : ''}${i < step ? ' done' : ''}`}>
              <span className="setup-step-num">{i < step ? '✓' : i + 1}</span>
              <span className="setup-step-label">{t(key)}</span>
            </li>
          ))}
        </ol>

        <div className="setup-body">
          {step === 0 ? <WelcomeStep session={session} /> : null}
          {step === 1 ? <DeploymentStep onToast={onToast} mode={deployMode} setMode={setDeployMode} /> : null}
          {step === 2 ? (
            <SigninStep
              busy={busy} setBusy={setBusy} setMessage={setMessage} onToast={onToast}
              done={ssoDone} setDone={setSsoDone} setRestartNeeded={setSsoRestartNeeded}
              clustered={deployMode === 'clustered'}
            />
          ) : null}
          {step === 3 ? (
            <SiteStep busy={busy} setBusy={setBusy} setMessage={setMessage} onToast={onToast} done={siteDone} setDone={setSiteDone} />
          ) : null}
          {step === 4 ? (
            <NodeStep busy={busy} setBusy={setBusy} setMessage={setMessage} onToast={onToast} done={nodeDone} setDone={setNodeDone} clustered={deployMode === 'clustered'} />
          ) : null}
          {step === 5 ? (
            <HandoffStep busy={busy} setBusy={setBusy} setMessage={setMessage} onToast={onToast} session={session} done={handoffDone} setDone={setHandoffDone} />
          ) : null}
          {step === 6 ? (
            <DoneStep
              ssoDone={ssoDone} ssoRestartNeeded={ssoRestartNeeded}
              siteDone={siteDone} nodeDone={nodeDone} handoffDone={handoffDone}
              deployMode={deployMode}
            />
          ) : null}
        </div>

        <Message value={message} />

        <div className="setup-nav">
          {step > 0 ? (
            <button type="button" className="quiet" onClick={back} disabled={busy}>
              <span className="btn-icon"><Ico n="arr-left" /> {t('setup.back')}</span>
            </button>
          ) : <span />}
          {step < STEP_KEYS.length - 1 ? (
            <button type="button" onClick={next} disabled={busy}>
              <span className="btn-icon">{t('setup.next')} <Ico n="arr-right" /></span>
            </button>
          ) : (
            <button type="button" onClick={finish} disabled={busy}>
              <span className="btn-icon"><Ico n="check-ok" /> {t('setup.finish')}</span>
            </button>
          )}
        </div>
      </div>
    </main>
  );
}

// Step 1 — orientation. The bootstrap password was already forced through the
// must-change screen before this wizard can render, so there is nothing to collect
// here; this pane explains what the control plane is and what the next steps do.
function WelcomeStep({ session }) {
  const t = useT();
  return (
    <div className="setup-pane">
      <h2>{t('setup.welcomeTitle')}</h2>
      <p>{t('setup.welcomeBody')}</p>
      <div className="setup-account">
        <span className="field-hint"><Ico n="user" sz={16} /> {t('setup.welcomeSignedIn', { who: session?.email || session?.name || '—' })}</span>
        <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.welcomePasswordSet')}</span>
      </div>
      <ul className="setup-summary">
        <li>· {t('setup.stepSignin')} — {t('setup.welcomeLiSignin')}</li>
        <li>· {t('setup.stepSite')} — {t('setup.welcomeLiSite')}</li>
        <li>· {t('setup.stepNode')} — {t('setup.welcomeLiNode')}</li>
        <li>· {t('setup.stepHandoff')} — {t('setup.welcomeLiHandoff')}</li>
      </ul>
      <p className="field-hint">{t('setup.welcomeSkippable')}</p>
    </div>
  );
}

// Step 2 — deployment shape. The one question in this wizard whose answer is mostly
// about the world OUTSIDE the application: a load balancer's configuration, where the
// encryption key file lives, which addresses the nodes will dial. The panel reports what
// the server can actually verify from in here and states plainly what it cannot.
//
// Skippable like every other step, and it defaults to single instance — which is what
// every install was before this existed.
function DeploymentStep({ onToast, mode, setMode }) {
  const t = useT();
  return (
    <div className="setup-pane">
      <h2>{t('setup.deploymentTitle')}</h2>
      <p>{t('setup.deploymentBody')}</p>
      <DeploymentPanel
        api={api}
        appLabel="MySeliaSan"
        operatorSteps={deploymentOperatorSteps(t)}
        caveats={deploymentCaveats(t)}
        labels={{ llmMode: 'setup.deployCheckLlm' }}
        onToast={onToast}
        onSaved={setMode}
      />
      {mode === 'clustered' ? <p className="field-hint">{t('setup.deploymentNextHint')}</p> : null}
    </div>
  );
}

// Step 3 — sign-in. myidsan's Apps page exports the client it just registered as a
// small JSON file; every value in it has to match here byte-for-byte, and retyping it
// across two consoles is exactly where operators slip. Unlike the Settings page (which
// only FILLS the form and waits for a Save), the wizard saves — that is the whole point
// of being here — and then reports that the change needs a restart.
function SigninStep({ busy, setBusy, setMessage, onToast, done, setDone, setRestartNeeded, clustered }) {
  const t = useT();

  const applyBundle = async (file) => {
    if (!file) return;
    setMessage('');
    let bundle;
    try { bundle = JSON.parse(await file.text()); } catch (_) { setMessage(t('setup.ssoBadJson')); return; }
    if (!bundle || bundle.kind !== SSO_BUNDLE_KIND) { setMessage(t('setup.ssoWrongKind')); return; }
    // A newer major bundle may carry fields this build cannot honour; refuse rather
    // than import a subset and let the operator believe it is complete.
    if (Number(bundle.version) > SSO_BUNDLE_MAX_VERSION) { setMessage(t('setup.ssoTooNew')); return; }

    setBusy(true);
    try {
      // Read the live section first and merge onto it: PUT replaces the section, so
      // sending only the bundle's leaves would blank every field it does not carry.
      // The section payload is keyed by section name (values.sso is {sso: {...}}), and
      // the editable leaves live one level down — writing at the wrapper level would
      // save cleanly and change nothing.
      const cur = await api('/api/settings', { noRedirect: true });
      const payload = cur.ok && cur.body && cur.body.values ? cur.body.values.sso : null;
      if (!payload || !payload.sso) throw new Error(cur.message || t('setup.ssoLoadFailed'));
      const next = JSON.parse(JSON.stringify(payload));
      const src = bundle.sso || {};
      const applied = [];
      for (const leaf of SSO_BUNDLE_FIELDS) {
        const value = src[leaf];
        if (value === undefined || value === null || value === '') continue;
        next.sso[leaf] = value;
        applied.push(leaf);
      }
      if (!applied.length) { setMessage(t('setup.ssoNoFields')); return; }
      // clientSecret is masked to "" on read and the server keeps the stored value when
      // it is sent blank, so a bundle without a secret cannot wipe a working one.
      const r = await api('/api/settings/sso', { method: 'PUT', body: JSON.stringify(next), noRedirect: true });
      if (!r.ok) throw new Error(r.message || t('setup.ssoSaveFailed'));
      if (r.body && r.body.needsRestart) setRestartNeeded(true);
      setDone(true);
      // The secret is the one field myidsan can only hand over at generation time, so
      // say plainly when the bundle did not carry one and the old value still stands.
      onToast && onToast(
        applied.includes('clientSecret') ? t('setup.ssoImportedWithSecret', { n: applied.length }) : t('setup.ssoImportedNoSecret', { n: applied.length }),
        applied.includes('clientSecret') ? 'success' : 'info',
      );
    } catch (err) {
      setMessage(err.message || t('setup.ssoSaveFailed'));
    } finally { setBusy(false); }
  };

  return (
    <div className="setup-pane">
      <h2>{t('setup.signinTitle')}</h2>
      <p>{t('setup.signinBody')}</p>
      {done ? (
        <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.ssoSaved')}</span>
      ) : (
        <label className="setup-field">
          <span>{t('setup.ssoFile')}</span>
          <input type="file" accept=".json" disabled={busy} onChange={(e) => applyBundle(e.target.files && e.target.files[0])} />
        </label>
      )}
      {/* A clustered install must send users back to the load balancer, not to whichever
          instance happened to serve the wizard — a per-instance redirect URL works
          perfectly until the second instance exists. */}
      {clustered ? <p className="field-hint"><Ico n="info" sz={15} /> {t('setup.signinClusterHint')}</p> : null}
      <p className="field-hint">{t('setup.signinLocalHint')}</p>
    </div>
  );
}

// Step 3 — the first site, so the fleet map is not an empty world. Same two calls the
// map's own asset wizard makes: create the site, then its areas in order.
function SiteStep({ busy, setBusy, setMessage, onToast, done, setDone }) {
  const t = useT();
  const [name, setName] = useState('');
  const [kind, setKind] = useState(KIND_ORDER[0]);

  const create = async () => {
    const trimmed = name.trim();
    if (!trimmed) { setMessage(t('setup.siteNameRequired')); return; }
    setBusy(true);
    setMessage('');
    try {
      const icon = defaultIconFor(kind);
      const r = await api('/api/sites', { method: 'POST', body: JSON.stringify({ name: trimmed, icon, kind: normKind(kind) }), noRedirect: true });
      if (!r.ok || !r.body || !r.body.id) throw new Error(r.message || t('setup.siteFailed'));
      // A point asset has no surface to author, so it gets no areas at all.
      if (hasPlans(kind)) {
        const area = multiPlan(kind) ? t('bld.areaMain') : t('bld.areaGrounds');
        await api(`/api/sites/${r.body.id}/areas`, { method: 'POST', body: JSON.stringify({ name: area, ordinal: 0 }), noRedirect: true });
      }
      setDone(trimmed);
      onToast && onToast(t('setup.siteCreated', { name: trimmed }), 'success');
    } catch (err) {
      setMessage(err.message || t('setup.siteFailed'));
    } finally { setBusy(false); }
  };

  return (
    <div className="setup-pane">
      <h2>{t('setup.siteTitle')}</h2>
      <p>{t('setup.siteBody')}</p>
      {done ? (
        <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.siteCreated', { name: done })}</span>
      ) : (
        <>
          <label className="setup-field">
            <span>{t('setup.siteName')}</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('setup.siteNamePlaceholder')} disabled={busy} />
          </label>
          <div className="setup-field">
            <span>{t('bld.kindQuestion')}</span>
            <div className="bld-choice bld-choice-3" role="radiogroup" aria-label={t('bld.kindQuestion')}>
              {KIND_ORDER.map((k) => (
                <button
                  key={k}
                  type="button"
                  className={`bld-choice-opt${kind === k ? ' active' : ''}`}
                  role="radio"
                  aria-checked={kind === k}
                  onClick={() => setKind(k)}
                  disabled={busy}
                >
                  <Ico n={KIND_ICO[k]} sz={15} />
                  <span className="bld-choice-t">{t(`bld.kind.${k}`)}</span>
                </button>
              ))}
            </div>
          </div>
          <div className="setup-actions">
            <button type="button" onClick={create} disabled={busy || !name.trim()}>
              <span className="btn-icon"><Ico n="plus" /> {t('setup.siteCreate')}</span>
            </button>
          </div>
        </>
      )}
      <p className="field-hint">{t('setup.sitePlaceLater')}</p>
    </div>
  );
}

// Step 4 — adopt the first node. The fleet key has to be on the node before it will
// answer, so it is shown FIRST and the scan comes after; a node that was never given
// the key simply will not appear, and saying so here saves a support round-trip.
function NodeStep({ busy, setBusy, setMessage, onToast, done, setDone, clustered }) {
  const t = useT();
  const [fleetKey, setFleetKey] = useState(null);
  const [found, setFound] = useState(null);
  const [form, setForm] = useState(null); // { ip, httpsPort, claimCode, name }

  useEffect(() => {
    api('/api/nodes/fleet-key', { noRedirect: true })
      .then((r) => { if (r.ok) setFleetKey(r.body); })
      .catch(() => { /* the pane shows a hint when the key is unavailable */ });
  }, []);

  const scan = async () => {
    setBusy(true);
    setMessage('');
    const r = await api('/api/nodes/scan', { method: 'POST', body: JSON.stringify({ timeoutMs: 4000 }), noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) { setMessage(r.message || t('setup.nodeScanFailed')); return; }
    setFound(Array.isArray(r.body) ? r.body : []);
  };

  const adopt = async () => {
    if (!form) return;
    const ip = (form.ip || '').trim();
    const code = (form.claimCode || '').trim();
    if (!ip || !code) { setMessage(t('setup.nodeNeedsCode')); return; }
    setBusy(true);
    setMessage('');
    const r = await api('/api/nodes/adopt', {
      method: 'POST',
      noRedirect: true,
      body: JSON.stringify({
        ip,
        httpsPort: Number(form.httpsPort) || 0,
        claimCode: code,
        name: (form.name || '').trim(),
        description: '',
        icon: 'monitor',
      }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) { setMessage(r.message || t('setup.nodeAdoptFailed')); return; }
    setDone((form.name || '').trim() || ip);
    setForm(null);
    onToast && onToast(t('setup.nodeAdopted'), 'success');
  };

  return (
    <div className="setup-pane">
      <h2>{t('setup.nodeTitle')}</h2>
      <p>{t('setup.nodeBody')}</p>

      {/* GET /nodes/fleet-key answers {fleetKey, set} — `set` is false on a fresh install. */}
      {fleetKey && fleetKey.set && fleetKey.fleetKey ? (
        <div className="setup-toggle-row">
          <div>
            <strong>{t('setup.nodeFleetKey')}</strong>
            <code>{fleetKey.fleetKey}</code>
          </div>
          <button type="button" className="quiet" onClick={() => navigator.clipboard && navigator.clipboard.writeText(fleetKey.fleetKey)} disabled={busy}>
            <span className="btn-icon"><Ico n="copy" /> {t('setup.nodeCopyKey')}</span>
          </button>
        </div>
      ) : (
        <p className="field-hint">{t('setup.nodeNoFleetKey')}</p>
      )}

      {/* A node dials the parent back and holds that connection open. Pointed at one
          instance it stays pinned to it; pointed at the load balancer it can reconnect
          to whichever instance is alive. */}
      {clustered ? <p className="field-hint"><Ico n="info" sz={15} /> {t('setup.nodeClusterHint')}</p> : null}

      {done ? (
        <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.nodeAdoptedName', { name: done })}</span>
      ) : (
        <>
          <div className="setup-actions">
            <button type="button" onClick={scan} disabled={busy}>
              <span className="btn-icon"><Ico n="search" /> {t('setup.nodeScan')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => setForm({ ip: '', httpsPort: '', claimCode: '', name: '' })} disabled={busy}>
              {t('setup.nodeManual')}
            </button>
          </div>

          {found && !found.length ? <p className="field-hint">{t('setup.nodeNoneFound')}</p> : null}
          {found && found.length ? (
            <ul className="setup-found-list">
              {found.map((n) => (
                <li key={`${n.ip}:${n.httpsPort}`} className="setup-found-row-wrap">
                  <div className="setup-found-row">
                    <span className="setup-found-name">
                      <strong>{n.name || n.ip}</strong>
                      <span className="field-hint">{n.ip}{n.httpsPort ? `:${n.httpsPort}` : ''}</span>
                    </span>
                    <button
                      type="button"
                      className="quiet"
                      disabled={busy}
                      onClick={() => setForm({ ip: n.ip || '', httpsPort: String(n.httpsPort || ''), claimCode: '', name: n.name || '' })}
                    >
                      {t('setup.nodeSelect')}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          ) : null}

          {form ? (
            <div className="setup-cred-form">
              <label className="setup-field">
                <span>{t('setup.nodeAddress')}</span>
                <input value={form.ip} onChange={(e) => setForm({ ...form, ip: e.target.value })} placeholder="192.168.1.20" disabled={busy} />
              </label>
              <label className="setup-field">
                <span>{t('setup.nodePort')}</span>
                <input value={form.httpsPort} onChange={(e) => setForm({ ...form, httpsPort: e.target.value })} placeholder="4000" disabled={busy} />
              </label>
              <label className="setup-field">
                <span>{t('setup.nodeClaimCode')}</span>
                <input value={form.claimCode} onChange={(e) => setForm({ ...form, claimCode: e.target.value })} disabled={busy} />
              </label>
              <label className="setup-field">
                <span>{t('setup.nodeName')}</span>
                <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} disabled={busy} />
              </label>
              <p className="field-hint">{t('setup.nodeClaimHint')}</p>
              <button type="button" className="setup-cred-add" onClick={adopt} disabled={busy}>
                <span className="btn-icon"><Ico n="plus" /> {t('setup.nodeAdopt')}</span>
              </button>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}

// Step 5 — get off the bootstrap account. myseliasan has no user-creation API on
// purpose: real accounts arrive by signing in (through myidsan SSO, or local auth), and
// a superadmin then elevates one. So this pane lists who has actually signed in and
// offers the elevate — it cannot invent an account that has never logged in.
function HandoffStep({ busy, setBusy, setMessage, onToast, session, done, setDone }) {
  const t = useT();
  const [users, setUsers] = useState(null);
  const [roles, setRoles] = useState([]);

  // A user row carries roleId, NOT an isSuperadmin flag, so the roles table is what
  // says which role is the superadmin one — same pairing the Users page makes.
  const load = useCallback(async () => {
    const [u, r] = await Promise.all([
      api('/api/rbac/users', { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    if (r.ok) setRoles(Array.isArray(r.body) ? r.body : []);
    setUsers(u.ok && Array.isArray(u.body) ? u.body : []);
  }, []);
  useEffect(() => { load(); }, [load]);

  const userLabel = (u) => u.email || u.username || u.name || `user ${u.id}`;
  const roleOf = (u) => roles.find((r) => Number(r.id) === Number(u.roleId));

  const elevate = async (user) => {
    const label = userLabel(user);
    setBusy(true);
    setMessage('');
    const r = await api(`/api/rbac/users/${user.id}/elevate`, { method: 'POST', noRedirect: true }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) { setMessage(r.message || t('setup.handoffFailed')); return; }
    setDone(label);
    onToast && onToast((r.body && r.body.warning) || t('setup.handoffGranted', { who: label }), 'success');
    load();
  };

  // Anyone who is not this bootstrap session, is not the stock account, is not already
  // a superadmin, and is not disabled. Elevating the account you are already signed in
  // as, or the stock one you are trying to retire, would defeat the whole step.
  const candidates = (users || []).filter((u) => (
    Number(u.id) !== Number(session?.userId)
    && !u.isStock
    && !u.disabled
    && !(roleOf(u) || {}).isSuperadmin
  ));

  return (
    <div className="setup-pane">
      <h2>{t('setup.handoffTitle')}</h2>
      <p>{t('setup.handoffBody')}</p>
      {done ? (
        <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.handoffGranted', { who: done })}</span>
      ) : users === null ? (
        <p className="field-hint">{t('common.loading')}</p>
      ) : candidates.length ? (
        <ul className="setup-found-list">
          {candidates.map((u) => (
            <li key={u.id} className="setup-found-row-wrap">
              <div className="setup-found-row">
                <span className="setup-found-name">
                  <strong>{userLabel(u)}</strong>
                  <span className="field-hint">{(roleOf(u) || {}).name || t('rb.noRolePending')}</span>
                </span>
                <button type="button" className="quiet" onClick={() => elevate(u)} disabled={busy}>
                  {t('setup.handoffElevate')}
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className="field-hint">{t('setup.handoffNobody')}</p>
      )}
    </div>
  );
}

function DoneStep({ ssoDone, ssoRestartNeeded, siteDone, nodeDone, handoffDone, deployMode }) {
  const t = useT();
  const clustered = deployMode === 'clustered';
  return (
    <div className="setup-pane setup-done">
      <Ico n="check-ok" sz={40} />
      <h2>{t('setup.doneTitle')}</h2>
      <p>{t('setup.doneBody')}</p>
      <ul className="setup-summary">
        <li>{clustered ? '✓' : '·'} {clustered ? t('setup.sumDeployClustered') : t('setup.sumDeployStandalone')}</li>
        <li>{ssoDone ? '✓' : '·'} {t('setup.sumSignin')}</li>
        <li>{siteDone ? '✓' : '·'} {t('setup.sumSite')}</li>
        <li>{nodeDone ? '✓' : '·'} {t('setup.sumNode')}</li>
        <li>{handoffDone ? '✓' : '·'} {t('setup.sumHandoff')}</li>
      </ul>
      {/* The remaining work for a clustered install is on the OTHER instances, and it is
          the step most likely to be forgotten once this one looks finished. */}
      {clustered ? <p className="field-hint">{t('setup.doneClusterHint')}</p> : null}
      {ssoRestartNeeded ? <p className="field-hint">{t('setup.doneRestartHint')}</p> : null}
    </div>
  );
}

export default SetupWizard;
