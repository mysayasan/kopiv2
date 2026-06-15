import { useEffect, useMemo, useRef, useState } from 'react';
import './styles/app.css';
import { Message } from './components/ui';
import { THEMES, emptyLogin, defaultStreamConfig, defaultRuntimeSettings, defaultNewUser, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings, defaultVisionThreshold, defaultVisionMinFrames } from './lib/constants';
import {readLiveViewsCookie,saveLiveViewsCookie,layoutCapacity,normalizeLayout,unwrap,errorMessage,apiBase,parseMetadata,cameraTitle,normalizeScanDevice,orderedSavedCameras,isActionableVisionAlert,latestAlertsByCamera,sameCamera,liveSource,normalizeRuntimeSettings,normalizeMachineHealthSettings,defaultZonePolygon,isLineDetectionType,defaultLineRuleConfig,parseLineRuleConfig,lineRuleConfigText,defaultVisionRuleDraft,playAlertSound,hasH264VideoTrack,streamOptionLabel } from './lib/helpers';
import { LoginPage, TopBar } from './components/layout';
import { ViewsTab, CamerasTab } from './components/cameras';
import { VisionTab } from './components/vision';
import { SettingsTab } from './components/settings';
import { RecordingTab } from './components/recording';


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
  const [notificationSettings, setNotificationSettings] = useState(defaultNotificationSettings);
  const [savedNotificationSettings, setSavedNotificationSettings] = useState(defaultNotificationSettings);
  const [healthSettings, setHealthSettings] = useState(defaultHealthSettings);
  const [savedHealthSettings, setSavedHealthSettings] = useState(defaultHealthSettings);
  const [machineHealthSettings, setMachineHealthSettings] = useState(defaultMachineHealthSettings);
  const [savedMachineHealthSettings, setSavedMachineHealthSettings] = useState(defaultMachineHealthSettings);
  const [machineMetrics, setMachineMetrics] = useState(null);
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
  const loadVisionRef = useRef(null);
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

  // refreshCameraHealth quietly reconciles just the live health fields onto the
  // saved cameras, without the busy overlay or live-tile churn of a full refresh.
  // Used by the background poll and the SSE stream so offline/online badges stay
  // current as the health monitor flips state.
  async function refreshCameraHealth() {
    try {
      const result = await request('/api/cameras?limit=100&offset=0');
      const devices = Array.isArray(result) ? result : [];
      const byId = new Map(devices.map((device) => [Number(device.id), device]));
      setSaved((current) =>
        current.map((item) => {
          const fresh = byId.get(Number(item.id));
          return fresh
            ? { ...item, healthStatus: fresh.healthStatus, lastHealthCheckAt: fresh.lastHealthCheckAt }
            : item;
        })
      );
    } catch (_) {
      // Best-effort; the next poll will retry.
    }
  }

  // probeCameraHealth triggers an immediate server-side reachability probe of all
  // cameras (concurrent, short timeout) and merges the fresh status onto the saved
  // cameras. Called right after login so offline cameras are flagged within a
  // couple of seconds — and their live tiles short-circuit to "Offline" — instead
  // of waiting for the debounced background health sweep. Fire-and-forget so it
  // never delays the landing page.
  async function probeCameraHealth() {
    try {
      const result = await request('/api/cameras/health/refresh', { method: 'POST' });
      const snapshots = Array.isArray(result) ? result : [];
      if (snapshots.length === 0) {
        return;
      }
      const byId = new Map(snapshots.map((snap) => [Number(snap.cameraId), snap]));
      setSaved((current) =>
        current.map((item) => {
          const fresh = byId.get(Number(item.id));
          return fresh
            ? { ...item, healthStatus: fresh.status, lastHealthCheckAt: fresh.checkedAt }
            : item;
        })
      );
    } catch (_) {
      // Best-effort; the background poll will reconcile.
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
      // Render the landing immediately with basic tiles; resolving each camera's
      // live view can be slow (or hang on an offline camera), so it must not block
      // login. Live URIs are refined in the background below.
      const initialTiles = initialTilesFromDevices(devices, preference);
      setSaved(devices);
      setVisionRuleDraft((current) => ({ ...current, cameraId: current.cameraId || orderedDevices[0]?.id || '' }));
      setViewLayout(preference.layout);
      setViewTiles(initialTiles);
      saveLiveViewsCookie(preference.layout, initialTiles);
      setAuthenticated(true);
      setActiveTab('views');
      setMessage('');
      // Kick off an immediate health probe (non-blocking) so offline cameras are
      // flagged right away rather than after the background sweep interval.
      probeCameraHealth();
      // Refine tiles with resolved live-view URIs in the background; offline
      // cameras are skipped and never block.
      resolvedTilesFromDevices(devices, preference)
        .then((resolved) => {
          setViewTiles(resolved);
          saveLiveViewsCookie(preference.layout, resolved);
        })
        .catch(() => {});
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

  async function loadNotificationSettings({ quiet = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const result = await request('/api/settings/notification');
      const merged = {
        ...defaultNotificationSettings,
        ...result,
        webhook: { ...defaultNotificationSettings.webhook, ...(result?.webhook || {}) },
        telegram: { ...defaultNotificationSettings.telegram, ...(result?.telegram || {}) },
        retention: { ...defaultNotificationSettings.retention, ...(result?.retention || {}) },
      };
      setNotificationSettings(merged);
      setSavedNotificationSettings(merged);
      return merged;
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function saveNotificationSettings(event) {
    if (event) event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/notification', {
        method: 'PUT',
        body: JSON.stringify(notificationSettings),
      });
      const merged = {
        ...defaultNotificationSettings,
        ...result,
        webhook: { ...defaultNotificationSettings.webhook, ...(result?.webhook || {}) },
        telegram: { ...defaultNotificationSettings.telegram, ...(result?.telegram || {}) },
        retention: { ...defaultNotificationSettings.retention, ...(result?.retention || {}) },
      };
      setNotificationSettings(merged);
      setSavedNotificationSettings(merged);
      setMessage('Notification settings saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function testNotificationChannel(channel) {
    setBusy(true);
    setMessage('');
    try {
      // Send a high-severity test so it passes any minimum-severity filter.
      await request('/api/settings/notification/test?severity=critical', { method: 'POST' });
      setMessage(`Test notification dispatched${channel ? ` to ${channel}` : ''}. Check your ${channel || 'channel'}.`);
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function loadHealthSettings({ quiet = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const result = await request('/api/settings/health');
      const merged = { ...defaultHealthSettings, ...(result || {}) };
      setHealthSettings(merged);
      setSavedHealthSettings(merged);
      return merged;
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function saveHealthSettings(event) {
    if (event) event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/health', {
        method: 'PUT',
        body: JSON.stringify(healthSettings),
      });
      const merged = { ...defaultHealthSettings, ...(result || {}) };
      setHealthSettings(merged);
      setSavedHealthSettings(merged);
      setMessage('Camera health settings saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function loadMachineHealthSettings({ quiet = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const result = normalizeMachineHealthSettings(await request('/api/settings/machine-health'));
      setMachineHealthSettings(result);
      setSavedMachineHealthSettings(result);
      loadMachineMetrics().catch(() => {});
      return result;
    } catch (err) {
      setMessage(err.message);
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  async function saveMachineHealthSettings(event) {
    if (event) event.preventDefault();
    setBusy(true);
    setMessage('');
    try {
      const result = normalizeMachineHealthSettings(await request('/api/settings/machine-health', {
        method: 'PUT',
        body: JSON.stringify(machineHealthSettings),
      }));
      setMachineHealthSettings(result);
      setSavedMachineHealthSettings(result);
      setMessage('Machine health settings saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  // loadMachineMetrics fetches a one-shot host CPU/memory/disk snapshot for the
  // live readout / "Check now" button.
  async function loadMachineMetrics() {
    try {
      const result = await request('/api/settings/machine-health/metrics');
      setMachineMetrics(result || null);
      return result;
    } catch (_) {
      return null;
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

  // Keep a stable reference to the latest loadVision so the SSE stream (which is
  // set up once per session) always calls the current closure.
  loadVisionRef.current = loadVision;

  useEffect(() => {
    if (!authenticated) {
      return undefined;
    }
    loadVision({ quiet: true }).catch(() => {});
    // Fallback poll. The unified notification SSE stream below delivers new
    // events in real time; this slower interval reconciles acknowledgements and
    // covers the case where the stream is unavailable (e.g. cross-origin dev).
    const id = window.setInterval(() => {
      loadVision({ quiet: true, notifyNew: true }).catch(() => {});
      refreshCameraHealth();
    }, 15000);
    return () => window.clearInterval(id);
  }, [authenticated]);

  // Real-time notification stream (Server-Sent Events). The backend funnels all
  // events (vision alerts, health checks, ...) through one feed; on each push we
  // refresh vision state immediately, reusing the same new-alert dedup and sound
  // logic as the poll. EventSource authenticates via the local-auth cookie set
  // by earlier authenticated requests, and auto-reconnects on error.
  useEffect(() => {
    if (!authenticated || typeof window.EventSource === 'undefined') {
      return undefined;
    }
    let source = null;
    let closed = false;
    const connect = () => {
      if (closed) return;
      try {
        source = new EventSource(`${apiBase()}/api/notifications/stream`, { withCredentials: true });
      } catch (_) {
        return;
      }
      source.addEventListener('notification', () => {
        if (loadVisionRef.current) {
          loadVisionRef.current({ quiet: true, notifyNew: true }).catch(() => {});
        }
        refreshCameraHealth();
      });
      // On error EventSource retries automatically; we only force a clean
      // reconnect if the browser gave up and closed the connection.
      source.onerror = () => {
        if (source && source.readyState === EventSource.CLOSED && !closed) {
          source.close();
          window.setTimeout(connect, 5000);
        }
      };
    };
    connect();
    return () => {
      closed = true;
      if (source) source.close();
    };
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

  async function captureAutoConfig() {
    setBusy(true);
    setMessage('');
    try {
      const result = normalizeRuntimeSettings(await request('/api/settings/runtime/capture-auto-config', {
        method: 'POST',
      }));
      setRuntimeSettings(result);
      setSavedRuntimeSettings(result);
      setStreamConfig(result.stream);
      setMessage('Capture auto-config applied.');
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
    } else if (section === 'notifications') {
      loadNotificationSettings({ quiet: true }).catch(() => {});
    } else if (section === 'health') {
      loadHealthSettings({ quiet: true }).catch(() => {});
    } else if (section === 'machine') {
      loadMachineHealthSettings({ quiet: true }).catch(() => {});
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

  // initialTilesFromDevices builds the view tiles synchronously (no per-camera
  // live-view resolution) so the landing page can render immediately; the live
  // URIs are refined in the background by resolvedTilesFromDevices.
  function initialTilesFromDevices(devices, preference) {
    const layout = normalizeLayout(preference.layout);
    const maxTiles = layoutCapacity(layout);
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    const targets = preference.hasPreference
      ? preference.ids.map((id) => devicesById.get(Number(id))).filter(Boolean).slice(0, maxTiles)
      : devices.slice(0, maxTiles);
    return targets.map((device) => ({
      ...tileFromDevice(device),
      title: cameraTitle(device),
      ptzSupported: Boolean(device.ptzSupported),
    }));
  }

  async function resolvedTilesFromDevices(devices, preference = readLiveViewsCookie(viewLayout)) {
    const layout = normalizeLayout(preference.layout);
    const maxTiles = layoutCapacity(layout);
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
    // Known-offline cameras: skip ONVIF/live-view resolution entirely. Those calls
    // probe the camera and would hang on an unreachable one; the live tile
    // short-circuits to "Offline" via health status anyway.
    if ((device.healthStatus || '').toLowerCase() === 'offline') {
      return device;
    }
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
    const maxTiles = layoutCapacity(viewLayout);
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
            setViewTilesWithCookie((current) => current.slice(0, layoutCapacity(value)), value);
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
          onCameraNav={(nav) => {
            setCameraNav(nav);
            if (nav === 'recording') {
              loadRecording({ quiet: true }).catch(() => {});
            }
          }}
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
          recordingConfigSlot={
            <RecordingTab
              mode="config"
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
          }
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
          onCaptureAutoConfig={captureAutoConfig}
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
          notificationSettings={notificationSettings}
          notificationHasChanges={JSON.stringify(notificationSettings) !== JSON.stringify(savedNotificationSettings)}
          onNotificationChange={setNotificationSettings}
          onSaveNotification={saveNotificationSettings}
          onDiscardNotification={() => setNotificationSettings(savedNotificationSettings)}
          onTestNotification={testNotificationChannel}
          healthSettings={healthSettings}
          healthHasChanges={JSON.stringify(healthSettings) !== JSON.stringify(savedHealthSettings)}
          onHealthChange={setHealthSettings}
          onSaveHealth={saveHealthSettings}
          onDiscardHealth={() => setHealthSettings(savedHealthSettings)}
          machineHealthSettings={machineHealthSettings}
          machineHealthHasChanges={JSON.stringify(machineHealthSettings) !== JSON.stringify(savedMachineHealthSettings)}
          onMachineHealthChange={setMachineHealthSettings}
          onSaveMachineHealth={saveMachineHealthSettings}
          onDiscardMachineHealth={() => setMachineHealthSettings(savedMachineHealthSettings)}
          machineMetrics={machineMetrics}
          onRefreshMachineMetrics={loadMachineMetrics}
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

