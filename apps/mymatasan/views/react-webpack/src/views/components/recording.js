import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import {formatFileSize,segmentDuration,segmentFilename,detectionTypeLabel,todayDateString,apiBase,formatTimestamp } from '../lib/helpers';

// hevcPlaybackSupported reports whether this browser can decode HEVC in a plain
// <video> element. Chrome/Edge (with OS HW support) and Safari return a non-empty
// canPlayType; Firefox returns "". Memoized — the answer is fixed per session.
let _hevcSupport = null;
function hevcPlaybackSupported() {
  if (_hevcSupport !== null) return _hevcSupport;
  try {
    const v = document.createElement('video');
    _hevcSupport = !!(v.canPlayType('video/mp4; codecs="hvc1.1.6.L93.B0"')
      || v.canPlayType('video/mp4; codecs="hev1.1.6.L93.B0"'));
  } catch (_) {
    _hevcSupport = false;
  }
  return _hevcSupport;
}

// segmentPlaybackUrl builds the segment serve URL, asking the server to transcode
// HEVC→H.264 on the fly ONLY when the segment is stored as HEVC and this browser
// can't decode it. Browsers that can play HEVC (and all non-HEVC segments) stream
// the stored bytes untouched, so they incur no server-side transcode cost.
function segmentPlaybackUrl(seg) {
  const base = `${apiBase()}/api/recording/segments/${seg.id}/download`;
  if (String(seg?.codec).toLowerCase() === 'hevc' && !hevcPlaybackSupported()) {
    return `${base}?transcode=h264`;
  }
  return base;
}

// CameraRecordingsPanel is the per-camera recordings browser (date timeline, segment
// list, and playback) shown as a tab inside the camera node, beside AI. Per-camera
// recording config and stream URLs live in the Saved-camera Settings panel
// (CameraRecordingConfig / CameraStreamConfig). The camera is fixed by the caller —
// the camera node's tree drives selection — so there is no in-panel camera picker.
export function CameraRecordingsPanel({ camera, canManage = true, busy, authHeader, onDeleteSegment, onPurgeExpired, onReload, unacknowledgedAlertIds, onAcknowledgeAlert, alerts }) {
  const t = useT();
  const effectiveCameraId = Number(camera?.id) || 0;
  const selectedCamera = camera || null;
  const alertById = useMemo(
    () => new Map((alerts || []).map((a) => [Number(a.id), a])),
    [alerts],
  );
  const [downloading, setDownloading] = useState(null);
  const [playingSegment, setPlayingSegment] = useState(null);
  const [videoUrl, setVideoUrl] = useState(null);
  const [loadingVideo, setLoadingVideo] = useState(false);

  // Recordings browse state (scoped to this camera + the selected date).
  const [browseDate, setBrowseDate] = useState(todayDateString);
  const [allBrowseSegments, setAllBrowseSegments] = useState([]);
  const [browseLoading, setBrowseLoading] = useState(false);
  const [browseLoaded, setBrowseLoaded] = useState(false);
  const [timelineSelectedMin, setTimelineSelectedMin] = useState(null);
  const [timelineHoverMin, setTimelineHoverMin] = useState(null);
  const [timelineScrollTargetId, setTimelineScrollTargetId] = useState(null);
  const timelineBarRef = useRef(null);
  const segmentRefsMap = useRef({});

  useEffect(() => {
    if (!playingSegment) return;
    const onKey = (e) => { if (e.key === 'Escape') closeVideoModal(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [playingSegment]);

  // Reset browse state only when the selected camera actually changes — not on
  // every background reload (which re-creates the configs/selectedCamera refs and
  // would otherwise snap the date back to today and wipe the loaded list).
  useEffect(() => {
    setAllBrowseSegments([]);
    setBrowseLoaded(false);
    setTimelineSelectedMin(null);
    setBrowseDate(todayDateString());
  }, [effectiveCameraId]);

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
    loadBrowseSegments();
  }, [loadBrowseSegments]);

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
      const resp = await fetch(segmentPlaybackUrl(seg), {
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
          if (opts.segRef) opts.segRef(el);
        }}
        className={`segment-row${isFocused ? ' focused' : ''}${extraClass ? ` ${extraClass}` : ''}`}
      >
        <button type="button" className="segment-thumb-btn" onClick={() => playSegment(seg)} title={t('rec.play')}>
          <svg width="22" height="22" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M8 5v14l11-7z"/>
          </svg>
        </button>
        <div className="segment-info">
          <div className="segment-title-row">
            <strong className="segment-filename">{segmentFilename(seg)}</strong>
            {eventDesc && <span className="segment-event-label">{eventDesc}</span>}
            {seg.alertId > 0 && unacknowledgedAlertIds && unacknowledgedAlertIds.has(Number(seg.alertId)) && (
              <span className="segment-unreviewed">{t('rec.unreviewed')}</span>
            )}
          </div>
          <span className="segment-meta">
            {formatTimestamp(seg.startedAt)}
            {' · '}
            {segmentDuration(seg)}
            {' · '}
            {formatFileSize(seg.fileSize)}
            {seg.alertId ? ` · ${t('rec.alertNum', { id: seg.alertId })}` : ''}
          </span>
        </div>
        <div className="segment-actions">
          {seg.alertId > 0 && unacknowledgedAlertIds && unacknowledgedAlertIds.has(Number(seg.alertId)) && (
            <button type="button" className="quiet" disabled={busy} onClick={() => onAcknowledgeAlert(seg.alertId)}>
              <span className="btn-icon"><Ico n="acknowledge" /> {t('rec.acknowledge')}</span>
            </button>
          )}
          <button type="button" className="quiet" onClick={() => playSegment(seg)}>
            <span className="btn-icon"><Ico n="play" /> {t('rec.play')}</span>
          </button>
          <button type="button" className="quiet" disabled={downloading === seg.id} onClick={() => downloadSegment(seg)}>
            <span className="btn-icon"><Ico n="download" /> {downloading === seg.id ? t('rec.downloading') : t('common.download')}</span>
          </button>
          {canManage ? (
            <button type="button" className="quiet danger-text" disabled={busy} onClick={() => onDeleteSegment(seg.id)}>
              <span className="btn-icon"><Ico n="trash" /> {t('common.delete')}</span>
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <section className="camera-recordings-panel">
      <div className="toolbar">
        <div>
          <h2 className="section-title">{t('rec.title')}</h2>
          <p className="section-subtitle">{t('rec.subtitle')}</p>
        </div>
        <div className="toolbar-actions">
          {canManage && onPurgeExpired ? (
            <button
              type="button"
              className="quiet"
              disabled={busy}
              title={t('rec.purgeTitle')}
              onClick={() => {
                if (window.confirm(t('rec.purgeConfirm'))) {
                  onPurgeExpired();
                }
              }}
            >
              <span className="btn-icon"><Ico n="trash" /> {t('rec.purgeExpired')}</span>
            </button>
          ) : null}
          <button type="button" className="quiet" onClick={onReload} disabled={busy}>
            <span className="btn-icon"><Ico n="reload" /> {t('common.reload')}</span>
          </button>
        </div>
      </div>

      {selectedCamera ? (
        <div className="recording-layout">
          <section className="settings-panel">
                {(() => {
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
                      <h2>{t('rec.allRecordings')}</h2>
                      <span className="status-pill">{browseLoaded ? allBrowseSegments.length : '—'}</span>
                    </header>
                    <div className="log-toolbar" style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
                      <label style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', margin: 0 }}>
                        {t('rec.date')}
                        <input
                          type="date"
                          value={browseDate}
                          max={todayDateString()}
                          onChange={(e) => { setBrowseDate(e.target.value); setAllBrowseSegments([]); setBrowseLoaded(false); setTimelineSelectedMin(null); }}
                        />
                      </label>
                      <button type="button" className="quiet" onClick={() => { const t = todayDateString(); setBrowseDate(t); setAllBrowseSegments([]); setBrowseLoaded(false); setTimelineSelectedMin(null); }} disabled={browseDate === todayDateString()}>
                        {t('rec.today')}
                      </button>
                    </div>
                    {browseLoading && <p className="empty-hint">{t('common.loading')}</p>}

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
                          title={t('rec.clickJump')}
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
                                title={`${t('rec.alertNum', { id: seg.alertId })} · ${formatTimestamp(seg.startedAt)} · ${segmentDuration(seg)}`}
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
                          <span className="timeline-legend-item timeline-legend-item--cont">{t('rec.continuous')}</span>
                          <span className="timeline-legend-item timeline-legend-item--event">{t('rec.eventClip')}</span>
                          {timelineSelectedMin !== null && (
                            <span style={{fontSize: '12px', color: '#667788', marginLeft: 'auto'}}>
                              {t('rec.selectedSegs', { time: selectedLabel, n: allBrowseSegments.filter((s) => {
                                const startMin = (s.startedAt - dayStartSec) / 60;
                                const endMin = ((s.endedAt || s.startedAt) - dayStartSec) / 60;
                                return timelineSelectedMin >= startMin && timelineSelectedMin <= endMin;
                              }).length })}
                            </span>
                          )}
                        </div>
                      </div>
                    )}

                    {browseLoaded && allBrowseSegments.length === 0 && (
                      <p className="empty-hint">{t('rec.noRecordingsDate')}</p>
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
        <p className="empty-hint">{t('rec.noCameras')}</p>
      )}

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
              <button type="button" className="video-dialog-close" onClick={closeVideoModal} aria-label={t('common.close')}>✕</button>
            </div>
            <div className="video-dialog-body">
              {loadingVideo && <div className="video-loading-msg">{t('rec.loadingVideo')}</div>}
              {videoUrl && (
                <video className="video-player" controls autoPlay src={videoUrl} />
              )}
            </div>
            <div className="video-dialog-meta">
              {formatTimestamp(playingSegment.startedAt)} · {segmentDuration(playingSegment)} · {formatFileSize(playingSegment.fileSize)}
              {playingSegment.alertId ? ` · ${t('rec.alertNum', { id: playingSegment.alertId })}` : ''}
            </div>
          </div>
        </div>
        );
      })()}
    </section>
  );
}

// configDraftFor builds the full recording-config shape for a camera from its existing
// config (or sane defaults), so the per-camera Recording and Stream tabs — which both
// edit the SAME entity — always submit a complete config and never wipe each other's
// fields. liveFallback seeds the live-view URL from the camera's RTSP when unset.
function configDraftFor(existing, cameraId, liveFallback = '') {
  return {
    cameraId,
    enabled: existing?.enabled ?? false,
    preRollSec: existing?.preRollSec ?? 30,
    postRollSec: existing?.postRollSec ?? 10,
    storagePath: existing?.storagePath ?? 'recordings',
    retentionDays: existing?.retentionDays ?? 7,
    segmentMinutes: existing?.segmentMinutes ?? 15,
    liveStreamUrl: existing?.liveStreamUrl || liveFallback || '',
    streamUrl: existing?.streamUrl ?? '',
    fallbackStreamUrl: existing?.fallbackStreamUrl ?? '',
  };
}

// CameraRecordingConfig is the per-camera NVR recording settings form shown in the
// Saved-camera panel's Recording tab. Stream URLs live in the Stream tab; this form
// preserves them on save by merging over the camera's existing config. It also polls
// the recorder status so the user gets immediate feedback after enabling recording.
export function CameraRecordingConfig({ device, configs, busy, canManage = true, authHeader, onSaveConfig, onMessage }) {
  const t = useT();
  const notify = useCallback((msg, kind) => { if (onMessage && msg) onMessage(msg, kind); }, [onMessage]);
  const cameraId = Number(device?.id) || 0;
  const existing = useMemo(
    () => (configs || []).find((c) => Number(c.cameraId) === cameraId) || null,
    [configs, cameraId],
  );
  const [draft, setDraft] = useState(() => configDraftFor(existing, cameraId));
  useEffect(() => { setDraft(configDraftFor(existing, cameraId)); }, [existing, cameraId]);

  const [statuses, setStatuses] = useState([]);
  const fetchStatus = useCallback(async () => {
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/recording/status`, { credentials: 'include', headers });
      if (!resp.ok) return;
      const payload = await resp.json();
      const items = payload?.data?.result ?? payload?.result ?? payload;
      setStatuses(Array.isArray(items) ? items : []);
    } catch (_) {}
  }, [authHeader]);
  useEffect(() => {
    fetchStatus();
    const id = setInterval(fetchStatus, 10000);
    return () => clearInterval(id);
  }, [fetchStatus]);

  function save() {
    // Base on the existing entity so the Stream tab's URLs are preserved, then apply
    // this form's recording fields.
    onSaveConfig({
      ...configDraftFor(existing, cameraId),
      enabled: draft.enabled,
      preRollSec: draft.preRollSec,
      postRollSec: draft.postRollSec,
      retentionDays: draft.retentionDays,
      segmentMinutes: draft.segmentMinutes,
      storagePath: draft.storagePath,
    });
  }

  // — Camera-side recording quality (Phase 3): push H.265 + a bitrate cap to the
  // camera's own encoder via ONVIF. This shrinks footage at the source with zero
  // host CPU/GPU cost; the recorder just stream-copies whatever the camera sends.
  const [encoder, setEncoder] = useState(null);
  const [encoderBusy, setEncoderBusy] = useState(false);
  const [encoderCodec, setEncoderCodec] = useState('h265');
  const [encoderKbps, setEncoderKbps] = useState('');
  useEffect(() => { setEncoder(null); setEncoderKbps(''); setEncoderCodec('h265'); }, [cameraId]);

  // syncEncoderControls mirrors the camera's actual encoder state into the form
  // inputs so the controls always reflect what the camera really has.
  function syncEncoderControls(cfg) {
    setEncoder(cfg || null);
    if (cfg?.encoding) setEncoderCodec(/265|hevc/i.test(cfg.encoding) ? 'h265' : 'h264');
    if (cfg?.bitrateLimit) setEncoderKbps(String(cfg.bitrateLimit));
  }

  const loadEncoder = useCallback(async () => {
    if (!cameraId) return;
    setEncoderBusy(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/encoder`, { credentials: 'include', headers });
      const payload = await resp.json().catch(() => null);
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      const cfg = payload?.data?.result ?? payload?.result ?? payload;
      syncEncoderControls(cfg);
    } catch (e) {
      setEncoder(null);
      notify(t('rec.couldNotReadEncoder', { err: e.message }), 'error');
    } finally {
      setEncoderBusy(false);
    }
  }, [cameraId, authHeader, notify]);

  async function applyEncoder() {
    if (!cameraId) return;
    setEncoderBusy(true);
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const body = { encoding: encoderCodec, bitrateLimitKbps: Number(encoderKbps) || 0 };
      const resp = await fetch(`${apiBase()}/api/cameras/${cameraId}/encoder`, {
        method: 'POST', credentials: 'include', headers, body: JSON.stringify(body),
      });
      const payload = await resp.json().catch(() => null);
      // The server verifies the change by re-reading from the camera; a rejection
      // comes back as an error carrying the camera's actual codec in the message.
      if (!resp.ok) throw new Error(payload?.message || payload?.error || `${resp.status}`);
      const cfg = payload?.data?.result ?? payload?.result ?? payload;
      syncEncoderControls(cfg);
      notify(t('rec.cameraNowEncoding', { codec: cfg?.encoding || encoderCodec.toUpperCase(), bitrate: cfg?.bitrateLimit ? t('rec.bitrateSuffix', { kbps: cfg.bitrateLimit }) : '' }));
    } catch (e) {
      notify(t('rec.qualityNotApplied', { err: e.message || 'apply failed' }), 'error');
    } finally {
      setEncoderBusy(false);
    }
  }

  const rs = statuses.find((s) => Number(s.cameraId) === cameraId);

  return (
    <section className="saved-tab-panel">
      <div className="recording-config-grid">
        <label className="field-label">
          <span>{t('rec.segLength')}</span>
          <input type="number" min="1" max="60" value={draft.segmentMinutes || 15}
            onChange={(e) => setDraft({ ...draft, segmentMinutes: Number(e.target.value) })} />
        </label>
        <label className="field-label">
          <span>{t('rec.preRoll')}</span>
          <input type="number" min="5" max="120" value={draft.preRollSec}
            onChange={(e) => setDraft({ ...draft, preRollSec: Number(e.target.value) })} />
        </label>
        <label className="field-label">
          <span>{t('rec.postRoll')}</span>
          <input type="number" min="3" max="120" value={draft.postRollSec}
            onChange={(e) => setDraft({ ...draft, postRollSec: Number(e.target.value) })} />
        </label>
        <label className="field-label">
          <span>{t('rec.retention')}</span>
          <input type="number" min="1" max="365" value={draft.retentionDays || 7}
            onChange={(e) => setDraft({ ...draft, retentionDays: Number(e.target.value) })} />
        </label>
        <label className="field-label">
          <span>{t('rec.storagePath')}</span>
          <input type="text" value={draft.storagePath || ''} placeholder="recordings"
            onChange={(e) => setDraft({ ...draft, storagePath: e.target.value })} />
        </label>
      </div>

      <div className="settings-actions">
        <label className="check-row">
          <input type="checkbox" checked={!!draft.enabled} disabled={!canManage}
            onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />
          {t('rec.enableForCamera')}
        </label>
        <button type="button" onClick={save} disabled={busy || !canManage}>
          <span className="btn-icon"><Ico n="save" /> {t('rec.saveConfig')}</span>
        </button>
      </div>

      {/* Recorder status feedback */}
      {(() => {
        if (rs) {
          const isOk = rs.state === 'streaming';
          const isErr = rs.state === 'error';
          return (
            <div style={{marginTop: '12px', padding: '10px 12px', borderRadius: '6px', background: isOk ? 'rgba(34,197,94,0.1)' : isErr ? 'rgba(239,68,68,0.1)' : 'rgba(148,163,184,0.1)', border: `1px solid ${isOk ? 'rgba(34,197,94,0.3)' : isErr ? 'rgba(239,68,68,0.3)' : 'rgba(148,163,184,0.3)'}`}}>
              <div style={{display:'flex', alignItems:'center', gap:'8px', marginBottom: rs.lastError || rs.liveDir ? '6px' : '0'}}>
                <span style={{width:'8px', height:'8px', borderRadius:'50%', background: isOk ? '#22c55e' : isErr ? '#ef4444' : '#94a3b8', display:'inline-block', flexShrink:0}} />
                <strong style={{fontSize:'13px'}}>{isOk ? t('rec.recActive') : isErr ? t('rec.recError') : t('rec.recStopped')}</strong>
                <span style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', marginLeft:'auto'}}>{t('rec.liveSegs', { n: rs.liveFiles })}</span>
                <button type="button" className="quiet" style={{fontSize:'11px', padding:'2px 6px'}} onClick={fetchStatus}>↻</button>
              </div>
              {rs.liveDir && <div style={{fontSize:'11px', color:'var(--text-muted, #94a3b8)', wordBreak:'break-all'}}>{rs.liveDir}</div>}
              {rs.activeStreamUrl && <div style={{fontSize:'11px', color:'var(--text-muted, #94a3b8)', wordBreak:'break-all', marginTop:'2px'}}>
                {rs.usingFallback ? t('rec.fallbackPrefix') : t('rec.streamPrefix')}{rs.activeStreamUrl}
              </div>}
              {rs.lastError && <div style={{fontSize:'12px', color:'#ef4444', marginTop:'4px', wordBreak:'break-all'}}>{rs.lastError}</div>}
            </div>
          );
        }
        if (draft.enabled) {
          return (
            <div style={{marginTop: '12px', padding: '10px 12px', borderRadius: '6px', background: 'rgba(234,179,8,0.1)', border: '1px solid rgba(234,179,8,0.3)'}}>
              <div style={{display:'flex', alignItems:'center', gap:'8px'}}>
                <span style={{width:'8px', height:'8px', borderRadius:'50%', background:'#eab308', display:'inline-block', flexShrink:0}} />
                <strong style={{fontSize:'13px'}}>{t('rec.noActiveRecorder')}</strong>
                <button type="button" className="quiet" style={{fontSize:'11px', padding:'2px 6px', marginLeft:'auto'}} onClick={fetchStatus}>↻</button>
              </div>
              <div style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', marginTop:'4px'}}>{t('rec.noRecorderHint')}</div>
            </div>
          );
        }
        return null;
      })()}

      {/* Camera-side recording quality (ONVIF): the zero host-cost way to shrink
          footage — the camera encodes H.265 at a capped bitrate and the recorder
          stream-copies it. */}
      <div className="camera-encoder-section" style={{marginTop:'16px', paddingTop:'12px', borderTop:'1px solid var(--border, rgba(148,163,184,0.2))'}}>
        <div style={{display:'flex', alignItems:'center', gap:'8px', marginBottom:'8px'}}>
          <strong style={{fontSize:'13px'}}>{t('rec.cameraSideQuality')}</strong>
          <button type="button" className="quiet" style={{marginLeft:'auto'}} onClick={loadEncoder} disabled={encoderBusy}>
            <span className="btn-icon"><Ico n="refresh" /> {encoder ? t('common.refresh') : t('rec.readFromCamera')}</span>
          </button>
        </div>
        <p style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', margin:'0 0 8px'}}>
          {t('rec.cameraSideHint')}
        </p>
        {encoder && (
          <div style={{fontSize:'12px', color:'var(--text-muted, #94a3b8)', marginBottom:'8px'}}>
            {t('rec.current')} <strong>{encoder.encoding || '?'}</strong>
            {encoder.width ? ` · ${encoder.width}×${encoder.height}` : ''}
            {encoder.frameRateLimit ? ` · ${encoder.frameRateLimit} fps` : ''}
            {encoder.bitrateLimit ? ` · ${encoder.bitrateLimit} kbps` : ''}
          </div>
        )}
        <div className="recording-config-grid">
          <label className="field-label">
            <span>{t('rec.codec')}</span>
            <select value={encoderCodec} disabled={!canManage} onChange={(e) => setEncoderCodec(e.target.value)}>
              <option value="h265">{t('rec.codecH265')}</option>
              <option value="h264">{t('rec.codecH264')}</option>
            </select>
          </label>
          <label className="field-label">
            <span>{t('rec.bitrateCap')}</span>
            <input type="number" min="0" max="20000" step="256" value={encoderKbps}
              placeholder="e.g. 2048" disabled={!canManage}
              onChange={(e) => setEncoderKbps(e.target.value)} />
          </label>
        </div>
        <div className="settings-actions" style={{marginTop:'14px', justifyContent:'flex-start'}}>
          <button type="button" onClick={applyEncoder} disabled={encoderBusy || !canManage}>
            <span className="btn-icon"><Ico n="save" /> {encoderBusy ? t('rec.applying') : t('rec.applyToCamera')}</span>
          </button>
        </div>
      </div>
    </section>
  );
}

// CameraStreamConfig is the per-camera stream-URL editor shown in the Saved-camera
// panel's Stream tab: live-view, detection/recording, and fallback URLs. These streams
// feed BOTH AI detection (even when recording is off) and recording when enabled. It
// preserves the camera's recording settings on save by merging over the existing config.
export function CameraStreamConfig({ device, configs, busy, authHeader, canManage = true, onSaveConfig }) {
  const t = useT();
  const cameraId = Number(device?.id) || 0;
  const liveFallback = device?.rtspUrl || '';
  const existing = useMemo(
    () => (configs || []).find((c) => Number(c.cameraId) === cameraId) || null,
    [configs, cameraId],
  );
  const [draft, setDraft] = useState(() => configDraftFor(existing, cameraId, liveFallback));
  useEffect(() => { setDraft(configDraftFor(existing, cameraId, liveFallback)); }, [existing, cameraId, liveFallback]);

  const [streamProfiles, setStreamProfiles] = useState(null);
  const [streamProfilesLoading, setStreamProfilesLoading] = useState(false);
  const [streamProfilesError, setStreamProfilesError] = useState('');
  useEffect(() => { setStreamProfiles(null); setStreamProfilesError(''); }, [cameraId]);

  const fetchStreamProfiles = useCallback(async () => {
    if (!cameraId) return;
    setStreamProfilesLoading(true);
    setStreamProfilesError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(`${apiBase()}/api/recording/streams/${cameraId}`, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setStreamProfiles(result || null);
    } catch (e) {
      setStreamProfilesError(e.message || t('rec.failedLoadStreams'));
      setStreamProfiles(null);
    } finally {
      setStreamProfilesLoading(false);
    }
  }, [cameraId, authHeader]);

  async function applyLiveStream(rtspUrl) {
    if (!rtspUrl || !cameraId) return;
    try {
      const headers = { 'Content-Type': 'application/json', ...(authHeader ? { Authorization: authHeader } : {}) };
      const resp = await fetch(`${apiBase()}/api/recording/streams/${cameraId}/live`, {
        method: 'POST', credentials: 'include', headers, body: JSON.stringify({ rtspUrl }),
      });
      if (!resp.ok) throw new Error(`${resp.status}`);
      await fetchStreamProfiles();
    } catch (e) {
      alert(t('rec.applyLiveFail', { err: e.message }));
    }
  }

  function autoConfigureStreams() {
    const opts = streamProfiles?.options;
    if (!opts?.length) return;
    if (opts.length >= 2) {
      setDraft((d) => ({ ...d, liveStreamUrl: opts[0].rtspUrl, streamUrl: opts[1].rtspUrl, fallbackStreamUrl: opts[0].rtspUrl }));
    } else {
      setDraft((d) => ({ ...d, liveStreamUrl: opts[0].rtspUrl, streamUrl: '', fallbackStreamUrl: '' }));
      alert(t('rec.oneProfile'));
    }
  }

  // See note in the prior RecordingTab implementation: zone/line geometry is normalized
  // (0–1), so resolution differences are fine — only a different aspect ratio shifts zones.
  const aspectMismatch = (() => {
    const opts = streamProfiles?.options;
    if (!opts?.length) return false;
    const liveUrl = draft.liveStreamUrl || '';
    const detectUrl = draft.streamUrl || '';
    if (!liveUrl || !detectUrl || liveUrl === detectUrl) return false;
    const ratioFor = (url) => {
      const opt = opts.find((o) => (o.rtspUrl || '') === url);
      const w = Number(opt?.width || opt?.Width);
      const h = Number(opt?.height || opt?.Height);
      if (!w || !h) return null;
      return w / h;
    };
    const a = ratioFor(liveUrl);
    const b = ratioFor(detectUrl);
    if (a == null || b == null) return false;
    return Math.abs(a - b) > 0.05;
  })();

  async function save() {
    const newLive = (draft.liveStreamUrl || '').trim();
    const prevLive = (device?.rtspUrl || '').trim();
    if (newLive && newLive !== prevLive) {
      await applyLiveStream(newLive);
    }
    onSaveConfig({
      ...configDraftFor(existing, cameraId, liveFallback),
      liveStreamUrl: draft.liveStreamUrl,
      streamUrl: draft.streamUrl,
      fallbackStreamUrl: draft.fallbackStreamUrl,
    });
  }

  const profileChips = (selectedUrl, onPick) => (
    streamProfiles?.options?.length > 0 ? (
      <div style={{display:'flex', gap:'6px', flexWrap:'wrap', marginBottom:'4px'}}>
        {streamProfiles.options.map((opt) => {
          const url = opt.rtspUrl || '';
          const isCurrent = selectedUrl === url;
          const label = `${opt.name || opt.Name || opt.profileToken} — ${opt.encoding || opt.Encoding} ${opt.width || opt.Width}×${opt.height || opt.Height}`;
          return (
            <button key={opt.profileToken || opt.ProfileToken} type="button" className={`quiet${isCurrent ? ' active' : ''}`} style={{fontSize:'11px'}}
              onClick={() => onPick(url)} title={url}>
              {isCurrent ? '✓ ' : ''}{label}
            </button>
          );
        })}
      </div>
    ) : null
  );

  return (
    <section className="saved-tab-panel" style={{display:'flex', flexDirection:'column', gap:'10px'}}>
      <p style={{margin:0, fontSize:'12px', color:'var(--text-muted,#94a3b8)'}}>
        {t('rec.streamsIntro')}
      </p>
      <div style={{display:'flex', gap:'8px', alignItems:'center'}}>
        <button type="button" className="quiet" style={{fontSize:'12px'}} onClick={fetchStreamProfiles} disabled={streamProfilesLoading}>
          {streamProfilesLoading ? t('rec.loadingStreams') : t('rec.detectStreams')}
        </button>
        {streamProfiles?.options?.length >= 2 && (
          <button type="button" className="quiet" style={{fontSize:'12px'}} onClick={autoConfigureStreams}>
            {t('rec.autoConfigure')}
          </button>
        )}
        {streamProfilesError && <span style={{fontSize:'12px', color:'#ef4444'}}>{streamProfilesError}</span>}
      </div>
      {aspectMismatch && (
        <p style={{margin:0, fontSize:'12px', color:'#f59e0b'}}>
          {t('rec.aspectMismatch')}
        </p>
      )}

      <label className="field-label" style={{gap:'4px'}}>
        <span style={{fontSize:'12px', fontWeight:'600'}}>{t('rec.liveStream')}</span>
        {profileChips(draft.liveStreamUrl, (url) => setDraft((d) => ({ ...d, liveStreamUrl: url })))}
        <input type="text" value={draft.liveStreamUrl || ''} placeholder="rtsp://user:pass@ip/stream1"
          onChange={(e) => setDraft({ ...draft, liveStreamUrl: e.target.value })} />
      </label>

      <label className="field-label" style={{gap:'4px'}}>
        <span style={{fontSize:'12px', fontWeight:'600'}}>{t('rec.detectStream')} <span style={{fontWeight:'normal', color:'var(--text-muted,#94a3b8)'}}>{t('rec.detectStreamHint')}</span></span>
        {profileChips(draft.streamUrl, (url) => setDraft((d) => ({ ...d, streamUrl: url })))}
        <input type="text" value={draft.streamUrl || ''} placeholder="rtsp://user:pass@ip/stream2"
          onChange={(e) => setDraft({ ...draft, streamUrl: e.target.value })} />
      </label>

      <label className="field-label" style={{gap:'4px'}}>
        <span style={{fontSize:'12px', fontWeight:'600'}}>{t('rec.fallbackStream')} <span style={{fontWeight:'normal', color:'var(--text-muted,#94a3b8)'}}>{t('rec.fallbackStreamHint')}</span></span>
        {profileChips(draft.fallbackStreamUrl, (url) => setDraft((d) => ({ ...d, fallbackStreamUrl: url })))}
        <input type="text" value={draft.fallbackStreamUrl || ''} placeholder="rtsp://user:pass@ip/stream1  (optional)"
          onChange={(e) => setDraft({ ...draft, fallbackStreamUrl: e.target.value })} />
      </label>

      <div className="settings-actions">
        <button type="button" onClick={save} disabled={busy || !canManage}>
          <span className="btn-icon"><Ico n="save" /> {t('rec.saveStreams')}</span>
        </button>
      </div>
    </section>
  );
}

