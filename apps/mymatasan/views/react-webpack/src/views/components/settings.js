import { useState, useEffect } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay, FieldTitle } from './ui';
import { defaultYoloConfig, bestYoloDefaults, defaultCaptureConfig, captureModeOptions, defaultAlertNotificationConfig, alertNotificationFields, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings } from '../lib/constants';
import {iceUrlsText,textToIceUrls,decoderTransportOptions,decoderHWAccelOptions } from '../lib/helpers';

export function SettingsTab({
  settingsNav,
  settings,
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
  onCaptureAutoConfig,
  gpuDevices,
  onCheckVisionTool,
  visionToolStatus,
  onInstallPackages,
  visionInstallResult,
  onLoadUsers,
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
}) {
  const iceServers = settings.stream.webrtc.iceServers || [];
  const capture = { ...defaultCaptureConfig, ...(settings.vision?.capture || {}),
    standalone: { ...defaultCaptureConfig.standalone, ...(settings.vision?.capture?.standalone || {}) },
    siphon: { ...defaultCaptureConfig.siphon, ...(settings.vision?.capture?.siphon || {}) } };
  const captureAuto = capture.mode === 'auto';
  const alertNotification = { ...defaultAlertNotificationConfig, ...(settings.vision?.alertNotification || {}) };
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
  function updateAlertNotification(patch) {
    update((current) => ({
      ...current,
      vision: {
        ...current.vision,
        alertNotification: { ...defaultAlertNotificationConfig, ...(current.vision?.alertNotification || {}), ...patch },
      },
    }));
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
          <label>
            <FieldTitle info="Executable used for RTSP-to-MJPEG fallback and RTSP frame capture. Leave as ffmpeg to resolve from PATH, or use an absolute service-safe path.">
              FFmpeg path
            </FieldTitle>
            <input
              value={settings.decoder.mjpeg.ffmpegPath}
              onChange={(event) => updateMJPEGDecoder({ ffmpegPath: event.target.value })}
              placeholder="ffmpeg"
              autoComplete="off"
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
            <FieldTitle info="Hardware acceleration mode for ffmpeg decoding. None uses CPU software decode; auto lets ffmpeg choose; platform-specific modes need matching ffmpeg build, drivers, and hardware.">
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
          <label>
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
        </>)}

        {settingsNav === 'ai' && (<>
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
              <FieldTitle info="Choose which detection-alert fields and media are included in notifications (webhook, Telegram, and the persisted notification meta). Identifiers (alert id, rule id, camera) are always included.">
                Alert Notification Fields
              </FieldTitle>
            </h2>
          </header>
          <p className="settings-hint">Applies to AI detection alerts raised by the background monitor. The snapshot is delivered as a Telegram photo and base64 in the webhook payload.</p>
          <div className="settings-field-grid">
            {alertNotificationFields.map(([key, label, help]) => (
              <label className="check-row" key={key}>
                <input
                  type="checkbox"
                  checked={alertNotification[key] === true}
                  onChange={(event) => updateAlertNotification({ [key]: event.target.checked })}
                />
                <FieldTitle info={help}>{label}</FieldTitle>
              </label>
            ))}
          </div>
        </section>

        <section className="settings-panel span-two">
          <header>
            <h2>
              <FieldTitle info="Controls how the AI detector sources frames per camera. Auto siphons decoded frames off the recorder when they are fresh and falls back to a standalone RTSP grab otherwise. Changing capture behavior ships in a later phase; these values are stored now.">
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
              <FieldTitle info="Frame source. Auto: siphon off the recorder when a recent frame is available, else standalone. Siphon: always read decoded frames off the recorder. Standalone: AI always opens its own one-frame RTSP grab from the live stream.">
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
                  <input
                    value={server.credential || ''}
                    onChange={(event) => updateIceServer(index, { credential: event.target.value })}
                    type="password"
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
          <section className="settings-panel span-two">
        <header>
          <h2>Users</h2>
          <button type="button" className="quiet" onClick={onLoadUsers} disabled={busy}>
            Reload
          </button>
        </header>
        <form className="user-create-row" onSubmit={onCreateUser}>
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
            <input
              value={newUser.password}
              onChange={(event) => onNewUser({ ...newUser, password: event.target.value })}
              type="password"
              autoComplete="new-password"
              required
            />
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={newUser.isAdmin}
              onChange={(event) => onNewUser({ ...newUser, isAdmin: event.target.checked })}
            />
            Admin
          </label>
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="user-plus" /> Add User</span>
          </button>
        </form>
        <div className="user-list">
          {users.length === 0 ? <p className="empty">No local users loaded.</p> : null}
          {users.map((user) => (
            <article className="user-row" key={user.id || user.username}>
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
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={Boolean(user.isAdmin)}
                  onChange={(event) => onEditUser(user.id, { isAdmin: event.target.checked })}
                />
                Admin
              </label>
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={Boolean(user.isActive)}
                  onChange={(event) => onEditUser(user.id, { isActive: event.target.checked })}
                />
                Active
              </label>
              <label>
                New password
                <input
                  value={passwordDrafts[user.id] || ''}
                  onChange={(event) => onPasswordDraft(user.id, event.target.value)}
                  type="password"
                  autoComplete="new-password"
                />
              </label>
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
          ))}
        </div>
          </section>
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
          />
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

export function NotificationSettingsPanel({ settings, busy, hasChanges, onChange, onSave, onDiscard, onTest }) {
  const webhook = settings.webhook || defaultNotificationSettings.webhook;
  const telegram = settings.telegram || defaultNotificationSettings.telegram;
  const retention = settings.retention || defaultNotificationSettings.retention;
  function patch(section, values) {
    onChange({ ...settings, [section]: { ...settings[section], ...values } });
  }
  return (
    <form className="settings-layout" onSubmit={onSave}>
      <FormBusyOverlay busy={busy} />

      <section className="settings-panel">
        <header>
          <h2><span className="btn-icon"><Ico n="wifi" /> Webhook</span></h2>
          <label className="check-row">
            <input
              type="checkbox"
              checked={webhook.enabled}
              onChange={(event) => patch('webhook', { enabled: event.target.checked })}
            />
            Enabled
          </label>
        </header>
        <p className="settings-hint">POSTs each notification as JSON to your endpoint (Slack, Discord, n8n, custom).</p>
        <label>
          Webhook URL
          <input
            value={webhook.url}
            onChange={(event) => patch('webhook', { url: event.target.value })}
            placeholder="https://hooks.example.com/..."
            type="url"
            autoComplete="off"
            disabled={!webhook.enabled}
          />
        </label>
        <label>
          Minimum severity
          <select
            value={webhook.minSeverity}
            onChange={(event) => patch('webhook', { minSeverity: event.target.value })}
            disabled={!webhook.enabled}
          >
            {SEVERITY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </label>
        <button type="button" className="quiet" onClick={() => onTest('webhook')} disabled={busy || !webhook.enabled}>
          <span className="btn-icon"><Ico n="send" /> Send Test</span>
        </button>
      </section>

      <section className="settings-panel">
        <header>
          <h2><span className="btn-icon"><Ico n="send" /> Telegram</span></h2>
          <label className="check-row">
            <input
              type="checkbox"
              checked={telegram.enabled}
              onChange={(event) => patch('telegram', { enabled: event.target.checked })}
            />
            Enabled
          </label>
        </header>
        <p className="settings-hint">Sends alerts to a Telegram chat via a bot. Create a bot with @BotFather, then get your chat id.</p>
        <label>
          Bot token
          <input
            value={telegram.botToken}
            onChange={(event) => patch('telegram', { botToken: event.target.value })}
            placeholder="123456:ABC-DEF..."
            type="password"
            autoComplete="off"
            disabled={!telegram.enabled}
          />
        </label>
        <label>
          Chat ID
          <input
            value={telegram.chatId}
            onChange={(event) => patch('telegram', { chatId: event.target.value })}
            placeholder="-1001234567890"
            autoComplete="off"
            disabled={!telegram.enabled}
          />
        </label>
        <label>
          Minimum severity
          <select
            value={telegram.minSeverity}
            onChange={(event) => patch('telegram', { minSeverity: event.target.value })}
            disabled={!telegram.enabled}
          >
            {SEVERITY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </label>
        <button type="button" className="quiet" onClick={() => onTest('telegram')} disabled={busy || !telegram.enabled}>
          <span className="btn-icon"><Ico n="send" /> Send Test</span>
        </button>
      </section>

      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="trash" /> Retention</span></h2>
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

export function MachineHealthSettingsPanel({ settings, busy, hasChanges, metrics, onChange, onSave, onDiscard, onRefreshMetrics }) {
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
    </form>
  );
}

