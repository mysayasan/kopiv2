import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { DataTable } from '@shared/DataTable';
import {formatFileSize,segmentDuration,segmentFilename,detectionTypeLabel,todayDateString,apiBase,formatTimestamp } from './lib/helpers';

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
  // In the myseliasan embed apiBase() is the node's command-proxy base. Recorded video
  // can't stream through the size-capped command proxy, so use the dedicated range-
  // streaming endpoint (…/recording-stream/{id}) which chunks the clip under the cap.
  const proxyMatch = apiBase().match(/^(.*)\/api\/nodes\/([^/]+)\/proxy$/);
  if (proxyMatch) {
    let url = `${proxyMatch[1]}/api/nodes/${proxyMatch[2]}/recording-stream/${seg.id}`;
    if (String(seg?.codec).toLowerCase() === 'hevc' && !hevcPlaybackSupported()) {
      url += '?transcode=h264';
    }
    return url;
  }
  const base = `${apiBase()}/api/recording/segments/${seg.id}/download`;
  if (String(seg?.codec).toLowerCase() === 'hevc' && !hevcPlaybackSupported()) {
    return `${base}?transcode=h264`;
  }
  return base;
}

// DEFAULT_MIN_CONF_PCT is the confidence floor the Object Search filter starts at, so
// results skip low-confidence noise by default; the user can lower it to 0 to see all.
const DEFAULT_MIN_CONF_PCT = 60;

// ObjectMultiSelect is a compact checklist dropdown for picking several object labels
// at once (e.g. car + person). Closed, it summarizes the selection; open, it shows a
// scrollable, color-coded checkbox list. Selecting none means "any object".
function ObjectMultiSelect({ options, value, onChange, anyLabel, someLabel, clearLabel, emptyLabel }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  useEffect(() => {
    if (!open) return undefined;
    const onDoc = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);
  const toggle = (label) => {
    const set = new Set(value);
    if (set.has(label)) set.delete(label); else set.add(label);
    onChange([...set]);
  };
  const summary = value.length === 0 ? anyLabel : (value.length <= 2 ? value.join(', ') : someLabel(value.length));
  return (
    <div className={`multi-select${open ? ' open' : ''}`} ref={ref}>
      <button type="button" className="multi-select-toggle" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span className="multi-select-summary">{summary}</span>
        <span className="multi-select-caret" aria-hidden="true">▾</span>
      </button>
      {open && (
        <div className="multi-select-menu" role="listbox">
          {value.length > 0 && (
            <button type="button" className="multi-select-clear" onClick={() => onChange([])}>{clearLabel}</button>
          )}
          {options.length === 0 ? (
            <div className="multi-select-empty">{emptyLabel}</div>
          ) : options.map((o) => (
            <label key={o} className="multi-select-option">
              <input type="checkbox" checked={value.includes(o)} onChange={() => toggle(o)} />
              <span className={`object-tag object-tag--${objectCategory(o)}`}>{o}</span>
            </label>
          ))}
        </div>
      )}
    </div>
  );
}

// boxStrFromPeak turns a stored peakBox JSON ({x,y,w,h} normalized) into the "x,y,w,h"
// query form the frame endpoint draws, or "" when there is no usable box.
function boxStrFromPeak(peakBox) {
  if (!peakBox) return '';
  try {
    const b = JSON.parse(peakBox);
    if (b && Number(b.w) > 0 && Number(b.h) > 0) return `${b.x},${b.y},${b.w},${b.h}`;
  } catch (_) {}
  return '';
}

// segmentFrameUrl builds the footage-frame endpoint URL for a segment at `seek`, with an
// optional detection box + label and render width.
function segmentFrameUrl(segmentId, { seek = 0, boxStr = '', label = '', width } = {}) {
  const params = new URLSearchParams({ seek: String(Number(seek) || 0) });
  if (width) params.set('w', String(width));
  if (boxStr) {
    params.set('box', boxStr);
    if (label) params.set('label', label);
  }
  return `${apiBase()}/api/recording/segments/${segmentId}/frame?${params}`;
}

// SnapshotButton is a reusable footage cell: a screenshot of the moment (detection box
// drawn server-side when given), with translucent overlay buttons — play always, and
// camera (maximize) when onMaximize is supplied. Used both by Object Search results and
// the Recordings list, replacing plain play buttons with a self-identifying preview.
function SnapshotButton({ segmentId, seek = 0, boxStr = '', label = '', size, authHeader, onPlay, onMaximize }) {
  const t = useT();
  const [url, setUrl] = useState(null);
  const [failed, setFailed] = useState(false);
  const segId = Number(segmentId) || 0;
  const cls = `obs-thumb${size === 'sm' ? ' obs-thumb--sm' : ''}`;
  useEffect(() => {
    if (!segId) return undefined;
    let cancelled = false;
    let obj = null;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(segmentFrameUrl(segId, { seek, boxStr, label }), { credentials: 'include', headers });
        if (!resp.ok) throw new Error(`${resp.status}`);
        obj = URL.createObjectURL(await resp.blob());
        if (cancelled) { URL.revokeObjectURL(obj); return; }
        setUrl(obj);
      } catch (_) { if (!cancelled) setFailed(true); }
    })();
    return () => { cancelled = true; if (obj) URL.revokeObjectURL(obj); };
  }, [segId, seek, boxStr, label, authHeader]);

  if (!segId || failed) {
    return (
      <button type="button" className={`${cls} obs-thumb--empty`} onClick={onPlay} title={t('meta.playFootage')} aria-label={t('meta.playFootage')}>
        <Ico n="play" sz={16} />
      </button>
    );
  }
  return (
    <div className={cls}>
      {url ? <img src={url} alt="" /> : <span className="obs-thumb-spinner" />}
      {url && (
        <div className="obs-thumb-actions">
          <button type="button" className="obs-thumb-btn" onClick={onPlay} title={t('meta.playFootage')} aria-label={t('meta.playFootage')}>
            <Ico n="play" sz={16} />
          </button>
          {onMaximize && (
            <button type="button" className="obs-thumb-btn" onClick={onMaximize} title={t('meta.maximizeSnapshot')} aria-label={t('meta.maximizeSnapshot')}>
              <Ico n="camera" sz={16} />
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// objectCategory buckets a detected object label into a broad family so the object
// pills in the search results can be color-coded (person / vehicle / animal / fire).
const OBJECT_CATEGORY = {
  person: 'person',
  vehicle: 'vehicle', car: 'vehicle', truck: 'vehicle', bus: 'vehicle', motorcycle: 'vehicle', bicycle: 'vehicle', train: 'vehicle', boat: 'vehicle',
  animal: 'animal', bird: 'animal', cat: 'animal', dog: 'animal', horse: 'animal', sheep: 'animal', cow: 'animal', elephant: 'animal', bear: 'animal', zebra: 'animal', giraffe: 'animal', deer: 'animal', goat: 'animal', pig: 'animal', monkey: 'animal', rabbit: 'animal',
  fire: 'fire', smoke: 'fire',
};
function objectCategory(label) {
  return OBJECT_CATEGORY[String(label || '').toLowerCase()] || 'other';
}

// defaultSearchFrom returns the ISO date 7 days ago (the default search range start).
function defaultSearchFrom() {
  const d = new Date();
  d.setDate(d.getDate() - 7);
  return d.toISOString().slice(0, 10);
}

// PurgeNowCountdown is the cancellable confirmation for "Purge now": it deletes ALL
// footage + snapshots for the camera regardless of expiry, so — mirroring the factory-reset
// wipe — it counts down (default 5s) and auto-proceeds unless cancelled, with Cancel focused.
function PurgeNowCountdown({ cameraName, seconds = 5, onCancel, onProceed }) {
  const t = useT();
  const [remaining, setRemaining] = useState(seconds);
  const proceedRef = useRef(onProceed);
  proceedRef.current = onProceed;
  useEffect(() => {
    const started = Date.now();
    const id = setInterval(() => {
      const left = Math.max(0, seconds - Math.floor((Date.now() - started) / 1000));
      setRemaining(left);
      if (left <= 0) {
        clearInterval(id);
        if (proceedRef.current) proceedRef.current();
      }
    }, 250);
    return () => clearInterval(id);
  }, [seconds]);
  return (
    <div className="modal-backdrop">
      <div className="modal-card danger-modal" role="alertdialog" aria-modal="true">
        <h2><span className="btn-icon"><Ico n="warning" /> {t('rec.purgeNowTitle')}</span></h2>
        <p>{t('rec.purgeNowWarning', { name: cameraName || t('rec.thisCamera') })}</p>
        <p className="wipe-countdown">{t('rec.purgeNowCountdown', { n: remaining })}</p>
        <div className="modal-actions">
          <button type="button" className="quiet" autoFocus onClick={onCancel}>
            <span className="btn-icon"><Ico n="x" /> {t('rec.purgeNowCancel')}</span>
          </button>
          <button type="button" className="danger-solid" onClick={() => proceedRef.current && proceedRef.current()}>
            <span className="btn-icon"><Ico n="trash" /> {t('rec.purgeNowConfirm')}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// CameraRecordingsPanel is the per-camera recordings browser (date timeline, segment
// list, and playback) shown as a tab inside the camera node, beside AI. Per-camera
// recording config and stream URLs live in the Saved-camera Settings panel
// (CameraRecordingConfig / CameraStreamConfig). The camera is fixed by the caller —
// the camera node's tree drives selection — so there is no in-panel camera picker.
export function CameraRecordingsPanel({ camera, canManage = true, busy, authHeader, onDeleteSegment, onPurgeExpired, onPurgeNow, onReload, unacknowledgedAlertIds, onAcknowledgeAlert, alerts }) {
  const t = useT();
  const effectiveCameraId = Number(camera?.id) || 0;
  const selectedCamera = camera || null;
  const [purgeArmed, setPurgeArmed] = useState(false);
  const alertById = useMemo(
    () => new Map((alerts || []).map((a) => [Number(a.id), a])),
    [alerts],
  );
  const [downloading, setDownloading] = useState(null);
  const [playingSegment, setPlayingSegment] = useState(null);
  const [videoUrl, setVideoUrl] = useState(null);
  const [videoError, setVideoError] = useState(false);
  const [loadingVideo, setLoadingVideo] = useState(false);
  // When playback is launched from a metadata search hit, seek the player to the
  // moment the object was seen (offset into the covering segment).
  const [seekSeconds, setSeekSeconds] = useState(0);

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

  function playSegment(seg, seek = 0) {
    setSeekSeconds(seek > 0 ? seek : 0);
    setPlayingSegment(seg);
    setVideoError(false);
    // Stream directly: the browser walks the file via HTTP Range (the endpoint serves
    // bounded chunks). Blob-downloading the whole clip would exceed the tunnel's cap.
    setVideoUrl(segmentPlaybackUrl(seg));
    setLoadingVideo(false);
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
        <SnapshotButton
          segmentId={seg.id}
          seek={0}
          size="sm"
          authHeader={authHeader}
          onPlay={() => playSegment(seg)}
        />
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
      {purgeArmed ? (
        <PurgeNowCountdown
          cameraName={selectedCamera?.name || selectedCamera?.model || selectedCamera?.host}
          onCancel={() => setPurgeArmed(false)}
          onProceed={async () => {
            setPurgeArmed(false);
            if (onPurgeNow) await onPurgeNow(effectiveCameraId);
            // Refresh only this panel's segment list (not the whole page) so the
            // purged footage clears from the timeline immediately.
            loadBrowseSegments();
          }}
        />
      ) : null}
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
          {canManage && onPurgeNow ? (
            <button
              type="button"
              className="quiet danger-text"
              disabled={busy}
              title={t('rec.purgeNowTitle')}
              onClick={() => setPurgeArmed(true)}
            >
              <span className="btn-icon"><Ico n="trash" /> {t('rec.purgeNow')}</span>
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
              {videoError ? (
                <div className="video-loading-msg">{t('rec.playbackUnavailable')}</div>
              ) : null}
              {videoUrl && !videoError && (
                <video className="video-player" controls autoPlay src={videoUrl}
                  onError={() => setVideoError(true)}
                  onLoadedMetadata={(e) => {
                    if (seekSeconds > 0) {
                      try { e.currentTarget.currentTime = seekSeconds; } catch (_) {}
                    }
                  }} />
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

// CameraObjectSearchPanel is the dedicated "Object Search" tab: a detection-oriented
// search across a DATE RANGE, by camera, object and minimum confidence, with paged
// results. It defaults to the camera whose node it is opened from but can search any
// or all cameras (the camera filter + a Camera column). Object metadata is bound to
// recording, so every hit has footage — clicking Play opens the covering segment and
// seeks to the exact moment the object was seen.
export function CameraObjectSearchPanel({ camera, busy, authHeader, canManage = true }) {
  const t = useT();
  const currentCameraId = Number(camera?.id) || 0;

  // Camera list (for the camera filter + resolving the Camera column name).
  const [cameras, setCameras] = useState([]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/cameras?limit=500`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const payload = await resp.json();
        const items = payload?.data?.result?.items ?? payload?.result?.items ?? payload?.items ?? payload?.data?.result ?? payload?.result ?? payload;
        if (!cancelled) setCameras(Array.isArray(items) ? items : []);
      } catch (_) {}
    })();
    return () => { cancelled = true; };
  }, [authHeader]);
  const cameraName = useCallback((id) => {
    const c = cameras.find((x) => Number(x.id) === Number(id));
    return (c && (c.name || c.model || c.host)) || t('dash.cameraN', { id });
  }, [cameras, t]);

  // Recording configs tell us which cameras actually log object metadata (metadata is
  // bound to recording). Cameras with it off are still listed and searchable — but the
  // result area explains they have nothing to find until recording is turned on.
  const [configs, setConfigs] = useState([]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const resp = await fetch(`${apiBase()}/api/recording/config`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const payload = await resp.json();
        const items = payload?.data?.result ?? payload?.result ?? payload;
        if (!cancelled) setConfigs(Array.isArray(items) ? items : []);
      } catch (_) {}
    })();
    return () => { cancelled = true; };
  }, [authHeader]);
  const recordingOff = useCallback((id) => {
    if (!Number(id)) return false;
    const c = configs.find((x) => Number(x.cameraId) === Number(id));
    return !c || !(c.metadataEnabled ?? c.enabled);
  }, [configs]);

  const [camFilter, setCamFilter] = useState(currentCameraId);
  const [searchedCam, setSearchedCam] = useState(currentCameraId);
  const [fromDate, setFromDate] = useState(defaultSearchFrom);
  const [toDate, setToDate] = useState(todayDateString);
  const [labels, setLabels] = useState([]);
  const [objLabels, setObjLabels] = useState([]);
  const [minConfPct, setMinConfPct] = useState(DEFAULT_MIN_CONF_PCT);
  // Applied (searched) filters, read by loadObs without becoming a changing dep.
  const buildApplied = () => ({
    cam: camFilter,
    from: fromDate ? Math.floor(new Date(fromDate + 'T00:00:00').getTime() / 1000) : 0,
    to: toDate ? Math.floor(new Date(toDate + 'T23:59:59').getTime() / 1000) : 0,
    objs: objLabels.map((l) => l.trim().toLowerCase()).filter(Boolean),
    conf: Number(minConfPct) || 0,
  });
  const appliedRef = useRef({
    cam: currentCameraId,
    from: Math.floor(new Date(defaultSearchFrom() + 'T00:00:00').getTime() / 1000),
    to: Math.floor(new Date(todayDateString() + 'T23:59:59').getTime() / 1000),
    objs: [], conf: DEFAULT_MIN_CONF_PCT,
  });

  // Reset to this camera when the panel is opened from a different node.
  const [prevCam, setPrevCam] = useState(currentCameraId);
  const [reloadKey, setReloadKey] = useState(0);
  if (currentCameraId !== prevCam) {
    setPrevCam(currentCameraId);
    setCamFilter(currentCameraId);
    setSearchedCam(currentCameraId);
    setObjLabels([]); setMinConfPct(DEFAULT_MIN_CONF_PCT); setFromDate(defaultSearchFrom()); setToDate(todayDateString());
    appliedRef.current = { cam: currentCameraId, from: Math.floor(new Date(defaultSearchFrom() + 'T00:00:00').getTime() / 1000), to: Math.floor(new Date(todayDateString() + 'T23:59:59').getTime() / 1000), objs: [], conf: DEFAULT_MIN_CONF_PCT };
    setReloadKey((k) => k + 1);
  }

  // Object labels for the dropdown (scoped to the selected camera, or all).
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const q = camFilter > 0 ? `?cameraId=${camFilter}` : '';
        const resp = await fetch(`${apiBase()}/api/observations/labels${q}`, { credentials: 'include', headers });
        if (!resp.ok) return;
        const payload = await resp.json();
        const items = payload?.data?.result ?? payload?.result ?? payload;
        if (!cancelled) setLabels(Array.isArray(items) ? items : []);
      } catch (_) {}
    })();
    return () => { cancelled = true; };
  }, [camFilter, authHeader]);

  const OBS_PAGE_SIZE = 20;
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  function applySearch() {
    appliedRef.current = buildApplied();
    setSearchedCam(camFilter);
    setReloadKey((k) => k + 1);
  }

  // buildObsParams turns the applied filters (+ optional grid filters/sorters) into the
  // /api/observations query string. Shared by the paged grid and the export sweep so
  // an export always matches exactly what the table shows. compare codes: 5=>=, 6=<=,
  // 7=IN (a multi-value label match).
  const buildObsParams = useCallback((offset, limit, gridFilters, sorters) => {
    const { cam, from, to, objs, conf } = appliedRef.current;
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    if (cam > 0) params.set('cameraId', String(cam));
    const merged = [...(gridFilters || [])];
    if (from) merged.push({ fieldName: 'startedAt', compare: 5, value: from });
    if (to) merged.push({ fieldName: 'startedAt', compare: 6, value: to });
    if (objs && objs.length) merged.push({ fieldName: 'label', compare: 7, value: objs });
    if (conf > 0) merged.push({ fieldName: 'maxConfidence', compare: 5, value: conf / 100 });
    if (merged.length) params.set('filters', JSON.stringify(merged));
    if ((sorters || []).length) params.set('sorters', JSON.stringify(sorters));
    return params;
  }, []);

  const loadObs = useCallback(async ({ filters, sorters, offset, limit }) => {
    setLoading(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const params = buildObsParams(offset || 0, limit || OBS_PAGE_SIZE, filters, sorters);
      const resp = await fetch(`${apiBase()}/api/observations?${params}`, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      setRows(Array.isArray(result?.items) ? result.items : []);
      setTotal(typeof result?.total === 'number' ? result.total : 0);
    } catch (_) { setRows([]); setTotal(0); } finally { setLoading(false); }
  }, [authHeader, buildObsParams]);

  // Export: sweep every page of the current search (the repo caps each read at 100) so
  // the report covers the whole result set, not just the visible page, then generate
  // the file entirely client-side (no server round-trip beyond the data fetch).
  const [exporting, setExporting] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const exportRef = useRef(null);
  useEffect(() => {
    if (!exportOpen) return undefined;
    const onDoc = (e) => { if (exportRef.current && !exportRef.current.contains(e.target)) setExportOpen(false); };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [exportOpen]);
  const fetchAllRows = useCallback(async () => {
    const headers = authHeader ? { Authorization: authHeader } : {};
    const limit = 100;
    const all = [];
    let offset = 0;
    let total = limit;
    while (offset < total && offset < 100000) {
      const params = buildObsParams(offset, limit, [], []);
      const resp = await fetch(`${apiBase()}/api/observations?${params}`, { credentials: 'include', headers });
      if (!resp.ok) break;
      const payload = await resp.json();
      const result = payload?.data?.result ?? payload?.result ?? payload;
      const items = Array.isArray(result?.items) ? result.items : [];
      all.push(...items);
      total = typeof result?.total === 'number' ? result.total : all.length;
      offset += limit;
    }
    return all;
  }, [authHeader, buildObsParams]);
  const exportRowFields = (r) => ([
    cameraName(r.cameraId),
    r.label,
    r.maxCount > 1 ? r.maxCount : 1,
    `${Math.round((Number(r.maxConfidence) || 0) * 100)}%`,
    formatTimestamp(r.startedAt),
    formatTimestamp(r.endedAt),
    r.footagePending ? t('meta.footagePending') : t('meta.footageAvailable'),
  ]);
  async function doExport(kind) {
    setExportOpen(false);
    if (exporting) return;
    setExporting(true);
    try {
      const data = await fetchAllRows();
      if (kind === 'csv') exportCSV(data); else exportPDF(data);
    } catch (_) {} finally { setExporting(false); }
  }
  function exportCSV(data) {
    const esc = (v) => { const s = String(v ?? ''); return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s; };
    const head = [t('meta.thCamera'), t('meta.thObject'), t('meta.count'), t('meta.thConfidence'), t('meta.detectedFrom'), t('meta.detectedTo'), t('meta.thFootage')];
    const lines = [head.map(esc).join(',')];
    for (const r of data) lines.push(exportRowFields(r).map(esc).join(','));
    const csv = '﻿' + lines.join('\r\n'); // BOM so Excel/Calc read UTF-8
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `object-search-${todayDateString()}.csv`;
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  }
  function exportPDF(data) {
    const w = window.open('', '_blank');
    if (!w) return;
    const esc = (s) => String(s ?? '').replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
    const head = [t('meta.thCamera'), t('meta.thObject'), t('meta.count'), t('meta.thConfidence'), t('meta.detectedFrom'), t('meta.detectedTo'), t('meta.thFootage')];
    const camPart = appliedRef.current.cam > 0 ? cameraName(appliedRef.current.cam) : t('meta.allCameras');
    const objsPart = (appliedRef.current.objs && appliedRef.current.objs.length) ? appliedRef.current.objs.join(', ') : t('meta.anyObject');
    const sub = `${camPart} · ${objsPart} · ≥ ${appliedRef.current.conf}% · ${data.length} ${t('meta.results')}`;
    const rowsHtml = data.map((r) => `<tr>${exportRowFields(r).map((c) => `<td>${esc(c)}</td>`).join('')}</tr>`).join('');
    w.document.write(`<!doctype html><html><head><meta charset="utf-8"><title>${esc(t('meta.searchTitle'))}</title>
      <style>body{font-family:Arial,Helvetica,sans-serif;color:#111;margin:24px}h1{font-size:18px;margin:0 0 4px}p.sub{color:#555;font-size:12px;margin:0 0 16px}
      table{border-collapse:collapse;width:100%;font-size:11px}th,td{border:1px solid #ccc;padding:5px 7px;text-align:left}th{background:#f2f4f7}tr:nth-child(even) td{background:#fafbfc}</style>
      </head><body><h1>${esc(t('meta.searchTitle'))}</h1><p class="sub">${esc(sub)}</p>
      <table><thead><tr>${head.map((h) => `<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${rowsHtml}</tbody></table></body></html>`);
    w.document.close();
    w.focus();
    setTimeout(() => { try { w.print(); } catch (_) {} }, 350);
  }

  // Footage playback: open the covering segment and seek to the sighting moment.
  const [playing, setPlaying] = useState(null);
  const [playingRow, setPlayingRow] = useState(null);
  const [videoUrl, setVideoUrl] = useState(null);
  const [loadingVideo, setLoadingVideo] = useState(false);
  const [seekSeconds, setSeekSeconds] = useState(0);
  const [boxVisible, setBoxVisible] = useState(false);
  const seekedRef = useRef(false);
  // The peak-confidence bounding box (normalized 0..1) for the sighting being played,
  // drawn over the video so it's self-evident which object the row refers to.
  const peakBox = useMemo(() => {
    if (!playingRow || !playingRow.peakBox) return null;
    try {
      const b = JSON.parse(playingRow.peakBox);
      if (b && Number(b.w) > 0 && Number(b.h) > 0) return b;
    } catch (_) {}
    return null;
  }, [playingRow]);
  async function playFootage(row) {
    const segmentId = Number(row?.segmentId) || 0;
    if (!segmentId) return;
    const seg = { id: segmentId, codec: row.segmentCodec };
    const seek = Number(row.seekSeconds) || 0;
    setSeekSeconds(seek > 0 ? seek : 0);
    seekedRef.current = false;
    setBoxVisible(true);
    setPlayingRow(row);
    setPlaying(seg); setVideoUrl(null); setLoadingVideo(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const resp = await fetch(segmentPlaybackUrl(seg), { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      setVideoUrl(URL.createObjectURL(await resp.blob()));
    } catch (_) { setPlaying(null); setPlayingRow(null); } finally { setLoadingVideo(false); }
  }
  // Jump the player to the detected moment. Setting currentTime on loadedmetadata
  // alone is unreliable on transcoded/streamed footage (the browser can reset it
  // once real data arrives), so we (re)assert the seek on both loadedmetadata and
  // canplay until it takes, and clamp to the clip's real duration.
  function applySeek(v) {
    if (!v || seekSeconds <= 0 || seekedRef.current) return;
    const dur = v.duration;
    const target = Number.isFinite(dur) && dur > 0 ? Math.min(seekSeconds, dur - 0.5) : seekSeconds;
    if (target <= 0) return;
    if (Math.abs(v.currentTime - target) < 0.75) { seekedRef.current = true; return; }
    try { v.currentTime = target; } catch (_) {}
  }
  // Match the video element to the footage aspect ratio so it never letterboxes —
  // that keeps the normalized box coordinates mapping straight onto the element.
  function fitAspect(v) {
    if (v && v.videoWidth > 0 && v.videoHeight > 0) {
      v.style.aspectRatio = `${v.videoWidth} / ${v.videoHeight}`;
    }
  }
  // Reveal the box only around the detection moment so it tracks the object honestly
  // instead of sitting static while the scene moves on.
  function onVideoTime(e) {
    if (!peakBox) return;
    const near = Math.abs(e.currentTarget.currentTime - Math.max(seekSeconds, 0)) < 2.5;
    setBoxVisible((prev) => (prev === near ? prev : near));
  }
  function closeVideo() {
    setVideoUrl((p) => { if (p) URL.revokeObjectURL(p); return null; });
    setPlaying(null); setPlayingRow(null); setLoadingVideo(false); setBoxVisible(false);
  }
  useEffect(() => {
    if (!playing) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') closeVideo(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [playing]);

  // Maximize: open the snapshot large (a higher-res render of the same boxed frame).
  const [maxRow, setMaxRow] = useState(null);
  const [maxUrl, setMaxUrl] = useState(null);
  const [maxLoading, setMaxLoading] = useState(false);
  async function openMaximize(row) {
    setMaxRow(row); setMaxUrl(null); setMaxLoading(true);
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      const url = segmentFrameUrl(Number(row.segmentId), { seek: row.seekSeconds, boxStr: boxStrFromPeak(row.peakBox), label: row.label, width: 1280 });
      const resp = await fetch(url, { credentials: 'include', headers });
      if (!resp.ok) throw new Error(`${resp.status}`);
      setMaxUrl(URL.createObjectURL(await resp.blob()));
    } catch (_) { setMaxRow(null); } finally { setMaxLoading(false); }
  }
  function closeMaximize() {
    setMaxUrl((p) => { if (p) URL.revokeObjectURL(p); return null; });
    setMaxRow(null); setMaxLoading(false);
  }
  useEffect(() => {
    if (!maxRow) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') closeMaximize(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [maxRow]);

  const columns = [
    { key: 'startedAt', label: t('meta.thTime'), filterable: false, render: (_v, row) => formatTimestamp(row.startedAt) },
    { key: 'cameraId', label: t('meta.thCamera'), filterable: false, render: (_v, row) => cameraName(row.cameraId) },
    { key: 'label', label: t('meta.thObject'), filterable: false, render: (_v, row) => (<span className={`object-tag object-tag--${objectCategory(row.label)}`}>{row.label}{row.maxCount > 1 ? ` ×${row.maxCount}` : ''}</span>) },
    { key: 'maxConfidence', label: t('meta.thConfidence'), filterable: false, render: (v) => `${Math.round((Number(v) || 0) * 100)}%` },
    {
      key: 'actions', label: t('meta.thFootage'), filterable: false,
      render: (_v, row) => (
        row.footagePending || !Number(row.segmentId) ? (
          <span className="footage-pending" title={t('meta.footagePendingHint')}>{t('meta.footagePending')}</span>
        ) : (
          <SnapshotButton
            segmentId={row.segmentId}
            seek={row.seekSeconds}
            boxStr={boxStrFromPeak(row.peakBox)}
            label={row.label}
            authHeader={authHeader}
            onPlay={() => playFootage(row)}
            onMaximize={() => openMaximize(row)}
          />
        )
      ),
    },
  ];

  return (
    <section className="camera-object-search-panel">
      <div className="toolbar">
        <div>
          <h2 className="section-title">{t('meta.searchTitle')}</h2>
          <p className="section-subtitle">{t('meta.searchSub')}</p>
        </div>
        <div className="toolbar-actions">
          <div className={`export-menu${exportOpen ? ' open' : ''}`} ref={exportRef}>
            <button type="button" className="quiet" onClick={() => setExportOpen((o) => !o)} disabled={exporting || total === 0} aria-expanded={exportOpen}>
              <span className="btn-icon"><Ico n="download" /> {exporting ? t('meta.exporting') : t('meta.export')}</span>
            </button>
            {exportOpen && (
              <div className="export-menu-list" role="menu">
                <button type="button" role="menuitem" onClick={() => doExport('csv')}>{t('meta.exportCsv')}</button>
                <button type="button" role="menuitem" onClick={() => doExport('pdf')}>{t('meta.exportPdf')}</button>
              </div>
            )}
          </div>
        </div>
      </div>

      <section className="settings-panel">
      <form className="object-search-filters" onSubmit={(e) => { e.preventDefault(); applySearch(); }}>
        <div className="field field--camera">
          <span className="field-label">{t('meta.camera')}</span>
          <select value={camFilter} onChange={(e) => setCamFilter(Number(e.target.value))}>
            <option value={0}>{t('meta.allCameras')}</option>
            {cameras.map((c) => (
              <option key={c.id} value={c.id}>
                {(c.name || c.model || c.host || `#${c.id}`)}{recordingOff(c.id) ? ` · ${t('meta.recordingOffTag')}` : ''}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <span className="field-label">{t('meta.from')}</span>
          <input type="date" value={fromDate} max={toDate || todayDateString()} onChange={(e) => setFromDate(e.target.value)} />
        </div>
        <div className="field">
          <span className="field-label">{t('meta.to')}</span>
          <input type="date" value={toDate} max={todayDateString()} onChange={(e) => setToDate(e.target.value)} />
        </div>
        <div className="field field--object">
          <span className="field-label">{t('meta.object')}</span>
          <ObjectMultiSelect
            options={labels}
            value={objLabels}
            onChange={setObjLabels}
            anyLabel={t('meta.anyObject')}
            someLabel={(n) => t('meta.objectsN', { n })}
            clearLabel={t('meta.clearSelection')}
            emptyLabel={t('meta.noObjectsYet')}
          />
        </div>
        <div className="field field--conf">
          <span className="field-label">{t('meta.minConfidence')}</span>
          <div className="conf-input">
            <input type="number" min="0" max="100" step="5" value={minConfPct}
              onChange={(e) => setMinConfPct(Math.max(0, Math.min(100, Number(e.target.value) || 0)))} />
            <span className="conf-unit">%</span>
          </div>
        </div>
        <div className="field field-actions">
          <button type="submit" disabled={loading}>
            <span className="btn-icon"><Ico n="search" /> {t('meta.search')}</span>
          </button>
        </div>
      </form>

      {searchedCam > 0 && recordingOff(searchedCam) && (
        <div className="obs-recording-off">
          <Ico n="info" sz={16} />
          <span>{t('meta.recordingOff', { camera: cameraName(searchedCam) })}</span>
        </div>
      )}

      <DataTable
        key={`obs-${reloadKey}`}
        serverMode
        rows={rows}
        columns={columns}
        total={total}
        pageSize={OBS_PAGE_SIZE}
        pageSizeOptions={[20, 50, 100]}
        busy={loading}
        onQuery={(q) => loadObs(q)}
        emptyText={t('meta.noResults')}
      />
      </section>

      {playing && (
        <div className="video-overlay" onClick={closeVideo}>
          <div className="video-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="video-dialog-header">
              <div className="video-dialog-title-group">
                <span className="video-dialog-title">{playingRow ? cameraName(playingRow.cameraId) : t('meta.footageTitle')}</span>
                {playingRow && (
                  <span className={`object-tag object-tag--${objectCategory(playingRow.label)}`}>
                    {playingRow.label}{playingRow.maxCount > 1 ? ` ×${playingRow.maxCount}` : ''}
                  </span>
                )}
              </div>
              <button type="button" className="video-dialog-close" onClick={closeVideo} aria-label={t('common.close')}>✕</button>
            </div>
            <div className="video-dialog-body">
              {loadingVideo && <div className="video-loading-msg">{t('rec.loadingVideo')}</div>}
              {videoUrl && (
                <div className="video-stage">
                  <video className="video-player" controls autoPlay src={videoUrl}
                    onLoadedMetadata={(e) => { applySeek(e.currentTarget); fitAspect(e.currentTarget); }}
                    onCanPlay={(e) => applySeek(e.currentTarget)}
                    onTimeUpdate={onVideoTime} />
                  {peakBox && (
                    <div
                      className={`detect-box${boxVisible ? ' show' : ''}`}
                      style={{
                        left: `${Math.max(0, Math.min(100, Number(peakBox.x) * 100))}%`,
                        top: `${Math.max(0, Math.min(100, Number(peakBox.y) * 100))}%`,
                        width: `${Math.max(0, Math.min(100, Number(peakBox.w) * 100))}%`,
                        height: `${Math.max(0, Math.min(100, Number(peakBox.h) * 100))}%`,
                      }}
                    >
                      <span className="detect-box-label">
                        {playingRow.label} · {Math.round((Number(playingRow.maxConfidence) || 0) * 100)}%
                      </span>
                    </div>
                  )}
                </div>
              )}
            </div>
            {playingRow && (
              <div className="video-dialog-meta">
                {formatTimestamp(playingRow.startedAt)} · {Math.round((Number(playingRow.maxConfidence) || 0) * 100)}%
                {seekSeconds > 0 ? ` · ${t('meta.jumpedTo', { time: `${Math.floor(seekSeconds / 60)}:${String(Math.floor(seekSeconds % 60)).padStart(2, '0')}` })}` : ''}
              </div>
            )}
          </div>
        </div>
      )}

      {maxRow && (
        <div className="video-overlay" onClick={closeMaximize}>
          <div className="snap-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="video-dialog-header">
              <div className="video-dialog-title-group">
                <span className="video-dialog-title">{cameraName(maxRow.cameraId)}</span>
                <span className={`object-tag object-tag--${objectCategory(maxRow.label)}`}>
                  {maxRow.label}{maxRow.maxCount > 1 ? ` ×${maxRow.maxCount}` : ''}
                </span>
              </div>
              <button type="button" className="video-dialog-close" onClick={closeMaximize} aria-label={t('common.close')}>✕</button>
            </div>
            <div className="snap-body">
              {maxLoading && <div className="video-loading-msg">{t('rec.loadingVideo')}</div>}
              {maxUrl && <img className="snap-image" src={maxUrl} alt="" />}
            </div>
            <div className="video-dialog-meta">
              {formatTimestamp(maxRow.startedAt)} · {Math.round((Number(maxRow.maxConfidence) || 0) * 100)}%
              <button type="button" className="quiet snap-play" onClick={() => { const r = maxRow; closeMaximize(); playFootage(r); }}>
                <span className="btn-icon"><Ico n="play" /> {t('meta.playFootage')}</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

// configDraftFor builds the full recording-config shape for a camera from its existing
// config (or sane defaults), so the per-camera Recording and Stream tabs — which both
// edit the SAME entity — always submit a complete config and never wipe each other's
// fields. liveFallback seeds the live-view URL from the camera's RTSP when unset.
export function configDraftFor(existing, cameraId, liveFallback = '') {
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
    metadataEnabled: existing?.metadataEnabled ?? false,
    metadataGapSeconds: existing?.metadataGapSeconds ?? 0,
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
      // Object-metadata sighting cooldown (seconds): how long an object may be unseen
      // before its next appearance is logged as a new Object Search entry. 0 = default.
      metadataGapSeconds: draft.metadataGapSeconds ?? 0,
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
        <label className="field-label">
          <span>{t('rec.metaCooldown')}</span>
          <input type="number" min="0" max="600" value={draft.metadataGapSeconds ?? 0}
            title={t('rec.metaCooldownHint')}
            onChange={(e) => setDraft({ ...draft, metadataGapSeconds: Math.max(0, Number(e.target.value) || 0) })} />
          <span className="field-hint">{t('rec.metaCooldownHint')}</span>
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
          // A detect-only stream (AI frame source) runs even when NVR recording is
          // OFF — it is NOT recording footage, so it must not read as "Recording
          // active" (green). Show it as a distinct blue "detect-only" state.
          const isDetect = rs.mode === 'detect';
          const isErr = rs.state === 'error';
          const isOk = rs.state === 'streaming' && !isDetect;
          const isDetectActive = isDetect && rs.state === 'streaming';
          const tone = isErr ? '239,68,68' : isOk ? '34,197,94' : isDetectActive ? '59,130,246' : '148,163,184';
          const dot = isErr ? '#ef4444' : isOk ? '#22c55e' : isDetectActive ? '#3b82f6' : '#94a3b8';
          const label = isErr ? t('rec.recError') : isOk ? t('rec.recActive') : isDetectActive ? t('rec.recDetectOnly') : t('rec.recStopped');
          return (
            <div style={{marginTop: '12px', padding: '10px 12px', borderRadius: '6px', background: `rgba(${tone},0.1)`, border: `1px solid rgba(${tone},0.3)`}}>
              <div style={{display:'flex', alignItems:'center', gap:'8px', marginBottom: rs.lastError || rs.liveDir ? '6px' : '0'}}>
                <span style={{width:'8px', height:'8px', borderRadius:'50%', background: dot, display:'inline-block', flexShrink:0}} />
                <strong style={{fontSize:'13px'}}>{label}</strong>
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

