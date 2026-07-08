import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import './styles/app.css';
import './styles/rbac-standard.css';
import { ToastStack } from './components/ui';
import { LangProvider, normalizeLang, useT } from '@shared/i18n';
import { AppFooter } from '@shared/AppFooter';
import { messages as appMessages } from './i18n';
import { THEMES, emptyLogin, defaultStreamConfig, defaultRuntimeSettings, defaultNewUser, defaultNotificationSettings, defaultHealthSettings, defaultMachineHealthSettings, defaultVisionThreshold, defaultVisionMinFrames } from './lib/constants';
import {readLiveViewsCookie,saveLiveViewsCookie,bestLiveViewLayout,unwrap,errorMessage,apiBase,parseMetadata,cameraTitle,normalizeScanDevice,orderedSavedCameras,isActionableVisionAlert,latestAlertsByCamera,sameCamera,liveSource,normalizeRuntimeSettings,normalizeMachineHealthSettings,defaultZonePolygon,isLineDetectionType,defaultLineRuleConfig,lineRuleConfigText,defaultVisionRuleDraft,playAlertSound,hasH264VideoTrack,isVisionAlertNotification } from './lib/helpers';
import { LoginPage, ChangePasswordPage, RecoveryGatePage, MagicWordEasterEgg, SideNav, WorkspaceHeader } from './components/layout';
import { DashboardTab } from './components/dashboard';
import { SetupWizard } from './components/setup';
import { ViewsTab, CamerasTab } from './components/cameras';
import { TeachTab } from './components/teach';
import { SettingsTab } from './components/settings';
import { NotificationsTab } from './components/notifications';
import { SecureWipeCountdown, ResetProgressOverlay } from './components/securewipe';


function AppInner({ lang, onLangChange }) {
  const t = useT();
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
  // Side-nav display mode: pinned (default, always in-flow) vs auto-hide (collapses
  // to a slim hover edge and slides in on hover). Persisted like the theme.
  const [navPinned, setNavPinned] = useState(() => {
    try { return localStorage.getItem('mymatasan_nav_pinned') !== 'false'; } catch (_) { return true; }
  });
  function toggleNavPinned() {
    setNavPinned((p) => {
      const next = !p;
      try { localStorage.setItem('mymatasan_nav_pinned', String(next)); } catch (_) {}
      return next;
    });
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
  // Jurassic Park "magic word" easter egg — shown only after the 3rd consecutive
  // failed sign-in (so genuine typos are spared). The nonce is bumped each time it
  // fires so the overlay remounts and replays its voice/animation.
  const [magicWord, setMagicWord] = useState(false);
  const [magicWordNonce, setMagicWordNonce] = useState(0);
  const failedLoginsRef = useRef(0);
  // First-run setup wizard: shown to an admin until setup is completed/dismissed.
  const [setupNeeded, setSetupNeeded] = useState(false);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [settingsNav, setSettingsNav] = useState('runtime');
  const [cameraNav, setCameraNav] = useState('probe');
  // The saved camera whose properties the Cameras page is showing, driven by the
  // side-nav camera tree (null = the probe/discovery view). Lifted here so the rail
  // and the page stay in sync (mirrors myseliasan's managingNodeId).
  const [managingCameraId, setManagingCameraId] = useState(null);
  // Per-camera credential status ("ok"|"unauthorized"|"unreachable") for the camera-node
  // access gate: when a camera's stored login stops working, all its tabs are blocked
  // behind a credential prompt until valid credentials are re-entered.
  const [cameraAuthById, setCameraAuthById] = useState({});
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
  // Status message bridged into the toast stack (authenticated app) or rendered
  // inline (login/setup). Held as { text, kind } | null so toasts can show severity.
  // setMessage stays string-compatible. Default kind is 'success' (green) because
  // nearly every literal status message reports a completed action; the contract is:
  //   - failures MUST pass 'error' (red) — every setMessage(err.message, 'error')
  //     does, plus the explicit validation/failure messages below;
  //   - in-progress messages pass 'info' (neutral, e.g. 'Downloading…');
  //   - setMessage('') clears.
  const [message, setMessageState] = useState(null);
  const setMessage = useCallback(
    (text, kind = 'success') => setMessageState(text ? { text: String(text), kind } : null),
    []
  );
  // Main-app status toasts (top-right, auto-dismissing). The `message` above is
  // bridged into this stack for the authenticated app; login/setup render it inline.
  const [toasts, setToasts] = useState([]);
  const toastIdRef = useRef(0);
  // Sticky bottom-right warning shown when the configured recording storage codec
  // can't run on this host (no compatible GPU to re-encode). Holds { codec, fallback }
  // or null; clicking it jumps to Settings → Recording storage.
  const [codecWarning, setCodecWarning] = useState(null);
  const [busy, setBusy] = useState(false);
  const [deviceDrafts, setDeviceDrafts] = useState({});
  const [deviceCredentials, setDeviceCredentials] = useState({});
  const [streamOptionsById, setStreamOptionsById] = useState({});
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
  // Recovery gate: when encryption is on and the master key is missing, the backend serves
  // only the recovery endpoints. recoveryPending=null means "not checked yet".
  const [recoveryPending, setRecoveryPending] = useState(null);
  const [recoveryKeyId, setRecoveryKeyId] = useState('');
  const [recoveryRestarting, setRecoveryRestarting] = useState(false);
  const [passwordDrafts, setPasswordDrafts] = useState({});
  const [visionRules, setVisionRules] = useState([]);
  const [visionAlerts, setVisionAlerts] = useState([]);
  const [visionClasses, setVisionClasses] = useState([]);
  const [visionLabels, setVisionLabels] = useState([]);
  const [activeModelClasses, setActiveModelClasses] = useState([]);
  // Cameras with a live teaching session — drives the side-nav "learning" badge.
  const [teachingCameraIds, setTeachingCameraIds] = useState([]);
  // Map of ruleId → { name, skillType } for detection rules auto-created by a
  // Teach skill, so the AI Detection list can badge them.
  const [taughtRuleMap, setTaughtRuleMap] = useState({});
  const [visionRuleDraft, setVisionRuleDraft] = useState(defaultVisionRuleDraft());
  const [recordingSegments, setRecordingSegments] = useState([]);
  const [recordingConfigs, setRecordingConfigs] = useState([]);
  const [notifUnread, setNotifUnread] = useState(0);
  // Unified notification feed (AI detections, camera/machine health, login
  // security, ...). notifUnread drives the side-nav Notifications badge. Loaded
  // unread-only from the shared /api/notifications store; notifUnread mirrors the
  // server's total unread.
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
        setMessage(t('app.sessionExpired'), 'error');
      }
      const error = new Error(errorMessage(payload, `Request failed with ${response.status}`));
      error.status = response.status;
      error.retryAfter = Number((payload && payload.retryAfterSeconds) || response.headers.get('Retry-After')) || 0;
      throw error;
    }
    return unwrap(payload);
  }

  // checkRecoveryGate probes the public recovery endpoint. It only returns pending when the
  // backend is running in recovery mode (encryption on + master key missing). In normal mode
  // the route does not exist (404) so this resolves to not-pending. Runs once before login.
  async function checkRecoveryGate() {
    try {
      const resp = await fetch(`${apiBase()}/api/system/recovery/gate`, { credentials: 'include' });
      if (!resp.ok) { setRecoveryPending(false); return; }
      const payload = await resp.json().catch(() => null);
      const body = unwrap(payload);
      if (body && body.pending) {
        setRecoveryKeyId(body.keyId || '');
        setRecoveryPending(true);
      } else {
        setRecoveryPending(false);
      }
    } catch (_) {
      setRecoveryPending(false);
    }
  }

  useEffect(() => {
    checkRecoveryGate();
    /* eslint-disable-next-line */
  }, []);

  // submitRecovery restores the master key from the uploaded escrow + passphrase, then waits
  // for the backend to restart out of recovery mode and reloads into the normal login.
  async function submitRecovery({ keyBase64, passphrase }) {
    setBusy(true);
    setMessage('');
    try {
      const resp = await fetch(`${apiBase()}/api/system/recovery/unlock`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ keyBase64, passphrase }),
      });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) {
        throw new Error(errorMessage(payload, t('rg.unlockFail')));
      }
      // Server is restarting; poll until it comes back no longer pending, then reload.
      setRecoveryRestarting(true);
      const deadline = Date.now() + 90000;
      const poll = async () => {
        try {
          const r = await fetch(`${apiBase()}/api/system/recovery/gate`, { credentials: 'include' });
          if (!r.ok) { window.location.reload(); return; }
          const b = unwrap(await r.json().catch(() => null));
          if (!b || !b.pending) { window.location.reload(); return; }
        } catch (_) { /* server still down — keep waiting */ }
        if (Date.now() < deadline) { setTimeout(poll, 2000); }
        else { setRecoveryRestarting(false); setMessage(t('rg.restartTimeout'), 'error'); }
      };
      setTimeout(poll, 2500);
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
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
        setMessage(t('app.savedCamsRefreshed'));
      }
      return Array.isArray(result) ? result : [];
    } catch (err) {
      setMessage(err.message, 'error');
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
    // Pre-warm the speech engine on this click gesture: the Windows TTS voices have
    // a ~1-2s cold start, so a silent priming utterance here means the "magic word"
    // easter egg (shown on the 3rd failure) speaks instantly instead of lagging.
    try {
      const synth = window.speechSynthesis;
      if (synth) {
        const warm = new SpeechSynthesisUtterance('a');
        warm.volume = 0;
        synth.speak(warm);
      }
    } catch (_) { /* ignore */ }
    if (!credentials.username || !credentials.password) {
      setMessage(t('app.userPassRequired'), 'error');
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
        setMessage(err.status === 401 ? 'Invalid username or password.' : err.message, 'error');
        if (err.status === 401 && (failedLoginsRef.current += 1) >= 3) {
          setMagicWordNonce((n) => n + 1);
          setMagicWord(true);
        }
      }
      setBusy(false);
      return;
    }
    failedLoginsRef.current = 0;  // credentials accepted — reset the easter-egg counter
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
      setActiveTab('dashboard');
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
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
        setMessage(err.message, 'error');
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
        setMessage(t('app.usersLoaded'));
      }
      return items;
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
      throw err;
    } finally {
      if (!quiet) {
        setBusy(false);
      }
    }
  }

  // mergeNotif fills any missing sub-sections of a server notification-settings
  // payload with defaults (same shape the load path builds).
  function mergeNotif(result) {
    return {
      ...defaultNotificationSettings,
      ...result,
      webhook: { ...defaultNotificationSettings.webhook, ...(result?.webhook || {}) },
      telegram: { ...defaultNotificationSettings.telegram, ...(result?.telegram || {}) },
      retention: { ...defaultNotificationSettings.retention, ...(result?.retention || {}) },
      destinations: Array.isArray(result?.destinations) ? result.destinations : [],
    };
  }

  // saveNotificationDestination persists ONE destination via the per-destination
  // endpoint. The saved baseline becomes the server truth, but other sections keep
  // their in-progress local edits — only the saved destination (at `index`) is
  // reconciled to its persisted form. index -1 appends a brand-new destination.
  async function saveNotificationDestination(index, dest) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/notification/destination', {
        method: 'PUT',
        body: JSON.stringify(dest),
      });
      const savedDest = result?.destination;
      setSavedNotificationSettings(mergeNotif(result?.settings));
      setNotificationSettings((cur) => {
        const dests = [...(cur.destinations || [])];
        if (index >= 0 && index < dests.length) dests[index] = savedDest;
        else dests.push(savedDest);
        return { ...cur, destinations: dests };
      });
      setMessage(t('app.notifSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  // deleteNotificationDestination persists removal of one saved destination.
  async function deleteNotificationDestination(id) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request(`/api/settings/notification/destination/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      });
      const merged = mergeNotif(result);
      setSavedNotificationSettings(merged);
      setNotificationSettings((cur) => ({ ...cur, destinations: (cur.destinations || []).filter((d) => d.id !== id) }));
      setMessage(t('app.notifSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  // saveNotificationRetention persists ONLY the retention section; destinations
  // (saved or in-progress) are left as they are.
  async function saveNotificationRetention(retention) {
    setBusy(true);
    setMessage('');
    try {
      const result = await request('/api/settings/notification/retention', {
        method: 'PUT',
        body: JSON.stringify(retention),
      });
      const merged = mergeNotif(result);
      setSavedNotificationSettings(merged);
      setNotificationSettings((cur) => ({ ...cur, retention: merged.retention }));
      setMessage(t('app.notifSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.testNotifDispatched', { to: channel ? t('app.testNotifToFrag', { channel }) : '', channel: channel || t('app.channelWord') }));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(deleted > 0 ? t('app.purgedN', { n: deleted }) : t('app.noExpiredPurge'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
      setMessage(t('app.camHealthSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
      setMessage(t('app.machineHealthSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
      return null;
    } finally {
      setBusy(false);
    }
  }

  // calibrateCapacity benchmarks the detector on this host for an accurate
  // cold-start estimate (no cameras needed). Slow on first run (model load).
  async function calibrateCapacity() {
    setBusy(true);
    setMessage(t('app.calibrating'), 'info');
    try {
      const result = await request('/api/capacity/calibrate', { method: 'POST' });
      setCapacity(result || null);
      setMessage(t('app.calibrationComplete'));
      return result;
    } catch (err) {
      setMessage(err.message, 'error');
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
    let restartingSince = 0;
    const id = setInterval(async () => {
      if (phase === 'progress') {
        try {
          const progress = await request('/api/system/reset/progress');
          setResetProgress(progress);
          if (progress?.stage === 'failed') { clearInterval(id); return; }
          if (progress?.stage === 'restarting') { phase = 'restarting'; restartingSince = Date.now(); }
        } catch {
          // Progress endpoint unreachable/401 — the database has been wiped and the
          // server is finishing the secure overwrite + restart. Show a clean state
          // instead of leaving the bar frozen on the last DB-backed reading.
          phase = 'restarting';
          restartingSince = Date.now();
          setResetProgress({ stage: 'restarting', percent: 100, message: 'Finalizing & restarting…', running: true });
        }
        return;
      }
      // Restarting: the server relaunches ~1.5s after entering this stage. Give the old
      // process a moment to exit, then reload as soon as /health answers — matching the
      // wizard-restore restart. We deliberately do NOT wait to observe /health go "down"
      // first: a fast relaunch (or the OS queuing the socket instead of refusing it) can
      // slip between polls, and requiring that transition left the overlay stuck forever.
      if (Date.now() - restartingSince < 3500) return;
      try {
        const health = await fetch(`${apiBase()}/health`, { cache: 'no-store' });
        if (health.ok) { clearInterval(id); window.location.reload(); }
      } catch {
        // still down — keep waiting for the new process to answer
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
      setMessage(t('app.passwordUpdated'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.destSaved'));
      return merged.destinations;
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.personAlertsAdded'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      // Send credentials with the save so the backend verifies them before persisting;
      // a rejected login throws so the wizard keeps the camera un-added and shows the error.
      await request('/api/cameras/discovered', {
        method: 'POST',
        body: JSON.stringify({
          ...deviceData,
          name,
          description: '',
          username: (creds.username || '').trim(),
          password: creds.password || '',
        }),
      });
      await refresh({ quiet: true });
      setMessage(t('app.cameraAdded'));
    } catch (err) {
      setMessage(err.message, 'error');
      throw err;
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
    setMessage(t('st.restarting'), 'info');
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
    setMessage(t('st.downloadingFfmpeg'), 'info');
    try {
      await request('/api/settings/decoder/ffmpeg/install', { method: 'POST' });
      const state = await pollJob('/api/settings/decoder/ffmpeg/install/status');
      if (state?.status === 'done') {
        try { await request('/api/settings/runtime/auto-tune', { method: 'POST' }); } catch (_) {}
        setMessage(t('app.ffmpegInstalled'));
      } else {
        setMessage(t('st.ffmpegInstallFailed', { log: state?.log || 'unknown error' }), 'error');
      }
      return state;
    } catch (err) {
      setMessage(err.message, 'error');
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
    setMessage(t('app.installingAi'), 'info');
    try {
      await request('/api/training/setup-deps', { method: 'POST' });
      const state = await pollJob('/api/training/setup-deps');
      setMessage(state?.status === 'done' ? 'AI dependencies installed. Restart to apply.' : `Install failed: ${state?.log || 'unknown error'}`, state?.status === 'done' ? 'success' : 'error');
      return state;
    } catch (err) {
      setMessage(err.message, 'error');
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
    setMessage(t('app.downloadingModel'), 'info');
    try {
      const result = await request('/api/training/stock-model', { method: 'POST', body: JSON.stringify({ model }) });
      setMessage(t('app.modelSet', { model: result?.current || model }));
      return result;
    } catch (err) {
      setMessage(err.message, 'error');
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

  // Poll active teaching sessions so the camera tree can show a "learning"
  // badge wherever the user is in the app (a session keeps running server-side
  // when they navigate away from the Teach page).
  useEffect(() => {
    if (!authenticated || !isAdmin) {
      setTeachingCameraIds([]);
      return undefined;
    }
    let cancelled = false;
    const load = async () => {
      try {
        const result = await request('/api/teach/active');
        const items = Array.isArray(result) ? result : result?.items || [];
        if (!cancelled) setTeachingCameraIds(items.map((s) => Number(s.cameraId)).filter(Boolean));
      } catch (_) { /* badge is best-effort */ }
    };
    load();
    const id = window.setInterval(load, 10000);
    return () => { cancelled = true; window.clearInterval(id); };
  }, [authenticated, isAdmin]);

  // Builds the ruleId → taught-skill map from the Teach skills' stored configs
  // (each active/trained skill's config carries the auto-created rule's id).
  async function loadTaughtRuleMap() {
    try {
      const result = await request('/api/teach/skills');
      const items = Array.isArray(result) ? result : result?.items || [];
      const map = {};
      items.forEach((skill) => {
        let cfg = {};
        try { cfg = JSON.parse(skill.config || '{}'); } catch (_) { cfg = {}; }
        if (cfg.ruleId) map[cfg.ruleId] = { name: skill.name, skillType: skill.skillType };
      });
      setTaughtRuleMap(map);
    } catch (_) {
      setTaughtRuleMap({});
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
      loadTaughtRuleMap();
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
        setMessage(t('app.aiRulesLoaded'));
      }
      return { rules, alerts };
    } catch (err) {
      setMessage(err.message, 'error');
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
        setMessage(err.message, 'error');
      }
      return [];
    }
  }
  loadNotificationsRef.current = loadNotifications;

  // checkStorageCodec asks the server whether the configured at-rest recording codec
  // can actually run on this host. A re-encode codec (H.264/H.265) needs a working
  // NVENC GPU; without one the recorder silently degrades to Copy (or, if fallback is
  // off, drops segments). Either way the setting is wrong for the hardware, so we
  // raise a sticky bottom-right warning that deep-links to the storage setting.
  async function checkStorageCodec() {
    try {
      const status = await request('/api/recording/storage/status');
      if (status && status.compatible === false) {
        setCodecWarning({ codec: status.codec, fallback: status.fallbackToCopy !== false });
      } else {
        setCodecWarning(null);
      }
    } catch (_) {
      /* best-effort; a transient failure just leaves the warning as-is */
    }
  }

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

  // openNotificationsPage opens the dedicated Notifications page and refreshes its
  // data. Reached from the side-nav Notifications item and from in-view alert banners
  // (tiles, recordings); the old topbar bell + its per-entry click handler are gone.
  function openNotificationsPage() {
    setActiveTab('notifications');
    loadNotifications({ quiet: true }).catch(() => {});
    // Enrich AI rows (full alert metadata) and detect which alerts have clips.
    loadVision({ quiet: true }).catch(() => {});
    loadRecording({ quiet: true }).catch(() => {});
  }

  useEffect(() => {
    if (!authenticated) {
      return undefined;
    }
    loadVision({ quiet: true }).catch(() => {});
    loadNotifications({ quiet: true }).catch(() => {});
    checkStorageCodec();
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
    setToasts((list) => [{ id, text: message.text, kind: message.kind }, ...list].slice(0, 5));
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
      setMessage(t('app.settingsSaved'));
      // Re-check codec compatibility: the save may have changed the storage codec.
      checkStorageCodec();
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.settingsReset'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
      setMessage(t('app.captureAutoApplied'));
    } catch (err) {
      setMessage(err.message, 'error');
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
        setMessage(err.message, 'error');
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
        setMessage(t('app.configSavedWarning', { warning }));
      } else {
        setMessage(t('app.recordingConfigSaved'));
      }
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.clipDeleted'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function installVisionPackages() {
    const packages = (visionToolStatus?.installHints || []).filter((h) => !h.manual).map((h) => h.pipName);
    if (packages.length === 0) return;
    setBusy(true);
    setMessage(t('app.installingPackages'), 'info');
    setVisionInstallResult(null);
    try {
      const result = await request('/api/settings/vision/ai-tool/install', {
        method: 'POST',
        body: JSON.stringify({ packages }),
      });
      setVisionInstallResult(result || null);
      setMessage(result?.success ? 'Install succeeded. Re-checking tool status...' : 'Install finished with errors.', result?.success ? 'success' : 'error');
      if (result?.success) {
        const status = await request('/api/settings/vision/ai-tool/status');
        setVisionToolStatus(status || null);
      }
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function createUser(event) {
    event.preventDefault();
    // Validate client-side so the user gets a clear message in every environment
    // (the server hides its own validation detail behind "bad request" in prod).
    if ((newUser.password || '').length < 8) {
      setMessage(t('app.passwordMin'), 'error');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      await request('/api/settings/users', {
        method: 'POST',
        body: JSON.stringify(newUser),
      });
      setNewUser(defaultNewUser);
      await loadUsers({ quiet: true });
      setMessage(t('app.userCreated'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.userSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function resetUserPassword(user) {
    // Same client-side guard as createUser so short passwords get a clear message.
    if ((passwordDrafts[user.id] || '').length < 8) {
      setMessage(t('app.passwordMin'), 'error');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/settings/users/${user.id}/password`, {
        method: 'POST',
        body: JSON.stringify({ password: passwordDrafts[user.id] || '' }),
      });
      setPasswordDrafts((current) => ({ ...current, [user.id]: '' }));
      setMessage(t('app.passwordReset'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.userDeleted'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.ruleSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  // visionRuleToDraft maps a rule (from the API list) into the exact draft shape the
  // save endpoint (POST /api/vision/rules) expects — typed, complete fields. Used by
  // both the editor and the in-list enable/disable toggle so both post a valid body.
  function visionRuleToDraft(rule) {
    return {
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
    };
  }

  function editVisionRule(rule) {
    setVisionRuleDraft(visionRuleToDraft(rule));
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
      setMessage(t('app.ruleDeleted'));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  // toggleVisionRule flips a rule's enabled state directly from the list (no editor) by
  // re-posting the rule with isEnabled inverted — the same upsert endpoint as save.
  async function toggleVisionRule(rule) {
    setBusy(true);
    setMessage('');
    try {
      await request('/api/vision/rules', {
        method: 'POST',
        body: JSON.stringify({ ...visionRuleToDraft(rule), isEnabled: !rule.isEnabled }),
      });
      await loadVision({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.classSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.classDeleted'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.testAlertCreated'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(err.message, 'error');
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
      setMessage(t('app.alertAck'));
    } catch (err) {
      setMessage(err.message, 'error');
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
    } else if (section === 'backup') {
      // The Secure Wipe panel lives in this tab and only renders when reset is
      // allowed, so load that flag here too — otherwise it stays hidden until the
      // Machine Health tab (the only other caller) has been opened once.
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
      setMessage(t('app.devicesFound', { n: devices.length, label, newCount, savedCount }));
    } catch (err) {
      setMessage(err.message, 'error');
    } finally {
      setBusy(false);
    }
  }

  async function probe(event) {
    event.preventDefault();
    if (!manualAddress.trim()) {
      setMessage(t('app.manualAddrRequired'), 'error');
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
      setMessage(t('app.manualProbeDone'));
    } catch (err) {
      setMessage(err.message, 'error');
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
      // Credentials are verified server-side before the camera is persisted; a camera that
      // rejects the login is not saved (the add dialog stays open with the error).
      // Save returns the new camera's id so we can jump straight to its node
      // (instead of landing on the first camera in the list).
      const created = await request('/api/cameras/discovered', {
        method: 'POST',
        body: JSON.stringify({
          ...deviceData,
          name: (draft.name || '').trim() || cameraTitle(device),
          description: (draft.description || '').trim(),
          username: (draft.username || '').trim(),
          password: draft.password || '',
        }),
      });
      setMessage(t('app.cameraSaved'));
      await refresh({ quiet: true });
      const newId = Number(created?.id ?? created) || 0;
      if (newId) {
        selectCamera(newId);
      } else {
        setCameraNav('saved');
      }
    } catch (err) {
      setMessage(err.message, 'error');
      throw err;
    } finally {
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
      setMessage(t('app.cameraDetailsSaved'));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.camsResolving', { n: failed }));
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
        setMessage(t('app.camUserPassRequired'), 'error');
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
        setMessage(t('app.camCredsSaved'));
        await refresh({ quiet: true });
      }
      return result;
    } catch (err) {
      setMessage(err.message, 'error');
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

  async function movePTZ(deviceId, direction) {
    setMessage('');
    try {
      // durationMs 0 = continuous move: the camera keeps moving until an explicit
      // stop (sent on D-pad release), so press-and-hold pans/tilts smoothly.
      await request(`/api/cameras/${deviceId}/ptz/move`, {
        method: 'POST',
        body: JSON.stringify({ direction, speed: 0.35, durationMs: 0 }),
      });
    } catch (err) {
      setMessage(err.message, 'error');
    }
  }

  async function stopPTZ(deviceId) {
    setMessage('');
    try {
      await request(`/api/cameras/${deviceId}/ptz/stop`, { method: 'POST' });
    } catch (err) {
      setMessage(err.message, 'error');
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
      setMessage(t('app.rtspOptionsFound', { n: (result?.options || []).length }));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
      setBusy(false);
    }
  }

  async function testStream(device, option) {
    setBusy(true);
    setMessage('');
    try {
      // A specific detected stream (option.rtspUrl) is probed WITHOUT switching the
      // camera's active stream — recording/detection (which read the saved RTSP URL)
      // are untouched; no option = test the active stream.
      const result = await request(`/api/cameras/${device.id}/rtsp-test`, {
        method: 'POST',
        ...(option?.rtspUrl ? { body: JSON.stringify({ rtspUrl: option.rtspUrl }) } : {}),
      });
      const tracks = result.tracks || [];
      const suffix = tracks.length && !hasH264VideoTrack(tracks)
        ? ' No H264 video track; live view will use MJPEG fallback.'
        : '';
      setMessage(t('app.rtspOnline', { n: tracks.length, suffix }));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
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

  async function previewCamera(device, option) {
    // Previewing a SPECIFIC detected stream: open the modal against that URL directly.
    // The WebRTC offer carries the URL and the backend streams it under a separate
    // source ID with the camera's saved credentials — the camera's active stream (and
    // thus recording + detection) is never switched. Creds are already saved by the time
    // streams are detected (Find streams saves them), so no live-view resolve is needed.
    if (option?.rtspUrl) {
      setPreview({
        id: device.id,
        title: cameraTitle(device),
        device,
        ptzSupported: Boolean(device.ptzSupported),
        previewUrl: option.rtspUrl,
      });
      setMessage(t('app.livePreviewOpened'));
      return;
    }
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
      setMessage(t('app.livePreviewOpened'));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
      setBusy(false);
    }
  }

  async function addToViews(device, opts = {}) {
    // No capacity cap: the grid is a per-page size and paging shows any overflow,
    // so adding a camera beyond the current page just lands on a new page.
    if (viewTiles.some((tile) => tile.id === device.id)) {
      if (!opts.stay) setActiveTab('views');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      let result = device;
      try {
        result = await ensureLiveView(device);
      } catch (err) {
        setMessage(t('app.camAddedResolving'));
      }
      setViewTilesWithCookie((current) => [
        ...current,
        {
          ...tileFromDevice({ ...device, ...result }),
          title: cameraTitle(result),
          ptzSupported: Boolean(result.ptzSupported || device.ptzSupported),
        },
      ]);
      // From the camera-node Live View we toggle in place; only jump to the tiles
      // grid when the button was used elsewhere (e.g. the discover/preview flow).
      if (!opts.stay) setActiveTab('views');
      setMessage(t('app.camAdded'));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
      setBusy(false);
    } finally {
      setBusy(false);
    }
  }

  // removeFromViews takes a camera out of the live-tiles grid (the Live View toggle's
  // "remove" side). It doesn't delete the camera, just the tile.
  function removeFromViews(device) {
    const id = device?.id ?? device;
    setViewTilesWithCookie((current) => current.filter((tile) => tile.id !== id));
    setMessage(t('app.camRemovedFromViews'));
  }

  async function removeDevice(id) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/cameras/${id}`, { method: 'DELETE' });
      setMessage(t('app.cameraRemoved'));
      setViewTilesWithCookie((current) => current.filter((tile) => tile.id !== id));
      setPreview((current) => (current?.id === id ? null : current));
      await refresh({ quiet: true });
    } catch (err) {
      setMessage(err.message, 'error');
      setBusy(false);
    }
  }

  // selectTab switches the active module and runs each tab's on-enter data load
  // (previously inline in the topbar). Leaving the Cameras section clears the managed
  // camera so returning lands on the probe/discovery view.
  function selectTab(tab) {
    if (tab !== 'cameras') setManagingCameraId(null);
    setActiveTab(tab);
    if (tab === 'settings' && settingsNav === 'users') {
      loadUsers().catch(() => {});
    }
    // Cameras hosts the per-camera Settings (Recording/Stream config), AI rules, and
    // Recordings browser tabs, so refresh recording configs + vision state on entry.
    if (tab === 'cameras') {
      loadRecording({ quiet: true }).catch(() => {});
      loadVision({ quiet: true }).catch(() => {});
    }
    if (tab === 'notifications') {
      loadNotifications({ quiet: true }).catch(() => {});
      loadVision({ quiet: true }).catch(() => {});
      loadRecording({ quiet: true }).catch(() => {});
    }
  }

  // selectCameraRoot opens the Cameras page on the probe/discovery view (no camera
  // selected); selectCamera jumps straight to a saved camera's properties tabs. Both
  // are driven by the side-nav camera tree.
  function selectCameraRoot() {
    setManagingCameraId(null);
    setCameraNav('probe');
    setActiveTab('cameras');
    loadRecording({ quiet: true }).catch(() => {});
  }
  function selectCamera(cameraId) {
    setManagingCameraId(cameraId);
    setCameraNav('saved');
    setActiveTab('cameras');
    loadRecording({ quiet: true }).catch(() => {});
    checkCameraAuth(cameraId);
  }

  // checkCameraAuth verifies a camera's stored credentials so the node can gate its tabs
  // when the login has stopped working. A check error is treated as "ok" (don't block on a
  // transient failure); only a definitive "unauthorized" raises the gate.
  async function checkCameraAuth(id) {
    if (!id) return;
    try {
      const data = await request(`/api/cameras/${id}/auth-check`);
      const status = (data && data.status) || 'ok';
      setCameraAuthById((cur) => ({ ...cur, [id]: status }));
    } catch (_) {
      setCameraAuthById((cur) => ({ ...cur, [id]: 'ok' }));
    }
  }

  // unlockCamera re-enters a camera's credentials from the access gate; the backend
  // verifies them (rejecting a bad login), then we re-check so a valid login clears the gate.
  async function unlockCamera(device, creds) {
    setBusy(true);
    setMessage('');
    try {
      await request(`/api/cameras/${device.id}/credentials`, {
        method: 'POST',
        body: JSON.stringify({ username: (creds.username || '').trim(), password: creds.password || '' }),
      });
      await refresh({ quiet: true });
      await checkCameraAuth(device.id);
      setMessage(t('app.camCredsSaved'));
    } catch (err) {
      setMessage(err.message, 'error');
      throw err;
    } finally {
      setBusy(false);
    }
  }

  // Recovery gate takes priority over login: while the backend is in recovery mode nothing
  // else is reachable, so the user restores the key here before they can sign in.
  if (recoveryPending) {
    return (
      <RecoveryGatePage
        keyId={recoveryKeyId}
        busy={busy}
        restarting={recoveryRestarting}
        message={message?.text || ''}
        lang={lang}
        onLangChange={onLangChange}
        onSubmit={submitRecovery}
      />
    );
  }

  if (!authenticated) {
    return (
      <div>
        {magicWord ? (
          <MagicWordEasterEgg key={magicWordNonce} onDismiss={() => setMagicWord(false)} />
        ) : null}
        {passwordChangeRequired ? (
          <ChangePasswordPage
            busy={busy}
            message={message?.text || ''}
            lang={lang}
            onLangChange={onLangChange}
            onSubmit={completePasswordChange}
            onCancel={logout}
          />
        ) : (
          <LoginPage
            credentials={credentials}
            busy={busy}
            message={message?.text || ''}
            lockoutUntil={lockoutUntil}
            lang={lang}
            onLangChange={onLangChange}
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
        authHeader={authHeader}
        lang={lang}
        onLangChange={onLangChange}
        busy={busy}
        message={message?.text || ''}
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
    <div className={`app-shell${navPinned ? '' : ' nav-autohide'}`}>
      {wipeCountdown ? (
        <SecureWipeCountdown onCancel={() => setWipeCountdown(false)} onProceed={startSecureWipe} />
      ) : null}
      {resetProgress ? (
        <ResetProgressOverlay progress={resetProgress} onDismiss={() => setResetProgress(null)} />
      ) : null}
      <SideNav
        activeTab={activeTab}
        isAdmin={isAdmin}
        busy={busy}
        cameras={saved}
        managingCameraId={managingCameraId}
        teachingCameraIds={teachingCameraIds}
        notifUnread={notifUnread}
        onTab={selectTab}
        onSelectCameraRoot={selectCameraRoot}
        onSelectCamera={selectCamera}
        onLogout={logout}
        pinned={navPinned}
        onTogglePinned={toggleNavPinned}
      />
      <main className="main-workspace">
        <WorkspaceHeader
          lang={lang}
          onLangChange={onLangChange}
          theme={theme}
          onThemeChange={changeTheme}
        />
        <ToastStack toasts={toasts} onDismiss={(id) => setToasts((list) => list.filter((t) => t.id !== id))} />

        {codecWarning ? (
          <div
            className="codec-warning-toast"
            role="alert"
            tabIndex={0}
            title={t('app.codecWarnHint')}
            onClick={() => { setActiveTab('settings'); openSettingsSection('runtime'); }}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { setActiveTab('settings'); openSettingsSection('runtime'); } }}
          >
            <span className="codec-warning-icon" aria-hidden="true">⚠</span>
            <div className="codec-warning-text">
              <strong>{t('app.codecWarnTitle')}</strong>
              <span>{codecWarning.fallback ? t('app.codecWarnBodyFallback') : t('app.codecWarnBodyDrop')}</span>
            </div>
            <button
              type="button"
              className="codec-warning-close"
              aria-label={t('app.dismiss')}
              onClick={(e) => { e.stopPropagation(); setCodecWarning(null); }}
            >
              ×
            </button>
          </div>
        ) : null}

      {activeTab === 'dashboard' ? (
        <DashboardTab
          authHeader={authHeader}
          saved={saved}
          refreshSignal={notifVersion}
          onMessage={setMessage}
        />
      ) : null}

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
          selectedSavedId={managingCameraId}
          onSelectSaved={selectCamera}
          cameraAuth={cameraAuthById}
          onUnlockCamera={unlockCamera}
          preview={preview}
          authHeader={authHeader}
          streamConfig={streamConfig}
          detailDraftsById={deviceDrafts}
          credentialsById={deviceCredentials}
          streamOptionsById={streamOptionsById}
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
          onSaveCredentials={saveCredentials}
          onResolve={resolveStream}
          onTest={testStream}
          onPreview={previewCamera}
          onAddToViews={addToViews}
          onRemoveFromViews={removeFromViews}
          viewTileIds={viewTiles.map((tile) => tile.id)}
          onPTZMove={movePTZ}
          onPTZStop={stopPTZ}
          onRemove={removeDevice}
          onClosePreview={() => setPreview(null)}
          recordingConfigs={recordingConfigs}
          onSaveRecordingConfig={saveRecordingConfig}
          onMessage={setMessage}
          canManage={isAdmin}
          ai={{
            rules: visionRules,
            alerts: visionAlerts,
            classes: visionClasses,
            activeModelClasses,
            taughtByRuleId: taughtRuleMap,
            destinations: notificationSettings.destinations,
            ruleDraft: visionRuleDraft,
            onRuleDraft: setVisionRuleDraft,
            onSaveRule: saveVisionRule,
            onEditRule: editVisionRule,
            onDeleteRule: deleteVisionRule,
            onToggleRule: toggleVisionRule,
            onTriggerTestAlert: triggerTestAlert,
            onAcknowledgeAlert: acknowledgeAlert,
            onPrepareCamera: prepareVisionLiveView,
            onReload: () => loadVision(),
          }}
          recordings={{
            alerts: visionAlerts,
            unacknowledgedAlertIds,
            onDeleteSegment: deleteRecordingSegment,
            onPurgeExpired: purgeExpiredRecordings,
            onAcknowledgeAlert: acknowledgeAlert,
            onReload: () => loadRecording(),
          }}
        />
      ) : null}

      {activeTab === 'teach' ? (
        <TeachTab
          authHeader={authHeader}
          onMessage={setMessage}
          cameras={saved}
          streamConfig={streamConfig}
          labelCatalog={visionLabels}
          onOpenModels={() => { setActiveTab('settings'); openSettingsSection('ai'); }}
        />
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
          savedNotificationSettings={savedNotificationSettings}
          notificationHasChanges={JSON.stringify(notificationSettings) !== JSON.stringify(savedNotificationSettings)}
          onNotificationChange={setNotificationSettings}
          onSaveDestination={saveNotificationDestination}
          onDeleteDestination={deleteNotificationDestination}
          onSaveRetention={saveNotificationRetention}
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
          onModelActivated={() => { loadVision({ quiet: true }); loadActiveModelClasses(); }}
          visionClasses={visionClasses}
          visionLabels={visionLabels}
          activeModelClasses={activeModelClasses}
          onSaveClass={saveVisionClass}
          onDeleteClass={deleteVisionClass}
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
          refreshSignal={notifVersion}
          onAcknowledgeAlert={acknowledgeAlert}
          onMarkRead={markNotificationRead}
          onMessage={setMessage}
          onOpenSettingsSection={(section) => { setActiveTab('settings'); openSettingsSection(section); }}
          onOpenUser={(username) => { setFocusUsername(username); setActiveTab('settings'); openSettingsSection('users'); }}
        />
      ) : null}

        <AppFooter appName="MyMataSan" apiBase={apiBase()} authHeader={authHeader} />
      </main>
    </div>
  );
}

const LANG_KEY = 'mymatasan_lang';

// App owns the active locale and wraps the tree in the shared LangProvider so the
// shared toast/icons (and future translated app strings) follow it. Persisted in
// localStorage like the theme; default browser language → English.
export default function App() {
  const [lang, setLang] = useState(() => {
    try { return normalizeLang(localStorage.getItem(LANG_KEY) || navigator.language); } catch (_) { return 'en'; }
  });
  const changeLang = useCallback((l) => {
    setLang(l);
    try { localStorage.setItem(LANG_KEY, l); } catch (_) {}
  }, []);
  return (
    <LangProvider lang={lang} messages={appMessages}>
      <AppInner lang={lang} onLangChange={changeLang} />
    </LangProvider>
  );
}

