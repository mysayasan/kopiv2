import { useState, useEffect } from 'react';
import { Ico } from './icons';
import { BrandLogo, PasswordField } from './layout';
import { Message, FormBusyOverlay } from './ui';
import { cameraTitle, sameCamera } from '../lib/helpers';
import { CapacityRetentionNote } from './settings';
import { defaultDestination } from '../lib/constants';

const STEPS = ['Welcome', 'System', 'AI', 'Capacity', 'Cameras', 'Recording', 'Alerts', 'Done'];
const capacityLimitText = { cpu: 'CPU', gpu: 'GPU', disk: 'disk space', memory: 'memory' };

// SetupWizard is the first-run onboarding overlay: welcome/account, machine
// capacity, add a camera, enable recording + person alerts, then finish. It is
// purely a thin orchestrator over the app's existing handlers, passed as props.
export function SetupWizard({
  username,
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
  // Persist the step so an in-app restart (e.g. after installing ffmpeg / GPU deps)
  // resumes the wizard where it left off rather than jumping back to the start.
  const [step, setStepState] = useState(() => {
    const saved = Number(window.localStorage.getItem('setupStep'));
    return Number.isInteger(saved) && saved > 0 && saved < STEPS.length ? saved : 0;
  });
  const setStep = (updater) => setStepState((s) => {
    const nextStep = typeof updater === 'function' ? updater(s) : updater;
    try { window.localStorage.setItem('setupStep', String(nextStep)); } catch (_) {}
    return nextStep;
  });
  const [recordingDone, setRecordingDone] = useState(false);
  const [alertsDone, setAlertsDone] = useState(false);
  const cameraCount = (saved || []).length;

  const next = () => setStep((s) => Math.min(STEPS.length - 1, s + 1));
  const back = () => setStep((s) => Math.max(0, s - 1));

  return (
    <main className="login-screen">
      <div className="setup-wizard">
        <FormBusyOverlay busy={busy} />
        <div className="setup-head">
          <BrandLogo size={44} />
          <button type="button" className="quiet setup-skip" onClick={onFinish} disabled={busy}>
            Skip setup
          </button>
        </div>

        <ol className="setup-steps" aria-label="Setup progress">
          {STEPS.map((label, i) => (
            <li key={label} className={`setup-step-dot${i === step ? ' active' : ''}${i < step ? ' done' : ''}`}>
              <span className="setup-step-num">{i < step ? '✓' : i + 1}</span>
              <span className="setup-step-label">{label}</span>
            </li>
          ))}
        </ol>

        <div className="setup-body">
          {step === 0 ? (
            <WelcomeStep username={username} busy={busy} onChangePassword={onChangePassword} />
          ) : null}
          {step === 1 ? (
            <SystemStep
              busy={busy}
              onFfmpegStatus={onFfmpegStatus}
              onInstallFfmpeg={onInstallFfmpeg}
              onSystemTime={onSystemTime}
              onRestart={onRestart}
            />
          ) : null}
          {step === 2 ? (
            <AiStep
              busy={busy}
              onAiCapability={onAiCapability}
              onInstallAiDeps={onInstallAiDeps}
              onStockModel={onStockModel}
              onApplyStockModel={onApplyStockModel}
              onRestart={onRestart}
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
            <DoneStep cameraCount={cameraCount} recordingDone={recordingDone} alertsDone={alertsDone} />
          ) : null}
        </div>

        <Message value={message} />

        <div className="setup-nav">
          {step > 0 ? (
            <button type="button" className="quiet" onClick={back} disabled={busy}>
              <span className="btn-icon"><Ico n="arr-left" /> Back</span>
            </button>
          ) : <span />}
          {step < STEPS.length - 1 ? (
            <button type="button" onClick={next} disabled={busy}>
              <span className="btn-icon">Next <Ico n="arr-right" /></span>
            </button>
          ) : (
            <button type="button" onClick={onFinish} disabled={busy}>
              <span className="btn-icon"><Ico n="check-ok" /> Finish</span>
            </button>
          )}
        </div>
      </div>
    </main>
  );
}

function WelcomeStep({ username, busy, onChangePassword }) {
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState('');
  const [done, setDone] = useState(false);

  async function submit() {
    if (next.length < 8) { setLocalError('New password must be at least 8 characters.'); return; }
    if (next !== confirm) { setLocalError('New passwords do not match.'); return; }
    setLocalError('');
    await onChangePassword({ currentPassword: current, newPassword: next });
    setDone(true);
    setOpen(false);
    setCurrent(''); setNext(''); setConfirm('');
  }

  return (
    <div className="setup-pane">
      <h2>Welcome to MyMataSan</h2>
      <p>Let&apos;s get your camera monitor set up. You&apos;re signed in as <strong>{username || 'admin'}</strong> (Administrator).</p>
      <div className="setup-account">
        <span className="field-hint good"><Ico n="check-ok" sz={14} /> Your account password is set{done ? ' (updated)' : ''}.</span>
        {onChangePassword ? (
          <button type="button" className="quiet" onClick={() => setOpen((o) => !o)} disabled={busy}>
            <span className="btn-icon"><Ico n="key" /> {open ? 'Cancel' : 'Change password'}</span>
          </button>
        ) : null}
      </div>
      {open ? (
        <div className="setup-pw-form">
          <label>Current password<PasswordField value={current} onChange={setCurrent} autoComplete="current-password" /></label>
          <label>New password<PasswordField value={next} onChange={setNext} autoComplete="new-password" /></label>
          <label>Confirm new password<PasswordField value={confirm} onChange={setConfirm} autoComplete="new-password" /></label>
          {localError ? <span className="field-hint danger-text">{localError}</span> : null}
          <button type="button" onClick={submit} disabled={busy}>
            <span className="btn-icon"><Ico n="save" /> Update password</span>
          </button>
        </div>
      ) : null}
    </div>
  );
}

function CapacityStep({ capacity, busy, onEstimateCapacity, onCalibrateCapacity }) {
  const limit = capacity ? (capacityLimitText[(capacity.workloads || []).find((w) => w.name === capacity.limitingWorkload)?.limit] || capacity.limitingWorkload) : '';
  return (
    <div className="setup-pane">
      <h2>How many cameras can this machine handle?</h2>
      <p>Run a quick benchmark for an accurate estimate before you add cameras — or just estimate from detected hardware.</p>
      <div className="setup-actions">
        <button type="button" onClick={() => onCalibrateCapacity && onCalibrateCapacity()} disabled={busy}>
          <span className="btn-icon"><Ico n="wand" /> Run calibration</span>
        </button>
        <button type="button" className="quiet" onClick={() => onEstimateCapacity && onEstimateCapacity()} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> Quick estimate</span>
        </button>
      </div>
      {capacity ? (
        <div className="setup-capacity">
          <span className="capacity-number">{capacity.estimatedMax}</span>
          <div className="capacity-headline-meta">
            <span className="capacity-caption">estimated cameras</span>
            <span className={`capacity-badge capacity-badge--${capacity.confidence}`}>
              {capacity.confidence === 'measured' ? 'Measured from live load' : capacity.confidence === 'calibrated' ? 'Calibrated on this host' : 'Ballpark estimate'}
            </span>
            {limit ? <span className="field-hint">Limited by {limit}</span> : null}
          </div>
          <CapacityRetentionNote capacity={capacity} />
        </div>
      ) : <p className="empty">No estimate yet.</p>}
    </div>
  );
}

// SystemStep checks the host prerequisites: the video engine (ffmpeg) and the clock.
// Each card only prompts an action when something is missing ("only if not found").
function SystemStep({ busy, onFfmpegStatus, onInstallFfmpeg, onSystemTime, onRestart }) {
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
      <h2>System check</h2>
      <p>Make sure the basics this app needs are in place before adding cameras.</p>

      <div className="setup-toggle-row">
        <div>
          <strong>Video engine (ffmpeg)</strong>
          <span className="field-hint">
            {ffmpeg === undefined ? 'Checking…'
              : ffmpeg?.found ? (ffmpeg.version || ffmpeg.path)
              : 'Not found — required for live view, recording, and AI capture.'}
          </span>
        </div>
        {ffmpeg === undefined ? <span /> : ffmpeg?.found && !installed ? (
          <span className="field-hint good"><Ico n="check-ok" sz={14} /> Ready</span>
        ) : installed ? (
          <button type="button" onClick={() => onRestart && onRestart()} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> Restart to apply</span>
          </button>
        ) : (
          <button type="button" onClick={download} disabled={busy}>
            <span className="btn-icon"><Ico n="download" /> Download ffmpeg</span>
          </button>
        )}
      </div>
      {installState && installState.status === 'failed' ? (
        <p className="field-hint danger-text">
          Download failed. {installState.supported === false ? 'Automatic download isn’t available for this platform — install ffmpeg manually and set its path in Settings → Decoder.' : 'You can install ffmpeg manually and set its path in Settings → Decoder.'}
        </p>
      ) : null}

      <div className="setup-toggle-row">
        <div>
          <strong>Clock &amp; timezone</strong>
          <span className="field-hint">
            {time ? `${time.now} (${time.abbrev || time.timezone})` : 'Checking…'}
          </span>
        </div>
        <span className="field-hint">{time ? 'Event timestamps use this. Fix in the OS if wrong.' : ''}</span>
      </div>
    </div>
  );
}

// AiStep ensures the AI runtime (Python/torch/ultralytics) is usable and lets the user
// pick a detection model, downloading and applying it. Both only prompt when needed.
function AiStep({ busy, onAiCapability, onInstallAiDeps, onStockModel, onApplyStockModel, onRestart }) {
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
      <h2>AI detection</h2>
      <p>The detector needs an AI runtime and a model. Set these up now, or skip and configure later in Settings.</p>

      <div className="setup-toggle-row">
        <div>
          <strong>AI runtime</strong>
          <span className="field-hint">{cap === undefined ? 'Checking…' : (cap?.detail || (ready ? 'Ready.' : 'Not ready.'))}</span>
        </div>
        {cap === undefined ? <span /> : ready && !installedDeps ? (
          <span className="field-hint good"><Ico n="check-ok" sz={14} /> Ready</span>
        ) : installedDeps ? (
          <button type="button" onClick={() => onRestart && onRestart()} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> Restart to apply</span>
          </button>
        ) : cap?.canInstall ? (
          <button type="button" onClick={installDeps} disabled={busy}>
            <span className="btn-icon"><Ico n="wand" /> Install AI support</span>
          </button>
        ) : (
          <span className="field-hint">Install Python + ultralytics, then re-check.</span>
        )}
      </div>

      {model?.options?.length ? (
        <div className="setup-toggle-row">
          <div>
            <strong>Detection model</strong>
            <span className="field-hint">Bigger models are more accurate but slower. Current: {model.current}</span>
          </div>
          <div className="setup-actions">
            <select value={choice} onChange={(e) => setChoice(e.target.value)} disabled={busy}>
              {model.options.map((opt) => (
                <option key={opt} value={opt.replace(/\.pt$/i, '')}>{opt}</option>
              ))}
            </select>
            <button type="button" onClick={apply} disabled={busy || !choice}>
              <span className="btn-icon"><Ico n="download" /> Download &amp; apply</span>
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function DiscoveredRow({ device, busy, onAdd }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(() => cameraTitle(device));
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [adding, setAdding] = useState(false);

  async function add() {
    setAdding(true);
    try {
      await onAdd(device, { name: name.trim(), username: username.trim(), password });
    } finally {
      setAdding(false);
    }
  }

  return (
    <li className="setup-found-row-wrap">
      <div className="setup-found-row">
        <span className="setup-found-name">{cameraTitle(device)}<span className="field-hint">{device.host}</span></span>
        <button type="button" className="quiet" onClick={() => setOpen((o) => !o)} disabled={busy}>
          <span className="btn-icon"><Ico n={open ? 'x' : 'plus'} /> {open ? 'Cancel' : 'Add'}</span>
        </button>
      </div>
      {open ? (
        <div className="setup-cred-form">
          <label>Camera name
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" placeholder={cameraTitle(device)} />
          </label>
          <span className="field-hint">A friendly name to identify this camera (e.g. Front Door, Garage). You can rename it later in Cameras.</span>
          <label>Username
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder="e.g. admin" />
          </label>
          <label>Password
            <PasswordField value={password} onChange={setPassword} autoComplete="off" />
          </label>
          <span className="field-hint">Most cameras need a login. Leave blank only if yours has none.</span>
          <button type="button" className="setup-cred-add" onClick={add} disabled={busy || adding}>
            <span className="btn-icon"><Ico n="shield" /> {adding ? 'Adding…' : 'Add camera'}</span>
          </button>
        </div>
      ) : null}
    </li>
  );
}

function CamerasStep({ busy, saved, discovered, onScan, onAddCamera }) {
  const savedCount = (saved || []).length;
  const newDevices = (discovered || []).filter((d) => !(saved || []).some((s) => sameCamera(d, s)));
  return (
    <div className="setup-pane">
      <h2>Add your first camera</h2>
      <p>Scan your network for ONVIF cameras, then add the ones you want to monitor. Most need a username and password to stream. You can always add more later in Cameras.</p>
      <div className="setup-actions">
        <button type="button" onClick={() => onScan && onScan()} disabled={busy}>
          <span className="btn-icon"><Ico n="search" /> Scan network</span>
        </button>
        <span className="field-hint">{savedCount} camera{savedCount === 1 ? '' : 's'} added</span>
      </div>
      {newDevices.length > 0 ? (
        <ul className="setup-found-list">
          {newDevices.map((device) => (
            <DiscoveredRow key={device.xAddr || device.host} device={device} busy={busy} onAdd={onAddCamera} />
          ))}
        </ul>
      ) : (discovered && discovered.length > 0 ? <p className="field-hint">All discovered cameras are already added.</p> : null)}
    </div>
  );
}

// RecordingStep enables continuous recording and warns if the storage volume is
// nearly full (recordings auto-purge, but a full disk still stops new writes).
function RecordingStep({ busy, cameraCount, recordingDone, onDiskInfo, onEnableRecording }) {
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
        <h2>Recording</h2>
        <p className="empty">Add a camera first to turn on recording. You can skip this and set it up later.</p>
      </div>
    );
  }
  return (
    <div className="setup-pane">
      <h2>Recording</h2>
      <p>Save footage for your {cameraCount} camera{cameraCount === 1 ? '' : 's'} with 7-day retention. Fine-tune per camera later in Cameras → Recording.</p>
      <label className="setup-field">
        <span>Storage folder</span>
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="recordings" disabled={busy || recordingDone} />
      </label>
      {lowSpace ? (
        <p className="field-hint danger-text">
          ⚠ {fullest.mountpoint} is {Math.round(fullest.usedPercent)}% full — recordings may stop. Free space or choose a folder on a larger drive.
        </p>
      ) : fullest ? (
        <p className="field-hint">Largest volume in use: {fullest.mountpoint} at {Math.round(fullest.usedPercent)}%.</p>
      ) : null}
      <div className="setup-toggle-row">
        <div>
          <strong>Record continuously</strong>
          <span className="field-hint">Continuous NVR with event clips.</span>
        </div>
        <button type="button" className={recordingDone ? 'quiet' : ''} onClick={() => onEnableRecording(path)} disabled={busy || recordingDone}>
          <span className="btn-icon">{recordingDone ? <><Ico n="check-ok" /> Enabled</> : <><Ico n="film" /> Enable recording</>}</span>
        </button>
      </div>
    </div>
  );
}

// AlertsStep adds a person-detection rule and (optionally) a delivery destination so
// alerts go somewhere beyond the in-app feed.
function AlertsStep({ busy, cameraCount, alertsDone, onAddDestination, onAddAlerts }) {
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
      <h2>Alerts</h2>
      {cameraCount === 0 ? (
        <p className="empty">Add a camera first to enable AI alerts. You can set this up later.</p>
      ) : (
        <>
          <p>Get notified when a person is detected. Add a destination so alerts reach you (otherwise they only appear in the in-app feed).</p>
          <div className="setup-toggle-row">
            <div>
              <strong>Alert on people</strong>
              <span className="field-hint">Adds a person rule to every camera.</span>
            </div>
            <button type="button" className={alertsDone ? 'quiet' : ''} onClick={onAddAlerts} disabled={busy || alertsDone}>
              <span className="btn-icon">{alertsDone ? <><Ico n="check-ok" /> Added</> : <><Ico n="bell" /> Add person alerts</>}</span>
            </button>
          </div>

          <div className="setup-dest">
            <label className="setup-field">
              <span>Deliver to</span>
              <select value={type} onChange={(e) => { setType(e.target.value); setDestDone(false); }} disabled={busy || destDone}>
                <option value="webhook">Webhook</option>
                <option value="telegram">Telegram</option>
                <option value="mqtt">MQTT</option>
              </select>
            </label>
            {type === 'telegram' ? (
              <>
                <label className="setup-field"><span>Bot token</span><input value={botToken} onChange={(e) => setBotToken(e.target.value)} disabled={busy || destDone} /></label>
                <label className="setup-field"><span>Chat ID</span><input value={chatId} onChange={(e) => setChatId(e.target.value)} disabled={busy || destDone} /></label>
              </>
            ) : type === 'mqtt' ? (
              <>
                <label className="setup-field"><span>Broker URL</span><input value={brokerUrl} onChange={(e) => setBrokerUrl(e.target.value)} placeholder="tls://broker.example.com:8883" disabled={busy || destDone} /></label>
                <label className="setup-field"><span>Topic</span><input value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="alerts/camera" disabled={busy || destDone} /></label>
                <span className="field-hint">Add authentication or TLS client certificates later in Settings → Notifications.</span>
              </>
            ) : (
              <label className="setup-field"><span>Webhook URL</span><input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" disabled={busy || destDone} /></label>
            )}
            <button type="button" className={destDone ? 'quiet' : ''} onClick={addDestination} disabled={busy || destDone || !destValid}>
              <span className="btn-icon">{destDone ? <><Ico n="check-ok" /> Saved</> : <><Ico n="save" /> Save destination</>}</span>
            </button>
          </div>
          <p className="field-hint">Optional — you can add or edit destinations anytime in Settings → Notifications.</p>
        </>
      )}
    </div>
  );
}

function DoneStep({ cameraCount, recordingDone, alertsDone }) {
  return (
    <div className="setup-pane setup-done">
      <Ico n="check-ok" sz={48} />
      <h2>You&apos;re all set!</h2>
      <ul className="setup-summary">
        <li>{cameraCount} camera{cameraCount === 1 ? '' : 's'} added</li>
        <li>Continuous recording {recordingDone ? 'enabled' : 'not enabled'}</li>
        <li>Person alerts {alertsDone ? 'enabled' : 'not enabled'}</li>
      </ul>
      <p>Click Finish to open your dashboard. You can change any of this in Settings.</p>
    </div>
  );
}
