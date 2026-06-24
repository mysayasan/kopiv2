import { useState, useEffect, useRef } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay, FieldTitle } from './ui';
import { PasswordField } from './layout';
import { defaultYoloConfig, bestYoloDefaults, defaultCaptureConfig, captureModeOptions, defaultAlertNotificationConfig, alertNotificationFields, alertFieldDataKeys, builtinPayloadKeys, notificationCategories, defaultDestination, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings } from '../lib/constants';
import {iceUrlsText,textToIceUrls,decoderTransportOptions,decoderHWAccelOptions,apiBase } from '../lib/helpers';

// stockModelHints describes the speed/accuracy trade-off of each base model.
const stockModelHints = {
  'yolo11n.pt': 'Nano — fastest, least accurate (default; best for CPU / Raspberry Pi)',
  'yolo11s.pt': 'Small — a bit slower, more accurate',
  'yolo11m.pt': 'Medium — noticeably slower; GPU recommended',
  'yolo11l.pt': 'Large — slow on CPU; GPU recommended',
  'yolo11x.pt': 'Extra-large — slowest, most accurate; GPU strongly recommended',
};

// StockModelPanel picks the always-on base detection model. Known variants are
// downloaded from the net by ultralytics; a custom path can also be used.
function StockModelPanel({ authHeader, onMessage }) {
  const [info, setInfo] = useState({ current: 'yolo11n.pt', options: [] });
  const [choice, setChoice] = useState('');
  const [customPath, setCustomPath] = useState('');
  const [busy, setBusy] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function load() {
    try {
      const result = await api('/api/training/stock-model');
      setInfo({ current: result?.current || 'yolo11n.pt', options: Array.isArray(result?.options) ? result.options : [] });
      setChoice(result?.current || 'yolo11n.pt');
    } catch (_) { /* best effort */ }
  }
  useEffect(() => { load(); }, [authHeader]);

  async function apply() {
    const model = choice === '__custom__' ? customPath.trim() : choice;
    if (!model) { if (onMessage) onMessage('Choose a model or enter a custom path.'); return; }
    setBusy(true);
    if (onMessage) onMessage('Applying base model (downloading if needed)…');
    try {
      const result = await api('/api/training/stock-model', { method: 'POST', body: JSON.stringify({ model }) });
      setInfo({ current: result?.current || model, options: info.options });
      if (onMessage) onMessage(`Base model set to ${result?.current || model}. Detection reloaded.`);
    } catch (err) {
      if (onMessage) onMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  const dirty = choice !== '__custom__' ? choice !== info.current : customPath.trim() !== '';

  return (
    <section className="settings-panel span-two">
      <header>
        <h2>
          <FieldTitle info="The always-on base detection model. It detects the general COCO classes (person, vehicle, animal, …) and runs in parallel with any activated custom model. Larger variants are more accurate but much slower — on CPU/Raspberry Pi stick to nano or small.">
            Stock (base) model
          </FieldTitle>
        </h2>
        <span className="status-pill">{info.current}</span>
      </header>
      <p className="settings-hint">
        Bigger models are more accurate but slower (each frame is inferenced once per active model). Known variants are
        downloaded from the internet on first use, then cached — the device needs internet access for that one-time download.
      </p>
      <div className="settings-field-grid">
        <label>
          Model
          <select value={choice} onChange={(e) => setChoice(e.target.value)} disabled={busy}>
            {info.options.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
            <option value="__custom__">Custom path…</option>
          </select>
          <span className="field-hint">{choice === '__custom__' ? 'Point to a local .pt file on the server.' : (stockModelHints[choice] || '')}</span>
        </label>
        {choice === '__custom__' ? (
          <label>
            Custom .pt path
            <input value={customPath} onChange={(e) => setCustomPath(e.target.value)} placeholder="/path/to/model.pt" disabled={busy} />
          </label>
        ) : null}
      </div>
      <div className="settings-actions">
        <button type="button" onClick={apply} disabled={busy || !dirty}>
          <span className="btn-icon"><Ico n="download" /> Download &amp; apply</span>
        </button>
      </div>
    </section>
  );
}

// FileBrowserModal is a server-side directory picker. It navigates the host
// filesystem via GET /settings/fs/browse, lets the user drill into folders and
// click a file to select it, and returns the chosen absolute path. Used to pick the
// ffmpeg binary without typing a path. Admin-gated server-side.
function FileBrowserModal({ authHeader, initialPath, onSelect, onClose }) {
  const [listing, setListing] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function browse(path) {
    setLoading(true);
    setError(null);
    try {
      const headers = {};
      if (authHeader) headers.Authorization = authHeader;
      const resp = await fetch(`${apiBase()}/api/settings/fs/browse?path=${encodeURIComponent(path || '')}`, { credentials: 'include', headers });
      const text = await resp.text();
      let payload = null;
      if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
      if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
      setListing(payload?.data?.result ?? payload?.result ?? payload);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }
  // Seed from the directory of the current value so the user starts somewhere useful.
  useEffect(() => { browse(initialPath || ''); /* eslint-disable-next-line */ }, []);

  const entries = listing?.entries || [];
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-card file-browser" onClick={(e) => e.stopPropagation()}>
        <header className="file-browser-head">
          <h2><span className="btn-icon"><Ico n="folder" /> Choose ffmpeg</span></h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}><Ico n="x" /></button>
        </header>
        <div className="file-browser-path" title={listing?.path || ''}>{listing?.path || 'Allowed locations'}</div>
        <div className="file-browser-list">
          {loading ? (
            <p className="field-hint">Loading…</p>
          ) : error ? (
            <p className="field-hint danger-text">{error}</p>
          ) : (
            <ul>
              {listing && listing.parent !== undefined ? (
                <li>
                  <button type="button" className="file-row" onClick={() => browse(listing.parent)} disabled={!listing.path}>
                    <span className="btn-icon"><Ico n="arr-up" /> ..</span>
                  </button>
                </li>
              ) : null}
              {entries.map((item) => (
                <li key={item.path}>
                  <button
                    type="button"
                    className={`file-row${item.dir ? ' is-dir' : ''}`}
                    onClick={() => (item.dir ? browse(item.path) : onSelect(item.path))}
                  >
                    <span className="btn-icon"><Ico n={item.dir ? 'folder' : 'film'} /> {item.name}</span>
                  </button>
                </li>
              ))}
              {entries.length === 0 ? <li><span className="field-hint">Empty folder.</span></li> : null}
            </ul>
          )}
        </div>
        <p className="field-hint file-browser-hint">Click a folder to open it, or a file to select it.</p>
      </div>
    </div>
  );
}

// FfmpegInstallControl shows whether a usable ffmpeg is available and, when it is
// not found, offers an in-app download (the same background installer the first-run
// wizard uses). ffmpeg is required for live view, recording, and AI frame capture,
// so this lets an operator fix a missing video engine without leaving Settings.
// Endpoints: GET /decoder/status, POST /decoder/ffmpeg/install (+ /install/status).
function FfmpegInstallControl({ authHeader, value, onChangePath, onMessage, onRestart, onInstalledPath }) {
  const [status, setStatus] = useState(undefined); // undefined = loading
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState(false);
  const [failure, setFailure] = useState(null);
  const [browsing, setBrowsing] = useState(false);

  async function api(path, options = {}) {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }

  async function check() {
    try { setStatus(await api('/api/settings/decoder/status')); }
    catch (_) { setStatus(null); }
  }
  useEffect(() => { check(); }, [authHeader]);

  async function download() {
    setInstalling(true);
    setFailure(null);
    if (onMessage) onMessage('Downloading ffmpeg…');
    try {
      await api('/api/settings/decoder/ffmpeg/install', { method: 'POST' });
      // Poll the background job until it finishes.
      const deadline = Date.now() + 180000;
      let state = null;
      for (;;) {
        state = await api('/api/settings/decoder/ffmpeg/install/status');
        if (state?.status === 'done' || state?.status === 'failed') break;
        if (Date.now() > deadline) { state = { status: 'failed', log: 'Timed out.' }; break; }
        await new Promise((r) => setTimeout(r, 1500));
      }
      if (state?.status === 'done') {
        setInstalled(true);
        if (state.path && onInstalledPath) onInstalledPath(state.path);
        if (onMessage) onMessage('ffmpeg installed. Restart the app to apply it everywhere.');
        await check();
      } else {
        setFailure(state || { status: 'failed' });
        if (onMessage) onMessage(`ffmpeg install failed: ${state?.log || 'unknown error'}`);
      }
    } catch (err) {
      setFailure({ status: 'failed', log: err.message });
      if (onMessage) onMessage(err.message);
    } finally {
      setInstalling(false);
    }
  }

  // Status icon shown between the input and Check button. The detail (version,
  // path, or why it matters) lives in a hover/focus balloon so the row stays tidy.
  const found = status?.found;
  const tip = status === undefined ? 'Checking for ffmpeg…'
    : found ? `ffmpeg found${status.version ? ` — ${status.version}` : status.path ? ` — ${status.path}` : ''}${installed ? ' (restart to apply)' : ''}`
    : 'ffmpeg not found — required for live view, recording, and AI capture. Use Download to install it, or set the path manually.';
  const iconState = status === undefined ? 'pending' : found ? 'ok' : 'bad';

  return (
    <>
      <div className="ffmpeg-input-row">
        <div className="input-with-icon">
          <input
            value={value}
            onChange={(e) => onChangePath(e.target.value)}
            placeholder="ffmpeg"
            autoComplete="off"
          />
          <button
            type="button"
            className="input-icon-btn"
            onClick={() => setBrowsing(true)}
            title="Browse for the ffmpeg binary"
            aria-label="Browse for the ffmpeg binary"
          >
            <Ico n="folder" sz={15} />
          </button>
        </div>
        <span
          className={`ffmpeg-status-icon ${iconState}`}
          data-tip={tip}
          tabIndex={0}
          role="img"
          aria-label={tip}
        >
          <Ico n={status === undefined ? 'reload' : found ? 'check-ok' : 'x'} sz={15} />
        </span>
        <button type="button" className="quiet ffmpeg-check-btn" onClick={check} disabled={installing}>
          <span className="btn-icon"><Ico n="reload" /> Check</span>
        </button>
      </div>
      {(status && !status.found && !installed) || (installed && onRestart) || failure ? (
        <div className="ffmpeg-status-actions">
          {status && !status.found && !installed ? (
            <button type="button" onClick={download} disabled={installing}>
              <span className="btn-icon"><Ico n="download" /> {installing ? 'Downloading…' : 'Download ffmpeg'}</span>
            </button>
          ) : null}
          {installed && onRestart ? (
            <button type="button" onClick={() => onRestart()} disabled={installing}>
              <span className="btn-icon"><Ico n="reload" /> Restart now</span>
            </button>
          ) : null}
          {failure ? (
            <p className="field-hint danger-text ffmpeg-status-error">
              {failure.supported === false
                ? 'Automatic download isn’t available for this platform — install ffmpeg manually and set its path above.'
                : 'Download failed — install ffmpeg manually and set its path above.'}
            </p>
          ) : null}
        </div>
      ) : null}
      {browsing ? (
        <FileBrowserModal
          authHeader={authHeader}
          initialPath={value}
          onSelect={(path) => { onChangePath(path); setBrowsing(false); }}
          onClose={() => setBrowsing(false)}
        />
      ) : null}
    </>
  );
}

// SystemStatusPanel shows the running software version and live service health by
// polling the public version/health/liveness/readiness endpoints. It is read-only:
// it confirms what build is running and whether the API, database, cache, and the
// app's own monitors report healthy. Endpoints:
//   GET /api/version  — runtime version (SendResult envelope)
//   GET /api/health   — API namespaces health  {"ok": true}
//   GET /health       — service liveness        {"alive": true}
//   GET /ready        — service readiness        {"ok", "db", "cache", ...advisory}
function SystemStatusPanel({ authHeader, onRestart }) {
  const [restarting, setRestarting] = useState(false);
  const [version, setVersion] = useState(null);
  const [apiHealth, setApiHealth] = useState(null);
  const [live, setLive] = useState(null);
  const [ready, setReady] = useState(null);
  const [busy, setBusy] = useState(false);
  const [checkedAt, setCheckedAt] = useState(null);

  async function probe(path) {
    const headers = {};
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    const body = payload?.data?.result ?? payload?.result ?? payload;
    return { ok: resp.ok, status: resp.status, body };
  }

  async function load() {
    setBusy(true);
    const fail = (e) => ({ ok: false, status: 0, body: { message: e?.message || 'unreachable' } });
    const [v, h, l, r] = await Promise.all([
      probe('/api/version').catch(fail),
      probe('/api/health').catch(fail),
      probe('/health').catch(fail),
      probe('/ready').catch(fail),
    ]);
    setVersion(v); setApiHealth(h); setLive(l); setReady(r);
    setCheckedAt(new Date());
    setBusy(false);
  }
  useEffect(() => { load(); }, [authHeader]);

  function pill(ok, label) {
    return <strong className={`status-pill ${ok ? 'online' : 'offline'}`}>{label}</strong>;
  }
  // readyExtras surfaces the advisory keys (db, cache, machine, cameras, …) that
  // /ready merges in beyond the ok verdict, so operators see what's degraded.
  const readyExtras = ready?.body && typeof ready.body === 'object'
    ? Object.entries(ready.body).filter(([k]) => k !== 'ok')
    : [];
  const ver = version?.ok && version?.body && typeof version.body === 'object' ? version.body : null;

  return (
    <div className="settings-layout">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="monitor" /> Software version</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load} disabled={busy || restarting}>
              <span className="btn-icon"><Ico n="reload" /> Refresh</span>
            </button>
            {onRestart ? (
              <button type="button" className="quiet" onClick={() => { setRestarting(true); onRestart(); }} disabled={restarting}>
                <span className="btn-icon"><Ico n="reload" /> {restarting ? 'Restarting…' : 'Restart app'}</span>
              </button>
            ) : null}
          </div>
        </header>
        {ver ? (
          <div className="machine-metrics">
            <div className="machine-metric-card">
              <dt>Application</dt>
              <dd><strong className="status-pill">{ver.app || '—'}</strong></dd>
              <span className="field-hint">v{ver.appVersion || '—'}</span>
            </div>
            <div className="machine-metric-card">
              <dt>Shared core</dt>
              <dd><strong className="status-pill">v{ver.coreVersion || '—'}</strong></dd>
            </div>
            {ver.commit ? (
              <div className="machine-metric-card">
                <dt>Commit</dt>
                <dd><strong className="status-pill">{String(ver.commit).slice(0, 12)}</strong></dd>
              </div>
            ) : null}
            {ver.updatedAt ? (
              <div className="machine-metric-card">
                <dt>Built</dt>
                <dd><strong className="status-pill">{ver.updatedAt}</strong></dd>
              </div>
            ) : null}
          </div>
        ) : (
          <p className="settings-hint">Version unavailable{version?.body?.message ? ` — ${version.body.message}` : ''}.</p>
        )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> Service health</span></h2>
          {checkedAt ? <span className="field-hint">Checked {checkedAt.toLocaleTimeString()}</span> : null}
        </header>
        <p className="settings-hint">
          Liveness confirms the process is responding; readiness additionally checks the database and cache are
          reachable. The app's own monitors (machine, cameras) appear as advisory readiness fields — they never
          block readiness, but a degraded value is worth investigating.
        </p>
        <div className="machine-metrics">
          <div className="machine-metric-card">
            <dt>API namespaces</dt>
            <dd>{pill(apiHealth?.ok && apiHealth?.body?.ok !== false, apiHealth?.ok ? 'OK' : 'Down')}</dd>
            <span className="field-hint">/api/health</span>
          </div>
          <div className="machine-metric-card">
            <dt>Liveness</dt>
            <dd>{pill(live?.ok && live?.body?.alive !== false, live?.ok ? 'Alive' : 'Down')}</dd>
            <span className="field-hint">/health</span>
          </div>
          <div className="machine-metric-card">
            <dt>Readiness</dt>
            <dd>{pill(ready?.ok && ready?.body?.ok === true, ready?.ok && ready?.body?.ok === true ? 'Ready' : 'Not ready')}</dd>
            <span className="field-hint">/ready</span>
          </div>
          {readyExtras.map(([key, val]) => (
            <div className="machine-metric-card" key={key}>
              <dt>{key.charAt(0).toUpperCase() + key.slice(1)}</dt>
              <dd>{pill(String(val).toLowerCase() === 'up' || String(val).toLowerCase() === 'ok', String(val))}</dd>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

export function SettingsTab({
  settingsNav,
  settings,
  authHeader,
  onMessage,
  users,
  newUser,
  passwordDrafts,
  busy,
  hasChanges,
  onChange,
  onSettingsNav,
  onSave,
  onDiscard,
  onReset,
  onAutoTune,
  autoTuneResult,
  onRestart,
  onCaptureAutoConfig,
  gpuDevices,
  onCheckVisionTool,
  visionToolStatus,
  onInstallPackages,
  visionInstallResult,
  onLoadUsers,
  focusUsername,
  onNewUser,
  onCreateUser,
  onEditUser,
  onUpdateUser,
  onPasswordDraft,
  onResetPassword,
  onDeleteUser,
  notificationSettings,
  notificationHasChanges,
  onNotificationChange,
  onSaveNotification,
  onDiscardNotification,
  onTestNotification,
  onPurgeNotifications,
  healthSettings,
  healthHasChanges,
  onHealthChange,
  onSaveHealth,
  onDiscardHealth,
  machineHealthSettings,
  machineHealthHasChanges,
  onMachineHealthChange,
  onSaveMachineHealth,
  onDiscardMachineHealth,
  machineMetrics,
  onRefreshMachineMetrics,
  capacity,
  onEstimateCapacity,
  onCalibrateCapacity,
  resetAllowed,
  onSecureWipe,
}) {
  const iceServers = settings.stream.webrtc.iceServers || [];
  const capture = { ...defaultCaptureConfig, ...(settings.vision?.capture || {}),
    standalone: { ...defaultCaptureConfig.standalone, ...(settings.vision?.capture?.standalone || {}) },
    siphon: { ...defaultCaptureConfig.siphon, ...(settings.vision?.capture?.siphon || {}) } };
  const captureAuto = capture.mode === 'auto';
  const gpuDeviceOptions = Array.isArray(gpuDevices?.devices) ? gpuDevices.devices : [];
  const selectedGpuDeviceIndex = gpuDeviceOptions.findIndex(
    (item) =>
      item.value === settings.decoder.ffmpeg.hwaccelDevice &&
      (!item.hwaccel || item.hwaccel === settings.decoder.ffmpeg.hwaccel || !settings.decoder.ffmpeg.hwaccelDevice)
  );
  const gpuDeviceSelectValue =
    settings.decoder.ffmpeg.hwaccelDevice === '' ? '__default__' : selectedGpuDeviceIndex >= 0 ? String(selectedGpuDeviceIndex) : '__manual__';
  const [showManualGpuInput, setShowManualGpuInput] = useState(() => gpuDeviceSelectValue === '__manual__');
  useEffect(() => {
    if (gpuDeviceSelectValue === '__manual__') {
      setShowManualGpuInput(true);
    }
  }, [gpuDeviceSelectValue]);
  const effectiveGpuSelectValue = showManualGpuInput ? '__manual__' : gpuDeviceSelectValue;
  // When a login-security notification deep-links here, scroll the targeted
  // user's card into view and highlight it.
  const focusedUserRef = useRef(null);
  useEffect(() => {
    if (settingsNav === 'users' && focusUsername && focusedUserRef.current) {
      focusedUserRef.current.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }, [settingsNav, focusUsername, users]);
  function update(mutator) {
    onChange(mutator(settings));
  }
  function updateIceServer(index, patch) {
    update((current) => {
      const nextServers = [...(current.stream.webrtc.iceServers || [])];
      nextServers[index] = { ...nextServers[index], ...patch };
      return {
        ...current,
        stream: {
          ...current.stream,
          webrtc: { ...current.stream.webrtc, iceServers: nextServers },
        },
      };
    });
  }
  function updateMJPEGDecoder(patch) {
    update((current) => ({
      ...current,
      decoder: {
        ...current.decoder,
        mjpeg: { ...current.decoder.mjpeg, ...patch },
      },
    }));
  }
  function updateYolo(patch) {
    update((current) => ({
      ...current,
      vision: {
        ...current.vision,
        yolo: { ...(current.vision?.yolo || defaultYoloConfig), ...patch },
      },
    }));
  }
  function updateCapture(patch) {
    update((current) => ({
      ...current,
      vision: {
        ...current.vision,
        capture: { ...(current.vision?.capture || defaultCaptureConfig), ...patch },
      },
    }));
  }
  function updateCaptureStandalone(patch) {
    update((current) => {
      const capture = current.vision?.capture || defaultCaptureConfig;
      return {
        ...current,
        vision: {
          ...current.vision,
          capture: { ...capture, standalone: { ...capture.standalone, ...patch } },
        },
      };
    });
  }
  function updateCaptureSiphon(patch) {
    update((current) => {
      const capture = current.vision?.capture || defaultCaptureConfig;
      return {
        ...current,
        vision: {
          ...current.vision,
          capture: { ...capture, siphon: { ...capture.siphon, ...patch } },
        },
      };
    });
  }
  function updateFFmpegDecoder(patch) {
    update((current) => ({
      ...current,
      decoder: {
        ...current.decoder,
        ffmpeg: { ...current.decoder.ffmpeg, ...patch },
      },
    }));
  }
  function updateRecordingStorage(patch) {
    update((current) => ({
      ...current,
      recording: {
        ...(current.recording || {}),
        storage: { ...((current.recording && current.recording.storage) || {}), ...patch },
      },
    }));
  }
  function selectGPUDevice(value) {
    if (value === '__default__') {
      updateFFmpegDecoder({ hwaccelDevice: '' });
      setShowManualGpuInput(false);
      return;
    }
    if (value === '__manual__') {
      setShowManualGpuInput(true);
      return;
    }
    setShowManualGpuInput(false);
    const option = gpuDeviceOptions[Number(value)];
    if (!option) {
      return;
    }
    updateFFmpegDecoder({
      hwaccelDevice: option.value || '',
      ...(option.hwaccel ? { hwaccel: option.hwaccel } : {}),
    });
  }

  return (
    <section className="workspace settings-workspace">
      <aside className="settings-side-nav" aria-label="Settings">
        <button type="button" className={settingsNav === 'runtime' ? 'active' : 'quiet'} onClick={() => onSettingsNav('runtime')}>
          <span className="btn-icon"><Ico n="sliders" /> Runtime</span>
        </button>
        <button type="button" className={settingsNav === 'ai' ? 'active' : 'quiet'} onClick={() => onSettingsNav('ai')}>
          <span className="btn-icon"><Ico n="cpu" /> AI</span>
        </button>
        <button type="button" className={settingsNav === 'notifications' ? 'active' : 'quiet'} onClick={() => onSettingsNav('notifications')}>
          <span className="btn-icon"><Ico n="bell" /> Notifications</span>
        </button>
        <button type="button" className={settingsNav === 'health' ? 'active' : 'quiet'} onClick={() => onSettingsNav('health')}>
          <span className="btn-icon"><Ico n="wifi" /> Camera Health</span>
        </button>
        <button type="button" className={settingsNav === 'machine' ? 'active' : 'quiet'} onClick={() => onSettingsNav('machine')}>
          <span className="btn-icon"><Ico n="cpu" /> Machine Health</span>
        </button>
        <button type="button" className={settingsNav === 'users' ? 'active' : 'quiet'} onClick={() => onSettingsNav('users')}>
          <span className="btn-icon"><Ico n="user" /> Users</span>
        </button>
        <button type="button" className={settingsNav === 'system' ? 'active' : 'quiet'} onClick={() => onSettingsNav('system')}>
          <span className="btn-icon"><Ico n="monitor" /> Version &amp; Health</span>
        </button>
      </aside>

      <div className="settings-content">
        {(settingsNav === 'runtime' || settingsNav === 'ai') ? (
          <form className="settings-layout" onSubmit={onSave}>
            <FormBusyOverlay busy={busy} />
        {settingsNav === 'runtime' && (<>
        <section className="settings-panel span-two">
          <header>
            <h2>Decoder</h2>
            <button type="button" className="quiet" onClick={onAutoTune} disabled={busy}>
              <span className="btn-icon"><Ico n="wand" /> Auto Tune</span>
            </button>
          </header>
          {autoTuneResult ? (
            <div className="auto-tune-result">
              <strong>{autoTuneResult.summary}</strong>
              {Array.isArray(autoTuneResult.observations) && autoTuneResult.observations.length > 0 ? (
                <ul>
                  {autoTuneResult.observations.map((item, index) => (
                    <li key={`auto-tune-${index}`}>{item}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
          <div className="settings-field-grid">
          <label className="field-span-two">
            <FieldTitle info="Executable used for RTSP-to-MJPEG fallback and RTSP frame capture. Leave as ffmpeg to resolve from PATH, or use an absolute service-safe path.">
              FFmpeg path
            </FieldTitle>
            <FfmpegInstallControl
              authHeader={authHeader}
              value={settings.decoder.mjpeg.ffmpegPath}
              onChangePath={(path) => updateMJPEGDecoder({ ffmpegPath: path })}
              onMessage={onMessage}
              onRestart={onRestart}
              onInstalledPath={(path) => updateMJPEGDecoder({ ffmpegPath: path })}
            />
          </label>
          <label>
            <FieldTitle info="RTSP transport passed to ffmpeg. TCP is most reliable on unstable camera networks; UDP can reduce latency when packet loss is low.">
              RTSP transport
            </FieldTitle>
            <select value={settings.decoder.ffmpeg.rtspTransport} onChange={(event) => updateFFmpegDecoder({ rtspTransport: event.target.value })}>
              {decoderTransportOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label>
            <FieldTitle info="Hardware acceleration mode for ffmpeg decoding — applies to live view, standalone AI capture, and the continuous detection/recording siphon decode (offloading it from the CPU). None uses CPU software decode; auto lets ffmpeg choose; platform-specific modes need matching ffmpeg build, drivers, and hardware.">
              Hardware decode
            </FieldTitle>
            <select value={settings.decoder.ffmpeg.hwaccel} onChange={(event) => updateFFmpegDecoder({ hwaccel: event.target.value })}>
              {decoderHWAccelOptions.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label className="field-span-two">
            <FieldTitle info="Optional hardware device or GPU index passed to ffmpeg hwaccel_device, such as 0, 1, or /dev/dri/renderD128 depending on platform.">
              GPU/device
            </FieldTitle>
            <select value={effectiveGpuSelectValue} onChange={(event) => selectGPUDevice(event.target.value)}>
              <option value="__default__">Default / ffmpeg decides</option>
              {gpuDeviceOptions.map((item, index) => (
                <option key={`${item.kind || 'gpu'}-${index}-${item.value}`} value={String(index)}>
                  {item.label}
                </option>
              ))}
              <option value="__manual__">
                {settings.decoder.ffmpeg.hwaccelDevice && selectedGpuDeviceIndex < 0
                  ? `Manual: ${settings.decoder.ffmpeg.hwaccelDevice}`
                  : 'Manual entry...'}
              </option>
            </select>
            {Array.isArray(gpuDevices?.observations) && gpuDevices.observations.length > 0 ? (
              <span className="field-hint">{gpuDevices.observations[0]}</span>
            ) : null}
            {showManualGpuInput ? (
              <input
                value={settings.decoder.ffmpeg.hwaccelDevice}
                onChange={(event) => updateFFmpegDecoder({ hwaccelDevice: event.target.value })}
                placeholder="Manual device value"
                autoComplete="off"
              />
            ) : null}
          </label>
          <label>
            <FieldTitle info="Optional ffmpeg init_hw_device value for advanced setups, for example vaapi=va:/dev/dri/renderD128 or d3d11va=cam:1.">
              Init hardware device
            </FieldTitle>
            <input
              value={settings.decoder.ffmpeg.initHwDevice}
              onChange={(event) => updateFFmpegDecoder({ initHwDevice: event.target.value })}
              placeholder="vaapi=va:/dev/dri/renderD128"
              autoComplete="off"
            />
          </label>
          <label>
            <FieldTitle info="Optional decoder name passed as ffmpeg -c:v before the input, such as h264_cuvid or hevc_cuvid. Leave empty for ffmpeg auto-selection.">
              Video decoder
            </FieldTitle>
            <input
              value={settings.decoder.ffmpeg.videoDecoder}
              onChange={(event) => updateFFmpegDecoder({ videoDecoder: event.target.value })}
              placeholder="auto"
              autoComplete="off"
            />
          </label>
          <label>
            <FieldTitle info="MJPEG output quality. Lower numbers are higher quality and more CPU/bandwidth; 7 is a balanced live-view default.">
              MJPEG quality
            </FieldTitle>
            <input
              type="number"
              min="2"
              max="31"
              value={settings.decoder.mjpeg.quality}
              onChange={(event) => updateMJPEGDecoder({ quality: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Thread count used by ffmpeg while writing MJPEG output. Keep this low on small devices to protect the rest of the app.">
              MJPEG threads
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="16"
              value={settings.decoder.mjpeg.threads}
              onChange={(event) => updateMJPEGDecoder({ threads: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Bytes ffmpeg may probe before decoding. Larger values can help unusual streams but slow startup.">
              Probe size
            </FieldTitle>
            <input
              type="number"
              min="32000"
              max="50000000"
              step="1000"
              value={settings.decoder.ffmpeg.probeSize}
              onChange={(event) => updateFFmpegDecoder({ probeSize: Number(event.target.value) })}
            />
          </label>
          <label>
            <FieldTitle info="Microseconds ffmpeg may analyze stream metadata. Larger values can help odd cameras but increase first-frame delay.">
              Analyze duration
            </FieldTitle>
            <input
              type="number"
              min="0"
              max="30000000"
              step="1000"
              value={settings.decoder.ffmpeg.analyzeDuration}
              onChange={(event) => updateFFmpegDecoder({ analyzeDuration: Number(event.target.value) })}
            />
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.decoder.ffmpeg.lowDelay}
              onChange={(event) => updateFFmpegDecoder({ lowDelay: event.target.checked })}
            />
            <FieldTitle info="Passes ffmpeg low_delay flags for lower latency. Disable only when a camera behaves badly with low-latency decoding.">
              Low delay
            </FieldTitle>
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.decoder.ffmpeg.noBuffer}
              onChange={(event) => updateFFmpegDecoder({ noBuffer: event.target.checked })}
            />
            <FieldTitle info="Passes ffmpeg nobuffer flags to reduce live-view lag. Disable if the stream becomes unstable or drops too many frames.">
              No buffer
            </FieldTitle>
          </label>
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="How recorded NVR segments are stored on disk. 'Copy' keeps the camera's native codec with no re-encode (default; existing installs unchanged). 'H.264'/'H.265' re-encode each segment once at remux time on the GPU (NVENC) to shrink it — live capture and event clips always stay stream-copy. For the smallest footage with zero host cost, set the camera's own encoder to H.265 in the camera's Recording tab instead.">
                Recording storage (compression)
              </FieldTitle>
            </h2>
          </header>
          <div className="settings-grid">
            <label>
              <FieldTitle info="At-rest video codec. Copy: store the camera's codec unchanged (no host CPU/GPU). H.265 (HEVC): ~40-60% smaller, re-encoded on the GPU once per segment; browsers that can't decode HEVC are transcoded to H.264 on playback. H.264: smaller than copy for high-bitrate cameras, plays everywhere.">
                Storage codec
              </FieldTitle>
              <select
                value={(settings.recording && settings.recording.storage && settings.recording.storage.codec) || 'copy'}
                onChange={(event) => updateRecordingStorage({ codec: event.target.value })}
              >
                <option value="copy">Copy (no re-encode — default)</option>
                <option value="hevc">H.265 / HEVC (smallest)</option>
                <option value="h264">H.264 (compatible)</option>
              </select>
            </label>
            <label>
              <FieldTitle info="NVENC constant-quality (CQ) target used when re-encoding. Lower = better quality and larger files; 23-28 is typical. 0 uses the built-in default (26). Ignored in Copy mode.">
                Re-encode quality (CQ)
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="51"
                value={(settings.recording && settings.recording.storage && settings.recording.storage.quality) || 0}
                onChange={(event) => updateRecordingStorage({ quality: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="Maximum simultaneous GPU (NVENC) encode sessions shared by remux-time re-encoding and playback transcode. Match your GPU's session cap so it is never oversubscribed. 0 uses the default (2).">
                Max concurrent GPU encodes
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="8"
                value={(settings.recording && settings.recording.storage && settings.recording.storage.maxConcurrentEncodes) || 0}
                onChange={(event) => updateRecordingStorage({ maxConcurrentEncodes: Number(event.target.value) })}
              />
            </label>
          </div>
          <p className="field-hint" style={{marginTop:'8px'}}>
            Codec/quality changes apply to newly recorded segments after you re-save a camera's recording config or restart the server. The concurrency limit applies on restart.
          </p>
        </section>
        </>)}

        {settingsNav === 'ai' && (<>
        <StockModelPanel authHeader={authHeader} onMessage={onMessage} />
        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="YOLO inference parameters sent to the AI worker on every frame. 0 or disabled means the worker uses its own env-var default. Changes take effect immediately without a restart.">
                YOLO Inference Tuning
              </FieldTitle>
            </h2>
            <button
              type="button"
              className="quiet"
              title="Apply best-practice defaults: conf=0.20, IOU=0.35, imgsz=640, maxDet=100, augment on"
              onClick={() => updateYolo(bestYoloDefaults)}
            >
              <span className="btn-icon"><Ico n="wand" /> Best Calibration</span>
            </button>
          </header>
          <div className="settings-field-grid">
            <label>
              <FieldTitle info="YOLO confidence threshold override (0–1). 0 uses the worker default (MYMATASAN_YOLO_CONF env var, usually 0.25). Lower values detect more objects at the cost of more false positives. Recommended: 0.15–0.20 for hard-to-detect poses like back-facing persons.">
                Confidence override
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1"
                step="0.01"
                value={settings.vision?.yolo?.conf ?? 0}
                onChange={(event) => updateYolo({ conf: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="NMS IOU threshold override (0–1). 0 uses the YOLO default (0.45). Lower values keep more overlapping bounding boxes, reducing suppression of back-facing or partially-occluded persons. Recommended: 0.3–0.4.">
                IOU threshold override
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1"
                step="0.01"
                value={settings.vision?.yolo?.iou ?? 0}
                onChange={(event) => updateYolo({ iou: Number(event.target.value) })}
              />
            </label>
            <label>
              <FieldTitle info="Inference image size override in pixels (0 uses env default, e.g. 640). Larger sizes improve detection of small or distant objects but are slower. Raspberry Pi 4 (1–4 GB RAM): use 320 or 480 — sizes above 640 may run out of memory. Jetson Nano: 480–640 is safe. x86/desktop: 640–1280.">
                Image size override
              </FieldTitle>
              <select
                value={settings.vision?.yolo?.imgsz ?? 0}
                onChange={(event) => updateYolo({ imgsz: Number(event.target.value) })}
              >
                <option value={0}>Default (env var)</option>
                <option value={320}>320</option>
                <option value={416}>416</option>
                <option value={480}>480</option>
                <option value={640}>640</option>
                <option value={960}>960</option>
                <option value={1280}>1280</option>
              </select>
            </label>
            <label>
              <FieldTitle info="Maximum detections per image (0 uses YOLO default of 300). Increase if you expect many objects in the frame.">
                Max detections override
              </FieldTitle>
              <input
                type="number"
                min="0"
                max="1000"
                step="10"
                value={settings.vision?.yolo?.maxDet ?? 0}
                onChange={(event) => updateYolo({ maxDet: Number(event.target.value) })}
              />
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.vision?.yolo?.augment === true}
                onChange={(event) => updateYolo({ augment: event.target.checked })}
              />
              <FieldTitle info="Enable test-time augmentation (TTA): YOLO runs inference with flipped and scaled copies of the image and merges the results. This is the single most effective setting for detecting back-facing, crouching, or partially-occluded persons. Roughly doubles inference time. Raspberry Pi 4: adds ~10–30 s per frame — only enable if accuracy matters more than speed. Jetson/x86: typically adds 1–3 s.">
                Augment (TTA — best for back-facing detection)
              </FieldTitle>
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={settings.vision?.yolo?.half === true}
                onChange={(event) => updateYolo({ half: event.target.checked })}
              />
              <FieldTitle info="Use FP16 half-precision inference. Only effective on CUDA GPUs (Jetson, NVIDIA desktop). Automatically ignored on CPU — will not crash on Raspberry Pi or other ARM/CPU-only devices. Reduces memory usage and can increase throughput on GPU, but may slightly reduce detection accuracy.">
                Half precision (GPU only — safe to enable anywhere)
              </FieldTitle>
            </label>
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="Controls how the AI detector sources frames per camera. Auto and Siphon keep a continuous decoded stream feeding the detector — off the recorder when recording is on, or off a dedicated detection-only stream when recording is off — so detection stays immediate. Standalone opens a fresh one-frame RTSP grab each interval (slower; ~2–3s per frame). For critical/industrial use pick Auto (default) or Siphon.">
                Capture
              </FieldTitle>
              {capture.mode === 'auto' ? <span className="auto-badge">Auto</span> : null}
            </h2>
            <button
              type="button"
              className="quiet"
              title="Detect local hardware (GPU/CPU and camera count) and apply recommended capture parameters."
              onClick={onCaptureAutoConfig}
              disabled={busy}
            >
              <span className="btn-icon"><Ico n="wand" /> Auto Config</span>
            </button>
          </header>
          <div className="settings-field-grid">
            <label>
              <FieldTitle info="Frame source. Auto: read fresh decoded frames off the continuous stream (recorder when recording is on, else a dedicated detection-only stream), falling back to a one-shot grab only if no frame is ready yet. Siphon: always read off the continuous stream. Standalone: AI opens its own one-frame RTSP grab each interval (slower). Auto/Siphon give immediate detection even with recording off.">
                Mode
              </FieldTitle>
              <select value={capture.mode} onChange={(event) => updateCapture({ mode: event.target.value })}>
                {captureModeOptions.map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label>
              <FieldTitle info="Per-camera sampling interval in milliseconds — how often each camera is sampled for detection. Lower values detect faster but cost more CPU/GPU. In Auto mode these are set by Auto Config.">
                Interval (ms)
              </FieldTitle>
              <input
                type="number"
                min="250"
                max="60000"
                step="250"
                value={capture.intervalMs}
                onChange={(event) => updateCapture({ intervalMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Downscaled frame width in pixels fed to detection. 640 is a good accuracy/speed balance; smaller is faster.">
                Frame width (px)
              </FieldTitle>
              <input
                type="number"
                min="160"
                max="1920"
                step="32"
                value={capture.frameWidth}
                onChange={(event) => updateCapture({ frameWidth: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Standalone only. Deadline in milliseconds for a self-opened one-frame RTSP grab before it is abandoned.">
                Standalone capture timeout (ms)
              </FieldTitle>
              <input
                type="number"
                min="500"
                max="60000"
                step="500"
                value={capture.standalone.captureTimeoutMs}
                onChange={(event) => updateCaptureStandalone({ captureTimeoutMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Siphon only. Frames-per-second the recorder tee produces for the detector. Higher gives fresher frames at more CPU cost.">
                Siphon FPS
              </FieldTitle>
              <input
                type="number"
                min="1"
                max="30"
                value={capture.siphon.fps}
                onChange={(event) => updateCaptureSiphon({ fps: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
            <label>
              <FieldTitle info="Siphon only. How old (ms) a siphoned frame may be before Auto mode falls back to a standalone grab. Typically about twice the interval.">
                Siphon stale limit (ms)
              </FieldTitle>
              <input
                type="number"
                min="500"
                max="120000"
                step="500"
                value={capture.siphon.staleLimitMs}
                onChange={(event) => updateCaptureSiphon({ staleLimitMs: Number(event.target.value) })}
                readOnly={captureAuto}
                disabled={captureAuto}
              />
            </label>
          </div>
          {captureAuto ? (
            <p className="settings-hint">In Auto mode these parameters are managed for you. Use Auto Config to recompute them from detected hardware, or switch Mode to Siphon/Standalone to edit them by hand.</p>
          ) : null}
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="Checks the configured AI detector command, Python packages, worker script, model file, and whether native fallback can keep non-AI detection available.">
                AI Tool
              </FieldTitle>
            </h2>
            <button type="button" className="quiet" onClick={onCheckVisionTool} disabled={busy}>
              <span className="btn-icon"><Ico n="check-ok" /> Check AI Tool</span>
            </button>
          </header>
          {visionToolStatus ? (
            <div className="auto-tune-result">
              <strong>{visionToolStatus.summary}</strong>
              <dl className="tool-status-grid">
                <div>
                  <dt>Mode</dt>
                  <dd>{visionToolStatus.mode || 'motion'}</dd>
                </div>
                <div>
                  <dt>AI ready</dt>
                  <dd>{visionToolStatus.available ? 'Yes' : 'No'}</dd>
                </div>
                <div>
                  <dt>Python</dt>
                  <dd>{visionToolStatus.pythonVersion || 'Not detected'}</dd>
                </div>
                <div>
                  <dt>AI packages</dt>
                  <dd>{(() => {
                    if (visionToolStatus.packagesAvailable) return '✓ ultralytics  ✓ cv2  ✓ torch';
                    const missing = visionToolStatus.missingPackages || [];
                    if (missing.length) {
                      return ['ultralytics', 'cv2', 'torch'].map((p) => `${missing.includes(p) ? '✗' : '✓'} ${p}`).join('  ');
                    }
                    return visionToolStatus.packageError || 'Not checked';
                  })()}</dd>
                </div>
                <div>
                  <dt>Native fallback</dt>
                  <dd>{visionToolStatus.nativeFallback ? 'Available' : 'Disabled'}</dd>
                </div>
                <div>
                  <dt>ByteTrack tracker</dt>
                  <dd>{visionToolStatus.trackerAvailable ? 'Available' : 'Not installed (optional — install lapx for ARM/Pi)'}</dd>
                </div>
                <div>
                  <dt>Command</dt>
                  <dd>{visionToolStatus.commandPath || 'Not found'}</dd>
                </div>
                <div>
                  <dt>Worker</dt>
                  <dd>{visionToolStatus.workerPath || 'Not configured'}</dd>
                </div>
                <div>
                  <dt>Model</dt>
                  <dd>{visionToolStatus.modelPath || 'Not configured'}</dd>
                </div>
              </dl>
              {Array.isArray(visionToolStatus.observations) && visionToolStatus.observations.length > 0 ? (
                <ul>
                  {visionToolStatus.observations.map((item, index) => (
                    <li key={`vision-tool-${index}`}>{item}</li>
                  ))}
                </ul>
              ) : null}
              {Array.isArray(visionToolStatus.installHints) && visionToolStatus.installHints.length > 0 ? (
                <div className="install-hints">
                  <p><strong>Missing packages — how to fix:</strong></p>
                  <ul>
                    {visionToolStatus.installHints.map((hint) => (
                      <li key={hint.importName}>
                        <code>{hint.importName}</code>
                        {hint.manual ? (
                          <span> — manual install required. {hint.note}</span>
                        ) : (
                          <span>
                            {' — '}
                            <code>{hint.command}</code>
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                  {visionToolStatus.installHints.some((h) => !h.manual) ? (
                    <button type="button" className="quiet" onClick={onInstallPackages} disabled={busy}>
                      <span className="btn-icon"><Ico n="download" /> Install missing packages</span>
                    </button>
                  ) : null}
                  {visionInstallResult ? (
                    <div className="install-result">
                      <strong>{visionInstallResult.success ? 'Install succeeded.' : 'Install failed.'}</strong>
                      {Array.isArray(visionInstallResult.observations) && visionInstallResult.observations.length > 0 ? (
                        <ul>
                          {visionInstallResult.observations.map((item, index) => (
                            <li key={`install-obs-${index}`}>{item}</li>
                          ))}
                        </ul>
                      ) : null}
                      {visionInstallResult.output ? (
                        <pre className="install-output">{visionInstallResult.output}</pre>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </section>
        </>)}

        {settingsNav === 'runtime' && (<>
        <section className="settings-panel">
          <header>
            <h2>Live Stream</h2>
          </header>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.stream.webrtc.enabled}
              onChange={(event) =>
                update((current) => ({
                  ...current,
                  stream: {
                    ...current.stream,
                    webrtc: { ...current.stream.webrtc, enabled: event.target.checked },
                  },
                }))
              }
            />
            WebRTC
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={settings.stream.mjpegFallback.enabled}
              onChange={(event) =>
                update((current) => ({
                  ...current,
                  stream: {
                    ...current.stream,
                    mjpegFallback: { enabled: event.target.checked },
                  },
                }))
              }
            />
            MJPEG fallback
          </label>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>ICE Servers</h2>
            <button
              type="button"
              className="quiet"
              onClick={() =>
                update((current) => ({
                  ...current,
                  stream: {
                    ...current.stream,
                    webrtc: {
                      ...current.stream.webrtc,
                      iceServers: [...(current.stream.webrtc.iceServers || []), { urls: [], username: '', credential: '' }],
                    },
                  },
                }))
              }
              disabled={busy}
            >
              Add Server
            </button>
          </header>
          <div className="ice-list">
            {iceServers.length === 0 ? <p className="empty">No STUN/TURN servers configured.</p> : null}
            {iceServers.map((server, index) => (
              <div className="ice-row" key={`ice-${index}`}>
                <label>
                  URLs
                  <textarea
                    value={iceUrlsText(server)}
                    onChange={(event) => updateIceServer(index, { urls: textToIceUrls(event.target.value) })}
                    placeholder="stun:stun.example.com:3478"
                  />
                </label>
                <label>
                  Username
                  <input
                    value={server.username || ''}
                    onChange={(event) => updateIceServer(index, { username: event.target.value })}
                    autoComplete="off"
                  />
                </label>
                <label>
                  Credential
                  <PasswordField
                    value={server.credential || ''}
                    onChange={(credential) => updateIceServer(index, { credential })}
                    autoComplete="off"
                  />
                </label>
                <button
                  type="button"
                  className="quiet danger-text"
                  onClick={() =>
                    update((current) => ({
                      ...current,
                      stream: {
                        ...current.stream,
                        webrtc: {
                          ...current.stream.webrtc,
                          iceServers: (current.stream.webrtc.iceServers || []).filter((_, itemIndex) => itemIndex !== index),
                        },
                      },
                    }))
                  }
                  disabled={busy}
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
        </section>
        </>)}

        <div className="settings-actions">
          <button type="submit" disabled={busy || !hasChanges}>
            <span className="btn-icon"><Ico n="save" /> Save Settings</span>
          </button>
          <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
            <span className="btn-icon"><Ico n="undo" /> Discard Changes</span>
          </button>
          <button type="button" className="quiet" onClick={onReset} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> Reset Defaults</span>
          </button>
        </div>
          </form>
        ) : null}

        {settingsNav === 'users' ? (
          <div className="settings-layout">
          <section className="settings-panel span-two">
            <header>
              <h2>Add user</h2>
            </header>
            <p className="settings-hint">Create a local sign-in account. Admins can manage users and settings; non-admins get view access.</p>
            <form onSubmit={onCreateUser}>
              <div className="settings-field-grid">
                <label>
                  Username
                  <input
                    value={newUser.username}
                    onChange={(event) => onNewUser({ ...newUser, username: event.target.value })}
                    autoComplete="off"
                    required
                  />
                </label>
                <label>
                  Display name
                  <input
                    value={newUser.displayName}
                    onChange={(event) => onNewUser({ ...newUser, displayName: event.target.value })}
                    autoComplete="off"
                  />
                </label>
                <label>
                  Password
                  <PasswordField
                    value={newUser.password}
                    onChange={(password) => onNewUser({ ...newUser, password })}
                    autoComplete="new-password"
                  />
                </label>
                <label className="check-row">
                  <input
                    type="checkbox"
                    checked={newUser.isAdmin}
                    onChange={(event) => onNewUser({ ...newUser, isAdmin: event.target.checked })}
                  />
                  Administrator
                </label>
              </div>
              <div className="settings-actions">
                <button type="submit" disabled={busy}>
                  <span className="btn-icon"><Ico n="user-plus" /> Add User</span>
                </button>
              </div>
            </form>
          </section>

          <section className="settings-panel span-two">
            <header>
              <h2>Users</h2>
              <button type="button" className="quiet" onClick={onLoadUsers} disabled={busy}>
                <span className="btn-icon"><Ico n="refresh" /> Reload</span>
              </button>
            </header>
            <div className="user-list">
              {users.length === 0 ? <p className="empty">No local users loaded.</p> : null}
              {users.map((user) => {
                const isFocused = focusUsername && user.username === focusUsername;
                return (
                <article
                  className={`user-card${isFocused ? ' user-card--focused' : ''}`}
                  key={user.id || user.username}
                  ref={isFocused ? focusedUserRef : null}
                >
                  <div className="user-card-head">
                    <Ico n="user" sz={16} />
                    <span className="user-card-name">{user.displayName || user.username}</span>
                    {user.isAdmin ? <span className="user-badge user-badge--admin">Admin</span> : null}
                    <span className={`user-badge ${user.isActive ? 'user-badge--active' : 'user-badge--inactive'}`}>
                      {user.isActive ? 'Active' : 'Inactive'}
                    </span>
                    {user.mustChangePassword ? <span className="user-badge user-badge--warn">Password change pending</span> : null}
                  </div>
                  <div className="settings-field-grid">
                    <label>
                      Username
                      <input
                        value={user.username || ''}
                        onChange={(event) => onEditUser(user.id, { username: event.target.value })}
                        autoComplete="off"
                      />
                    </label>
                    <label>
                      Display name
                      <input
                        value={user.displayName || ''}
                        onChange={(event) => onEditUser(user.id, { displayName: event.target.value })}
                        autoComplete="off"
                      />
                    </label>
                    <label>
                      New password
                      <PasswordField
                        value={passwordDrafts[user.id] || ''}
                        onChange={(password) => onPasswordDraft(user.id, password)}
                        autoComplete="new-password"
                        placeholder="Leave blank to keep current"
                      />
                    </label>
                    <div className="user-card-toggles">
                      <label className="check-row">
                        <input
                          type="checkbox"
                          checked={Boolean(user.isAdmin)}
                          onChange={(event) => onEditUser(user.id, { isAdmin: event.target.checked })}
                        />
                        Administrator
                      </label>
                      <label className="check-row">
                        <input
                          type="checkbox"
                          checked={Boolean(user.isActive)}
                          onChange={(event) => onEditUser(user.id, { isActive: event.target.checked })}
                        />
                        Active
                      </label>
                    </div>
                  </div>
                  <div className="user-actions">
                    <button type="button" onClick={() => onUpdateUser(user)} disabled={busy}>
                      <span className="btn-icon"><Ico n="save" /> Save</span>
                    </button>
                    <button
                      type="button"
                      className="quiet"
                      onClick={() => onResetPassword(user)}
                      disabled={busy || !(passwordDrafts[user.id] || '').trim()}
                    >
                      <span className="btn-icon"><Ico n="key" /> Reset Password</span>
                    </button>
                    <button type="button" className="quiet danger-text" onClick={() => onDeleteUser(user)} disabled={busy}>
                      <span className="btn-icon"><Ico n="trash" /> Delete</span>
                    </button>
                  </div>
                </article>
                );
              })}
            </div>
          </section>
          </div>
        ) : null}

        {settingsNav === 'notifications' ? (
          <NotificationSettingsPanel
            settings={notificationSettings}
            busy={busy}
            hasChanges={notificationHasChanges}
            onChange={onNotificationChange}
            onSave={onSaveNotification}
            onDiscard={onDiscardNotification}
            onTest={onTestNotification}
            onPurgeExpired={onPurgeNotifications}
          />
        ) : null}

        {settingsNav === 'health' ? (
          <HealthSettingsPanel
            settings={healthSettings}
            busy={busy}
            hasChanges={healthHasChanges}
            onChange={onHealthChange}
            onSave={onSaveHealth}
            onDiscard={onDiscardHealth}
          />
        ) : null}

        {settingsNav === 'machine' ? (
          <MachineHealthSettingsPanel
            settings={machineHealthSettings}
            busy={busy}
            hasChanges={machineHealthHasChanges}
            metrics={machineMetrics}
            onChange={onMachineHealthChange}
            onSave={onSaveMachineHealth}
            onDiscard={onDiscardMachineHealth}
            onRefreshMetrics={onRefreshMachineMetrics}
            capacity={capacity}
            onEstimateCapacity={onEstimateCapacity}
            onCalibrateCapacity={onCalibrateCapacity}
            resetAllowed={resetAllowed}
            onSecureWipe={onSecureWipe}
          />
        ) : null}

        {settingsNav === 'system' ? (
          <SystemStatusPanel authHeader={authHeader} onRestart={onRestart} />
        ) : null}
      </div>
    </section>
  );
}

export const SEVERITY_OPTIONS = [
  { value: 'info', label: 'Info and above' },
  { value: 'warning', label: 'Warning and above' },
  { value: 'critical', label: 'Critical only' },
];

export function NotificationSettingsPanel({ settings, busy, hasChanges, onChange, onSave, onDiscard, onTest, onPurgeExpired }) {
  const retention = settings.retention || defaultNotificationSettings.retention;
  const destinations = Array.isArray(settings.destinations) ? settings.destinations : [];
  function patch(section, values) {
    onChange({ ...settings, [section]: { ...settings[section], ...values } });
  }
  function setDestinations(next) {
    onChange({ ...settings, destinations: next });
  }
  function addDestination(type) {
    setDestinations([...destinations, defaultDestination(type)]);
  }
  function updateDestination(index, values) {
    setDestinations(destinations.map((d, i) => (i === index ? { ...d, ...values } : d)));
  }
  function removeDestination(index) {
    setDestinations(destinations.filter((_, i) => i !== index));
  }
  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="send" /> Delivery Destinations</span></h2>
          <button type="button" className="quiet" onClick={() => onTest('destinations')} disabled={busy}>
            <span className="btn-icon"><Ico n="send" /> Send Test</span>
          </button>
        </header>
        <p className="settings-hint">
          Each destination delivers independently — its own channel, severity floor, which notification types it
          receives, which detection fields it includes, and custom fields. The in-app feed always shows every alert in
          full. Test sends a <strong>System</strong> notification to destinations subscribed to it.
        </p>
        {destinations.length === 0 ? (
          <p className="settings-hint">No destinations yet — add a webhook or Telegram below.</p>
        ) : null}
        {destinations.map((dest, index) => (
          <DestinationCard
            key={dest.id || `new-${index}`}
            dest={dest}
            busy={busy}
            onChange={(values) => updateDestination(index, values)}
            onRemove={() => removeDestination(index)}
          />
        ))}
        <div className="action-row">
          <button type="button" className="quiet" onClick={() => addDestination('webhook')} disabled={busy}>
            <span className="btn-icon"><Ico n="wifi" /> Add webhook</span>
          </button>
          <button type="button" className="quiet" onClick={() => addDestination('telegram')} disabled={busy}>
            <span className="btn-icon"><Ico n="send" /> Add Telegram</span>
          </button>
          <button type="button" className="quiet" onClick={() => addDestination('mqtt')} disabled={busy}>
            <span className="btn-icon"><Ico n="wifi" /> Add MQTT</span>
          </button>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="trash" /> Retention</span></h2>
          {onPurgeExpired ? (
            <button
              type="button"
              className="quiet"
              disabled={busy || !(retention.days > 0)}
              title={retention.days > 0
                ? `Delete AI detections older than ${retention.days} day(s) now${retention.onlyRead ? ' (read only)' : ''}.`
                : 'Set a retention of at least 1 day to purge.'}
              onClick={() => {
                if (window.confirm(`Delete AI detections older than ${retention.days} day(s)${retention.onlyRead ? ' that have been read' : ''}? This cannot be undone.`)) {
                  onPurgeExpired({ days: retention.days, onlyRead: !!retention.onlyRead });
                }
              }}
            >
              <span className="btn-icon"><Ico n="trash" /> Purge expired now</span>
            </button>
          ) : null}
        </header>
        <p className="settings-hint">Old notifications are purged automatically. The in-app feed and live stream are always on and need no configuration.</p>
        <div className="settings-grid">
          <label>
            Keep for (days)
            <input
              type="number"
              min="0"
              value={retention.days}
              onChange={(event) => patch('retention', { days: Number(event.target.value) })}
            />
            <span className="settings-hint">0 disables automatic purging.</span>
          </label>
          <label>
            Purge interval (hours)
            <input
              type="number"
              min="1"
              value={retention.intervalHours}
              onChange={(event) => patch('retention', { intervalHours: Number(event.target.value) })}
            />
            <span className="settings-hint">Interval changes apply after restart.</span>
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={retention.onlyRead}
              onChange={(event) => patch('retention', { onlyRead: event.target.checked })}
            />
            Only purge read notifications
          </label>
        </div>
      </section>

      <div className="settings-actions">
        <button type="submit" disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="save" /> Save Settings</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="undo" /> Discard Changes</span>
        </button>
      </div>
    </form>
  );
}

// WEBHOOK_PAYLOAD_SAMPLE is an illustrative webhook body shown in the destination
// card so integrators see the shape without leaving the app. Snapshot/base64 and
// the per-destination data.* keys appear only when that destination enables them.
const WEBHOOK_PAYLOAD_SAMPLE = `{
  "id": "9f2c1a7e-…",
  "category": "vision.alert",
  "severity": "critical",
  "title": "Fire detected — Kitchen",
  "body": "Front Gate • fire • 92% confidence\\nsite: Front Gate",
  "source": "vision-monitor",
  "cameraId": 3,
  "refType": "alert_event",
  "refId": 481,
  "link": "/api/vision/alerts/481/snapshot",
  "data": {
    "alertId": 481,
    "ruleId": 12,
    "cameraName": "Front Gate",
    "detectionType": "presence",
    "ruleName": "Fire detected — Kitchen",
    "label": "fire",
    "confidence": 0.92,
    "boundingBox": "[0.41,0.22,0.18,0.30]",
    "zonePolygon": "[[0.1,0.1],[0.9,0.1],[0.9,0.9],[0.1,0.9]]",
    "snapshotPath": "recordings/cam3/snapshots/481.jpg",
    "site": "Front Gate"
  },
  "createdAt": 1781779140,
  "snapshotBase64": "/9j/4AAQSkZJRgABAQAA…",
  "snapshotContentType": "image/jpeg",
  "snapshotFilename": "alert-481.jpg"
}`;

// DestinationCard edits one notification destination: its channel/target,
// severity floor, category subscription, per-destination detection fields, and
// static custom fields. When a custom field key collides with a built-in/AI
// field, that field's toggle is disabled so the user sees the stock field is
// bypassed (custom wins).
function DestinationCard({ dest, busy, onChange, onRemove }) {
  const isMqtt = dest.type === 'mqtt';
  const isTelegram = dest.type === 'telegram';
  const fields = dest.fields || defaultAlertNotificationConfig;
  const categories = Array.isArray(dest.categories) ? dest.categories : [];
  const customFields = Array.isArray(dest.customFields) ? dest.customFields : [];
  const mqtt = dest.mqtt || {};
  const disabled = busy || dest.enabled === false;
  // Payload keys claimed by custom fields, used to disable the matching toggles.
  const customKeys = new Set(customFields.map((f) => String(f.key || '').trim()).filter(Boolean));
  const reservedKeys = new Set([...builtinPayloadKeys, ...Object.values(alertFieldDataKeys)]);

  function setField(key, value) {
    onChange({ fields: { ...fields, [key]: value } });
  }
  function setMqtt(values) {
    onChange({ mqtt: { ...mqtt, ...values } });
  }
  function toggleCategory(cat, on) {
    const next = on
      ? Array.from(new Set([...categories, cat]))
      : categories.filter((c) => c !== cat);
    onChange({ categories: next });
  }
  function setCustom(index, values) {
    onChange({ customFields: customFields.map((f, i) => (i === index ? { ...f, ...values } : f)) });
  }

  return (
    <div className="dest-card">
      <div className="dest-card-head">
        <input
          className="dest-name"
          value={dest.name || ''}
          onChange={(event) => onChange({ name: event.target.value })}
          placeholder="Destination name"
          disabled={busy}
        />
        <span className={`class-source-badge ${dest.type}`}>{dest.type}</span>
        <label className="check-row compact">
          <input type="checkbox" checked={dest.enabled !== false} onChange={(event) => onChange({ enabled: event.target.checked })} disabled={busy} />
          Enabled
        </label>
        <button type="button" className="quiet danger" onClick={onRemove} disabled={busy}>Remove</button>
      </div>

      {isMqtt ? (
        <>
          <div className="settings-grid">
            <label>
              Broker URL
              <input value={mqtt.brokerUrl || ''} onChange={(event) => setMqtt({ brokerUrl: event.target.value })} placeholder="ssl://broker.example.com:8883" autoComplete="off" disabled={disabled} />
            </label>
            <label>
              <FieldTitle info="Publish topic. Supports {{token}} placeholders from the payload data (cameraName, alertId, ruleId, detectionType, and label/confidence/ruleName when those fields are enabled) plus cameraId, category, severity. A token that resolves to nothing collapses its level (e.g. .../{{cameraId}} on a Test → no trailing slash).">
                Topic
              </FieldTitle>
              <input value={mqtt.topic || ''} onChange={(event) => setMqtt({ topic: event.target.value })} placeholder="matasan/alerts/{'{{cameraName}}'}" autoComplete="off" disabled={disabled} />
            </label>
            <label>
              Client ID
              <input value={mqtt.clientId || ''} onChange={(event) => setMqtt({ clientId: event.target.value })} placeholder="mymatasan-1 (optional)" autoComplete="off" disabled={disabled} />
            </label>
            <label>
              QoS
              <select value={Number(mqtt.qos ?? 1)} onChange={(event) => setMqtt({ qos: Number(event.target.value) })} disabled={disabled}>
                <option value={0}>0 — at most once</option>
                <option value={1}>1 — at least once</option>
                <option value={2}>2 — exactly once</option>
              </select>
            </label>
            <label>
              Username
              <input value={mqtt.username || ''} onChange={(event) => setMqtt({ username: event.target.value })} autoComplete="off" disabled={disabled} />
            </label>
            <label>
              Password
              <PasswordField value={mqtt.password || ''} onChange={(password) => setMqtt({ password })} autoComplete="off" disabled={disabled} />
            </label>
          </div>
          <label className="check-row">
            <input type="checkbox" checked={Boolean(mqtt.retain)} onChange={(event) => setMqtt({ retain: event.target.checked })} disabled={disabled} />
            Retain last message on the topic
          </label>
          <fieldset className="dest-group">
            <legend>
              <FieldTitle info="TLS for ssl:// brokers. Paste PEM contents. CA verifies the broker; client certificate + key enable mutual-TLS (client-certificate auth).">
                TLS / client certificate
              </FieldTitle>
            </legend>
            <label>
              CA certificate (PEM)
              <textarea rows="3" value={mqtt.caCert || ''} onChange={(event) => setMqtt({ caCert: event.target.value })} placeholder="-----BEGIN CERTIFICATE-----" disabled={disabled} />
            </label>
            <div className="settings-grid">
              <label>
                Client certificate (PEM)
                <textarea rows="3" value={mqtt.clientCert || ''} onChange={(event) => setMqtt({ clientCert: event.target.value })} placeholder="-----BEGIN CERTIFICATE-----" disabled={disabled} />
              </label>
              <label>
                Client key (PEM)
                <textarea rows="3" value={mqtt.clientKey || ''} onChange={(event) => setMqtt({ clientKey: event.target.value })} placeholder="-----BEGIN PRIVATE KEY-----" disabled={disabled} />
              </label>
            </div>
            <label className="check-row">
              <input type="checkbox" checked={Boolean(mqtt.insecureSkipVerify)} onChange={(event) => setMqtt({ insecureSkipVerify: event.target.checked })} disabled={disabled} />
              Skip broker certificate verification (insecure)
            </label>
          </fieldset>
          <details className="dest-sample">
            <summary>Sample payload</summary>
            <pre className="install-output">{WEBHOOK_PAYLOAD_SAMPLE}</pre>
          </details>
        </>
      ) : isTelegram ? (
        <div className="settings-grid">
          <label>
            Bot token
            <PasswordField value={dest.botToken || ''} onChange={(botToken) => onChange({ botToken })} placeholder="123456:ABC-DEF..." autoComplete="off" disabled={disabled} />
          </label>
          <label>
            Chat ID
            <input value={dest.chatId || ''} onChange={(event) => onChange({ chatId: event.target.value })} placeholder="-1001234567890" autoComplete="off" disabled={disabled} />
          </label>
        </div>
      ) : (
        <>
          <label>
            Webhook URL
            <input value={dest.url || ''} onChange={(event) => onChange({ url: event.target.value })} type="url" placeholder="https://hooks.example.com/..." autoComplete="off" disabled={disabled} />
          </label>
          <details className="dest-sample">
            <summary>Sample payload</summary>
            <pre className="install-output">{WEBHOOK_PAYLOAD_SAMPLE}</pre>
          </details>
        </>
      )}

      <label>
        Minimum severity
        <select value={dest.minSeverity || 'warning'} onChange={(event) => onChange({ minSeverity: event.target.value })} disabled={disabled}>
          {SEVERITY_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
      </label>

      <fieldset className="dest-group">
        <legend>Receives</legend>
        {notificationCategories.map(([value, label, help]) => (
          <label className="check-row" key={value} title={help}>
            <input
              type="checkbox"
              checked={categories.length === 0 || categories.includes(value)}
              onChange={(event) => toggleCategory(value, event.target.checked)}
              disabled={busy}
            />
            {label}
          </label>
        ))}
        <span className="field-hint">None checked = all notification types.</span>
      </fieldset>

      <fieldset className="dest-group">
        <legend>Detection fields (AI alerts)</legend>
        {alertNotificationFields.map(([key, label, help]) => {
          const overridden = customKeys.has(alertFieldDataKeys[key]);
          return (
            <label className="check-row" key={key} title={overridden ? `Overridden by a custom field named "${alertFieldDataKeys[key]}"` : help}>
              <input
                type="checkbox"
                checked={!overridden && fields[key] !== false}
                onChange={(event) => setField(key, event.target.checked)}
                disabled={busy || overridden}
              />
              {label}{overridden ? ' — overridden by custom field' : ''}
            </label>
          );
        })}
      </fieldset>

      {!customKeys.has(alertFieldDataKeys.includeSnapshot) && fields.includeSnapshot !== false ? (
        <label>
          Snapshot delivery
          <select value={dest.snapshotMode === 'link' ? 'link' : 'inline'} onChange={(event) => onChange({ snapshotMode: event.target.value })} disabled={busy}>
            <option value="inline">Inline — embed the image</option>
            <option value="link">Link only — reference, no bytes</option>
          </select>
          <span className="field-hint">Inline embeds the image (webhook/MQTT base64, Telegram photo). Link only sends a reference (smaller payloads); the consumer fetches the image itself.</span>
        </label>
      ) : null}

      <fieldset className="dest-group">
        <legend>
          <FieldTitle info="Key/value pairs added to the payload. A custom field overrides a built-in field of the same key. Values may use templates: {{ruleName}}, {{cameraName}}, {{label}}, {{confidence}}, {{detectionType}}, {{alertId}}, {{ruleId}}, {{cameraId}}.">
            Custom fields
          </FieldTitle>
        </legend>
        {customFields.map((field, index) => {
          const collides = reservedKeys.has(String(field.key || '').trim());
          return (
            <div className="dest-custom-row" key={index}>
              <input value={field.key || ''} onChange={(event) => setCustom(index, { key: event.target.value })} placeholder="key" disabled={busy} />
              <input value={field.value || ''} onChange={(event) => setCustom(index, { value: event.target.value })} placeholder="value" disabled={busy} />
              <button type="button" className="quiet danger" onClick={() => onChange({ customFields: customFields.filter((_, i) => i !== index) })} disabled={busy} aria-label="Remove field">✕</button>
              {collides ? <span className="field-hint">overrides built-in “{String(field.key).trim()}”</span> : null}
            </div>
          );
        })}
        <button type="button" className="quiet" onClick={() => onChange({ customFields: [...customFields, { key: '', value: '' }] })} disabled={busy}>
          <span className="btn-icon"><Ico n="plus" /> Add field</span>
        </button>
      </fieldset>
    </div>
  );
}

export function HealthSettingsPanel({ settings, busy, hasChanges, onChange, onSave, onDiscard }) {
  const value = { ...defaultHealthSettings, ...(settings || {}) };
  function patch(values) {
    onChange({ ...value, ...values });
  }
  // Interval and timeout are stored in milliseconds but edited in seconds for clarity.
  const intervalSec = Math.round(value.intervalMs / 1000);
  const timeoutSec = Math.round(value.timeoutMs / 1000);
  const detectionSec = intervalSec * Math.max(1, value.failureThreshold);
  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> Camera Health Monitor</span></h2>
          <label className="check-row">
            <input
              type="checkbox"
              checked={value.enabled}
              onChange={(event) => patch({ enabled: event.target.checked })}
            />
            Enabled
          </label>
        </header>
        <p className="settings-hint">
          Periodically checks whether each camera is reachable over the network (TCP, with an RTSP fallback check).
          When a camera goes offline a critical notification is raised, and an informational one when it recovers.
          Changes apply on the next sweep — no restart needed.
        </p>
        <div className="settings-grid">
          <label>
            <FieldTitle info="How often every camera is checked. Lower values detect outages sooner but probe the network more frequently. Minimum 5 seconds.">
              Check interval (seconds)
            </FieldTitle>
            <input
              type="number"
              min="5"
              step="5"
              value={intervalSec}
              onChange={(event) => patch({ intervalMs: Math.max(5, Number(event.target.value) || 0) * 1000 })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Per-probe deadline for the TCP dial and RTSP check. Cameras on slow links may need a higher value. 1–60 seconds.">
              Probe timeout (seconds)
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="60"
              value={timeoutSec}
              onChange={(event) => patch({ timeoutMs: Math.min(60, Math.max(1, Number(event.target.value) || 0)) * 1000 })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Consecutive failed checks before a camera is declared offline. Higher values avoid false alarms from brief network blips.">
              Failure threshold
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="10"
              value={value.failureThreshold}
              onChange={(event) => patch({ failureThreshold: Math.max(1, Number(event.target.value) || 0) })}
              disabled={!value.enabled}
            />
          </label>
          <label>
            <FieldTitle info="Consecutive successful checks before a camera is declared back online. Higher values avoid flapping when a camera is intermittently reachable.">
              Recovery threshold
            </FieldTitle>
            <input
              type="number"
              min="1"
              max="10"
              value={value.recoveryThreshold}
              onChange={(event) => patch({ recoveryThreshold: Math.max(1, Number(event.target.value) || 0) })}
              disabled={!value.enabled}
            />
          </label>
        </div>
        <p className="settings-hint">
          With these settings an outage is reported after roughly {detectionSec} second{detectionSec === 1 ? '' : 's'}
          {' '}({value.failureThreshold} × {intervalSec}s).
        </p>
      </section>

      <div className="settings-actions">
        <button type="submit" disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="save" /> Save Settings</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="undo" /> Discard Changes</span>
        </button>
      </div>
    </form>
  );
}

function formatBytes(n) {
  const bytes = Number(n) || 0;
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${(bytes / Math.pow(1024, i)).toFixed(i >= 3 ? 1 : 0)} ${units[i]}`;
}

function machineLevelClass(percent, warn, critical) {
  if (percent >= critical) return 'offline';
  if (percent >= warn) return 'unknown';
  return 'online';
}

const capacityLimitLabels = { cpu: 'CPU', gpu: 'GPU', disk: 'disk space', memory: 'memory' };

// formatRetentionDays renders an estimated retention span readably: minutes/hours
// below a day, otherwise days.
export function formatRetentionDays(days) {
  if (!days || days <= 0) return '—';
  if (days < 1) {
    const hours = days * 24;
    if (hours < 1) return `${Math.round(hours * 60)} min`;
    return `${hours.toFixed(1)} hours`;
  }
  return `${days < 10 ? days.toFixed(1) : Math.round(days)} days`;
}

// CapacityRetentionNote shows the recording retention achievable at the estimated
// camera count — disk shortens retention rather than blocking cameras.
export function CapacityRetentionNote({ capacity }) {
  if (!capacity || !capacity.configuredRetentionDays) return null;
  const constrained = capacity.retentionConstrained;
  return (
    <div className={`capacity-retention${constrained ? ' capacity-retention--warn' : ''}`}>
      <span className="btn-icon"><Ico n={constrained ? 'warning' : 'trash'} /></span>
      <span>
        At {capacity.estimatedMax} cameras, recording keeps about{' '}
        <strong>{formatRetentionDays(capacity.achievableRetentionDays)}</strong> of footage
        {' '}(target {capacity.configuredRetentionDays} days).
        {constrained ? ' Oldest footage auto-purges — lower the bitrate or add storage to keep more.' : ''}
      </span>
    </div>
  );
}

export function MachineHealthSettingsPanel({ settings, busy, hasChanges, metrics, onChange, onSave, onDiscard, onRefreshMetrics, capacity, onEstimateCapacity, onCalibrateCapacity, resetAllowed, onSecureWipe }) {
  const d = defaultMachineHealthSettings;
  const value = {
    ...d,
    ...(settings || {}),
    cpu: { ...d.cpu, ...(settings?.cpu || {}) },
    memory: { ...d.memory, ...(settings?.memory || {}) },
    disk: { ...d.disk, ...(settings?.disk || {}), paths: Array.isArray(settings?.disk?.paths) ? settings.disk.paths : [] },
    mitigation: { ...d.mitigation, ...(settings?.mitigation || {}) },
  };
  function patch(values) { onChange({ ...value, ...values }); }
  function patchCpu(v) { onChange({ ...value, cpu: { ...value.cpu, ...v } }); }
  function patchMem(v) { onChange({ ...value, memory: { ...value.memory, ...v } }); }
  function patchDisk(v) { onChange({ ...value, disk: { ...value.disk, ...v } }); }
  function patchMit(v) { onChange({ ...value, mitigation: { ...value.mitigation, ...v } }); }
  const intervalSec = Math.round(value.intervalMs / 1000);
  const disks = Array.isArray(metrics?.disks) ? metrics.disks : [];

  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="cpu" /> Camera Capacity Estimate</span></h2>
          <div className="capacity-actions">
            <button type="button" className="quiet" onClick={() => onCalibrateCapacity && onCalibrateCapacity()} disabled={busy} title="Benchmark the detector on this machine for an accurate estimate before any camera is added.">
              <span className="btn-icon"><Ico n="wand" /> Run calibration</span>
            </button>
            <button type="button" className="quiet" onClick={() => onEstimateCapacity && onEstimateCapacity()} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> Estimate</span>
            </button>
          </div>
        </header>
        <p className="settings-hint">
          A guide to how many cameras this host can process, from detected hardware
          {capacity?.confidence === 'measured' ? ' and current live load' : capacity?.confidence === 'calibrated' ? ' and a detector benchmark' : ''}. The total is the tightest continuous
          workload (AI detection, recording, memory); live view is on-demand and not counted.
          Run calibration for an accurate number before adding cameras.
        </p>
        {capacity ? (
          <>
            <div className="capacity-headline">
              <span className="capacity-number">{capacity.estimatedMax}</span>
              <div className="capacity-headline-meta">
                <span className="capacity-caption">estimated cameras</span>
                <span className={`capacity-badge capacity-badge--${capacity.confidence}`}>
                  {capacity.confidence === 'measured' ? 'Measured from live load' : capacity.confidence === 'calibrated' ? 'Calibrated on this host' : 'Ballpark estimate'}
                </span>
                {capacity.limitingWorkload ? (
                  <span className="field-hint">Limited by {capacityLimitLabels[(capacity.workloads || []).find((w) => w.name === capacity.limitingWorkload)?.limit] || capacity.limitingWorkload}</span>
                ) : null}
              </div>
            </div>
            <CapacityRetentionNote capacity={capacity} />
            <div className="capacity-grid">
              {(capacity.workloads || []).map((wl) => (
                <div className={`capacity-card${wl.name === capacity.limitingWorkload ? ' capacity-card--limit' : ''}`} key={wl.name}>
                  <dt>{wl.label}{!wl.continuous ? ' *' : ''}</dt>
                  <dd><strong>{wl.maxCameras}</strong> cameras</dd>
                  <span className="field-hint">{wl.note}</span>
                </div>
              ))}
            </div>
            {Array.isArray(capacity.assumptions) && capacity.assumptions.length > 0 ? (
              <details className="capacity-assumptions">
                <summary>Assumptions &amp; method</summary>
                <ul>{capacity.assumptions.map((a, i) => <li key={i}>{a}</li>)}</ul>
              </details>
            ) : null}
          </>
        ) : (
          <p className="empty">Click <strong>Estimate</strong> to gauge capacity for this machine.</p>
        )}
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="cpu" /> Machine Health Monitor</span></h2>
          <label className="check-row">
            <input type="checkbox" checked={value.enabled} onChange={(e) => patch({ enabled: e.target.checked })} />
            Enabled
          </label>
        </header>
        <p className="settings-hint">
          Samples host CPU, memory, and disk usage and raises warning/critical notifications (with recovery notices)
          when they cross your thresholds. Disk mitigation can purge expired recordings early and, as a last resort,
          pause recording so a full disk cannot break recordings, snapshots, or the database. Changes apply live.
        </p>

        <div className="machine-metrics">
          <div className="machine-metric-card">
            <dt>CPU</dt>
            <dd><strong className={`status-pill ${machineLevelClass(metrics?.cpuPercent ?? 0, value.cpu.warnPercent, value.cpu.criticalPercent)}`}>{metrics ? `${metrics.cpuPercent}%` : '—'}</strong></dd>
          </div>
          <div className="machine-metric-card">
            <dt>Memory</dt>
            <dd><strong className={`status-pill ${machineLevelClass(metrics?.memoryPercent ?? 0, value.memory.warnPercent, value.memory.criticalPercent)}`}>{metrics ? `${metrics.memoryPercent}%` : '—'}</strong></dd>
            {metrics ? <span className="field-hint">{formatBytes(metrics.memoryUsedBytes)} / {formatBytes(metrics.memoryTotalBytes)}</span> : null}
          </div>
          {disks.map((disk) => (
            <div className="machine-metric-card" key={disk.mountpoint}>
              <dt>Disk {disk.mountpoint}</dt>
              <dd><strong className={`status-pill ${machineLevelClass(disk.usedPercent, value.disk.warnPercent, value.disk.criticalPercent)}`}>{disk.usedPercent}%</strong></dd>
              <span className="field-hint">{formatBytes(disk.freeBytes)} free of {formatBytes(disk.totalBytes)}</span>
            </div>
          ))}
          {metrics?.recordingPaused ? (
            <div className="machine-metric-card">
              <dt>Recording</dt>
              <dd><strong className="status-pill offline">Paused (disk)</strong></dd>
            </div>
          ) : null}
          <div className="machine-metrics-action">
            <button type="button" className="quiet" onClick={() => onRefreshMetrics && onRefreshMetrics()} disabled={busy}>
              <span className="btn-icon"><Ico n="reload" /> Check now</span>
            </button>
          </div>
        </div>

        <div className="settings-grid">
          <label>
            <FieldTitle info="How often host metrics are sampled. Minimum 5 seconds.">Sample interval (seconds)</FieldTitle>
            <input type="number" min="5" step="5" value={intervalSec} onChange={(e) => patch({ intervalMs: Math.max(5, Number(e.target.value) || 0) * 1000 })} disabled={!value.enabled} />
          </label>
          <label>
            <FieldTitle info="Consecutive breaching samples before an alert fires (debounce against transient spikes).">Sustained samples</FieldTitle>
            <input type="number" min="1" max="60" value={value.sustainedSamples} onChange={(e) => patch({ sustainedSamples: Math.max(1, Number(e.target.value) || 0) })} disabled={!value.enabled} />
          </label>
          <label>
            <FieldTitle info="Consecutive normal samples before a recovery notice fires.">Recovery samples</FieldTitle>
            <input type="number" min="1" max="60" value={value.recoverySamples} onChange={(e) => patch({ recoverySamples: Math.max(1, Number(e.target.value) || 0) })} disabled={!value.enabled} />
          </label>
        </div>
      </section>

      <section className="settings-panel span-two">
        <header><h2>Thresholds</h2></header>
        <p className="settings-hint">Warning and critical percentages per metric. Critical must exceed warning.</p>
        <div className="settings-field-grid">
          <label><FieldTitle info="CPU usage % that raises a Warning.">CPU warning %</FieldTitle>
            <input type="number" min="1" max="100" value={value.cpu.warnPercent} onChange={(e) => patchCpu({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="CPU usage % that raises a Critical.">CPU critical %</FieldTitle>
            <input type="number" min="1" max="100" value={value.cpu.criticalPercent} onChange={(e) => patchCpu({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Memory usage % that raises a Warning.">Memory warning %</FieldTitle>
            <input type="number" min="1" max="100" value={value.memory.warnPercent} onChange={(e) => patchMem({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Memory usage % that raises a Critical.">Memory critical %</FieldTitle>
            <input type="number" min="1" max="100" value={value.memory.criticalPercent} onChange={(e) => patchMem({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Disk fullness % that raises a Warning.">Disk warning %</FieldTitle>
            <input type="number" min="1" max="100" value={value.disk.warnPercent} onChange={(e) => patchDisk({ warnPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
          <label><FieldTitle info="Disk fullness % that raises a Critical.">Disk critical %</FieldTitle>
            <input type="number" min="1" max="100" value={value.disk.criticalPercent} onChange={(e) => patchDisk({ criticalPercent: Number(e.target.value) })} disabled={!value.enabled} /></label>
        </div>
        <label>
          <FieldTitle info="Extra disk paths/volumes to monitor, one per line. The volumes the app writes to (recordings, logs, working dir) are monitored automatically.">
            Extra disk paths (one per line)
          </FieldTitle>
          <textarea
            rows="2"
            value={(value.disk.paths || []).join('\n')}
            onChange={(e) => patchDisk({ paths: e.target.value.split(/\r?\n/).map((p) => p.trim()).filter(Boolean) })}
            placeholder={'C:\\\nE:\\media'}
            disabled={!value.enabled}
          />
        </label>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="shield" /> Disk Mitigation</span></h2>
          <label className="check-row">
            <input type="checkbox" checked={value.mitigation.enabled} onChange={(e) => patchMit({ enabled: e.target.checked })} disabled={!value.enabled} />
            Enabled
          </label>
        </header>
        <p className="settings-hint">
          Automatic protective actions when a monitored disk gets dangerously full. Pausing recording stops new NVR
          footage until the disk recovers — footage during the pause is not captured.
        </p>
        <div className="settings-grid">
          <label>
            <FieldTitle info="When a monitored disk reaches this fullness, immediately purge expired recordings (only deletes footage already past its retention).">Early purge at %</FieldTitle>
            <input type="number" min="1" max="100" value={value.mitigation.purgeAtPercent} onChange={(e) => patchMit({ purgeAtPercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
          <label>
            <FieldTitle info="When a monitored disk reaches this fullness, pause NVR recording to stop the volume filling completely.">Pause recording at %</FieldTitle>
            <input type="number" min="1" max="100" value={value.mitigation.pauseRecordingAtPercent} onChange={(e) => patchMit({ pauseRecordingAtPercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
          <label>
            <FieldTitle info="Recording resumes once the disk drops below this fullness (hysteresis). Must be below the pause threshold.">Resume below %</FieldTitle>
            <input type="number" min="1" max="99" value={value.mitigation.resumePercent} onChange={(e) => patchMit({ resumePercent: Number(e.target.value) })} disabled={!value.enabled || !value.mitigation.enabled} />
          </label>
        </div>
      </section>

      <div className="settings-actions">
        <button type="submit" disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="save" /> Save Settings</span>
        </button>
        <button type="button" className="quiet" onClick={onDiscard} disabled={busy || !hasChanges}>
          <span className="btn-icon"><Ico n="undo" /> Discard Changes</span>
        </button>
      </div>

      {resetAllowed && onSecureWipe ? (
        <section className="settings-panel span-two danger-zone">
          <header>
            <h2><span className="btn-icon"><Ico n="warning" /> Danger Zone</span></h2>
          </header>
          <p className="settings-hint">
            <strong>Secure Wipe &amp; Reset</strong> permanently shreds every recording, snapshot, training dataset and
            upload, drops and rebuilds the database, and restarts the system back to first-run defaults. Runtime settings
            return to defaults. <strong>This cannot be undone.</strong>
          </p>
          <button type="button" className="danger-solid" onClick={onSecureWipe} disabled={busy}>
            <span className="btn-icon"><Ico n="warning" /> Secure Wipe &amp; Reset</span>
          </button>
        </section>
      ) : null}
    </form>
  );
}

