import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';
import { apiBase } from '../lib/helpers';

// Timeline playback: continuous, wall-clock review across segment boundaries and
// across cameras.
//
// The unit here is a MOMENT, not a file. Everything on screen — the shaded bar, the
// cursor, every tile — is a rendering of one number, `cursor`, in unix seconds. That is
// what makes multi-camera sync a property of the design rather than a feature bolted on:
// the tiles do not synchronise with each other, they each answer the same question about
// the same instant, and a camera that has no footage at that instant says so instead of
// showing an unrelated frame.

// TIMELINE_SPANS are the zoom levels, in seconds of visible window.
const TIMELINE_SPANS = [600, 3600, 6 * 3600, 24 * 3600, 7 * 86400];

// PLAYBACK_RATES are the offered speeds. 8× is the practical ceiling for a browser
// decoding a surveillance stream; past it frames are dropped rather than played fast,
// which looks like corrupt footage.
const PLAYBACK_RATES = [0.25, 0.5, 1, 2, 4, 8];

// MAX_TIMELINE_CAMERAS mirrors the API's own cap. Enforced here too so the screen
// refuses before the request rather than surfacing a 400 as a broken bar.
const MAX_TIMELINE_CAMERAS = 8;

// FOLLOWER_DRIFT_SECONDS is how far a tile may drift from the cursor before it is
// re-seeked. Independent <video> elements decoding at the same rate diverge slowly, and
// correcting every frame would stutter each tile; correcting past two seconds keeps a
// wall readable without fighting the decoders.
const FOLLOWER_DRIFT_SECONDS = 2;

// hevcPlaybackSupported reports whether this browser can decode HEVC natively, so a
// stored-HEVC segment is only transcoded server-side for browsers that need it.
let _tlHevc = null;
function hevcPlaybackSupported() {
  if (_tlHevc !== null) return _tlHevc;
  try {
    const v = document.createElement('video');
    _tlHevc = !!(v.canPlayType('video/mp4; codecs="hvc1.1.6.L93.B0"')
      || v.canPlayType('video/mp4; codecs="hev1.1.6.L93.B0"'));
  } catch (_) {
    _tlHevc = false;
  }
  return _tlHevc;
}

// segmentSrc is the streaming URL for one segment.
//
// Set directly on the element rather than fetched into a blob, which is what the
// per-segment modal player does. A blob has to arrive completely before the first frame
// shows and holds the whole clip in memory; a plain src lets the browser issue Range
// requests, so seeking to minute nine of a fifteen-minute segment fetches minute nine.
// Timeline playback is nothing but seeking, so this is the difference between the feature
// working and the feature being a download queue. It authenticates on the session cookie,
// the same way the live tiles and snapshot images already do.
function segmentSrc(segmentId, codec) {
  const base = `${apiBase()}/api/recording/segments/${segmentId}/download`;
  if (String(codec || '').toLowerCase() === 'hevc' && !hevcPlaybackSupported()) {
    return `${base}?transcode=h264`;
  }
  return base;
}

function clamp(v, lo, hi) {
  return Math.max(lo, Math.min(hi, v));
}

// formatClock renders a moment for the ruler and the readout, in the viewer's zone.
// Everything crossing the API is UTC unix seconds; the conversion happens only here.
function formatClock(unix, withDate = false) {
  const d = new Date(unix * 1000);
  const pad = (n) => String(n).padStart(2, '0');
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  if (!withDate) return time;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${time}`;
}

function unixToLocalInput(unix) {
  const d = new Date(unix * 1000);
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function localInputToUnix(value) {
  if (!value) return 0;
  const ms = new Date(value).getTime();
  return Number.isNaN(ms) ? 0 : Math.floor(ms / 1000);
}

// formatGap renders how much dead air a snap skipped, in a unit the number survives.
//
// This exists because rounding it to whole minutes made the note say "skipped 0 minutes"
// for anything under thirty seconds — which states that nothing was skipped, on the one
// tile whose whole purpose is to say that something was. The unit follows the magnitude
// so the figure is never rounded away.
function formatGap(seconds, t) {
  const s = Math.max(1, Math.round(Number(seconds) || 0));
  if (s < 60) return t('tl.gapSeconds', { n: s });
  if (s < 3600) return t('tl.gapMinutes', { n: Math.round(s / 60) });
  const h = Math.floor(s / 3600);
  const m = Math.round((s % 3600) / 60);
  return m > 0 ? t('tl.gapHoursMinutes', { h, m }) : t('tl.gapHours', { n: h });
}

// rulerTicks picks a tick interval that yields a readable number of labels for the
// visible span, and returns the tick moments aligned to it.
function rulerTicks(from, to) {
  const span = Math.max(1, to - from);
  const steps = [10, 30, 60, 300, 600, 1800, 3600, 3 * 3600, 6 * 3600, 12 * 3600, 86400];
  const step = steps.find((s) => span / s <= 12) || 86400;
  const ticks = [];
  // Align to the local clock, not to `from`: a ruler labelled 14:07, 14:17, 14:27 is
  // arithmetically correct and useless for finding 14:20 in an incident report.
  const offsetMin = new Date(from * 1000).getTimezoneOffset() * 60;
  let first = Math.ceil((from - offsetMin) / step) * step + offsetMin;
  for (let t = first; t <= to && ticks.length < 40; t += step) ticks.push(t);
  return { ticks, step };
}

// TimelineTile is one camera's video surface plus the state of that camera at the cursor.
function TimelineTile({ camera, seek, muted, isClock, videoRef, onEnded, onTimeUpdate }) {
  const t = useT();
  const src = seek?.found ? segmentSrc(seek.segmentId, seek.codec) : '';
  return (
    <div className={`tl-tile${isClock ? ' is-clock' : ''}`}>
      <div className="tl-tile-head">
        <span className="tl-tile-name">{camera.name || t('tl.cameraN', { n: camera.id })}</span>
        {isClock ? <span className="tl-tile-badge">{t('tl.clockSource')}</span> : null}
      </div>
      <div className="tl-tile-body">
        {src ? (
          <video
            ref={videoRef}
            className="tl-video"
            playsInline
            muted={muted}
            preload="auto"
            src={src}
            onEnded={onEnded}
            onTimeUpdate={onTimeUpdate}
          />
        ) : (
          <div className="tl-tile-empty">
            <Ico n="wifi" sz={26} />
            <span>{t('tl.noFootageHere')}</span>
          </div>
        )}
      </div>
      {/* A snapped tile is showing a DIFFERENT moment from the one on the bar. Saying so
          is the whole point: an operator must be able to tell "this camera missed the
          incident" from "the player put me in the wrong place". */}
      {seek?.found && seek.snapped ? (
        <p className="tl-tile-note">
          {t('tl.snappedNote', {
            gap: formatGap(seek.gapSeconds, t),
            time: formatClock(seek.resolvedAt),
          })}
        </p>
      ) : null}
    </div>
  );
}

export function TimelinePlayback({ cameras = [], authHeader }) {
  const t = useT();
  const savedCameras = useMemo(
    () => (cameras || []).filter((c) => Number(c?.id) > 0),
    [cameras],
  );

  const [selectedIds, setSelectedIds] = useState([]);
  // The visible window is ONE value, not two.
  //
  // Its end and its width are edited by two different controls, and as separate pieces of
  // state a change to each in the same tick reads the other's pre-change value out of the
  // closure — so setting the zoom and then the end time (an entirely ordinary thing to do)
  // applied one and quietly discarded the other. Held together, and mirrored into a ref
  // that is updated synchronously, a second edit in the same tick sees the first.
  const [win, setWin] = useState(() => ({ end: Math.floor(Date.now() / 1000), span: 3600 }));
  const winRef = useRef(win);
  const [cursor, setCursor] = useState(() => Math.floor(Date.now() / 1000) - 1800);
  const [rate, setRate] = useState(1);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  const [report, setReport] = useState(null);
  const [events, setEvents] = useState([]);
  const [eventsDenied, setEventsDenied] = useState(false);
  const [seeks, setSeeks] = useState({});
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const videoRefs = useRef({});
  // clockCameraId is which tile's currentTime advances the cursor. Chosen from the seek
  // results rather than fixed to the first tile, because a tile with no footage at the
  // cursor has no clock to offer and would freeze playback for every other camera.
  const clockCameraIdRef = useRef(0);
  const seeksRef = useRef({});
  const advancingRef = useRef(false);

  const windowEnd = win.end;
  const spanSeconds = win.span;
  const windowStart = windowEnd - spanSeconds;
  const cursorRef = useRef(cursor);
  useEffect(() => { winRef.current = win; }, [win]);
  useEffect(() => { cursorRef.current = cursor; }, [cursor]);

  // Default to the first camera so the screen is never an empty frame with a hint.
  useEffect(() => {
    if (selectedIds.length === 0 && savedCameras.length > 0) {
      setSelectedIds([Number(savedCameras[0].id)]);
    }
  }, [savedCameras, selectedIds.length]);

  const headers = useMemo(() => (authHeader ? { Authorization: authHeader } : {}), [authHeader]);
  const idsKey = selectedIds.join(',');

  // ---- the bar -------------------------------------------------------------

  useEffect(() => {
    if (!idsKey) { setReport(null); return undefined; }
    let alive = true;
    setLoading(true);
    setError('');
    const url = `${apiBase()}/api/recording/timeline?cameraId=${encodeURIComponent(idsKey)}`
      + `&from=${windowStart}&to=${windowEnd}`;
    fetch(url, { credentials: 'include', headers })
      .then(async (r) => {
        const payload = await r.json().catch(() => null);
        if (!alive) return;
        const body = payload?.data?.result ?? payload?.result ?? payload;
        if (!r.ok || !body || !Array.isArray(body.cameras)) {
          // Surface the server's own words. "Ask for a shorter window" is actionable;
          // "could not load the timeline" sends the operator to a support ticket.
          setError(payload?.message || payload?.data?.message || t('tl.loadFailed'));
          setReport(null);
          return;
        }
        setReport(body);
      })
      .catch(() => { if (alive) { setError(t('tl.loadFailed')); setReport(null); } })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [idsKey, windowStart, windowEnd, headers, t]);

  // ---- detection marks -----------------------------------------------------
  //
  // Fetched from /api/vision/alerts, NOT from the timeline endpoint, because they are a
  // different page grant. A role granted Recordings but not Alerts gets the bar and no
  // marks — a 403 here disables the marks and says so, rather than failing the screen.
  useEffect(() => {
    if (!idsKey) { setEvents([]); return undefined; }
    let alive = true;
    const filters = JSON.stringify([
      { fieldName: 'CameraId', compare: 7, value: selectedIds },
      { fieldName: 'CreatedAt', compare: 5, value: windowStart },
      { fieldName: 'CreatedAt', compare: 4, value: windowEnd },
    ]);
    const url = `${apiBase()}/api/vision/alerts?status=detections&limit=500&offset=0`
      + `&filters=${encodeURIComponent(filters)}`
      + `&sorters=${encodeURIComponent(JSON.stringify([{ fieldName: 'CreatedAt', sort: 1 }]))}`;
    fetch(url, { credentials: 'include', headers })
      .then(async (r) => {
        if (!alive) return;
        if (r.status === 401 || r.status === 403) { setEventsDenied(true); setEvents([]); return; }
        const payload = await r.json().catch(() => null);
        const items = payload?.data?.result?.items || payload?.result?.items || [];
        setEventsDenied(false);
        setEvents(Array.isArray(items) ? items : []);
      })
      .catch(() => { if (alive) setEvents([]); });
    return () => { alive = false; };
  }, [idsKey, windowStart, windowEnd, headers, selectedIds]);

  // ---- seeking -------------------------------------------------------------

  // seekTo resolves one moment for every selected camera and points each tile at the
  // segment and offset the server named. Always a round trip: the resolution rule lives
  // once, on the server, and on an appliance evicting its oldest footage under disk
  // pressure a client-side index can name a segment that no longer exists.
  const seekTo = useCallback(async (at, { resume = false } = {}) => {
    if (!idsKey) return;
    const url = `${apiBase()}/api/recording/timeline/seek?cameraId=${encodeURIComponent(idsKey)}&at=${Math.floor(at)}`;
    let results = [];
    try {
      const r = await fetch(url, { credentials: 'include', headers });
      const payload = await r.json().catch(() => null);
      const body = payload?.data?.result ?? payload?.result ?? payload;
      results = Array.isArray(body?.cameras) ? body.cameras : [];
    } catch (_) {
      setError(t('tl.seekFailed'));
      return;
    }
    const next = {};
    for (const res of results) next[res.cameraId] = res;
    seeksRef.current = next;
    setSeeks(next);

    // The clock is whichever selected camera actually has footage at this moment, in the
    // operator's chosen order. When none does, the cursor still has to advance or
    // playback dies in the gap — handled by the wall-clock fallback below.
    clockCameraIdRef.current = selectedIds.find((id) => next[id]?.found && !next[id]?.snapped)
      || selectedIds.find((id) => next[id]?.found)
      || 0;

    // Apply positions after the element has the new src. Setting currentTime before the
    // metadata is loaded is silently discarded by every browser, which lands playback at
    // the START of the segment — a scrub to 14:52 that plays 14:45 and looks plausible.
    requestAnimationFrame(() => {
      for (const id of selectedIds) {
        const el = videoRefs.current[id];
        const res = next[id];
        if (!el || !res?.found) continue;
        const applyPosition = () => {
          try { el.currentTime = Math.max(0, res.offsetSeconds); } catch (_) { /* not seekable yet */ }
          // playbackRate is reset to 1 by the element on every source change. Re-applying
          // it here rather than only when the speed button is pressed is what keeps a 4×
          // review at 4× across a segment boundary instead of silently dropping to real
          // time partway through.
          el.playbackRate = rate;
          if (resume) el.play().catch(() => {});
        };
        if (el.readyState >= 1) applyPosition();
        else el.addEventListener('loadedmetadata', applyPosition, { once: true });
      }
    });
  }, [idsKey, headers, selectedIds, rate, t]);

  // Re-resolve whenever the operator moves the cursor by hand or changes the camera set.
  // Not on every cursor change — during playback the cursor is an OUTPUT of the videos,
  // and re-seeking on it would restart the clip on every frame.
  const jumpTo = useCallback((at) => {
    setCursor(at);
    seekTo(at, { resume: playing });
  }, [seekTo, playing]);

  useEffect(() => {
    if (!idsKey) return;
    seekTo(cursor, { resume: false });
    // Intentionally keyed on the camera set only: a change of window or cursor must not
    // reload every tile.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idsKey]);

  // ---- playback ------------------------------------------------------------

  useEffect(() => {
    for (const id of selectedIds) {
      const el = videoRefs.current[id];
      if (!el) continue;
      el.playbackRate = rate;
      if (playing) el.play().catch(() => {}); else el.pause();
    }
  }, [playing, rate, selectedIds, seeks]);

  // The cursor advances from the clock tile's own currentTime, so the bar tracks the
  // frames actually decoded rather than a timer that drifts away from them whenever the
  // decoder stalls, buffers or the tab is backgrounded.
  const handleTimeUpdate = useCallback((cameraId) => {
    if (clockCameraIdRef.current !== cameraId) return;
    const res = seeksRef.current[cameraId];
    const el = videoRefs.current[cameraId];
    if (!res?.found || !el) return;
    // Ignore a position reported by a source this seek did not resolve to.
    //
    // Between a scrub and the element loading its new segment, the OLD clip goes on
    // firing timeupdate. Pairing that position with the new segment's start puts the
    // cursor at a moment neither of them is showing — and while playing, the
    // keep-the-cursor-visible effect above then drags the whole window after it, so an
    // operator who moves the window mid-playback watches it spring back. Checking the
    // element is actually playing the segment the cursor is being computed from closes
    // that window entirely.
    if (!el.currentSrc || !el.currentSrc.includes(`/segments/${res.segmentId}/`)) return;
    const segStart = res.resolvedAt - res.offsetSeconds;
    setCursor(segStart + el.currentTime);
  }, []);

  // When a tile reaches the end of its segment it resolves ITSELF forward. Only the
  // clock tile moves everybody, because a follower running out of footage is a hole in
  // that camera, not a reason to reposition the wall.
  const handleEnded = useCallback(async (cameraId) => {
    const res = seeksRef.current[cameraId];
    if (!res?.found) return;
    const isClock = clockCameraIdRef.current === cameraId;
    const at = res.segmentEndsAt > 0 ? res.segmentEndsAt : Math.floor(cursor);
    if (isClock) {
      if (advancingRef.current) return;
      advancingRef.current = true;
      setCursor(at);
      await seekTo(at, { resume: playing });
      advancingRef.current = false;
      return;
    }
    // Follower: re-resolve just this tile at the shared cursor so it rejoins when its
    // footage resumes, without disturbing anyone else.
    try {
      const r = await fetch(
        `${apiBase()}/api/recording/timeline/seek?cameraId=${cameraId}&at=${Math.floor(at)}`,
        { credentials: 'include', headers },
      );
      const payload = await r.json().catch(() => null);
      const body = payload?.data?.result ?? payload?.result ?? payload;
      const one = Array.isArray(body?.cameras) ? body.cameras[0] : null;
      if (!one) return;
      seeksRef.current = { ...seeksRef.current, [cameraId]: one };
      setSeeks((prev) => ({ ...prev, [cameraId]: one }));
    } catch (_) { /* the tile simply stays where it stopped */ }
  }, [cursor, seekTo, playing, headers]);

  // Drift correction. Followers are left alone while they track; only a tile more than a
  // couple of seconds away from the cursor is nudged, and only when it is not the clock.
  useEffect(() => {
    if (!playing) return undefined;
    const timer = setInterval(() => {
      for (const id of selectedIds) {
        if (clockCameraIdRef.current === id) continue;
        const el = videoRefs.current[id];
        const res = seeksRef.current[id];
        if (!el || !res?.found || el.readyState < 1) continue;
        const segStart = res.resolvedAt - res.offsetSeconds;
        const want = cursor - segStart;
        if (want < 0 || want > el.duration) continue;
        if (Math.abs(el.currentTime - want) > FOLLOWER_DRIFT_SECONDS) {
          try { el.currentTime = want; } catch (_) { /* ignore */ }
        }
      }
    }, 5000);
    return () => clearInterval(timer);
  }, [playing, selectedIds, cursor]);

  // Keep the cursor visible WHILE PLAYING, and only ever by moving forward.
  //
  // The guards are the whole point. Without the `playing` check the bar follows the
  // playhead even when nobody is playing, so an operator who sets the window to last
  // night watches it snap straight back to wherever the cursor happens to sit — which
  // makes the window control useless for the one thing it is for. And without the
  // forward-only check the same effect drags the window BACKWARDS to a paused cursor,
  // which is the same bug wearing the opposite sign.
  useEffect(() => {
    if (!playing) return;
    if (cursor > windowEnd) {
      const next = { end: Math.floor(cursor) + Math.floor(spanSeconds / 4), span: spanSeconds };
      winRef.current = next;
      setWin(next);
    }
  }, [playing, cursor, windowEnd, spanSeconds]);

  // moveWindow is the one way the visible span changes by hand. When the operator moves
  // the window somewhere the cursor is not, the cursor goes with it — otherwise playback
  // continues off-screen and the tiles show a moment that is nowhere on the bar.
  const moveWindow = useCallback((patch) => {
    const next = { ...winRef.current, ...patch };
    next.end = Math.floor(next.end);
    next.span = Math.floor(next.span);
    // Written to the ref BEFORE the state update so a second control changed in the same
    // tick composes with this one instead of overwriting it.
    winRef.current = next;
    setWin(next);
    const at = cursorRef.current;
    if (at < next.end - next.span || at > next.end) {
      jumpTo(next.end - next.span);
    }
  }, [jumpTo]);

  // ---- bar interaction -----------------------------------------------------

  // atFromClientX maps a click to the moment under it.
  //
  // It measures against the TRACK THAT WAS CLICKED, taken from the event, and that is
  // load-bearing rather than incidental. Everything drawn on a track — the shaded spans,
  // the detection marks, the cursor — is positioned as a percentage of the track's own
  // box, but the track sits inside a row that begins with a fixed-width camera label, so
  // the track's box is inset from the surrounding panel's. Measuring the click against
  // the panel instead put every seek a constant fraction of the window late: about 5.6%
  // of the visible span, which is three minutes on an hour and eighty on a day. Nothing
  // about it looks wrong — a segment loads, the readout shows a time, the frames play —
  // so it survives every check that does not compare where the cursor lands with where
  // the operator clicked.
  const atFromClientX = useCallback((clientX, trackEl) => {
    if (!trackEl) return cursor;
    const rect = trackEl.getBoundingClientRect();
    // Read the fraction from the START edge in writing order, so the bar scrubs the
    // right way round under an RTL locale instead of running backwards.
    const rtl = getComputedStyle(trackEl).direction === 'rtl';
    const raw = (clientX - rect.left) / Math.max(1, rect.width);
    const fraction = clamp(rtl ? 1 - raw : raw, 0, 1);
    return Math.round(windowStart + fraction * spanSeconds);
  }, [cursor, windowStart, spanSeconds]);

  const positionPct = useCallback(
    (at) => clamp(((at - windowStart) / spanSeconds) * 100, 0, 100),
    [windowStart, spanSeconds],
  );

  function toggleCamera(id) {
    setSelectedIds((prev) => {
      if (prev.includes(id)) return prev.filter((x) => x !== id);
      if (prev.length >= MAX_TIMELINE_CAMERAS) return prev;
      return [...prev, id];
    });
  }

  const { ticks } = rulerTicks(windowStart, windowEnd);
  const cameraById = useMemo(
    () => new Map(savedCameras.map((c) => [Number(c.id), c])),
    [savedCameras],
  );
  const eventsByCamera = useMemo(() => {
    const map = new Map();
    for (const ev of events) {
      const id = Number(ev.cameraId);
      if (!map.has(id)) map.set(id, []);
      map.get(id).push(ev);
    }
    return map;
  }, [events]);

  const atLive = windowEnd >= Math.floor(Date.now() / 1000) - 5;

  return (
    <section className="tl-screen">
      <header className="tl-head">
        <h3>
          <span className="btn-icon"><Ico n="film" /> {t('tl.title')}</span>
        </h3>
        <HelpButton slug="recordings" anchor="finding" />
      </header>
      <p className="field-hint">{t('tl.intro')}</p>

      {error ? <FormAlert message={error} /> : null}

      <div className="tl-cameras" role="group" aria-label={t('tl.pickCameras')}>
        {savedCameras.length === 0 ? (
          <p className="field-hint">{t('tl.noCameras')}</p>
        ) : savedCameras.map((c) => {
          const id = Number(c.id);
          const on = selectedIds.includes(id);
          const full = !on && selectedIds.length >= MAX_TIMELINE_CAMERAS;
          return (
            <label key={id} className={`tl-camera-chip${on ? ' is-on' : ''}${full ? ' is-full' : ''}`}>
              <input type="checkbox" checked={on} disabled={full} onChange={() => toggleCamera(id)} />
              <span>{c.name || t('tl.cameraN', { n: id })}</span>
            </label>
          );
        })}
        {selectedIds.length >= MAX_TIMELINE_CAMERAS ? (
          <span className="field-hint">{t('tl.cameraCap', { n: MAX_TIMELINE_CAMERAS })}</span>
        ) : null}
      </div>

      <div className="tl-transport">
        <button type="button" className="btn-icon" onClick={() => jumpTo(cursor - 10)} title={t('tl.back10')}>
          <Ico n="arr-left" /> {t('tl.gapSeconds', { n: 10 })}
        </button>
        <button
          type="button"
          className="primary btn-icon"
          onClick={() => setPlaying((p) => !p)}
          aria-pressed={playing}
        >
          <Ico n={playing ? 'pause' : 'play'} /> {playing ? t('tl.pause') : t('tl.play')}
        </button>
        <button type="button" className="btn-icon" onClick={() => jumpTo(cursor + 10)} title={t('tl.fwd10')}>
          {t('tl.gapSeconds', { n: 10 })} <Ico n="arr-right" />
        </button>

        <div className="tl-rates" role="group" aria-label={t('tl.speed')}>
          {PLAYBACK_RATES.map((r) => (
            <button
              key={r}
              type="button"
              className={`tl-rate${rate === r ? ' is-on' : ''}`}
              onClick={() => setRate(r)}
              aria-pressed={rate === r}
            >
              {r}×
            </button>
          ))}
        </div>

        <label className="tl-mute">
          <input type="checkbox" checked={!muted} onChange={(e) => setMuted(!e.target.checked)} />
          <span>{t('tl.audio')}</span>
        </label>

        <span className="tl-readout" aria-live="polite">{formatClock(cursor, true)}</span>
      </div>

      <div className="tl-window">
        <label>
          <span>{t('tl.zoom')}</span>
          <select value={spanSeconds} onChange={(e) => moveWindow({ span: Number(e.target.value) })}>
            {TIMELINE_SPANS.map((s) => (
              <option key={s} value={s}>{t('tl.spanLabel', { label: spanLabel(s, t) })}</option>
            ))}
          </select>
        </label>
        <label>
          <span>{t('tl.windowEnd')}</span>
          <input
            type="datetime-local"
            value={unixToLocalInput(windowEnd)}
            onChange={(e) => {
              const v = localInputToUnix(e.target.value);
              if (v > 0) moveWindow({ end: v });
            }}
          />
        </label>
        <button type="button" onClick={() => moveWindow({ end: Math.floor(Date.now() / 1000) })} disabled={atLive}>
          {t('tl.jumpNow')}
        </button>
        {loading ? <span className="field-hint">{t('common.loading')}</span> : null}
      </div>

      <div className="tl-bar-wrap">
        <div className="tl-ruler" aria-hidden="true">
          {ticks.map((tick) => (
            <span key={tick} className="tl-tick" style={{ insetInlineStart: `${positionPct(tick)}%` }}>
              {formatClock(tick)}
            </span>
          ))}
        </div>

        {selectedIds.map((id) => {
          const cam = report?.cameras?.find((c) => Number(c.cameraId) === id);
          const spans = cam?.spans || [];
          const marks = eventsByCamera.get(id) || [];
          return (
            <div key={id} className="tl-row">
              <span className="tl-row-label">
                {cameraById.get(id)?.name || t('tl.cameraN', { n: id })}
                <em>{cam ? t('tl.rowCoverage', { percent: cam.percent.toFixed(1) }) : ''}</em>
              </span>
              <div
                className="tl-track"
                role="slider"
                tabIndex={0}
                aria-label={t('tl.trackAria', { camera: cameraById.get(id)?.name || id })}
                aria-valuemin={windowStart}
                aria-valuemax={windowEnd}
                aria-valuenow={Math.floor(cursor)}
                aria-valuetext={formatClock(cursor, true)}
                onClick={(e) => jumpTo(atFromClientX(e.clientX, e.currentTarget))}
                onKeyDown={(e) => {
                  if (e.key === 'ArrowLeft') { e.preventDefault(); jumpTo(cursor - 10); }
                  if (e.key === 'ArrowRight') { e.preventDefault(); jumpTo(cursor + 10); }
                }}
              >
                {/* An empty track is the honest rendering of "no footage": the gaps are
                    the background, and only real footage is painted. */}
                {spans.map((sp) => (
                  <span
                    key={`${sp.from}-${sp.to}`}
                    className="tl-span"
                    style={{
                      insetInlineStart: `${positionPct(sp.from)}%`,
                      inlineSize: `${Math.max(0.2, positionPct(sp.to) - positionPct(sp.from))}%`,
                    }}
                    title={`${formatClock(sp.from)} – ${formatClock(sp.to)}`}
                  />
                ))}
                {marks.map((ev) => (
                  <button
                    key={ev.id}
                    type="button"
                    className="tl-mark"
                    style={{ insetInlineStart: `${positionPct(ev.createdAt)}%` }}
                    title={`${ev.label || ev.detectionType} — ${formatClock(ev.createdAt)}`}
                    onClick={(e) => { e.stopPropagation(); jumpTo(Number(ev.createdAt)); }}
                    aria-label={t('tl.markAria', {
                      label: ev.label || ev.detectionType,
                      time: formatClock(ev.createdAt),
                    })}
                  />
                ))}
                <span className="tl-cursor" style={{ insetInlineStart: `${positionPct(cursor)}%` }} />
              </div>
            </div>
          );
        })}

        {eventsDenied ? <p className="field-hint">{t('tl.marksDenied')}</p> : null}
      </div>

      <div className={`tl-grid tl-grid-${Math.min(selectedIds.length, 4)}`}>
        {selectedIds.map((id) => (
          <TimelineTile
            key={id}
            camera={cameraById.get(id) || { id }}
            seek={seeks[id]}
            muted={muted || clockCameraIdRef.current !== id}
            isClock={clockCameraIdRef.current === id}
            videoRef={(el) => { videoRefs.current[id] = el; }}
            onEnded={() => handleEnded(id)}
            onTimeUpdate={() => handleTimeUpdate(id)}
          />
        ))}
      </div>
    </section>
  );
}

function spanLabel(seconds, t) {
  if (seconds < 3600) return t('tl.spanMinutes', { n: seconds / 60 });
  if (seconds < 86400) return t('tl.spanHours', { n: seconds / 3600 });
  return t('tl.spanDays', { n: seconds / 86400 });
}
