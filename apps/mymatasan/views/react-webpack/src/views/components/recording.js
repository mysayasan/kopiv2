import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import {formatFileSize,segmentDuration,segmentFilename,detectionTypeLabel,todayDateString,apiBase,formatTimestamp,cameraTitle,orderedSavedCameras } from '../lib/helpers';
import { SavedDeviceNav } from './cameras';

// mode controls which half renders: "view" (default) shows the recordings viewer
// (event clips + all recordings), "config" shows the per-camera recording
// configuration. The config half lives on the Cameras page; the Recording page is
// view-only.
export function RecordingTab({ mode = 'view', saved, segments, configs, busy, authHeader, onSaveConfig, onDeleteSegment, onReload, focusCameraId, focusAlertId, unacknowledgedAlertIds, onAcknowledgeAlert, alerts }) {
  const showConfig = mode === 'config';
  const showView = mode !== 'config';
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
  }, [effectiveCameraId, configs, selectedCamera]);

  // Reset browse state only when the selected camera actually changes — not on
  // every background reload (which re-creates the configs/selectedCamera refs and
  // would otherwise snap the date back to today and wipe the loaded list).
  useEffect(() => {
    setAllBrowseSegments([]);
    setBrowseLoaded(false);
    setTimelineSelectedMin(null);
    setBrowseDate(todayDateString());
  }, [effectiveCameraId]);

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
          <h2 className="section-title">{showConfig ? 'Recording Settings' : 'Recordings'}</h2>
          <p className="section-subtitle">
            {showConfig
              ? 'Per-camera NVR recording configuration.'
              : 'Continuous NVR recording with event clip extraction.'}
          </p>
        </div>
        {showView ? (
          <button type="button" className="quiet" onClick={onReload} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> Reload</span>
          </button>
        ) : null}
      </div>

      <section className="saved-browser">
        <SavedDeviceNav devices={saved} selectedId={selectedCamera?.id} onSelect={setSelectedCameraId} />

        <main className="saved-detail">
          {selectedCamera ? (
            <div className="recording-layout">
              {showConfig && (
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
              )}

              {showView && (
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
              )}
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

