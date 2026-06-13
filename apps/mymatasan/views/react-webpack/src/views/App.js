import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import config from 'config';

const icoSvg = {
  monitor:     '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
  camera:      '<path d="M23 19a2 2 0 0 1-2 2H3a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4l2-3h6l2 3h4a2 2 0 0 1 2 2z"/><circle cx="12" cy="13" r="4"/>',
  cpu:         '<rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/>',
  film:        '<rect x="2" y="2" width="20" height="20" rx="2"/><path d="M7 2v20M17 2v20M2 12h20M2 7h5M2 17h5M17 17h5M17 7h5"/>',
  sliders:     '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>',
  refresh:     '<polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/>',
  lock:        '<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
  login:       '<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" y1="12" x2="3" y2="12"/>',
  plus:        '<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>',
  x:           '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
  'arr-up':    '<line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/>',
  'arr-down':  '<line x1="12" y1="5" x2="12" y2="19"/><polyline points="19 12 12 19 5 12"/>',
  'arr-left':  '<line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/>',
  'arr-right': '<line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/>',
  stop:        '<rect x="3" y="3" width="18" height="18" rx="2"/>',
  search:      '<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>',
  save:        '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>',
  trash:       '<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>',
  eye:         '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>',
  play:        '<polygon points="5 3 19 12 5 21 5 3"/>',
  download:    '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
  'check-ok':  '<path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>',
  wand:        '<path d="m21.64 3.64-1.28-1.28a1.21 1.21 0 0 0-1.72 0L2.36 18.64a1.21 1.21 0 0 0 0 1.72l1.28 1.28a1.2 1.2 0 0 0 1.72 0L21.64 5.36a1.2 1.2 0 0 0 0-1.72"/><path d="m14 7 3 3"/><path d="M5 6v4"/><path d="M19 14v4"/><path d="M10 2v2"/><path d="M7 8H3"/><path d="M21 16h-4"/><path d="M11 3H9"/>',
  'volume-2':  '<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M15.54 8.46a5 5 0 0 1 0 7.07"/>',
  'volume-x':  '<polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/>',
  key:         '<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/>',
  wifi:        '<path d="M5 12.55a11 11 0 0 1 14.08 0"/><path d="M1.42 9a16 16 0 0 1 21.16 0"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><line x1="12" y1="20" x2="12.01" y2="20"/>',
  sun:         '<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>',
  moon:        '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>',
  palette:     '<circle cx="13.5" cy="6.5" r=".5"/><circle cx="17.5" cy="10.5" r=".5"/><circle cx="8.5" cy="7.5" r=".5"/><circle cx="6.5" cy="12.5" r=".5"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"/>',
  undo:        '<polyline points="9 14 4 9 9 4"/><path d="M20 20v-7a4 4 0 0 0-4-4H4"/>',
  grid2:       '<rect x="3" y="3" width="8" height="8"/><rect x="13" y="3" width="8" height="8"/><rect x="3" y="13" width="8" height="8"/><rect x="13" y="13" width="8" height="8"/>',
  grid4:       '<rect x="3" y="3" width="4" height="4"/><rect x="10" y="3" width="4" height="4"/><rect x="17" y="3" width="4" height="4"/><rect x="3" y="10" width="4" height="4"/><rect x="10" y="10" width="4" height="4"/><rect x="17" y="10" width="4" height="4"/><rect x="3" y="17" width="4" height="4"/><rect x="10" y="17" width="4" height="4"/><rect x="17" y="17" width="4" height="4"/>',
  folder:      '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>',
  list:        '<line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/>',
  reload:      '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>',
  'chev-up':   '<polyline points="18 15 12 9 6 15"/>',
  'chev-down': '<polyline points="6 9 12 15 18 9"/>',
  user:        '<path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
  'user-plus': '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><line x1="19" y1="8" x2="19" y2="14"/><line x1="22" y1="11" x2="16" y2="11"/>',
  shield:      '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  'edit-2':    '<path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z"/>',
  video:       '<polygon points="23 7 16 12 23 17 23 7"/><rect x="1" y="5" width="15" height="14" rx="2"/>',
  acknowledge: '<path d="M20 6 9 17l-5-5"/>',
};

function Ico({ n, sz = 14, style: extraStyle }) {
  return (
    <svg
      width={sz} height={sz}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      style={{ verticalAlign: 'middle', flexShrink: 0, display: 'inline-block', ...extraStyle }}
      dangerouslySetInnerHTML={{ __html: icoSvg[n] || '' }}
    />
  );
}

const THEMES = ['light', 'dark', 'slate'];
const THEME_LABELS = { light: 'Light', dark: 'Dark', slate: 'Slate' };
const THEME_ICONS  = { light: 'sun',   dark: 'moon', slate: 'palette' };

function ThemeDropdown({ theme, onThemeChange }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  return (
    <div className="theme-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet theme-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={THEME_ICONS[theme]} sz={13} />
          Theme
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="theme-menu" role="listbox" aria-label="Select theme">
          {THEMES.map((t) => (
            <button
              key={t}
              type="button"
              role="option"
              aria-selected={t === theme}
              className={`theme-menu-item${t === theme ? ' active' : ''}`}
              onClick={() => { onThemeChange(t); setOpen(false); }}
            >
              <Ico n={THEME_ICONS[t]} sz={14} /> {THEME_LABELS[t]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function FormBusyOverlay({ busy }) {
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label="Loading">
      <div className="form-busy-spinner" />
    </div>
  );
}

const emptyLogin = { username: '', password: '' };
const defaultDeviceCredentials = { username: '', password: '' };
const defaultStreamConfig = {
  webrtc: { enabled: true, iceServers: [] },
  mjpegFallback: { enabled: true },
};
const defaultDecoderConfig = {
  mjpeg: { ffmpegPath: '', quality: 7, threads: 1 },
  ffmpeg: {
    rtspTransport: 'tcp',
    hwaccel: 'none',
    hwaccelDevice: '',
    initHwDevice: '',
    videoDecoder: '',
    probeSize: 1000000,
    analyzeDuration: 1000000,
    lowDelay: true,
    noBuffer: true,
  },
};
const defaultYoloConfig = {
  conf: 0,
  iou: 0,
  augment: false,
  imgsz: 0,
  half: false,
  maxDet: 0,
};
// Best-practice starting point: good accuracy/speed balance; augment on for hard-to-detect poses
const bestYoloDefaults = {
  conf: 0.20,
  iou: 0.35,
  augment: true,
  imgsz: 640,
  half: false,
  maxDet: 100,
};
const defaultRuntimeSettings = {
  decoder: defaultDecoderConfig,
  stream: defaultStreamConfig,
  vision: { yolo: defaultYoloConfig },
};
const defaultNewUser = { username: '', displayName: '', password: '', isAdmin: false, isActive: true };
const defaultZonePoints = [
  [0.15, 0.15],
  [0.85, 0.15],
  [0.85, 0.85],
  [0.15, 0.85],
];
const defaultVisionThreshold = 0.35;
const defaultVisionMinFrames = 2;
const lineDetectionTypes = ['line_crossing', 'multi_line_crossing'];
const lineClassOptions = ['person', 'vehicle', 'car', 'truck', 'bus', 'motorcycle', 'bicycle', 'animal', 'bird', 'cat', 'dog', 'horse', 'sheep', 'cow', 'mouse', 'rat'];
const defaultLineClasses = ['person'];
const maxCrossingLines = 5;
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

function parseBoundingBox(value) {
  if (!value || typeof value !== 'string') return null;
  try {
    const b = JSON.parse(value);
    if (b && typeof b.x === 'number') return b;
  } catch (_) {}
  return null;
}

const DETECTION_SOURCE_LABELS = {
  'motion-detector': 'Motion',
  'motion-line-crossing-detector': 'Motion Line Crossing',
  'motion-multi-line-crossing-detector': 'Motion Multi-Line',
  'object-detector': 'YOLO Object',
  'persistent-object-detector': 'YOLO Persistent',
  'object-line-crossing-detector': 'YOLO Line Crossing',
  'vision-monitor': 'System',
};

function formatSourceLabel(source) {
  return DETECTION_SOURCE_LABELS[source] || source || '-';
}

function useSnapshotBlob(alertId, authHeader) {
  const [url, setUrl] = React.useState(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState(false);
  React.useEffect(() => {
    if (!alertId) { setUrl(null); setError(false); return; }
    let revoked = false;
    let objectUrl = null;
    setLoading(true);
    setError(false);
    setUrl(null);
    const headers = authHeader ? { Authorization: authHeader } : {};
    fetch(`${apiBase()}/api/vision/alerts/${alertId}/snapshot`, { credentials: 'include', headers })
      .then((r) => { if (!r.ok) throw new Error(r.status); return r.blob(); })
      .then((blob) => {
        if (revoked) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => { if (!revoked) setError(true); })
      .finally(() => { if (!revoked) setLoading(false); });
    return () => {
      revoked = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [alertId, authHeader]);
  return { url, loading, error };
}

function cameraTitle(device) {
  return device?.name || device?.model || device?.host || 'Camera';
}

function normalizeScanDevice(d) {
  return {
    host: d.ip || '',
    port: d.httpPort || 80,
    name: d.hostname || d.model || d.manufacturer || d.ip || '',
    manufacturer: d.manufacturer || '',
    model: d.model || '',
    serialNumber: d.serial || '',
    firmwareVersion: d.firmwareVersion || '',
    rtspUrl: d.rtspUrl || '',
    _discoveryMethods: d.methods || [],
    _openPorts: d.openPorts || [],
  };
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

function liveSource(id, options = {}) {
  const params = new URLSearchParams({
    fps: String(options.fps || 5),
    width: String(options.width || 480),
    t: String(Date.now()),
  });
  if (options.preferSnapshot) {
    params.set('preferSnapshot', '1');
  }
  return `${apiBase()}/api/cameras/${id}/live.mjpeg?${params.toString()}`;
}

function fallbackLiveSource(id) {
  return liveSource(id, { preferSnapshot: true });
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

function numberOrDefault(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

function normalizeRuntimeSettings(value) {
  const yolo = value?.vision?.yolo || {};
  return {
    decoder: {
      mjpeg: {
        ffmpegPath: value?.decoder?.mjpeg?.ffmpegPath || '',
        quality: numberOrDefault(value?.decoder?.mjpeg?.quality, defaultDecoderConfig.mjpeg.quality),
        threads: numberOrDefault(value?.decoder?.mjpeg?.threads, defaultDecoderConfig.mjpeg.threads),
      },
      ffmpeg: {
        rtspTransport: value?.decoder?.ffmpeg?.rtspTransport || defaultDecoderConfig.ffmpeg.rtspTransport,
        hwaccel: value?.decoder?.ffmpeg?.hwaccel || defaultDecoderConfig.ffmpeg.hwaccel,
        hwaccelDevice: value?.decoder?.ffmpeg?.hwaccelDevice || '',
        initHwDevice: value?.decoder?.ffmpeg?.initHwDevice || '',
        videoDecoder: value?.decoder?.ffmpeg?.videoDecoder || '',
        probeSize: numberOrDefault(value?.decoder?.ffmpeg?.probeSize, defaultDecoderConfig.ffmpeg.probeSize),
        analyzeDuration: numberOrDefault(value?.decoder?.ffmpeg?.analyzeDuration, defaultDecoderConfig.ffmpeg.analyzeDuration),
        lowDelay: value?.decoder?.ffmpeg?.lowDelay !== false,
        noBuffer: value?.decoder?.ffmpeg?.noBuffer !== false,
      },
    },
    stream: normalizeStreamConfig(value?.stream),
    vision: {
      yolo: {
        conf: typeof yolo.conf === 'number' ? yolo.conf : defaultYoloConfig.conf,
        iou: typeof yolo.iou === 'number' ? yolo.iou : defaultYoloConfig.iou,
        augment: yolo.augment === true,
        imgsz: typeof yolo.imgsz === 'number' ? yolo.imgsz : defaultYoloConfig.imgsz,
        half: yolo.half === true,
        maxDet: typeof yolo.maxDet === 'number' ? yolo.maxDet : defaultYoloConfig.maxDet,
      },
    },
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

const decoderTransportOptions = ['tcp', 'udp', 'udp_multicast', 'http', 'https'];
const decoderHWAccelOptions = ['none', 'auto', 'd3d11va', 'dxva2', 'vaapi', 'cuda', 'qsv', 'videotoolbox', 'vdpau', 'vulkan'];

function InfoButton({ text }) {
  return (
    <button type="button" className="info-button" title={text} aria-label={text}>
      i
    </button>
  );
}

function FieldTitle({ children, info }) {
  return (
    <span className="label-row">
      <span>{children}</span>
      <InfoButton text={info} />
    </span>
  );
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

function isLineDetectionType(value) {
  return lineDetectionTypes.includes(value);
}

function defaultLineRuleConfig(type = 'line_crossing') {
  const lines =
    type === 'multi_line_crossing'
      ? [
          { id: 'line-1', points: [[0.35, 0.2], [0.35, 0.8]] },
          { id: 'line-2', points: [[0.65, 0.2], [0.65, 0.8]] },
        ]
      : [{ id: 'line-1', points: [[0.5, 0.2], [0.5, 0.8]] }];
  return {
    classes: defaultLineClasses,
    direction: 'both',
    maxSecondsBetweenLines: 20,
    lines,
  };
}

function normalizeLineConfig(config, type = 'line_crossing') {
  const fallback = defaultLineRuleConfig(type);
  const source = config && typeof config === 'object' ? config : {};
  const classes = Array.isArray(source.classes)
    ? source.classes.map((item) => String(item).trim().toLowerCase()).filter(Boolean)
    : fallback.classes;
  const rawLines = Array.isArray(source.lines) ? source.lines : fallback.lines;
  const lines = rawLines
    .slice(0, maxCrossingLines)
    .map((line, index) => {
      const points = Array.isArray(line?.points) ? line.points : [];
      const first = points[0] || fallback.lines[Math.min(index, fallback.lines.length - 1)]?.points?.[0] || [0.5, 0.2];
      const second = points[1] || fallback.lines[Math.min(index, fallback.lines.length - 1)]?.points?.[1] || [0.5, 0.8];
      return {
        id: String(line?.id || `line-${index + 1}`),
        points: [roundedPoint([Number(first[0]), Number(first[1])]), roundedPoint([Number(second[0]), Number(second[1])])],
      };
    })
    .filter((line) => line.points.length >= 2);
  const minimumLines = type === 'multi_line_crossing' ? 2 : 1;
  const ensuredLines = lines.length >= minimumLines ? lines : fallback.lines;
  return {
    classes: classes.length ? classes : fallback.classes,
    direction: ['both', 'forward', 'reverse'].includes(source.direction) ? source.direction : 'both',
    maxSecondsBetweenLines: Math.max(1, Number(source.maxSecondsBetweenLines || fallback.maxSecondsBetweenLines || 20)),
    lines: ensuredLines.slice(0, maxCrossingLines),
  };
}

function parseLineRuleConfig(value, type = 'line_crossing') {
  if (!value) {
    return defaultLineRuleConfig(type);
  }
  try {
    return normalizeLineConfig(JSON.parse(value), type);
  } catch (_) {
    return defaultLineRuleConfig(type);
  }
}

function lineRuleConfigText(config, type = 'line_crossing') {
  return JSON.stringify(normalizeLineConfig(config, type), null, 2);
}

function lineCountFromRule(rule) {
  if (!isLineDetectionType(rule?.detectionType)) {
    return '';
  }
  const config = parseLineRuleConfig(rule.ruleConfig, rule.detectionType);
  return `${config.lines.length} line${config.lines.length === 1 ? '' : 's'}`;
}

function defaultVisionRuleDraft(cameraId = '') {
  return {
    id: 0,
    cameraId: cameraId || '',
    name: '',
    detectionType: 'fire',
    zonePolygon: defaultZonePolygon,
    ruleConfig: '',
    schedulePolicy: '',
    threshold: defaultVisionThreshold,
    minFrames: defaultVisionMinFrames,
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
  const response = await fetch(`${apiBase()}/api/cameras/${deviceId}/webrtc/offer`, {
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

function parseTracks(value) {
  if (typeof value === 'string' && value.trim()) {
    try {
      const tracks = JSON.parse(value);
      return Array.isArray(tracks) ? tracks : [];
    } catch (_) {
      return [];
    }
  } else if (Array.isArray(value)) {
    return value;
  }
  return [];
}

function isVideoTrack(track) {
  const mediaType = String(track?.mediaType || '').toLowerCase();
  const codec = String(track?.codec || '').toLowerCase();
  return mediaType.includes('video') || ['h264', 'h265', 'hevc'].some((value) => codec.includes(value));
}

function hasH264VideoTrack(value) {
  return parseTracks(value).some((track) => isVideoTrack(track) && /h\.?264|avc/.test(String(track?.codec || '').toLowerCase()));
}

function shouldUseMJPEGForTracks(value) {
  const videoTracks = parseTracks(value).filter(isVideoTrack);
  return videoTracks.length > 0 && !videoTracks.some((track) => /h\.?264|avc/.test(String(track?.codec || '').toLowerCase()));
}

function Tracks({ value }) {
  const tracks = parseTracks(value);
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

function streamOptionLabel(option) {
  const name = option?.name || option?.profileToken || 'Stream';
  const details = [option?.encoding, option?.width && option?.height ? `${option.width}x${option.height}` : '']
    .filter(Boolean)
    .join(' ');
  const badges = [option?.preferred ? 'preferred' : '', option?.selected ? 'selected' : ''].filter(Boolean).join(', ');
  return `${name}${details ? ` - ${details}` : ''}${badges ? ` (${badges})` : ''}`;
}

function Message({ value }) {
  if (!value) {
    return null;
  }
  return <div className="status-line">{value}</div>;
}

function LiveViewport({ deviceId, title, authHeader, streamConfig, rtspTracks, streamKey, startDelayMs = 0 }) {
  const videoRef = useRef(null);
  const audioRef = useRef(null);
  const [state, setState] = useState('Connecting');
  const [fallbackSrc, setFallbackSrc] = useState('');
  const [hasAudio, setHasAudio] = useState(false);
  const [muted, setMuted] = useState(true);

  useEffect(() => {
    if (!deviceId) {
      return undefined;
    }
    const configValue = normalizeStreamConfig(streamConfig);
    const forceMJPEG = shouldUseMJPEGForTracks(rtspTracks);
    setFallbackSrc('');
    setHasAudio(false);
    setMuted(true);
    setState(forceMJPEG || !configValue.webrtc.enabled ? 'MJPEG' : 'Connecting');

    if (forceMJPEG) {
      if (configValue.mjpegFallback.enabled) {
        setState('MJPEG fallback');
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState('WebRTC needs H264');
      }
      return undefined;
    }

    if (!configValue.webrtc.enabled) {
      if (configValue.mjpegFallback.enabled) {
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState('Live view disabled');
      }
      return undefined;
    }
    if (typeof RTCPeerConnection === 'undefined') {
      if (configValue.mjpegFallback.enabled) {
        setState('MJPEG fallback');
        setFallbackSrc(fallbackLiveSource(deviceId));
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
        pc.addTransceiver('audio', { direction: 'recvonly' });
        pc.ontrack = (event) => {
          if (cancelled) return;
          if (event.track.kind === 'video' && videoRef.current) {
            // Use the browser-managed stream directly; avoids re-initialising the
            // GPU decode pipeline that a custom MediaStream can trigger on Windows.
            const stream = event.streams[0] || new MediaStream([event.track]);
            videoRef.current.srcObject = stream;
            videoRef.current.play().catch(() => {});
            setState('Live');
          } else if (event.track.kind === 'audio' && audioRef.current) {
            setHasAudio(true);
            // Route audio to a dedicated element so the video element stays muted
            // (required for autoPlay) while audio is user-controlled independently.
            audioRef.current.srcObject = new MediaStream([event.track]);
          }
        };
        pc.onconnectionstatechange = () => {
          if (cancelled) return;
          const cs = pc.connectionState;
          if (cs === 'disconnected') {
            // Transient — ICE may recover; keep the video element alive and
            // show a status hint instead of switching to MJPEG fallback.
            setState('Reconnecting…');
          } else if (cs === 'failed' || cs === 'closed') {
            if (configValue.mjpegFallback.enabled) {
              setState('MJPEG fallback');
              setFallbackSrc(fallbackLiveSource(deviceId));
            } else {
              setState(`WebRTC ${cs}`);
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
            setFallbackSrc(fallbackLiveSource(deviceId));
          } else {
            setState(err.message || 'WebRTC failed');
          }
          pc.close();
        }
      }
    }

    // Stagger GPU decode session creation across tiles so the hardware decoder
    // is not asked to initialise multiple sessions simultaneously, which can
    // trigger a Windows GPU TDR (monitor blackout) on some driver versions.
    let startTimer = null;
    if (startDelayMs > 0) {
      startTimer = setTimeout(connect, startDelayMs);
    } else {
      connect();
    }

    return () => {
      cancelled = true;
      if (startTimer !== null) clearTimeout(startTimer);
      if (videoRef.current?.srcObject) {
        videoRef.current.srcObject.getTracks().forEach((track) => track.stop());
        videoRef.current.srcObject = null;
      }
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current.srcObject = null;
      }
      pc.close();
    };
  }, [deviceId, authHeader, streamConfig, rtspTracks, streamKey, startDelayMs]);

  function toggleMute() {
    setMuted((prev) => {
      const next = !prev;
      if (audioRef.current) {
        if (next) {
          audioRef.current.pause();
        } else {
          audioRef.current.play().catch(() => {});
        }
      }
      return next;
    });
  }

  return (
    <div className="live-frame">
      {fallbackSrc ? (
        <img src={fallbackSrc} alt={`${title} live view`} />
      ) : (
        <video ref={videoRef} autoPlay muted playsInline aria-label={`${title} live view`} />
      )}
      <audio ref={audioRef} playsInline style={{ display: 'none' }} />
      <span className="stream-state">{state}</span>
      {hasAudio && (
        <button
          type="button"
          className="audio-mute-btn"
          onClick={toggleMute}
          aria-label={muted ? 'Unmute audio' : 'Mute audio'}
          title={muted ? 'Unmute audio' : 'Mute audio'}
        >
          <Ico n={muted ? 'volume-x' : 'volume-2'} sz={14} />
        </button>
      )}
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
            <LiveViewport
              key={`${camera.id}:${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
              deviceId={camera.id}
              title={cameraTitle(camera)}
              authHeader={authHeader}
              streamConfig={streamConfig}
              rtspTracks={camera.rtspTracks}
              streamKey={`${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
            />
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
          <span className="btn-icon"><Ico n="undo" /> Undo Point</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit([])} disabled={disabled}>
          <span className="btn-icon"><Ico n="trash" /> Clear Zone</span>
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
          <span className="btn-icon"><Ico n="video" /> Full Frame</span>
        </button>
      </div>
    </section>
  );
}

function LineDrawingPreview({ camera, config, detectionType, onConfig, authHeader, streamConfig, disabled }) {
  const overlayRef = useRef(null);
  const [dragging, setDragging] = useState(null);
  const maxLines = detectionType === 'multi_line_crossing' ? maxCrossingLines : 1;
  const lines = normalizeLineConfig(config, detectionType).lines.slice(0, maxLines);

  function commit(nextLines) {
    onConfig({ lines: nextLines.slice(0, maxLines) });
  }

  function pointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return roundedPoint([(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height]);
  }

  function addLine(point = null) {
    if (disabled || !camera || lines.length >= maxLines) {
      return;
    }
    const start = point || [0.5, 0.25 + lines.length * 0.12];
    const end = roundedPoint([start[0], start[1] + 0.25]);
    commit([...lines, { id: `line-${lines.length + 1}`, points: [roundedPoint(start), end] }]);
  }

  function movePoint(event) {
    if (disabled || !dragging) {
      return;
    }
    const nextLines = lines.map((line, lineIndex) => {
      if (lineIndex !== dragging.lineIndex) {
        return line;
      }
      const nextPoints = [...line.points];
      nextPoints[dragging.pointIndex] = pointFromEvent(event);
      return { ...line, points: nextPoints };
    });
    commit(nextLines);
  }

  function stopDrag(event) {
    if (dragging && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDragging(null);
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>{detectionType === 'multi_line_crossing' ? 'Crossing Sequence' : 'Crossing Line'}</h3>
        <span className="status-pill">{lines.length}/{maxLines} lines</span>
      </header>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <LiveViewport
              key={`${camera.id}:${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
              deviceId={camera.id}
              title={cameraTitle(camera)}
              authHeader={authHeader}
              streamConfig={streamConfig}
              rtspTracks={camera.rtspTracks}
              streamKey={`${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
            />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label="Draw crossing lines"
              onPointerDown={(event) => {
                if (event.button !== 0 || lines.length >= maxLines) {
                  return;
                }
                overlayRef.current?.setPointerCapture?.(event.pointerId);
                addLine(pointFromEvent(event));
              }}
              onPointerMove={movePoint}
              onPointerUp={stopDrag}
              onPointerCancel={stopDrag}
            >
              <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                {lines.map((line, lineIndex) => {
                  const [first, second] = line.points;
                  return (
                    <g key={line.id || lineIndex}>
                      <line
                        x1={first[0] * 100}
                        y1={first[1] * 100}
                        x2={second[0] * 100}
                        y2={second[1] * 100}
                        className="crossing-line"
                        vectorEffect="non-scaling-stroke"
                      />
                      <text x={(first[0] * 100 + second[0] * 100) / 2} y={(first[1] * 100 + second[1] * 100) / 2 - 2} className="crossing-label">
                        {lineIndex + 1}
                      </text>
                      {line.points.map((point, pointIndex) => (
                        <circle
                          key={`${lineIndex}-${pointIndex}`}
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
                            setDragging({ lineIndex, pointIndex });
                          }}
                        />
                      ))}
                    </g>
                  );
                })}
              </svg>
            </div>
          </>
        ) : (
          <div className="zone-empty-state">Select camera</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={() => addLine()} disabled={disabled || lines.length >= maxLines}>
          <span className="btn-icon"><Ico n="plus" /> Add Line</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit(lines.slice(0, -1))} disabled={disabled || !lines.length}>
          <span className="btn-icon"><Ico n="undo" /> Undo Line</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit([])} disabled={disabled}>
          <span className="btn-icon"><Ico n="trash" /> Clear Lines</span>
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
          <span className="btn-icon"><Ico n="login" /> Sign In</span>
        </button>
        <Message value={message} />
        <FormBusyOverlay busy={busy} />
      </form>
    </main>
  );
}

function TopBar({ activeTab, busy, onTab, onRefresh, onLogout, alerts, savedDevices, notifOpen, notifUnread, onNotifToggle, onNotifClick, theme, onThemeChange }) {
  const tabs = [
    { id: 'views',     label: 'Live Views', icon: 'monitor' },
    { id: 'cameras',   label: 'Cameras',    icon: 'camera'  },
    { id: 'ai',        label: 'AI',         icon: 'cpu'     },
    { id: 'recording', label: 'Recording',  icon: 'film'    },
    { id: 'settings',  label: 'Settings',   icon: 'sliders' },
  ];
  const notifAlerts = useMemo(
    () => (alerts || []).filter((a) => !a.isAcknowledged && !parseMetadata(a.metadata).diagnostic).slice(0, 20),
    [alerts],
  );
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
            <span className="btn-icon"><Ico n={tab.icon} /> {tab.label}</span>
          </button>
        ))}
      </nav>
      <div className="topbar-actions">
        <div className="notif-wrap">
          <button
            type="button"
            className={`quiet notif-btn${notifOpen ? ' active' : ''}`}
            onClick={onNotifToggle}
            aria-label={`Events${notifUnread > 0 ? `, ${notifUnread} unread` : ''}`}
          >
            <span className="btn-icon">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" style={{verticalAlign:'middle',flexShrink:0}}>
                <path d="M12 22c1.1 0 2-.9 2-2h-4c0 1.1.9 2 2 2zm6-6V11c0-3.07-1.63-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68C7.63 5.36 6 7.92 6 11v5l-2 2v1h16v-1l-2-2z"/>
              </svg>
              Events
              <span className={`notif-badge${notifUnread > 0 ? ' notif-badge--visible' : ''}`}>
                {notifUnread > 99 ? '99+' : notifUnread || ''}
              </span>
            </span>
          </button>
          {notifOpen && (
            <div className="notif-panel" role="dialog" aria-label="Recent events">
              <div className="notif-panel-header">Recent events</div>
              {notifAlerts.length === 0 ? (
                <p className="notif-empty">No recent events.</p>
              ) : (
                notifAlerts.map((alert) => {
                  const cam = (savedDevices || []).find((d) => Number(d.id) === Number(alert.cameraId));
                  return (
                    <button
                      key={alert.id}
                      type="button"
                      className="notif-item"
                      onClick={() => onNotifClick(alert.cameraId, alert.id)}
                    >
                      <span className="notif-label">{alert.label || alert.detectionType || 'Event'}</span>
                      <span className="notif-camera">{cam ? cameraTitle(cam) : `Camera ${alert.cameraId}`}</span>
                      <span className="notif-time">{formatTimestamp(alert.createdAt)}</span>
                    </button>
                  );
                })
              )}
            </div>
          )}
        </div>
        <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
        <button type="button" className="quiet" onClick={onRefresh} disabled={busy}>
          <span className="btn-icon"><Ico n="refresh" /> Refresh</span>
        </button>
        <button type="button" className="quiet danger-text" onClick={onLogout} disabled={busy}>
          <span className="btn-icon"><Ico n="lock" /> Lock</span>
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
            <span className="btn-icon"><Ico n="grid2" /> 2×2</span>
          </button>
          <button type="button" className={layout === '4x4' ? 'active' : 'quiet'} onClick={() => onLayout('4x4')}>
            <span className="btn-icon"><Ico n="grid4" /> 4×4</span>
          </button>
        </div>
        <div className="add-strip">
          {available.length === 0 ? <span>No saved cameras available</span> : null}
          {available.map((device) => (
            <button type="button" className="quiet" key={device.id} disabled={busy} onClick={() => onAdd(device)}>
              <span className="btn-icon"><Ico n="plus" /> {cameraTitle(device)}</span>
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
                    <Ico n="x" sz={12} />
                  </button>
                </div>
                <LiveViewport
                  key={`${tile.id}:${tile.rtspUrl || ''}:${tile.rtspTracks || ''}`}
                  deviceId={tile.id}
                  title={tile.title}
                  authHeader={authHeader}
                  streamConfig={streamConfig}
                  rtspTracks={tile.rtspTracks}
                  streamKey={`${tile.rtspUrl || ''}:${tile.rtspTracks || ''}`}
                  startDelayMs={idx * 700}
                />
                {latestAlert ? (
                  <button type="button" className="tile-ai-banner" onClick={() => onOpenAlerts(tile.id)}>
                    <strong>{latestAlert.label || latestAlert.detectionType || 'AI event'}</strong>
                    <span>{formatTimestamp(latestAlert.createdAt)}</span>
                  </button>
                ) : null}
                {tile.ptzSupported ? (
                  <div className="ptz-ring-overlay">
                    <PTZRing
                      busy={busy}
                      size={100}
                      onMove={(dir) => onPTZMove(tile.id, dir)}
                      onStop={() => onPTZStop(tile.id)}
                    />
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
            <span className="btn-icon"><Ico n="save" /> Save</span>
          </button>
        </div>
        <DeviceMeta device={device} />
        {device._discoveryMethods && device._discoveryMethods.length > 0 ? (
          <div className="discovery-method-badges">
            {device._discoveryMethods.map((m) => (
              <span key={m} className="discovery-method-badge">{m}</span>
            ))}
            {device._openPorts && device._openPorts.length > 0 ? (
              <span className="discovery-ports">ports: {device._openPorts.join(', ')}</span>
            ) : null}
          </div>
        ) : null}
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
  streamOptions,
  selectedStreamToken,
  onDetailDraft,
  onSaveDetails,
  onDiscardDetails,
  onCredential,
  onPasswordDraft,
  onSaveCredentials,
  onChangePassword,
  onResolve,
  onStreamToken,
  onSelectStream,
  onTest,
  onPreview,
  onAdd,
  onRemove,
}) {
  const [activePanel, setActivePanel] = useState('details');
  const localDetails = detailDraft || { name: device.name || '', description: device.description || '' };
  const localCred = credentials || { username: device.username || '', password: '' };
  const savedDetails = { name: device.name || '', description: device.description || '' };
  const detailsHaveChanges = localDetails.name !== savedDetails.name || localDetails.description !== savedDetails.description;
  const savedCred = { username: device.username || '', password: '' };
  const credHaveChanges = localCred.username !== savedCred.username || localCred.password !== '';
  const localPasswordDraft = passwordDraft || { targetUsername: device.username || '', newPassword: '' };
  const streamReady = Boolean(device.rtspUrl);
  const options = Array.isArray(streamOptions?.options) ? streamOptions.options : [];
  const selectedToken = selectedStreamToken || device.profileToken || streamOptions?.selectedProfileToken || options[0]?.profileToken || '';
  const selectedOption = options.find((option) => option.profileToken === selectedToken) || null;

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
            <FormBusyOverlay busy={busy} />
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
              <button type="submit" className="quiet" disabled={busy || !detailsHaveChanges}>
                <span className="btn-icon"><Ico n="save" /> Save Details</span>
              </button>
              <button type="button" className="quiet" onClick={() => onDiscardDetails(device.id)} disabled={busy || !detailsHaveChanges}>
                <span className="btn-icon"><Ico n="undo" /> Discard</span>
              </button>
              <button type="button" className="quiet danger-text" onClick={() => onRemove(device.id)} disabled={busy}>
                <span className="btn-icon"><Ico n="trash" /> Remove</span>
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {activePanel === 'access' ? (
        <section className="saved-tab-panel">
          <FormBusyOverlay busy={busy} />
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
            <button type="button" className="quiet" onClick={() => onSaveCredentials(device)} disabled={busy || !credHaveChanges}>
              <span className="btn-icon"><Ico n="shield" /> Save Credentials</span>
            </button>
            <button type="button" className="quiet" onClick={() => onCredential(device.id, savedCred)} disabled={busy || !credHaveChanges}>
              <span className="btn-icon"><Ico n="undo" /> Discard</span>
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
              <span className="btn-icon"><Ico n="key" /> Change Camera Password</span>
            </button>
          </div>
        </section>
      ) : null}

      {activePanel === 'stream' ? (
        <section className="saved-tab-panel">
          <dl className="stream-meta">
            <div>
              <dt>Profile</dt>
              <dd>{fieldValue(device.profileToken)}</dd>
            </div>
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
          {options.length > 0 ? (
            <div className="stream-option-panel">
              <label>
                ONVIF stream
                <select value={selectedToken} onChange={(event) => onStreamToken(device.id, event.target.value)}>
                  {options.map((option) => (
                    <option key={option.profileToken} value={option.profileToken}>
                      {streamOptionLabel(option)}
                    </option>
                  ))}
                </select>
              </label>
              <div className="stream-option-uri">{selectedOption ? selectedOption.rtspUrl : '-'}</div>
              <button type="button" className="quiet" onClick={() => onSelectStream(device, selectedOption)} disabled={busy || !selectedOption}>
                Use Selected Stream
              </button>
            </div>
          ) : null}
          <div className="stream-action-flow">
            <button type="button" onClick={() => onResolve(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="search" /> Find Streams</span>
            </button>
            <button type="button" className="quiet" onClick={() => onTest(device)} disabled={busy || !streamReady}>
              <span className="btn-icon"><Ico n="play" /> Test RTSP</span>
            </button>
            <button type="button" className="quiet" onClick={() => onPreview(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="eye" /> Live Preview</span>
            </button>
            <button type="button" className="quiet" onClick={() => onAdd(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="plus" /> Add to Live Views</span>
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

// Circular D-pad PTZ controller. Renders as an inline SVG; parent is responsible for positioning.
function PTZRing({ busy, size, onMove, onStop }) {
  const sz = size || 140;
  // All path geometry is authored for a 200×200 viewBox then scaled by SVG.
  const ro = 94;                    // outer ring radius
  const ri = 35;                    // inner (stop) circle radius
  const d  = ro / Math.SQRT2;      // ≈ 66.47 — outer ring diagonal intersection
  const di = ri / Math.SQRT2;      // ≈ 24.75 — inner ring diagonal intersection
  const cx = 100, cy = 100;

  // Annular sector paths: inner-arc CW (sweep=1) then outer-arc CCW (sweep=0)
  const UP    = `M ${cx-di} ${cy-di} A ${ri} ${ri} 0 0 1 ${cx+di} ${cy-di} L ${cx+d} ${cy-d} A ${ro} ${ro} 0 0 0 ${cx-d} ${cy-d} Z`;
  const RIGHT = `M ${cx+di} ${cy-di} A ${ri} ${ri} 0 0 1 ${cx+di} ${cy+di} L ${cx+d} ${cy+d} A ${ro} ${ro} 0 0 0 ${cx+d} ${cy-d} Z`;
  const DOWN  = `M ${cx+di} ${cy+di} A ${ri} ${ri} 0 0 1 ${cx-di} ${cy+di} L ${cx-d} ${cy+d} A ${ro} ${ro} 0 0 0 ${cx+d} ${cy+d} Z`;
  const LEFT  = `M ${cx-di} ${cy+di} A ${ri} ${ri} 0 0 1 ${cx-di} ${cy-di} L ${cx-d} ${cy-d} A ${ro} ${ro} 0 0 0 ${cx-d} ${cy+d} Z`;

  // Block arrow icons (filled), centered in each sector at r≈64.5
  const A_UP    = 'M 100 24 L 112 40 L 106 40 L 106 48 L 94 48 L 94 40 L 88 40 Z';
  const A_RIGHT = 'M 176 100 L 160 88 L 160 94 L 152 94 L 152 106 L 160 106 L 160 112 Z';
  const A_DOWN  = 'M 100 176 L 88 160 L 94 160 L 94 152 L 106 152 L 106 160 L 112 160 Z';
  const A_LEFT  = 'M 24 100 L 40 112 L 40 106 L 48 106 L 48 94 L 40 94 L 40 88 Z';

  const cls = `ptz-sector${busy ? ' ptz-sector-busy' : ''}`;

  function sector(d, label, dir) {
    return (
      <path
        key={dir}
        d={d}
        className={cls}
        role="button"
        aria-label={label}
        tabIndex={busy ? -1 : 0}
        onClick={busy ? undefined : () => onMove(dir)}
        onKeyDown={(e) => !busy && e.key === 'Enter' && onMove(dir)}
      />
    );
  }

  return (
    <svg
      viewBox="0 0 200 200"
      width={sz}
      height={sz}
      className={`ptz-ring${busy ? ' ptz-ring-busy' : ''}`}
      aria-label="PTZ controls"
    >
      {/* Interactive sectors — bottom layer so hover fill stays under structural lines */}
      {sector(UP,    'PTZ Up',    'up')}
      {sector(RIGHT, 'PTZ Right', 'right')}
      {sector(DOWN,  'PTZ Down',  'down')}
      {sector(LEFT,  'PTZ Left',  'left')}
      {/* Center stop */}
      <circle
        cx={cx} cy={cy} r={ri}
        className={cls}
        role="button"
        aria-label="PTZ Stop"
        tabIndex={busy ? -1 : 0}
        onClick={busy ? undefined : onStop}
        onKeyDown={(e) => !busy && e.key === 'Enter' && onStop()}
      />

      {/* Visual layer — drawn on top; pointer-events disabled so clicks pass through */}
      <g pointerEvents="none" strokeLinecap="round" strokeLinejoin="round">
        <circle cx={cx} cy={cy} r={ro} fill="none" stroke="currentColor" strokeWidth="1.5" />
        <circle cx={cx} cy={cy} r={ri} fill="none" stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx-di} y1={cy-di} x2={cx-d} y2={cy-d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx+di} y1={cy-di} x2={cx+d} y2={cy-d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx+di} y1={cy+di} x2={cx+d} y2={cy+d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx-di} y1={cy+di} x2={cx-d} y2={cy+d} stroke="currentColor" strokeWidth="1.5" />
        <path d={A_UP}    fill="currentColor" />
        <path d={A_RIGHT} fill="currentColor" />
        <path d={A_DOWN}  fill="currentColor" />
        <path d={A_LEFT}  fill="currentColor" />
        <rect x="89" y="89" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.5" />
      </g>
    </svg>
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
      <div className="preview-viewport">
        <LiveViewport
          key={`${preview.id}:${preview.device?.rtspUrl || ''}:${preview.device?.rtspTracks || ''}`}
          deviceId={preview.id}
          title={preview.title}
          authHeader={authHeader}
          streamConfig={streamConfig}
          rtspTracks={preview.device?.rtspTracks}
          streamKey={`${preview.device?.rtspUrl || ''}:${preview.device?.rtspTracks || ''}`}
        />
        {preview.ptzSupported ? (
          <div className="ptz-ring-overlay">
            <PTZRing
              busy={busy}
              size={150}
              onMove={(dir) => onPTZMove(preview.id, dir)}
              onStop={() => onPTZStop(preview.id)}
            />
          </div>
        ) : null}
      </div>
      <div className="preview-actions">
        <div className="action-row">
          <button type="button" className="quiet" onClick={() => onAdd(preview.device)} disabled={busy || !preview.device}>
            <span className="btn-icon"><Ico n="plus" /> Add to Live Views</span>
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
  streamOptionsById,
  selectedStreamTokens,
  saveDrafts,
  onCameraNav,
  onManualAddress,
  onTimeout,
  onScan,
  scanCIDR,
  onScanCIDR,
  onProbe,
  onSave,
  onSaveDraft,
  onDetailDraft,
  onSaveDetails,
  onDiscardDetails,
  onCredential,
  onPasswordDraft,
  onSaveCredentials,
  onChangePassword,
  onResolve,
  onStreamToken,
  onSelectStream,
  onTest,
  onPreview,
  onAddToViews,
  onPTZMove,
  onPTZStop,
  onRemove,
  onClosePreview,
}) {
  const [selectedSavedId, setSelectedSavedId] = useState(null);
  const [scanProtocol, setScanProtocol] = useState('all');
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
            <span className="btn-icon"><Ico n="search" /> Probe</span>
          </button>
          <button type="button" className={cameraNav === 'saved' ? 'active' : 'quiet'} onClick={() => onCameraNav('saved')}>
            <span className="btn-icon"><Ico n="camera" /> Saved</span>
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
              <label className="scan-protocol-label">
                Protocol
                <select value={scanProtocol} onChange={(e) => setScanProtocol(e.target.value)} className="scan-protocol-select">
                  <option value="all">All Methods</option>
                  <option value="onvif">ONVIF</option>
                  <option value="ssdp">SSDP / UPnP</option>
                  <option value="mdns">mDNS / Bonjour</option>
                  <option value="sadp">Hikvision SADP</option>
                  <option value="portscan">Port Scan</option>
                </select>
              </label>
              <label className="scan-protocol-label">
                <span className="scan-label-row">
                  Subnet
                  <InfoButton text={'Enter a subnet in CIDR notation to scan a specific network range.\nExamples:\n  192.168.1.0/24  — scan 192.168.1.1 to .254\n  10.10.20.0/24   — scan a VLAN\nLeave empty to auto-detect your local subnet.'} />
                </span>
                <input
                  value={scanCIDR}
                  onChange={(e) => onScanCIDR(e.target.value)}
                  placeholder="auto"
                  className="scan-cidr-input"
                />
              </label>
              <button type="button" onClick={() => onScan(scanProtocol, scanCIDR)} disabled={busy}>
                <span className="btn-icon"><Ico n="wifi" /> Scan</span>
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
                <span className="btn-icon"><Ico n="search" /> Probe</span>
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
                  streamOptions={streamOptionsById[selectedSaved.id]}
                  selectedStreamToken={selectedStreamTokens[selectedSaved.id]}
                  onDetailDraft={onDetailDraft}
                  onSaveDetails={onSaveDetails}
                  onDiscardDetails={onDiscardDetails}
                  onCredential={onCredential}
                  onPasswordDraft={onPasswordDraft}
                  onSaveCredentials={onSaveCredentials}
                  onChangePassword={onChangePassword}
                  onResolve={onResolve}
                  onStreamToken={onStreamToken}
                  onSelectStream={onSelectStream}
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
  const lineRule = isLineDetectionType(ruleDraft.detectionType);
  const lineRuleConfig = parseLineRuleConfig(ruleDraft.ruleConfig, ruleDraft.detectionType);
  const selectedZonePoints = parseZonePolygon(ruleDraft.zonePolygon);
  const scheduleDraft = scheduleDraftFromPolicy(ruleDraft.schedulePolicy);
  const [logSelectedAlertId, setLogSelectedAlertId] = useState(null);

  // Alert Log — self-contained server-paged state
  const logPageSize = 20;
  const [logPage, setLogPage] = useState(0);
  const [logDate, setLogDate] = useState(todayDateString);
  const [logAlerts, setLogAlerts] = useState([]);
  const [logTotal, setLogTotal] = useState(0);
  const [logLoading, setLogLoading] = useState(false);

  const fetchLogAlerts = useCallback(async (cameraId, page, dateStr) => {
    if (!cameraId) return;
    setLogLoading(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const params = new URLSearchParams({
        limit: String(logPageSize),
        offset: String(page * logPageSize),
        cameraId: String(cameraId),
      });
      if (dateStr) {
        const start = new Date(dateStr);
        start.setHours(0, 0, 0, 0);
        const end = new Date(dateStr);
        end.setHours(23, 59, 59, 999);
        params.set('createdAfter', String(Math.floor(start.getTime() / 1000)));
        params.set('createdBefore', String(Math.floor(end.getTime() / 1000)));
      }
      const resp = await fetch(`${apiBase()}/api/vision/alerts?${params}`, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setLogAlerts(Array.isArray(result?.items) ? result.items : []);
      setLogTotal(typeof result?.total === 'number' ? result.total : 0);
    } catch (_) {
      setLogAlerts([]);
      setLogTotal(0);
    } finally {
      setLogLoading(false);
    }
  }, [authHeader]);

  useEffect(() => {
    setLogPage(0);
  }, [selectedCamera?.id, logDate]);

  useEffect(() => {
    fetchLogAlerts(selectedCamera?.id, logPage, logDate);
  }, [selectedCamera?.id, logPage, logDate, fetchLogAlerts]);

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
    if (logSelectedAlertId !== null && !logAlerts.some((alert) => Number(alert.id) === Number(logSelectedAlertId))) {
      setLogSelectedAlertId(null);
    }
  }, [logAlerts, logSelectedAlertId]);

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

  function changeDetectionType(detectionType) {
    const next = { ...ruleDraft, detectionType };
    if (isLineDetectionType(detectionType)) {
      next.ruleConfig = lineRuleConfigText(parseLineRuleConfig(ruleDraft.ruleConfig, detectionType), detectionType);
      next.zonePolygon = ruleDraft.zonePolygon || defaultZonePolygon;
    } else {
      next.ruleConfig = '';
    }
    onRuleDraft(next);
  }

  function changeLineConfig(patch) {
    const next = normalizeLineConfig({ ...lineRuleConfig, ...patch }, ruleDraft.detectionType);
    onRuleDraft({ ...ruleDraft, ruleConfig: lineRuleConfigText(next, ruleDraft.detectionType) });
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
          <span className="btn-icon"><Ico n="reload" /> Reload</span>
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
                  <FormBusyOverlay busy={busy} />
                  <header>
                    <h2>{ruleDraft.id ? 'Edit Rule' : 'New Rule'}</h2>
                    {ruleDraft.id ? (
                      <button type="button" className="quiet" onClick={() => onRuleDraft(defaultVisionRuleDraft(selectedCamera.id))} disabled={busy}>
                        <span className="btn-icon"><Ico n="plus" /> New Rule</span>
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
                        onChange={(event) => changeDetectionType(event.target.value)}
                      >
                        <option value="fire">Fire</option>
                        <option value="smoke">Smoke</option>
                        <option value="person">Person</option>
                        <option value="vehicle">Vehicle</option>
                        <option value="animal">Animal</option>
                        <option value="intrusion">Intrusion</option>
                        <option value="line_crossing">Line crossing</option>
                        <option value="multi_line_crossing">Multi-line crossing</option>
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
                  {lineRule ? (
                    <>
                      <section className="schedule-panel">
                        <header>
                          <h3>Object Classes</h3>
                          <span className="status-pill">
                            {lineRuleConfig.classes.includes('*') ? 'any' : lineRuleConfig.classes.length}
                          </span>
                        </header>
                        <div className="schedule-days">
                          <label className="check-row line-class-any">
                            <input
                              type="checkbox"
                              checked={lineRuleConfig.classes.includes('*')}
                              onChange={(event) => {
                                changeLineConfig({ classes: event.target.checked ? ['*'] : defaultLineClasses });
                              }}
                            />
                            <strong>Anything</strong> — any object
                          </label>
                          {!lineRuleConfig.classes.includes('*') && lineClassOptions.map((label) => (
                            <label className="check-row" key={label}>
                              <input
                                type="checkbox"
                                checked={lineRuleConfig.classes.includes(label)}
                                onChange={(event) => {
                                  const current = new Set(lineRuleConfig.classes);
                                  if (event.target.checked) {
                                    current.add(label);
                                  } else {
                                    current.delete(label);
                                  }
                                  changeLineConfig({ classes: Array.from(current) });
                                }}
                              />
                              {label}
                            </label>
                          ))}
                        </div>
                        <div className="metadata-row">
                          <label>
                            Direction
                            <select value={lineRuleConfig.direction} onChange={(event) => changeLineConfig({ direction: event.target.value })}>
                              <option value="both">Either direction</option>
                              <option value="forward">Forward side</option>
                              <option value="reverse">Reverse side</option>
                            </select>
                          </label>
                          {ruleDraft.detectionType === 'multi_line_crossing' ? (
                            <label>
                              Max seconds between lines
                              <input
                                type="number"
                                min="1"
                                value={lineRuleConfig.maxSecondsBetweenLines}
                                onChange={(event) => changeLineConfig({ maxSecondsBetweenLines: Number(event.target.value) })}
                              />
                            </label>
                          ) : null}
                        </div>
                      </section>
                      <LineDrawingPreview
                        camera={selectedCamera}
                        config={lineRuleConfig}
                        detectionType={ruleDraft.detectionType}
                        authHeader={authHeader}
                        streamConfig={streamConfig}
                        disabled={busy}
                        onConfig={changeLineConfig}
                      />
                    </>
                  ) : (
                    <ZoneDrawingPreview
                      camera={selectedCamera}
                      polygonValue={ruleDraft.zonePolygon}
                      authHeader={authHeader}
                      streamConfig={streamConfig}
                      disabled={busy}
                      onPolygon={(zonePolygon) => onRuleDraft({ ...ruleDraft, cameraId: selectedCamera.id, zonePolygon })}
                    />
                  )}
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
                    <button type="submit" disabled={busy || (!lineRule && selectedZonePoints.length < 3) || (lineRule && lineRuleConfig.lines.length < (ruleDraft.detectionType === 'multi_line_crossing' ? 2 : 1))}>
                      <span className="btn-icon"><Ico n="save" /> Save Rule</span>
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
                        <p>
                          {rule.detectionType} / threshold {Number(rule.threshold || 0).toFixed(2)}
                          {lineCountFromRule(rule) ? ` / ${lineCountFromRule(rule)}` : ''} / {scheduleSummary(rule.schedulePolicy)}
                        </p>
                      </div>
                      <strong className={`status-pill ${rule.isEnabled ? 'online' : 'unknown'}`}>
                        {rule.isEnabled ? 'enabled' : 'disabled'}
                      </strong>
                      <div className="action-row">
                        <button type="button" className="quiet" onClick={() => onEditRule(rule)} disabled={busy}>
                          <span className="btn-icon"><Ico n="edit-2" /> Edit</span>
                        </button>
                        <button type="button" onClick={() => onTriggerTestAlert(rule)} disabled={busy}>
                          <span className="btn-icon"><Ico n="play" /> Test Alert</span>
                        </button>
                        <button type="button" className="quiet danger-text" onClick={() => onDeleteRule(rule.id)} disabled={busy}>
                          <span className="btn-icon"><Ico n="trash" /> Delete</span>
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              </section>

              <section className="settings-panel">
                <header>
                  <h2>Alert Log</h2>
                  <span className="status-pill">{logLoading ? '…' : logTotal}</span>
                </header>
                <div className="vision-list">
                  <div className="log-toolbar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
                    <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                      Date
                      <input
                        type="date"
                        value={logDate}
                        max={todayDateString()}
                        onChange={(e) => setLogDate(e.target.value)}
                      />
                    </label>
                    <button type="button" className="quiet" onClick={() => setLogDate('')} disabled={!logDate}>
                      All dates
                    </button>
                    <button type="button" className="quiet" onClick={() => setLogDate(todayDateString())} disabled={logDate === todayDateString()}>
                      Today
                    </button>
                    <button type="button" className="quiet" onClick={() => fetchLogAlerts(selectedCamera?.id, logPage, logDate)} disabled={logLoading}>
                      Reload
                    </button>
                  </div>
                  {logLoading ? <p className="empty">Loading…</p> : null}
                  {!logLoading && logAlerts.length === 0 ? <p className="empty">No alert events for this camera{logDate ? ' on this date' : ''}.</p> : null}
                  {logAlerts.length > 0 ? (
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
                          {logAlerts.map((alert) => {
                            const metadata = parseMetadata(alert.metadata);
                            const diagnostic = Boolean(metadata.diagnostic);
                            const rule = selectedRules.find((item) => Number(item.id) === Number(alert.ruleId));
                            const objectLabel = metadata.objectLabel;
                            return (
                              <tr key={alert.id} className={Number(logSelectedAlertId) === Number(alert.id) ? 'selected' : ''}>
                                <td>{formatTimestamp(alert.createdAt)}</td>
                                <td>
                                  <strong>{objectLabel || alert.label || alert.detectionType || 'Detection event'}</strong>
                                  <span>{formatSourceLabel(metadata.source)}</span>
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
                                    <button type="button" className="quiet" onClick={() => setLogSelectedAlertId(Number(logSelectedAlertId) === Number(alert.id) ? null : alert.id)}>
                                      {Number(logSelectedAlertId) === Number(alert.id) ? 'Close' : 'Details'}
                                    </button>
                                    <button
                                      type="button"
                                      className="quiet"
                                      onClick={() => onAcknowledgeAlert(alert.id)}
                                      disabled={busy || alert.isAcknowledged || diagnostic}
                                    >
                                      <Ico n="acknowledge" sz={12} />
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
                  {logTotal > logPageSize ? (
                    <div className="pagination-bar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginTop: '0.5rem' }}>
                      <button type="button" className="quiet" onClick={() => setLogPage((p) => Math.max(0, p - 1))} disabled={logPage === 0 || logLoading}>
                        ‹ Prev
                      </button>
                      <span style={{ fontSize: '0.85rem' }}>
                        Page {logPage + 1} / {Math.ceil(logTotal / logPageSize)}
                      </span>
                      <button type="button" className="quiet" onClick={() => setLogPage((p) => p + 1)} disabled={(logPage + 1) * logPageSize >= logTotal || logLoading}>
                        Next ›
                      </button>
                    </div>
                  ) : null}
                  {null /* detail shown in AlertDetailModal overlay */}
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
      {(() => {
        const logSelectedAlert = logAlerts.find((a) => Number(a.id) === Number(logSelectedAlertId)) || null;
        if (!logSelectedAlert) return null;
        const logSelectedAlertRule = selectedRules.find((r) => Number(r.id) === Number(logSelectedAlert.ruleId));
        return (
          <AlertDetailModal
            alert={logSelectedAlert}
            rule={logSelectedAlertRule}
            authHeader={authHeader}
            onClose={() => setLogSelectedAlertId(null)}
          />
        );
      })()}
    </section>
  );
}

function AlertDetailModal({ alert, rule, authHeader, onClose }) {
  const meta = parseMetadata(alert.metadata);
  const bb = parseBoundingBox(alert.boundingBox);
  const isDiagnostic = Boolean(meta.diagnostic);
  const isYolo = meta.source && (meta.source.includes('object') || meta.source.includes('yolo'));
  const isMotion = meta.source && meta.source.includes('motion');
  const isLineCrossing = alert.detectionType === 'line-crossing' || alert.detectionType === 'multi-line-crossing';
  const conf = Number(alert.confidence || 0);
  const objectMeta = meta.objectMeta && typeof meta.objectMeta === 'object' ? meta.objectMeta : {};
  const trackId = meta.trackId || objectMeta.trackId;
  const { url: snapUrl, loading: snapLoading, error: snapError } = useSnapshotBlob(alert.id, authHeader);

  React.useEffect(() => {
    function onKey(e) { if (e.key === 'Escape') onClose(); }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const title = meta.objectLabel || alert.label || alert.detectionType || 'Detection event';

  return (
    <div className="alert-modal-overlay" onClick={onClose}>
      <div className="alert-modal-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={title}>
        <div className="alert-modal-header">
          <div className="alert-modal-title-group">
            <span className="alert-modal-title" style={{ textTransform: 'capitalize' }}>{title}</span>
            <span className={`status-pill ${isDiagnostic ? 'unknown' : alert.isAcknowledged ? 'resolved' : 'offline'}`}>
              {isDiagnostic ? 'diagnostic' : alert.isAcknowledged ? 'acknowledged' : 'active'}
            </span>
            <span className="alert-modal-time">{formatTimestamp(alert.createdAt)}</span>
          </div>
          <button type="button" className="alert-modal-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="alert-modal-image-wrap">
          {snapLoading && <div className="alert-modal-snap-msg">Loading snapshot…</div>}
          {snapError && !snapLoading && <div className="alert-modal-snap-msg alert-modal-snap-none">No snapshot available</div>}
          {snapUrl && (
            <div className="alert-modal-snap-container">
              <img className="alert-modal-snap" src={snapUrl} alt="Detection snapshot" />
              {bb && (
                <div className="alert-modal-bb" style={{
                  left: `${(bb.x * 100).toFixed(3)}%`,
                  top: `${(bb.y * 100).toFixed(3)}%`,
                  width: `${(bb.w * 100).toFixed(3)}%`,
                  height: `${(bb.h * 100).toFixed(3)}%`,
                }}>
                  <span className="alert-modal-bb-label">{meta.objectLabel || alert.label || ''}</span>
                </div>
              )}
            </div>
          )}
        </div>

        <div className="alert-modal-meta">
          <dl className="event-grid">
            <div><dt>Rule</dt><dd>{rule?.name || `#${alert.ruleId || '-'}`}</dd></div>
            <div><dt>Type</dt><dd>{fieldValue(alert.detectionType)}</dd></div>
            <div><dt>Source</dt><dd>{formatSourceLabel(meta.source)}</dd></div>
            <div>
              <dt>Confidence</dt>
              <dd>
                <span className="conf-value">{conf.toFixed(3)}</span>
                <div className="conf-bar-wrap">
                  <div className="conf-bar" style={{ width: `${(conf * 100).toFixed(1)}%`, background: conf >= 0.6 ? 'var(--accent)' : conf >= 0.35 ? 'var(--warn-color, #e8a000)' : 'var(--danger-color, #d9534f)' }} />
                </div>
              </dd>
            </div>
            {isYolo && meta.objectLabel ? <div><dt>Object Label</dt><dd style={{ textTransform: 'capitalize' }}>{meta.objectLabel}</dd></div> : null}
            {trackId ? <div><dt>Track ID</dt><dd>{trackId}</dd></div> : null}
            {isLineCrossing && meta.lineId ? (
              <div><dt>Line</dt><dd>{meta.lineId}{meta.lineCount > 1 ? ` (${Number(meta.lineIndex) + 1}/${meta.lineCount})` : ''}</dd></div>
            ) : null}
            {isMotion && meta.changedRatio !== undefined ? <div><dt>Changed Area</dt><dd>{formatPercent(meta.changedRatio)}</dd></div> : null}
            {isDiagnostic ? (
              <>
                <div><dt>Status</dt><dd>{fieldValue(meta.status)}</dd></div>
                <div><dt>Threshold</dt><dd>{meta.ruleThreshold !== undefined ? Number(meta.ruleThreshold).toFixed(2) : '-'}</dd></div>
                <div><dt>Min Frames</dt><dd>{meta.ruleMinFrames ?? '-'}</dd></div>
                <div><dt>Cooldown</dt><dd>{meta.ruleCooldownSec !== undefined ? `${meta.ruleCooldownSec}s` : '-'}</dd></div>
                {meta.message ? <div style={{ gridColumn: '1 / -1' }}><dt>Message</dt><dd>{meta.message}</dd></div> : null}
              </>
            ) : null}
            {bb ? <div><dt>Bounding Box</dt><dd className="bb-coords-inline">X {formatPercent(bb.x)} Y {formatPercent(bb.y)} W {formatPercent(bb.w)} H {formatPercent(bb.h)}</dd></div> : null}
            <div><dt>Acknowledged</dt><dd>{alert.isAcknowledged ? formatTimestamp(alert.acknowledgedAt) : '-'}</dd></div>
          </dl>
        </div>
      </div>
    </div>
  );
}

function SettingsTab({
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
}) {
  const iceServers = settings.stream.webrtc.iceServers || [];
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
        <button type="button" className={settingsNav === 'users' ? 'active' : 'quiet'} onClick={() => onSettingsNav('users')}>
          <span className="btn-icon"><Ico n="user" /> Users</span>
        </button>
      </aside>

      <div className="settings-content">
        {settingsNav === 'runtime' ? (
          <form className="settings-layout" onSubmit={onSave}>
            <FormBusyOverlay busy={busy} />
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
      </div>
    </section>
  );
}

function formatFileSize(bytes) {
  const n = Number(bytes || 0);
  if (!n) return '-';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function segmentDuration(seg) {
  const start = Number(seg.startedAt || 0);
  const end = Number(seg.endedAt || 0);
  if (!start || !end || end <= start) return '-';
  const secs = end - start;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function segmentFilename(seg) {
  const p = seg.filePath || '';
  const parts = p.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || `clip-${seg.id}`;
}

function detectionTypeLabel(type) {
  const map = { motion: 'Motion', intrusion: 'Intrusion', fire: 'Fire', line_crossing: 'Line crossing', multi_line_crossing: 'Multi-line crossing' };
  return map[type] || (type ? type.replace(/_/g, ' ') : 'Event');
}

function todayDateString() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function RecordingTab({ saved, segments, configs, busy, authHeader, onSaveConfig, onDeleteSegment, onReload, focusCameraId, focusAlertId, unacknowledgedAlertIds, onAcknowledgeAlert, alerts }) {
  const orderedSaved = useMemo(() => orderedSavedCameras(saved), [saved]);
  const [selectedCameraId, setSelectedCameraId] = useState(0);
  const [recordingSubTab, setRecordingSubTab] = useState('events');
  const focusedRowRef = useRef(null);
  const onReloadRef = useRef(onReload);
  useEffect(() => { onReloadRef.current = onReload; });
  const effectiveCameraId = selectedCameraId || Number(orderedSaved[0]?.id) || 0;
  const selectedCamera = saved.find((d) => Number(d.id) === effectiveCameraId) || orderedSaved[0] || null;
  const eventClips = useMemo(
    () => segments.filter((s) => Number(s.cameraId) === effectiveCameraId && Number(s.alertId) > 0),
    [segments, effectiveCameraId],
  );
  const alertById = useMemo(
    () => new Map((alerts || []).map((a) => [Number(a.id), a])),
    [alerts],
  );
  const defaultDraft = useMemo(
    () => ({ cameraId: effectiveCameraId, enabled: false, preRollSec: 30, postRollSec: 10, storagePath: 'recordings', retentionDays: 7, segmentMinutes: 15, liveStreamUrl: '', streamUrl: '', fallbackStreamUrl: '' }),
    [effectiveCameraId],
  );
  const [configDraft, setConfigDraft] = useState(defaultDraft);
  const [downloading, setDownloading] = useState(null);
  const [playingSegment, setPlayingSegment] = useState(null);
  const [videoUrl, setVideoUrl] = useState(null);
  const [loadingVideo, setLoadingVideo] = useState(false);
  const [awaitAttempts, setAwaitAttempts] = useState(0);
  const maxAwaitAttempts = 12;

  // All Recordings browse state
  const [browseDate, setBrowseDate] = useState(todayDateString);
  const [allBrowseSegments, setAllBrowseSegments] = useState([]);
  const [browseLoading, setBrowseLoading] = useState(false);
  const [browseLoaded, setBrowseLoaded] = useState(false);
  const [timelineSelectedMin, setTimelineSelectedMin] = useState(null);
  const [timelineHoverMin, setTimelineHoverMin] = useState(null);
  const [timelineScrollTargetId, setTimelineScrollTargetId] = useState(null);
  const timelineBarRef = useRef(null);
  const segmentRefsMap = useRef({});

  // Recorder status
  const [recorderStatuses, setRecorderStatuses] = useState([]);
  const recorderStatusRef = useRef(null);

  const fetchRecorderStatus = useCallback(async () => {
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/recording/status`, { credentials: 'include', headers });
      if (!resp.ok) return;
      const payload = await resp.json();
      const items = payload?.data?.result ?? payload?.result ?? payload;
      setRecorderStatuses(Array.isArray(items) ? items : []);
    } catch (_) {}
  }, [authHeader]);

  useEffect(() => {
    fetchRecorderStatus();
    const id = setInterval(fetchRecorderStatus, 10000);
    recorderStatusRef.current = id;
    return () => clearInterval(id);
  }, [fetchRecorderStatus]);

  // ONVIF stream profiles for the selected camera
  const [streamProfiles, setStreamProfiles] = useState(null); // null = not loaded
  const [streamProfilesLoading, setStreamProfilesLoading] = useState(false);
  const [streamProfilesError, setStreamProfilesError] = useState('');

  const fetchStreamProfiles = useCallback(async () => {
    if (!effectiveCameraId) return;
    setStreamProfilesLoading(true);
    setStreamProfilesError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/recording/streams/${effectiveCameraId}`, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setStreamProfiles(result || null);
    } catch (e) {
      setStreamProfilesError(e.message || 'Failed to load streams');
      setStreamProfiles(null);
    } finally {
      setStreamProfilesLoading(false);
    }
  }, [effectiveCameraId, authHeader]);

  // Reset stream profiles when camera changes
  useEffect(() => {
    setStreamProfiles(null);
    setStreamProfilesError('');
  }, [effectiveCameraId]);

  const isAwaitingClip = Boolean(focusAlertId) &&
    (!focusCameraId || Number(effectiveCameraId) === Number(focusCameraId)) &&
    eventClips.every((s) => Number(s.alertId) !== Number(focusAlertId));

  useEffect(() => {
    if (focusCameraId) setSelectedCameraId(Number(focusCameraId));
  }, [focusCameraId]);

  useEffect(() => {
    setAwaitAttempts(0);
  }, [focusAlertId]);

  useEffect(() => {
    if (!isAwaitingClip || awaitAttempts >= maxAwaitAttempts) return;
    const id = setTimeout(() => {
      onReloadRef.current();
      setAwaitAttempts((n) => n + 1);
    }, 5000);
    return () => clearTimeout(id);
  }, [isAwaitingClip, awaitAttempts]);

  useEffect(() => {
    if (focusedRowRef.current) {
      focusedRowRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }, [focusAlertId, eventClips]);

  useEffect(() => {
    if (!playingSegment) return;
    const onKey = (e) => { if (e.key === 'Escape') closeVideoModal(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [playingSegment]);

  useEffect(() => {
    const existing = configs.find((c) => Number(c.cameraId) === effectiveCameraId);
    const currentLiveUrl = selectedCamera?.rtspUrl || '';
    setConfigDraft(existing ? { ...existing, liveStreamUrl: existing.liveStreamUrl || currentLiveUrl } : { ...defaultDraft, liveStreamUrl: currentLiveUrl });
    setAllBrowseSegments([]);
    setBrowseLoaded(false);
    setTimelineSelectedMin(null);
    setBrowseDate(todayDateString);
  }, [effectiveCameraId, configs, selectedCamera]);

  async function applyLiveStream(rtspUrl) {
    if (!rtspUrl || !effectiveCameraId) return;
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const resp = await fetch(`${apiBase()}/api/recording/streams/${effectiveCameraId}/live`, {
        method: 'POST',
        credentials: 'include',
        headers,
        body: JSON.stringify({ rtspUrl }),
      });
      if (!resp.ok) throw new Error(`${resp.status}`);
      await fetchStreamProfiles();
    } catch (e) {
      alert(`Failed to apply live stream: ${e.message}`);
    }
  }

  async function autoConfigureStreams() {
    if (!streamProfiles?.options?.length) return;
    const opts = streamProfiles.options;
    if (opts.length >= 2) {
      // Main stream → live view, sub-stream → recording
      setConfigDraft((d) => ({ ...d, liveStreamUrl: opts[0].rtspUrl, streamUrl: opts[1].rtspUrl, fallbackStreamUrl: opts[0].rtspUrl }));
    } else if (opts.length === 1) {
      // Only one stream — use it for everything
      setConfigDraft((d) => ({ ...d, liveStreamUrl: opts[0].rtspUrl, streamUrl: '', fallbackStreamUrl: '' }));
      alert('Only one stream profile found. Both live and recording will use the same stream.');
    }
  }

  const loadBrowseSegments = useCallback(async () => {
    if (!effectiveCameraId || !browseDate) return;
    setBrowseLoading(true);
    setTimelineSelectedMin(null);
    try {
      const dayStart = new Date(browseDate + 'T00:00:00');
      const dayEnd = new Date(browseDate + 'T23:59:59');
      const after = Math.floor(dayStart.getTime() / 1000);
      const before = Math.floor(dayEnd.getTime() / 1000);
      const headers = authHeader ? { Authorization: authHeader } : {};
      const url = `${apiBase()}/api/recording/segments?limit=500&offset=0&cameraId=${effectiveCameraId}&startedAfter=${after}&startedBefore=${before}`;
      const resp = await fetch(url, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const items = payload?.data?.result?.items || payload?.result?.items || payload?.items || [];
      const sorted = Array.isArray(items) ? [...items].sort((a, b) => a.startedAt - b.startedAt) : [];
      setAllBrowseSegments(sorted);
      setBrowseLoaded(true);
    } catch (_) {
      setAllBrowseSegments([]);
      setBrowseLoaded(true);
    } finally {
      setBrowseLoading(false);
    }
  }, [effectiveCameraId, browseDate, authHeader]);

  useEffect(() => {
    if (recordingSubTab !== 'browse') return;
    loadBrowseSegments();
  }, [loadBrowseSegments, recordingSubTab]);

  function handleTimelineClick(e) {
    if (!timelineBarRef.current) return;
    const rect = timelineBarRef.current.getBoundingClientRect();
    const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const clickedMin = Math.floor(fraction * 24 * 60);
    setTimelineSelectedMin(clickedMin);
    const dayStart = new Date(browseDate + 'T00:00:00');
    const clickedSec = dayStart.getTime() / 1000 + clickedMin * 60;
    let nearest = null;
    let nearestDist = Infinity;
    for (const seg of allBrowseSegments) {
      const mid = (seg.startedAt + (seg.endedAt || seg.startedAt)) / 2;
      const dist = Math.abs(mid - clickedSec);
      if (dist < nearestDist) { nearestDist = dist; nearest = seg; }
    }
    if (nearest) setTimelineScrollTargetId(nearest.id);
  }

  function handleTimelineHover(e) {
    if (!timelineBarRef.current) return;
    const rect = timelineBarRef.current.getBoundingClientRect();
    const fraction = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    setTimelineHoverMin(Math.floor(fraction * 24 * 60));
  }

  useEffect(() => {
    if (!timelineScrollTargetId) return;
    const el = segmentRefsMap.current[timelineScrollTargetId];
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }, [timelineScrollTargetId]);

  async function downloadSegment(seg) {
    setDownloading(seg.id);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const response = await fetch(`${apiBase()}/api/recording/segments/${seg.id}/download`, {
        credentials: 'include',
        headers,
      });
      if (!response.ok) throw new Error(`Download failed: ${response.status}`);
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = segmentFilename(seg);
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (_) {
      // silent
    } finally {
      setDownloading(null);
    }
  }

  async function playSegment(seg) {
    setPlayingSegment(seg);
    setVideoUrl(null);
    setLoadingVideo(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/recording/segments/${seg.id}/download`, {
        credentials: 'include',
        headers,
      });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const blob = await resp.blob();
      setVideoUrl(URL.createObjectURL(blob));
    } catch (_) {
      setPlayingSegment(null);
    } finally {
      setLoadingVideo(false);
    }
  }

  function closeVideoModal() {
    setVideoUrl((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return null;
    });
    setPlayingSegment(null);
    setLoadingVideo(false);
  }

  function renderSegmentRow(seg, isFocused, opts = {}) {
    const segAlert = seg.alertId ? alertById.get(Number(seg.alertId)) : null;
    const eventDesc = segAlert ? (segAlert.label || detectionTypeLabel(segAlert.detectionType)) : null;
    const extraClass = opts.extraClass || '';
    return (
      <div
        key={seg.id}
        ref={(el) => {
          if (isFocused) focusedRowRef.current = el;
          if (opts.segRef) opts.segRef(el);
        }}
        className={`segment-row${isFocused ? ' focused' : ''}${extraClass ? ` ${extraClass}` : ''}`}
      >
        <button type="button" className="segment-thumb-btn" onClick={() => playSegment(seg)} title="Play">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M8 5v14l11-7z"/>
          </svg>
        </button>
        <div className="segment-info">
          <div className="segment-title-row">
            <strong className="segment-filename">{segmentFilename(seg)}</strong>
            {eventDesc && <span className="segment-event-label">{eventDesc}</span>}
            {seg.alertId > 0 && unacknowledgedAlertIds && unacknowledgedAlertIds.has(Number(seg.alertId)) && (
              <span className="segment-unreviewed">Unreviewed</span>
            )}
          </div>
          <span className="segment-meta">
            {formatTimestamp(seg.startedAt)}
            {' · '}
            {segmentDuration(seg)}
            {' · '}
            {formatFileSize(seg.fileSize)}
            {seg.alertId ? ` · Alert #${seg.alertId}` : ''}
          </span>
        </div>
        <div className="segment-actions">
          {seg.alertId > 0 && unacknowledgedAlertIds && unacknowledgedAlertIds.has(Number(seg.alertId)) && (
            <button type="button" className="quiet" disabled={busy} onClick={() => onAcknowledgeAlert(seg.alertId)}>
              <span className="btn-icon"><Ico n="acknowledge" /> Acknowledge</span>
            </button>
          )}
          <button type="button" className="quiet" onClick={() => playSegment(seg)}>
            <span className="btn-icon"><Ico n="play" /> Play</span>
          </button>
          <button type="button" className="quiet" disabled={downloading === seg.id} onClick={() => downloadSegment(seg)}>
            <span className="btn-icon"><Ico n="download" /> {downloading === seg.id ? 'Downloading…' : 'Download'}</span>
          </button>
          <button type="button" className="quiet danger-text" disabled={busy} onClick={() => onDeleteSegment(seg.id)}>
            <span className="btn-icon"><Ico n="trash" /> Delete</span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <section className="workspace">
      <div className="toolbar">
        <div>
          <h2 className="section-title">Recording</h2>
          <p className="section-subtitle">Continuous NVR recording with event clip extraction.</p>
        </div>
        <button type="button" className="quiet" onClick={onReload} disabled={busy}>
          <span className="btn-icon"><Ico n="reload" /> Reload</span>
        </button>
      </div>

      <section className="saved-browser">
        <SavedDeviceNav devices={saved} selectedId={selectedCamera?.id} onSelect={setSelectedCameraId} />

        <main className="saved-detail">
          {selectedCamera ? (
            <div className="recording-layout">
              <section className="settings-panel">
                <header>
                  <div>
                    <h2>{cameraTitle(selectedCamera)}</h2>
                    <p className="section-subtitle">{selectedCamera.host || selectedCamera.xAddr || 'Saved camera'}</p>
                  </div>
                  <span className={`status-pill${configDraft.enabled ? ' online' : ''}`}>
                    {configDraft.enabled ? 'Recording on' : 'Recording off'}
                  </span>
                </header>

                <div className="recording-config-grid">
                  <label className="field-label">
                    <span>Segment length (minutes)</span>
                    <input
                      type="number" min="1" max="60"
                      value={configDraft.segmentMinutes || 15}
                      onChange={(e) => setConfigDraft({ ...configDraft, segmentMinutes: Number(e.target.value) })}
                    />
                  </label>
                  <label className="field-label">
                    <span>Pre-roll (seconds)</span>
                    <input
                      type="number" min="5" max="120"
                      value={configDraft.preRollSec}
                      onChange={(e) => setConfigDraft({ ...configDraft, preRollSec: Number(e.target.value) })}
                    />
                  </label>
                  <label className="field-label">
                    <span>Post-roll (seconds)</span>
                    <input
                      type="number" min="3" max="120"
                      value={configDraft.postRollSec}
                      onChange={(e) => setConfigDraft({ ...configDraft, postRollSec: Number(e.target.value) })}
                    />
                  </label>
                  <label className="field-label">
                    <span>Retention (days)</span>
                    <input
                      type="number" min="1" max="365"
                      value={configDraft.retentionDays || 7}
                      onChange={(e) => setConfigDraft({ ...configDraft, retentionDays: Number(e.target.value) })}
                    />
                  </label>
                  <label className="field-label">
                    <span>Storage path</span>
                    <input
                      type="text"
                      value={configDraft.storagePath || ''}
                      onChange={(e) => setConfigDraft({ ...configDraft, storagePath: e.target.value })}
                      placeholder="recordings"
                    />
                  </label>
                </div>

                {/* Stream Configuration */}
                <details style={{marginTop:'12px'}}>
                  <summary style={{cursor:'pointer', fontSize:'13px', fontWeight:'600', userSelect:'none', padding:'4px 0'}}>
                    Stream Configuration
                    {configDraft.streamUrl ? <span style={{marginLeft:'8px', fontSize:'11px', color:'var(--text-muted,#94a3b8)', fontWeight:'normal'}}>custom recording stream set</span> : null}
                  </summary>
                  <div style={{marginTop:'10px', display:'flex', flexDirection:'column', gap:'10px'}}>
                    {/* Auto-detect + auto-configure */}
                    <div style={{display:'flex', gap:'8px', alignItems:'center'}}>
                      <button type="button" className="quiet" style={{fontSize:'12px'}} onClick={fetchStreamProfiles} disabled={streamProfilesLoading}>
                        {streamProfilesLoading ? 'Loading streams…' : 'Detect streams'}
                      </button>
                      {streamProfiles?.options?.length >= 2 && (
                        <button type="button" className="quiet" style={{fontSize:'12px'}} onClick={autoConfigureStreams}>
                          Auto-configure (main→live, sub→recording)
                        </button>
                      )}
                      {streamProfilesError && <span style={{fontSize:'12px', color:'#ef4444'}}>{streamProfilesError}</span>}
                    </div>

                    {/* Live view stream */}
                    <label className="field-label" style={{gap:'4px'}}>
                      <span style={{fontSize:'12px', fontWeight:'600'}}>Live view stream</span>
                      {streamProfiles?.options?.length > 0 && (
                        <div style={{display:'flex', gap:'6px', flexWrap:'wrap', marginBottom:'4px'}}>
                          {streamProfiles.options.map((opt) => {
                            const url = opt.rtspUrl || '';
                            const isCurrent = configDraft.liveStreamUrl === url;
                            const label = `${opt.name || opt.Name || opt.profileToken} — ${opt.encoding || opt.Encoding} ${opt.width || opt.Width}×${opt.height || opt.Height}`;
                            return (
                              <button key={opt.profileToken || opt.ProfileToken} type="button" className={`quiet${isCurrent ? ' active' : ''}`} style={{fontSize:'11px'}}
                                onClick={() => setConfigDraft((d) => ({ ...d, liveStreamUrl: url }))} title={url}>
                                {isCurrent ? '✓ ' : ''}{label}
                              </button>
                            );
                          })}
                        </div>
                      )}
                      <input type="text" value={configDraft.liveStreamUrl || ''} onChange={(e) => setConfigDraft({ ...configDraft, liveStreamUrl: e.target.value })}
                        placeholder="rtsp://user:pass@ip/stream1" />
                    </label>

                    {/* Recording stream */}
                    <label className="field-label" style={{gap:'4px'}}>
                      <span style={{fontSize:'12px', fontWeight:'600'}}>Recording stream <span style={{fontWeight:'normal', color:'var(--text-muted,#94a3b8)'}}>(leave blank to use live-view stream)</span></span>
                      {streamProfiles?.options?.length > 0 && (
                        <div style={{display:'flex', gap:'6px', flexWrap:'wrap', marginBottom:'4px'}}>
                          {streamProfiles.options.map((opt) => {
                            const url = opt.rtspUrl || '';
                            const isCurrent = configDraft.streamUrl === url;
                            const label = `${opt.name || opt.Name || opt.profileToken} — ${opt.encoding || opt.Encoding} ${opt.width || opt.Width}×${opt.height || opt.Height}`;
                            return (
                              <button key={opt.profileToken || opt.ProfileToken} type="button" className={`quiet${isCurrent ? ' active' : ''}`} style={{fontSize:'11px'}}
                                onClick={() => setConfigDraft((d) => ({ ...d, streamUrl: url }))} title={url}>
                                {isCurrent ? '✓ ' : ''}{label}
                              </button>
                            );
                          })}
                        </div>
                      )}
                      <input type="text" value={configDraft.streamUrl || ''} onChange={(e) => setConfigDraft({ ...configDraft, streamUrl: e.target.value })}
                        placeholder="rtsp://user:pass@ip/stream2" />
                    </label>

                    {/* Fallback stream */}
                    <label className="field-label" style={{gap:'4px'}}>
                      <span style={{fontSize:'12px', fontWeight:'600'}}>Fallback recording stream <span style={{fontWeight:'normal', color:'var(--text-muted,#94a3b8)'}}>(tried after 2 quick failures of the primary)</span></span>
                      {streamProfiles?.options?.length > 0 && (
                        <div style={{display:'flex', gap:'6px', flexWrap:'wrap', marginBottom:'4px'}}>
                          {streamProfiles.options.map((opt) => {
                            const url = opt.rtspUrl || '';
                            const isCurrent = configDraft.fallbackStreamUrl === url;
                            const label = `${opt.name || opt.Name || opt.profileToken} — ${opt.encoding || opt.Encoding} ${opt.width || opt.Width}×${opt.height || opt.Height}`;
                            return (
                              <button key={opt.profileToken || opt.ProfileToken} type="button" className={`quiet${isCurrent ? ' active' : ''}`} style={{fontSize:'11px'}}
                                onClick={() => setConfigDraft((d) => ({ ...d, fallbackStreamUrl: url }))} title={url}>
                                {isCurrent ? '✓ ' : ''}{label}
                              </button>
                            );
                          })}
                        </div>
                      )}
                      <input type="text" value={configDraft.fallbackStreamUrl || ''} onChange={(e) => setConfigDraft({ ...configDraft, fallbackStreamUrl: e.target.value })}
                        placeholder="rtsp://user:pass@ip/stream1  (optional)" />
                    </label>
                  </div>
                </details>

                <div className="settings-actions">
                  <label className="check-row">
                    <input
                      type="checkbox"
                      checked={!!configDraft.enabled}
                      onChange={(e) => setConfigDraft({ ...configDraft, enabled: e.target.checked })}
                    />
                    Enable recording for this camera
                  </label>
                  <button type="button" onClick={async () => {
                    const newLive = (configDraft.liveStreamUrl || '').trim();
                    const prevLive = (selectedCamera?.rtspUrl || '').trim();
                    if (newLive && newLive !== prevLive) {
                      await applyLiveStream(newLive);
                    }
                    onSaveConfig(configDraft);
                  }} disabled={busy}>
                    <span className="btn-icon"><Ico n="save" /> Save config</span>
                  </button>
                </div>

                {/* Recorder status panel */}
                {(() => {
                  const rs = recorderStatuses.find((s) => Number(s.cameraId) === effectiveCameraId);
                  if (rs) {
                    const isOk = rs.state === 'streaming';
                    const isErr = rs.state === 'error';
                    return (
                      <div style={{marginTop: '12px', padding: '10px 12px', borderRadius: '6px', background: isOk ? 'rgba(34,197,94,0.1)' : isErr ? 'rgba(239,68,68,0.1)' : 'rgba(148,163,184,0.1)', border: `1px solid ${isOk ? 'rgba(34,197,94,0.3)' : isErr ? 'rgba(239,68,68,0.3)' : 'rgba(148,163,184,0.3)'}`}}>
                        <div style={{display:'flex', alignItems:'center', gap:'8px', marginBottom: rs.lastError || rs.liveDir ? '6px' : '0'}}>
                          <span style={{width:'8px', height:'8px', borderRadius:'50%', background: isOk ? '#22c55e' : isErr ? '#ef4444' : '#94a3b8', display:'inline-block', flexShrink:0}} />
                          <strong style={{fontSize:'13px'}}>{isOk ? 'Recording active' : isErr ? 'Recorder error' : 'Recorder stopped'}</strong>
                          <span style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', marginLeft:'auto'}}>{rs.liveFiles} live segment{rs.liveFiles !== 1 ? 's' : ''}</span>
                          <button type="button" className="quiet" style={{fontSize:'11px', padding:'2px 6px'}} onClick={fetchRecorderStatus}>↻</button>
                        </div>
                        {rs.liveDir && <div style={{fontSize:'11px', color:'var(--text-muted, #94a3b8)', wordBreak:'break-all'}}>{rs.liveDir}</div>}
                        {rs.activeStreamUrl && <div style={{fontSize:'11px', color:'var(--text-muted, #94a3b8)', wordBreak:'break-all', marginTop:'2px'}}>
                          {rs.usingFallback ? '⚠ Fallback: ' : 'Stream: '}{rs.activeStreamUrl}
                        </div>}
                        {rs.lastError && <div style={{fontSize:'12px', color:'#ef4444', marginTop:'4px', wordBreak:'break-all'}}>{rs.lastError}</div>}
                      </div>
                    );
                  }
                  if (configDraft.enabled) {
                    return (
                      <div style={{marginTop: '12px', padding: '10px 12px', borderRadius: '6px', background: 'rgba(234,179,8,0.1)', border: '1px solid rgba(234,179,8,0.3)'}}>
                        <div style={{display:'flex', alignItems:'center', gap:'8px'}}>
                          <span style={{width:'8px', height:'8px', borderRadius:'50%', background:'#eab308', display:'inline-block', flexShrink:0}} />
                          <strong style={{fontSize:'13px'}}>No active recorder</strong>
                          <button type="button" className="quiet" style={{fontSize:'11px', padding:'2px 6px', marginLeft:'auto'}} onClick={fetchRecorderStatus}>↻</button>
                        </div>
                        <div style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', marginTop:'4px'}}>Recording is enabled but no recorder is running. Ensure the camera has an RTSP URI configured and the storage path is writable. Check server logs for details.</div>
                      </div>
                    );
                  }
                  return null;
                })()}
              </section>

              <section className="settings-panel">
                <nav className="secondary-tabs" style={{marginBottom: '12px'}} aria-label="Recording view">
                  <button type="button" className={recordingSubTab === 'events' ? 'active' : 'quiet'} onClick={() => setRecordingSubTab('events')}>
                    <span className="btn-icon"><Ico n="list" /> Event Clips</span>
                  </button>
                  <button type="button" className={recordingSubTab === 'browse' ? 'active' : 'quiet'} onClick={() => setRecordingSubTab('browse')}>
                    <span className="btn-icon"><Ico n="folder" /> All Recordings</span>
                  </button>
                </nav>

                {recordingSubTab === 'events' && (
                  <>
                    <header>
                      <h2>Event Clips</h2>
                      <span className="status-pill">{eventClips.length}</span>
                    </header>

                    {isAwaitingClip && awaitAttempts < maxAwaitAttempts && (
                      <div className="recording-pending">
                        <span className="recording-pending-dot" />
                        Recording in progress for Alert #{focusAlertId} — checking for clip every 5 s
                        {awaitAttempts > 0 ? ` (${awaitAttempts}/${maxAwaitAttempts})` : '…'}
                      </div>
                    )}
                    {isAwaitingClip && awaitAttempts >= maxAwaitAttempts && (
                      <div className="recording-pending recording-pending--timeout">
                        Clip not found after 60 s for Alert #{focusAlertId}. Check that recording is enabled and the storage path is writable, then click Reload.
                      </div>
                    )}

                    {eventClips.length === 0 ? (
                      <p className="empty-hint">No event clips yet. Enable recording and trigger an alert to capture a clip.</p>
                    ) : (
                      <div className="segment-list">
                        {eventClips.map((seg) => {
                          const isFocused = focusAlertId && Number(seg.alertId) === Number(focusAlertId);
                          return renderSegmentRow(seg, isFocused);
                        })}
                      </div>
                    )}
                  </>
                )}

                {recordingSubTab === 'browse' && (() => {
                  const dayStartSec = browseDate ? new Date(browseDate + 'T00:00:00').getTime() / 1000 : 0;
                  const MINS_IN_DAY = 24 * 60;
                  const continuousSegs = allBrowseSegments.filter((s) => !s.alertId || Number(s.alertId) === 0);
                  const eventSegs = allBrowseSegments.filter((s) => Number(s.alertId) > 0);
                  const hoverLabel = timelineHoverMin !== null
                    ? `${String(Math.floor(timelineHoverMin / 60)).padStart(2, '0')}:${String(timelineHoverMin % 60).padStart(2, '0')}`
                    : null;
                  const selectedLabel = timelineSelectedMin !== null
                    ? `${String(Math.floor(timelineSelectedMin / 60)).padStart(2, '0')}:${String(timelineSelectedMin % 60).padStart(2, '0')}`
                    : null;
                  return (
                  <>
                    <header>
                      <h2>All Recordings</h2>
                      <span className="status-pill">{browseLoaded ? allBrowseSegments.length : '—'}</span>
                    </header>
                    <div className="log-toolbar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
                      <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                        Date
                        <input
                          type="date"
                          value={browseDate}
                          max={todayDateString()}
                          onChange={(e) => { setBrowseDate(e.target.value); setAllBrowseSegments([]); setBrowseLoaded(false); setTimelineSelectedMin(null); }}
                        />
                      </label>
                      <button type="button" className="quiet" onClick={() => { const t = todayDateString(); setBrowseDate(t); setAllBrowseSegments([]); setBrowseLoaded(false); setTimelineSelectedMin(null); }} disabled={browseDate === todayDateString()}>
                        Today
                      </button>
                    </div>
                    {browseLoading && <p className="empty-hint">Loading…</p>}

                    {browseLoaded && (
                      <div className="timeline-wrap">
                        <div className="timeline-hour-labels">
                          {[0, 3, 6, 9, 12, 15, 18, 21].map((h) => (
                            <span key={h} className="timeline-hour-label" style={{left: `${(h / 24) * 100}%`}}>
                              {h === 0 ? '12am' : h < 12 ? `${h}am` : h === 12 ? '12pm' : `${h - 12}pm`}
                            </span>
                          ))}
                          <span className="timeline-hour-label" style={{left: '100%', transform: 'translateX(-100%)'}}>12am</span>
                        </div>

                        <div
                          className="timeline-bar"
                          ref={timelineBarRef}
                          onClick={handleTimelineClick}
                          onMouseMove={handleTimelineHover}
                          onMouseLeave={() => setTimelineHoverMin(null)}
                          title="Click to jump to a time"
                        >
                          {/* hour tick marks */}
                          {Array.from({length: 25}, (_, h) => (
                            <div key={h} className="timeline-tick" style={{left: `${(h / 24) * 100}%`}} />
                          ))}
                          {/* 3-hour major ticks */}
                          {[0, 3, 6, 9, 12, 15, 18, 21, 24].map((h) => (
                            <div key={`major-${h}`} className="timeline-tick timeline-tick--major" style={{left: `${(h / 24) * 100}%`}} />
                          ))}

                          {/* continuous recordings — blue */}
                          {continuousSegs.map((seg) => {
                            const startMin = Math.max(0, (seg.startedAt - dayStartSec) / 60);
                            const endMin = Math.min(MINS_IN_DAY, ((seg.endedAt || seg.startedAt + 900) - dayStartSec) / 60);
                            if (endMin <= 0 || startMin >= MINS_IN_DAY) return null;
                            const left = (startMin / MINS_IN_DAY) * 100;
                            const width = Math.max(0.3, ((endMin - startMin) / MINS_IN_DAY) * 100);
                            return (
                              <div
                                key={seg.id}
                                className="timeline-segment timeline-segment--cont"
                                style={{left: `${left}%`, width: `${width}%`}}
                                title={`${formatTimestamp(seg.startedAt)} · ${segmentDuration(seg)}`}
                              />
                            );
                          })}

                          {/* event clips — red */}
                          {eventSegs.map((seg) => {
                            const startMin = Math.max(0, (seg.startedAt - dayStartSec) / 60);
                            const endMin = Math.min(MINS_IN_DAY, ((seg.endedAt || seg.startedAt + 60) - dayStartSec) / 60);
                            if (endMin <= 0 || startMin >= MINS_IN_DAY) return null;
                            const left = (startMin / MINS_IN_DAY) * 100;
                            const width = Math.max(0.5, ((endMin - startMin) / MINS_IN_DAY) * 100);
                            return (
                              <div
                                key={seg.id}
                                className="timeline-segment timeline-segment--event"
                                style={{left: `${left}%`, width: `${width}%`}}
                                title={`Alert #${seg.alertId} · ${formatTimestamp(seg.startedAt)} · ${segmentDuration(seg)}`}
                              />
                            );
                          })}

                          {/* hover line */}
                          {timelineHoverMin !== null && (
                            <div className="timeline-hover-line" style={{left: `${(timelineHoverMin / MINS_IN_DAY) * 100}%`}}>
                              <span className="timeline-time-label">{hoverLabel}</span>
                            </div>
                          )}

                          {/* selected cursor */}
                          {timelineSelectedMin !== null && (
                            <div className="timeline-cursor-line" style={{left: `${(timelineSelectedMin / MINS_IN_DAY) * 100}%`}}>
                              <span className="timeline-time-label timeline-time-label--selected">{selectedLabel}</span>
                            </div>
                          )}
                        </div>

                        <div className="timeline-legend">
                          <span className="timeline-legend-item timeline-legend-item--cont">Continuous</span>
                          <span className="timeline-legend-item timeline-legend-item--event">Event clip</span>
                          {timelineSelectedMin !== null && (
                            <span style={{fontSize: '12px', color: '#667788', marginLeft: 'auto'}}>
                              Selected: {selectedLabel} · {allBrowseSegments.filter((s) => {
                                const startMin = (s.startedAt - dayStartSec) / 60;
                                const endMin = ((s.endedAt || s.startedAt) - dayStartSec) / 60;
                                return timelineSelectedMin >= startMin && timelineSelectedMin <= endMin;
                              }).length} segment(s) at this time
                            </span>
                          )}
                        </div>
                      </div>
                    )}

                    {browseLoaded && allBrowseSegments.length === 0 && (
                      <p className="empty-hint">No recordings found for this date.</p>
                    )}
                    {allBrowseSegments.length > 0 && (
                      <div className="segment-list" style={{marginTop: '8px'}}>
                        {allBrowseSegments.map((seg) => {
                          const isTarget = timelineScrollTargetId && Number(seg.id) === Number(timelineScrollTargetId);
                          const segStartMin = dayStartSec ? (seg.startedAt - dayStartSec) / 60 : null;
                          const segEndMin = dayStartSec ? ((seg.endedAt || seg.startedAt) - dayStartSec) / 60 : null;
                          const isInSelectedSlot = timelineSelectedMin !== null && segStartMin !== null
                            && timelineSelectedMin >= segStartMin && timelineSelectedMin <= segEndMin;
                          const extraClass = isInSelectedSlot && !isTarget ? 'timeline-highlighted' : '';
                          return renderSegmentRow(seg, isTarget, {
                            extraClass,
                            segRef: (el) => { segmentRefsMap.current[seg.id] = el; },
                          });
                        })}
                      </div>
                    )}
                  </>
                  );
                })()}
              </section>
            </div>
          ) : (
            <p className="empty-hint">No cameras saved. Add a camera in the Cameras tab first.</p>
          )}
        </main>
      </section>

      {playingSegment && (() => {
        const playAlert = playingSegment.alertId ? alertById.get(Number(playingSegment.alertId)) : null;
        const playEventDesc = playAlert ? (playAlert.label || detectionTypeLabel(playAlert.detectionType)) : null;
        return (
        <div className="video-overlay" onClick={closeVideoModal}>
          <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="video-dialog-header">
              <div className="video-dialog-title-group">
                <span className="video-dialog-title">{segmentFilename(playingSegment)}</span>
                {playEventDesc && <span className="segment-event-label">{playEventDesc}</span>}
              </div>
              <button type="button" className="video-dialog-close" onClick={closeVideoModal} aria-label="Close">✕</button>
            </div>
            <div className="video-dialog-body">
              {loadingVideo && <div className="video-loading-msg">Loading video…</div>}
              {videoUrl && (
                <video className="video-player" controls autoPlay src={videoUrl} />
              )}
            </div>
            <div className="video-dialog-meta">
              {formatTimestamp(playingSegment.startedAt)} · {segmentDuration(playingSegment)} · {formatFileSize(playingSegment.fileSize)}
              {playingSegment.alertId ? ` · Alert #${playingSegment.alertId}` : ''}
            </div>
          </div>
        </div>
        );
      })()}
    </section>
  );
}

export default function App() {
  const initialLiveViews = readLiveViewsCookie();
  const [theme, setTheme] = useState(() => {
    try { return localStorage.getItem('mymatasan_theme') || 'light'; } catch (_) { return 'light'; }
  });
  useEffect(() => {
    const root = document.documentElement;
    THEMES.forEach((t) => root.classList.remove(`theme-${t}`));
    root.classList.add(`theme-${theme}`);
  }, [theme]);
  function changeTheme(t) {
    setTheme(t);
    try { localStorage.setItem('mymatasan_theme', t); } catch (_) {}
  }
  const [credentials, setCredentials] = useState(emptyLogin);
  const [authenticated, setAuthenticated] = useState(false);
  const [activeTab, setActiveTab] = useState('views');
  const [settingsNav, setSettingsNav] = useState('runtime');
  const [cameraNav, setCameraNav] = useState('probe');
  const [manualAddress, setManualAddress] = useState('');
  const [timeoutMs, setTimeoutMs] = useState(3000);
  const [scanCIDR, setScanCIDR] = useState('');
  useEffect(() => {
    if (!authenticated || scanCIDR !== '') return;
    fetch('/api/onvif/local-subnets')
      .then((r) => r.json())
      .then((res) => {
        const subnets = res && res.data;
        if (Array.isArray(subnets) && subnets.length > 0) {
          setScanCIDR(subnets[0]);
        }
      })
      .catch(() => {});
  }, [authenticated]);
  const [saved, setSaved] = useState([]);
  const [discovered, setDiscovered] = useState([]);
  const [saveDrafts, setSaveDrafts] = useState({});
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [deviceDrafts, setDeviceDrafts] = useState({});
  const [deviceCredentials, setDeviceCredentials] = useState({});
  const [cameraPasswordDrafts, setCameraPasswordDrafts] = useState({});
  const [streamOptionsById, setStreamOptionsById] = useState({});
  const [selectedStreamTokens, setSelectedStreamTokens] = useState({});
  const [viewLayout, setViewLayout] = useState(initialLiveViews.layout);
  const [viewTiles, setViewTiles] = useState([]);
  const [draggedTileId, setDraggedTileId] = useState(null);
  const [preview, setPreview] = useState(null);
  const [streamConfig, setStreamConfig] = useState(defaultStreamConfig);
  const [runtimeSettings, setRuntimeSettings] = useState(defaultRuntimeSettings);
  const [savedRuntimeSettings, setSavedRuntimeSettings] = useState(defaultRuntimeSettings);
  const [runtimeAutoTune, setRuntimeAutoTune] = useState(null);
  const [decoderGpuDevices, setDecoderGpuDevices] = useState(null);
  const [visionToolStatus, setVisionToolStatus] = useState(null);
  const [visionInstallResult, setVisionInstallResult] = useState(null);
  const [users, setUsers] = useState([]);
  const [newUser, setNewUser] = useState(defaultNewUser);
  const [passwordDrafts, setPasswordDrafts] = useState({});
  const [visionRules, setVisionRules] = useState([]);
  const [visionAlerts, setVisionAlerts] = useState([]);
  const [visionRuleDraft, setVisionRuleDraft] = useState(defaultVisionRuleDraft());
  const [recordingSegments, setRecordingSegments] = useState([]);
  const [recordingConfigs, setRecordingConfigs] = useState([]);
  const [notifOpen, setNotifOpen] = useState(false);
  const [notifUnread, setNotifUnread] = useState(0);
  const [recordingFocusCameraId, setRecordingFocusCameraId] = useState(0);
  const [recordingFocusAlertId, setRecordingFocusAlertId] = useState(0);
  const [seenInRecordingIds, setSeenInRecordingIds] = useState(new Set());
  const seenVisionAlertIdsRef = useRef(new Set());
  const initialNotifDoneRef = useRef(false);
  const activeVisionAlertsByCamera = useMemo(() => latestAlertsByCamera(visionAlerts), [visionAlerts]);
  const tileAlertsByCamera = useMemo(() => {
    const map = latestAlertsByCamera(visionAlerts);
    for (const [camId, alerts] of map) {
      if (alerts.every((a) => seenInRecordingIds.has(a.id))) {
        map.delete(camId);
      }
    }
    return map;
  }, [visionAlerts, seenInRecordingIds]);
  const unacknowledgedAlertIds = useMemo(
    () => new Set(visionAlerts.filter((a) => !a.isAcknowledged && !parseMetadata(a.metadata).diagnostic).map((a) => Number(a.id))),
    [visionAlerts],
  );

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
      await loadDecoderGpuDevices({ quiet: true });
      const result = await request('/api/cameras?limit=100&offset=0');
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
      await loadDecoderGpuDevices({ quiet: true });
      const result = await request('/api/cameras?limit=100&offset=0');
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
    setDecoderGpuDevices(null);
    setUsers([]);
    setNewUser(defaultNewUser);
    setPasswordDrafts({});
    setVisionRules([]);
    setVisionAlerts([]);
    setVisionRuleDraft(defaultVisionRuleDraft());
    setRecordingSegments([]);
    setRecordingConfigs([]);
    setNotifOpen(false);
    setNotifUnread(0);
    setRecordingFocusCameraId(0);
    setRecordingFocusAlertId(0);
    setSeenInRecordingIds(new Set());
    seenVisionAlertIdsRef.current = new Set();
    initialNotifDoneRef.current = false;
    setMessage('');
  }

  async function loadRuntimeSettings() {
    const result = normalizeRuntimeSettings(await request('/api/settings/runtime'));
    setRuntimeSettings(result);
    setSavedRuntimeSettings(result);
    setStreamConfig(result.stream);
    return result;
  }

  async function loadDecoderGpuDevices({ quiet = false } = {}) {
    try {
      const result = await request('/api/settings/runtime/gpu-devices');
      setDecoderGpuDevices(result || null);
      return result;
    } catch (err) {
      setDecoderGpuDevices(null);
      if (!quiet) {
        setMessage(err.message);
      }
      return null;
    }
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
      if (!notifyNew && !initialNotifDoneRef.current) {
        initialNotifDoneRef.current = true;
        const existingUnread = newActiveAlerts.filter((a) => !parseMetadata(a.metadata).diagnostic).length;
        if (existingUnread > 0) setNotifUnread(existingUnread);
      }
      if (notifyNew && newActiveAlerts.length > 0) {
        const realNew = newActiveAlerts.filter((a) => !parseMetadata(a.metadata).diagnostic);
        if (realNew.length > 0) setNotifUnread((n) => n + realNew.length);
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
    if (!authenticated) {
      return undefined;
    }
    loadVision({ quiet: true }).catch(() => {});
    const id = window.setInterval(() => {
      loadVision({ quiet: true, notifyNew: true }).catch(() => {});
    }, 3000);
    return () => window.clearInterval(id);
  }, [authenticated]);

  function openCameraRecording(cameraId, alertId) {
    setRecordingFocusCameraId(Number(cameraId));
    setRecordingFocusAlertId(Number(alertId) || 0);
    setActiveTab('recording');
    loadRecording({ quiet: true }).catch(() => {});
    setSeenInRecordingIds((prev) => {
      const next = new Set(prev);
      visionAlerts
        .filter((a) => Number(a.cameraId) === Number(cameraId) && isActionableVisionAlert(a))
        .forEach((a) => next.add(a.id));
      return next;
    });
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
      setSavedRuntimeSettings(result);
      setStreamConfig(result.stream);
      setRuntimeAutoTune(null);
      setVisionToolStatus(null);
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
      setSavedRuntimeSettings(result);
      setStreamConfig(result.stream);
      setRuntimeAutoTune(null);
      setVisionToolStatus(null);
      setMessage('Settings reset to config defaults.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  function discardRuntimeSettings() {
    setRuntimeSettings(savedRuntimeSettings);
  }

  async function autoTuneRuntimeSettings() {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/runtime/auto-tune', {
        method: 'POST',
      });
      const settings = normalizeRuntimeSettings(result?.settings);
      setRuntimeSettings(settings);
      setStreamConfig(settings.stream);
      setRuntimeAutoTune(result || null);
      setMessage(result?.summary || 'Decoder auto-tune applied.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function loadRecording({ quiet = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const [segsResult, cfgsResult] = await Promise.all([
        request('/api/recording/segments?limit=200&offset=0'),
        request('/api/recording/config'),
      ]);
      setRecordingSegments(Array.isArray(segsResult?.items) ? segsResult.items : Array.isArray(segsResult) ? segsResult : []);
      setRecordingConfigs(Array.isArray(cfgsResult) ? cfgsResult : []);
    } catch (err) {
      if (!quiet) {
        setMessage(err.message);
      }
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function saveRecordingConfig(cfg) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/recording/config', {
        method: 'PUT',
        body: JSON.stringify(cfg),
      });
      // Response is { config, recorderWarning } or (legacy) the config directly.
      const saved = result?.config || result;
      const warning = result?.recorderWarning;
      setRecordingConfigs((current) => {
        const next = current.filter((c) => Number(c.cameraId) !== Number(cfg.cameraId));
        return saved ? [...next, saved] : next;
      });
      if (warning) {
        setMessage(`Config saved. Recorder warning: ${warning}`);
      } else {
        setMessage('Recording config saved.');
      }
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function deleteRecordingSegment(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/recording/segments/${id}`, { method: 'DELETE' });
      setRecordingSegments((current) => current.filter((s) => Number(s.id) !== Number(id)));
      setMessage('Clip deleted.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function checkVisionTool() {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/vision/ai-tool/status');
      setVisionToolStatus(result || null);
      setVisionInstallResult(null);
      setMessage(result?.summary || 'AI tool status checked.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function installVisionPackages() {
    const packages = (visionToolStatus?.installHints || []).filter((h) => !h.manual).map((h) => h.pipName);
    if (packages.length === 0) return;
    setBusy(true);
    setMessage('Installing packages...');
    setVisionInstallResult(null);
    try {
      const result = await request('/api/settings/vision/ai-tool/install', {
        method: 'POST',
        body: JSON.stringify({ packages }),
      });
      setVisionInstallResult(result || null);
      setMessage(result?.success ? 'Install succeeded. Re-checking tool status...' : 'Install finished with errors.');
      if (result?.success) {
        const status = await request('/api/settings/vision/ai-tool/status');
        setVisionToolStatus(status || null);
      }
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
      const payload = {
        ...visionRuleDraft,
        ruleConfig: isLineDetectionType(visionRuleDraft.detectionType)
          ? lineRuleConfigText(parseLineRuleConfig(visionRuleDraft.ruleConfig, visionRuleDraft.detectionType), visionRuleDraft.detectionType)
          : '',
      };
      await request('/api/vision/rules', {
        method: 'POST',
        body: JSON.stringify(payload),
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
      ruleConfig: rule.ruleConfig || (isLineDetectionType(rule.detectionType) ? lineRuleConfigText(defaultLineRuleConfig(rule.detectionType), rule.detectionType) : ''),
      schedulePolicy: rule.schedulePolicy || '',
      threshold: rule.threshold || defaultVisionThreshold,
      minFrames: rule.minFrames || defaultVisionMinFrames,
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
          confidence: Math.max(0.01, Math.min(1, rule.threshold || defaultVisionThreshold)),
          zonePolygon: rule.zonePolygon,
          metadata: JSON.stringify({ source: 'manual-test' }),
        }),
      });
      if (alert?.id) {
        seenVisionAlertIdsRef.current.add(alert.id);
        // Test alerts bypass the poll-based counter path, so increment manually.
        if (!parseMetadata(alert.metadata || '{}').diagnostic) {
          setNotifUnread((n) => n + 1);
        }
      }
      setVisionAlerts((current) => [alert, ...current]);
      if (rule.soundEnabled) {
        playAlertSound();
      }
      setMessage('Test alert created. Navigating to Recording…');
      if (alert?.id && rule.cameraId) {
        openCameraRecording(rule.cameraId, alert.id);
      }
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
      const target = visionAlerts.find((a) => Number(a.id) === Number(id));
      const wasCountable = target && !target.isAcknowledged && !parseMetadata(target.metadata || '{}').diagnostic;
      await request(`/api/vision/alerts/${id}/ack`, { method: 'POST' });
      await loadVision({ quiet: true });
      if (wasCountable) setNotifUnread((n) => Math.max(0, n - 1));
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
      rtspUrl: device.rtspUrl || '',
      profileToken: device.profileToken || '',
      rtspStatus: device.rtspStatus || '',
      rtspTracks: device.rtspTracks || '',
    };
  }

  function enrichTilesWithDevices(tiles, devices) {
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    return tiles.map((tile) => {
      const device = devicesById.get(Number(tile.id));
      if (!device) {
        return tile;
      }
      return {
        ...tile,
        title: cameraTitle(device),
        ptzSupported: Boolean(device.ptzSupported),
        rtspUrl: device.rtspUrl || tile.rtspUrl || '',
        profileToken: device.profileToken || tile.profileToken || '',
        rtspStatus: device.rtspStatus || tile.rtspStatus || '',
        rtspTracks: device.rtspTracks || tile.rtspTracks || '',
      };
    });
  }

  function applyDeviceUpdate(device) {
    if (!device?.id) {
      return;
    }
    setSaved((current) => current.map((item) => (Number(item.id) === Number(device.id) ? { ...item, ...device } : item)));
    setViewTilesWithCookie((current) => enrichTilesWithDevices(current, [device]));
    setPreview((current) => {
      if (!current || Number(current.id) !== Number(device.id)) {
        return current;
      }
      const nextDevice = { ...(current.device || {}), ...device };
      return {
        ...current,
        title: cameraTitle(nextDevice),
        device: nextDevice,
        ptzSupported: Boolean(nextDevice.ptzSupported),
      };
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

  async function scan(protocol, cidr) {
    setBusy(true);
    setMessage('');
    const cidrParam = (cidr || '').trim();
    try {
      let devices = [];

      if (protocol === 'all') {
        // Run ONVIF and multi-protocol scan concurrently.
        const ms = Number(timeoutMs) || 5000;
        const scanBody = { timeoutMs: ms };
        if (cidrParam) scanBody.cidr = cidrParam;
        const [onvifSettled, scanSettled] = await Promise.allSettled([
          request('/api/onvif/discover', { method: 'POST', body: JSON.stringify({ timeoutMs: ms }) }),
          request('/api/onvif/scan', { method: 'POST', body: JSON.stringify(scanBody) }),
        ]);
        const onvifDevices = (onvifSettled.status === 'fulfilled' && Array.isArray(onvifSettled.value)
          ? onvifSettled.value : []).map((d) => ({ ...d, _discoveryMethods: ['onvif'] }));
        const scanDevices = (scanSettled.status === 'fulfilled' && Array.isArray(scanSettled.value)
          ? scanSettled.value : []).map(normalizeScanDevice);
        // Merge: keep ONVIF results, add scan-only devices by IP.
        const onvifHosts = new Set(onvifDevices.map((d) => d.host));
        devices = [...onvifDevices, ...scanDevices.filter((d) => !onvifHosts.has(d.host))];
      } else if (protocol === 'onvif') {
        const result = await request('/api/onvif/discover', {
          method: 'POST',
          body: JSON.stringify({ timeoutMs: Number(timeoutMs) || 3000 }),
        });
        devices = (Array.isArray(result) ? result : []).map((d) => ({ ...d, _discoveryMethods: ['onvif'] }));
      } else {
        // Single non-ONVIF protocol: ssdp | mdns | sadp | portscan
        const body = { timeoutMs: Number(timeoutMs) || 5000, methods: [protocol] };
        if (cidrParam) body.cidr = cidrParam;
        const result = await request('/api/onvif/scan', { method: 'POST', body: JSON.stringify(body) });
        devices = (Array.isArray(result) ? result : []).map(normalizeScanDevice);
      }

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
      const label = { all: 'all methods', onvif: 'ONVIF', ssdp: 'SSDP/UPnP', mdns: 'mDNS', sadp: 'SADP', portscan: 'port scan' }[protocol] || protocol;
      setMessage(`${devices.length} device(s) found via ${label}: ${newCount} not saved, ${savedCount} saved.`);
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
    // Strip frontend-only fields that the backend struct doesn't accept.
    const { _discoveryMethods, _openPorts, ...deviceData } = device;
    try {
      await request('/api/cameras/discovered', {
        method: 'POST',
        body: JSON.stringify({
          ...deviceData,
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
      await request(`/api/cameras/${device.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: (draft.name || '').trim() || cameraTitle(device),
          description: (draft.description || '').trim(),
        }),
      });
      setDeviceDrafts((current) => ({ ...current, [device.id]: null }));
      setMessage('Camera details saved.');
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  function discardDeviceDetails(id) {
    setDeviceDrafts((current) => ({ ...current, [id]: null }));
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
      const nextDevice = { ...device, ...result };
      return {
        ...tileFromDevice(nextDevice),
        title: cameraTitle(nextDevice),
        ptzSupported: Boolean(nextDevice.ptzSupported),
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
      const result = await request(`/api/cameras/${device.id}/credentials`, {
        method: 'POST',
        body: JSON.stringify(cameraCredentials),
      });
      if (!quiet) {
        setDeviceCredentials((current) => ({ ...current, [device.id]: null }));
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
      const result = await request(`/api/cameras/${device.id}/camera-password`, {
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
      await request(`/api/cameras/${deviceId}/ptz/move`, {
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
      await request(`/api/cameras/${deviceId}/ptz/stop`, { method: 'POST' });
    } catch (err) {
      setMessage(err.message);
    }
  }

  async function resolveStream(device) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request(`/api/cameras/${device.id}/stream-options`, {
        method: 'POST',
        body: JSON.stringify(credentialsFor(device)),
      });
      setStreamOptionsById((current) => ({ ...current, [device.id]: result }));
      const selectedToken =
        result?.selectedProfileToken || result?.preferredProfileToken || (result?.options || [])[0]?.profileToken || '';
      if (selectedToken) {
        setSelectedStreamTokens((current) => ({ ...current, [device.id]: selectedToken }));
      }
      setMessage(`${(result?.options || []).length} RTSP stream option(s) found.`);
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
    }
  }

  async function selectStreamOption(device, option) {
    if (!option?.profileToken) {
      setMessage('Choose an ONVIF stream first.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      const result = await request(`/api/cameras/${device.id}/stream-uri`, {
        method: 'POST',
        body: JSON.stringify({
          ...credentialsFor(device),
          profileToken: option.profileToken,
          rtspUrl: option.rtspUrl || '',
        }),
      });
      setSelectedStreamTokens((current) => ({ ...current, [device.id]: option.profileToken }));
      setStreamOptionsById((current) => {
        const existing = current[device.id];
        if (!existing?.options) {
          return current;
        }
        return {
          ...current,
          [device.id]: {
            ...existing,
            selectedProfileToken: option.profileToken,
            options: existing.options.map((item) => ({
              ...item,
              selected: item.profileToken === option.profileToken,
            })),
          },
        };
      });
      applyDeviceUpdate(result);

      // Auto-enable recording when a stream is first selected or recording was disabled.
      const existingConfig = recordingConfigs.find((c) => Number(c.cameraId) === Number(device.id));
      if (!existingConfig?.enabled) {
        const streamUrl = result?.rtspUrl || option.rtspUrl || '';
        const configToSave = {
          cameraId: device.id,
          enabled: true,
          preRollSec:       existingConfig?.preRollSec       ?? 30,
          postRollSec:      existingConfig?.postRollSec      ?? 10,
          storagePath:      existingConfig?.storagePath      ?? 'recordings',
          retentionDays:    existingConfig?.retentionDays    ?? 7,
          segmentMinutes:   existingConfig?.segmentMinutes   ?? 15,
          liveStreamUrl:    existingConfig?.liveStreamUrl    ?? '',
          streamUrl,
          fallbackStreamUrl: existingConfig?.fallbackStreamUrl ?? '',
        };
        try {
          const recResult = await request('/api/recording/config', {
            method: 'PUT',
            body: JSON.stringify(configToSave),
          });
          const savedCfg = recResult?.config || recResult;
          setRecordingConfigs((current) => {
            const rest = current.filter((c) => Number(c.cameraId) !== Number(device.id));
            return savedCfg ? [...rest, savedCfg] : rest;
          });
          setMessage(`${streamOptionLabel(option)} saved. Recording enabled automatically.`);
        } catch (_) {
          setMessage(`${streamOptionLabel(option)} saved. Recording auto-enable failed — configure it in the Recording tab.`);
        }
      } else {
        setMessage(`${streamOptionLabel(option)} saved.`);
      }

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
      const result = await request(`/api/cameras/${device.id}/rtsp-test`, { method: 'POST' });
      const tracks = result.tracks || [];
      const suffix = tracks.length && !hasH264VideoTrack(tracks)
        ? ' No H264 video track; live view will use MJPEG fallback.'
        : '';
      setMessage(`RTSP online: ${tracks.length} track(s).${suffix}`);
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
    const result = await request(`/api/cameras/${device.id}/live-view`, {
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
          ...tileFromDevice({ ...device, ...result }),
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
      await request(`/api/cameras/${id}`, { method: 'DELETE' });
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
      <div>
        <style>{styles}</style>
        <LoginPage
          credentials={credentials}
          busy={busy}
          message={message}
          onChange={setCredentials}
          onSubmit={login}
        />
      </div>
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
          if (tab === 'recording') {
            loadRecording({ quiet: true }).catch(() => {});
          }
        }}
        onRefresh={() => refresh()}
        onLogout={logout}
        alerts={visionAlerts}
        savedDevices={saved}
        notifOpen={notifOpen}
        notifUnread={notifUnread}
        onNotifToggle={() => { setNotifOpen((o) => !o); setNotifUnread(0); }}
        onNotifClick={(cameraId, alertId) => { setNotifOpen(false); openCameraRecording(cameraId, alertId); }}
        theme={theme}
        onThemeChange={changeTheme}
      />
      <Message value={message} />

      {activeTab === 'views' ? (
        <ViewsTab
          devices={saved}
          layout={viewLayout}
          viewTiles={viewTiles}
          alertsByCamera={tileAlertsByCamera}
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
          onOpenAlerts={openCameraRecording}
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
          streamOptionsById={streamOptionsById}
          selectedStreamTokens={selectedStreamTokens}
          saveDrafts={saveDrafts}
          onCameraNav={setCameraNav}
          onManualAddress={setManualAddress}
          onTimeout={setTimeoutMs}
          onScan={scan}
          scanCIDR={scanCIDR}
          onScanCIDR={setScanCIDR}
          onProbe={probe}
          onSave={save}
          onSaveDraft={(key, value) => setSaveDrafts((current) => ({ ...current, [key]: value }))}
          onDetailDraft={(id, value) => setDeviceDrafts((current) => ({ ...current, [id]: value }))}
          onSaveDetails={saveDeviceDetails}
          onDiscardDetails={discardDeviceDetails}
          onCredential={(id, value) => setDeviceCredentials((current) => ({ ...current, [id]: value }))}
          onPasswordDraft={(id, value) => setCameraPasswordDrafts((current) => ({ ...current, [id]: value }))}
          onSaveCredentials={saveCredentials}
          onChangePassword={changeCameraPassword}
          onResolve={resolveStream}
          onStreamToken={(id, token) => setSelectedStreamTokens((current) => ({ ...current, [id]: token }))}
          onSelectStream={selectStreamOption}
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
          hasChanges={JSON.stringify(runtimeSettings) !== JSON.stringify(savedRuntimeSettings)}
          onChange={setRuntimeSettings}
          onSettingsNav={openSettingsSection}
          onSave={saveRuntimeSettings}
          onDiscard={discardRuntimeSettings}
          onReset={resetRuntimeSettings}
          onAutoTune={autoTuneRuntimeSettings}
          autoTuneResult={runtimeAutoTune}
          gpuDevices={decoderGpuDevices}
          onCheckVisionTool={checkVisionTool}
          visionToolStatus={visionToolStatus}
          onInstallPackages={installVisionPackages}
          visionInstallResult={visionInstallResult}
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

      {activeTab === 'recording' ? (
        <RecordingTab
          saved={saved}
          segments={recordingSegments}
          configs={recordingConfigs}
          busy={busy}
          authHeader={authHeader}
          onSaveConfig={saveRecordingConfig}
          onDeleteSegment={deleteRecordingSegment}
          onReload={() => loadRecording()}
          focusCameraId={recordingFocusCameraId}
          focusAlertId={recordingFocusAlertId}
          unacknowledgedAlertIds={unacknowledgedAlertIds}
          onAcknowledgeAlert={acknowledgeAlert}
          alerts={visionAlerts}
        />
      ) : null}
    </main>
  );
}

const styles = `
:root, .theme-light {
  --bg-body: #f4f6f8;
  --bg-surface: #ffffff;
  --bg-subtle: #fbfcfd;
  --bg-subtle2: #f7f9fb;
  --bg-video: #111923;
  --text-primary: #18212f;
  --text-secondary: #233044;
  --text-muted: #59687a;
  --text-faint: #6a7888;
  --text-link: #3d6bcc;
  --accent: #2d6cdf;
  --accent-bg: #edf4ff;
  --accent-badge: #e4ebf8;
  --border-soft: #c7d1dc;
  --border-panel: #d6dee7;
  --border-divider: #dce3ea;
  --border-subtle: #e5ebf1;
  --danger: #d23f3f;
  --danger-text: #9f3434;
  --danger-bg: #fde7e7;
  --ok-text: #247455;
  --ok-bg: #dff2ea;
  --warn-border: #d28d1f;
  --warn-bg: #fff8e9;
  --warn-text: #64450d;
  --status-neutral-bg: #eef1f4;
  --shadow-panel: rgba(32, 42, 54, 0.08);
  color-scheme: light;
}

.theme-dark {
  --bg-body: #0d1117;
  --bg-surface: #161c26;
  --bg-subtle: #1a2231;
  --bg-subtle2: #1e2636;
  --bg-video: #060a0e;
  --text-primary: #e2eaf4;
  --text-secondary: #c0cede;
  --text-muted: #8a9fb8;
  --text-faint: #7a8ea4;
  --text-link: #6fa3e0;
  --accent: #4a84e8;
  --accent-bg: #1a2d4a;
  --accent-badge: #1e3050;
  --border-soft: #2a3850;
  --border-panel: #243044;
  --border-divider: #202c3e;
  --border-subtle: #1c2838;
  --danger: #e05252;
  --danger-text: #cc7878;
  --danger-bg: #2a1818;
  --ok-text: #44aa80;
  --ok-bg: #122a20;
  --warn-border: #c9882a;
  --warn-bg: #1e1608;
  --warn-text: #d4952a;
  --status-neutral-bg: #1c2434;
  --shadow-panel: rgba(0, 0, 0, 0.3);
  color-scheme: dark;
}

.theme-slate {
  --bg-body: #e6eaef;
  --bg-surface: #f0f4f8;
  --bg-subtle: #eaeef3;
  --bg-subtle2: #e4e8ed;
  --bg-video: #0f1620;
  --text-primary: #1e2a38;
  --text-secondary: #2a3850;
  --text-muted: #4a5e72;
  --text-faint: #5c6e82;
  --text-link: #3a5e8a;
  --accent: #3a6298;
  --accent-bg: #dce8f8;
  --accent-badge: #ccdaf0;
  --border-soft: #b0c0d4;
  --border-panel: #bccad8;
  --border-divider: #c4d0de;
  --border-subtle: #ccd6e2;
  --danger: #c43030;
  --danger-text: #8a2828;
  --danger-bg: #f0dede;
  --ok-text: #246448;
  --ok-bg: #d0ede2;
  --warn-border: #b87a14;
  --warn-bg: #f8f0d8;
  --warn-text: #5a3e08;
  --status-neutral-bg: #dce4ee;
  --shadow-panel: rgba(20, 36, 52, 0.1);
  color-scheme: light;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  font-family: Inter, Segoe UI, Arial, sans-serif;
  color: var(--text-primary);
  background: var(--bg-body);
}

button,
input,
select,
textarea {
  font: inherit;
}

button {
  min-height: 38px;
  border: 1px solid var(--accent);
  border-radius: 6px;
  background: var(--accent);
  color: var(--bg-surface);
  padding: 0 14px;
  cursor: pointer;
  white-space: nowrap;
}

button.quiet {
  border-color: var(--border-soft);
  background: var(--bg-surface);
  color: var(--text-secondary);
}

button.active {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--bg-surface);
}

button.danger-text {
  color: var(--danger-text);
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
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  background: var(--bg-surface);
  color: var(--text-primary);
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
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 650;
}

.label-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.label-row > span:first-child {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.info-button {
  width: 20px;
  min-height: 20px;
  flex: 0 0 auto;
  border-color: var(--border-soft);
  border-radius: 999px;
  background: var(--bg-surface);
  color: var(--text-muted);
  padding: 0;
  font-size: 12px;
  font-weight: 800;
  line-height: 1;
}

.info-button:hover {
  border-color: var(--accent);
  color: var(--accent);
}

.login-screen {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}

.login-panel {
  position: relative;
  width: min(100%, 380px);
  display: grid;
  gap: 16px;
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 24px;
  box-shadow: 0 12px 30px var(--shadow-panel);
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
  color: var(--text-faint);
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
  border-bottom: 1px solid var(--border-divider);
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
  border-left: 4px solid var(--warn-border);
  background: var(--warn-bg);
  color: var(--warn-text);
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
  color: var(--text-faint);
  font-size: 13px;
}

.add-strip {
  justify-content: end;
}

.add-strip span {
  color: var(--text-faint);
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
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--bg-video);
  overflow: hidden;
}

.view-tile[draggable='true'] {
  cursor: move;
}

.view-tile.dragging {
  opacity: 0.58;
  outline: 2px solid var(--accent);
}

.view-tile.has-ai-alert {
  border-color: var(--danger);
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
  background: var(--bg-video);
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
  background: var(--bg-video);
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

.audio-mute-btn {
  position: absolute;
  left: 8px;
  bottom: 36px;
  z-index: 2;
  background: rgba(15, 23, 33, 0.72);
  color: #ffffff;
  border: none;
  border-radius: 50%;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  padding: 0;
  transition: background 0.15s;
}

.audio-mute-btn:hover {
  background: rgba(15, 23, 33, 0.92);
}

.ptz-ring-overlay {
  position: absolute;
  right: 8px;
  bottom: 8px;
  z-index: 2;
}

.preview-viewport .ptz-ring-overlay {
  right: 14px;
  bottom: 14px;
}

.ptz-ring {
  display: block;
  color: rgba(255, 255, 255, 0.88);
  filter: drop-shadow(0 1px 6px rgba(0, 0, 0, 0.72));
}

.ptz-sector {
  fill: transparent;
  cursor: pointer;
  outline: none;
  transition: fill 0.1s;
}

.ptz-sector:hover {
  fill: rgba(255, 255, 255, 0.14);
}

.ptz-sector:focus-visible {
  fill: rgba(255, 255, 255, 0.18);
}

.ptz-sector-busy {
  pointer-events: none;
}

.ptz-ring-busy {
  opacity: 0.45;
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
  border-color: var(--danger);
  background: var(--danger);
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
  color: var(--text-muted);
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
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
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
  background: var(--accent-badge);
  color: var(--accent);
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
  background: var(--bg-surface);
  color: var(--text-secondary);
  padding: 8px 10px;
  text-align: left;
}

.saved-device-button:hover,
.saved-device-button.active {
  border-color: var(--accent);
  background: var(--accent-bg);
}

.saved-device-button strong,
.saved-device-button span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.saved-device-button span {
  color: var(--text-faint);
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
  border-bottom: 1px solid var(--border-subtle);
  padding-bottom: 10px;
}

.saved-detail-tabs button {
  min-height: 34px;
}

.saved-tab-panel {
  position: relative;
  display: grid;
  gap: 12px;
}

.probe-panel {
  display: grid;
  gap: 14px;
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
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

.scan-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px;
}

.scan-protocol-label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
  color: var(--text-muted);
}

.scan-protocol-select {
  height: 36px;
  padding: 0 8px;
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  background: var(--bg-base);
  color: var(--text-base);
  font-size: 14px;
  cursor: pointer;
  min-width: 160px;
}

.scan-cidr-input {
  height: 36px;
  width: 150px;
}

.scan-label-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.credential-row,
.metadata-row {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.preview-panel {
  display: grid;
  gap: 10px;
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
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
  color: var(--text-faint);
  font-size: 13px;
}

.preview-viewport {
  position: relative;
  width: min(100%, 860px);
  overflow: hidden;
  border-radius: 8px;
}

.preview-panel .live-frame {
  width: 100%;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--border-soft);
}

.preview-actions {
  display: grid;
  gap: 10px;
}

.device-section > header {
  min-height: 38px;
  align-items: center;
}

.device-section header span {
  min-width: 30px;
  text-align: center;
  border-radius: 999px;
  background: var(--accent-badge);
  color: var(--accent);
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
  background: var(--status-neutral-bg);
  color: var(--text-muted);
  padding: 3px 8px;
  font-weight: 720;
}

.compact-button {
  min-height: 30px;
  padding: 0 10px;
  font-size: 13px;
}

.discovery-method-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-bottom: 6px;
}

.discovery-method-badge {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--accent-badge);
  color: var(--accent);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.discovery-ports {
  font-size: 11px;
  color: var(--text-faint);
  padding: 2px 4px;
}

.empty {
  margin: 0;
  color: var(--text-faint);
}

.device-card {
  display: grid;
  gap: 12px;
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 14px;
}

.device-card h3 {
  margin: 0;
  font-size: 16px;
}

.device-card p {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  color: var(--text-faint);
  font-size: 13px;
}

.device-card .device-description {
  margin: 0;
  color: #3e4a59;
}

.device-edit-form {
  position: relative;
  display: grid;
  gap: 10px;
}

.field-hint {
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
}

.field-hint.good {
  color: var(--ok-text);
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

.stream-option-panel {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(0, 1.4fr) auto;
  gap: 10px;
  align-items: end;
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
  background: var(--bg-subtle);
  padding: 12px;
}

.stream-option-uri {
  min-height: 38px;
  overflow-wrap: anywhere;
  border: 1px solid var(--border-panel);
  border-radius: 6px;
  background: var(--bg-surface);
  padding: 9px 10px;
  color: var(--text-muted);
  font-size: 13px;
}

.stream-action-flow {
  display: grid;
  grid-template-columns: repeat(4, minmax(120px, 1fr));
  gap: 8px;
}

dt {
  color: var(--text-faint);
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
  background: var(--status-neutral-bg);
  color: var(--text-muted);
  padding: 5px 9px;
  font-size: 12px;
  line-height: 1;
}

.status-pill.online {
  background: var(--ok-bg);
  color: var(--ok-text);
}

.status-pill.offline {
  background: var(--danger-bg);
  color: var(--danger-text);
}

.status-pill.resolved {
  background: var(--accent-badge);
  color: var(--accent);
}

.status-pill.saved {
  background: var(--ok-bg);
  color: var(--ok-text);
}

.track-list {
  display: grid;
  gap: 4px;
  margin: 0;
  padding-left: 18px;
}

.settings-layout {
  position: relative;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.settings-field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  align-items: end;
}

.auto-tune-result {
  display: grid;
  gap: 8px;
  border: 1px solid var(--border-soft);
  border-left: 4px solid var(--accent);
  border-radius: 6px;
  background: var(--bg-subtle2);
  padding: 10px 12px;
  color: var(--text-secondary);
  font-size: 13px;
}

.auto-tune-result ul {
  display: grid;
  gap: 4px;
  margin: 0;
  padding-left: 18px;
  color: var(--text-muted);
}

.tool-status-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}

.tool-status-grid > div {
  min-width: 0;
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  background: var(--bg-surface);
  padding: 8px 10px;
}

.tool-status-grid dd {
  overflow-wrap: anywhere;
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
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
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
  position: relative;
  display: grid;
  gap: 14px;
  border-top: 1px solid var(--border-subtle);
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
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
  background: var(--bg-subtle);
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
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 720;
}

.schedule-days {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 8px;
}

.line-class-any {
  grid-column: 1 / -1;
  padding: 4px 6px;
  border-radius: 6px;
  background: var(--accent-bg);
  color: var(--accent);
  border: 1px solid var(--accent-badge);
}

.schedule-days .check-row {
  justify-content: center;
  min-height: 38px;
  border: 1px solid var(--border-panel);
  border-radius: 6px;
  background: var(--bg-surface);
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
  border-top: 1px solid var(--border-subtle);
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
  color: var(--text-faint);
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
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  background: var(--bg-subtle);
  padding: 8px 10px;
}

.event-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
}

.event-table {
  width: 100%;
  min-width: 760px;
  border-collapse: collapse;
  background: var(--bg-surface);
}

.event-table th,
.event-table td {
  border-bottom: 1px solid var(--border-subtle);
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}

.event-table th {
  color: var(--text-muted);
  background: var(--bg-subtle2);
  font-size: 12px;
  font-weight: 760;
}

.event-table tbody tr:last-child td {
  border-bottom: 0;
}

.event-table tbody tr.selected td {
  background: var(--accent-bg);
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
  color: var(--text-faint);
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
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  background: var(--bg-surface);
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
  color: var(--text-faint);
  font-size: 13px;
}

.event-details {
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  background: var(--bg-subtle);
  padding: 8px 10px;
}

.event-details strong {
  display: block;
  color: var(--text-muted);
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

.conf-value {
  font-size: 13px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.conf-bar-wrap {
  margin-top: 5px;
  height: 5px;
  border-radius: 3px;
  background: var(--border-subtle);
  overflow: hidden;
}

.conf-bar {
  height: 100%;
  border-radius: 3px;
  transition: width 0.2s;
}

.bb-preview-wrap {
  margin-top: 8px;
}

.bb-frame {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  background: var(--bg-subtle2);
  border: 1px solid var(--border-subtle);
  border-radius: 4px;
  overflow: hidden;
}

.bb-box {
  position: absolute;
  border: 2px solid var(--accent);
  border-radius: 2px;
  background: color-mix(in srgb, var(--accent) 15%, transparent);
  box-sizing: border-box;
}

.bb-coords {
  margin: 6px 0 0;
  font: 12px/1.4 Consolas, Menlo, monospace;
  color: var(--text-faint);
}

.bb-coords-inline {
  font: 12px/1.4 Consolas, Menlo, monospace;
}

.alert-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 16px;
}

.alert-modal-dialog {
  background: var(--bg-surface);
  border-radius: 10px;
  overflow: hidden;
  max-width: min(840px, 100%);
  width: 100%;
  max-height: 92vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 24px 72px rgba(0, 0, 0, 0.55);
}

.alert-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: var(--bg-subtle2);
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
}

.alert-modal-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
  flex-wrap: wrap;
}

.alert-modal-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.alert-modal-time {
  font-size: 12px;
  color: var(--text-faint);
}

.alert-modal-close {
  flex-shrink: 0;
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  padding: 2px 8px;
  border-radius: 4px;
}

.alert-modal-close:hover {
  background: var(--bg-subtle);
  color: var(--text-primary);
}

.alert-modal-image-wrap {
  background: #000;
  flex-shrink: 0;
  max-height: 55vh;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.alert-modal-snap-container {
  position: relative;
  display: inline-block;
  max-width: 100%;
  max-height: 55vh;
  line-height: 0;
}

.alert-modal-snap {
  display: block;
  max-width: 100%;
  max-height: 55vh;
  object-fit: contain;
}

.alert-modal-bb {
  position: absolute;
  border: 2px solid #00e5ff;
  border-radius: 2px;
  box-sizing: border-box;
  pointer-events: none;
}

.alert-modal-bb-label {
  position: absolute;
  top: -22px;
  left: -1px;
  background: #00e5ff;
  color: #000;
  font: 700 11px/20px system-ui, sans-serif;
  padding: 0 5px;
  border-radius: 3px 3px 0 0;
  text-transform: capitalize;
  white-space: nowrap;
}

.alert-modal-snap-msg {
  color: #6688aa;
  font-size: 13px;
  padding: 20px;
}

.alert-modal-snap-none {
  color: var(--text-faint);
}

.alert-modal-meta {
  padding: 14px;
  overflow-y: auto;
  flex: 1;
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
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--bg-video);
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
  fill: rgba(45, 108, 223, 0.22);
  stroke: #ffffff;
  stroke-width: 0.7;
}

.zone-line {
  fill: none;
  stroke: #ffffff;
  stroke-width: 0.7;
  stroke-dasharray: 2 1.5;
}

.crossing-line {
  fill: none;
  stroke: #f5d742;
  stroke-width: 1.1;
}

.crossing-label {
  fill: #ffffff;
  paint-order: stroke;
  stroke: rgba(15, 23, 33, 0.86);
  stroke-width: 3px;
  font-size: 7px;
  font-weight: 800;
  text-anchor: middle;
  dominant-baseline: central;
}

.zone-point {
  fill: var(--accent);
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
  color: var(--text-muted);
  font-weight: 750;
}

.check-row {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--text-primary);
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
  border-top: 1px solid var(--border-subtle);
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
  border-top: 1px solid var(--border-subtle);
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
  .settings-field-grid,
  .ice-row,
  .user-create-row,
  .user-row,
  .vision-row,
  .event-grid,
  .tool-status-grid,
  .scan-row,
  .probe-row,
  .credential-row,
  .metadata-row,
  .stream-option-panel,
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

.recording-layout {
  display: grid;
  gap: 14px;
}

.recording-config-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.field-label {
  display: grid;
  gap: 6px;
  font-size: 14px;
}

.field-label span {
  font-weight: 600;
  color: var(--text-secondary);
}

.segment-list {
  display: grid;
  gap: 0;
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  overflow: hidden;
}

.segment-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-surface);
}

.segment-row:last-child {
  border-bottom: none;
}

.segment-row:nth-child(even) {
  background: var(--bg-subtle);
}

.segment-info {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.segment-filename {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.segment-meta {
  font-size: 12px;
  color: var(--text-faint);
}

.segment-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.segment-actions button {
  min-height: 32px;
  padding: 0 10px;
  font-size: 13px;
}

.empty-hint {
  color: var(--text-faint);
  font-size: 14px;
  margin: 0;
}

.segment-row.focused {
  background: #fffae5;
  border-color: #d28d1f;
  box-shadow: 0 0 0 2px #f5c842 inset;
}

.recording-pending {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  background: #f0f7ff;
  border: 1px solid #b8d4f0;
  border-radius: 6px;
  margin-bottom: 10px;
  font-size: 13px;
  color: #1a4a80;
}

.recording-pending--timeout {
  background: #fff8e9;
  border-color: #f0c872;
  color: #7a4e00;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.recording-pending-dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #2277cc;
  animation: pulse 1.4s ease-in-out infinite;
}

.segment-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.segment-unreviewed {
  flex-shrink: 0;
  padding: 2px 7px;
  border-radius: 10px;
  background: #fff0e0;
  color: #b85c00;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.02em;
  border: 1px solid #f0c090;
}

.segment-event-label {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: 10px;
  background: #1e3a5f;
  color: #7ab8f5;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  border: 1px solid #2d5488;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.timeline-wrap {
  margin: 8px 0 4px;
  display: flex;
  flex-direction: column;
  gap: 0;
}

.timeline-hour-labels {
  position: relative;
  height: 16px;
  margin-bottom: 2px;
  user-select: none;
}

.timeline-hour-label {
  position: absolute;
  transform: translateX(-50%);
  font-size: 10px;
  color: #7a8b9a;
  font-weight: 500;
  white-space: nowrap;
}

.timeline-bar {
  position: relative;
  height: 40px;
  background: #1a2030;
  border: 1px solid #2a3448;
  border-radius: 5px;
  cursor: crosshair;
  overflow: hidden;
  user-select: none;
}

.timeline-tick {
  position: absolute;
  top: 0;
  width: 1px;
  height: 6px;
  background: #2d3d52;
}

.timeline-tick--major {
  height: 100%;
  background: rgba(255,255,255,0.04);
  width: 1px;
}

.timeline-segment {
  position: absolute;
  top: 4px;
  height: 14px;
  border-radius: 2px;
  pointer-events: none;
}

.timeline-segment--cont {
  background: #2563eb;
  opacity: 0.85;
  top: 4px;
}

.timeline-segment--event {
  background: #dc2626;
  opacity: 0.9;
  top: 22px;
  height: 12px;
}

.timeline-hover-line {
  position: absolute;
  top: 0;
  width: 1px;
  height: 100%;
  background: rgba(255,255,255,0.35);
  pointer-events: none;
}

.timeline-cursor-line {
  position: absolute;
  top: 0;
  width: 2px;
  height: 100%;
  background: #f59e0b;
  pointer-events: none;
  box-shadow: 0 0 6px rgba(245,158,11,0.7);
}

.timeline-time-label {
  position: absolute;
  top: -18px;
  left: 4px;
  background: rgba(30,40,56,0.92);
  color: #c8d8e8;
  font-size: 11px;
  padding: 2px 5px;
  border-radius: 3px;
  white-space: nowrap;
  pointer-events: none;
}

.timeline-time-label--selected {
  background: #92400e;
  color: #fcd34d;
  top: -18px;
}

.timeline-legend {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-top: 5px;
  flex-wrap: wrap;
}

.timeline-legend-item {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: #7a8b9a;
}

.timeline-legend-item::before {
  content: '';
  display: inline-block;
  width: 20px;
  height: 8px;
  border-radius: 2px;
}

.timeline-legend-item--cont::before { background: #2563eb; }
.timeline-legend-item--event::before { background: #dc2626; }

.segment-row.timeline-highlighted {
  background: #f0f6ff;
  border-left: 3px solid #2563eb;
}

.segment-row.timeline-highlighted:nth-child(even) {
  background: #e8f2ff;
}

.segment-thumb-btn {
  flex-shrink: 0;
  width: 60px;
  height: 46px;
  background: #1a2030;
  border: 1px solid #2a3448;
  border-radius: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: #6688aa;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.segment-thumb-btn:hover {
  background: #2b4a8c;
  color: #ffffff;
  border-color: #3d6bcc;
}

.video-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.82);
  z-index: 300;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}

.video-dialog {
  background: #1a2030;
  border-radius: 10px;
  overflow: hidden;
  max-width: min(900px, 100%);
  width: 100%;
  display: flex;
  flex-direction: column;
  box-shadow: 0 24px 72px rgba(0, 0, 0, 0.65);
}

.video-dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: #242c3e;
  border-bottom: 1px solid #2e3a52;
}

.video-dialog-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.video-dialog-title {
  font-size: 13px;
  font-weight: 600;
  color: #c8d8e8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-right: 12px;
}

.video-dialog-close {
  flex-shrink: 0;
  background: none;
  border: none;
  color: #6688aa;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  padding: 2px 8px;
  border-radius: 4px;
}

.video-dialog-close:hover {
  background: #3a4a60;
  color: #ffffff;
}

.video-dialog-body {
  background: #000000;
  min-height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.video-player {
  width: 100%;
  max-height: 72vh;
  display: block;
}

.video-loading-msg {
  color: #6688aa;
  font-size: 14px;
}

.video-dialog-meta {
  padding: 8px 16px;
  font-size: 12px;
  color: #6688aa;
  background: #1e2638;
  border-top: 1px solid #2e3a52;
}

.notif-wrap {
  position: relative;
}

.notif-btn {
  position: relative;
}

.notif-badge {
  position: absolute;
  top: -5px;
  right: -6px;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  border-radius: 9px;
  background: #e53935;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 17px;
  text-align: center;
  pointer-events: none;
  opacity: 0;
  transform: scale(0.6);
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.notif-badge--visible {
  opacity: 1;
  transform: scale(1);
}

.notif-panel {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 300px;
  background: var(--bg-surface);
  border: 1px solid var(--border-divider);
  border-radius: 8px;
  box-shadow: 0 8px 24px var(--shadow-panel);
  z-index: 200;
  overflow: hidden;
  max-height: 400px;
  overflow-y: auto;
}

.notif-panel-header {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-divider);
  font-weight: 600;
  font-size: 13px;
  position: sticky;
  top: 0;
  background: var(--bg-surface);
}

.notif-empty {
  padding: 14px;
  color: var(--text-faint);
  font-size: 13px;
  margin: 0;
}

.notif-item {
  display: grid;
  width: 100%;
  text-align: left;
  padding: 9px 14px;
  border: none;
  border-bottom: 1px solid var(--border-subtle);
  background: none;
  cursor: pointer;
  gap: 2px;
  color: var(--text-primary);
}

.notif-item:last-child {
  border-bottom: none;
}

.notif-item:hover {
  background: var(--bg-body);
}

.notif-label {
  font-weight: 600;
  font-size: 13px;
  text-transform: capitalize;
  color: var(--text-primary);
}

.notif-camera {
  font-size: 12px;
  color: var(--text-link);
}

.notif-time {
  font-size: 11px;
  color: var(--text-faint);
}

@media (max-width: 980px) {
  .recording-config-grid {
    grid-template-columns: 1fr;
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

button .btn-icon {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.theme-drop-wrap {
  position: relative;
}

.theme-toggle {
  min-height: 34px;
  padding: 0 10px;
  font-size: 12px;
  gap: 5px;
  display: inline-flex;
  align-items: center;
  border-color: var(--border-soft);
  background: var(--bg-surface);
  color: var(--text-muted);
}

.theme-toggle:hover,
.theme-toggle.active {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-bg);
}

.theme-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 140px;
  background: var(--bg-surface);
  border: 1px solid var(--border-panel);
  border-radius: 8px;
  box-shadow: 0 8px 24px var(--shadow-panel);
  z-index: 300;
  overflow: hidden;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.theme-menu-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 10px;
  border: 1px solid transparent;
  background: none;
  color: var(--text-secondary);
  border-radius: 6px;
  text-align: left;
  font-size: 13px;
  cursor: pointer;
}

.theme-menu-item:hover {
  background: var(--accent-bg);
  color: var(--accent);
}

.theme-menu-item.active {
  background: var(--accent-badge);
  color: var(--accent);
  font-weight: 500;
}

.form-busy-overlay {
  position: absolute;
  inset: 0;
  background: color-mix(in srgb, var(--bg-body) 75%, transparent);
  backdrop-filter: blur(3px);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  z-index: 100;
  gap: 14px;
  border-radius: 12px;
}

.form-busy-spinner {
  width: 36px;
  height: 36px;
  border: 3px solid var(--border-soft);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: form-busy-spin 0.75s linear infinite;
}

@keyframes form-busy-spin {
  to { transform: rotate(360deg); }
}
`;
