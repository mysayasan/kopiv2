import { useState, useEffect } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { LanguageDropdown } from '@shared/LanguageDropdown';
import { BrandLogo, PasswordField } from './layout';
import { Message, FormBusyOverlay } from './ui';
import { cameraTitle, sameCamera, apiBase } from '../lib/helpers';
import { CapacityRetentionNote } from './settings';
import { defaultDestination } from '../lib/constants';

const STEP_KEYS = ['setup.stepWelcome', 'setup.stepSystem', 'setup.stepAi', 'setup.stepCapacity', 'setup.stepCameras', 'setup.stepRecording', 'setup.stepAlerts', 'setup.stepConnectivity', 'setup.stepDone'];
const capacityLimitKey = { cpu: 'setup.limitCpu', gpu: 'setup.limitGpu', disk: 'setup.limitDisk', memory: 'setup.limitMemory' };

// SetupWizard is the first-run onboarding overlay: welcome/account, machine
// capacity, add a camera, enable recording + person alerts, then finish. It is
// purely a thin orchestrator over the app's existing handlers, passed as props.
export function SetupWizard({
  username,
  authHeader,
  lang,
  onLangChange,
  busy,
  message,
  capacity,
  saved,
  discovered,
  onChangePassword,
  onEstimateCapacity,
  onCalibrateCapacity,
  onScan,
  onAddCamera,
  onEnableRecordingAll,
  onAddPersonRuleAll,
  onRestart,
  onFinish,
  onFfmpegStatus,
  onInstallFfmpeg,
  onAiCapability,
  onInstallAiDeps,
  onStockModel,
  onApplyStockModel,
  onSystemTime,
  onDiskInfo,
  onAddDestination,
}) {
  // Resume the wizard's step ONLY across an in-app restart the wizard itself
  // triggered (installing ffmpeg / GPU deps sets the one-shot `setupResume` flag just
  // before restarting). A plain first-run load — including a fresh install or reset in
  // a browser that still holds a stale `setupStep` — starts at Welcome, not mid-wizard.
  const [step, setStepState] = useState(() => {
    const resume = window.localStorage.getItem('setupResume') === '1';
    const saved = Number(window.localStorage.getItem('setupStep'));
    try { window.localStorage.removeItem('setupResume'); } catch (_) {}
    return resume && Number.isInteger(saved) && saved > 0 && saved < STEP_KEYS.length ? saved : 0;
  });
  const setStep = (updater) => setStepState((s) => {
    const nextStep = typeof updater === 'function' ? updater(s) : updater;
    try { window.localStorage.setItem('setupStep', String(nextStep)); } catch (_) {}
    return nextStep;
  });
  // Mark that the next load should resume at the current step, then restart. Used by
  // the steps whose install actions require an in-app restart to take effect.
  const restartAndResume = () => {
    try { window.localStorage.setItem('setupResume', '1'); } catch (_) {}
    if (onRestart) onRestart();
  };
  const t = useT();
  const [recordingDone, setRecordingDone] = useState(false);
  const [alertsDone, setAlertsDone] = useState(false);
  const cameraCount = (saved || []).length;

  const next = () => setStep((s) => Math.min(STEP_KEYS.length - 1, s + 1));
  const back = () => setStep((s) => Math.max(0, s - 1));

  // A Welcome-step restore replaces the whole configuration and needs a restart for
  // every service to pick it up (and the wizard is bypassed afterwards). When it
  // fires, collapse the entire wizard to a single wait pane — no Skip / Back / Next /
  // step content left to click — and kick off the restart; the app reloads when it's
  // back up.
  const [restarting, setRestarting] = useState(false);
  const restoreRestart = () => { setRestarting(true); if (onRestart) onRestart(); };

  if (restarting) {
    return (
      <main className="login-screen">
        <div className="setup-wizard">
          <div className="setup-head"><BrandLogo size={44} /></div>
          <div className="setup-body setup-restarting">
            <span className="field-hint good"><Ico n="check-ok" sz={16} /> {t('setup.bkupRestored')}</span>
            <span className="field-hint"><Ico n="reload" sz={16} /> {t('setup.bkupRestartingWait')}</span>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="login-screen">
      <div className="setup-wizard">
        <FormBusyOverlay busy={busy} />
        <div className="setup-head">
          <BrandLogo size={44} />
          <div className="setup-head-actions">
            <LanguageDropdown lang={lang} onLang={onLangChange} />
            <button type="button" className="quiet setup-skip" onClick={onFinish} disabled={busy}>
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
          {step === 0 ? (
            <WelcomeStep username={username} busy={busy} onChangePassword={onChangePassword} authHeader={authHeader} onRestart={restoreRestart} />
          ) : null}
          {step === 1 ? (
            <SystemStep
              busy={busy}
              onFfmpegStatus={onFfmpegStatus}
              onInstallFfmpeg={onInstallFfmpeg}
              onSystemTime={onSystemTime}
              onRestart={restartAndResume}
            />
          ) : null}
          {step === 2 ? (
            <AiStep
              busy={busy}
              onAiCapability={onAiCapability}
              onInstallAiDeps={onInstallAiDeps}
              onStockModel={onStockModel}
              onApplyStockModel={onApplyStockModel}
              onRestart={restartAndResume}
            />
          ) : null}
          {step === 3 ? (
            <CapacityStep capacity={capacity} busy={busy} onEstimateCapacity={onEstimateCapacity} onCalibrateCapacity={onCalibrateCapacity} />
          ) : null}
          {step === 4 ? (
            <CamerasStep busy={busy} saved={saved} discovered={discovered} onScan={onScan} onAddCamera={onAddCamera} />
          ) : null}
          {step === 5 ? (
            <RecordingStep
              busy={busy}
              cameraCount={cameraCount}
              recordingDone={recordingDone}
              onDiskInfo={onDiskInfo}
              onEnableRecording={async (path) => { await onEnableRecordingAll(path); setRecordingDone(true); }}
            />
          ) : null}
          {step === 6 ? (
            <AlertsStep
              busy={busy}
              cameraCount={cameraCount}
              alertsDone={alertsDone}
              onAddDestination={onAddDestination}
              onAddAlerts={async () => { await onAddPersonRuleAll(); setAlertsDone(true); }}
            />
          ) : null}
          {step === 7 ? (
            <ConnectivityStep authHeader={authHeader} />
          ) : null}
          {step === 8 ? (
            <DoneStep cameraCount={cameraCount} recordingDone={recordingDone} alertsDone={alertsDone} />
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
            <button type="button" onClick={onFinish} disabled={busy}>
              <span className="btn-icon"><Ico n="check-ok" /> {t('setup.finish')}</span>
            </button>
          )}
        </div>
      </div>
    </main>
  );
}

// ConnectivityStep (optional) lets the operator link this node to a myseliasan
// control plane during onboarding: paste the fleet key, then generate a claim code
// to enter in the control plane. Fully skippable — pairing can also be done later
// from Settings → Connectivity.
function ConnectivityStep({ authHeader }) {
  const t = useT();
  const [status, setStatus] = useState(null);
  const [fleetKey, setFleetKey] = useState('');
  const [claim, setClaim] = useState(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState(null);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, body, message: payload?.message };
  }

  async function load() {
    const r = await api('/api/pairing/status').catch(() => ({ ok: false }));
    if (r.ok) setStatus(r.body);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [authHeader]);

  async function saveKey() {
    if (fleetKey.trim().length < 16) { setNote({ err: true, text: t('setup.fleetKeyMin') }); return; }
    setBusy(true);
    const r = await api('/api/pairing/fleet-key', { method: 'PUT', body: JSON.stringify({ key: fleetKey.trim() }) });
    setBusy(false);
    if (r.ok) { setNote({ err: false, text: t('setup.fleetKeySaved') }); setFleetKey(''); load(); }
    else setNote({ err: true, text: r.message || t('setup.fleetKeyFail') });
  }

  async function genClaim() {
    setBusy(true);
    const r = await api('/api/pairing/claim-code', { method: 'POST' });
    setBusy(false);
    if (r.ok) { setClaim(r.body); setNote(null); }
    else setNote({ err: true, text: r.message || t('setup.claimFail') });
  }

  const fleetKeySet = !!status?.fleetKeySet;
  const paired = !!status?.paired;

  return (
    <div className="setup-pane">
      <h2>{t('setup.connectTitle')} <span className="field-hint">{t('setup.optional')}</span></h2>
      <p>{t('setup.connectIntro')}</p>
      {note ? <span className={note.err ? 'field-hint danger-text' : 'field-hint good'}>{note.text}</span> : null}
      {paired ? (
        <p>{t('setup.alreadyPaired', { name: status?.parentName || status?.parentId })}</p>
      ) : (
        <div className="setup-pw-form">
          <label>{t('setup.fleetKey')} {fleetKeySet ? t('setup.fleetKeySetSuffix') : ''}
            <input
              type="password"
              value={fleetKey}
              onChange={(e) => setFleetKey(e.target.value)}
              placeholder={fleetKeySet ? t('setup.fleetKeyPlaceholderSet') : t('setup.fleetKeyPlaceholder')}
              disabled={busy}
              autoComplete="off"
            />
          </label>
          <div className="setup-account">
            <button type="button" onClick={saveKey} disabled={busy || fleetKey.trim().length < 16}>
              <span className="btn-icon"><Ico n="save" /> {t('setup.saveFleetKey')}</span>
            </button>
            <button type="button" className="quiet" onClick={genClaim} disabled={busy || !fleetKeySet}>
              <span className="btn-icon"><Ico n="refresh" /> {t('setup.genClaim')}</span>
            </button>
          </div>
          {claim ? (
            <p>
              {t('setup.claimCodeLabel')} <strong style={{ letterSpacing: 2, fontSize: '1.2em' }}>{claim.code}</strong>
              {claim.expiresAt ? <>{t('setup.expiresAt', { time: new Date(claim.expiresAt * 1000).toLocaleTimeString() })}</> : null}
            </p>
          ) : null}
        </div>
      )}
    </div>
  );
}

// WelcomeRestore lets a fresh install recover its whole configuration from a
// passphrase-protected .mmbackup file made on another machine, right at the start of
// onboarding — so the operator can skip the rest of the wizard. A restart afterwards
// lets every service pick up the restored settings. See services/backup.go.
function WelcomeRestore({ authHeader, onRestart }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [fileData, setFileData] = useState('');
  const [pass, setPass] = useState('');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState(null);
  const [result, setResult] = useState(null);
  const [restarting, setRestarting] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, body, message: payload?.message };
  }

  async function onPickFile(e) {
    const file = e.target.files && e.target.files[0];
    setResult(null); setNote(null);
    if (!file) { setFileData(''); return; }
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      setFileData(btoa(bin));
    } catch (_) { setFileData(''); setNote({ err: true, text: t('setup.bkupFail') }); }
  }

  async function restore() {
    if (!fileData) { setNote({ err: true, text: t('setup.bkupNeedFile') }); return; }
    if (!pass) { setNote({ err: true, text: t('setup.bkupNeedPass') }); return; }
    setBusy(true);
    try {
      // Fresh install → replace, so the backup is the source of truth.
      const r = await api('/api/settings/backup/restore', { method: 'POST', body: JSON.stringify({ dataBase64: fileData, passphrase: pass, mode: 'replace' }) });
      if (!r.ok || !r.body?.restored) { setNote({ err: true, text: r.message || t('setup.bkupFail') }); return; }
      setResult(r.body); setNote(null);
    } catch (err) {
      setNote({ err: true, text: err?.message || t('setup.bkupFail') });
    } finally {
      setBusy(false);
    }
  }

  // Once the restore succeeds there is nothing left for the operator to do but wait:
  // a restart is required for every service to pick up the restored settings, and the
  // wizard is bypassed afterwards (restore marks setup complete). Kick off the restart
  // automatically, exactly once, and collapse the form below to a wait state — so no
  // stray "Restore"/"Cancel" buttons remain clickable mid-restart.
  useEffect(() => {
    if (result && !restarting) {
      setRestarting(true);
      if (onRestart) onRestart();
    }
  }, [result]); // eslint-disable-line react-hooks/exhaustive-deps

  if (result) {
    return (
      <div className="setup-account setup-restore-done">
        <span className="field-hint good"><Ico n="check-ok" sz={14} /> {t('setup.bkupRestored')}</span>
        <span className="field-hint"><Ico n="reload" sz={14} /> {t('setup.bkupRestartingWait')}</span>
      </div>
    );
  }

  return (
    <>
      <div className="setup-account">
        <span className="field-hint"><Ico n="reload" sz={14} /> {t('setup.haveBackup')}</span>
        <button type="button" className="quiet" onClick={() => setOpen((o) => !o)} disabled={busy}>
          <span className="btn-icon"><Ico n={open ? 'x' : 'reload'} /> {open ? t('setup.cancel') : t('setup.restoreBackup')}</span>
        </button>
      </div>
      {open ? (
        <div className="setup-pw-form">
          <label>{t('setup.bkupFile')}
            <input type="file" accept=".mmbackup,application/octet-stream" onChange={onPickFile} disabled={busy} />
          </label>
          <label>{t('setup.bkupPassphrase')}
            <PasswordField value={pass} onChange={setPass} autoComplete="off" />
          </label>
          {note ? <span className={note.err ? 'field-hint danger-text' : 'field-hint good'}>{note.text}</span> : null}
          <button type="button" onClick={restore} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> {busy ? t('setup.bkupRestoring') : t('setup.bkupRestoreBtn')}</span>
          </button>
        </div>
      ) : null}
    </>
  );
}

function WelcomeStep({ username, busy, onChangePassword, authHeader, onRestart }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState('');
  const [done, setDone] = useState(false);

  async function submit() {
    if (next.length < 8) { setLocalError(t('setup.pwMin')); return; }
    if (next !== confirm) { setLocalError(t('setup.pwNoMatch')); return; }
    setLocalError('');
    await onChangePassword({ currentPassword: current, newPassword: next });
    setDone(true);
    setOpen(false);
    setCurrent(''); setNext(''); setConfirm('');
  }

  return (
    <div className="setup-pane">
      <h2>{t('setup.welcomeTitle')}</h2>
      <p>{t('setup.welcomeIntro', { username: username || 'admin' })}</p>
      <div className="setup-account">
        <span className="field-hint good"><Ico n="check-ok" sz={14} /> {t('setup.pwSet', { updated: done ? t('setup.pwUpdatedSuffix') : '' })}</span>
        {onChangePassword ? (
          <button type="button" className="quiet" onClick={() => setOpen((o) => !o)} disabled={busy}>
            <span className="btn-icon"><Ico n="key" /> {open ? t('setup.cancel') : t('setup.changePassword')}</span>
          </button>
        ) : null}
      </div>
      {open ? (
        <div className="setup-pw-form">
          <label>{t('setup.current')}<PasswordField value={current} onChange={setCurrent} autoComplete="current-password" /></label>
          <label>{t('setup.new')}<PasswordField value={next} onChange={setNext} autoComplete="new-password" /></label>
          <label>{t('setup.confirm')}<PasswordField value={confirm} onChange={setConfirm} autoComplete="new-password" /></label>
          {localError ? <span className="field-hint danger-text">{localError}</span> : null}
          <button type="button" onClick={submit} disabled={busy}>
            <span className="btn-icon"><Ico n="save" /> {t('setup.updatePassword')}</span>
          </button>
        </div>
      ) : null}

      <hr className="settings-divider" />
      <WelcomeRestore authHeader={authHeader} onRestart={onRestart} />
    </div>
  );
}

function CapacityStep({ capacity, busy, onEstimateCapacity, onCalibrateCapacity }) {
  const t = useT();
  const limitRaw = capacity ? ((capacity.workloads || []).find((w) => w.name === capacity.limitingWorkload)?.limit) : null;
  const limit = limitRaw ? (capacityLimitKey[limitRaw] ? t(capacityLimitKey[limitRaw]) : limitRaw) : (capacity ? capacity.limitingWorkload : '');
  return (
    <div className="setup-pane">
      <h2>{t('setup.capacityTitle')}</h2>
      <p>{t('setup.capacityIntro')}</p>
      <div className="setup-actions">
        <button type="button" onClick={() => onCalibrateCapacity && onCalibrateCapacity()} disabled={busy}>
          <span className="btn-icon"><Ico n="wand" /> {t('setup.runCalibration')}</span>
        </button>
        <button type="button" className="quiet" onClick={() => onEstimateCapacity && onEstimateCapacity()} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> {t('setup.quickEstimate')}</span>
        </button>
      </div>
      {capacity ? (
        <div className="setup-capacity">
          <span className="capacity-number">{capacity.estimatedMax}</span>
          <div className="capacity-headline-meta">
            <span className="capacity-caption">{t('setup.estCameras')}</span>
            <span className={`capacity-badge capacity-badge--${capacity.confidence}`}>
              {capacity.confidence === 'measured' ? t('setup.measured') : capacity.confidence === 'calibrated' ? t('setup.calibrated') : t('setup.ballpark')}
            </span>
            {limit ? <span className="field-hint">{t('setup.limitedBy', { limit })}</span> : null}
          </div>
          <CapacityRetentionNote capacity={capacity} />
        </div>
      ) : <p className="empty">{t('setup.noEstimate')}</p>}
    </div>
  );
}

// SystemStep checks the host prerequisites: the video engine (ffmpeg) and the clock.
// Each card only prompts an action when something is missing ("only if not found").
function SystemStep({ busy, onFfmpegStatus, onInstallFfmpeg, onSystemTime, onRestart }) {
  const t = useT();
  const [ffmpeg, setFfmpeg] = useState(undefined); // undefined = loading
  const [time, setTime] = useState(null);
  const [installed, setInstalled] = useState(false);
  const [installState, setInstallState] = useState(null);

  const refresh = async () => { setFfmpeg(await onFfmpegStatus()); };
  useEffect(() => { refresh(); onSystemTime().then(setTime); /* eslint-disable-next-line */ }, []);

  async function download() {
    const state = await onInstallFfmpeg();
    setInstallState(state);
    if (state?.status === 'done') { setInstalled(true); await refresh(); }
  }

  return (
    <div className="setup-pane">
      <h2>{t('setup.systemCheck')}</h2>
      <p>{t('setup.systemIntro')}</p>

      <div className="setup-toggle-row">
        <div>
          <strong>{t('setup.videoEngine')}</strong>
          <span className="field-hint">
            {ffmpeg === undefined ? t('setup.checking')
              : ffmpeg?.found ? (ffmpeg.version || ffmpeg.path)
              : t('setup.ffmpegNotFound')}
          </span>
        </div>
        {ffmpeg === undefined ? <span /> : ffmpeg?.found && !installed ? (
          <span className="field-hint good"><Ico n="check-ok" sz={14} /> {t('setup.ready')}</span>
        ) : installed ? (
          <button type="button" onClick={() => onRestart && onRestart()} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> {t('setup.restartApply')}</span>
          </button>
        ) : (
          <button type="button" onClick={download} disabled={busy}>
            <span className="btn-icon"><Ico n="download" /> {t('setup.downloadFfmpeg')}</span>
          </button>
        )}
      </div>
      {installState && installState.status === 'failed' ? (
        <p className="field-hint danger-text">
          {installState.supported === false ? t('setup.ffmpegFailUnsupported') : t('setup.ffmpegFail')}
        </p>
      ) : null}

      <div className="setup-toggle-row">
        <div>
          <strong>{t('setup.clockTz')}</strong>
          <span className="field-hint">
            {time ? `${time.now} (${time.abbrev || time.timezone})` : t('setup.checking')}
          </span>
        </div>
        <span className="field-hint">{time ? t('setup.clockNote') : ''}</span>
      </div>
    </div>
  );
}

// AiStep ensures the AI runtime (Python/torch/ultralytics) is usable and lets the user
// pick a detection model, downloading and applying it. Both only prompt when needed.
function AiStep({ busy, onAiCapability, onInstallAiDeps, onStockModel, onApplyStockModel, onRestart }) {
  const t = useT();
  const [cap, setCap] = useState(undefined);
  const [model, setModel] = useState(null);
  const [choice, setChoice] = useState('');
  const [installedDeps, setInstalledDeps] = useState(false);

  const refresh = async () => {
    const c = await onAiCapability();
    setCap(c);
    const m = await onStockModel();
    setModel(m);
    if (m?.current && !choice) setChoice(m.current.replace(/\.pt$/i, ''));
  };
  useEffect(() => { refresh(); /* eslint-disable-next-line */ }, []);

  async function installDeps() {
    const state = await onInstallAiDeps();
    if (state?.status === 'done') { setInstalledDeps(true); await refresh(); }
  }
  async function apply() {
    await onApplyStockModel(choice);
    await refresh();
  }

  const ready = cap?.available;
  return (
    <div className="setup-pane">
      <h2>{t('setup.aiDetection')}</h2>
      <p>{t('setup.aiIntro')}</p>

      <div className="setup-toggle-row">
        <div>
          <strong>{t('setup.aiRuntime')}</strong>
          <span className="field-hint">{cap === undefined ? t('setup.checking') : (cap?.detail || (ready ? t('setup.readyDot') : t('setup.notReady')))}</span>
        </div>
        {cap === undefined ? <span /> : ready && !installedDeps ? (
          <span className="field-hint good"><Ico n="check-ok" sz={14} /> {t('setup.ready')}</span>
        ) : installedDeps ? (
          <button type="button" onClick={() => onRestart && onRestart()} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> {t('setup.restartApply')}</span>
          </button>
        ) : cap?.canInstall ? (
          <button type="button" onClick={installDeps} disabled={busy}>
            <span className="btn-icon"><Ico n="wand" /> {t('setup.installAi')}</span>
          </button>
        ) : (
          <span className="field-hint">{t('setup.installPyHint')}</span>
        )}
      </div>

      {model?.options?.length ? (
        <div className="setup-toggle-row">
          <div>
            <strong>{t('setup.detModel')}</strong>
            <span className="field-hint">{t('setup.modelHint', { current: model.current })}</span>
          </div>
          <div className="setup-actions">
            <select value={choice} onChange={(e) => setChoice(e.target.value)} disabled={busy}>
              {model.options.map((opt) => (
                <option key={opt} value={opt.replace(/\.pt$/i, '')}>{opt}</option>
              ))}
            </select>
            <button type="button" onClick={apply} disabled={busy || !choice}>
              <span className="btn-icon"><Ico n="download" /> {t('setup.downloadApply')}</span>
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function DiscoveredRow({ device, busy, onAdd }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(() => cameraTitle(device));
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState('');

  async function add() {
    setAdding(true);
    setError('');
    try {
      // onAdd verifies the credentials server-side and throws if the camera rejects them,
      // so a failed login leaves the camera un-added with the error shown.
      await onAdd(device, { name: name.trim(), username: username.trim(), password });
    } catch (err) {
      setError(err?.message || t('setup.addFailed'));
    } finally {
      setAdding(false);
    }
  }

  return (
    <li className="setup-found-row-wrap">
      <div className="setup-found-row">
        <span className="setup-found-name">{cameraTitle(device)}<span className="field-hint">{device.host}</span></span>
        <button type="button" className="quiet" onClick={() => setOpen((o) => !o)} disabled={busy}>
          <span className="btn-icon"><Ico n={open ? 'x' : 'plus'} /> {open ? t('setup.cancel') : t('setup.add')}</span>
        </button>
      </div>
      {open ? (
        <div className="setup-cred-form">
          <label>{t('setup.cameraName')}
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" placeholder={cameraTitle(device)} />
          </label>
          <span className="field-hint">{t('setup.cameraNameHint')}</span>
          <label>{t('setup.username')}
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder={t('setup.usernamePlaceholder')} />
          </label>
          <label>{t('setup.password')}
            <PasswordField value={password} onChange={setPassword} autoComplete="off" />
          </label>
          <span className="field-hint">{t('setup.passwordHint')}</span>
          {error ? <span className="field-hint danger-text">{error}</span> : null}
          <button type="button" className="setup-cred-add" onClick={add} disabled={busy || adding}>
            <span className="btn-icon"><Ico n="shield" /> {adding ? t('setup.adding') : t('setup.addCamera')}</span>
          </button>
        </div>
      ) : null}
    </li>
  );
}

function CamerasStep({ busy, saved, discovered, onScan, onAddCamera }) {
  const t = useT();
  const savedCount = (saved || []).length;
  const newDevices = (discovered || []).filter((d) => !(saved || []).some((s) => sameCamera(d, s)));
  return (
    <div className="setup-pane">
      <h2>{t('setup.addFirstCamera')}</h2>
      <p>{t('setup.camerasIntro')}</p>
      <div className="setup-actions">
        <button type="button" onClick={() => onScan && onScan()} disabled={busy}>
          <span className="btn-icon"><Ico n="search" /> {t('setup.scanNetwork')}</span>
        </button>
        <span className="field-hint">{t(savedCount === 1 ? 'setup.camerasAdded1' : 'setup.camerasAddedN', { n: savedCount })}</span>
      </div>
      {newDevices.length > 0 ? (
        <ul className="setup-found-list">
          {newDevices.map((device) => (
            <DiscoveredRow key={device.xAddr || device.host} device={device} busy={busy} onAdd={onAddCamera} />
          ))}
        </ul>
      ) : (discovered && discovered.length > 0 ? <p className="field-hint">{t('setup.allAdded')}</p> : null)}
    </div>
  );
}

// RecordingStep enables continuous recording and warns if the storage volume is
// nearly full (recordings auto-purge, but a full disk still stops new writes).
function RecordingStep({ busy, cameraCount, recordingDone, onDiskInfo, onEnableRecording }) {
  const t = useT();
  const [path, setPath] = useState('recordings');
  const [disks, setDisks] = useState(null);
  useEffect(() => { onDiskInfo && onDiskInfo().then(setDisks); /* eslint-disable-next-line */ }, []);
  const fullest = Array.isArray(disks) && disks.length
    ? disks.reduce((a, b) => (b.usedPercent > (a?.usedPercent ?? -1) ? b : a), null)
    : null;
  const lowSpace = fullest && fullest.usedPercent >= 85;

  if (cameraCount === 0) {
    return (
      <div className="setup-pane">
        <h2>{t('setup.recording')}</h2>
        <p className="empty">{t('setup.recordingNoCam')}</p>
      </div>
    );
  }
  return (
    <div className="setup-pane">
      <h2>{t('setup.recording')}</h2>
      <p>{t(cameraCount === 1 ? 'setup.recordingIntro1' : 'setup.recordingIntroN', { n: cameraCount })}</p>
      <label className="setup-field">
        <span>{t('setup.storageFolder')}</span>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="recordings" disabled={busy || recordingDone} />
      </label>
      {lowSpace ? (
        <p className="field-hint danger-text">
          {t('setup.lowSpace', { mount: fullest.mountpoint, pct: Math.round(fullest.usedPercent) })}
        </p>
      ) : fullest ? (
        <p className="field-hint">{t('setup.largestVolume', { mount: fullest.mountpoint, pct: Math.round(fullest.usedPercent) })}</p>
      ) : null}
      <div className="setup-toggle-row">
        <div>
          <strong>{t('setup.recordContinuously')}</strong>
          <span className="field-hint">{t('setup.nvrHint')}</span>
        </div>
        <button type="button" className={recordingDone ? 'quiet' : ''} onClick={() => onEnableRecording(path)} disabled={busy || recordingDone}>
          <span className="btn-icon">{recordingDone ? <><Ico n="check-ok" /> {t('setup.enabled')}</> : <><Ico n="film" /> {t('setup.enableRecording')}</>}</span>
        </button>
      </div>
    </div>
  );
}

// AlertsStep adds a person-detection rule and (optionally) a delivery destination so
// alerts go somewhere beyond the in-app feed.
function AlertsStep({ busy, cameraCount, alertsDone, onAddDestination, onAddAlerts }) {
  const t = useT();
  const [type, setType] = useState('webhook');
  const [url, setUrl] = useState('');
  const [botToken, setBotToken] = useState('');
  const [chatId, setChatId] = useState('');
  const [brokerUrl, setBrokerUrl] = useState('');
  const [topic, setTopic] = useState('');
  const [destDone, setDestDone] = useState(false);

  async function addDestination() {
    const base = defaultDestination(type);
    let dest;
    if (type === 'telegram') {
      dest = { ...base, botToken: botToken.trim(), chatId: chatId.trim() };
    } else if (type === 'mqtt') {
      dest = { ...base, mqtt: { ...base.mqtt, brokerUrl: brokerUrl.trim(), topic: topic.trim() } };
    } else {
      dest = { ...base, url: url.trim() };
    }
    const result = await onAddDestination(dest);
    if (result) setDestDone(true);
  }

  const destValid = type === 'telegram'
    ? (botToken.trim() && chatId.trim())
    : type === 'mqtt'
      ? (brokerUrl.trim() && topic.trim())
      : url.trim();

  return (
    <div className="setup-pane">
      <h2>{t('setup.alerts')}</h2>
      {cameraCount === 0 ? (
        <p className="empty">{t('setup.alertsNoCam')}</p>
      ) : (
        <>
          <p>{t('setup.alertsIntro')}</p>
          <div className="setup-toggle-row">
            <div>
              <strong>{t('setup.alertOnPeople')}</strong>
              <span className="field-hint">{t('setup.personRuleHint')}</span>
            </div>
            <button type="button" className={alertsDone ? 'quiet' : ''} onClick={onAddAlerts} disabled={busy || alertsDone}>
              <span className="btn-icon">{alertsDone ? <><Ico n="check-ok" /> {t('setup.added')}</> : <><Ico n="bell" /> {t('setup.addPersonAlerts')}</>}</span>
            </button>
          </div>

          <div className="setup-dest">
            <label className="setup-field">
              <span>{t('setup.deliverTo')}</span>
              <select value={type} onChange={(e) => { setType(e.target.value); setDestDone(false); }} disabled={busy || destDone}>
                <option value="webhook">{t('setup.webhook')}</option>
                <option value="telegram">{t('setup.telegram')}</option>
                <option value="mqtt">{t('setup.mqtt')}</option>
              </select>
            </label>
            {type === 'telegram' ? (
              <>
                <label className="setup-field"><span>{t('setup.botToken')}</span><input value={botToken} onChange={(e) => setBotToken(e.target.value)} disabled={busy || destDone} /></label>
                <label className="setup-field"><span>{t('setup.chatId')}</span><input value={chatId} onChange={(e) => setChatId(e.target.value)} disabled={busy || destDone} /></label>
              </>
            ) : type === 'mqtt' ? (
              <>
                <label className="setup-field"><span>{t('setup.brokerUrl')}</span><input value={brokerUrl} onChange={(e) => setBrokerUrl(e.target.value)} placeholder="tls://broker.example.com:8883" disabled={busy || destDone} /></label>
                <label className="setup-field"><span>{t('setup.topic')}</span><input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="alerts/camera" disabled={busy || destDone} /></label>
                <span className="field-hint">{t('setup.mqttHint')}</span>
              </>
            ) : (
              <label className="setup-field"><span>{t('setup.webhookUrl')}</span><input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" disabled={busy || destDone} /></label>
            )}
            <button type="button" className={destDone ? 'quiet' : ''} onClick={addDestination} disabled={busy || destDone || !destValid}>
              <span className="btn-icon">{destDone ? <><Ico n="check-ok" /> {t('setup.saved')}</> : <><Ico n="save" /> {t('setup.saveDestination')}</>}</span>
            </button>
          </div>
          <p className="field-hint">{t('setup.alertsOptional')}</p>
        </>
      )}
    </div>
  );
}

function DoneStep({ cameraCount, recordingDone, alertsDone }) {
  const t = useT();
  return (
    <div className="setup-pane setup-done">
      <Ico n="check-ok" sz={48} />
      <h2>{t('setup.allSet')}</h2>
      <ul className="setup-summary">
        <li>{t(cameraCount === 1 ? 'setup.summaryCameras1' : 'setup.summaryCamerasN', { n: cameraCount })}</li>
        <li>{t(recordingDone ? 'setup.recordingEnabled' : 'setup.recordingNotEnabled')}</li>
        <li>{t(alertsDone ? 'setup.alertsEnabled' : 'setup.alertsNotEnabled')}</li>
      </ul>
      <p>{t('setup.finishHint')}</p>
    </div>
  );
}
