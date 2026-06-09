import React, { useEffect, useMemo, useRef, useState } from 'react';
import config from 'config';

const emptyLogin = { username: '', password: '' };
const defaultDeviceCredentials = { username: '', password: '' };
const defaultStreamConfig = {
  webrtc: { enabled: true, iceServers: [] },
  mjpegFallback: { enabled: true },
};
const defaultRuntimeSettings = {
  decoder: { mjpeg: { ffmpegPath: '' } },
  stream: defaultStreamConfig,
};
const defaultNewUser = { username: '', displayName: '', password: '', isAdmin: false, isActive: true };
const defaultZonePoints = [
  [0.15, 0.15],
  [0.85, 0.15],
  [0.85, 0.85],
  [0.15, 0.85],
];
const scheduleDayOptions = [
  ['mon', 'Mon'],
  ['tue', 'Tue'],
  ['wed', 'Wed'],
  ['thu', 'Thu'],
  ['fri', 'Fri'],
  ['sat', 'Sat'],
  ['sun', 'Sun'],
];
const weekdayScheduleDays = ['mon', 'tue', 'wed', 'thu', 'fri'];
const weekendScheduleDays = ['sat', 'sun'];
const allScheduleDays = scheduleDayOptions.map(([id]) => id);
const liveViewsCookieName = 'mymatasan_live_views';

function readCookie(name) {
  if (typeof document === 'undefined') {
    return '';
  }
  const prefix = `${name}=`;
  return document.cookie
    .split(';')
    .map((item) => item.trim())
    .find((item) => item.startsWith(prefix))
    ?.slice(prefix.length) || '';
}

function readLiveViewsCookie(fallbackLayout = '2x2') {
  const fallback = { layout: fallbackLayout, ids: [], hasPreference: false };
  const raw = readCookie(liveViewsCookieName);
  if (!raw) {
    return fallback;
  }
  try {
    const parsed = JSON.parse(decodeURIComponent(raw));
    const layout = parsed?.layout === '4x4' ? '4x4' : '2x2';
    const ids = Array.isArray(parsed?.ids)
      ? parsed.ids.map((id) => Number(id)).filter((id) => Number.isFinite(id) && id > 0)
      : [];
    return { layout, ids, hasPreference: true };
  } catch (_) {
    return fallback;
  }
}

function saveLiveViewsCookie(layout, tiles) {
  if (typeof document === 'undefined') {
    return;
  }
  const payload = {
    layout: layout === '4x4' ? '4x4' : '2x2',
    ids: (tiles || []).map((tile) => Number(tile?.id)).filter((id) => Number.isFinite(id) && id > 0),
  };
  document.cookie = `${liveViewsCookieName}=${encodeURIComponent(JSON.stringify(payload))}; path=/; max-age=31536000; SameSite=Lax`;
}

function unwrap(payload) {
  if (payload && payload.data && Object.prototype.hasOwnProperty.call(payload.data, 'result')) {
    return payload.data.result;
  }
  if (payload && Object.prototype.hasOwnProperty.call(payload, 'result')) {
    return payload.result;
  }
  return payload;
}

function errorMessage(payload, fallback) {
  const details = payload?.details;
  if (Array.isArray(details) && details.length > 0) {
    const first = details[0];
    if (typeof first === 'string' && first.trim()) {
      return first;
    }
    if (typeof first?.error === 'string' && first.error.trim()) {
      return first.error;
    }
    if (typeof first?.message === 'string' && first.message.trim()) {
      return first.message;
    }
  }
  if (typeof details?.error === 'string' && details.error.trim()) {
    return details.error;
  }
  if (typeof details?.message === 'string' && details.message.trim()) {
    return details.message;
  }
  return payload?.message || fallback;
}

function apiBase() {
  const origin = window.location.origin;
  if (origin.includes(':4000') && config.apiUrl) {
    return config.apiUrl;
  }
  return origin;
}

function fieldValue(value) {
  return value === undefined || value === null || value === '' ? '-' : value;
}

function formatTimestamp(value) {
  const raw = Number(value || 0);
  if (!raw) {
    return '-';
  }
  const millis = raw > 9999999999 ? raw : raw * 1000;
  try {
    return new Intl.DateTimeFormat(undefined, {
      year: 'numeric',
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(new Date(millis));
  } catch (_) {
    return new Date(millis).toLocaleString();
  }
}

function parseMetadata(value) {
  if (!value || typeof value !== 'string') {
    return {};
  }
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch (_) {
    return { raw: value };
  }
}

function formatPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return '-';
  }
  return `${(number * 100).toFixed(1)}%`;
}

function cameraTitle(device) {
  return device?.name || device?.model || device?.host || 'ONVIF camera';
}

function cameraDescription(device) {
  return device?.description || '';
}

function compareSavedCameras(left, right) {
  const leftTitle = cameraTitle(left).toLowerCase();
  const rightTitle = cameraTitle(right).toLowerCase();
  if (leftTitle !== rightTitle) {
    return leftTitle.localeCompare(rightTitle);
  }
  const leftAddress = normalizeCameraValue(left?.host || left?.xAddr);
  const rightAddress = normalizeCameraValue(right?.host || right?.xAddr);
  if (leftAddress !== rightAddress) {
    return leftAddress.localeCompare(rightAddress);
  }
  return (Number(left?.id) || 0) - (Number(right?.id) || 0);
}

function orderedSavedCameras(devices) {
  return [...(devices || [])].sort(compareSavedCameras);
}

function isActionableVisionAlert(alert) {
  if (!alert || alert.isAcknowledged) {
    return false;
  }
  return !parseMetadata(alert.metadata).diagnostic;
}

function latestAlertsByCamera(alerts) {
  const grouped = new Map();
  (alerts || []).forEach((alert) => {
    if (!isActionableVisionAlert(alert)) {
      return;
    }
    const cameraId = Number(alert.cameraId);
    if (!cameraId) {
      return;
    }
    const items = grouped.get(cameraId) || [];
    items.push(alert);
    grouped.set(cameraId, items);
  });
  grouped.forEach((items) => {
    items.sort((left, right) => Number(right.createdAt || 0) - Number(left.createdAt || 0));
  });
  return grouped;
}

function normalizeCameraValue(value) {
  return String(value || '').trim().toLowerCase();
}

function cameraMatchKeys(device) {
  const keys = [];
  const xAddr = normalizeCameraValue(device?.xAddr);
  const host = normalizeCameraValue(device?.host);
  const port = normalizeCameraValue(device?.port);
  const serial = normalizeCameraValue(device?.serialNumber);
  if (xAddr) {
    keys.push(`xaddr:${xAddr}`);
  }
  if (host) {
    keys.push(`host:${host}:${port}`);
  }
  if (serial) {
    keys.push(`serial:${serial}`);
  }
  return keys;
}

function sameCamera(left, right) {
  const rightKeys = new Set(cameraMatchKeys(right));
  return cameraMatchKeys(left).some((key) => rightKeys.has(key));
}

function liveSource(id) {
  return `${apiBase()}/api/onvif/devices/${id}/live.mjpeg?fps=5&width=480&t=${Date.now()}`;
}

function normalizeStreamConfig(value) {
  return {
    webrtc: {
      enabled: value?.webrtc?.enabled !== false,
      iceServers: Array.isArray(value?.webrtc?.iceServers) ? value.webrtc.iceServers : [],
    },
    mjpegFallback: {
      enabled: value?.mjpegFallback?.enabled !== false,
    },
  };
}

function normalizeRuntimeSettings(value) {
  return {
    decoder: {
      mjpeg: {
        ffmpegPath: value?.decoder?.mjpeg?.ffmpegPath || '',
      },
    },
    stream: normalizeStreamConfig(value?.stream),
  };
}

function iceUrlsText(server) {
  return Array.isArray(server?.urls) ? server.urls.join('\n') : '';
}

function textToIceUrls(value) {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function clamp01(value) {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(1, value));
}

function roundedPoint(point) {
  return [Number(clamp01(point[0]).toFixed(4)), Number(clamp01(point[1]).toFixed(4))];
}

function zonePolygonText(points) {
  const normalized = (points || []).map(roundedPoint);
  return JSON.stringify(normalized, null, 2);
}

function parseZonePolygon(value) {
  if (!value) {
    return defaultZonePoints;
  }
  try {
    const parsed = JSON.parse(value);
    if (!Array.isArray(parsed)) {
      return defaultZonePoints;
    }
    const points = parsed
      .filter((point) => Array.isArray(point) && point.length >= 2)
      .map((point) => roundedPoint([Number(point[0]), Number(point[1])]));
    return points;
  } catch (_) {
    return defaultZonePoints;
  }
}

const defaultZonePolygon = zonePolygonText(defaultZonePoints);

function defaultVisionRuleDraft(cameraId = '') {
  return {
    id: 0,
    cameraId: cameraId || '',
    name: '',
    detectionType: 'fire',
    zonePolygon: defaultZonePolygon,
    schedulePolicy: '',
    threshold: 0.75,
    minFrames: 3,
    cooldownSeconds: 30,
    soundEnabled: true,
    isEnabled: true,
  };
}

function browserTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Local';
  } catch (_) {
    return 'Local';
  }
}

function parseSchedulePolicy(value) {
  if (!value || typeof value !== 'string') {
    return null;
  }
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null;
  } catch (_) {
    return null;
  }
}

function sameScheduleDays(left, right) {
  const leftSet = new Set(left || []);
  const rightSet = new Set(right || []);
  if (leftSet.size !== rightSet.size) {
    return false;
  }
  return [...leftSet].every((day) => rightSet.has(day));
}

function schedulePolicyText(policy) {
  if (!policy) {
    return '';
  }
  const hasWindows = Array.isArray(policy.windows) && policy.windows.length > 0;
  const hasRanges = Array.isArray(policy.dateRanges) && policy.dateRanges.length > 0;
  if (!hasWindows && !hasRanges) {
    return '';
  }
  return JSON.stringify(policy);
}

function weeklySchedulePolicy({ days, start, end, mode = 'allow', timezone = browserTimezone(), preset = '' }) {
  const policy = {
    preset,
    timezone,
    mode,
    windows: [{ days, start, end }],
  };
  if (!preset) {
    delete policy.preset;
  }
  return schedulePolicyText(policy);
}

function rangeSchedulePolicy({ start, end, mode = 'allow', timezone = browserTimezone(), preset = '' }) {
  if (!start || !end) {
    return '';
  }
  const policy = {
    preset,
    timezone,
    mode,
    dateRanges: [{ start: datetimeLocalToRFC3339(start), end: datetimeLocalToRFC3339(end) }],
  };
  if (!preset) {
    delete policy.preset;
  }
  return schedulePolicyText(policy);
}

function schedulePresetPolicy(preset, current = scheduleDraftFromPolicy('')) {
  switch (preset) {
    case 'daytime':
      return weeklySchedulePolicy({ days: allScheduleDays, start: '07:00', end: '19:00', timezone: current.timezone });
    case 'nighttime':
      return weeklySchedulePolicy({ days: allScheduleDays, start: '19:00', end: '07:00', timezone: current.timezone });
    case 'weekdays':
      return weeklySchedulePolicy({ days: weekdayScheduleDays, start: '00:00', end: '23:59', timezone: current.timezone });
    case 'weekends':
      return weeklySchedulePolicy({ days: weekendScheduleDays, start: '00:00', end: '23:59', timezone: current.timezone });
    case 'custom':
      if (current.preset !== 'custom') {
        return weeklySchedulePolicy({
          days: allScheduleDays,
          start: '08:00',
          end: '18:00',
          mode: 'allow',
          timezone: current.timezone,
          preset: 'custom',
        });
      }
      return weeklySchedulePolicy({
        days: current.days.length ? current.days : allScheduleDays,
        start: current.start || '08:00',
        end: current.end || '18:00',
        mode: current.mode || 'allow',
        timezone: current.timezone,
        preset: 'custom',
      });
    case 'range':
      return rangeSchedulePolicy({
        start: current.rangeStart || datetimeLocalFromDate(new Date()),
        end: current.rangeEnd || datetimeLocalFromDate(new Date(Date.now() + 60 * 60 * 1000)),
        mode: current.mode || 'allow',
        timezone: current.timezone,
        preset: 'range',
      });
    default:
      return '';
  }
}

function scheduleDraftFromPolicy(value) {
  const timezone = browserTimezone();
  const fallback = {
    preset: 'always',
    mode: 'allow',
    timezone,
    days: allScheduleDays,
    start: '07:00',
    end: '19:00',
    rangeStart: '',
    rangeEnd: '',
  };
  const policy = parseSchedulePolicy(value);
  if (!policy) {
    return fallback;
  }
  const explicitPreset = typeof policy.preset === 'string' ? policy.preset : '';
  const mode = policy.mode === 'deny' ? 'deny' : 'allow';
  const policyTimezone = policy.timezone || timezone;
  const range = Array.isArray(policy.dateRanges) ? policy.dateRanges[0] : null;
  if (range) {
    return {
      ...fallback,
      preset: 'range',
      mode,
      timezone: policyTimezone,
      rangeStart: datetimeLocalFromRFC3339(range.start),
      rangeEnd: datetimeLocalFromRFC3339(range.end),
    };
  }
  const window = Array.isArray(policy.windows) ? policy.windows[0] : null;
  if (!window) {
    return fallback;
  }
  const days = Array.isArray(window.days) && window.days.length ? window.days : allScheduleDays;
  const start = window.start || fallback.start;
  const end = window.end || fallback.end;
  let preset = 'custom';
  if (explicitPreset !== 'custom' && mode === 'allow' && sameScheduleDays(days, allScheduleDays) && start === '07:00' && end === '19:00') {
    preset = 'daytime';
  } else if (explicitPreset !== 'custom' && mode === 'allow' && sameScheduleDays(days, allScheduleDays) && start === '19:00' && end === '07:00') {
    preset = 'nighttime';
  } else if (explicitPreset !== 'custom' && mode === 'allow' && sameScheduleDays(days, weekdayScheduleDays) && start === '00:00' && end === '23:59') {
    preset = 'weekdays';
  } else if (explicitPreset !== 'custom' && mode === 'allow' && sameScheduleDays(days, weekendScheduleDays) && start === '00:00' && end === '23:59') {
    preset = 'weekends';
  }
  return {
    ...fallback,
    preset,
    mode,
    timezone: policyTimezone,
    days,
    start,
    end,
  };
}

function datetimeLocalFromDate(date) {
  const pad = (value) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function datetimeLocalFromRFC3339(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return datetimeLocalFromDate(date);
}

function datetimeLocalToRFC3339(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return date.toISOString();
}

function scheduleSummary(value) {
  const draft = scheduleDraftFromPolicy(value);
  switch (draft.preset) {
    case 'daytime':
      return 'Daytime only';
    case 'nighttime':
      return 'Nighttime only';
    case 'weekdays':
      return 'Weekdays only';
    case 'weekends':
      return 'Weekends only';
    case 'custom':
      return `${draft.mode === 'deny' ? 'Paused' : 'Active'} ${draft.days.join(', ')} ${draft.start}-${draft.end} ${draft.timezone}`;
    case 'range':
      return `${draft.mode === 'deny' ? 'Paused' : 'Active'} for selected datetime range`;
    default:
      return 'Always active';
  }
}

function playAlertSound() {
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (!AudioContext) {
    return;
  }
  const ctx = new AudioContext();
  const oscillator = ctx.createOscillator();
  const gain = ctx.createGain();
  oscillator.type = 'sine';
  oscillator.frequency.value = 880;
  gain.gain.setValueAtTime(0.001, ctx.currentTime);
  gain.gain.exponentialRampToValueAtTime(0.2, ctx.currentTime + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.35);
  oscillator.connect(gain);
  gain.connect(ctx.destination);
  oscillator.start();
  oscillator.stop(ctx.currentTime + 0.38);
}

function waitForIceGathering(pc) {
  if (pc.iceGatheringState === 'complete') {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timeout = window.setTimeout(resolve, 3000);
    function checkState() {
      if (pc.iceGatheringState === 'complete') {
        window.clearTimeout(timeout);
        pc.removeEventListener('icegatheringstatechange', checkState);
        resolve();
      }
    }
    pc.addEventListener('icegatheringstatechange', checkState);
  });
}

async function createWebRTCAnswer(deviceId, offer, authHeader) {
  const headers = { 'Content-Type': 'application/json' };
  if (authHeader) {
    headers.Authorization = authHeader;
  }
  const response = await fetch(`${apiBase()}/api/onvif/devices/${deviceId}/webrtc/offer`, {
    method: 'POST',
    credentials: 'include',
    headers,
    body: JSON.stringify({ type: offer.type, sdp: offer.sdp }),
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch (_) {
      payload = { message: text };
    }
    }
    if (!response.ok) {
      throw new Error(errorMessage(payload, `WebRTC offer failed with ${response.status}`));
    }
    return unwrap(payload);
  }

function Tracks({ value }) {
  let tracks = [];
  if (typeof value === 'string' && value.trim()) {
    try {
      tracks = JSON.parse(value);
    } catch (_) {
      tracks = [];
    }
  } else if (Array.isArray(value)) {
    tracks = value;
  }
  if (!tracks.length) {
    return <span>-</span>;
  }
  return (
    <ul className="track-list">
      {tracks.map((track, idx) => (
        <li key={`${track.codec || 'track'}-${idx}`}>
          {track.mediaType || 'media'} / {track.codec || 'codec'} {track.clockRate ? `@ ${track.clockRate}` : ''}
        </li>
      ))}
    </ul>
  );
}

function Message({ value }) {
  if (!value) {
    return null;
  }
  return <div className="status-line">{value}</div>;
}

function LiveViewport({ deviceId, title, authHeader, streamConfig }) {
  const videoRef = useRef(null);
  const [state, setState] = useState('Connecting');
  const [fallbackSrc, setFallbackSrc] = useState('');

  useEffect(() => {
    if (!deviceId) {
      return undefined;
    }
    const configValue = normalizeStreamConfig(streamConfig);
    setFallbackSrc('');
    setState(configValue.webrtc.enabled ? 'Connecting' : 'MJPEG');

    if (!configValue.webrtc.enabled) {
      if (configValue.mjpegFallback.enabled) {
        setFallbackSrc(liveSource(deviceId));
      } else {
        setState('Live view disabled');
      }
      return undefined;
    }
    if (typeof RTCPeerConnection === 'undefined') {
      if (configValue.mjpegFallback.enabled) {
        setState('MJPEG fallback');
        setFallbackSrc(liveSource(deviceId));
      } else {
        setState('WebRTC unavailable');
      }
      return undefined;
    }

    let cancelled = false;
    const pc = new RTCPeerConnection({ iceServers: configValue.webrtc.iceServers });

    async function connect() {
      try {
        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.ontrack = (event) => {
          if (cancelled || !videoRef.current) {
            return;
          }
          const stream = event.streams[0] || new MediaStream([event.track]);
          videoRef.current.srcObject = stream;
          videoRef.current.play().catch(() => {});
          setState('Live');
        };
        pc.onconnectionstatechange = () => {
          if (['failed', 'disconnected', 'closed'].includes(pc.connectionState) && !cancelled) {
            if (configValue.mjpegFallback.enabled) {
              setState('MJPEG fallback');
              setFallbackSrc(liveSource(deviceId));
            } else {
              setState(`WebRTC ${pc.connectionState}`);
            }
          }
        };

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        await waitForIceGathering(pc);
        const answer = await createWebRTCAnswer(deviceId, pc.localDescription, authHeader);
        if (cancelled) {
          return;
        }
        await pc.setRemoteDescription(answer);
      } catch (err) {
        if (!cancelled) {
          if (configValue.mjpegFallback.enabled) {
            setState(err.message || 'MJPEG fallback');
            setFallbackSrc(liveSource(deviceId));
          } else {
            setState(err.message || 'WebRTC failed');
          }
          pc.close();
        }
      }
    }

    connect();

    return () => {
      cancelled = true;
      if (videoRef.current?.srcObject) {
        videoRef.current.srcObject.getTracks().forEach((track) => track.stop());
        videoRef.current.srcObject = null;
      }
      pc.close();
    };
  }, [deviceId, authHeader, streamConfig]);

  return (
    <div className="live-frame">
      {fallbackSrc ? (
        <img src={fallbackSrc} alt={`${title} live view`} />
      ) : (
        <video ref={videoRef} autoPlay muted playsInline aria-label={`${title} live view`} />
      )}
      <span className="stream-state">{state}</span>
    </div>
  );
}

function ZoneDrawingPreview({ camera, polygonValue, onPolygon, authHeader, streamConfig, disabled }) {
  const overlayRef = useRef(null);
  const [draggingIndex, setDraggingIndex] = useState(null);
  const points = useMemo(() => parseZonePolygon(polygonValue), [polygonValue]);
  const polygonPoints = points.map((point) => `${point[0] * 100},${point[1] * 100}`).join(' ');

  function commit(nextPoints) {
    onPolygon(zonePolygonText(nextPoints));
  }

  function pointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return roundedPoint([(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height]);
  }

  function addPoint(event) {
    if (disabled || !camera) {
      return;
    }
    commit([...points, pointFromEvent(event)]);
  }

  function movePoint(event) {
    if (disabled || draggingIndex === null) {
      return;
    }
    const nextPoints = [...points];
    nextPoints[draggingIndex] = pointFromEvent(event);
    commit(nextPoints);
  }

  function stopDrag(event) {
    if (draggingIndex !== null && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDraggingIndex(null);
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>Detection Zone</h3>
        <span className="status-pill">{points.length} points</span>
      </header>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <LiveViewport deviceId={camera.id} title={cameraTitle(camera)} authHeader={authHeader} streamConfig={streamConfig} />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label="Draw detection zone"
              onPointerDown={(event) => {
                if (event.button !== 0) {
                  return;
                }
                overlayRef.current?.setPointerCapture?.(event.pointerId);
                addPoint(event);
              }}
              onPointerMove={movePoint}
              onPointerUp={stopDrag}
              onPointerCancel={stopDrag}
            >
              <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                {points.length >= 3 ? <polygon points={polygonPoints} className="zone-shape" /> : null}
                {points.length >= 2 ? <polyline points={polygonPoints} className="zone-line" /> : null}
                {points.map((point, index) => (
                  <circle
                    key={`${point[0]}-${point[1]}-${index}`}
                    cx={point[0] * 100}
                    cy={point[1] * 100}
                    r="2.3"
                    className="zone-point"
                    vectorEffect="non-scaling-stroke"
                    onPointerDown={(event) => {
                      if (disabled || event.button !== 0) {
                        return;
                      }
                      event.stopPropagation();
                      overlayRef.current?.setPointerCapture?.(event.pointerId);
                      setDraggingIndex(index);
                    }}
                  />
                ))}
              </svg>
            </div>
          </>
        ) : (
          <div className="zone-empty-state">Select camera</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={() => commit(points.slice(0, -1))} disabled={disabled || !points.length}>
          Undo Point
        </button>
        <button type="button" className="quiet" onClick={() => commit([])} disabled={disabled}>
          Clear Zone
        </button>
        <button
          type="button"
          className="quiet"
          onClick={() =>
            commit([
              [0, 0],
              [1, 0],
              [1, 1],
              [0, 1],
            ])
          }
          disabled={disabled}
        >
          Full Frame
        </button>
      </div>
    </section>
  );
}

function LoginPage({ credentials, busy, message, onChange, onSubmit }) {
  return (
    <main className="login-screen">
      <style>{styles}</style>
      <form className="login-panel" onSubmit={onSubmit}>
        <div>
          <h1>MyMataSan</h1>
          <p>Standalone camera monitor</p>
        </div>
        <label>
          Username
          <input
            value={credentials.username}
            onChange={(event) => onChange({ ...credentials, username: event.target.value })}
            autoComplete="username"
            autoFocus
          />
        </label>
        <label>
          Password
          <input
            value={credentials.password}
            onChange={(event) => onChange({ ...credentials, password: event.target.value })}
            type="password"
            autoComplete="current-password"
          />
        </label>
        <button type="submit" disabled={busy}>
          Login
        </button>
        <Message value={message} />
      </form>
    </main>
  );
}

function TopBar({ activeTab, busy, onTab, onRefresh, onLogout }) {
  const tabs = [
    { id: 'views', label: 'Live Views' },
    { id: 'cameras', label: 'Cameras' },
    { id: 'ai', label: 'AI' },
    { id: 'settings', label: 'Settings' },
  ];
  return (
    <header className="topbar">
      <div>
        <h1>MyMataSan</h1>
        <p>Device console</p>
      </div>
      <nav className="primary-tabs" aria-label="Main">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={activeTab === tab.id ? 'active' : 'quiet'}
            onClick={() => onTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </nav>
      <div className="topbar-actions">
        <button type="button" className="quiet" onClick={onRefresh} disabled={busy}>
          Refresh
        </button>
        <button type="button" className="quiet danger-text" onClick={onLogout} disabled={busy}>
          Lock
        </button>
      </div>
    </header>
  );
}

function DeviceMeta({ device }) {
  return (
    <dl className="meta-grid">
      <div>
        <dt>Host</dt>
        <dd>{fieldValue(device.host)}</dd>
      </div>
      <div>
        <dt>Port</dt>
        <dd>{fieldValue(device.port)}</dd>
      </div>
      <div>
        <dt>Model</dt>
        <dd>{fieldValue(device.model)}</dd>
      </div>
      <div>
        <dt>Serial</dt>
        <dd>{fieldValue(device.serialNumber)}</dd>
      </div>
    </dl>
  );
}

function DeviceDescription({ device }) {
  const description = cameraDescription(device);
  if (!description) {
    return null;
  }
  return <p className="device-description">{description}</p>;
}

function OnvifDetails({ device }) {
  return (
    <section className="capability-panel">
      <header>
        <h4>ONVIF Information</h4>
        <strong className={`status-pill ${device.ptzSupported ? 'online' : 'unknown'}`}>
          PTZ {device.ptzSupported ? 'supported' : 'not detected'}
        </strong>
      </header>
      <dl className="capability-grid">
        <div>
          <dt>Manufacturer</dt>
          <dd>{fieldValue(device.manufacturer)}</dd>
        </div>
        <div>
          <dt>Firmware</dt>
          <dd>{fieldValue(device.firmwareVersion)}</dd>
        </div>
        <div>
          <dt>Hardware ID</dt>
          <dd>{fieldValue(device.hardwareId)}</dd>
        </div>
        <div>
          <dt>Media Service</dt>
          <dd>{fieldValue(device.mediaXAddr)}</dd>
        </div>
        <div>
          <dt>PTZ Service</dt>
          <dd>{fieldValue(device.ptzXAddr)}</dd>
        </div>
        <div>
          <dt>Profile Token</dt>
          <dd>{fieldValue(device.profileToken)}</dd>
        </div>
        <div>
          <dt>Snapshot URI</dt>
          <dd>{fieldValue(device.snapshotUri)}</dd>
        </div>
        <div>
          <dt>RTSP Transport</dt>
          <dd>{fieldValue(device.rtspTransport)}</dd>
        </div>
        <div>
          <dt>Types</dt>
          <dd>{fieldValue(device.types)}</dd>
        </div>
        <div>
          <dt>Scopes</dt>
          <dd>{fieldValue(device.scopes)}</dd>
        </div>
      </dl>
    </section>
  );
}

function ViewsTab({
  devices,
  layout,
  viewTiles,
  alertsByCamera = new Map(),
  draggedTileId,
  busy,
  authHeader,
  streamConfig,
  onLayout,
  onAdd,
  onRemove,
  onMove,
  onDragTile,
  onPTZMove,
  onPTZStop,
  onOpenAlerts,
}) {
  const tileCount = layout === '4x4' ? 16 : 4;
  const layoutClass = layout === '4x4' ? 'layout-four' : 'layout-two';
  const tiles = [...viewTiles.slice(0, tileCount)];
  while (tiles.length < tileCount) {
    tiles.push(null);
  }
  const available = devices.filter((device) => !viewTiles.some((tile) => tile?.id === device.id));

  return (
    <section className="workspace">
      <div className="toolbar">
        <div className="segmented">
          <button type="button" className={layout === '2x2' ? 'active' : 'quiet'} onClick={() => onLayout('2x2')}>
            2x2
          </button>
          <button type="button" className={layout === '4x4' ? 'active' : 'quiet'} onClick={() => onLayout('4x4')}>
            4x4
          </button>
        </div>
        <div className="add-strip">
          {available.length === 0 ? <span>No saved cameras available</span> : null}
          {available.map((device) => (
            <button type="button" className="quiet" key={device.id} disabled={busy} onClick={() => onAdd(device)}>
              Add {cameraTitle(device)}
            </button>
          ))}
        </div>
      </div>

      <div className={`view-grid ${layoutClass}`}>
        {tiles.map((tile, idx) => {
          const tileAlerts = tile ? alertsByCamera.get(Number(tile.id)) || [] : [];
          const latestAlert = tileAlerts[0] || null;
          return (
          <article
            className={[
              'view-tile',
              tile && draggedTileId === tile.id ? 'dragging' : '',
              tileAlerts.length > 0 ? 'has-ai-alert' : '',
            ].filter(Boolean).join(' ')}
            key={tile ? tile.id : `empty-${idx}`}
            draggable={Boolean(tile)}
            onDragStart={(event) => {
              if (!tile) {
                return;
              }
              event.dataTransfer.effectAllowed = 'move';
              event.dataTransfer.setData('text/plain', String(idx));
              onDragTile(tile.id);
            }}
            onDragEnd={() => onDragTile(null)}
            onDragOver={(event) => {
              if (tile) {
                event.preventDefault();
                event.dataTransfer.dropEffect = 'move';
              }
            }}
            onDrop={(event) => {
              if (!tile) {
                return;
              }
              event.preventDefault();
              const from = Number(event.dataTransfer.getData('text/plain'));
              onMove(from, idx);
              onDragTile(null);
            }}
          >
            {tile ? (
              <>
                <div className="tile-header">
                  <span className="drag-handle" title="Drag to reorder" aria-label="Drag to reorder">
                    ::
                  </span>
                  <strong>{tile.title}</strong>
                  {tileAlerts.length > 0 ? (
                    <button
                      type="button"
                      className="tile-alert-pill"
                      onClick={() => onOpenAlerts(tile.id)}
                      aria-label={`${tileAlerts.length} AI alert${tileAlerts.length === 1 ? '' : 's'} for ${tile.title}`}
                    >
                      AI {tileAlerts.length}
                    </button>
                  ) : null}
                  <button type="button" className="icon-button" onClick={() => onRemove(tile.id)} aria-label="Remove live view">
                    X
                  </button>
                </div>
                <LiveViewport deviceId={tile.id} title={tile.title} authHeader={authHeader} streamConfig={streamConfig} />
                {latestAlert ? (
                  <button type="button" className="tile-ai-banner" onClick={() => onOpenAlerts(tile.id)}>
                    <strong>{latestAlert.label || latestAlert.detectionType || 'AI event'}</strong>
                    <span>{formatTimestamp(latestAlert.createdAt)}</span>
                  </button>
                ) : null}
                {tile.ptzSupported ? (
                  <div className="ptz-pad" aria-label={`${tile.title} PTZ controls`}>
                    <button type="button" onClick={() => onPTZMove(tile.id, 'up')} disabled={busy}>
                      Up
                    </button>
                    <button type="button" onClick={() => onPTZMove(tile.id, 'left')} disabled={busy}>
                      Left
                    </button>
                    <button type="button" className="quiet" onClick={() => onPTZStop(tile.id)} disabled={busy}>
                      Stop
                    </button>
                    <button type="button" onClick={() => onPTZMove(tile.id, 'right')} disabled={busy}>
                      Right
                    </button>
                    <button type="button" onClick={() => onPTZMove(tile.id, 'down')} disabled={busy}>
                      Down
                    </button>
                  </div>
                ) : null}
              </>
            ) : (
              <div className="empty-tile">Empty</div>
            )}
          </article>
          );
        })}
      </div>
    </section>
  );
}

function DiscoveredDevices({ devices, saved, busy, drafts, onDraft, onSave }) {
  const [savedExpanded, setSavedExpanded] = useState(false);
  const notSavedDevices = devices.filter((device) => !saved.some((savedDevice) => sameCamera(device, savedDevice)));
  const savedDevices = devices.filter((device) => saved.some((savedDevice) => sameCamera(device, savedDevice)));

  useEffect(() => {
    setSavedExpanded(false);
  }, [devices]);

  function renderUnsaved(device) {
    const key = device.xAddr || `${device.host}:${device.port}`;
    const draft = drafts[key] || { name: cameraTitle(device), description: '' };
    return (
      <article className="device-card" key={key}>
        <div className="device-title-row">
          <div>
            <h3>{cameraTitle(device)}</h3>
            <p>{device.xAddr}</p>
          </div>
          <button type="button" onClick={() => onSave(device, draft)} disabled={busy}>
            Save
          </button>
        </div>
        <DeviceMeta device={device} />
        <div className="metadata-row">
          <label>
            Camera name
            <input
              value={draft.name}
              onChange={(event) => onDraft(key, { ...draft, name: event.target.value })}
              autoComplete="off"
            />
          </label>
          <label>
            Description
            <input
              value={draft.description}
              onChange={(event) => onDraft(key, { ...draft, description: event.target.value })}
              autoComplete="off"
            />
          </label>
        </div>
      </article>
    );
  }

  function renderSaved(device) {
    const key = device.xAddr || `${device.host}:${device.port}`;
    return (
      <article className="device-card" key={key}>
        <div className="device-title-row">
          <div>
            <h3>{cameraTitle(device)}</h3>
            <p>{device.xAddr}</p>
          </div>
          <strong className="status-pill saved">Saved</strong>
        </div>
        <DeviceMeta device={device} />
      </article>
    );
  }

  return (
    <section className="device-section">
      <header>
        <h2>Discovered</h2>
        <span>{devices.length}</span>
      </header>
      <div className="discovery-groups">
        {devices.length === 0 ? <p className="empty">No discovered devices.</p> : null}
        {notSavedDevices.length > 0 ? (
          <section className="discovery-group">
            <header>
              <h3>Not Saved</h3>
              <span className="discovery-group-count">{notSavedDevices.length}</span>
            </header>
            <div className="device-list compact">{notSavedDevices.map(renderUnsaved)}</div>
          </section>
        ) : null}
        {savedDevices.length > 0 ? (
          <section className="discovery-group">
            <header>
              <h3>Saved</h3>
              <div className="discovery-group-actions">
                <span className="discovery-group-count">{savedDevices.length}</span>
                <button
                  type="button"
                  className="quiet compact-button"
                  aria-expanded={savedExpanded}
                  onClick={() => setSavedExpanded((current) => !current)}
                >
                  {savedExpanded ? 'Collapse' : 'Expand'}
                </button>
              </div>
            </header>
            {savedExpanded ? <div className="device-list compact">{savedDevices.map(renderSaved)}</div> : null}
          </section>
        ) : null}
      </div>
    </section>
  );
}

function SavedCameraRow({
  device,
  busy,
  detailDraft,
  credentials,
  passwordDraft,
  onDetailDraft,
  onSaveDetails,
  onCredential,
  onPasswordDraft,
  onSaveCredentials,
  onChangePassword,
  onResolve,
  onTest,
  onPreview,
  onAdd,
  onRemove,
}) {
  const [activePanel, setActivePanel] = useState('details');
  const localDetails = detailDraft || { name: device.name || '', description: device.description || '' };
  const localCred = credentials || { username: device.username || '', password: '' };
  const localPasswordDraft = passwordDraft || { targetUsername: device.username || '', newPassword: '' };
  const streamReady = Boolean(device.rtspUrl);

  useEffect(() => {
    setActivePanel('details');
  }, [device.id]);

  return (
    <article className="device-card">
      <div className="device-title-row">
        <div>
          <h3>{cameraTitle(device)}</h3>
          <p>{device.xAddr}</p>
        </div>
        <strong className={`status-pill ${device.rtspStatus || 'unknown'}`}>{device.rtspStatus || 'not ready'}</strong>
      </div>

      <nav className="saved-detail-tabs" aria-label="Saved camera settings">
        {[
          ['details', 'Details'],
          ['access', 'Access'],
          ['stream', 'Stream'],
          ['onvif', 'ONVIF'],
        ].map(([id, label]) => (
          <button
            type="button"
            key={id}
            className={activePanel === id ? 'active' : 'quiet'}
            onClick={() => setActivePanel(id)}
          >
            {label}
          </button>
        ))}
      </nav>

      {activePanel === 'details' ? (
        <section className="saved-tab-panel">
          <DeviceDescription device={device} />
          <DeviceMeta device={device} />
          <form
            className="device-edit-form"
            onSubmit={(event) => {
              event.preventDefault();
              onSaveDetails(device);
            }}
          >
            <div className="metadata-row">
              <label>
                Camera name
                <input
                  value={localDetails.name}
                  onChange={(event) => onDetailDraft(device.id, { ...localDetails, name: event.target.value })}
                  autoComplete="off"
                />
              </label>
              <label>
                Description
                <input
                  value={localDetails.description}
                  onChange={(event) => onDetailDraft(device.id, { ...localDetails, description: event.target.value })}
                  autoComplete="off"
                />
              </label>
            </div>
            <div className="action-row">
              <button type="submit" className="quiet" disabled={busy}>
                Save Details
              </button>
              <button type="button" className="quiet danger-text" onClick={() => onRemove(device.id)} disabled={busy}>
                Remove
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {activePanel === 'access' ? (
        <section className="saved-tab-panel">
          <div className="credential-row">
            <label>
              Camera username
              <input
                value={localCred.username}
                onChange={(event) => onCredential(device.id, { ...localCred, username: event.target.value })}
                autoComplete="off"
              />
            </label>
            <label>
              Camera password
              <input
                value={localCred.password}
                onChange={(event) => onCredential(device.id, { ...localCred, password: event.target.value })}
                type="password"
                autoComplete="off"
                placeholder={device.hasPassword ? 'Saved password kept' : ''}
              />
              <span className={device.hasPassword ? 'field-hint good' : 'field-hint'}>
                {device.hasPassword ? 'Password saved' : 'No saved password'}
              </span>
            </label>
          </div>
          <div className="action-row">
            <button type="button" className="quiet" onClick={() => onSaveCredentials(device)} disabled={busy}>
              Save Credentials
            </button>
          </div>
          <div className="credential-row">
            <label>
              ONVIF user
              <input
                value={localPasswordDraft.targetUsername}
                onChange={(event) => onPasswordDraft(device.id, { ...localPasswordDraft, targetUsername: event.target.value })}
                placeholder={device.username || 'camera user'}
                autoComplete="off"
              />
            </label>
            <label>
              New ONVIF password
              <input
                value={localPasswordDraft.newPassword}
                onChange={(event) => onPasswordDraft(device.id, { ...localPasswordDraft, newPassword: event.target.value })}
                type="password"
                autoComplete="new-password"
              />
            </label>
          </div>
          <div className="action-row">
            <button
              type="button"
              className="quiet"
              onClick={() => onChangePassword(device)}
              disabled={busy || !localPasswordDraft.newPassword}
            >
              Change Camera Password
            </button>
          </div>
        </section>
      ) : null}

      {activePanel === 'stream' ? (
        <section className="saved-tab-panel">
          <dl className="stream-meta">
            <div>
              <dt>RTSP URI</dt>
              <dd>{fieldValue(device.rtspUrl)}</dd>
            </div>
            <div>
              <dt>Tracks</dt>
              <dd>
                <Tracks value={device.rtspTracks} />
              </dd>
            </div>
          </dl>
          <div className="stream-action-flow">
            <button type="button" onClick={() => onResolve(device)} disabled={busy}>
              1. Resolve RTSP
            </button>
            <button type="button" className="quiet" onClick={() => onTest(device)} disabled={busy || !streamReady}>
              2. Test RTSP
            </button>
            <button type="button" className="quiet" onClick={() => onPreview(device)} disabled={busy}>
              3. Live Preview
            </button>
            <button type="button" className="quiet" onClick={() => onAdd(device)} disabled={busy}>
              4. Add to Live Views
            </button>
          </div>
        </section>
      ) : null}

      {activePanel === 'onvif' ? (
        <section className="saved-tab-panel">
          <OnvifDetails device={device} />
        </section>
      ) : null}
    </article>
  );
}

function SavedDeviceNav({ devices, selectedId, onSelect }) {
  const orderedDevices = useMemo(() => orderedSavedCameras(devices), [devices]);
  return (
    <aside className="saved-sidebar">
      <header>
        <h2>Saved Cameras</h2>
        <span>{devices.length}</span>
      </header>
      <nav className="saved-device-nav" aria-label="Saved cameras">
        {devices.length === 0 ? <p className="empty">No saved cameras.</p> : null}
        {orderedDevices.map((device) => (
          <button
            type="button"
            className={Number(selectedId) === Number(device.id) ? 'saved-device-button active' : 'saved-device-button'}
            key={device.id || device.xAddr}
            onClick={() => onSelect(device.id)}
          >
            <strong>{cameraTitle(device)}</strong>
            <span>{device.host || device.xAddr || 'Camera'}</span>
          </button>
        ))}
      </nav>
    </aside>
  );
}

function CameraPreviewPanel({ preview, busy, authHeader, streamConfig, onClose, onAdd, onPTZMove, onPTZStop }) {
  if (!preview) {
    return null;
  }
  return (
    <section className="preview-panel">
      <header>
        <div>
          <h2>{preview.title}</h2>
          <p>{preview.ptzSupported ? 'PTZ controls available' : 'Live preview'}</p>
        </div>
        <button type="button" className="quiet" onClick={onClose}>
          Close
        </button>
      </header>
      <LiveViewport deviceId={preview.id} title={preview.title} authHeader={authHeader} streamConfig={streamConfig} />
      <div className="preview-actions">
        {preview.ptzSupported ? (
          <div className="preview-ptz-controls" aria-label={`${preview.title} PTZ controls`}>
            <button type="button" onClick={() => onPTZMove(preview.id, 'up')} disabled={busy}>
              Up
            </button>
            <button type="button" onClick={() => onPTZMove(preview.id, 'left')} disabled={busy}>
              Left
            </button>
            <button type="button" className="quiet" onClick={() => onPTZStop(preview.id)} disabled={busy}>
              Stop
            </button>
            <button type="button" onClick={() => onPTZMove(preview.id, 'right')} disabled={busy}>
              Right
            </button>
            <button type="button" onClick={() => onPTZMove(preview.id, 'down')} disabled={busy}>
              Down
            </button>
          </div>
        ) : null}
        <div className="action-row">
          <button type="button" className="quiet" onClick={() => onAdd(preview.device)} disabled={busy || !preview.device}>
            Add to Live Views
          </button>
        </div>
      </div>
    </section>
  );
}

function CamerasTab({
  saved,
  discovered,
  busy,
  manualAddress,
  timeoutMs,
  cameraNav,
  preview,
  authHeader,
  streamConfig,
  detailDraftsById,
  credentialsById,
  passwordDraftsById,
  saveDrafts,
  onCameraNav,
  onManualAddress,
  onTimeout,
  onScan,
  onProbe,
  onSave,
  onSaveDraft,
  onDetailDraft,
  onSaveDetails,
  onCredential,
  onPasswordDraft,
  onSaveCredentials,
  onChangePassword,
  onResolve,
  onTest,
  onPreview,
  onAddToViews,
  onPTZMove,
  onPTZStop,
  onRemove,
  onClosePreview,
}) {
  const [selectedSavedId, setSelectedSavedId] = useState(null);
  const orderedSaved = useMemo(() => orderedSavedCameras(saved), [saved]);
  const selectedSaved =
    saved.find((device) => Number(device.id) === Number(selectedSavedId)) || orderedSaved[0] || null;
  const selectedPreview =
    selectedSaved && preview && Number(preview.id) === Number(selectedSaved.id) ? preview : null;

  useEffect(() => {
    if (!saved.length) {
      if (selectedSavedId !== null) {
        setSelectedSavedId(null);
      }
      return;
    }
    if (!selectedSaved || Number(selectedSaved.id) !== Number(selectedSavedId)) {
      setSelectedSavedId(orderedSaved[0]?.id || null);
    }
  }, [saved, orderedSaved, selectedSaved, selectedSavedId]);

  return (
    <section className="workspace">
      <div className="toolbar">
        <nav className="secondary-tabs" aria-label="Cameras">
          <button type="button" className={cameraNav === 'probe' ? 'active' : 'quiet'} onClick={() => onCameraNav('probe')}>
            Probe
          </button>
          <button type="button" className={cameraNav === 'saved' ? 'active' : 'quiet'} onClick={() => onCameraNav('saved')}>
            Saved
          </button>
        </nav>
      </div>

      {cameraNav === 'probe' ? (
        <section className="camera-grid">
          <div className="probe-panel">
            <div className="scan-row">
              <label>
                Scan timeout
                <input value={timeoutMs} onChange={(event) => onTimeout(event.target.value)} inputMode="numeric" />
              </label>
              <button type="button" onClick={onScan} disabled={busy}>
                Scan local network
              </button>
            </div>
            <form className="probe-row" onSubmit={onProbe}>
              <label>
                Manual address
                <input
                  value={manualAddress}
                  onChange={(event) => onManualAddress(event.target.value)}
                  placeholder="192.168.1.40"
                />
              </label>
              <button type="submit" disabled={busy}>
                Probe
              </button>
            </form>
          </div>
          <DiscoveredDevices
            devices={discovered}
            saved={saved}
            busy={busy}
            drafts={saveDrafts}
            onDraft={onSaveDraft}
            onSave={onSave}
          />
        </section>
      ) : (
        <section className="saved-browser">
          <SavedDeviceNav devices={saved} selectedId={selectedSaved?.id} onSelect={setSelectedSavedId} />
          <main className="saved-detail">
            {selectedSaved ? (
              <>
                <SavedCameraRow
                  key={selectedSaved.id || selectedSaved.xAddr}
                  device={selectedSaved}
                  busy={busy}
                  detailDraft={detailDraftsById[selectedSaved.id] || { name: selectedSaved.name || '', description: selectedSaved.description || '' }}
                  credentials={credentialsById[selectedSaved.id] || { ...defaultDeviceCredentials, username: selectedSaved.username || '' }}
                  passwordDraft={passwordDraftsById[selectedSaved.id] || { targetUsername: selectedSaved.username || '', newPassword: '' }}
                  onDetailDraft={onDetailDraft}
                  onSaveDetails={onSaveDetails}
                  onCredential={onCredential}
                  onPasswordDraft={onPasswordDraft}
                  onSaveCredentials={onSaveCredentials}
                  onChangePassword={onChangePassword}
                  onResolve={onResolve}
                  onTest={onTest}
                  onPreview={onPreview}
                  onAdd={onAddToViews}
                  onRemove={onRemove}
                />
                <CameraPreviewPanel
                  preview={selectedPreview}
                  busy={busy}
                  authHeader={authHeader}
                  streamConfig={streamConfig}
                  onClose={onClosePreview}
                  onAdd={onAddToViews}
                  onPTZMove={onPTZMove}
                  onPTZStop={onPTZStop}
                />
              </>
            ) : (
              <section className="device-card empty-detail">
                <h2>No saved camera selected</h2>
                <p className="empty">Scan or probe a camera, then save one to manage it here.</p>
              </section>
            )}
          </main>
        </section>
      )}
    </section>
  );
}

function VisionTab({
  saved,
  rules,
  alerts,
  ruleDraft,
  busy,
  authHeader,
  streamConfig,
  onRuleDraft,
  onSaveRule,
  onEditRule,
  onDeleteRule,
  onTriggerTestAlert,
  onAcknowledgeAlert,
  onPrepareCamera,
  onReload,
}) {
  const orderedSaved = useMemo(() => orderedSavedCameras(saved), [saved]);
  const selectedCameraId = Number(ruleDraft.cameraId) || Number(orderedSaved[0]?.id) || 0;
  const selectedCamera = saved.find((device) => Number(device.id) === selectedCameraId) || orderedSaved[0] || null;
  const selectedRules = selectedCamera
    ? rules.filter((rule) => Number(rule.cameraId) === Number(selectedCamera.id))
    : [];
  const selectedAlerts = selectedCamera
    ? alerts.filter((alert) => Number(alert.cameraId) === Number(selectedCamera.id))
    : alerts;
  const selectedZonePoints = parseZonePolygon(ruleDraft.zonePolygon);
  const scheduleDraft = scheduleDraftFromPolicy(ruleDraft.schedulePolicy);
  const [selectedAlertId, setSelectedAlertId] = useState(null);
  const selectedAlert = selectedAlerts.find((alert) => Number(alert.id) === Number(selectedAlertId)) || null;
  const selectedAlertMetadata = parseMetadata(selectedAlert?.metadata);
  const selectedAlertRule = selectedAlert
    ? selectedRules.find((rule) => Number(rule.id) === Number(selectedAlert.ruleId))
    : null;

  useEffect(() => {
    if (!selectedCamera) {
      return;
    }
    if (Number(ruleDraft.cameraId) !== Number(selectedCamera.id)) {
      onRuleDraft({ ...defaultVisionRuleDraft(selectedCamera.id), id: 0 });
    }
  }, [selectedCamera?.id, ruleDraft.cameraId]);

  useEffect(() => {
    if (selectedCamera && onPrepareCamera) {
      onPrepareCamera(selectedCamera).catch(() => {});
    }
  }, [selectedCamera?.id]);

  useEffect(() => {
    if (selectedAlertId !== null && !selectedAlerts.some((alert) => Number(alert.id) === Number(selectedAlertId))) {
      setSelectedAlertId(null);
    }
  }, [selectedAlerts, selectedAlertId]);

  function selectCamera(cameraId) {
    onRuleDraft(defaultVisionRuleDraft(cameraId));
  }

  function changeSchedulePreset(preset) {
    onRuleDraft({ ...ruleDraft, schedulePolicy: schedulePresetPolicy(preset, scheduleDraft) });
  }

  function changeCustomSchedule(patch) {
    const next = { ...scheduleDraft, ...patch, preset: 'custom' };
    onRuleDraft({ ...ruleDraft, schedulePolicy: weeklySchedulePolicy(next) });
  }

  function changeRangeSchedule(patch) {
    const next = { ...scheduleDraft, ...patch, preset: 'range' };
    onRuleDraft({ ...ruleDraft, schedulePolicy: rangeSchedulePolicy(next) });
  }

  function toggleScheduleDay(day) {
    const current = new Set(scheduleDraft.days);
    if (current.has(day)) {
      current.delete(day);
    } else {
      current.add(day);
    }
    const days = scheduleDayOptions.map(([id]) => id).filter((id) => current.has(id));
    if (days.length === 0) {
      return;
    }
    changeCustomSchedule({ days });
  }

  return (
    <section className="workspace">
      <div className="toolbar">
        <div>
          <h2 className="section-title">AI Detection</h2>
          <p className="section-subtitle">Camera rules and alert events.</p>
        </div>
        <button type="button" className="quiet" onClick={onReload} disabled={busy}>
          Reload
        </button>
      </div>

      <section className="saved-browser vision-browser">
        <SavedDeviceNav devices={saved} selectedId={selectedCamera?.id} onSelect={selectCamera} />
        <main className="saved-detail">
          {selectedCamera ? (
            <>
              <section className="settings-panel">
                <header>
                  <div>
                    <h2>{cameraTitle(selectedCamera)}</h2>
                    <p className="section-subtitle">{selectedCamera.host || selectedCamera.xAddr || 'Saved camera'}</p>
                  </div>
                  <span className="status-pill">{selectedRules.length} rules</span>
                </header>
                <form className="vision-rule-form" onSubmit={onSaveRule}>
                  <header>
                    <h2>{ruleDraft.id ? 'Edit Rule' : 'New Rule'}</h2>
                    {ruleDraft.id ? (
                      <button type="button" className="quiet" onClick={() => onRuleDraft(defaultVisionRuleDraft(selectedCamera.id))} disabled={busy}>
                        New Rule
                      </button>
                    ) : null}
                  </header>
                  <div className="metadata-row">
                    <label>
                      Rule name
                      <input
                        value={ruleDraft.name || ''}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, name: event.target.value })}
                        placeholder={`${cameraTitle(selectedCamera)} fire watch`}
                      />
                    </label>
                    <label>
                      Detection type
                      <select
                        value={ruleDraft.detectionType}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, detectionType: event.target.value })}
                      >
                        <option value="fire">Fire</option>
                        <option value="smoke">Smoke</option>
                        <option value="person">Person</option>
                        <option value="vehicle">Vehicle</option>
                        <option value="intrusion">Intrusion</option>
                      </select>
                    </label>
                  </div>
                  <div className="metadata-row">
                    <label>
                      Threshold
                      <input
                        type="number"
                        min="0.01"
                        max="1"
                        step="0.01"
                        value={ruleDraft.threshold}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, threshold: Number(event.target.value) })}
                      />
                    </label>
                    <label>
                      Min frames
                      <input
                        type="number"
                        min="1"
                        value={ruleDraft.minFrames}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, minFrames: Number(event.target.value) })}
                      />
                    </label>
                  </div>
                  <label>
                    Cooldown seconds
                    <input
                      type="number"
                      min="0"
                      value={ruleDraft.cooldownSeconds}
                      onChange={(event) => onRuleDraft({ ...ruleDraft, cooldownSeconds: Number(event.target.value) })}
                    />
                  </label>
                  <section className="schedule-panel">
                    <header>
                      <h3>Schedule</h3>
                      <span className="status-pill">{scheduleSummary(ruleDraft.schedulePolicy)}</span>
                    </header>
                    <div className="metadata-row">
                      <label>
                        Detection schedule
                        <select value={scheduleDraft.preset} onChange={(event) => changeSchedulePreset(event.target.value)}>
                          <option value="always">Always active</option>
                          <option value="daytime">Daytime</option>
                          <option value="nighttime">Nighttime</option>
                          <option value="weekdays">Weekdays</option>
                          <option value="weekends">Weekends</option>
                          <option value="custom">Custom weekly</option>
                          <option value="range">Specific datetime</option>
                        </select>
                      </label>
                      {scheduleDraft.preset === 'custom' || scheduleDraft.preset === 'range' ? (
                        <label>
                          Policy mode
                          <select
                            value={scheduleDraft.mode}
                            onChange={(event) => {
                              if (scheduleDraft.preset === 'range') {
                                changeRangeSchedule({ mode: event.target.value });
                              } else {
                                changeCustomSchedule({ mode: event.target.value });
                              }
                            }}
                          >
                            <option value="allow">Detect only during this schedule</option>
                            <option value="deny">Pause during this schedule</option>
                          </select>
                        </label>
                      ) : null}
                    </div>
                    {scheduleDraft.preset === 'custom' ? (
                      <>
                        <label>
                          Timezone
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeCustomSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="schedule-edit-block">
                          <strong>Active days</strong>
                          <div className="schedule-days" aria-label="Schedule days">
                            {scheduleDayOptions.map(([day, label]) => (
                              <label className="check-row" key={day}>
                                <input
                                  type="checkbox"
                                  checked={scheduleDraft.days.includes(day)}
                                  onChange={() => toggleScheduleDay(day)}
                                />
                                {label}
                              </label>
                            ))}
                          </div>
                        </div>
                        <div className="metadata-row">
                          <label>
                            Start time (HH:MM)
                            <input
                              type="time"
                              value={scheduleDraft.start}
                              onChange={(event) => changeCustomSchedule({ start: event.target.value })}
                            />
                          </label>
                          <label>
                            End time (HH:MM)
                            <input
                              type="time"
                              value={scheduleDraft.end}
                              onChange={(event) => changeCustomSchedule({ end: event.target.value })}
                            />
                          </label>
                        </div>
                      </>
                    ) : null}
                    {scheduleDraft.preset === 'range' ? (
                      <>
                        <label>
                          Timezone
                          <input
                            value={scheduleDraft.timezone}
                            onChange={(event) => changeRangeSchedule({ timezone: event.target.value })}
                            placeholder="Asia/Kuala_Lumpur"
                            autoComplete="off"
                          />
                        </label>
                        <div className="metadata-row">
                          <label>
                            Start datetime
                            <input
                              type="datetime-local"
                              value={scheduleDraft.rangeStart}
                              onChange={(event) => changeRangeSchedule({ rangeStart: event.target.value })}
                            />
                          </label>
                          <label>
                            End datetime
                            <input
                              type="datetime-local"
                              value={scheduleDraft.rangeEnd}
                              onChange={(event) => changeRangeSchedule({ rangeEnd: event.target.value })}
                            />
                          </label>
                        </div>
                      </>
                    ) : null}
                  </section>
                  <ZoneDrawingPreview
                    camera={selectedCamera}
                    polygonValue={ruleDraft.zonePolygon}
                    authHeader={authHeader}
                    streamConfig={streamConfig}
                    disabled={busy}
                    onPolygon={(zonePolygon) => onRuleDraft({ ...ruleDraft, cameraId: selectedCamera.id, zonePolygon })}
                  />
                  <div className="action-row">
                    <label className="check-row">
                      <input
                        type="checkbox"
                        checked={Boolean(ruleDraft.soundEnabled)}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, soundEnabled: event.target.checked })}
                      />
                      Sound alert
                    </label>
                    <label className="check-row">
                      <input
                        type="checkbox"
                        checked={Boolean(ruleDraft.isEnabled)}
                        onChange={(event) => onRuleDraft({ ...ruleDraft, isEnabled: event.target.checked })}
                      />
                      Enabled
                    </label>
                  </div>
                  <div className="action-row">
                    <button type="submit" disabled={busy || selectedZonePoints.length < 3}>
                      Save Rule
                    </button>
                    <button
                      type="button"
                      className="quiet"
                      onClick={() => onRuleDraft(defaultVisionRuleDraft(selectedCamera.id))}
                      disabled={busy}
                    >
                      Clear
                    </button>
                  </div>
                </form>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>Rules</h2>
                  <span className="status-pill">{selectedRules.length}</span>
                </header>
                <div className="vision-list">
                  {selectedRules.length === 0 ? <p className="empty">No AI detection rules for this camera.</p> : null}
                  {selectedRules.map((rule) => (
                    <article className="vision-row" key={rule.id}>
                      <div>
                        <h3>{rule.name || rule.detectionType}</h3>
                        <p>{rule.detectionType} / threshold {Number(rule.threshold || 0).toFixed(2)} / {scheduleSummary(rule.schedulePolicy)}</p>
                      </div>
                      <strong className={`status-pill ${rule.isEnabled ? 'online' : 'unknown'}`}>
                        {rule.isEnabled ? 'enabled' : 'disabled'}
                      </strong>
                      <div className="action-row">
                        <button type="button" className="quiet" onClick={() => onEditRule(rule)} disabled={busy}>
                          Edit
                        </button>
                        <button type="button" onClick={() => onTriggerTestAlert(rule)} disabled={busy}>
                          Test Alert
                        </button>
                        <button type="button" className="quiet danger-text" onClick={() => onDeleteRule(rule.id)} disabled={busy}>
                          Delete
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>Alert Log</h2>
                  <span className="status-pill">{selectedAlerts.length}</span>
                </header>
                <div className="vision-list">
                  {selectedAlerts.length === 0 ? <p className="empty">No alert events for this camera.</p> : null}
                  {selectedAlerts.length > 0 ? (
                    <div className="event-table-wrap">
                      <table className="event-table">
                        <thead>
                          <tr>
                            <th>Time</th>
                            <th>Event</th>
                            <th>Rule</th>
                            <th>Confidence</th>
                            <th>Status</th>
                            <th>Action</th>
                          </tr>
                        </thead>
                        <tbody>
                          {selectedAlerts.map((alert) => {
                            const metadata = parseMetadata(alert.metadata);
                            const diagnostic = Boolean(metadata.diagnostic);
                            const rule = selectedRules.find((item) => Number(item.id) === Number(alert.ruleId));
                            return (
                              <tr key={alert.id} className={Number(selectedAlertId) === Number(alert.id) ? 'selected' : ''}>
                                <td>{formatTimestamp(alert.createdAt)}</td>
                                <td>
                                  <strong>{alert.label || alert.detectionType || 'Detection event'}</strong>
                                  <span>{metadata.source || fieldValue(alert.detectionType)}</span>
                                </td>
                                <td>{rule?.name || `#${alert.ruleId || '-'}`}</td>
                                <td>{Number(alert.confidence || 0).toFixed(3)}</td>
                                <td>
                                  <span className={`status-pill ${diagnostic ? 'unknown' : alert.isAcknowledged ? 'resolved' : 'offline'}`}>
                                    {diagnostic ? 'diagnostic' : alert.isAcknowledged ? 'acknowledged' : 'active'}
                                  </span>
                                </td>
                                <td>
                                  <div className="table-actions">
                                    <button type="button" className="quiet" onClick={() => setSelectedAlertId(alert.id)}>
                                      Details
                                    </button>
                                    <button
                                      type="button"
                                      className="quiet"
                                      onClick={() => onAcknowledgeAlert(alert.id)}
                                      disabled={busy || alert.isAcknowledged || diagnostic}
                                    >
                                      Ack
                                    </button>
                                  </div>
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  ) : null}
                  {selectedAlert ? (
                    <section className="event-detail-panel">
                      <header>
                        <div>
                          <h3>{selectedAlert.label || selectedAlert.detectionType || 'Detection event'}</h3>
                          <p>{formatTimestamp(selectedAlert.createdAt)}</p>
                        </div>
                        <button type="button" className="quiet" onClick={() => setSelectedAlertId(null)}>
                          Close
                        </button>
                      </header>
                      <dl className="event-grid">
                        <div>
                          <dt>Rule</dt>
                          <dd>{selectedAlertRule?.name || `#${selectedAlert.ruleId || '-'}`}</dd>
                        </div>
                        <div>
                          <dt>Type</dt>
                          <dd>{fieldValue(selectedAlert.detectionType)}</dd>
                        </div>
                        <div>
                          <dt>Confidence</dt>
                          <dd>{Number(selectedAlert.confidence || 0).toFixed(3)}</dd>
                        </div>
                        <div>
                          <dt>Motion</dt>
                          <dd>{selectedAlertMetadata.changedRatio === undefined ? '-' : formatPercent(selectedAlertMetadata.changedRatio)}</dd>
                        </div>
                        <div>
                          <dt>Monitor</dt>
                          <dd>{fieldValue(selectedAlertMetadata.status)}</dd>
                        </div>
                        <div>
                          <dt>Acknowledged</dt>
                          <dd>{selectedAlert.isAcknowledged ? formatTimestamp(selectedAlert.acknowledgedAt) : '-'}</dd>
                        </div>
                      </dl>
                      {Object.keys(selectedAlertMetadata).length > 0 ? (
                        <div className="event-details">
                          <strong>Metadata</strong>
                          <pre>{JSON.stringify(selectedAlertMetadata, null, 2)}</pre>
                        </div>
                      ) : null}
                    </section>
                  ) : null}
                </div>
              </section>
            </>
          ) : (
            <section className="device-card empty-detail">
              <h2>No saved camera selected</h2>
              <p className="empty">Save a camera first, then create AI detection rules here.</p>
            </section>
          )}
        </main>
      </section>
    </section>
  );
}

function SettingsTab({
  settingsNav,
  settings,
  users,
  newUser,
  passwordDrafts,
  busy,
  onChange,
  onSettingsNav,
  onSave,
  onReset,
  onLoadUsers,
  onNewUser,
  onCreateUser,
  onEditUser,
  onUpdateUser,
  onPasswordDraft,
  onResetPassword,
  onDeleteUser,
}) {
  const iceServers = settings.stream.webrtc.iceServers || [];
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

  return (
    <section className="workspace settings-workspace">
      <aside className="settings-side-nav" aria-label="Settings">
        <button type="button" className={settingsNav === 'runtime' ? 'active' : 'quiet'} onClick={() => onSettingsNav('runtime')}>
          Runtime
        </button>
        <button type="button" className={settingsNav === 'users' ? 'active' : 'quiet'} onClick={() => onSettingsNav('users')}>
          Users
        </button>
      </aside>

      <div className="settings-content">
        {settingsNav === 'runtime' ? (
          <form className="settings-layout" onSubmit={onSave}>
        <section className="settings-panel">
          <header>
            <h2>Decoder</h2>
          </header>
          <label>
            MJPEG ffmpeg path
            <input
              value={settings.decoder.mjpeg.ffmpegPath}
              onChange={(event) =>
                update((current) => ({
                  ...current,
                  decoder: {
                    ...current.decoder,
                    mjpeg: { ...current.decoder.mjpeg, ffmpegPath: event.target.value },
                  },
                }))
              }
              placeholder="ffmpeg"
              autoComplete="off"
            />
          </label>
        </section>

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

        <div className="settings-actions">
          <button type="submit" disabled={busy}>
            Save Settings
          </button>
          <button type="button" className="quiet" onClick={onReset} disabled={busy}>
            Reset Defaults
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
            Add User
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
                  Save
                </button>
                <button
                  type="button"
                  className="quiet"
                  onClick={() => onResetPassword(user)}
                  disabled={busy || !(passwordDrafts[user.id] || '').trim()}
                >
                  Reset Password
                </button>
                <button type="button" className="quiet danger-text" onClick={() => onDeleteUser(user)} disabled={busy}>
                  Delete
                </button>
              </div>
            </article>
          ))}
        </div>
          </section>
        ) : null}
      </div>
    </section>
  );
}

export default function App() {
  const initialLiveViews = readLiveViewsCookie();
  const [credentials, setCredentials] = useState(emptyLogin);
  const [authenticated, setAuthenticated] = useState(false);
  const [activeTab, setActiveTab] = useState('views');
  const [settingsNav, setSettingsNav] = useState('runtime');
  const [cameraNav, setCameraNav] = useState('probe');
  const [manualAddress, setManualAddress] = useState('');
  const [timeoutMs, setTimeoutMs] = useState(3000);
  const [saved, setSaved] = useState([]);
  const [discovered, setDiscovered] = useState([]);
  const [saveDrafts, setSaveDrafts] = useState({});
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [deviceDrafts, setDeviceDrafts] = useState({});
  const [deviceCredentials, setDeviceCredentials] = useState({});
  const [cameraPasswordDrafts, setCameraPasswordDrafts] = useState({});
  const [viewLayout, setViewLayout] = useState(initialLiveViews.layout);
  const [viewTiles, setViewTiles] = useState([]);
  const [draggedTileId, setDraggedTileId] = useState(null);
  const [preview, setPreview] = useState(null);
  const [streamConfig, setStreamConfig] = useState(defaultStreamConfig);
  const [runtimeSettings, setRuntimeSettings] = useState(defaultRuntimeSettings);
  const [users, setUsers] = useState([]);
  const [newUser, setNewUser] = useState(defaultNewUser);
  const [passwordDrafts, setPasswordDrafts] = useState({});
  const [visionRules, setVisionRules] = useState([]);
  const [visionAlerts, setVisionAlerts] = useState([]);
  const [visionRuleDraft, setVisionRuleDraft] = useState(defaultVisionRuleDraft());
  const seenVisionAlertIdsRef = useRef(new Set());
  const activeVisionAlertsByCamera = useMemo(() => latestAlertsByCamera(visionAlerts), [visionAlerts]);

  const authHeader = useMemo(() => {
    if (!credentials.username && !credentials.password) {
      return '';
    }
    return `Basic ${btoa(`${credentials.username}:${credentials.password}`)}`;
  }, [credentials]);

  async function request(path, options = {}) {
    const headers = {
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    };
    if (authHeader) {
      headers.Authorization = authHeader;
    }
    const response = await fetch(`${apiBase()}${path}`, {
      ...options,
      credentials: 'include',
      headers,
    });
    const text = await response.text();
    let payload = null;
    if (text) {
      try {
        payload = JSON.parse(text);
      } catch (_) {
        payload = { message: text };
      }
    }
    if (!response.ok) {
      throw new Error(errorMessage(payload, `Request failed with ${response.status}`));
    }
    return unwrap(payload);
  }

  async function refresh({ quiet = false } = {}) {
    setBusy(true);
    if (!quiet) {
      setMessage('');
    }
    try {
      await loadRuntimeSettings();
      const result = await request('/api/onvif/devices?limit=100&offset=0');
      const devices = Array.isArray(result) ? result : [];
      const orderedDevices = orderedSavedCameras(devices);
      setVisionRuleDraft((current) => ({ ...current, cameraId: current.cameraId || orderedDevices[0]?.id || '' }));
      const preference = readLiveViewsCookie(viewLayout);
      const nextTiles = viewTiles.length > 0 ? null : await resolvedTilesFromDevices(devices, preference);
      setSaved(devices);
      if (nextTiles) {
        setViewLayout(preference.layout);
        setViewTiles(nextTiles);
        saveLiveViewsCookie(preference.layout, nextTiles);
      } else {
        setViewTiles((current) => enrichTilesWithDevices(current, devices));
      }
      if (!quiet) {
        setMessage('Saved cameras refreshed.');
      }
      return Array.isArray(result) ? result : [];
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      setBusy(false);
    }
  }

  async function login(event) {
    event.preventDefault();
    if (!credentials.username || !credentials.password) {
      setMessage('Username and password are required.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      await loadRuntimeSettings();
      const result = await request('/api/onvif/devices?limit=100&offset=0');
      const devices = Array.isArray(result) ? result : [];
      const orderedDevices = orderedSavedCameras(devices);
      const preference = readLiveViewsCookie(viewLayout);
      const nextTiles = await resolvedTilesFromDevices(devices, preference);
      setSaved(devices);
      setVisionRuleDraft((current) => ({ ...current, cameraId: current.cameraId || orderedDevices[0]?.id || '' }));
      setViewLayout(preference.layout);
      setViewTiles(nextTiles);
      saveLiveViewsCookie(preference.layout, nextTiles);
      setAuthenticated(true);
      setActiveTab('views');
      setMessage('');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  function logout() {
    setAuthenticated(false);
    setCredentials(emptyLogin);
    setSaved([]);
    setDiscovered([]);
    setSaveDrafts({});
    setDeviceDrafts({});
    setDeviceCredentials({});
    setCameraPasswordDrafts({});
    setViewTiles([]);
    setPreview(null);
    setStreamConfig(defaultStreamConfig);
    setRuntimeSettings(defaultRuntimeSettings);
    setUsers([]);
    setNewUser(defaultNewUser);
    setPasswordDrafts({});
    setVisionRules([]);
    setVisionAlerts([]);
    setVisionRuleDraft(defaultVisionRuleDraft());
    setMessage('');
  }

  async function loadRuntimeSettings() {
    const result = normalizeRuntimeSettings(await request('/api/settings/runtime'));
    setRuntimeSettings(result);
    setStreamConfig(result.stream);
    return result;
  }

  async function loadUsers({ quiet = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const result = await request('/api/settings/users?limit=100&offset=0');
      const items = Array.isArray(result) ? result : result?.items || [];
      setUsers(items);
      if (!quiet) {
        setMessage('Users loaded.');
      }
      return items;
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function loadVision({ quiet = false, notifyNew = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const [rulesResult, alertsResult] = await Promise.all([
        request('/api/vision/rules?limit=100&offset=0'),
        request('/api/vision/alerts?limit=100&offset=0'),
      ]);
      const rules = Array.isArray(rulesResult) ? rulesResult : rulesResult?.items || [];
      const alerts = Array.isArray(alertsResult) ? alertsResult : alertsResult?.items || [];
      setVisionRules(rules);
      setVisionAlerts(alerts);
      const seen = seenVisionAlertIdsRef.current;
      const newActiveAlerts = alerts.filter((alert) => alert?.id && !alert.isAcknowledged && !seen.has(alert.id));
      alerts.forEach((alert) => {
        if (alert?.id) {
          seen.add(alert.id);
        }
      });
      if (notifyNew && newActiveAlerts.length > 0) {
        if (newActiveAlerts.some((alert) => {
          if (parseMetadata(alert.metadata).diagnostic) {
            return false;
          }
          const rule = rules.find((item) => Number(item.id) === Number(alert.ruleId));
          return !rule || rule.soundEnabled;
        })) {
          playAlertSound();
        }
      }
      if (!quiet) {
        setMessage('AI rules and alerts loaded.');
      }
      return { rules, alerts };
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  useEffect(() => {
    if (!authenticated || !['views', 'ai'].includes(activeTab)) {
      return undefined;
    }
    loadVision({ quiet: true }).catch(() => {});
    const id = window.setInterval(() => {
      loadVision({ quiet: true, notifyNew: true }).catch(() => {});
    }, 3000);
    return () => window.clearInterval(id);
  }, [authenticated, activeTab]);

  function openCameraAlerts(cameraId) {
    setVisionRuleDraft(defaultVisionRuleDraft(cameraId));
    setActiveTab('ai');
    loadVision({ quiet: true }).catch(() => {});
  }

  async function saveRuntimeSettings(event) {
    event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      const result = normalizeRuntimeSettings(
        await request('/api/settings/runtime', {
          method: 'PUT',
          body: JSON.stringify(runtimeSettings),
        })
      );
      setRuntimeSettings(result);
      setStreamConfig(result.stream);
      setMessage('Settings saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function resetRuntimeSettings() {
    setBusy(true);
    setMessage('');
    try {
      const result = normalizeRuntimeSettings(
        await request('/api/settings/runtime/reset', {
          method: 'POST',
        })
      );
      setRuntimeSettings(result);
      setStreamConfig(result.stream);
      setMessage('Settings reset to config defaults.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function createUser(event) {
    event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      await request('/api/settings/users', {
        method: 'POST',
        body: JSON.stringify(newUser),
      });
      setNewUser(defaultNewUser);
      await loadUsers({ quiet: true });
      setMessage('User created.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  function editUser(id, patch) {
    setUsers((current) => current.map((user) => (user.id === id ? { ...user, ...patch } : user)));
  }

  async function updateUser(user) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/settings/users/${user.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          username: user.username,
          displayName: user.displayName,
          isAdmin: Boolean(user.isAdmin),
          isActive: Boolean(user.isActive),
        }),
      });
      await loadUsers({ quiet: true });
      setMessage('User saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function resetUserPassword(user) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/settings/users/${user.id}/password`, {
        method: 'POST',
        body: JSON.stringify({ password: passwordDrafts[user.id] || '' }),
      });
      setPasswordDrafts((current) => ({ ...current, [user.id]: '' }));
      setMessage('Password reset.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function deleteUser(user) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/settings/users/${user.id}`, { method: 'DELETE' });
      await loadUsers({ quiet: true });
      setMessage('User deleted.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function saveVisionRule(event) {
    event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      await request('/api/vision/rules', {
        method: 'POST',
        body: JSON.stringify(visionRuleDraft),
      });
      setVisionRuleDraft(defaultVisionRuleDraft(visionRuleDraft.cameraId || orderedSavedCameras(saved)[0]?.id));
      await loadVision({ quiet: true });
      setMessage('AI detection rule saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  function editVisionRule(rule) {
    setVisionRuleDraft({
      id: rule.id,
      cameraId: rule.cameraId || '',
      name: rule.name || '',
      detectionType: rule.detectionType || 'fire',
      zonePolygon: rule.zonePolygon || defaultZonePolygon,
      schedulePolicy: rule.schedulePolicy || '',
      threshold: rule.threshold || 0.75,
      minFrames: rule.minFrames || 3,
      cooldownSeconds: rule.cooldownSeconds || 30,
      soundEnabled: Boolean(rule.soundEnabled),
      isEnabled: Boolean(rule.isEnabled),
    });
    const camera = saved.find((device) => Number(device.id) === Number(rule.cameraId));
    if (camera) {
      prepareVisionLiveView(camera).catch(() => {});
    }
  }

  async function deleteVisionRule(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/vision/rules/${id}`, { method: 'DELETE' });
      await loadVision({ quiet: true });
      setMessage('AI detection rule deleted.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function triggerTestAlert(rule) {
    setBusy(true);
    setMessage('');
    try {
      const alert = await request('/api/vision/alerts', {
        method: 'POST',
        body: JSON.stringify({
          ruleId: rule.id,
          cameraId: rule.cameraId,
          detectionType: rule.detectionType,
          label: `Test ${rule.detectionType}`,
          confidence: Math.max(0.01, Math.min(1, rule.threshold || 0.75)),
          zonePolygon: rule.zonePolygon,
          metadata: JSON.stringify({ source: 'manual-test' }),
        }),
      });
      if (alert?.id) {
        seenVisionAlertIdsRef.current.add(alert.id);
      }
      setVisionAlerts((current) => [alert, ...current]);
      if (rule.soundEnabled) {
        playAlertSound();
      }
      setMessage('Test alert created.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function prepareVisionLiveView(device) {
    if (!device?.id) {
      return;
    }
    try {
      await ensureLiveView(device);
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      throw err;
    }
  }

  async function acknowledgeAlert(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/vision/alerts/${id}/ack`, { method: 'POST' });
      await loadVision({ quiet: true });
      setMessage('Alert acknowledged.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  function tileFromDevice(device) {
    return {
      id: device.id,
      title: cameraTitle(device),
      src: liveSource(device.id),
      ptzSupported: Boolean(device.ptzSupported),
    };
  }

  function enrichTilesWithDevices(tiles, devices) {
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    return tiles.map((tile) => {
      const device = devicesById.get(Number(tile.id));
      if (!device) {
        return tile;
      }
      return { ...tile, title: cameraTitle(device), ptzSupported: Boolean(device.ptzSupported) };
    });
  }

  function setViewTilesWithCookie(updater, layout = viewLayout) {
    setViewTiles((current) => {
      const next = typeof updater === 'function' ? updater(current) : updater;
      saveLiveViewsCookie(layout, next);
      return next;
    });
  }

  function moveViewTile(fromIndex, toIndex) {
    setViewTilesWithCookie((current) => {
      if (
        !Number.isInteger(fromIndex) ||
        !Number.isInteger(toIndex) ||
        fromIndex < 0 ||
        toIndex < 0 ||
        fromIndex >= current.length ||
        toIndex >= current.length ||
        fromIndex === toIndex
      ) {
        return current;
      }
      const next = [...current];
      const [moved] = next.splice(fromIndex, 1);
      next.splice(toIndex, 0, moved);
      return next;
    });
  }

  function openSettingsSection(section) {
    setSettingsNav(section);
    if (section === 'users') {
      loadUsers().catch(() => {});
    }
  }

  async function scan() {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/onvif/discover', {
        method: 'POST',
        body: JSON.stringify({ timeoutMs: Number(timeoutMs) || 3000 }),
      });
      const devices = Array.isArray(result) ? result : [];
      const newCount = devices.filter((device) => !saved.some((savedDevice) => sameCamera(device, savedDevice))).length;
      const savedCount = devices.length - newCount;
      setDiscovered(devices);
      setSaveDrafts((current) => {
        const next = { ...current };
        devices.forEach((device) => {
          const key = device.xAddr || `${device.host}:${device.port}`;
          if (!next[key]) {
            next[key] = { name: cameraTitle(device), description: '' };
          }
        });
        return next;
      });
      setMessage(`${devices.length} device(s) discovered: ${newCount} not saved, ${savedCount} saved.`);
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function probe(event) {
    event.preventDefault();
    if (!manualAddress.trim()) {
      setMessage('Manual address is required.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/onvif/probe', {
        method: 'POST',
        body: JSON.stringify({ address: manualAddress.trim() }),
      });
      setDiscovered((current) => [result, ...current.filter((item) => item.xAddr !== result.xAddr)]);
      setSaveDrafts((current) => ({
        ...current,
        [result.xAddr || `${result.host}:${result.port}`]: { name: cameraTitle(result), description: '' },
      }));
      setMessage('Manual probe completed.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function save(device, draft = {}) {
    setBusy(true);
    setMessage('');
    try {
      await request('/api/onvif/devices/discovered', {
        method: 'POST',
        body: JSON.stringify({
          ...device,
          name: (draft.name || '').trim() || cameraTitle(device),
          description: (draft.description || '').trim(),
        }),
      });
      setMessage('Camera saved.');
      await refresh({ quiet: true });
      setCameraNav('saved');
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function saveDeviceDetails(device) {
    const draft = deviceDrafts[device.id] || { name: device.name || '', description: device.description || '' };
    setBusy(true);
    setMessage('');
    try {
      await request('/api/onvif/devices', {
        method: 'POST',
        body: JSON.stringify({
          ...device,
          hasPassword: undefined,
          name: (draft.name || '').trim() || cameraTitle(device),
          description: (draft.description || '').trim(),
        }),
      });
      setMessage('Camera details saved.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  function credentialsFor(device) {
    return deviceCredentials[device.id] || { username: device.username || '', password: '' };
  }

  async function resolvedTilesFromDevices(devices, preference = readLiveViewsCookie(viewLayout)) {
    const layout = preference.layout === '4x4' ? '4x4' : '2x2';
    const maxTiles = layout === '4x4' ? 16 : 4;
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    const targets = preference.hasPreference
      ? preference.ids.map((id) => devicesById.get(Number(id))).filter(Boolean).slice(0, maxTiles)
      : devices.slice(0, maxTiles);
    const results = await Promise.allSettled(targets.map((device) => ensureLiveView(device)));
    const failed = results.filter((result) => result.status === 'rejected').length;
    if (failed > 0) {
      setMessage(`${failed} saved camera(s) may still be resolving live view.`);
    }
    return targets.map((device, idx) => {
      const result = results[idx].status === 'fulfilled' ? results[idx].value : device;
      return {
        ...tileFromDevice(device),
        title: cameraTitle(result),
        ptzSupported: Boolean(result.ptzSupported || device.ptzSupported),
      };
    });
  }

  async function saveCredentials(device, { quiet = false } = {}) {
    const cameraCredentials = credentialsFor(device);
    if (!cameraCredentials.username && !cameraCredentials.password) {
      if (!quiet) {
        setMessage('Camera username or password is required.');
      }
      return null;
    }
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const result = await request(`/api/onvif/devices/${device.id}/credentials`, {
        method: 'POST',
        body: JSON.stringify(cameraCredentials),
      });
      if (!quiet) {
        setMessage('Camera credentials saved.');
        await refresh({ quiet: true });
      }
      return result;
    } catch (err) {
      setMessage(err.message);
      if (!quiet) {
        setBusy(false);
      }
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function changeCameraPassword(device) {
    const draft = cameraPasswordDrafts[device.id] || {};
    if (!draft.newPassword) {
      setMessage('New ONVIF password is required.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      const result = await request(`/api/onvif/devices/${device.id}/camera-password`, {
        method: 'POST',
        body: JSON.stringify({
          targetUsername: (draft.targetUsername || '').trim() || device.username,
          newPassword: draft.newPassword,
        }),
      });
      setCameraPasswordDrafts((current) => ({
        ...current,
        [device.id]: { targetUsername: result.username || device.username || '', newPassword: '' },
      }));
      setDeviceCredentials((current) => ({
        ...current,
        [device.id]: { username: result.username || device.username || '', password: draft.newPassword },
      }));
      setMessage('Camera password changed.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function movePTZ(deviceId, direction) {
    setMessage('');
    try {
      await request(`/api/onvif/devices/${deviceId}/ptz/move`, {
        method: 'POST',
        body: JSON.stringify({ direction, speed: 0.35, durationMs: 350 }),
      });
    } catch (err) {
      setMessage(err.message);
    }
  }

  async function stopPTZ(deviceId) {
    setMessage('');
    try {
      await request(`/api/onvif/devices/${deviceId}/ptz/stop`, { method: 'POST' });
    } catch (err) {
      setMessage(err.message);
    }
  }

  async function resolveStream(device) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/onvif/devices/${device.id}/stream-uri`, {
        method: 'POST',
        body: JSON.stringify(credentialsFor(device)),
      });
      setMessage('RTSP URI resolved.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function testStream(device) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request(`/api/onvif/devices/${device.id}/rtsp-test`, { method: 'POST' });
      setMessage(`RTSP online: ${(result.tracks || []).length} track(s).`);
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function ensureLiveView(device) {
    const cameraCredentials = credentialsFor(device);
    if (cameraCredentials.username || cameraCredentials.password) {
      await saveCredentials(device, { quiet: true });
    }
    const result = await request(`/api/onvif/devices/${device.id}/live-view`, {
      method: 'POST',
      body: JSON.stringify(cameraCredentials),
    });
    return result || device;
  }

  async function previewCamera(device) {
    setBusy(true);
    setMessage('');
    try {
      const result = await ensureLiveView(device);
      setPreview({
        id: device.id,
        title: cameraTitle(result),
        device: { ...device, ...result },
        ptzSupported: Boolean(result.ptzSupported || device.ptzSupported),
      });
      setMessage('Live preview opened.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function addToViews(device) {
    const maxTiles = viewLayout === '4x4' ? 16 : 4;
    if (viewTiles.some((tile) => tile.id === device.id)) {
      setActiveTab('views');
      return;
    }
    if (viewTiles.length >= maxTiles) {
      setMessage(`${viewLayout} view is full.`);
      setActiveTab('views');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      let result = device;
      try {
        result = await ensureLiveView(device);
      } catch (err) {
        setMessage('Camera added to Live Views; live stream may still be resolving.');
      }
      setViewTilesWithCookie((current) => [
        ...current,
        {
          ...tileFromDevice(device),
          title: cameraTitle(result),
          ptzSupported: Boolean(result.ptzSupported || device.ptzSupported),
        },
      ]);
      setActiveTab('views');
      setMessage('Camera added to Live Views.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function removeDevice(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/onvif/devices/${id}`, { method: 'DELETE' });
      setMessage('Camera removed.');
      setViewTilesWithCookie((current) => current.filter((tile) => tile.id !== id));
      setPreview((current) => (current?.id === id ? null : current));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  if (!authenticated) {
    return (
      <LoginPage
        credentials={credentials}
        busy={busy}
        message={message}
        onChange={setCredentials}
        onSubmit={login}
      />
    );
  }

  return (
    <main className="app-shell">
      <style>{styles}</style>
      <TopBar
        activeTab={activeTab}
        busy={busy}
        onTab={(tab) => {
          setActiveTab(tab);
          if (tab === 'settings' && settingsNav === 'users') {
            loadUsers().catch(() => {});
          }
          if (tab === 'ai') {
            loadVision({ quiet: true }).catch(() => {});
          }
        }}
        onRefresh={() => refresh()}
        onLogout={logout}
      />
      <Message value={message} />

      {activeTab === 'views' ? (
        <ViewsTab
          devices={saved}
          layout={viewLayout}
          viewTiles={viewTiles}
          alertsByCamera={activeVisionAlertsByCamera}
          draggedTileId={draggedTileId}
          busy={busy}
          authHeader={authHeader}
          streamConfig={streamConfig}
          onLayout={(value) => {
            setViewLayout(value);
            setViewTilesWithCookie((current) => current.slice(0, value === '4x4' ? 16 : 4), value);
          }}
          onAdd={addToViews}
          onRemove={(id) => setViewTilesWithCookie((current) => current.filter((tile) => tile.id !== id))}
          onMove={moveViewTile}
          onDragTile={setDraggedTileId}
          onPTZMove={movePTZ}
          onPTZStop={stopPTZ}
          onOpenAlerts={openCameraAlerts}
        />
      ) : null}

      {activeTab === 'cameras' ? (
        <CamerasTab
          saved={saved}
          discovered={discovered}
          busy={busy}
          manualAddress={manualAddress}
          timeoutMs={timeoutMs}
          cameraNav={cameraNav}
          preview={preview}
          authHeader={authHeader}
          streamConfig={streamConfig}
          detailDraftsById={deviceDrafts}
          credentialsById={deviceCredentials}
          passwordDraftsById={cameraPasswordDrafts}
          saveDrafts={saveDrafts}
          onCameraNav={setCameraNav}
          onManualAddress={setManualAddress}
          onTimeout={setTimeoutMs}
          onScan={scan}
          onProbe={probe}
          onSave={save}
          onSaveDraft={(key, value) => setSaveDrafts((current) => ({ ...current, [key]: value }))}
          onDetailDraft={(id, value) => setDeviceDrafts((current) => ({ ...current, [id]: value }))}
          onSaveDetails={saveDeviceDetails}
          onCredential={(id, value) => setDeviceCredentials((current) => ({ ...current, [id]: value }))}
          onPasswordDraft={(id, value) => setCameraPasswordDrafts((current) => ({ ...current, [id]: value }))}
          onSaveCredentials={saveCredentials}
          onChangePassword={changeCameraPassword}
          onResolve={resolveStream}
          onTest={testStream}
          onPreview={previewCamera}
          onAddToViews={addToViews}
          onPTZMove={movePTZ}
          onPTZStop={stopPTZ}
          onRemove={removeDevice}
          onClosePreview={() => setPreview(null)}
        />
      ) : null}

      {activeTab === 'ai' ? (
        <VisionTab
          saved={saved}
          rules={visionRules}
          alerts={visionAlerts}
          ruleDraft={visionRuleDraft}
          busy={busy}
          authHeader={authHeader}
          streamConfig={streamConfig}
          onRuleDraft={setVisionRuleDraft}
          onSaveRule={saveVisionRule}
          onEditRule={editVisionRule}
          onDeleteRule={deleteVisionRule}
          onTriggerTestAlert={triggerTestAlert}
          onAcknowledgeAlert={acknowledgeAlert}
          onPrepareCamera={prepareVisionLiveView}
          onReload={() => loadVision()}
        />
      ) : null}

      {activeTab === 'settings' ? (
        <SettingsTab
          settingsNav={settingsNav}
          settings={runtimeSettings}
          users={users}
          newUser={newUser}
          passwordDrafts={passwordDrafts}
          busy={busy}
          onChange={setRuntimeSettings}
          onSettingsNav={openSettingsSection}
          onSave={saveRuntimeSettings}
          onReset={resetRuntimeSettings}
          onLoadUsers={() => loadUsers()}
          onNewUser={setNewUser}
          onCreateUser={createUser}
          onEditUser={editUser}
          onUpdateUser={updateUser}
          onPasswordDraft={(id, value) => setPasswordDrafts((current) => ({ ...current, [id]: value }))}
          onResetPassword={resetUserPassword}
          onDeleteUser={deleteUser}
        />
      ) : null}
    </main>
  );
}

const styles = `
* {
  box-sizing: border-box;
}

body {
  margin: 0;
  font-family: Inter, Segoe UI, Arial, sans-serif;
  color: #18212f;
  background: #f4f6f8;
}

button,
input,
select,
textarea {
  font: inherit;
}

button {
  min-height: 38px;
  border: 1px solid #2d6cdf;
  border-radius: 6px;
  background: #2d6cdf;
  color: #ffffff;
  padding: 0 14px;
  cursor: pointer;
  white-space: nowrap;
}

button.quiet {
  border-color: #c7d1dc;
  background: #ffffff;
  color: #233044;
}

button.active {
  border-color: #2d6cdf;
  background: #2d6cdf;
  color: #ffffff;
}

button.danger-text {
  color: #9f3434;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

input,
select,
textarea {
  width: 100%;
  min-height: 38px;
  border: 1px solid #c7d1dc;
  border-radius: 6px;
  background: #ffffff;
  color: #18212f;
  padding: 8px 10px;
}

select {
  appearance: auto;
}

textarea {
  min-height: 76px;
  resize: vertical;
}

label {
  display: grid;
  gap: 6px;
  min-width: 0;
  color: #59687a;
  font-size: 13px;
  font-weight: 650;
}

.login-screen {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}

.login-panel {
  width: min(100%, 380px);
  display: grid;
  gap: 16px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 24px;
  box-shadow: 0 12px 30px rgba(32, 42, 54, 0.08);
}

.login-panel h1,
.topbar h1 {
  margin: 0;
  font-size: 28px;
  font-weight: 760;
}

.login-panel p,
.topbar p {
  margin: 6px 0 0;
  color: #6a7888;
}

.app-shell {
  width: min(1360px, calc(100vw - 32px));
  margin: 0 auto;
  padding: 18px 0 32px;
}

.topbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto auto;
  align-items: center;
  gap: 18px;
  min-height: 68px;
  border-bottom: 1px solid #dce3ea;
}

.primary-tabs,
.secondary-tabs,
.segmented,
.topbar-actions,
.action-row,
.add-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.topbar-actions {
  justify-content: end;
}

.status-line {
  border-left: 4px solid #d28d1f;
  background: #fff8e9;
  color: #64450d;
  padding: 10px 12px;
  margin: 16px 0 0;
}

.workspace {
  display: grid;
  gap: 16px;
  padding-top: 18px;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}

.section-title {
  margin: 0;
  font-size: 22px;
}

.section-subtitle {
  margin: 4px 0 0;
  color: #6a7888;
  font-size: 13px;
}

.add-strip {
  justify-content: end;
}

.add-strip span {
  color: #6a7888;
  font-size: 13px;
}

.view-grid {
  display: grid;
  gap: 10px;
}

.view-grid.layout-two {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.view-grid.layout-four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.view-tile {
  position: relative;
  min-width: 0;
  aspect-ratio: 16 / 9;
  border: 1px solid #cfd8e2;
  border-radius: 8px;
  background: #111923;
  overflow: hidden;
}

.view-tile[draggable='true'] {
  cursor: move;
}

.view-tile.dragging {
  opacity: 0.58;
  outline: 2px solid #2d6cdf;
}

.view-tile.has-ai-alert {
  border-color: #d23f3f;
  box-shadow: 0 0 0 2px rgba(210, 63, 63, 0.28), 0 0 18px rgba(210, 63, 63, 0.25);
}

.view-tile.has-ai-alert::after {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 1;
  border: 2px solid rgba(210, 63, 63, 0.78);
  border-radius: 8px;
  pointer-events: none;
  animation: ai-alert-pulse 1.4s ease-in-out infinite;
}

.live-frame {
  position: relative;
  width: 100%;
  height: 100%;
  background: #111923;
}

.view-tile img,
.view-tile video,
.preview-panel img,
.preview-panel video,
.zone-live img,
.zone-live video {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: contain;
  background: #111923;
}

.stream-state {
  position: absolute;
  left: 8px;
  bottom: 8px;
  max-width: calc(100% - 16px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  border-radius: 999px;
  background: rgba(15, 23, 33, 0.72);
  color: #ffffff;
  padding: 4px 8px;
  font-size: 12px;
  font-weight: 700;
}

.ptz-pad {
  position: absolute;
  right: 8px;
  bottom: 8px;
  z-index: 2;
  display: grid;
  grid-template-columns: repeat(3, 48px);
  gap: 4px;
  padding: 6px;
  border-radius: 8px;
  background: rgba(15, 23, 33, 0.7);
}

.ptz-pad button {
  min-height: 28px;
  padding: 0 6px;
  border-color: rgba(255, 255, 255, 0.28);
  font-size: 11px;
}

.ptz-pad button:first-child,
.ptz-pad button:last-child {
  grid-column: 2;
}

.tile-header {
  position: absolute;
  z-index: 4;
  top: 0;
  left: 0;
  right: 0;
  min-height: 34px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  background: rgba(15, 23, 33, 0.78);
  color: #ffffff;
  padding: 6px 8px;
}

.drag-handle {
  flex: 0 0 auto;
  color: #b9c7d6;
  font-weight: 800;
  letter-spacing: 0;
  line-height: 1;
}

.tile-header strong {
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.tile-alert-pill {
  flex: 0 0 auto;
  min-height: 26px;
  border-color: #d23f3f;
  background: #d23f3f;
  color: #ffffff;
  padding: 0 8px;
  font-size: 12px;
  font-weight: 800;
}

.tile-ai-banner {
  position: absolute;
  z-index: 3;
  left: 8px;
  top: 44px;
  max-width: calc(100% - 16px);
  display: grid;
  gap: 2px;
  justify-items: start;
  min-height: 0;
  border-color: rgba(210, 63, 63, 0.92);
  background: rgba(159, 52, 52, 0.9);
  color: #ffffff;
  padding: 8px 10px;
  text-align: left;
  box-shadow: 0 8px 18px rgba(0, 0, 0, 0.22);
}

.tile-ai-banner strong,
.tile-ai-banner span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tile-ai-banner strong {
  font-size: 13px;
}

.tile-ai-banner span {
  font-size: 11px;
  opacity: 0.9;
}

.icon-button {
  width: 26px;
  min-height: 26px;
  border-radius: 6px;
  border-color: rgba(255, 255, 255, 0.28);
  background: rgba(255, 255, 255, 0.12);
  padding: 0;
}

.empty-tile {
  height: 100%;
  display: grid;
  place-items: center;
  color: #9aa8b6;
  font-weight: 700;
}

@keyframes ai-alert-pulse {
  0%,
  100% {
    opacity: 0.35;
  }

  50% {
    opacity: 1;
  }
}

.camera-grid {
  display: grid;
  grid-template-columns: minmax(280px, 420px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.saved-browser {
  display: grid;
  grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.saved-sidebar {
  display: grid;
  gap: 12px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 12px;
}

.saved-sidebar header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.saved-sidebar h2 {
  margin: 0;
  font-size: 17px;
}

.saved-sidebar header span {
  min-width: 30px;
  text-align: center;
  border-radius: 999px;
  background: #e4ebf8;
  color: #2d6cdf;
  padding: 4px 8px;
  font-weight: 720;
}

.saved-device-nav {
  display: grid;
  gap: 6px;
}

.saved-device-button {
  width: 100%;
  min-height: 52px;
  display: grid;
  gap: 3px;
  justify-items: start;
  border-color: transparent;
  background: #ffffff;
  color: #233044;
  padding: 8px 10px;
  text-align: left;
}

.saved-device-button:hover,
.saved-device-button.active {
  border-color: #2d6cdf;
  background: #edf4ff;
}

.saved-device-button strong,
.saved-device-button span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.saved-device-button span {
  color: #6a7888;
  font-size: 12px;
}

.saved-detail {
  min-width: 0;
  display: grid;
  gap: 12px;
}

.empty-detail h2 {
  margin: 0;
  font-size: 18px;
}

.saved-detail-tabs {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  border-bottom: 1px solid #e5ebf1;
  padding-bottom: 10px;
}

.saved-detail-tabs button {
  min-height: 34px;
}

.saved-tab-panel {
  display: grid;
  gap: 12px;
}

.probe-panel {
  display: grid;
  gap: 14px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 14px;
}

.scan-row,
.probe-row,
.credential-row,
.metadata-row {
  display: grid;
  gap: 12px;
  align-items: end;
}

.scan-row,
.probe-row {
  grid-template-columns: minmax(0, 1fr) auto;
}

.credential-row,
.metadata-row {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.preview-panel {
  display: grid;
  gap: 10px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 14px;
}

.preview-panel header,
.device-section > header,
.device-title-row {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 12px;
}

.preview-panel h2,
.device-section h2 {
  margin: 0;
  font-size: 18px;
}

.preview-panel p {
  margin: 4px 0 0;
  color: #6a7888;
  font-size: 13px;
}

.preview-panel .live-frame {
  width: min(100%, 860px);
  aspect-ratio: 16 / 9;
  border: 1px solid #cfd8e2;
  border-radius: 8px;
}

.preview-actions {
  display: grid;
  gap: 10px;
}

.preview-ptz-controls {
  width: min(100%, 360px);
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}

.preview-ptz-controls button:first-child,
.preview-ptz-controls button:last-child {
  grid-column: 2;
}

.device-section > header {
  min-height: 38px;
  align-items: center;
}

.device-section header span {
  min-width: 30px;
  text-align: center;
  border-radius: 999px;
  background: #e4ebf8;
  color: #2d6cdf;
  padding: 4px 8px;
  font-weight: 720;
}

.device-list {
  display: grid;
  gap: 12px;
}

.device-list.compact {
  gap: 10px;
}

.discovery-groups {
  display: grid;
  gap: 14px;
}

.discovery-group {
  display: grid;
  gap: 10px;
}

.discovery-group header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.discovery-group h3 {
  margin: 0;
  font-size: 15px;
}

.discovery-group-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.discovery-group-count {
  min-width: 28px;
  text-align: center;
  border-radius: 999px;
  background: #eef1f4;
  color: #59687a;
  padding: 3px 8px;
  font-weight: 720;
}

.compact-button {
  min-height: 30px;
  padding: 0 10px;
  font-size: 13px;
}

.empty {
  margin: 0;
  color: #6a7888;
}

.device-card {
  display: grid;
  gap: 12px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 14px;
}

.device-card h3 {
  margin: 0;
  font-size: 16px;
}

.device-card p {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  color: #6a7888;
  font-size: 13px;
}

.device-card .device-description {
  margin: 0;
  color: #3e4a59;
}

.device-edit-form {
  display: grid;
  gap: 10px;
}

.field-hint {
  color: #7a8796;
  font-size: 12px;
  font-weight: 600;
}

.field-hint.good {
  color: #247455;
}

.capability-panel {
  display: grid;
  gap: 10px;
}

.capability-panel header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.capability-panel h4 {
  margin: 0;
  font-size: 15px;
}

.meta-grid,
.capability-grid,
.stream-meta {
  display: grid;
  gap: 10px;
  margin: 0;
}

.meta-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.capability-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.stream-meta {
  grid-template-columns: 1fr;
}

.stream-action-flow {
  display: grid;
  grid-template-columns: repeat(4, minmax(120px, 1fr));
  gap: 8px;
}

dt {
  color: #6a7888;
  font-size: 12px;
  font-weight: 720;
}

dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
}

.status-pill {
  flex: 0 0 auto;
  border-radius: 999px;
  background: #eef1f4;
  color: #59687a;
  padding: 5px 9px;
  font-size: 12px;
  line-height: 1;
}

.status-pill.online {
  background: #dff2ea;
  color: #247455;
}

.status-pill.offline {
  background: #fde7e7;
  color: #9f3434;
}

.status-pill.resolved {
  background: #e4ebf8;
  color: #2d6cdf;
}

.status-pill.saved {
  background: #dff2ea;
  color: #247455;
}

.track-list {
  display: grid;
  gap: 4px;
  margin: 0;
  padding-left: 18px;
}

.settings-layout {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.settings-workspace {
  grid-template-columns: 180px minmax(0, 1fr);
  align-items: start;
}

.settings-side-nav {
  display: grid;
  gap: 8px;
}

.settings-side-nav button {
  width: 100%;
  text-align: left;
}

.settings-content {
  min-width: 0;
}

.settings-panel {
  display: grid;
  gap: 14px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 18px;
}

.settings-panel.span-two,
.settings-actions {
  grid-column: 1 / -1;
}

.settings-panel header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.settings-panel h2 {
  margin: 0;
  font-size: 18px;
}

.vision-layout {
  display: grid;
  grid-template-columns: minmax(280px, 420px) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

.vision-browser {
  align-items: start;
}

.vision-rule-form {
  display: grid;
  gap: 14px;
  border-top: 1px solid #e5ebf1;
  padding-top: 14px;
}

.vision-rule-form header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.vision-rule-form h2 {
  margin: 0;
  font-size: 18px;
}

.schedule-panel {
  display: grid;
  gap: 12px;
  border: 1px solid #e5ebf1;
  border-radius: 8px;
  background: #fbfcfd;
  padding: 12px;
}

.schedule-panel header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.schedule-panel h3 {
  margin: 0;
  font-size: 15px;
}

.schedule-edit-block {
  display: grid;
  gap: 8px;
}

.schedule-edit-block > strong {
  color: #59687a;
  font-size: 13px;
  font-weight: 720;
}

.schedule-days {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 8px;
}

.schedule-days .check-row {
  justify-content: center;
  min-height: 38px;
  border: 1px solid #d6dee7;
  border-radius: 6px;
  background: #ffffff;
  padding: 0 8px;
}

.vision-list {
  display: grid;
  gap: 10px;
}

.vision-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 12px;
  align-items: center;
  border-top: 1px solid #e5ebf1;
  padding-top: 10px;
}

.vision-row:first-child {
  border-top: 0;
  padding-top: 0;
}

.vision-row h3 {
  margin: 0;
  font-size: 16px;
}

.vision-row p {
  margin: 4px 0 0;
  color: #6a7888;
  font-size: 13px;
}

.alert-row {
  grid-template-columns: minmax(0, 1fr) auto auto;
}

.event-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
}

.event-grid > div {
  min-width: 0;
  border: 1px solid #edf1f5;
  border-radius: 6px;
  background: #fbfcfd;
  padding: 8px 10px;
}

.event-table-wrap {
  overflow-x: auto;
  border: 1px solid #e5ebf1;
  border-radius: 8px;
}

.event-table {
  width: 100%;
  min-width: 760px;
  border-collapse: collapse;
  background: #ffffff;
}

.event-table th,
.event-table td {
  border-bottom: 1px solid #edf1f5;
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}

.event-table th {
  color: #59687a;
  background: #f7f9fb;
  font-size: 12px;
  font-weight: 760;
}

.event-table tbody tr:last-child td {
  border-bottom: 0;
}

.event-table tbody tr.selected td {
  background: #edf4ff;
}

.event-table td strong,
.event-table td span {
  display: block;
}

.event-table td strong {
  font-size: 14px;
}

.event-table td span:not(.status-pill) {
  margin-top: 3px;
  color: #6a7888;
  font-size: 12px;
}

.table-actions {
  display: flex;
  gap: 6px;
  flex-wrap: nowrap;
}

.table-actions button {
  min-height: 32px;
  padding: 0 10px;
}

.event-detail-panel {
  display: grid;
  gap: 12px;
  border: 1px solid #d6dee7;
  border-radius: 8px;
  background: #ffffff;
  padding: 14px;
}

.event-detail-panel header {
  display: flex;
  justify-content: space-between;
  align-items: start;
  gap: 12px;
}

.event-detail-panel h3 {
  margin: 0;
  font-size: 17px;
}

.event-detail-panel p {
  margin: 4px 0 0;
  color: #6a7888;
  font-size: 13px;
}

.event-details {
  border: 1px solid #edf1f5;
  border-radius: 6px;
  background: #fbfcfd;
  padding: 8px 10px;
}

.event-details strong {
  display: block;
  color: #59687a;
  font-size: 13px;
  font-weight: 720;
}

.event-details pre {
  max-height: 180px;
  overflow: auto;
  margin: 8px 0 0;
  font: 12px/1.45 Consolas, Menlo, monospace;
  white-space: pre-wrap;
}

.zone-drawer {
  display: grid;
  gap: 10px;
}

.zone-drawer header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
}

.zone-drawer h3 {
  margin: 0;
  font-size: 15px;
}

.zone-live {
  position: relative;
  aspect-ratio: 16 / 9;
  min-height: 220px;
  border: 1px solid #cfd8e2;
  border-radius: 8px;
  background: #111923;
  overflow: hidden;
}

.zone-live .live-frame {
  width: 100%;
  height: 100%;
}

.zone-overlay {
  position: absolute;
  inset: 0;
  z-index: 3;
  cursor: crosshair;
  touch-action: none;
}

.zone-overlay svg {
  width: 100%;
  height: 100%;
  display: block;
}

.zone-shape {
  fill: rgba(45, 108, 223, 0.2);
  stroke: #ffffff;
  stroke-width: 0.7;
}

.zone-line {
  fill: none;
  stroke: #ffffff;
  stroke-width: 0.7;
  stroke-dasharray: 2 1.5;
}

.zone-point {
  fill: #2d6cdf;
  stroke: #ffffff;
  stroke-width: 0.8;
  cursor: grab;
}

.zone-point:active {
  cursor: grabbing;
}

.empty-zone {
  display: grid;
  place-items: center;
}

.zone-empty-state {
  color: #c3ceda;
  font-weight: 750;
}

.check-row {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #18212f;
  font-size: 14px;
}

.check-row input {
  width: 18px;
  min-height: 18px;
  padding: 0;
}

.ice-list {
  display: grid;
  gap: 12px;
}

.ice-row {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(160px, 220px) minmax(160px, 220px) auto;
  gap: 10px;
  align-items: end;
  border-top: 1px solid #e5ebf1;
  padding-top: 12px;
}

.user-create-row,
.user-row {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) minmax(160px, 1fr) minmax(160px, 1fr) auto auto;
  gap: 10px;
  align-items: end;
}

.user-list {
  display: grid;
  gap: 12px;
}

.user-row {
  grid-template-columns: minmax(130px, 1fr) minmax(150px, 1fr) auto auto minmax(150px, 1fr) auto;
  border-top: 1px solid #e5ebf1;
  padding-top: 12px;
}

.user-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.settings-actions {
  display: flex;
  gap: 8px;
  justify-content: end;
  flex-wrap: wrap;
}

@media (max-width: 980px) {
  .topbar,
  .camera-grid,
  .saved-browser,
  .vision-layout,
  .settings-workspace,
  .settings-layout,
  .ice-row,
  .user-create-row,
  .user-row,
  .vision-row,
  .event-grid,
  .scan-row,
  .probe-row,
  .credential-row,
  .metadata-row,
  .stream-action-flow {
    grid-template-columns: 1fr;
  }

  .schedule-days {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .topbar-actions,
  .add-strip {
    justify-content: start;
  }

  .view-grid.layout-four {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 620px) {
  .app-shell {
    width: min(100vw - 20px, 560px);
  }

  .view-grid.layout-two,
  .view-grid.layout-four,
  .meta-grid,
  .capability-grid {
    grid-template-columns: 1fr;
  }
}
`;
