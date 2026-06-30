import { useState, useEffect, useMemo, useRef } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
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

export function DiscoveredDevices({ devices, saved, busy, drafts, onDraft, onSave }) {
  const t = useT();
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
            <span className="btn-icon"><Ico n="save" /> {t('common.save')}</span>
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
        <div className="metadata-row">
          <label>
            {t('cam.cameraName')}
            <input
              value={draft.name}
              onChange={(event) => onDraft(key, { ...draft, name: event.target.value })}
              autoComplete="off"
            />
          </label>
          <label>
            {t('common.description')}
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
              <h3>{t('cam.saved')}</h3>
              <div className="discovery-group-actions">
                <span className="discovery-group-count">{savedDevices.length}</span>
                <button
                  type="button"
                  className="quiet compact-button"
                  aria-expanded={savedExpanded}
                  onClick={() => setSavedExpanded((current) => !current)}
                >
                  {savedExpanded ? t('cam.collapse') : t('cam.expand')}
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
  activePanel,
  onPanelChange,
  onMessage,
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
  const t = useT();
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

  return (
    <article className="device-card">
      <div className="device-title-row">
        <div>
          <h3>{cameraTitle(device)}</h3>
          <p>{device.xAddr}</p>
        </div>
        <div className="device-pill-group">
          <HealthPill device={device} />
          <strong className={`status-pill ${device.rtspStatus || 'unknown'}`}>{device.rtspStatus || t('cam.notReady')}</strong>
        </div>
      </div>

      <nav className="saved-detail-tabs" aria-label={t('cam.settingsAria')}>
        {[
          ['details', 'cam.tabDetails'],
          ['access', 'cam.tabAccess'],
          ['stream', 'cam.tabStream'],
          ['recording', 'cam.tabRecording'],
          ['onvif', 'cam.tabOnvif'],
        ].map(([id, labelKey]) => (
          <button
            type="button"
            key={id}
            className={activePanel === id ? 'active' : 'quiet'}
            onClick={() => onPanelChange(id)}
          >
            {t(labelKey)}
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
              <button type="button" className="quiet danger-text" onClick={() => onRemove(device.id)} disabled={busy}>
                <span className="btn-icon"><Ico n="trash" /> {t('common.remove')}</span>
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
          <div className="credential-row">
            <label>
              {t('cam.onvifUser')}
              <input
                value={localPasswordDraft.targetUsername}
                onChange={(event) => onPasswordDraft(device.id, { ...localPasswordDraft, targetUsername: event.target.value })}
                placeholder={device.username || t('cam.cameraUser')}
                autoComplete="off"
              />
            </label>
            <label>
              {t('cam.newOnvifPassword')}
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
              <span className="btn-icon"><Ico n="key" /> {t('cam.changeCameraPassword')}</span>
            </button>
          </div>
        </section>
      ) : null}

      {activePanel === 'stream' ? (
        <section className="saved-tab-panel">
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
            <div className="stream-option-panel">
              <label>
                {t('cam.onvifStream')}
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
                {t('cam.useSelectedStream')}
              </button>
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
          onMessage={onMessage}
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
  const t = useT();
  const orderedDevices = useMemo(() => orderedSavedCameras(devices), [devices]);
  return (
    <aside className="saved-sidebar">
      <header>
        <h2>{t('cam.savedCameras')}</h2>
        <span>{devices.length}</span>
      </header>
      <nav className="saved-device-nav" aria-label={t('cam.savedCameras')}>
        {devices.length === 0 ? <p className="empty">{t('cam.noSavedCameras')}</p> : null}
        {orderedDevices.map((device) => (
          <button
            type="button"
            className={Number(selectedId) === Number(device.id) ? 'saved-device-button active' : 'saved-device-button'}
            key={device.id || device.xAddr}
            onClick={() => onSelect(device.id)}
          >
            <strong>{cameraTitle(device)}</strong>
            <span>{device.host || device.xAddr || t('cam.cameraFallback')}</span>
          </button>
        ))}
      </nav>
    </aside>
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

export function CameraPreviewPanel({ preview, busy, authHeader, streamConfig, onClose, onAdd, onPTZMove, onPTZStop }) {
  const t = useT();
  if (!preview) {
    return null;
  }
  return (
    <section className="preview-panel">
      <header>
        <div>
          <h2>{preview.title}</h2>
          <p>{preview.ptzSupported ? t('cam.ptzAvailable') : t('cam.livePreviewSub')}</p>
        </div>
        <button type="button" className="quiet" onClick={onClose}>
          {t('common.close')}
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
            <span className="btn-icon"><Ico n="plus" /> {t('cam.addToLiveViews')}</span>
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
  onMessage,
  canManage = true,
}) {
  const t = useT();
  const [selectedSavedId, setSelectedSavedId] = useState(null);
  const [scanProtocol, setScanProtocol] = useState('all');
  // Held at this level (not inside SavedCameraRow, which remounts per camera) so the
  // open settings tab persists when switching between cameras in the left panel.
  const [savedPanel, setSavedPanel] = useState('details');
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
        <nav className="secondary-tabs" aria-label={t('cam.camerasAria')}>
          <button type="button" className={cameraNav === 'probe' ? 'active' : 'quiet'} onClick={() => onCameraNav('probe')}>
            <span className="btn-icon"><Ico n="search" /> {t('cam.probe')}</span>
          </button>
          <button type="button" className={cameraNav === 'saved' ? 'active' : 'quiet'} onClick={() => onCameraNav('saved')}>
            <span className="btn-icon"><Ico n="camera" /> {t('cam.saved')}</span>
          </button>
        </nav>
      </div>

      <>
          <div className="camera-tab-header">
            <h2 className="section-title">{cameraNav === 'probe' ? t('cam.discoverCameras') : t('cam.savedCameras')}</h2>
            <p className="section-subtitle">
              {cameraNav === 'probe' ? t('cam.probeSubtitle') : t('cam.savedSubtitle')}
            </p>
          </div>
          {cameraNav === 'probe' ? (
        <section className="camera-grid">
          <div className="probe-panel">
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
                  activePanel={savedPanel}
                  onPanelChange={setSavedPanel}
                  onMessage={onMessage}
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
                <h2>{t('cam.noCameraSelected')}</h2>
                <p className="empty">{t('cam.noCameraSelectedHint')}</p>
              </section>
            )}
          </main>
        </section>
          )}
        </>
    </section>
  );
}

