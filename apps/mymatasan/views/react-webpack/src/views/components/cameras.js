import { useState, useEffect, useMemo, useRef } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay, InfoButton, Tracks, LayoutDropdown } from './ui';
import { defaultDeviceCredentials } from '../lib/constants';
import {fieldValue,formatTimestamp,cameraTitle,cameraDescription,orderedSavedCameras,sameCamera,streamOptionLabel,layoutCapacity,layoutColumns,layoutRows } from '../lib/helpers';
import { LiveViewport } from './previews';
import { PasswordField } from './layout';
import { CameraRecordingConfig, CameraStreamConfig } from './recording';

// healthPillProps maps a camera's live health status into a pill class and label.
// The status-pill online/offline/unknown classes are shared with the RTSP pill.
function healthPillProps(status) {
  switch ((status || '').toLowerCase()) {
    case 'online':
      return { cls: 'online', label: 'Online' };
    case 'offline':
      return { cls: 'offline', label: 'Offline' };
    default:
      return { cls: 'unknown', label: 'Unknown' };
  }
}

// HealthPill shows the camera's network reachability as decided by the health monitor.
export function HealthPill({ device }) {
  const { cls, label } = healthPillProps(device.healthStatus);
  const checked = device.lastHealthCheckAt ? formatTimestamp(device.lastHealthCheckAt) : '';
  return (
    <strong className={`status-pill ${cls}`} title={checked ? `Last checked ${checked}` : 'Not checked yet'}>
      {label}
    </strong>
  );
}

export function DeviceMeta({ device }) {
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
      <div>
        <dt>Health</dt>
        <dd>{healthPillProps(device.healthStatus).label}</dd>
      </div>
      <div>
        <dt>Last checked</dt>
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
            <div className="view-pager" role="group" aria-label="Live view pages">
              <button type="button" className="quiet" onClick={() => setPage(safePage - 1)} disabled={safePage <= 0} aria-label="Previous page">
                <Ico n="arr-left" sz={14} />
              </button>
              <span className="view-pager-label">{safePage + 1} / {pageCount}</span>
              <button type="button" className="quiet" onClick={() => setPage(safePage + 1)} disabled={safePage >= pageCount - 1} aria-label="Next page">
                <Ico n="arr-right" sz={14} />
              </button>
            </div>
          ) : null}
          <button type="button" className="quiet" onClick={toggleFullscreen} title="Fullscreen (Esc to exit)">
            <span className="btn-icon"><Ico n="maximize" /> Fullscreen</span>
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
                  <button
                    type="button"
                    className="icon-button"
                    onClick={() => toggleMaximize(tile.id)}
                    aria-label={maximizedId === tile.id ? 'Restore tile' : 'Maximize tile'}
                    title={maximizedId === tile.id ? 'Restore' : 'Maximize'}
                  >
                    <Ico n={maximizedId === tile.id ? 'minimize' : 'maximize'} sz={12} />
                  </button>
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
                  healthStatus={tileDevice?.healthStatus}
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

export function DiscoveredDevices({ devices, saved, busy, drafts, onDraft, onSave }) {
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

export function SavedCameraRow({
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
  recordingConfigs,
  onSaveRecordingConfig,
  authHeader,
  canManage = true,
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
        <div className="device-pill-group">
          <HealthPill device={device} />
          <strong className={`status-pill ${device.rtspStatus || 'unknown'}`}>{device.rtspStatus || 'not ready'}</strong>
        </div>
      </div>

      <nav className="saved-detail-tabs" aria-label="Saved camera settings">
        {[
          ['details', 'Details'],
          ['access', 'Access'],
          ['stream', 'Stream'],
          ['recording', 'Recording'],
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
              <PasswordField
                value={localCred.password}
                onChange={(password) => onCredential(device.id, { ...localCred, password })}
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
              <PasswordField
                value={localPasswordDraft.newPassword}
                onChange={(newPassword) => onPasswordDraft(device.id, { ...localPasswordDraft, newPassword })}
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
          <hr className="saved-tab-divider" />
          <CameraStreamConfig
            device={device}
            configs={recordingConfigs}
            busy={busy}
            authHeader={authHeader}
            canManage={canManage}
            onSaveConfig={onSaveRecordingConfig}
          />
        </section>
      ) : null}

      {activePanel === 'recording' ? (
        <CameraRecordingConfig
          device={device}
          configs={recordingConfigs}
          busy={busy}
          authHeader={authHeader}
          canManage={canManage}
          onSaveConfig={onSaveRecordingConfig}
        />
      ) : null}

      {activePanel === 'onvif' ? (
        <section className="saved-tab-panel">
          <OnvifDetails device={device} />
        </section>
      ) : null}
    </article>
  );
}

export function SavedDeviceNav({ devices, selectedId, onSelect }) {
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
export function PTZRing({ busy, size, onMove, onStop }) {
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

export function CameraPreviewPanel({ preview, busy, authHeader, streamConfig, onClose, onAdd, onPTZMove, onPTZStop }) {
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

export function CamerasTab({
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
  recordingConfigs,
  onSaveRecordingConfig,
  canManage = true,
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

      <>
          <div className="camera-tab-header">
            <h2 className="section-title">{cameraNav === 'probe' ? 'Discover Cameras' : 'Saved Cameras'}</h2>
            <p className="section-subtitle">
              {cameraNav === 'probe'
                ? 'Scan the local network or probe a specific address to find ONVIF/RTSP cameras, then save them.'
                : 'Manage your saved cameras — edit details, set access credentials, resolve the stream, and view ONVIF info.'}
            </p>
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
        </>
    </section>
  );
}

