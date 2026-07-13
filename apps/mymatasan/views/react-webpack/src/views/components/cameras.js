import { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { Ico } from './icons';
import { Tabs } from '@shared/Tabs';
import { CameraHero, statusTone } from '@shared/CameraHero';
import { useT } from '@shared/i18n';
import { FormBusyOverlay, InfoButton, Tracks, LayoutDropdown } from './ui';
import { CameraAiPanel } from './vision';
import { defaultDeviceCredentials } from '../lib/constants';
import {apiBase,fieldValue,formatTimestamp,cameraTitle,cameraDescription,orderedSavedCameras,sameCamera,streamOptionLabel,layoutCapacity,layoutColumns,layoutRows,fetchTalkCapability,saveTalkPassword } from '../lib/helpers';
import { LiveViewport } from './previews';
import { PasswordField } from './layout';
import { CameraRecordingConfig, CameraStreamConfig, CameraRecordingsPanel } from './recording';

// healthPillProps maps a camera's live health status into a pill class and label.
// The status-pill online/offline/unknown classes are shared with the RTSP pill.
function healthPillProps(status) {
  switch ((status || '').toLowerCase()) {
    case 'online':
      return { cls: 'online', labelKey: 'cam.online' };
    case 'offline':
      return { cls: 'offline', labelKey: 'cam.offline' };
    default:
      return { cls: 'unknown', labelKey: 'cam.unknown' };
  }
}

// HealthPill shows the camera's network reachability as decided by the health monitor.
export function HealthPill({ device }) {
  const t = useT();
  const { cls, labelKey } = healthPillProps(device.healthStatus);
  const checked = device.lastHealthCheckAt ? formatTimestamp(device.lastHealthCheckAt) : '';
  return (
    <strong className={`status-pill ${cls}`} title={checked ? t('cam.lastChecked', { time: checked }) : t('cam.notChecked')}>
      {t(labelKey)}
    </strong>
  );
}

export function DeviceMeta({ device }) {
  const t = useT();
  return (
    <dl className="meta-grid">
      <div>
        <dt>{t('cam.host')}</dt>
        <dd>{fieldValue(device.host)}</dd>
      </div>
      <div>
        <dt>{t('cam.port')}</dt>
        <dd>{fieldValue(device.port)}</dd>
      </div>
      <div>
        <dt>{t('cam.model')}</dt>
        <dd>{fieldValue(device.model)}</dd>
      </div>
      <div>
        <dt>{t('cam.serial')}</dt>
        <dd>{fieldValue(device.serialNumber)}</dd>
      </div>
      <div>
        <dt>{t('cam.health')}</dt>
        <dd>{t(healthPillProps(device.healthStatus).labelKey)}</dd>
      </div>
      <div>
        <dt>{t('cam.lastCheckedLabel')}</dt>
        <dd>{device.lastHealthCheckAt ? formatTimestamp(device.lastHealthCheckAt) : '-'}</dd>
      </div>
    </dl>
  );
}

export function DeviceDescription({ device }) {
  const description = cameraDescription(device);
  if (!description) {
    return null;
  }
  return <p className="device-description">{description}</p>;
}

export function OnvifDetails({ device }) {
  const t = useT();
  return (
    <section className="capability-panel">
      <header>
        <h4>{t('cam.onvifInfo')}</h4>
        <strong className={`status-pill ${device.ptzSupported ? 'online' : 'unknown'}`}>
          {device.ptzSupported ? t('cam.ptzSupported') : t('cam.ptzNotDetected')}
        </strong>
      </header>
      <dl className="capability-grid">
        <div>
          <dt>{t('cam.manufacturer')}</dt>
          <dd>{fieldValue(device.manufacturer)}</dd>
        </div>
        <div>
          <dt>{t('cam.firmware')}</dt>
          <dd>{fieldValue(device.firmwareVersion)}</dd>
        </div>
        <div>
          <dt>{t('cam.hardwareId')}</dt>
          <dd>{fieldValue(device.hardwareId)}</dd>
        </div>
        <div>
          <dt>{t('cam.mediaService')}</dt>
          <dd>{fieldValue(device.mediaXAddr)}</dd>
        </div>
        <div>
          <dt>{t('cam.ptzService')}</dt>
          <dd>{fieldValue(device.ptzXAddr)}</dd>
        </div>
        <div>
          <dt>{t('cam.profileToken')}</dt>
          <dd>{fieldValue(device.profileToken)}</dd>
        </div>
        <div>
          <dt>{t('cam.snapshotUri')}</dt>
          <dd>{fieldValue(device.snapshotUri)}</dd>
        </div>
        <div>
          <dt>{t('cam.rtspTransport')}</dt>
          <dd>{fieldValue(device.rtspTransport)}</dd>
        </div>
        <div>
          <dt>{t('cam.types')}</dt>
          <dd>{fieldValue(device.types)}</dd>
        </div>
        <div>
          <dt>{t('cam.scopes')}</dt>
          <dd>{fieldValue(device.scopes)}</dd>
        </div>
      </dl>
    </section>
  );
}

export function ViewsTab({
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
  const t = useT();
  const tileCount = layoutCapacity(layout);
  const columns = layoutColumns(layout);
  const rows = layoutRows(layout);

  // The grid size is a per-page size, not a cap: all selected cameras are kept
  // and shown `tileCount` at a time, so changing the grid re-paginates instead of
  // dropping cameras.
  const pageCount = Math.max(1, Math.ceil(viewTiles.length / tileCount));
  const [page, setPage] = useState(0);
  useEffect(() => {
    if (page > pageCount - 1) {
      setPage(Math.max(0, pageCount - 1));
    }
  }, [page, pageCount]);
  // Left/Right arrows page through cameras — the only way to page while in
  // fullscreen, where the toolbar pager is hidden. Ignored while typing.
  useEffect(() => {
    function onKey(event) {
      const tag = event.target?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
        return;
      }
      if (event.key === 'ArrowRight') {
        setPage((p) => Math.min(pageCount - 1, p + 1));
      } else if (event.key === 'ArrowLeft') {
        setPage((p) => Math.max(0, p - 1));
      }
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [pageCount]);
  const safePage = Math.min(page, pageCount - 1);
  const pageStart = safePage * tileCount;
  const tiles = [...viewTiles.slice(pageStart, pageStart + tileCount)];
  while (tiles.length < tileCount) {
    tiles.push(null);
  }
  const available = devices.filter((device) => !viewTiles.some((tile) => tile?.id === device.id));

  // Fullscreen: enter on the Views workspace via the Fullscreen API. While
  // fullscreen, all chrome is hidden (see .views-fullscreen CSS) and the grid is
  // sized rows × cols to fit the screen with no scrolling. Esc exits natively.
  const workspaceRef = useRef(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  useEffect(() => {
    const onChange = () => {
      const fsEl = document.fullscreenElement || document.webkitFullscreenElement;
      setIsFullscreen(fsEl === workspaceRef.current);
    };
    document.addEventListener('fullscreenchange', onChange);
    document.addEventListener('webkitfullscreenchange', onChange);
    return () => {
      document.removeEventListener('fullscreenchange', onChange);
      document.removeEventListener('webkitfullscreenchange', onChange);
    };
  }, []);
  function toggleFullscreen() {
    const fsEl = document.fullscreenElement || document.webkitFullscreenElement;
    if (fsEl) {
      (document.exitFullscreen || document.webkitExitFullscreen)?.call(document);
    } else {
      const el = workspaceRef.current;
      if (el) {
        (el.requestFullscreen || el.webkitRequestFullscreen)?.call(el);
      }
    }
  }

  // Maximize a single tile to fill the whole grid (and the whole screen when in
  // fullscreen). The header maximize/restore button drives it in normal mode;
  // double-clicking a tile toggles it too, which is the only control available in
  // fullscreen where the headers are hidden.
  const [maximizedTileId, setMaximizedTileId] = useState(null);
  // Ignore a stale id if its tile was removed or filtered out by a layout change.
  const maximizedId = viewTiles.some((tile) => tile?.id === maximizedTileId) ? maximizedTileId : null;
  function toggleMaximize(id) {
    setMaximizedTileId((current) => (current === id ? null : id));
  }

  return (
    <section className={`workspace${isFullscreen ? ' views-fullscreen' : ''}`} ref={workspaceRef}>
      <div className="toolbar">
        <div className="segmented">
          <LayoutDropdown layout={layout} onLayout={onLayout} />
          {pageCount > 1 ? (
            <div className="view-pager" role="group" aria-label={t('cam.liveViewPages')}>
              <button type="button" className="quiet" onClick={() => setPage(safePage - 1)} disabled={safePage <= 0} aria-label={t('cam.prevPage')}>
                <Ico n="arr-left" sz={14} />
              </button>
              <span className="view-pager-label">{safePage + 1} / {pageCount}</span>
              <button type="button" className="quiet" onClick={() => setPage(safePage + 1)} disabled={safePage >= pageCount - 1} aria-label={t('cam.nextPage')}>
                <Ico n="arr-right" sz={14} />
              </button>
            </div>
          ) : null}
          <button type="button" className="quiet" onClick={toggleFullscreen} title={t('cam.fullscreenTitle')}>
            <span className="btn-icon"><Ico n="maximize" /> {t('cam.fullscreen')}</span>
          </button>
        </div>
        <div className="add-strip">
          {available.length === 0 ? <span>{t('cam.noCamerasAvailable')}</span> : null}
          {available.map((device) => (
            <button type="button" className="quiet" key={device.id} disabled={busy} onClick={() => onAdd(device)}>
              <span className="btn-icon"><Ico n="plus" /> {cameraTitle(device)}</span>
            </button>
          ))}
        </div>
      </div>

      <div className={`view-grid${maximizedId ? ' has-maximized' : ''}`} style={{ '--view-cols': columns, '--view-rows': rows }}>
        {tiles.map((tile, idx) => {
          const tileAlerts = tile ? alertsByCamera.get(Number(tile.id)) || [] : [];
          const latestAlert = tileAlerts[0] || null;
          // Resolve the camera's live health so the tile can short-circuit to an
          // "Offline" placeholder instead of waiting on a dead stream's timeouts.
          const tileDevice = tile ? devices.find((device) => Number(device.id) === Number(tile.id)) : null;
          return (
          <article
            className={[
              'view-tile',
              tile && draggedTileId === tile.id ? 'dragging' : '',
              tileAlerts.length > 0 ? 'has-ai-alert' : '',
              tile && maximizedId === tile.id ? 'maximized' : '',
            ].filter(Boolean).join(' ')}
            key={tile ? tile.id : `empty-${idx}`}
            onDoubleClick={() => tile && toggleMaximize(tile.id)}
            draggable={Boolean(tile)}
            onDragStart={(event) => {
              if (!tile) {
                return;
              }
              event.dataTransfer.effectAllowed = 'move';
              event.dataTransfer.setData('text/plain', String(pageStart + idx));
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
              onMove(from, pageStart + idx);
              onDragTile(null);
            }}
          >
            {tile ? (
              <>
                <div className="tile-header">
                  <span className="drag-handle" title={t('cam.dragReorder')} aria-label={t('cam.dragReorder')}>
                    ::
                  </span>
                  <strong>{tile.title}</strong>
                  {tileAlerts.length > 0 ? (
                    <button
                      type="button"
                      className="tile-alert-pill"
                      onClick={() => onOpenAlerts(tile.id)}
                      aria-label={t('cam.aiAlertsFor', { n: tileAlerts.length, title: tile.title })}
                    >
                      {t('cam.aiCount', { n: tileAlerts.length })}
                    </button>
                  ) : null}
                  <button
                    type="button"
                    className="icon-button"
                    onClick={() => toggleMaximize(tile.id)}
                    aria-label={maximizedId === tile.id ? t('cam.restoreTile') : t('cam.maximizeTile')}
                    title={maximizedId === tile.id ? t('cam.restore') : t('cam.maximize')}
                  >
                    <Ico n={maximizedId === tile.id ? 'minimize' : 'maximize'} sz={12} />
                  </button>
                  <button type="button" className="icon-button" onClick={() => onRemove(tile.id)} aria-label={t('cam.removeLiveView')}>
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
                  healthStatus={tileDevice?.healthStatus}
                  streamKey={`${tile.rtspUrl || ''}:${tile.rtspTracks || ''}`}
                  startDelayMs={idx * 700}
                />
                {latestAlert ? (
                  <button type="button" className="tile-ai-banner" onClick={() => onOpenAlerts(tile.id)}>
                    <strong>{latestAlert.label || latestAlert.detectionType || t('cam.aiEvent')}</strong>
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
              <div className="empty-tile">{t('cam.empty')}</div>
            )}
          </article>
          );
        })}
      </div>
    </section>
  );
}

export function DiscoveredDevices({ devices, saved, busy, onSave }) {
  const t = useT();
  const [savedExpanded, setSavedExpanded] = useState(false);
  // The device whose "Add" credential dialog is open (credentials are verified before the
  // camera can be saved), or null when no dialog is showing.
  const [addingDevice, setAddingDevice] = useState(null);
  const notSavedDevices = devices.filter((device) => !saved.some((savedDevice) => sameCamera(device, savedDevice)));
  const savedDevices = devices.filter((device) => saved.some((savedDevice) => sameCamera(device, savedDevice)));

  useEffect(() => {
    setSavedExpanded(false);
  }, [devices]);

  function renderUnsaved(device) {
    const key = device.xAddr || `${device.host}:${device.port}`;
    return (
      <article className="device-card" key={key}>
        <div className="device-title-row">
          <div>
            <h3>{cameraTitle(device)}</h3>
            <p>{device.xAddr}</p>
          </div>
          <button type="button" onClick={() => setAddingDevice(device)} disabled={busy}>
            <span className="btn-icon"><Ico n="plus" /> {t('cam.addCamera')}</span>
          </button>
        </div>
        <DeviceMeta device={device} />
        {device._discoveryMethods && device._discoveryMethods.length > 0 ? (
          <div className="discovery-method-badges">
            {device._discoveryMethods.map((m) => (
              <span key={m} className="discovery-method-badge">{m}</span>
            ))}
            {device._openPorts && device._openPorts.length > 0 ? (
              <span className="discovery-ports">{t('cam.ports', { ports: device._openPorts.join(', ') })}</span>
            ) : null}
          </div>
        ) : null}
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
          <strong className="status-pill saved">{t('cam.saved')}</strong>
        </div>
        <DeviceMeta device={device} />
      </article>
    );
  }

  return (
    <section className="device-section">
      <header>
        <h2>{t('cam.discovered')}</h2>
        <span>{devices.length}</span>
      </header>
      <div className="discovery-groups">
        {devices.length === 0 ? <p className="empty">{t('cam.noDiscovered')}</p> : null}
        {notSavedDevices.length > 0 ? (
          <section className="discovery-group">
            <header>
              <h3>{t('cam.notSaved')}</h3>
              <span className="discovery-group-count">{notSavedDevices.length}</span>
            </header>
            <div className="device-list compact">{notSavedDevices.map(renderUnsaved)}</div>
          </section>
        ) : null}
        {savedDevices.length > 0 ? (
          <section className="discovery-group">
            <header>
              <button
                type="button"
                className="discovery-group-toggle"
                aria-expanded={savedExpanded}
                aria-label={savedExpanded ? t('cam.collapse') : t('cam.expand')}
                title={savedExpanded ? t('cam.collapse') : t('cam.expand')}
                onClick={() => setSavedExpanded((current) => !current)}
              >
                <Ico n={savedExpanded ? 'chev-up' : 'chev-down'} sz={16} />
                <h3>{t('cam.saved')}</h3>
              </button>
              <span className="discovery-group-count">{savedDevices.length}</span>
            </header>
            {savedExpanded ? <div className="device-list compact">{savedDevices.map(renderSaved)}</div> : null}
          </section>
        ) : null}
      </div>
      {addingDevice ? (
        <AddCameraDialog
          device={addingDevice}
          busy={busy}
          onCancel={() => setAddingDevice(null)}
          onSave={onSave}
        />
      ) : null}
    </section>
  );
}

// AddCameraDialog collects a name + login for a discovered camera and verifies the
// credentials on save. `onSave` throws when the camera rejects the login, so the dialog
// stays open showing the error; on success the parent view unmounts it (navigates to Saved).
function AddCameraDialog({ device, busy, onCancel, onSave }) {
  const t = useT();
  const [name, setName] = useState(() => cameraTitle(device));
  const [description, setDescription] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSaving(true);
    setError('');
    try {
      await onSave(device, { name: name.trim(), description: description.trim(), username: username.trim(), password });
    } catch (err) {
      setError(err?.message || t('cam.authFailed'));
      setSaving(false);
    }
  }

  return (
    <div
      className="preview-overlay"
      role="dialog"
      aria-modal="true"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onCancel(); }}
    >
      <div className="preview-dialog add-camera-dialog">
        <header className="preview-dialog-head">
          <h3>{t('cam.addDialogTitle')}</h3>
          <button type="button" className="quiet icon-only" onClick={onCancel} aria-label={t('common.close')}>
            <Ico n="x" />
          </button>
        </header>
        <form className="add-camera-form" onSubmit={submit}>
          <p className="field-hint">{cameraTitle(device)} · {device.xAddr || device.host}</p>
          <label>
            {t('cam.cameraName')}
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
          </label>
          <label>
            {t('common.description')}
            <input value={description} onChange={(e) => setDescription(e.target.value)} autoComplete="off" />
          </label>
          <div className="credential-row">
            <label>
              {t('cam.credUsername')}
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder="admin" />
            </label>
            <label>
              {t('cam.credPassword')}
              <PasswordField value={password} onChange={setPassword} autoComplete="off" />
            </label>
          </div>
          <p className="field-hint">{t('cam.addDialogHint')}</p>
          {error ? <p className="field-hint danger-text">{error}</p> : null}
          <div className="add-camera-actions">
            <button type="button" className="quiet" onClick={onCancel} disabled={saving}>{t('common.cancel')}</button>
            <button type="submit" disabled={saving || busy}>
              <span className="btn-icon"><Ico n="shield" /> {saving ? t('cam.verifying') : t('cam.verifyAndSave')}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// CameraUsers manages the camera's local ONVIF user accounts (Device Management
// GetUsers/CreateUsers/DeleteUsers). The list is loaded on demand — the ONVIF call hits
// the camera, so it isn't auto-fetched every time the Access panel opens.
function CameraUsers({ device, busy, authHeader, canManage }) {
  const t = useT();
  const cameraId = Number(device?.id) || 0;
  const [users, setUsers] = useState(null); // null = not loaded yet
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [draft, setDraft] = useState({ username: '', password: '', userLevel: 'User' });
  const [pwFor, setPwFor] = useState(null); // username whose password is being changed
  const [pwValue, setPwValue] = useState('');

  useEffect(() => {
    setUsers(null);
    setError('');
    setDraft({ username: '', password: '', userLevel: 'User' });
    setPwFor(null);
    setPwValue('');
  }, [cameraId]);

  const resultOf = (payload) => payload?.data?.result ?? payload?.result ?? payload;
  const errOf = (payload, status) => payload?.message || payload?.error || `${status}`;

  const load = useCallback(async () => {
    if (!cameraId) return;
    setLoading(true);
    setError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/onvif-users`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(errOf(payload, resp.status));
      const result = resultOf(payload);
      setUsers(Array.isArray(result) ? result : []);
    } catch (e) {
      setError(e.message || 'failed');
      setUsers([]);
    } finally {
      setLoading(false);
    }
  }, [cameraId, authHeader]);

  async function addUser(event) {
    event.preventDefault();
    if (!draft.username.trim() || !draft.password) return;
    setSaving(true);
    setError('');
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/onvif-users`, {
        method: 'POST', credentials: 'include', headers, body: JSON.stringify(draft),
      });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(errOf(payload, resp.status));
      const result = resultOf(payload);
      setUsers(Array.isArray(result) ? result : users);
      setDraft({ username: '', password: '', userLevel: 'User' });
    } catch (e) {
      setError(e.message || 'failed');
    } finally {
      setSaving(false);
    }
  }

  async function changePassword(user) {
    if (!pwValue) return;
    setSaving(true);
    setError('');
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/camera-password`, {
        method: 'POST', credentials: 'include', headers,
        body: JSON.stringify({ targetUsername: user.username, newPassword: pwValue, userLevel: user.userLevel || 'User' }),
      });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(errOf(payload, resp.status));
      setPwFor(null);
      setPwValue('');
    } catch (e) {
      setError(e.message || 'failed');
    } finally {
      setSaving(false);
    }
  }

  async function removeUser(username) {
    if (!window.confirm(t('cam.removeUserConfirm', { name: username }))) return;
    setSaving(true);
    setError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/onvif-users/${encodeURIComponent(username)}`, {
        method: 'DELETE', credentials: 'include', headers,
      });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(errOf(payload, resp.status));
      const result = resultOf(payload);
      setUsers(Array.isArray(result) ? result : (users || []).filter((u) => u.username !== username));
    } catch (e) {
      setError(e.message || 'failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="camera-users">
      <hr className="saved-tab-divider" />
      <div className="camera-users-head">
        <div>
          <strong>{t('cam.cameraUsers')}</strong>
          <span className="field-hint">{t('cam.usersIntro')}</span>
        </div>
        <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
          <span className="btn-icon"><Ico n="refresh" /> {users === null ? t('cam.loadUsers') : t('common.refresh')}</span>
        </button>
      </div>
      {error ? <p className="field-hint danger-text">{error}</p> : null}
      {loading ? <p className="empty-hint">{t('common.loading')}</p> : null}
      {users !== null && !loading ? (
        users.length === 0 ? (
          <p className="empty-hint">{t('cam.noUsers')}</p>
        ) : (
          <ul className="user-list">
            {users.map((u) => (
              <li key={u.username} className="user-row">
                <div className="user-row-main">
                  <span className="user-name"><Ico n="user" sz={14} /> {u.username}</span>
                  <span className="user-level">{u.userLevel || '-'}</span>
                  {canManage ? (
                    <>
                      <button type="button" className="quiet" onClick={() => { setPwFor(pwFor === u.username ? null : u.username); setPwValue(''); }} disabled={saving || busy} title={t('cam.changeUserPassword')} aria-label={t('cam.changeUserPassword')}>
                        <Ico n="key" sz={13} />
                      </button>
                      <button type="button" className="quiet danger-text" onClick={() => removeUser(u.username)} disabled={saving || busy} title={t('common.delete')} aria-label={t('common.delete')}>
                        <Ico n="trash" sz={13} />
                      </button>
                    </>
                  ) : null}
                </div>
                {canManage && pwFor === u.username ? (
                  <form className="user-pw-form" onSubmit={(e) => { e.preventDefault(); changePassword(u); }}>
                    <PasswordField value={pwValue} onChange={setPwValue} autoComplete="new-password" placeholder={t('cam.newUserPassword')} />
                    <button type="submit" className="quiet" disabled={!pwValue || saving}>{t('common.save')}</button>
                    <button type="button" className="quiet" onClick={() => setPwFor(null)}>{t('common.cancel')}</button>
                  </form>
                ) : null}
              </li>
            ))}
          </ul>
        )
      ) : null}
      {canManage ? (
        <form className="camera-users-add" onSubmit={addUser}>
          <div className="credential-row">
            <label>
              {t('cam.newUserName')}
              <input value={draft.username} onChange={(e) => setDraft({ ...draft, username: e.target.value })} autoComplete="off" />
            </label>
            <label>
              {t('cam.newUserPassword')}
              <PasswordField value={draft.password} onChange={(password) => setDraft({ ...draft, password })} autoComplete="new-password" />
            </label>
          </div>
          <div className="credential-row">
            <label>
              {t('cam.userRole')}
              <select value={draft.userLevel} onChange={(e) => setDraft({ ...draft, userLevel: e.target.value })}>
                <option value="Administrator">{t('cam.roleAdmin')}</option>
                <option value="Operator">{t('cam.roleOperator')}</option>
                <option value="User">{t('cam.roleUser')}</option>
              </select>
            </label>
          </div>
          <div className="action-row">
            <button type="submit" className="quiet" disabled={saving || busy || !draft.username.trim() || !draft.password}>
              <span className="btn-icon"><Ico n="user-plus" /> {t('cam.addUser')}</span>
            </button>
          </div>
        </form>
      ) : null}
    </div>
  );
}

// CameraMaintenance exposes ONVIF Device-Management maintenance: reboot + factory reset
// (soft keeps network config, hard wipes everything). Destructive → confirm dialogs.
function CameraMaintenance({ device, busy, authHeader, canManage, onMessage }) {
  const t = useT();
  const cameraId = Number(device?.id) || 0;
  const [working, setWorking] = useState('');
  const notify = (msg, kind) => { if (onMessage) onMessage(msg, kind); };
  async function call(path, body, confirmMsg, okMsg) {
    if (confirmMsg && !window.confirm(confirmMsg)) return;
    setWorking(path);
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/${path}`, {
        method: 'POST', credentials: 'include', headers, body: body ? JSON.stringify(body) : undefined,
      });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      notify(okMsg);
    } catch (e) {
      notify(e.message || 'failed', 'error');
    } finally {
      setWorking('');
    }
  }
  const disabled = !canManage || busy || Boolean(working);
  return (
    <section className="settings-box danger-zone-box">
      <header><h3>{t('cam.maintenance')}</h3></header>
      <div className="settings-box-body">
        <p className="danger-zone-hint">{t('cam.maintenanceHint')}</p>
        <div className="action-row">
          <button type="button" className="quiet" disabled={disabled} onClick={() => call('reboot', null, t('cam.rebootConfirm'), t('cam.rebootStarted'))}>
            <span className="btn-icon"><Ico n="reload" /> {t('cam.reboot')}</span>
          </button>
          <button type="button" className="quiet danger-text" disabled={disabled} onClick={() => call('factory-default', { hard: false }, t('cam.softResetConfirm'), t('cam.resetStarted'))}>
            <span className="btn-icon"><Ico n="undo" /> {t('cam.softReset')}</span>
          </button>
          <button type="button" className="danger-solid" disabled={disabled} onClick={() => call('factory-default', { hard: true }, t('cam.hardResetConfirm'), t('cam.resetStarted'))}>
            <span className="btn-icon"><Ico n="warning" /> {t('cam.hardReset')}</span>
          </button>
        </div>
      </div>
    </section>
  );
}

// CameraTime reads/sets the camera clock (ONVIF Get/SetSystemDateAndTime). Loaded on
// demand; Manual mode syncs the browser's current time as the camera's UTC clock.
// Common POSIX/ONVIF time-zone offsets for the Time dropdown (the "GMT±HH:MM" format the
// cameras report). Kept as plain offsets so we don't need a full tz database.
const TIMEZONE_OPTIONS = [
  'GMT-12:00', 'GMT-11:00', 'GMT-10:00', 'GMT-09:30', 'GMT-09:00', 'GMT-08:00', 'GMT-07:00',
  'GMT-06:00', 'GMT-05:00', 'GMT-04:00', 'GMT-03:30', 'GMT-03:00', 'GMT-02:00', 'GMT-01:00',
  'GMT+00:00', 'GMT+01:00', 'GMT+02:00', 'GMT+03:00', 'GMT+03:30', 'GMT+04:00', 'GMT+04:30',
  'GMT+05:00', 'GMT+05:30', 'GMT+05:45', 'GMT+06:00', 'GMT+06:30', 'GMT+07:00', 'GMT+08:00',
  'GMT+08:45', 'GMT+09:00', 'GMT+09:30', 'GMT+10:00', 'GMT+10:30', 'GMT+11:00', 'GMT+12:00',
  'GMT+12:45', 'GMT+13:00', 'GMT+14:00',
];

function CameraTime({ device, busy, authHeader, canManage, onMessage }) {
  const t = useT();
  const cameraId = Number(device?.id) || 0;
  const [info, setInfo] = useState(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState({ dateTimeType: 'NTP', daylightSavings: false, timeZone: '', ntpFromDhcp: false, ntpServers: '' });
  const notify = (msg, kind) => { if (onMessage) onMessage(msg, kind); };

  useEffect(() => { setInfo(null); setError(''); }, [cameraId]);

  const load = useCallback(async () => {
    if (!cameraId) return;
    setLoading(true);
    setError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/datetime`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setInfo(result || {});
      if (result) setDraft({ dateTimeType: result.dateTimeType || 'NTP', daylightSavings: !!result.daylightSavings, timeZone: result.timeZone || '', ntpFromDhcp: !!result.ntpFromDhcp, ntpServers: (result.ntpServers || []).join(', ') });
    } catch (e) {
      setError(e.message || 'failed');
      setInfo(null);
    } finally {
      setLoading(false);
    }
  }, [cameraId, authHeader]);

  async function save() {
    setSaving(true);
    setError('');
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const body = {
        dateTimeType: draft.dateTimeType,
        daylightSavings: draft.daylightSavings,
        timeZone: draft.timeZone,
        utcDateTime: '',
        ntpFromDhcp: draft.ntpFromDhcp,
        ntpServers: draft.ntpServers.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean),
      };
      if (draft.dateTimeType === 'Manual') body.utcDateTime = new Date().toISOString();
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/datetime`, { method: 'POST', credentials: 'include', headers, body: JSON.stringify(body) });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      const result = payload?.data?.result ?? payload?.result ?? payload;
      if (result && result.dateTimeType) setInfo(result);
      notify(t('cam.timeSaved'));
    } catch (e) {
      setError(e.message || 'failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="settings-box">
      <header><h3>{t('cam.timeSettings')}</h3></header>
      <div className="settings-box-body">
        <div className="camera-users-head">
          <span className="field-hint">
            {info?.utcDateTime ? t('cam.cameraTimeNow', { time: formatTimestamp(Math.floor(new Date(info.utcDateTime).getTime() / 1000)) }) : t('cam.timeHint')}
          </span>
          <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
            <span className="btn-icon"><Ico n="refresh" /> {info === null ? t('cam.loadSettings') : t('common.refresh')}</span>
          </button>
        </div>
        {error ? <p className="field-hint danger-text">{error}</p> : null}
        {info !== null ? (
          <>
            <div className="metadata-row">
              <label>
                {t('cam.timeMode')}
                <select value={draft.dateTimeType} onChange={(e) => setDraft({ ...draft, dateTimeType: e.target.value })} disabled={!canManage}>
                  <option value="NTP">{t('cam.timeNtp')}</option>
                  <option value="Manual">{t('cam.timeManual')}</option>
                </select>
              </label>
              <label>
                {t('cam.timeZone')}
                <select value={draft.timeZone} onChange={(e) => setDraft({ ...draft, timeZone: e.target.value })} disabled={!canManage}>
                  <option value="">{t('cam.timeZonePick')}</option>
                  {draft.timeZone && !TIMEZONE_OPTIONS.includes(draft.timeZone) ? (
                    <option value={draft.timeZone}>{draft.timeZone}</option>
                  ) : null}
                  {TIMEZONE_OPTIONS.map((tz) => <option key={tz} value={tz}>{tz}</option>)}
                </select>
              </label>
            </div>
            <label className="check-row">
              <input type="checkbox" checked={draft.daylightSavings} onChange={(e) => setDraft({ ...draft, daylightSavings: e.target.checked })} disabled={!canManage} />
              {t('cam.daylightSavings')}
            </label>
            {draft.dateTimeType === 'NTP' ? (
              <>
                <label className="check-row">
                  <input type="checkbox" checked={draft.ntpFromDhcp} onChange={(e) => setDraft({ ...draft, ntpFromDhcp: e.target.checked })} disabled={!canManage} />
                  {t('cam.ntpFromDhcp')}
                </label>
                {!draft.ntpFromDhcp ? (
                  <label>
                    {t('cam.ntpServers')}
                    <input value={draft.ntpServers} onChange={(e) => setDraft({ ...draft, ntpServers: e.target.value })} placeholder="pool.ntp.org, 192.168.1.1" autoComplete="off" disabled={!canManage} />
                  </label>
                ) : null}
              </>
            ) : null}
            {draft.dateTimeType === 'Manual' ? <p className="field-hint">{t('cam.manualTimeHint')}</p> : null}
            {canManage ? (
              <div className="action-row">
                <button type="button" className="quiet" onClick={save} disabled={saving || busy}>
                  <span className="btn-icon"><Ico n="save" /> {t('cam.saveTime')}</span>
                </button>
              </div>
            ) : null}
          </>
        ) : null}
      </div>
    </section>
  );
}

// CameraNetwork reads/sets the camera's IPv4 (ONVIF Get/SetNetworkInterfaces + gateway +
// DNS). Loaded on demand; a static-IP change can orphan the camera, so it warns + confirms.
function CameraNetwork({ device, busy, authHeader, canManage, onMessage }) {
  const t = useT();
  const cameraId = Number(device?.id) || 0;
  const [net, setNet] = useState(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState(null);
  const notify = (msg, kind) => { if (onMessage) onMessage(msg, kind); };

  useEffect(() => { setNet(null); setDraft(null); setError(''); }, [cameraId]);

  const load = useCallback(async () => {
    if (!cameraId) return;
    setLoading(true);
    setError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/network`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setNet(result || { interfaces: [] });
      const iface = (result?.interfaces || [])[0] || null;
      setDraft(iface ? {
        interfaceToken: iface.token,
        dhcp: !!iface.dhcp,
        ipAddress: iface.ipAddress || '',
        prefixLength: iface.prefixLength || 24,
        gateway: result?.gateway || '',
        dns: (result?.dns || []).join(', '),
      } : null);
    } catch (e) {
      setError(e.message || 'failed');
      setNet(null);
    } finally {
      setLoading(false);
    }
  }, [cameraId, authHeader]);

  async function save() {
    if (!draft) return;
    if (!draft.dhcp && !window.confirm(t('cam.networkConfirm'))) return;
    setSaving(true);
    setError('');
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const body = {
        interfaceToken: draft.interfaceToken,
        dhcp: draft.dhcp,
        ipAddress: draft.ipAddress,
        prefixLength: Number(draft.prefixLength) || 24,
        gateway: draft.gateway,
        dns: draft.dns.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean),
      };
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/network`, { method: 'POST', credentials: 'include', headers, body: JSON.stringify(body) });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      notify(t('cam.networkSaved'));
    } catch (e) {
      setError(e.message || 'failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="settings-box">
      <header><h3>{t('cam.networkSettings')}</h3></header>
      <div className="settings-box-body">
        <div className="camera-users-head">
          <span className="field-hint">{t('cam.networkHint')}</span>
          <button type="button" className="quiet" onClick={load} disabled={loading || busy}>
            <span className="btn-icon"><Ico n="refresh" /> {net === null ? t('cam.loadSettings') : t('common.refresh')}</span>
          </button>
        </div>
        {error ? <p className="field-hint danger-text">{error}</p> : null}
        {net !== null && draft ? (
          <>
            <label className="check-row">
              <input type="checkbox" checked={draft.dhcp} onChange={(e) => setDraft({ ...draft, dhcp: e.target.checked })} disabled={!canManage} />
              {t('cam.useDhcp')}
            </label>
            {!draft.dhcp ? (
              <>
                <div className="metadata-row">
                  <label>{t('cam.ipAddress')}<input value={draft.ipAddress} onChange={(e) => setDraft({ ...draft, ipAddress: e.target.value })} placeholder="192.168.1.40" autoComplete="off" disabled={!canManage} /></label>
                  <label>{t('cam.prefixLength')}<input type="number" min="1" max="32" value={draft.prefixLength} onChange={(e) => setDraft({ ...draft, prefixLength: e.target.value })} disabled={!canManage} /></label>
                </div>
                <div className="metadata-row">
                  <label>{t('cam.gateway')}<input value={draft.gateway} onChange={(e) => setDraft({ ...draft, gateway: e.target.value })} placeholder="192.168.1.1" autoComplete="off" disabled={!canManage} /></label>
                  <label>{t('cam.dns')}<input value={draft.dns} onChange={(e) => setDraft({ ...draft, dns: e.target.value })} placeholder="8.8.8.8, 1.1.1.1" autoComplete="off" disabled={!canManage} /></label>
                </div>
              </>
            ) : null}
            <p className="field-hint danger-text">{t('cam.networkWarning')}</p>
            {canManage ? (
              <div className="action-row">
                <button type="button" className="quiet danger-text" onClick={save} disabled={saving || busy}>
                  <span className="btn-icon"><Ico n="save" /> {t('cam.saveNetwork')}</span>
                </button>
              </div>
            ) : null}
          </>
        ) : net !== null && !draft ? (
          <p className="empty-hint">{t('cam.noInterfaces')}</p>
        ) : null}
      </div>
    </section>
  );
}

// CameraCapabilities displays which ONVIF services a camera advertises as chips. The
// capabilities (incl. per-operation probes) are fetched once by SavedCameraRow and passed
// in, so it can also gate the management boxes.
function CameraCapabilities({ caps, loading, error, onRefresh, busy }) {
  const t = useT();
  const chips = caps
    ? [['PTZ', caps.ptz], ['Media', caps.media], ['Imaging', caps.imaging], ['Analytics', caps.analytics], ['Events', caps.events]]
        .filter(([, ok]) => ok).map(([label]) => label)
    : [];
  return (
    <div className="camera-caps">
      <div className="camera-users-head">
        <span className="field-hint">{t('cam.capsHint')}</span>
        <button type="button" className="quiet" onClick={onRefresh} disabled={loading || busy}>
          <span className="btn-icon"><Ico n="refresh" /> {t('common.refresh')}</span>
        </button>
      </div>
      {error ? <p className="field-hint danger-text">{error}</p> : null}
      {loading ? <p className="empty-hint">{t('cam.checkingCaps')}</p> : null}
      {caps && !loading ? (
        <div className="caps-chips">
          {chips.length ? chips.map((c) => <span key={c} className="user-level">{c}</span>) : <span className="field-hint">{t('cam.noExtraCaps')}</span>}
        </div>
      ) : null}
    </div>
  );
}

// TalkAccessPanel shows the TP-Link speaker password config in the Access tab —
// ONLY for a camera that actually speaks the TP-Link talk protocol (cap.needsPassword,
// which the backend sets only after a genuine port-8800 "Streamd" fingerprint match).
// ONVIF-backchannel cameras (Hikvision/Dahua/Axis) talk with their stored
// credentials, so no field is shown; non-TP-Link / unknown cameras show nothing.
function TalkAccessPanel({ device, authHeader, onMessage }) {
  const t = useT();
  const [cap, setCap] = useState(null);
  const [pw, setPw] = useState('');
  const [busy, setBusy] = useState(false);
  const notify = (msg, kind) => { if (onMessage) onMessage(msg, kind); };

  useEffect(() => {
    let cancelled = false;
    fetchTalkCapability(device.id, authHeader)
      .then((c) => { if (!cancelled) setCap(c); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [device.id, authHeader]);

  if (!cap || !cap.supported || !cap.needsPassword) {
    return null;
  }

  async function save() {
    setBusy(true);
    try {
      const updated = await saveTalkPassword(device.id, pw, authHeader);
      setCap(updated || cap);
      setPw('');
      notify(t('cam.talkPwSaved'), 'success');
    } catch (err) {
      notify(err?.message || t('cam.talkPwFailed'), 'error');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="talk-access">
      <h4><span className="btn-icon"><Ico n="mic" /> {t('cam.talkTitle')}</span></h4>
      <p className="field-hint">{t(`cam.talkHint_${cap.transport}`)}</p>
      <label>
        {t('cam.talkPassword')}
        <PasswordField
          value={pw}
          onChange={setPw}
          autoComplete="off"
          placeholder={cap.hasPassword ? t('cam.savedPwKept') : ''}
        />
        <span className={cap.hasPassword ? 'field-hint good' : 'field-hint'}>
          {cap.hasPassword ? t('cam.talkPwStored') : t('cam.talkPwNeeded')}
        </span>
      </label>
      <div className="action-row">
        <button type="button" className="quiet" onClick={save} disabled={busy || !pw}>
          <span className="btn-icon"><Ico n="save" /> {t('cam.talkSavePassword')}</span>
        </button>
      </div>
      <details className="talk-help">
        <summary>{t('cam.talkHelpTitle')}</summary>
        <ul>
          <li>{t('cam.talkHelp1')}</li>
          <li>{t('cam.talkHelp2')}</li>
          <li>{t('cam.talkHelp3')}</li>
        </ul>
      </details>
    </div>
  );
}

export function SavedCameraRow({
  device,
  onMessage,
  busy,
  detailDraft,
  credentials,
  streamOptions,
  onDetailDraft,
  onSaveDetails,
  onDiscardDetails,
  onCredential,
  onSaveCredentials,
  onResolve,
  onTest,
  onPreview,
  onAdd,
  onRemove,
  recordingConfigs,
  onSaveRecordingConfig,
  authHeader,
  canManage = true,
}) {
  const t = useT();
  const localDetails = detailDraft || { name: device.name || '', description: device.description || '' };
  const localCred = credentials || { username: device.username || '', password: '' };
  const savedDetails = { name: device.name || '', description: device.description || '' };
  const detailsHaveChanges = localDetails.name !== savedDetails.name || localDetails.description !== savedDetails.description;
  const savedCred = { username: device.username || '', password: '' };
  const credHaveChanges = localCred.username !== savedCred.username || localCred.password !== '';
  const streamReady = Boolean(device.rtspUrl);
  const options = Array.isArray(streamOptions?.options) ? streamOptions.options : [];
  // Which role(s) each detected stream currently fills, derived from this camera's
  // recording config (live-view / detection / recording / fallback) — so the badges say
  // WHERE a stream is used, not a vague "In use". Comparison is on the bare RTSP URL,
  // which is what the config chips store.
  const streamCfg = (recordingConfigs || []).find((c) => Number(c.cameraId) === Number(device.id)) || null;
  const streamRoles = (option) => {
    const optUrl = (option?.rtspUrl || '').trim();
    if (!optUrl) {
      return [];
    }
    const norm = (u) => (u || '').trim();
    const roles = [];
    // Live view uses liveStreamUrl, or the camera's active RTSP when that's unset.
    if ((norm(streamCfg?.liveStreamUrl) || norm(device.rtspUrl)) === optUrl) {
      roles.push(t('cam.roleLive'));
    }
    if (norm(streamCfg?.streamUrl) === optUrl) {
      roles.push(t('cam.roleDetection'));
      if (streamCfg?.enabled) {
        roles.push(t('cam.roleRecording'));
      }
    }
    if (norm(streamCfg?.fallbackStreamUrl) === optUrl) {
      roles.push(t('cam.roleFallback'));
    }
    return roles;
  };
  // GitHub-style destructive confirm: the Remove button stays disabled until the user
  // re-types the camera's exact name. Reset when switching cameras (keyed remount).
  const [confirmRemove, setConfirmRemove] = useState('');
  const removeName = cameraTitle(device);
  const removeReady = confirmRemove.trim() === removeName;
  // ONVIF device-management (users / time / network / maintenance / capabilities) only
  // works on ONVIF cameras; a manually-added RTSP camera has no device service URL.
  const isOnvif = Boolean(device.xAddr);
  // Fetch the camera's capabilities once (it also probes GetUsers/GetDateTime/GetNetwork
  // server-side) so each management box is shown only if that operation actually works.
  const [caps, setCaps] = useState(null);
  const [capsLoading, setCapsLoading] = useState(false);
  const [capsError, setCapsError] = useState('');
  const loadCaps = useCallback(async () => {
    if (!isOnvif || !device.id) return;
    setCapsLoading(true);
    setCapsError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${device.id}/capabilities`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      setCaps((payload?.data?.result ?? payload?.result ?? payload) || {});
    } catch (e) {
      setCapsError(e.message || 'failed');
      setCaps(null);
    } finally {
      setCapsLoading(false);
    }
  }, [isOnvif, device.id, authHeader]);
  useEffect(() => { loadCaps(); }, [loadCaps]);

  return (
    <div className="settings-boxes">
      <div className="device-title-row">
        <div>
          <h3>{removeName}</h3>
          <p>{device.xAddr}</p>
        </div>
        <div className="device-pill-group">
          <HealthPill device={device} />
          <strong className={`status-pill ${device.rtspStatus || 'unknown'}`}>{device.rtspStatus || t('cam.notReady')}</strong>
        </div>
      </div>

      <section className="settings-box">
        <header><h3>{t('cam.tabDetails')}</h3></header>
        <div className="settings-box-body">
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
                {t('cam.cameraName')}
                <input
                  value={localDetails.name}
                  onChange={(event) => onDetailDraft(device.id, { ...localDetails, name: event.target.value })}
                  autoComplete="off"
                />
              </label>
              <label>
                {t('common.description')}
                <input
                  value={localDetails.description}
                  onChange={(event) => onDetailDraft(device.id, { ...localDetails, description: event.target.value })}
                  autoComplete="off"
                />
              </label>
            </div>
            <div className="action-row">
              <button type="submit" className="quiet" disabled={busy || !detailsHaveChanges}>
                <span className="btn-icon"><Ico n="save" /> {t('cam.saveDetails')}</span>
              </button>
              <button type="button" className="quiet" onClick={() => onDiscardDetails(device.id)} disabled={busy || !detailsHaveChanges}>
                <span className="btn-icon"><Ico n="undo" /> {t('common.discard')}</span>
              </button>
            </div>
          </form>
        </div>
      </section>

      <section className="settings-box">
        <header><h3>{t('cam.tabAccess')}</h3></header>
        <div className="settings-box-body">
          <FormBusyOverlay busy={busy} />
          <div className="credential-row">
            <label>
              {t('cam.cameraUsername')}
              <input
                value={localCred.username}
                onChange={(event) => onCredential(device.id, { ...localCred, username: event.target.value })}
                autoComplete="off"
              />
            </label>
            <label>
              {t('cam.cameraPassword')}
              <PasswordField
                value={localCred.password}
                onChange={(password) => onCredential(device.id, { ...localCred, password })}
                autoComplete="off"
                placeholder={device.hasPassword ? t('cam.savedPwKept') : ''}
              />
              <span className={device.hasPassword ? 'field-hint good' : 'field-hint'}>
                {device.hasPassword ? t('cam.pwSaved') : t('cam.noPwSaved')}
              </span>
            </label>
          </div>
          <div className="action-row">
            <button type="button" className="quiet" onClick={() => onSaveCredentials(device)} disabled={busy || !credHaveChanges}>
              <span className="btn-icon"><Ico n="shield" /> {t('cam.saveCredentials')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => onCredential(device.id, savedCred)} disabled={busy || !credHaveChanges}>
              <span className="btn-icon"><Ico n="undo" /> {t('common.discard')}</span>
            </button>
          </div>
          {caps?.userMgmt ? <CameraUsers device={device} busy={busy} authHeader={authHeader} canManage={canManage} /> : null}
          {canManage ? <TalkAccessPanel device={device} authHeader={authHeader} onMessage={onMessage} /> : null}
        </div>
      </section>

      <section className="settings-box">
        <header><h3>{t('cam.tabStream')}</h3></header>
        <div className="settings-box-body">
          <dl className="stream-meta">
            <div>
              <dt>{t('cam.profile')}</dt>
              <dd>{fieldValue(device.profileToken)}</dd>
            </div>
            <div>
              <dt>{t('cam.rtspUri')}</dt>
              <dd>{fieldValue(device.rtspUrl)}</dd>
            </div>
            <div>
              <dt>{t('cam.tracks')}</dt>
              <dd>
                <Tracks value={device.rtspTracks} />
              </dd>
            </div>
          </dl>
          {options.length > 0 ? (
            <div className="stream-list">
              <span className="stream-list-label">{t('cam.onvifStream')}</span>
              {options.map((option) => {
                const roles = streamRoles(option);
                return (
                  <div key={option.profileToken} className={`stream-row${roles.length ? ' active' : ''}`}>
                    <div className="stream-row-info">
                      <strong className="stream-row-name">
                        {streamOptionLabel(option)}
                        {roles.map((role) => (
                          <span key={role} className="stream-row-badge">{role}</span>
                        ))}
                      </strong>
                      <span className="stream-row-uri">{option.rtspUrl || '-'}</span>
                    </div>
                    <div className="stream-row-actions">
                      <button type="button" className="quiet" onClick={() => onTest(device, option)} disabled={busy} title={t('cam.testRtsp')}>
                        <span className="btn-icon"><Ico n="play" /> {t('cam.testRtsp')}</span>
                      </button>
                      <button type="button" className="quiet" onClick={() => onPreview(device, option)} disabled={busy} title={t('cam.livePreview')}>
                        <span className="btn-icon"><Ico n="eye" /> {t('cam.livePreview')}</span>
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          ) : null}
          <div className="stream-action-flow">
            <button type="button" onClick={() => onResolve(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="search" /> {t('cam.findStreams')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => onTest(device)} disabled={busy || !streamReady}>
              <span className="btn-icon"><Ico n="play" /> {t('cam.testRtsp')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => onPreview(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="eye" /> {t('cam.livePreview')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => onAdd(device)} disabled={busy}>
              <span className="btn-icon"><Ico n="plus" /> {t('cam.addToLiveViews')}</span>
            </button>
          </div>
          <hr className="saved-tab-divider" />
          <CameraStreamConfig
            device={device}
            configs={recordingConfigs}
            busy={busy}
            authHeader={authHeader}
            canManage={canManage}
            onSaveConfig={onSaveRecordingConfig}
          />
        </div>
      </section>

      <section className="settings-box">
        <header><h3>{t('cam.tabRecording')}</h3></header>
        <div className="settings-box-body">
          <CameraRecordingConfig
            device={device}
            configs={recordingConfigs}
            busy={busy}
            authHeader={authHeader}
            canManage={canManage}
            onSaveConfig={onSaveRecordingConfig}
            onMessage={onMessage}
          />
        </div>
      </section>

      {isOnvif ? (
        <section className="settings-box">
          <header><h3>{t('cam.tabOnvif')}</h3></header>
          <div className="settings-box-body">
            <CameraCapabilities caps={caps} loading={capsLoading} error={capsError} onRefresh={loadCaps} busy={busy} />
            <OnvifDetails device={device} />
          </div>
        </section>
      ) : null}

      {caps?.dateTime ? (
        <CameraTime device={device} busy={busy} authHeader={authHeader} canManage={canManage} onMessage={onMessage} />
      ) : null}
      {caps?.network ? (
        <CameraNetwork device={device} busy={busy} authHeader={authHeader} canManage={canManage} onMessage={onMessage} />
      ) : null}
      {isOnvif ? (
        <CameraMaintenance device={device} busy={busy} authHeader={authHeader} canManage={canManage} onMessage={onMessage} />
      ) : null}

      <section className="settings-box danger-zone-box">
        <header><h3>{t('cam.dangerZone')}</h3></header>
        <div className="settings-box-body">
          <p className="danger-zone-hint">{t('cam.removeCameraHint')}</p>
          <label className="danger-confirm">
            {t('cam.removeConfirmLabel', { name: removeName })}
            <input
              value={confirmRemove}
              onChange={(event) => setConfirmRemove(event.target.value)}
              placeholder={removeName}
              autoComplete="off"
            />
          </label>
          <div className="action-row">
            <button type="button" className="danger-solid" onClick={() => onRemove(device.id)} disabled={busy || !removeReady}>
              <span className="btn-icon"><Ico n="trash" /> {t('cam.removeCamera')}</span>
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}

// Circular D-pad PTZ controller. Renders as an inline SVG; parent is responsible for positioning.
export function PTZRing({ busy, size, onMove, onStop }) {
  const t = useT();
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

  // Press-and-hold: a pointer-down starts a continuous move and the release stops
  // it, so one sustained press pans/tilts smoothly instead of needing repeated
  // taps. A quick tap still nudges (down starts, up stops a moment later).
  const holdingRef = useRef(false);
  const safetyRef = useRef(null);

  function startHold(dir) {
    if (busy || holdingRef.current) return;
    holdingRef.current = true;
    onMove(dir);
    // Safety net: auto-stop if a release event is ever missed (e.g. the tab loses
    // focus mid-press) so the camera can never keep moving indefinitely.
    if (safetyRef.current) clearTimeout(safetyRef.current);
    safetyRef.current = setTimeout(endHold, 20000);
  }

  function endHold() {
    if (safetyRef.current) {
      clearTimeout(safetyRef.current);
      safetyRef.current = null;
    }
    if (!holdingRef.current) return;
    holdingRef.current = false;
    onStop();
  }

  // Stop on unmount and whenever the window loses focus during a press.
  useEffect(() => {
    const onBlur = () => endHold();
    window.addEventListener('blur', onBlur);
    return () => {
      window.removeEventListener('blur', onBlur);
      endHold();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function sector(d, label, dir) {
    return (
      <path
        key={dir}
        d={d}
        className={cls}
        role="button"
        aria-label={label}
        tabIndex={busy ? -1 : 0}
        onPointerDown={(e) => {
          if (busy) return;
          e.preventDefault();
          e.currentTarget.setPointerCapture?.(e.pointerId);
          startHold(dir);
        }}
        onPointerUp={(e) => { e.currentTarget.releasePointerCapture?.(e.pointerId); endHold(); }}
        onPointerCancel={endHold}
        onLostPointerCapture={endHold}
        onKeyDown={(e) => { if (!busy && (e.key === 'Enter' || e.key === ' ') && !e.repeat) { e.preventDefault(); startHold(dir); } }}
        onKeyUp={(e) => { if (e.key === 'Enter' || e.key === ' ') endHold(); }}
      />
    );
  }

  return (
    <svg
      viewBox="0 0 200 200"
      width={sz}
      height={sz}
      className={`ptz-ring${busy ? ' ptz-ring-busy' : ''}`}
      aria-label={t('cam.ptzControls')}
    >
      {/* Interactive sectors — bottom layer so hover fill stays under structural lines */}
      {sector(UP,    t('cam.ptzUp'),    'up')}
      {sector(RIGHT, t('cam.ptzRight'), 'right')}
      {sector(DOWN,  t('cam.ptzDown'),  'down')}
      {sector(LEFT,  t('cam.ptzLeft'),  'left')}
      {/* Center stop */}
      <circle
        cx={cx} cy={cy} r={ri}
        className={cls}
        role="button"
        aria-label={t('cam.ptzStop')}
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

// CameraPreviewPanel is the live-preview popup: a modal dialog over the workspace with
// the WebRTC live view (its own audio/mute button, same as tiles) + the PTZ ring. Opened
// per camera or per detected stream from the Settings → Stream section; Esc / backdrop
// click closes it.
export function CameraPreviewPanel({ preview, busy, authHeader, streamConfig, onClose, onAdd, onPTZMove, onPTZStop }) {
  const t = useT();
  useEffect(() => {
    if (!preview) {
      return undefined;
    }
    const onKey = (event) => { if (event.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [preview, onClose]);
  if (!preview) {
    return null;
  }
  return (
    <div className="preview-overlay" onClick={onClose}>
      <div className="preview-dialog" onClick={(event) => event.stopPropagation()} role="dialog" aria-modal="true" aria-label={preview.title}>
        <header>
          <div>
            <h2>{preview.title}</h2>
            <p>{preview.ptzSupported ? t('cam.ptzAvailable') : t('cam.livePreviewSub')}</p>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label={t('common.close')} title={t('common.close')}>
            <Ico n="x" sz={16} />
          </button>
        </header>
        <div className="preview-viewport">
          <LiveViewport
            key={`${preview.id}:${preview.previewUrl || preview.device?.rtspUrl || ''}:${preview.device?.rtspTracks || ''}`}
            deviceId={preview.id}
            title={preview.title}
            authHeader={authHeader}
            streamConfig={streamConfig}
            rtspTracks={preview.device?.rtspTracks}
            sourceUrl={preview.previewUrl || ''}
            streamKey={`${preview.previewUrl || preview.device?.rtspUrl || ''}:${preview.device?.rtspTracks || ''}`}
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
              <span className="btn-icon"><Ico n="plus" /> {t('cam.addToLiveViews')}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// CameraLiveView is the camera node's Live View tab: the camera's main live stream (with
// sound + PTZ, like a tile) plus read-only camera info, in a box panel like the other
// tabs. Everything here is view-only — editing lives in the Settings tab.
// CameraAuthGate blocks a camera node's tabs when the stored credentials have stopped
// working (e.g. the password was changed on the camera). It overlays a centered credential
// prompt; entering a valid login re-verifies server-side and clears the gate.
function CameraAuthGate({ device, busy, onUnlock, onRemove }) {
  const t = useT();
  const [username, setUsername] = useState(() => device?.username || '');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  // Two-step confirm so a forgotten-password user can delete the camera from the
  // gate without accidentally nuking it on a stray click.
  const [confirmRemove, setConfirmRemove] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSaving(true);
    setError('');
    try {
      await onUnlock(device, { username: username.trim(), password });
    } catch (err) {
      setError(err?.message || t('cam.authFailed'));
      setSaving(false);
    }
  }

  return (
    <div className="camera-auth-gate">
      <form className="camera-auth-card" onSubmit={submit}>
        <span className="camera-auth-icon"><Ico n="lock" sz={22} /></span>
        <h3>{t('cam.authGateTitle')}</h3>
        <p className="field-hint">{t('cam.authGateHint')}</p>
        <label>
          {t('cam.credUsername')}
          <input className={error ? 'input-error' : ''} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" placeholder="admin" />
        </label>
        <label>
          {t('cam.credPassword')}
          <PasswordField value={password} onChange={setPassword} autoComplete="off" error={!!error} />
        </label>
        {error ? <p className="field-hint danger-text">{error}</p> : null}
        <button type="submit" disabled={saving || busy}>
          <span className="btn-icon"><Ico n="shield" /> {saving ? t('cam.verifying') : t('cam.unlock')}</span>
        </button>
        {onRemove ? (
          <>
            <p className="field-hint camera-auth-remove-hint">{t('cam.authGateForgot')}</p>
            {confirmRemove ? (
              <div className="action-row camera-auth-remove-row">
                <button type="button" className="danger-solid" onClick={() => onRemove(device.id)} disabled={busy}>
                  <span className="btn-icon"><Ico n="trash" /> {t('cam.removeConfirm')}</span>
                </button>
                <button type="button" className="quiet" onClick={() => setConfirmRemove(false)} disabled={busy}>
                  {t('common.cancel')}
                </button>
              </div>
            ) : (
              <button type="button" className="quiet danger-text camera-auth-remove-btn" onClick={() => setConfirmRemove(true)} disabled={busy}>
                <span className="btn-icon"><Ico n="trash" /> {t('cam.removeCamera')}</span>
              </button>
            )}
          </>
        ) : null}
      </form>
    </div>
  );
}

// onvifLocation pulls a readable location out of a camera's ONVIF scopes (space-separated
// onvif:// URIs; the location value sits under .../location/<value>).
function onvifLocation(scopes) {
  const raw = String(scopes || '');
  for (const scope of raw.split(/\s+/)) {
    const idx = scope.indexOf('/location/');
    if (idx >= 0) {
      const val = scope.slice(idx + '/location/'.length).replace(/^\/+|\/+$/g, '').replace(/\/+/g, ', ');
      try { return decodeURIComponent(val); } catch (_) { return val; }
    }
  }
  return '';
}

export function CameraLiveView({ camera, busy, authHeader, streamConfig, inLiveViews, onAddToViews, onRemoveFromViews, onPTZMove, onPTZStop }) {
  const t = useT();
  const cameraId = Number(camera?.id) || 0;
  const isOnvif = Boolean(camera?.xAddr);
  // MAC address + ONVIF version aren't stored on the camera; pull them live on demand so
  // the default Live View stays fast and doesn't hit the camera on every open.
  const [devInfo, setDevInfo] = useState(null);
  const [devLoading, setDevLoading] = useState(false);
  const [devError, setDevError] = useState('');
  useEffect(() => { setDevInfo(null); setDevError(''); }, [cameraId]);
  const loadDevInfo = useCallback(async () => {
    if (!cameraId) return;
    setDevLoading(true);
    setDevError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/device-info`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      setDevInfo((payload?.data?.result ?? payload?.result ?? payload) || {});
    } catch (e) {
      setDevError(e.message || 'failed');
    } finally {
      setDevLoading(false);
    }
  }, [cameraId, authHeader]);
  if (!camera) {
    return null;
  }
  const status = (camera.healthStatus || '').toLowerCase();
  const dotState = status === 'online' ? 'online' : status === 'offline' ? 'offline' : 'unknown';
  const description = cameraDescription(camera);
  const location = onvifLocation(camera.scopes) || (devInfo && devInfo.location) || '';
  const info = [
    [t('cam.manufacturer'), fieldValue(camera.manufacturer)],
    [t('cam.model'), fieldValue(camera.model)],
    [t('cam.firmware'), fieldValue(camera.firmwareVersion)],
    [t('cam.hardwareId'), fieldValue(camera.hardwareId)],
    [t('cam.serial'), fieldValue(camera.serialNumber)],
    ...(location ? [[t('cam.location'), location]] : []),
    ...(devInfo && devInfo.macAddress ? [[t('cam.macAddress'), devInfo.macAddress]] : []),
    ...(devInfo && devInfo.onvifVersion ? [[t('cam.onvifVersion'), devInfo.onvifVersion]] : []),
    [t('cam.host'), fieldValue(camera.host)],
    [t('cam.port'), fieldValue(camera.port)],
    ...(camera.xAddr ? [[t('cam.onvifUri'), camera.xAddr]] : []),
    [t('cam.lastCheckedLabel'), camera.lastHealthCheckAt ? formatTimestamp(camera.lastHealthCheckAt) : '-'],
  ];
  return (
    <section className="camera-live-panel">
      <div className="camera-live-stage">
        <div className="camera-live-view">
          <LiveViewport
            key={`live-${camera.id}`}
            deviceId={camera.id}
            title={cameraTitle(camera)}
            authHeader={authHeader}
            streamConfig={streamConfig}
            rtspTracks={camera.rtspTracks}
            healthStatus={camera.healthStatus}
            streamKey={`${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
          />
          {camera.ptzSupported ? (
            <div className="ptz-ring-overlay">
              <PTZRing
                busy={busy}
                size={120}
                onMove={(dir) => onPTZMove(camera.id, dir)}
                onStop={() => onPTZStop(camera.id)}
              />
            </div>
          ) : null}
        </div>
        <div className="camera-live-bar">
          <span className="camera-live-name">
            <span className={`live-dot ${dotState}`} aria-hidden="true" />
            <span className="camera-live-name-text">{cameraTitle(camera)}</span>
          </span>
          {inLiveViews ? (
            <button type="button" className="camera-live-add is-remove" onClick={() => onRemoveFromViews(camera)} disabled={busy}>
              <span className="btn-icon"><Ico n="trash" /> {t('cam.removeFromLiveViews')}</span>
            </button>
          ) : (
            <button type="button" className="camera-live-add" onClick={() => onAddToViews(camera, { stay: true })} disabled={busy}>
              <span className="btn-icon"><Ico n="plus" /> {t('cam.addToLiveViews')}</span>
            </button>
          )}
        </div>
      </div>
      {description ? <p className="camera-live-desc">{description}</p> : null}
      <dl className="camera-live-info">
        {info.map(([label, value]) => (
          <div key={label} className="live-info-chip">
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      {isOnvif && !devInfo ? (
        <div className="camera-live-more">
          <button type="button" className="quiet" onClick={loadDevInfo} disabled={devLoading || busy}>
            <span className="btn-icon"><Ico n="refresh" /> {devLoading ? t('cam.checkingCaps') : t('cam.loadDeviceInfo')}</span>
          </button>
          {devError ? <span className="field-hint danger-text">{devError}</span> : null}
        </div>
      ) : null}
    </section>
  );
}

export function CamerasTab({
  saved,
  discovered,
  busy,
  manualAddress,
  timeoutMs,
  cameraNav,
  selectedSavedId: controlledSelectedId,
  onSelectSaved,
  cameraAuth,
  onUnlockCamera,
  preview,
  authHeader,
  streamConfig,
  detailDraftsById,
  credentialsById,
  streamOptionsById,
  saveDrafts,
  onCameraNav,
  onSelectCameraRoot,
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
  onSaveCredentials,
  onResolve,
  onTest,
  onPreview,
  onAddToViews,
  onRemoveFromViews,
  viewTileIds,
  onPTZMove,
  onPTZStop,
  onRemove,
  onClosePreview,
  recordingConfigs,
  onSaveRecordingConfig,
  onMessage,
  ai,
  recordings,
  canManage = true,
}) {
  const t = useT();
  // Which top-level tab of a selected camera is showing: its Settings (details,
  // access, stream, recording, onvif — as an accordion) or its AI detection rules.
  const [cameraDetailTab, setCameraDetailTab] = useState('liveview');
  // Selection is controlled when the parent passes selectedSavedId/onSelectSaved (so
  // the side-nav camera tree and this page stay in sync); otherwise fall back to a
  // local state so the component still works standalone.
  const [internalSelectedId, setInternalSelectedId] = useState(null);
  const selectedSavedId = controlledSelectedId != null ? controlledSelectedId : internalSelectedId;
  const setSelectedSavedId = onSelectSaved || setInternalSelectedId;
  const [scanProtocol, setScanProtocol] = useState('all');
  const orderedSaved = useMemo(() => orderedSavedCameras(saved), [saved]);
  const selectedSaved =
    saved.find((device) => Number(device.id) === Number(selectedSavedId)) || orderedSaved[0] || null;
  const selectedPreview =
    selectedSaved && preview && Number(preview.id) === Number(selectedSaved.id) ? preview : null;

  // Auto-select the first camera when entering the saved view with no valid
  // selection. Gated to the saved view so opening the probe view (managed camera
  // cleared) doesn't immediately re-select a camera and steal the root highlight.
  useEffect(() => {
    if (cameraNav !== 'saved' || !saved.length) {
      return;
    }
    if (!selectedSaved || Number(selectedSaved.id) !== Number(selectedSavedId)) {
      setSelectedSavedId(orderedSaved[0]?.id || null);
    }
  }, [cameraNav, saved, orderedSaved, selectedSaved, selectedSavedId]);

  return (
    <section className="workspace">
      {/* Cameras are navigated from the side-nav tree now (root → probe, each camera →
          its properties), so the in-page probe/saved toggle and the duplicate saved
          list are gone: the root opens discovery, a camera opens its detail directly. */}
      {cameraNav === 'probe' ? (
        <>
          <div className="camera-tab-header">
            <h2 className="section-title">{t('cam.discoverCameras')}</h2>
            <p className="section-subtitle">{t('cam.probeSubtitle')}</p>
          </div>
          <section className="discover-layout">
          <div className="discover-actions">
            <div className="discover-card discover-card--scan">
              <div className="discover-card-head">
                <span className="discover-card-icon"><Ico n="wifi" /></span>
                <div className="discover-card-heading">
                  <h3>{t('cam.scanTitle')}</h3>
                  <p>{t('cam.scanCardHint')}</p>
                </div>
              </div>
              <div className="scan-row">
                <label>
                  {t('cam.scanTimeout')}
                  <input value={timeoutMs} onChange={(event) => onTimeout(event.target.value)} inputMode="numeric" />
                </label>
                <label className="scan-protocol-label">
                  {t('cam.protocol')}
                  <select value={scanProtocol} onChange={(e) => setScanProtocol(e.target.value)} className="scan-protocol-select">
                    <option value="all">{t('cam.allMethods')}</option>
                    <option value="onvif">ONVIF</option>
                    <option value="ssdp">{t('cam.ssdp')}</option>
                    <option value="mdns">{t('cam.mdns')}</option>
                    <option value="sadp">{t('cam.sadp')}</option>
                    <option value="portscan">{t('cam.portScan')}</option>
                  </select>
                </label>
                <label className="scan-protocol-label">
                  <span className="scan-label-row">
                    {t('cam.subnet')}
                    <InfoButton text={t('cam.subnetInfo')} />
                  </span>
                  <input
                    value={scanCIDR}
                    onChange={(e) => onScanCIDR(e.target.value)}
                    placeholder="auto"
                    className="scan-cidr-input"
                  />
                </label>
                <button type="button" onClick={() => onScan(scanProtocol, scanCIDR)} disabled={busy}>
                  <span className="btn-icon"><Ico n="wifi" /> {t('cam.scan')}</span>
                </button>
              </div>
            </div>

            <div className="discover-divider" aria-hidden="true"><span>{t('common.or')}</span></div>

            <div className="discover-card discover-card--manual">
              <div className="discover-card-head">
                <span className="discover-card-icon"><Ico n="plus" /></span>
                <div className="discover-card-heading">
                  <h3>{t('cam.manualTitle')}</h3>
                  <p>{t('cam.manualCardHint')}</p>
                </div>
              </div>
              <form className="probe-row" onSubmit={onProbe}>
                <label>
                  {t('cam.manualAddress')}
                  <input
                    value={manualAddress}
                    onChange={(event) => onManualAddress(event.target.value)}
                    placeholder="192.168.1.40"
                  />
                </label>
                <button type="submit" disabled={busy}>
                  <span className="btn-icon"><Ico n="search" /> {t('cam.probe')}</span>
                </button>
              </form>
            </div>
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
        </>
      ) : (
          <main className="saved-detail">
            {selectedSaved ? (
              <>
                {/* Same shared hero myseliasan's Nodes → Camera page renders, so a camera
                    looks the same whether you reach it on the node or through the control
                    plane. The breadcrumb's root returns to camera discovery. */}
                <CameraHero
                  crumbs={[
                    // Use the side-nav's own root handler so the rail's highlight follows
                    // the breadcrumb; onCameraNav alone would leave the camera selected.
                    { label: t('nav.cameras'), onClick: onSelectCameraRoot || (() => onCameraNav('probe')) },
                    { label: cameraTitle(selectedSaved) },
                  ]}
                  name={cameraTitle(selectedSaved)}
                  description={cameraDescription(selectedSaved)}
                  tone={statusTone(selectedSaved.healthStatus)}
                  chips={[
                    {
                      key: 'health',
                      label: t(healthPillProps(selectedSaved.healthStatus).labelKey),
                      tone: statusTone(selectedSaved.healthStatus),
                    },
                    {
                      key: 'stream',
                      label: selectedSaved.rtspStatus || t('cam.notReady'),
                      tone: statusTone(selectedSaved.rtspStatus, 'resolved'),
                      icon: 'video',
                      capitalize: true,
                    },
                  ]}
                />
                <Tabs
                  ariaLabel={t('cam.detailTabsAria')}
                  active={cameraDetailTab}
                  onChange={setCameraDetailTab}
                  tabs={[
                    { id: 'liveview', label: t('cam.detailLive'), icon: 'video' },
                    { id: 'ai', label: t('cam.detailDetection'), icon: 'cpu' },
                    { id: 'recordings', label: t('tab.recording'), icon: 'film' },
                    { id: 'settings', label: t('cam.detailSettings'), icon: 'sliders' },
                  ]}
                />
                {cameraDetailTab === 'liveview' ? (
                  <CameraLiveView
                    key={`live-${selectedSaved.id}`}
                    camera={selectedSaved}
                    busy={busy}
                    authHeader={authHeader}
                    streamConfig={streamConfig}
                    inLiveViews={(viewTileIds || []).includes(selectedSaved.id)}
                    onAddToViews={onAddToViews}
                    onRemoveFromViews={onRemoveFromViews}
                    onPTZMove={onPTZMove}
                    onPTZStop={onPTZStop}
                  />
                ) : cameraDetailTab === 'settings' ? (
                  <section className="camera-settings-panel">
                    <div className="toolbar">
                      <div>
                        <h2 className="section-title">{t('cam.settingsTitle')}</h2>
                        <p className="section-subtitle">{t('cam.settingsSub')}</p>
                      </div>
                    </div>
                    <SavedCameraRow
                      key={selectedSaved.id || selectedSaved.xAddr}
                      device={selectedSaved}
                      onMessage={onMessage}
                      busy={busy}
                      detailDraft={detailDraftsById[selectedSaved.id] || { name: selectedSaved.name || '', description: selectedSaved.description || '' }}
                      credentials={credentialsById[selectedSaved.id] || { ...defaultDeviceCredentials, username: selectedSaved.username || '' }}
                      streamOptions={streamOptionsById[selectedSaved.id]}
                      onDetailDraft={onDetailDraft}
                      onSaveDetails={onSaveDetails}
                      onDiscardDetails={onDiscardDetails}
                      onCredential={onCredential}
                      onSaveCredentials={onSaveCredentials}
                      onResolve={onResolve}
                      onTest={onTest}
                      onPreview={onPreview}
                      onAdd={onAddToViews}
                      onRemove={onRemove}
                      recordingConfigs={recordingConfigs}
                      onSaveRecordingConfig={onSaveRecordingConfig}
                      authHeader={authHeader}
                      canManage={canManage}
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
                  </section>
                ) : cameraDetailTab === 'recordings' ? (
                  <CameraRecordingsPanel
                    key={`rec-${selectedSaved.id}`}
                    camera={selectedSaved}
                    busy={busy}
                    authHeader={authHeader}
                    canManage={canManage}
                    {...(recordings || {})}
                  />
                ) : (
                  <CameraAiPanel
                    key={`ai-${selectedSaved.id}`}
                    camera={selectedSaved}
                    busy={busy}
                    authHeader={authHeader}
                    streamConfig={streamConfig}
                    {...(ai || {})}
                  />
                )}
                {cameraAuth && cameraAuth[selectedSaved.id] === 'unauthorized' ? (
                  <CameraAuthGate device={selectedSaved} busy={busy} onUnlock={onUnlockCamera} onRemove={onRemove} />
                ) : null}
              </>
            ) : (
              <section className="device-card empty-detail">
                <h2>{t('cam.noCameraSelected')}</h2>
                <p className="empty">{t('cam.noCameraSelectedHint')}</p>
              </section>
            )}
          </main>
      )}
    </section>
  );
}

