import { useEffect, useMemo, useRef, useState } from 'react';
import './styles/app.css';
import { ToastStack } from './components/ui';
import { THEMES, emptyLogin, defaultStreamConfig, defaultRuntimeSettings, defaultNewUser, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings, defaultVisionThreshold, defaultVisionMinFrames } from './lib/constants';
import {readLiveViewsCookie,saveLiveViewsCookie,bestLiveViewLayout,unwrap,errorMessage,apiBase,parseMetadata,cameraTitle,normalizeScanDevice,orderedSavedCameras,isActionableVisionAlert,latestAlertsByCamera,sameCamera,liveSource,normalizeRuntimeSettings,normalizeMachineHealthSettings,defaultZonePolygon,isLineDetectionType,defaultLineRuleConfig,lineRuleConfigText,defaultVisionRuleDraft,playAlertSound,hasH264VideoTrack,streamOptionLabel,isVisionAlertNotification } from './lib/helpers';
import { LoginPage, ChangePasswordPage, TopBar } from './components/layout';
import { SetupWizard } from './components/setup';
import { ViewsTab, CamerasTab } from './components/cameras';
import { VisionTab } from './components/vision';
import { TrainingTab } from './components/training';
import { SettingsTab } from './components/settings';
import { RecordingTab } from './components/recording';
import { NotificationsTab } from './components/notifications';
import { SecureWipeCountdown, ResetProgressOverlay } from './components/securewipe';


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
  // credentialsRef always holds the latest credentials so request() builds its
  // Basic-auth header from current values even right after a password change,
  // before React re-renders (a stale header would fail auth and trip the lockout).
  const credentialsRef = useRef(credentials);
  // Refs used by request() to handle a background 401 (expired session) cleanly:
  // authenticatedRef avoids firing during a login attempt; sessionExpiredRef makes it
  // fire once; resetActiveRef suppresses it during a factory reset / restart (where
  // 401s are expected and the dedicated overlay handles the reload).
  const authenticatedRef = useRef(false);
  const sessionExpiredRef = useRef(false);
  const resetActiveRef = useRef(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [passwordChangeRequired, setPasswordChangeRequired] = useState(false);
  // Epoch ms until which login is locked (0 = not locked); drives the countdown.
  const [lockoutUntil, setLockoutUntil] = useState(0);
  // First-run setup wizard: shown to an admin until setup is completed/dismissed.
  const [setupNeeded, setSetupNeeded] = useState(false);
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
  // Main-app status toasts (top-right, auto-dismissing). The single `message`
  // string above is bridged into this stack for the authenticated app; login/setup
  // still render `message` inline.
  const [toasts, setToasts] = useState([]);
  const toastIdRef = useRef(0);
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
  const [capacity, setCapacity] = useState(null);
  const [resetAllowed, setResetAllowed] = useState(false);
  const [wipeCountdown, setWipeCountdown] = useState(false);
  const [resetProgress, setResetProgress] = useState(null);
  const [passwordDrafts, setPasswordDrafts] = useState({});
  const [visionRules, setVisionRules] = useState([]);
  const [visionAlerts, setVisionAlerts] = useState([]);
  const [visionClasses, setVisionClasses] = useState([]);
  const [visionLabels, setVisionLabels] = useState([]);
  const [activeModelClasses, setActiveModelClasses] = useState([]);
  const [visionRuleDraft, setVisionRuleDraft] = useState(defaultVisionRuleDraft());
  const [recordingSegments, setRecordingSegments] = useState([]);
  const [recordingConfigs, setRecordingConfigs] = useState([]);
  const [notifOpen, setNotifOpen] = useState(false);
  const [notifUnread, setNotifUnread] = useState(0);
  // Unified notification feed (AI detections, camera/machine health, login
  // security, ...) backing the topbar bell. Loaded unread-only from the shared
  // /api/notifications store; notifUnread mirrors the server's total unread.
  const [notifications, setNotifications] = useState([]);
  const loadNotificationsRef = useRef(null);
  // Bumped whenever the unread feed actually changes (new arrivals, reads), so the
  // Notifications page can auto-refresh in step with the bell rather than staying
  // stale on its own independently-fetched list.
  const [notifVersion, setNotifVersion] = useState(0);
  const notifSigRef = useRef('');
  // Username a login-security notification deep-links to, so the Users settings
  // highlights the account that was targeted.
  const [focusUsername, setFocusUsername] = useState('');
  // Notification id the page should scroll to/highlight after a dropdown click.
  const [notifFocusId, setNotifFocusId] = useState(0);
  const [seenInRecordingIds, setSeenInRecordingIds] = useState(new Set());
  const seenVisionAlertIdsRef = useRef(new Set());
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
  // Map of alertId → its recorded clip segment, so the Notifications page can
  // offer (and play) "View clip" only when footage exists.
  const clipByAlertId = useMemo(() => {
    const map = new Map();
    (recordingSegments || []).forEach((s) => {
      const alertId = Number(s.alertId);
      if (alertId > 0 && !map.has(alertId)) {
        map.set(alertId, s);
      }
    });
    return map;
  }, [recordingSegments]);

  useEffect(() => {
    credentialsRef.current = credentials;
  }, [credentials]);

  useEffect(() => {
    authenticatedRef.current = authenticated;
    if (authenticated) sessionExpiredRef.current = false;
  }, [authenticated]);

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
    const creds = credentialsRef.current;
    if (creds && (creds.username || creds.password)) {
      headers.Authorization = `Basic ${btoa(`${creds.username}:${creds.password}`)}`;
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
      // A 401 on a normal request after we were logged in means the session expired
      // (password-change-required is 403 and lockout is 429, so they're not caught
      // here). Drop to the login screen with a friendly note instead of leaving the
      // user staring at a broken page. Suppressed during a factory reset / restart,
      // and never during the login/auth calls themselves.
      if (
        response.status === 401 &&
        authenticatedRef.current &&
        !resetActiveRef.current &&
        !sessionExpiredRef.current &&
        !path.startsWith('/api/auth')
      ) {
        sessionExpiredRef.current = true;
        logout();
        setMessage('Your session has expired. Please sign in again.');
      }
      const error = new Error(errorMessage(payload, `Request failed with ${response.status}`));
      error.status = response.status;
      error.retryAfter = Number((payload && payload.retryAfterSeconds) || response.headers.get('Retry-After')) || 0;
      throw error;
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
    let session;
    try {
      // A single authenticated probe verifies the credentials and reveals the
      // user's role and whether a forced password change is pending.
      session = await request('/api/auth/session');
    } catch (err) {
      if (err.status === 429 && err.retryAfter > 0) {
        setLockoutUntil(Date.now() + err.retryAfter * 1000);
        setMessage('');
      } else {
        setMessage(err.status === 401 ? 'Invalid username or password.' : err.message);
      }
      setBusy(false);
      return;
    }
    const adminUser = Boolean(session && session.isAdmin);
    setIsAdmin(adminUser);
    if (session && session.mustChangePassword) {
      setPasswordChangeRequired(true);
      setBusy(false);
      return;
    }
    await enterAppOrWizard(adminUser);
  }

  // enterAppOrWizard loads the app, then shows the first-run wizard if setup is
  // pending and the user is an admin (only admins can perform setup).
  async function enterAppOrWizard(adminUser) {
    await enterApp();
    if (!adminUser) return;
    try {
      const setup = await request('/api/setup/state');
      if (setup && !setup.completed) {
        setSetupNeeded(true);
      }
    } catch (_) {
      // Setup state is best-effort; never block entry on it.
    }
  }

  // enterApp loads the landing data and reveals the app shell. Shared by the
  // normal login path and the forced password-change completion.
  async function enterApp() {
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
      setPasswordChangeRequired(false);
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

  // completePasswordChange sets a new password for the must-change user, then
  // enters the app. The stored credential is updated synchronously (via the ref)
  // so the very next request replays the new password rather than the stale one.
  async function completePasswordChange({ currentPassword, newPassword }) {
    setBusy(true);
    setMessage('');
    try {
      await request('/api/auth/change-password', {
        method: 'POST',
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      const nextCredentials = { ...credentialsRef.current, password: newPassword };
      credentialsRef.current = nextCredentials;
      setCredentials(nextCredentials);
    } catch (err) {
      setMessage(err.message);
      setBusy(false);
      return;
    }
    // A forced change is always a first login — show the wizard if setup is pending.
    await enterAppOrWizard(isAdmin);
  }

  function logout() {
    setAuthenticated(false);
    setIsAdmin(false);
    setPasswordChangeRequired(false);
    setLockoutUntil(0);
    setSetupNeeded(false);
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
    setNotifications([]);
    setFocusUsername('');
    setSeenInRecordingIds(new Set());
    seenVisionAlertIdsRef.current = new Set();
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
        destinations: Array.isArray(result?.destinations) ? result.destinations : [],
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
        destinations: Array.isArray(result?.destinations) ? result.destinations : [],
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

  async function purgeExpiredNotifications({ days, onlyRead } = {}) {
    if (!(days > 0)) return;
    setBusy(true);
    setMessage('');
    try {
      const params = new URLSearchParams({ olderThanDays: String(days) });
      if (onlyRead) params.set('onlyRead', 'true');
      const result = await request(`/api/notifications/purge?${params.toString()}`, { method: 'POST' });
      const deleted = Number(result?.deleted) || 0;
      setMessage(deleted > 0 ? `Purged ${deleted} expired detection${deleted === 1 ? '' : 's'}.` : 'No expired detections to purge.');
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

  // estimateCapacity asks the backend how many cameras this host can process,
  // based on detected hardware and (once cameras run) live load.
  async function estimateCapacity() {
    setBusy(true);
    try {
      const result = await request('/api/capacity');
      setCapacity(result || null);
      return result;
    } catch (err) {
      setMessage(err.message);
      return null;
    } finally {
      setBusy(false);
    }
  }

  // calibrateCapacity benchmarks the detector on this host for an accurate
  // cold-start estimate (no cameras needed). Slow on first run (model load).
  async function calibrateCapacity() {
    setBusy(true);
    setMessage('Calibrating detector… this can take several seconds.');
    try {
      const result = await request('/api/capacity/calibrate', { method: 'POST' });
      setCapacity(result || null);
      setMessage('Calibration complete.');
      return result;
    } catch (err) {
      setMessage(err.message);
      return null;
    } finally {
      setBusy(false);
    }
  }

  // --- Secure Wipe & Reset (factory reset) ---

  // loadResetState checks whether factory reset is enabled (bootstrap.allowReset) so
  // the Danger Zone button only appears when the deployment permits it.
  async function loadResetState() {
    try {
      const result = await request('/api/system/reset/state');
      setResetAllowed(!!result?.allowed);
    } catch {
      setResetAllowed(false);
    }
  }

  // startSecureWipe begins the reset, then polls progress. When the server restarts,
  // progress polling fails; once /health recovers we reload into the fresh first-run
  // state (the setup wizard reappears since all users were wiped).
  async function startSecureWipe() {
    resetActiveRef.current = true; // expected 401s during the wipe shouldn't flip to login
    setWipeCountdown(false);
    try {
      const progress = await request('/api/system/reset', { method: 'POST' });
      setResetProgress(progress || { stage: 'erasing', percent: 0, message: 'Starting…', running: true });
      pollResetProgress();
    } catch (err) {
      setResetProgress({ stage: 'failed', percent: 0, error: err.message });
    }
  }

  function pollResetProgress() {
    let phase = 'progress';
    let healthWasDown = false;
    const id = setInterval(async () => {
      if (phase === 'progress') {
        try {
          const progress = await request('/api/system/reset/progress');
          setResetProgress(progress);
          if (progress?.stage === 'failed') { clearInterval(id); return; }
          if (progress?.stage === 'restarting') phase = 'restarting';
        } catch {
          // Progress endpoint unreachable/401 — the database has been wiped and the
          // server is finishing the secure overwrite + restart. Show a clean state
          // instead of leaving the bar frozen on the last DB-backed reading.
          phase = 'restarting';
          setResetProgress({ stage: 'restarting', percent: 100, message: 'Finalizing & restarting…', running: true });
        }
        return;
      }
      // Restarting: wait for the server to drop, then come back, then reload.
      try {
        const health = await fetch(`${apiBase()}/health`, { cache: 'no-store' });
        if (!health.ok) throw new Error('down');
        if (healthWasDown) { clearInterval(id); window.location.reload(); }
      } catch {
        healthWasDown = true;
      }
    }, 1000);
  }

  // --- First-run setup wizard handlers ---

  // wizardChangePassword changes the password from inside the wizard without
  // re-entering the app (keeps the stored credential in sync for later requests).
  async function wizardChangePassword({ currentPassword, newPassword }) {
    setBusy(true);
    setMessage('');
    try {
      await request('/api/auth/change-password', {
        method: 'POST',
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      const nextCredentials = { ...credentialsRef.current, password: newPassword };
      credentialsRef.current = nextCredentials;
      setCredentials(nextCredentials);
      setMessage('Password updated.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  // wizardEnableRecordingAll turns on continuous recording for every saved camera
  // with sensible defaults (7-day retention). storagePath defaults to 'recordings'.
  async function wizardEnableRecordingAll(storagePath) {
    const path = (storagePath || '').trim() || 'recordings';
    for (const cam of saved) {
      await saveRecordingConfig({
        cameraId: cam.id,
        enabled: true,
        preRollSec: 30,
        postRollSec: 10,
        storagePath: path,
        retentionDays: 7,
        segmentMinutes: 15,
        liveStreamUrl: '',
        streamUrl: '',
        fallbackStreamUrl: '',
      });
    }
  }

  // getDiskInfo returns the host volumes (mountpoint + usedPercent) so the wizard can
  // warn when the recording disk is nearly full.
  async function getDiskInfo() {
    try {
      const sample = await request('/api/settings/machine-health/metrics');
      return Array.isArray(sample?.disks) ? sample.disks : [];
    } catch (_) {
      return [];
    }
  }

  // wizardAddDestination appends one notification destination and saves it, so the
  // person-alert rule has somewhere to deliver. Merges over current settings.
  async function wizardAddDestination(dest) {
    setBusy(true);
    setMessage('');
    try {
      const next = {
        ...notificationSettings,
        destinations: [...(notificationSettings.destinations || []), dest],
      };
      const result = await request('/api/settings/notification', { method: 'PUT', body: JSON.stringify(next) });
      const merged = { ...next, destinations: Array.isArray(result?.destinations) ? result.destinations : next.destinations };
      setNotificationSettings(merged);
      setSavedNotificationSettings(merged);
      setMessage('Notification destination saved.');
      return merged.destinations;
    } catch (err) {
      setMessage(err.message);
      return null;
    } finally {
      setBusy(false);
    }
  }

  // wizardAddPersonRuleAll adds a person-detection alert rule to every saved camera.
  async function wizardAddPersonRuleAll() {
    setBusy(true);
    setMessage('');
    try {
      for (const cam of saved) {
        await request('/api/vision/rules', {
          method: 'POST',
          body: JSON.stringify({ ...defaultVisionRuleDraft(cam.id), name: 'Person alert' }),
        });
      }
      await loadVision({ quiet: true });
      setMessage('Person alerts added.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  // wizardAddCamera saves a discovered camera and, when supplied, its credentials
  // (ONVIF cameras need a login before their RTSP stream resolves), then refreshes.
  async function wizardAddCamera(device, creds = {}) {
    setBusy(true);
    setMessage('');
    try {
      const { _discoveryMethods, _openPorts, ...deviceData } = device;
      const name = (creds.name || '').trim() || cameraTitle(device);
      const newId = await request('/api/cameras/discovered', {
        method: 'POST',
        body: JSON.stringify({ ...deviceData, name, description: '' }),
      });
      if (newId && (creds.username || creds.password)) {
        await request(`/api/cameras/${newId}/credentials`, {
          method: 'POST',
          body: JSON.stringify({ username: creds.username || '', password: creds.password || '' }),
        });
      }
      await refresh({ quiet: true });
      setMessage('Camera added.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  // finishSetup marks the wizard complete so it does not show again, then lands on
  // Live Views showing the cameras just added.
  async function finishSetup() {
    try {
      await request('/api/setup/complete', { method: 'POST' });
    } catch (_) {
      // Best-effort; even if persistence fails, don't trap the user in the wizard.
    }
    // Auto-load every camera into Live Views with the grid auto-sized to the
    // count (ignoring any prior tile selection), resolving live URIs in the
    // background. Without this the cameras only show in the saved list, not tiles.
    const devices = orderedSavedCameras(saved);
    if (devices.length > 0) {
      const layout = bestLiveViewLayout(devices.length);
      const preference = { layout, hasPreference: false, ids: devices.map((d) => d.id) };
      const initialTiles = initialTilesFromDevices(devices, preference);
      setViewLayout(layout);
      setViewTiles(initialTiles);
      saveLiveViewsCookie(layout, initialTiles);
      resolvedTilesFromDevices(devices, preference)
        .then((resolved) => {
          setViewTiles(resolved);
          saveLiveViewsCookie(layout, resolved);
        })
        .catch(() => {});
    }
    try { window.localStorage.removeItem('setupStep'); } catch (_) {}
    setActiveTab('views');
    setSetupNeeded(false);
  }

  // restartApp asks the server to relaunch (so startup-only config like a new ffmpeg
  // path or freshly installed deps takes effect), then polls until it is back and
  // reloads the page. The wizard persists its step in localStorage, so it resumes
  // where it left off after the reload.
  async function restartApp() {
    resetActiveRef.current = true; // suppress session-expiry handling during the restart
    setMessage('Restarting…');
    try {
      await request('/api/system/restart', { method: 'POST' });
    } catch (_) {
      // The relaunch can drop the in-flight response; treat that as success and poll.
    }
    // Give the process a moment to exit, then poll /health until the new one answers.
    await new Promise((r) => setTimeout(r, 2000));
    const deadline = Date.now() + 120000;
    for (;;) {
      try {
        const resp = await fetch(`${apiBase()}/health`, { cache: 'no-store' });
        if (resp.ok) break;
      } catch (_) {
        // server still down — keep waiting
      }
      if (Date.now() > deadline) break;
      await new Promise((r) => setTimeout(r, 1500));
    }
    window.location.reload();
  }

  // --- Setup wizard pre-flight handlers (ffmpeg / AI / system) ---

  // pollJob polls a status endpoint until its `status` field is done/failed (or a
  // timeout), returning the final state. Used for the ffmpeg and GPU-deps installers,
  // which run in the background server-side.
  async function pollJob(path, { timeoutMs = 20 * 60 * 1000, intervalMs = 2000 } = {}) {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      let state = null;
      try { state = await request(path); } catch (_) { /* keep polling */ }
      const status = state?.status;
      if (status === 'done' || status === 'failed') return state;
      if (Date.now() > deadline) return state || { status: 'failed', log: 'Timed out.' };
      await new Promise((r) => setTimeout(r, intervalMs));
    }
  }

  async function getFfmpegStatus() {
    try { return await request('/api/settings/decoder/status'); } catch (_) { return null; }
  }

  // installFfmpeg downloads ffmpeg (background job), then auto-tunes the decoder.
  // Returns the final install state so the wizard can flag "restart recommended".
  async function installFfmpeg() {
    setBusy(true);
    setMessage('Downloading ffmpeg…');
    try {
      await request('/api/settings/decoder/ffmpeg/install', { method: 'POST' });
      const state = await pollJob('/api/settings/decoder/ffmpeg/install/status');
      if (state?.status === 'done') {
        try { await request('/api/settings/runtime/auto-tune', { method: 'POST' }); } catch (_) {}
        setMessage('ffmpeg installed. Restart to apply it everywhere.');
      } else {
        setMessage(`ffmpeg install failed: ${state?.log || 'unknown error'}`);
      }
      return state;
    } catch (err) {
      setMessage(err.message);
      return { status: 'failed', log: err.message };
    } finally {
      setBusy(false);
    }
  }

  async function getAiCapability() {
    try { return await request('/api/training/capability'); } catch (_) { return null; }
  }

  // installAiDeps runs the in-app GPU/Python dependency installer (background job).
  async function installAiDeps() {
    setBusy(true);
    setMessage('Installing AI dependencies… this can take several minutes.');
    try {
      await request('/api/training/setup-deps', { method: 'POST' });
      const state = await pollJob('/api/training/setup-deps');
      setMessage(state?.status === 'done' ? 'AI dependencies installed. Restart to apply.' : `Install failed: ${state?.log || 'unknown error'}`);
      return state;
    } catch (err) {
      setMessage(err.message);
      return { status: 'failed', log: err.message };
    } finally {
      setBusy(false);
    }
  }

  async function getStockModel() {
    try { return await request('/api/training/stock-model'); } catch (_) { return null; }
  }

  async function applyStockModel(model) {
    setBusy(true);
    setMessage('Downloading and applying model…');
    try {
      const result = await request('/api/training/stock-model', { method: 'POST', body: JSON.stringify({ model }) });
      setMessage(`Model set to ${result?.current || model}.`);
      return result;
    } catch (err) {
      setMessage(err.message);
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function getSystemTime() {
    try { return await request('/api/system/time'); } catch (_) { return null; }
  }

  // Loads the active model's class labels so the rule "Detect" picker and the
  // Object Classes list can flag which trained classes the active model actually
  // produces (only one model is active at a time).
  async function loadActiveModelClasses() {
    try {
      const result = await request('/api/training/models');
      const items = Array.isArray(result) ? result : result?.items || [];
      const active = items.find((m) => m.isActive);
      let cls = [];
      if (active) { try { cls = JSON.parse(active.classes || '[]'); } catch (_) { cls = []; } }
      setActiveModelClasses(cls.map((c) => String(c).toLowerCase()));
    } catch (_) {
      setActiveModelClasses([]);
    }
  }

  async function loadVision({ quiet = false, notifyNew = false } = {}) {
    if (!quiet) {
      setBusy(true);
      setMessage('');
    }
    try {
      const [rulesResult, alertsResult, classesResult, labelsResult] = await Promise.all([
        request('/api/vision/rules?limit=100&offset=0'),
        // Real detections only (status=detections excludes the periodic vision-
        // monitor diagnostics). Without this, diagnostics flood the bounded window
        // and evict real alerts, emptying the events panel while the badge lingers.
        request('/api/vision/alerts?status=detections&limit=100&offset=0'),
        request('/api/vision/classes'),
        request('/api/vision/labels'),
      ]);
      const rules = Array.isArray(rulesResult) ? rulesResult : rulesResult?.items || [];
      const alerts = Array.isArray(alertsResult) ? alertsResult : alertsResult?.items || [];
      const classes = Array.isArray(classesResult) ? classesResult : classesResult?.items || [];
      const labels = Array.isArray(labelsResult) ? labelsResult : labelsResult?.items || [];
      setVisionRules(rules);
      setVisionAlerts(alerts);
      setVisionClasses(classes);
      setVisionLabels(labels);
      // The unread badge is owned by the unified notification feed; loadVision
      // only needs the new-alert set to decide whether to chime. Diagnostics
      // never become notifications, so they are excluded from the sound trigger.
      const seen = seenVisionAlertIdsRef.current;
      const newActiveAlerts = alerts.filter((alert) => alert?.id && !alert.isAcknowledged && !seen.has(alert.id));
      alerts.forEach((alert) => {
        if (alert?.id) {
          seen.add(alert.id);
        }
      });
      if (notifyNew && newActiveAlerts.some((alert) => {
        if (parseMetadata(alert.metadata).diagnostic) {
          return false;
        }
        const rule = rules.find((item) => Number(item.id) === Number(alert.ruleId));
        return !rule || rule.soundEnabled;
      })) {
        playAlertSound();
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

  // loadNotifications refreshes the unified bell feed: the newest unread
  // notifications from every source, plus the server's total unread for the badge.
  async function loadNotifications({ quiet = true } = {}) {
    try {
      const result = await request('/api/notifications?unread=true&limit=30&offset=0');
      const items = Array.isArray(result) ? result : result?.items || [];
      const total = typeof result?.total === 'number' ? result.total : items.length;
      setNotifications(items);
      setNotifUnread(total);
      // Signal the Notifications page to re-fetch only when the feed truly changed
      // (count or newest id), so a quiet 15s poll doesn't reload it needlessly.
      const signature = `${total}:${items[0]?.id || 0}`;
      if (signature !== notifSigRef.current) {
        notifSigRef.current = signature;
        setNotifVersion((v) => v + 1);
      }
      return items;
    } catch (err) {
      if (!quiet) {
        setMessage(err.message);
      }
      return [];
    }
  }
  loadNotificationsRef.current = loadNotifications;

  // markNotificationRead dismisses one notification: optimistically drop it from
  // the feed and decrement the badge, then persist and reconcile.
  async function markNotificationRead(id) {
    if (!id) {
      return;
    }
    setNotifications((current) => current.filter((n) => Number(n.id) !== Number(id)));
    setNotifUnread((n) => Math.max(0, n - 1));
    try {
      await request(`/api/notifications/${id}/read`, { method: 'POST' });
    } catch (_) {
      /* best-effort; the next reload reconciles */
    }
    loadNotifications({ quiet: true }).catch(() => {});
  }

  // handleNotificationClick (topbar bell): every click opens the dedicated
  // Notifications page focused on the clicked entry. A non-detection click also
  // dismisses it from the bell (a plain click handles it); AI detections stay in
  // the list until acknowledged on the page.
  function openNotificationsPage() {
    setActiveTab('notifications');
    loadNotifications({ quiet: true }).catch(() => {});
    // Enrich AI rows (full alert metadata) and detect which alerts have clips.
    loadVision({ quiet: true }).catch(() => {});
    loadRecording({ quiet: true }).catch(() => {});
  }

  function handleNotificationClick(notif) {
    if (!notif) {
      return;
    }
    setNotifOpen(false);
    setNotifFocusId(Number(notif.id));
    openNotificationsPage();
    if (!isVisionAlertNotification(notif)) {
      markNotificationRead(notif.id);
      return;
    }
    // AI detection: if its alert was already acknowledged, the notification is a
    // leftover with no Acknowledge action — dismiss it so it can't get stuck.
    const alert = visionAlerts.find((a) => Number(a.id) === Number(notif.refId));
    if (alert && alert.isAcknowledged) {
      markNotificationRead(notif.id);
    }
  }

  useEffect(() => {
    if (!authenticated) {
      return undefined;
    }
    loadVision({ quiet: true }).catch(() => {});
    loadNotifications({ quiet: true }).catch(() => {});
    loadActiveModelClasses();
    // Load notification settings so the rule editor's per-rule routing knows the
    // configured delivery destinations (also refreshed when the Notifications
    // settings section is opened).
    loadNotificationSettings({ quiet: true }).catch(() => {});
    // Fallback poll. The unified notification SSE stream below delivers new
    // events in real time; this slower interval reconciles acknowledgements and
    // covers the case where the stream is unavailable (e.g. cross-origin dev).
    const id = window.setInterval(() => {
      loadVision({ quiet: true, notifyNew: true }).catch(() => {});
      loadNotifications({ quiet: true }).catch(() => {});
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
        if (loadNotificationsRef.current) {
          loadNotificationsRef.current({ quiet: true }).catch(() => {});
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

  // Bridge the main app's status `message` into the top-right toast stack: each
  // new message becomes a toast that self-dismisses after a few seconds, and the
  // message is cleared immediately so repeated/identical messages still fire (and
  // login/setup, which render `message` inline, are unaffected). Each toast carries
  // a unique incrementing id so they stack rather than replace one another.
  useEffect(() => {
    if (!authenticated || setupNeeded || !message) {
      return;
    }
    const id = ++toastIdRef.current;
    const text = message;
    setToasts((list) => [{ id, text }, ...list].slice(0, 5));
    setMessage('');
  }, [authenticated, setupNeeded, message]);

  // openCameraAlerts clears a camera's live-tile alert banner (marking its alerts
  // seen) and opens the Notifications page, where its detections can be reviewed,
  // acknowledged, and played. Replaces the old jump to Recording's Event Clips.
  function openCameraAlerts(cameraId) {
    setSeenInRecordingIds((prev) => {
      const next = new Set(prev);
      visionAlerts
        .filter((a) => Number(a.cameraId) === Number(cameraId) && isActionableVisionAlert(a))
        .forEach((a) => next.add(a.id));
      return next;
    });
    openNotificationsPage();
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

  async function purgeExpiredRecordings() {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/recording/segments/purge', { method: 'POST' });
      const deleted = Number(result?.deleted) || 0;
      // Reflect the deletions; a full reload keeps paging/totals honest.
      await loadRecording({ quiet: true });
      setMessage(deleted > 0 ? `Purged ${deleted} expired clip${deleted === 1 ? '' : 's'}.` : 'No expired clips to purge.');
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
      // The two-axis editor maintains a mode-correct ruleConfig (target classes
      // for presence/crowd/intrusion, geometry + classes for line modes), so send
      // it as-is rather than stripping it.
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
      detectionType: rule.detectionType || 'presence',
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

  async function saveVisionClass(payload) {
    setBusy(true);
    setMessage('');
    try {
      await request('/api/vision/classes', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      await loadVision({ quiet: true });
      setMessage('Object class saved.');
    } catch (err) {
      setMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function deleteVisionClass(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/vision/classes/${id}`, { method: 'DELETE' });
      await loadVision({ quiet: true });
      setMessage('Object class deleted.');
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
      }
      setVisionAlerts((current) => [alert, ...current]);
      // The test alert publishes a real notification server-side; pull it into
      // the bell feed immediately.
      loadNotifications({ quiet: true }).catch(() => {});
      if (rule.soundEnabled) {
        playAlertSound();
      }
      setMessage('Test alert created. Opening Notifications…');
      openNotificationsPage();
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
    // Optimistically mark it acknowledged so it drops out of the active lists
    // right away instead of lingering until the reload round-trip.
    setVisionAlerts((current) => current.map((a) => (Number(a.id) === Number(id) ? { ...a, isAcknowledged: true } : a)));
    // Acknowledging a detection also dismisses its bell notification, so the
    // unified feed and the recording view stay in sync.
    const linked = notifications.find((n) => isVisionAlertNotification(n) && Number(n.refId) === Number(id));
    if (linked) {
      markNotificationRead(linked.id);
    }
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/vision/alerts/${id}/ack`, { method: 'POST' });
      await loadVision({ quiet: true });
      setMessage('Alert acknowledged.');
    } catch (err) {
      setMessage(err.message);
      // Re-sync so a failed ack doesn't leave the optimistic change stuck.
      loadVision({ quiet: true }).catch(() => {});
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
      loadResetState();
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
    // Keep every selected camera as a tile; the Views grid paginates them, so the
    // tile set is no longer capped to the layout capacity.
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    const targets = preference.hasPreference
      ? preference.ids.map((id) => devicesById.get(Number(id))).filter(Boolean)
      : devices;
    return targets.map((device) => ({
      ...tileFromDevice(device),
      title: cameraTitle(device),
      ptzSupported: Boolean(device.ptzSupported),
    }));
  }

  async function resolvedTilesFromDevices(devices, preference = readLiveViewsCookie(viewLayout)) {
    const devicesById = new Map(devices.map((device) => [Number(device.id), device]));
    const targets = preference.hasPreference
      ? preference.ids.map((id) => devicesById.get(Number(id))).filter(Boolean)
      : devices;
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
    // No capacity cap: the grid is a per-page size and paging shows any overflow,
    // so adding a camera beyond the current page just lands on a new page.
    if (viewTiles.some((tile) => tile.id === device.id)) {
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
        {passwordChangeRequired ? (
          <ChangePasswordPage
            busy={busy}
            message={message}
            onSubmit={completePasswordChange}
            onCancel={logout}
          />
        ) : (
          <LoginPage
            credentials={credentials}
            busy={busy}
            message={message}
            lockoutUntil={lockoutUntil}
            onChange={setCredentials}
            onSubmit={login}
          />
        )}
      </div>
    );
  }

  if (setupNeeded) {
    return (
      <SetupWizard
        username={credentials.username}
        busy={busy}
        message={message}
        capacity={capacity}
        saved={saved}
        discovered={discovered}
        onChangePassword={wizardChangePassword}
        onEstimateCapacity={estimateCapacity}
        onCalibrateCapacity={calibrateCapacity}
        onScan={() => scan('onvif')}
        onAddCamera={wizardAddCamera}
        onEnableRecordingAll={wizardEnableRecordingAll}
        onAddPersonRuleAll={wizardAddPersonRuleAll}
        onRestart={restartApp}
        onFinish={finishSetup}
        onFfmpegStatus={getFfmpegStatus}
        onInstallFfmpeg={installFfmpeg}
        onAiCapability={getAiCapability}
        onInstallAiDeps={installAiDeps}
        onStockModel={getStockModel}
        onApplyStockModel={applyStockModel}
        onSystemTime={getSystemTime}
        onDiskInfo={getDiskInfo}
        onAddDestination={wizardAddDestination}
      />
    );
  }

  return (
    <main className="app-shell">
      {wipeCountdown ? (
        <SecureWipeCountdown onCancel={() => setWipeCountdown(false)} onProceed={startSecureWipe} />
      ) : null}
      {resetProgress ? (
        <ResetProgressOverlay progress={resetProgress} onDismiss={() => setResetProgress(null)} />
      ) : null}
      <TopBar
        activeTab={activeTab}
        isAdmin={isAdmin}
        busy={busy}
        onTab={(tab) => {
          setActiveTab(tab);
          if (tab === 'settings' && settingsNav === 'users') {
            loadUsers().catch(() => {});
          }
          if (tab === 'ai') {
            loadVision({ quiet: true }).catch(() => {});
          }
          // Cameras hosts the per-camera Recording/Stream config tabs, which read
          // recordingConfigs — keep them fresh when entering either tab.
          if (tab === 'recording' || tab === 'cameras') {
            loadRecording({ quiet: true }).catch(() => {});
          }
          if (tab === 'notifications') {
            loadNotifications({ quiet: true }).catch(() => {});
            loadVision({ quiet: true }).catch(() => {});
            loadRecording({ quiet: true }).catch(() => {});
          }
        }}
        onRefresh={() => refresh()}
        onLogout={logout}
        notifications={notifications}
        notifOpen={notifOpen}
        notifUnread={notifUnread}
        onNotifToggle={() => setNotifOpen((o) => !o)}
        onNotifClick={handleNotificationClick}
        theme={theme}
        onThemeChange={changeTheme}
      />
      <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

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
            // The grid is a per-page size now, not a cap — keep every tile and let
            // paging show the overflow (re-persist the cookie with the new layout).
            setViewTilesWithCookie((current) => current, value);
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
          streamOptionsById={streamOptionsById}
          selectedStreamTokens={selectedStreamTokens}
          saveDrafts={saveDrafts}
          onCameraNav={(nav) => {
            setCameraNav(nav);
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
          recordingConfigs={recordingConfigs}
          onSaveRecordingConfig={saveRecordingConfig}
          canManage={isAdmin}
        />
      ) : null}

      {activeTab === 'ai' ? (
        <VisionTab
          saved={saved}
          rules={visionRules}
          alerts={visionAlerts}
          classes={visionClasses}
          labelCatalog={visionLabels}
          activeModelClasses={activeModelClasses}
          destinations={notificationSettings.destinations}
          ruleDraft={visionRuleDraft}
          busy={busy}
          authHeader={authHeader}
          streamConfig={streamConfig}
          onRuleDraft={setVisionRuleDraft}
          onSaveRule={saveVisionRule}
          onSaveClass={saveVisionClass}
          onDeleteClass={deleteVisionClass}
          onEditRule={editVisionRule}
          onDeleteRule={deleteVisionRule}
          onTriggerTestAlert={triggerTestAlert}
          onAcknowledgeAlert={acknowledgeAlert}
          onPrepareCamera={prepareVisionLiveView}
          onReload={() => loadVision()}
        />
      ) : null}

      {activeTab === 'training' ? (
        <TrainingTab authHeader={authHeader} cameras={saved} onMessage={setMessage} onModelActivated={() => { loadVision({ quiet: true }); loadActiveModelClasses(); }} />
      ) : null}

      {activeTab === 'settings' ? (
        <SettingsTab
          settingsNav={settingsNav}
          settings={runtimeSettings}
          authHeader={authHeader}
          onMessage={setMessage}
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
          onRestart={restartApp}
          onCaptureAutoConfig={captureAutoConfig}
          gpuDevices={decoderGpuDevices}
          onCheckVisionTool={checkVisionTool}
          visionToolStatus={visionToolStatus}
          onInstallPackages={installVisionPackages}
          visionInstallResult={visionInstallResult}
          onLoadUsers={() => loadUsers()}
          focusUsername={focusUsername}
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
          onPurgeNotifications={purgeExpiredNotifications}
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
          capacity={capacity}
          onEstimateCapacity={estimateCapacity}
          onCalibrateCapacity={calibrateCapacity}
          resetAllowed={resetAllowed}
          onSecureWipe={() => setWipeCountdown(true)}
        />
      ) : null}

      {activeTab === 'recording' ? (
        <RecordingTab
          canManage={isAdmin}
          saved={saved}
          segments={recordingSegments}
          busy={busy}
          authHeader={authHeader}
          onDeleteSegment={deleteRecordingSegment}
          onPurgeExpired={purgeExpiredRecordings}
          onReload={() => loadRecording()}
          unacknowledgedAlertIds={unacknowledgedAlertIds}
          onAcknowledgeAlert={acknowledgeAlert}
          alerts={visionAlerts}
        />
      ) : null}

      {activeTab === 'notifications' ? (
        <NotificationsTab
          authHeader={authHeader}
          isAdmin={isAdmin}
          saved={saved}
          visionAlerts={visionAlerts}
          visionRules={visionRules}
          clipByAlertId={clipByAlertId}
          focusId={notifFocusId}
          refreshSignal={notifVersion}
          onAcknowledgeAlert={acknowledgeAlert}
          onMarkRead={markNotificationRead}
          onMessage={setMessage}
          onOpenSettingsSection={(section) => { setActiveTab('settings'); openSettingsSection(section); }}
          onOpenUser={(username) => { setFocusUsername(username); setActiveTab('settings'); openSettingsSection('users'); }}
        />
      ) : null}
    </main>
  );
}

