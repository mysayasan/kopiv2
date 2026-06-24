import config from 'config';
import { defaultDecoderConfig, defaultYoloConfig, defaultCaptureConfig, defaultAlertNotificationConfig, defaultRecordingConfig, defaultMachineHealthSettings, defaultZonePoints, defaultVisionThreshold, defaultVisionMinFrames, lineDetectionTypes, defaultLineClasses, maxCrossingLines, defaultCrowdMinCount, defaultMinOcrConfidence, weekdayScheduleDays, weekendScheduleDays, allScheduleDays, liveViewsCookieName, liveViewLayouts, defaultLiveViewLayout } from './constants';

// normalizeLayout returns a known Live View layout id, defaulting unknown values.
export function normalizeLayout(id) {
  return liveViewLayouts.some((layout) => layout.id === id) ? id : defaultLiveViewLayout;
}

// layoutSpec returns the {id, cols, rows, label} for a layout id (or the default).
export function layoutSpec(id) {
  return liveViewLayouts.find((layout) => layout.id === normalizeLayout(id)) || liveViewLayouts[0];
}

// layoutCapacity returns the number of tiles a layout holds (cols × rows).
export function layoutCapacity(id) {
  const spec = layoutSpec(id);
  return spec.cols * spec.rows;
}

// layoutColumns returns the column count for a layout id.
export function layoutColumns(id) {
  return layoutSpec(id).cols;
}

// layoutRows returns the row count for a layout id.
export function layoutRows(id) {
  return layoutSpec(id).rows;
}

// bestLiveViewLayout picks the smallest layout whose capacity holds `count`
// cameras, so the grid auto-sizes to how many cameras you have. Falls back to the
// largest layout when there are more cameras than any single grid can show.
export function bestLiveViewLayout(count) {
  const sorted = [...liveViewLayouts].sort((a, b) => a.cols * a.rows - b.cols * b.rows);
  const fit = sorted.find((layout) => layout.cols * layout.rows >= count);
  return (fit || sorted[sorted.length - 1]).id;
}

export function readCookie(name) {
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

export function readLiveViewsCookie(fallbackLayout = defaultLiveViewLayout) {
  const fallback = { layout: normalizeLayout(fallbackLayout), ids: [], hasPreference: false };
  const raw = readCookie(liveViewsCookieName);
  if (!raw) {
    return fallback;
  }
  try {
    const parsed = JSON.parse(decodeURIComponent(raw));
    const layout = normalizeLayout(parsed?.layout);
    const ids = Array.isArray(parsed?.ids)
      ? parsed.ids.map((id) => Number(id)).filter((id) => Number.isFinite(id) && id > 0)
      : [];
    return { layout, ids, hasPreference: true };
  } catch (_) {
    return fallback;
  }
}


export function formatFileSize(bytes) {
  const n = Number(bytes || 0);
  if (!n) return '-';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function segmentDuration(seg) {
  const start = Number(seg.startedAt || 0);
  const end = Number(seg.endedAt || 0);
  if (!start || !end || end <= start) return '-';
  const secs = end - start;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

export function segmentFilename(seg) {
  const p = seg.filePath || '';
  const parts = p.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || `clip-${seg.id}`;
}

export function detectionTypeLabel(type) {
  const map = { motion: 'Motion', intrusion: 'Intrusion', fire: 'Fire', line_crossing: 'Line crossing', multi_line_crossing: 'Multi-line crossing' };
  return map[type] || (type ? type.replace(/_/g, ' ') : 'Event');
}

// The notification feed unifies every event source. These map a stored
// notification's source/category to a short human kind shown in the dropdown.
export const notificationSourceLabels = {
  'vision-monitor': 'AI Detection',
  'camera-health-monitor': 'Camera Health',
  'machine-health-monitor': 'Machine Health',
  'local-auth': 'Login Security',
  settings: 'Settings',
};

export const notificationCategoryLabels = {
  'vision.alert': 'AI Detection',
  'health.check': 'Camera Health',
  system: 'System',
};

// notificationKind returns the short label for a notification's origin, preferring
// the specific source (machine vs camera health) over the coarse category.
export function notificationKind(notif) {
  if (!notif) {
    return 'Notification';
  }
  return notificationSourceLabels[notif.source] || notificationCategoryLabels[notif.category] || 'Notification';
}

// isVisionAlertNotification reports whether a notification is an AI detection that
// links back to an AlertEvent (so a click can deep-link into the recording clip).
export function isVisionAlertNotification(notif) {
  return notif?.refType === 'alert_event' && Number(notif?.refId) > 0;
}

export function todayDateString() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function saveLiveViewsCookie(layout, tiles) {
  if (typeof document === 'undefined') {
    return;
  }
  const payload = {
    layout: normalizeLayout(layout),
    ids: (tiles || []).map((tile) => Number(tile?.id)).filter((id) => Number.isFinite(id) && id > 0),
  };
  document.cookie = `${liveViewsCookieName}=${encodeURIComponent(JSON.stringify(payload))}; path=/; max-age=31536000; SameSite=Lax`;
}

export function unwrap(payload) {
  if (payload && payload.data && Object.prototype.hasOwnProperty.call(payload.data, 'result')) {
    return payload.data.result;
  }
  if (payload && Object.prototype.hasOwnProperty.call(payload, 'result')) {
    return payload.result;
  }
  return payload;
}

export function errorMessage(payload, fallback) {
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

export function apiBase() {
  const origin = window.location.origin;
  if (origin.includes(':4000') && config.apiUrl) {
    return config.apiUrl;
  }
  return origin;
}

export function fieldValue(value) {
  return value === undefined || value === null || value === '' ? '-' : value;
}

export function formatTimestamp(value) {
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

export function parseMetadata(value) {
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

export function formatPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return '-';
  }
  return `${(number * 100).toFixed(1)}%`;
}

export function parseBoundingBox(value) {
  if (!value || typeof value !== 'string') return null;
  try {
    const b = JSON.parse(value);
    if (b && typeof b.x === 'number') return b;
  } catch (_) {}
  return null;
}

export const DETECTION_SOURCE_LABELS = {
  'motion-detector': 'Motion',
  'motion-line-crossing-detector': 'Motion Line Crossing',
  'motion-multi-line-crossing-detector': 'Motion Multi-Line',
  'object-detector': 'YOLO Object',
  'persistent-object-detector': 'YOLO Persistent',
  'object-line-crossing-detector': 'YOLO Line Crossing',
  'vision-monitor': 'System',
};

export function formatSourceLabel(source) {
  return DETECTION_SOURCE_LABELS[source] || source || '-';
}

export function cameraTitle(device) {
  return device?.name || device?.model || device?.host || 'Camera';
}

export function normalizeScanDevice(d) {
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

export function cameraDescription(device) {
  return device?.description || '';
}

export function compareSavedCameras(left, right) {
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

export function orderedSavedCameras(devices) {
  return [...(devices || [])].sort(compareSavedCameras);
}

export function isActionableVisionAlert(alert) {
  if (!alert || alert.isAcknowledged) {
    return false;
  }
  return !parseMetadata(alert.metadata).diagnostic;
}

export function latestAlertsByCamera(alerts) {
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

export function normalizeCameraValue(value) {
  return String(value || '').trim().toLowerCase();
}

export function cameraMatchKeys(device) {
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

export function sameCamera(left, right) {
  const rightKeys = new Set(cameraMatchKeys(right));
  return cameraMatchKeys(left).some((key) => rightKeys.has(key));
}

export function liveSource(id, options = {}) {
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

export function fallbackLiveSource(id) {
  return liveSource(id, { preferSnapshot: true });
}

export function normalizeStreamConfig(value) {
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

export function numberOrDefault(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

// normalizeMachineHealthSettings deep-merges the persisted host-health settings
// onto the defaults so the panel always has the full nested shape (cpu/memory/
// disk/mitigation) even when the server omits a field.
export function normalizeMachineHealthSettings(value) {
  const v = value || {};
  return {
    enabled: v.enabled !== false,
    intervalMs: numberOrDefault(v.intervalMs, defaultMachineHealthSettings.intervalMs),
    sustainedSamples: numberOrDefault(v.sustainedSamples, defaultMachineHealthSettings.sustainedSamples),
    recoverySamples: numberOrDefault(v.recoverySamples, defaultMachineHealthSettings.recoverySamples),
    cpu: { ...defaultMachineHealthSettings.cpu, ...(v.cpu || {}) },
    memory: { ...defaultMachineHealthSettings.memory, ...(v.memory || {}) },
    disk: {
      ...defaultMachineHealthSettings.disk,
      ...(v.disk || {}),
      paths: Array.isArray(v.disk?.paths) ? v.disk.paths : [],
    },
    mitigation: { ...defaultMachineHealthSettings.mitigation, ...(v.mitigation || {}) },
  };
}

export function normalizeRuntimeSettings(value) {
  const yolo = value?.vision?.yolo || {};
  const capture = value?.vision?.capture || {};
  const captureMode = ['auto', 'siphon', 'standalone'].includes(capture.mode) ? capture.mode : defaultCaptureConfig.mode;
  // A nil alertNotification (legacy/unset) means the backend includes everything;
  // mirror that here so the checkboxes render all-on until the user changes them.
  const an = value?.vision?.alertNotification;
  const alertNotification = an
    ? Object.fromEntries(Object.keys(defaultAlertNotificationConfig).map((k) => [k, an[k] === true]))
    : { ...defaultAlertNotificationConfig };
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
      capture: {
        mode: captureMode,
        intervalMs: numberOrDefault(capture.intervalMs, defaultCaptureConfig.intervalMs),
        frameWidth: numberOrDefault(capture.frameWidth, defaultCaptureConfig.frameWidth),
        standalone: {
          captureTimeoutMs: numberOrDefault(capture.standalone?.captureTimeoutMs, defaultCaptureConfig.standalone.captureTimeoutMs),
        },
        siphon: {
          fps: numberOrDefault(capture.siphon?.fps, defaultCaptureConfig.siphon.fps),
          staleLimitMs: numberOrDefault(capture.siphon?.staleLimitMs, defaultCaptureConfig.siphon.staleLimitMs),
        },
      },
      alertNotification,
    },
    recording: {
      storage: {
        codec: ['copy', 'h264', 'hevc'].includes(value?.recording?.storage?.codec)
          ? value.recording.storage.codec
          : defaultRecordingConfig.storage.codec,
        quality: numberOrDefault(value?.recording?.storage?.quality, defaultRecordingConfig.storage.quality),
        maxConcurrentEncodes: numberOrDefault(value?.recording?.storage?.maxConcurrentEncodes, defaultRecordingConfig.storage.maxConcurrentEncodes),
      },
    },
  };
}

export function iceUrlsText(server) {
  return Array.isArray(server?.urls) ? server.urls.join('\n') : '';
}

export function textToIceUrls(value) {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export const decoderTransportOptions = ['tcp', 'udp', 'udp_multicast', 'http', 'https'];
export const decoderHWAccelOptions = ['none', 'auto', 'd3d11va', 'dxva2', 'vaapi', 'cuda', 'qsv', 'videotoolbox', 'vdpau', 'vulkan'];

export function clamp01(value) {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(1, value));
}

export function roundedPoint(point) {
  return [Number(clamp01(point[0]).toFixed(4)), Number(clamp01(point[1]).toFixed(4))];
}

export function zonePolygonText(points) {
  const normalized = (points || []).map(roundedPoint);
  return JSON.stringify(normalized, null, 2);
}

export function parseZonePolygon(value) {
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

export const defaultZonePolygon = zonePolygonText(defaultZonePoints);

export function isLineDetectionType(value) {
  return lineDetectionTypes.includes(value);
}

export function isCrowdDetectionType(value) {
  return value === 'crowd';
}

export function defaultCrowdRuleConfig() {
  return { minCount: defaultCrowdMinCount };
}

export function normalizeCrowdConfig(config) {
  const source = config && typeof config === 'object' ? config : {};
  const minCount = Math.max(2, Math.min(100, Math.round(Number(source.minCount) || defaultCrowdMinCount)));
  return { minCount };
}

export function parseCrowdRuleConfig(value) {
  if (!value) {
    return defaultCrowdRuleConfig();
  }
  try {
    return normalizeCrowdConfig(JSON.parse(value));
  } catch (_) {
    return defaultCrowdRuleConfig();
  }
}

export function crowdRuleConfigText(config) {
  return JSON.stringify(normalizeCrowdConfig(config), null, 2);
}

export function defaultLineRuleConfig(type = 'line_crossing') {
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

export function normalizeLineConfig(config, type = 'line_crossing') {
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

export function parseLineRuleConfig(value, type = 'line_crossing') {
  if (!value) {
    return defaultLineRuleConfig(type);
  }
  try {
    return normalizeLineConfig(JSON.parse(value), type);
  } catch (_) {
    return defaultLineRuleConfig(type);
  }
}

export function lineRuleConfigText(config, type = 'line_crossing') {
  return JSON.stringify(normalizeLineConfig(config, type), null, 2);
}

export function lineCountFromRule(rule) {
  if (!isLineDetectionType(rule?.detectionType)) {
    return '';
  }
  const config = parseLineRuleConfig(rule.ruleConfig, rule.detectionType);
  return `${config.lines.length} line${config.lines.length === 1 ? '' : 's'}`;
}

// ---- Two-axis rule model: Mode (how) + Target classes (what) ----

// detectionModes are the fixed "how" behaviors. The "what" is the target class
// list, sourced from the class registry.
export const detectionModes = [
  ['presence', 'Presence'],
  ['crowd', 'Crowd / count'],
  ['intrusion', 'Intrusion (zone)'],
  ['line_crossing', 'Line crossing'],
  ['multi_line_crossing', 'Multi-line crossing'],
  ['lpr', 'License plate (LPR)'],
];

// Legacy single-object detection types map to presence mode with that class as
// the sole target, so existing rules open correctly in the two-axis editor.
export const legacyPresenceTypes = ['fire', 'smoke', 'person', 'vehicle', 'animal'];
const legacyTypeTargets = {
  fire: ['fire'],
  smoke: ['smoke'],
  person: ['person'],
  vehicle: ['vehicle'],
  animal: ['animal'],
  intrusion: ['person', 'vehicle'],
};

export function modeFromDetectionType(type) {
  const value = String(type || '').toLowerCase();
  if (value === 'presence' || legacyPresenceTypes.includes(value)) {
    return 'presence';
  }
  if (['crowd', 'intrusion', 'line_crossing', 'multi_line_crossing', 'lpr'].includes(value)) {
    return value;
  }
  return 'presence';
}

export function isLprDetectionType(value) {
  return String(value || '').toLowerCase() === 'lpr';
}

// parseLPRRuleConfig reads an LPR rule's ruleConfig into a normalized shape:
// { plates: string[], matchMode: 'any'|'include'|'exclude', minOcrConfidence }.
export function parseLPRRuleConfig(value) {
  let cfg = {};
  if (value) {
    try { cfg = JSON.parse(value) || {}; } catch (_) { cfg = {}; }
  }
  const plates = Array.isArray(cfg.plates)
    ? cfg.plates.map((p) => String(p).trim().toUpperCase().replace(/[^A-Z0-9]/g, '')).filter(Boolean)
    : [];
  let matchMode = String(cfg.matchMode || '').toLowerCase();
  if (!['any', 'include', 'exclude'].includes(matchMode)) {
    matchMode = plates.length ? 'include' : 'any';
  }
  let minOcrConfidence = Number(cfg.minOcrConfidence);
  if (!(minOcrConfidence > 0 && minOcrConfidence <= 1)) {
    minOcrConfidence = defaultMinOcrConfidence;
  }
  return { plates, matchMode, minOcrConfidence };
}

// lprRuleConfigText serializes the LPR config back to ruleConfig JSON.
export function lprRuleConfigText(cfg) {
  return JSON.stringify({
    plates: cfg.plates || [],
    matchMode: cfg.matchMode || 'any',
    minOcrConfidence: cfg.minOcrConfidence || defaultMinOcrConfidence,
  }, null, 2);
}

export function detectionTypeForMode(mode) {
  return mode === 'presence' ? 'presence' : mode;
}

// targetClassesFromRule returns the rule's selected class slugs, falling back to
// the legacy classMap defaults for old rules that carried the class in the type.
export function targetClassesFromRule(rule) {
  const fromConfig = parseRuleClasses(rule?.ruleConfig);
  if (fromConfig.length) {
    return fromConfig;
  }
  const type = String(rule?.detectionType || '').toLowerCase();
  if (legacyTypeTargets[type]) {
    return [...legacyTypeTargets[type]];
  }
  if (type && type !== 'presence' && !['crowd', 'line_crossing', 'multi_line_crossing'].includes(type)) {
    return [type];
  }
  return [];
}

function parseRuleClasses(value) {
  if (!value) {
    return [];
  }
  try {
    const cfg = JSON.parse(value);
    return Array.isArray(cfg?.classes)
      ? cfg.classes.map((c) => String(c).trim().toLowerCase()).filter(Boolean)
      : [];
  } catch (_) {
    return [];
  }
}

// buildRuleConfigForMode produces the ruleConfig JSON string for a given mode and
// target classes, preserving mode-specific fields (crowd minCount, line geometry)
// from the existing config.
export function buildRuleConfigForMode(mode, targetClasses, existingConfig) {
  const classes = (targetClasses || []).map((c) => String(c).trim().toLowerCase()).filter(Boolean);
  if (mode === 'line_crossing' || mode === 'multi_line_crossing') {
    const cfg = normalizeLineConfig(parseLineRuleConfig(existingConfig, mode), mode);
    cfg.classes = classes.length ? classes : defaultLineClasses;
    return lineRuleConfigText(cfg, mode);
  }
  if (mode === 'crowd') {
    const cfg = parseCrowdRuleConfig(existingConfig);
    return JSON.stringify({ minCount: cfg.minCount, classes }, null, 2);
  }
  if (mode === 'lpr') {
    // LPR matches plates (via the worker's plate label), not the generic class
    // list, so it carries plates/matchMode/minOcrConfidence instead of classes.
    return lprRuleConfigText(parseLPRRuleConfig(existingConfig));
  }
  // presence / intrusion
  return JSON.stringify({ classes }, null, 2);
}

// ruleDestinationsFromConfig reads the per-rule routing list (ruleConfig.destinations)
// — the notification destination ids this rule's alerts go to. Empty = all.
export function ruleDestinationsFromConfig(ruleConfig) {
  try {
    const cfg = JSON.parse(ruleConfig || '{}');
    return Array.isArray(cfg.destinations) ? cfg.destinations.map(String) : [];
  } catch (_) {
    return [];
  }
}

// applyRuleDestinations returns the ruleConfig JSON with its destinations key set
// to the given ids (or removed when empty), preserving all other config fields.
export function applyRuleDestinations(ruleConfig, destinationIds) {
  let cfg;
  try {
    cfg = JSON.parse(ruleConfig || '{}');
  } catch (_) {
    cfg = {};
  }
  const ids = (destinationIds || []).map(String).filter(Boolean);
  if (ids.length) {
    cfg.destinations = ids;
  } else {
    delete cfg.destinations;
  }
  return JSON.stringify(cfg, null, 2);
}

// ---- Class registry helpers ----

// groupedClassOptions buckets registry classes for the Target picker. Disabled
// rows are dropped. Each bucket is [label, items[]].
export function groupedClassOptions(classes) {
  const buckets = { object: [], hazard: [], group: [] };
  (classes || []).forEach((cls) => {
    if (cls.isEnabled === false) {
      return;
    }
    const kind = ['object', 'hazard', 'group'].includes(cls.kind) ? cls.kind : 'object';
    buckets[kind].push(cls);
  });
  return [
    ['Objects', buckets.object],
    ['Hazards', buckets.hazard],
    ['Groups', buckets.group],
  ].filter(([, items]) => items.length);
}

export function classDisplayName(classes, slug) {
  const match = (classes || []).find((cls) => cls.name === slug);
  return match?.displayName || titleizeSlug(slug);
}

function titleizeSlug(slug) {
  return String(slug || '')
    .split(/[_\-\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

export function defaultVisionRuleDraft(cameraId = '') {
  return {
    id: 0,
    cameraId: cameraId || '',
    name: '',
    detectionType: 'presence',
    zonePolygon: defaultZonePolygon,
    ruleConfig: JSON.stringify({ classes: ['person'] }),
    schedulePolicy: '',
    threshold: defaultVisionThreshold,
    minFrames: defaultVisionMinFrames,
    cooldownSeconds: 30,
    soundEnabled: true,
    isEnabled: true,
  };
}

export function browserTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Local';
  } catch (_) {
    return 'Local';
  }
}

export function parseSchedulePolicy(value) {
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

export function sameScheduleDays(left, right) {
  const leftSet = new Set(left || []);
  const rightSet = new Set(right || []);
  if (leftSet.size !== rightSet.size) {
    return false;
  }
  return [...leftSet].every((day) => rightSet.has(day));
}

export function schedulePolicyText(policy) {
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

export function weeklySchedulePolicy({ days, start, end, mode = 'allow', timezone = browserTimezone(), preset = '' }) {
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

export function rangeSchedulePolicy({ start, end, mode = 'allow', timezone = browserTimezone(), preset = '' }) {
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

export function schedulePresetPolicy(preset, current = scheduleDraftFromPolicy('')) {
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

export function scheduleDraftFromPolicy(value) {
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

export function datetimeLocalFromDate(date) {
  const pad = (value) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function datetimeLocalFromRFC3339(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return datetimeLocalFromDate(date);
}

export function datetimeLocalToRFC3339(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return date.toISOString();
}

export function scheduleSummary(value) {
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

export function playAlertSound() {
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

export function waitForIceGathering(pc) {
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

export async function createWebRTCAnswer(deviceId, offer, authHeader) {
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

export function parseTracks(value) {
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

export function isVideoTrack(track) {
  const mediaType = String(track?.mediaType || '').toLowerCase();
  const codec = String(track?.codec || '').toLowerCase();
  return mediaType.includes('video') || ['h264', 'h265', 'hevc'].some((value) => codec.includes(value));
}

export function hasH264VideoTrack(value) {
  return parseTracks(value).some((track) => isVideoTrack(track) && /h\.?264|avc/.test(String(track?.codec || '').toLowerCase()));
}

export function shouldUseMJPEGForTracks(value) {
  const videoTracks = parseTracks(value).filter(isVideoTrack);
  return videoTracks.length > 0 && !videoTracks.some((track) => /h\.?264|avc/.test(String(track?.codec || '').toLowerCase()));
}

export function streamOptionLabel(option) {
  const name = option?.name || option?.profileToken || 'Stream';
  const details = [option?.encoding, option?.width && option?.height ? `${option.width}x${option.height}` : '']
    .filter(Boolean)
    .join(' ');
  const badges = [option?.preferred ? 'preferred' : '', option?.selected ? 'selected' : ''].filter(Boolean).join(', ');
  return `${name}${details ? ` - ${details}` : ''}${badges ? ` (${badges})` : ''}`;
}


