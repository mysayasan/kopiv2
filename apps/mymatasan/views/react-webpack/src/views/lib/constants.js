export const THEMES = ['light', 'dark', 'slate'];
export const THEME_LABELS = { light: 'Light', dark: 'Dark', slate: 'Slate' };
export const THEME_ICONS  = { light: 'sun',   dark: 'moon', slate: 'palette' };

export const emptyLogin = { username: '', password: '' };
export const defaultDeviceCredentials = { username: '', password: '' };
export const defaultStreamConfig = {
  webrtc: { enabled: true, iceServers: [] },
  mjpegFallback: { enabled: true },
};
export const defaultDecoderConfig = {
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
export const defaultYoloConfig = {
  conf: 0,
  iou: 0,
  augment: false,
  imgsz: 0,
  half: false,
  maxDet: 0,
};
// Best-practice starting point: good accuracy/speed balance; augment on for hard-to-detect poses
export const bestYoloDefaults = {
  conf: 0.20,
  iou: 0.35,
  augment: true,
  imgsz: 640,
  half: false,
  maxDet: 100,
};
// Safe fixed frame-sourcing defaults; mirror the Go createDefaults seed.
// mode "auto" switches siphon/standalone per camera; params apply to both sources.
export const defaultCaptureConfig = {
  mode: 'auto',
  intervalMs: 2000,
  frameWidth: 640,
  standalone: { captureTimeoutMs: 5000 },
  siphon: { fps: 1, staleLimitMs: 4000 },
};
export const captureModeOptions = [
  ['auto', 'Auto (siphon when fresh, else standalone)'],
  ['siphon', 'Siphon (read off recorder)'],
  ['standalone', 'Standalone (AI opens its own grab)'],
];
// Which detection-alert fields/media get included in notifications (webhook,
// telegram, persisted meta). Mirrors the Go defaultAlertNotificationSettings:
// everything on by default.
export const defaultAlertNotificationConfig = {
  includeRuleName: true,
  includeLabel: true,
  includeConfidence: true,
  includeBoundingBox: true,
  includeZonePolygon: true,
  includeSnapshot: true,
};
// Field metadata drives the settings checkboxes (key, label, help text).
export const alertNotificationFields = [
  ['includeRuleName', 'Rule name', 'The triggering rule name; used as the notification title.'],
  ['includeLabel', 'Object label', 'The detected object class, e.g. "person".'],
  ['includeConfidence', 'Confidence', 'Detection confidence percentage.'],
  ['includeBoundingBox', 'Bounding box', 'Detected object box coordinates (JSON).'],
  ['includeZonePolygon', 'Zone polygon', 'The rule zone polygon (JSON).'],
  ['includeSnapshot', 'Snapshot image', 'Attach the snapshot — Telegram photo, webhook base64.'],
];
export const defaultRuntimeSettings = {
  decoder: defaultDecoderConfig,
  stream: defaultStreamConfig,
  vision: { yolo: defaultYoloConfig, capture: defaultCaptureConfig, alertNotification: defaultAlertNotificationConfig },
};
export const defaultNewUser = { username: '', displayName: '', password: '', isAdmin: false, isActive: true };
export const defaultNotificationSettings = {
  webhook: { enabled: false, url: '', minSeverity: 'warning' },
  telegram: { enabled: false, botToken: '', chatId: '', minSeverity: 'warning' },
  retention: { days: 30, onlyRead: false, intervalHours: 6 },
};
export const defaultHealthSettings = {
  enabled: true,
  intervalMs: 30000,
  timeoutMs: 5000,
  failureThreshold: 3,
  recoveryThreshold: 2,
};
// Host (machine) health monitor defaults; mirror Go DefaultMachineHealthSettings.
export const defaultMachineHealthSettings = {
  enabled: true,
  intervalMs: 30000,
  sustainedSamples: 3,
  recoverySamples: 2,
  cpu: { warnPercent: 85, criticalPercent: 95 },
  memory: { warnPercent: 85, criticalPercent: 95 },
  disk: { warnPercent: 80, criticalPercent: 90, paths: [] },
  mitigation: { enabled: true, purgeAtPercent: 88, pauseRecordingAtPercent: 95, resumePercent: 80 },
};
export const defaultZonePoints = [
  [0.15, 0.15],
  [0.85, 0.15],
  [0.85, 0.85],
  [0.15, 0.85],
];
export const defaultVisionThreshold = 0.35;
export const defaultVisionMinFrames = 2;
export const lineDetectionTypes = ['line_crossing', 'multi_line_crossing'];
export const defaultCrowdMinCount = 2;
export const lineClassOptions = ['person', 'vehicle', 'car', 'truck', 'bus', 'motorcycle', 'bicycle', 'animal', 'bird', 'cat', 'dog', 'horse', 'sheep', 'cow', 'mouse', 'rat'];
export const defaultLineClasses = ['person'];
export const maxCrossingLines = 5;
export const scheduleDayOptions = [
  ['mon', 'Mon'],
  ['tue', 'Tue'],
  ['wed', 'Wed'],
  ['thu', 'Thu'],
  ['fri', 'Fri'],
  ['sat', 'Sat'],
  ['sun', 'Sun'],
];
export const weekdayScheduleDays = ['mon', 'tue', 'wed', 'thu', 'fri'];
export const weekendScheduleDays = ['sat', 'sun'];
export const allScheduleDays = scheduleDayOptions.map(([id]) => id);
export const liveViewsCookieName = 'mymatasan_live_views';
// Live View grid layouts, in (columns × rows) order. Tile capacity is cols × rows.
export const liveViewLayouts = [
  { id: '2x2', cols: 2, rows: 2, label: '2×2' },
  { id: '2x4', cols: 2, rows: 4, label: '2×4' },
  { id: '3x3', cols: 3, rows: 3, label: '3×3' },
  { id: '3x4', cols: 3, rows: 4, label: '3×4' },
  { id: '4x4', cols: 4, rows: 4, label: '4×4' },
];
export const defaultLiveViewLayout = '2x2';
