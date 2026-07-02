import { useState, useEffect, useRef, useMemo } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { maxCrossingLines } from '../lib/constants';
import {fallbackLiveSource,normalizeStreamConfig,roundedPoint,parseZonePolygons,zonePolygonsText,normalizeLineConfig,waitForIceGathering,createWebRTCAnswer,shouldUseMJPEGForTracks,apiBase,fetchTalkCapability,createTalkAnswer } from '../lib/helpers';

// DetectionFrameBackdrop shows the exact frame the AI detector samples for a
// camera (honoring the capture mode / recording stream). Zones and lines are drawn
// on this so "what you draw on" equals "what gets detected" — even when the
// recording and live streams differ. object-fit:fill preserves the normalized
// (0–1) coordinate mapping the overlay uses.
//
// The next frame is preloaded and only swapped in once decoded (double-buffering)
// so the background never blanks/blinks, and refresh PAUSES while the user is
// dragging a point so the scene stays stable mid-draw. This is a pure UI display
// (a cheap re-read of the cached siphon frame) — it never affects detection.
function DetectionFrameBackdrop({ cameraId, paused, refreshSignal }) {
  const t = useT();
  const [src, setSrc] = useState('');
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  // refreshSignal is a bump counter from the toolbox "refresh frame" button — it is
  // in the dependency list so incrementing it re-runs the effect, which immediately
  // fetches a fresh frame (and restarts the poll cadence).
  useEffect(() => {
    if (!cameraId) {
      setSrc('');
      return undefined;
    }
    let cancelled = false;
    const base = `${apiBase()}/api/vision/cameras/${cameraId}/frame`;
    const loadNext = () => {
      if (pausedRef.current) {
        return;
      }
      const next = `${base}?t=${Date.now()}`;
      const img = new Image();
      img.onload = () => {
        if (!cancelled) setSrc(next);
      };
      img.src = next;
    };
    loadNext();
    const id = setInterval(loadNext, 700);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [cameraId, refreshSignal]);

  if (!cameraId) {
    return null;
  }
  if (!src) {
    return <div className="detection-frame-backdrop detection-frame-loading" />;
  }
  return (
    <img
      className="detection-frame-backdrop"
      src={src}
      alt={t('prev.detectionFrame')}
      draggable={false}
    />
  );
}

export function LiveViewport({ deviceId, title, authHeader, streamConfig, rtspTracks, healthStatus, streamKey, sourceUrl, startDelayMs = 0 }) {
  const t = useT();
  const videoRef = useRef(null);
  const audioRef = useRef(null);
  const [state, setState] = useState(() => '');
  const [fallbackSrc, setFallbackSrc] = useState('');
  const [hasAudio, setHasAudio] = useState(false);
  const [muted, setMuted] = useState(true);
  // Talk-back (browser mic → camera speaker). talkCap is the camera's capability
  // (null until probed); talking is true while an active mic session is open.
  const [talkCap, setTalkCap] = useState(null);
  const [talking, setTalking] = useState(false);
  const [talkBusy, setTalkBusy] = useState(false);
  const [talkError, setTalkError] = useState('');
  const talkPcRef = useRef(null);
  const talkStreamRef = useRef(null);
  // The health monitor is the source of truth for reachability. When a camera is
  // known offline, skip the WebRTC/MJPEG attempt entirely — both would otherwise
  // hang on the dead RTSP stream until their timeouts fire — and show an offline
  // placeholder. The tile auto-recovers when health flips back to online.
  const offline = (healthStatus || '').toLowerCase() === 'offline';

  useEffect(() => {
    if (!deviceId) {
      return undefined;
    }
    if (offline) {
      setFallbackSrc('');
      setHasAudio(false);
      setState(t('prev.stOffline'));
      return undefined;
    }
    const configValue = normalizeStreamConfig(streamConfig);
    const forceMJPEG = shouldUseMJPEGForTracks(rtspTracks);
    setFallbackSrc('');
    setHasAudio(false);
    setMuted(true);
    setState(forceMJPEG || !configValue.webrtc.enabled ? t('prev.stMjpeg') : t('prev.stConnecting'));

    if (forceMJPEG) {
      if (configValue.mjpegFallback.enabled) {
        setState(t('prev.stMjpegFallback'));
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState(t('prev.stNeedH264'));
      }
      return undefined;
    }

    if (!configValue.webrtc.enabled) {
      if (configValue.mjpegFallback.enabled) {
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState(t('prev.stLiveDisabled'));
      }
      return undefined;
    }
    if (typeof RTCPeerConnection === 'undefined') {
      if (configValue.mjpegFallback.enabled) {
        setState(t('prev.stMjpegFallback'));
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState(t('prev.stWebrtcUnavailable'));
      }
      return undefined;
    }

    let cancelled = false;
    const pc = new RTCPeerConnection({ iceServers: configValue.webrtc.iceServers });

    async function connect() {
      try {
        pc.addTransceiver('video', { direction: 'recvonly' });
        pc.addTransceiver('audio', { direction: 'recvonly' });
        pc.ontrack = (event) => {
          if (cancelled) return;
          if (event.track.kind === 'video' && videoRef.current) {
            // Use the browser-managed stream directly; avoids re-initialising the
            // GPU decode pipeline that a custom MediaStream can trigger on Windows.
            const stream = event.streams[0] || new MediaStream([event.track]);
            videoRef.current.srcObject = stream;
            videoRef.current.play().catch(() => {});
            setState(t('prev.stLive'));
          } else if (event.track.kind === 'audio' && audioRef.current) {
            setHasAudio(true);
            // Route audio to a dedicated element so the video element stays muted
            // (required for autoPlay) while audio is user-controlled independently.
            audioRef.current.srcObject = new MediaStream([event.track]);
          }
        };
        pc.onconnectionstatechange = () => {
          if (cancelled) return;
          const cs = pc.connectionState;
          if (cs === 'disconnected') {
            // Transient — ICE may recover; keep the video element alive and
            // show a status hint instead of switching to MJPEG fallback.
            setState(t('prev.stReconnecting'));
          } else if (cs === 'failed' || cs === 'closed') {
            if (configValue.mjpegFallback.enabled) {
              setState(t('prev.stMjpegFallback'));
              setFallbackSrc(fallbackLiveSource(deviceId));
            } else {
              setState(`WebRTC ${cs}`);
            }
          }
        };

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        await waitForIceGathering(pc);
        const answer = await createWebRTCAnswer(deviceId, pc.localDescription, authHeader, sourceUrl);
        if (cancelled) {
          return;
        }
        await pc.setRemoteDescription(answer);
      } catch (err) {
        if (!cancelled) {
          if (configValue.mjpegFallback.enabled) {
            setState(err.message || 'MJPEG fallback');
            setFallbackSrc(fallbackLiveSource(deviceId));
          } else {
            setState(err.message || t('prev.stWebrtcFailed'));
          }
          pc.close();
        }
      }
    }

    // Stagger GPU decode session creation across tiles so the hardware decoder
    // is not asked to initialise multiple sessions simultaneously, which can
    // trigger a Windows GPU TDR (monitor blackout) on some driver versions.
    let startTimer = null;
    if (startDelayMs > 0) {
      startTimer = setTimeout(connect, startDelayMs);
    } else {
      connect();
    }

    return () => {
      cancelled = true;
      if (startTimer !== null) clearTimeout(startTimer);
      if (videoRef.current?.srcObject) {
        videoRef.current.srcObject.getTracks().forEach((track) => track.stop());
        videoRef.current.srcObject = null;
      }
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current.srcObject = null;
      }
      pc.close();
    };
  }, [deviceId, authHeader, streamConfig, rtspTracks, healthStatus, streamKey, sourceUrl, startDelayMs]);

  function toggleMute() {
    setMuted((prev) => {
      const next = !prev;
      if (audioRef.current) {
        if (next) {
          audioRef.current.pause();
        } else {
          audioRef.current.play().catch(() => {});
        }
      }
      return next;
    });
  }

  // Probe talk-back capability whenever the camera changes (skip while offline).
  useEffect(() => {
    if (!deviceId || offline) {
      setTalkCap(null);
      return undefined;
    }
    let cancelled = false;
    fetchTalkCapability(deviceId, authHeader)
      .then((cap) => {
        if (!cancelled) setTalkCap(cap);
      })
      .catch(() => {
        if (!cancelled) setTalkCap(null);
      });
    return () => {
      cancelled = true;
    };
  }, [deviceId, authHeader, offline]);

  function stopTalk() {
    if (talkStreamRef.current) {
      talkStreamRef.current.getTracks().forEach((track) => track.stop());
      talkStreamRef.current = null;
    }
    if (talkPcRef.current) {
      talkPcRef.current.close();
      talkPcRef.current = null;
    }
    setTalking(false);
  }

  async function startTalk() {
    setTalkError('');
    if (typeof RTCPeerConnection === 'undefined' || !navigator.mediaDevices?.getUserMedia) {
      setTalkError(t('prev.talkUnavailable'));
      return;
    }
    setTalkBusy(true);
    try {
      // 8 kHz mono matches the camera's G.711 speaker codec; echo cancellation
      // stops the camera's own audio (played via the speaker button) feeding back.
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, channelCount: 1, sampleRate: 8000 },
      });
      talkStreamRef.current = stream;
      const pc = new RTCPeerConnection({ iceServers: normalizeStreamConfig(streamConfig).webrtc.iceServers });
      talkPcRef.current = pc;
      stream.getAudioTracks().forEach((track) => pc.addTrack(track, stream));
      pc.oniceconnectionstatechange = () => {
        const st = pc.iceConnectionState;
        if (st === 'failed' || st === 'closed' || st === 'disconnected') {
          stopTalk();
        }
      };
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      await waitForIceGathering(pc);
      const answer = await createTalkAnswer(deviceId, pc.localDescription, authHeader);
      await pc.setRemoteDescription(answer);
      setTalking(true);
    } catch (err) {
      stopTalk();
      setTalkError(err?.message || t('prev.talkFailed'));
    } finally {
      setTalkBusy(false);
    }
  }

  function toggleTalk() {
    if (talking) {
      stopTalk();
    } else {
      startTalk();
    }
  }

  // Stop any live mic session when the camera changes or the tile unmounts.
  useEffect(() => stopTalk, [deviceId]);

  return (
    <div className="live-frame">
      {offline ? (
        <div className="live-offline" aria-label={t('prev.offlineAria', { title })}>
          <Ico n="wifi" sz={28} />
          <span>{t('prev.cameraOffline')}</span>
        </div>
      ) : fallbackSrc ? (
        <img src={fallbackSrc} alt={t('prev.liveViewAria', { title })} />
      ) : (
        <video ref={videoRef} autoPlay muted playsInline aria-label={t('prev.liveViewAria', { title })} />
      )}
      <audio ref={audioRef} playsInline style={{ display: 'none' }} />
      {!offline && <span className="stream-state">{state}</span>}
      {hasAudio && (
        <button
          type="button"
          className="audio-mute-btn"
          onClick={toggleMute}
          aria-label={muted ? t('prev.unmute') : t('prev.mute')}
          title={muted ? t('prev.unmute') : t('prev.mute')}
        >
          <Ico n={muted ? 'volume-x' : 'volume-2'} sz={14} />
        </button>
      )}
      {!offline && talkCap?.supported && (
        <button
          type="button"
          className={`talk-btn${talking ? ' talking' : ''}`}
          onClick={toggleTalk}
          disabled={talkBusy}
          aria-label={talking ? t('prev.talkStop') : t('prev.talkStart')}
          title={talking ? t('prev.talkStop') : t('prev.talkStart')}
        >
          <Ico n={talking ? 'mic' : 'mic-off'} sz={14} />
        </button>
      )}
      {talkError ? <span className="talk-error" role="alert">{talkError}</span> : null}
    </div>
  );
}

// distanceToSegment returns the shortest distance from point p to the segment a–b
// (all in normalized 0–1 coordinates). Used to find which polygon edge a new
// point is closest to.
function distanceToSegment(p, a, b) {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const len2 = dx * dx + dy * dy;
  let t = len2 === 0 ? 0 : ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / len2;
  t = Math.max(0, Math.min(1, t));
  return Math.hypot(p[0] - (a[0] + t * dx), p[1] - (a[1] + t * dy));
}

// insertPointOnNearestEdge inserts p into the polygon between the two existing
// vertices whose edge is nearest to p, so adding a point splits the closest edge
// instead of always tacking onto the end (which made the new point jump back to
// the first/"starting" vertex). With fewer than 3 points there is no closed shape
// yet, so it just appends in click order while the initial triangle is built.
function insertPointOnNearestEdge(pts, p) {
  if (pts.length < 3) {
    return [...pts, p];
  }
  let bestIdx = pts.length - 1;
  let bestDist = Infinity;
  for (let i = 0; i < pts.length; i += 1) {
    const d = distanceToSegment(p, pts[i], pts[(i + 1) % pts.length]);
    if (d < bestDist) {
      bestDist = d;
      bestIdx = i;
    }
  }
  const next = [...pts];
  next.splice(bestIdx + 1, 0, p);
  return next;
}

// Grid-snap step (fraction of the frame) and keyboard-nudge step (Shift = coarse).
const GRID_STEP = 0.05;
const NUDGE_STEP = 0.01;
const NUDGE_STEP_COARSE = 0.05;

// snapToGrid quantizes a point to the coarse grid when snapping is on, so edges and
// handles line up tidily; otherwise the (already rounded) point passes through.
function snapToGrid(p, on) {
  if (!on) {
    return p;
  }
  return roundedPoint([Math.round(p[0] / GRID_STEP) * GRID_STEP, Math.round(p[1] / GRID_STEP) * GRID_STEP]);
}

// clampPoint keeps a point inside the frame (used for keyboard nudging).
function clampPoint(p) {
  return roundedPoint([Math.min(Math.max(p[0], 0), 1), Math.min(Math.max(p[1], 0), 1)]);
}

// pointInPolygon is an even-odd ray-cast test in normalized coords — used to detect
// a click inside the zone body (to drag the whole shape).
function pointInPolygon(p, pts) {
  if (pts.length < 3) {
    return false;
  }
  let inside = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i, i += 1) {
    const [xi, yi] = pts[i];
    const [xj, yj] = pts[j];
    const hit = (yi > p[1]) !== (yj > p[1]) && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi;
    if (hit) {
      inside = !inside;
    }
  }
  return inside;
}

// polygonArea returns the polygon's coverage as a fraction of the frame (0–1) via
// the shoelace formula; the frame is the unit square so the value is the coverage.
function polygonArea(pts) {
  if (pts.length < 3) {
    return 0;
  }
  let sum = 0;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i, i += 1) {
    sum += (pts[j][0] + pts[i][0]) * (pts[j][1] - pts[i][1]);
  }
  return Math.abs(sum) / 2;
}

// translatePoints shifts every point by (dx,dy), clamping the shift so the whole
// shape stays inside the frame (rigid translation — the shape is never deformed).
function translatePoints(pts, dx, dy) {
  if (!pts.length) {
    return pts;
  }
  const xs = pts.map((p) => p[0]);
  const ys = pts.map((p) => p[1]);
  const cdx = Math.min(Math.max(dx, -Math.min(...xs)), 1 - Math.max(...xs));
  const cdy = Math.min(Math.max(dy, -Math.min(...ys)), 1 - Math.max(...ys));
  return pts.map((p) => roundedPoint([p[0] + cdx, p[1] + cdy]));
}

// constrainToAxis snaps `moving` so the segment from `fixed` locks to the nearest
// 45° increment (horizontal / vertical / diagonal) — used for Shift-drag on lines.
function constrainToAxis(fixed, moving) {
  const dx = moving[0] - fixed[0];
  const dy = moving[1] - fixed[1];
  const len = Math.hypot(dx, dy);
  if (len === 0) {
    return moving;
  }
  const step = Math.PI / 4;
  const ang = Math.round(Math.atan2(dy, dx) / step) * step;
  return roundedPoint([fixed[0] + Math.cos(ang) * len, fixed[1] + Math.sin(ang) * len]);
}

// extendLineToBox stretches segment a–b along its own direction until both ends hit
// the frame border (slab clipping), so a tripwire spans the whole view.
function extendLineToBox(a, b) {
  const d = [b[0] - a[0], b[1] - a[1]];
  if (!d[0] && !d[1]) {
    return [a, b];
  }
  let t0 = -Infinity;
  let t1 = Infinity;
  for (let i = 0; i < 2; i += 1) {
    if (d[i] === 0) {
      if (a[i] < 0 || a[i] > 1) {
        return [a, b];
      }
    } else {
      let ta = (0 - a[i]) / d[i];
      let tb = (1 - a[i]) / d[i];
      if (ta > tb) {
        [ta, tb] = [tb, ta];
      }
      t0 = Math.max(t0, ta);
      t1 = Math.min(t1, tb);
    }
  }
  if (t0 > t1) {
    return [a, b];
  }
  return [
    roundedPoint([a[0] + d[0] * t0, a[1] + d[1] * t0]),
    roundedPoint([a[0] + d[0] * t1, a[1] + d[1] * t1]),
  ];
}

// DIRECTION_ORDER is the cycle the line-crossing direction toggle steps through.
const DIRECTION_ORDER = ['both', 'forward', 'reverse'];

// Zone-creation drag: a pointer move shorter than RECT_CLICK_THRESHOLD (normalized)
// counts as a click and drops a DEFAULT_BOX_SIZE square; a longer drag sizes the box.
const RECT_CLICK_THRESHOLD = 0.025;
const DEFAULT_BOX_SIZE = 0.2;

// rectPolygon builds an axis-aligned rectangle (4 points, clockwise) from two
// opposite corners.
function rectPolygon(a, b) {
  const x1 = Math.min(a[0], b[0]);
  const y1 = Math.min(a[1], b[1]);
  const x2 = Math.max(a[0], b[0]);
  const y2 = Math.max(a[1], b[1]);
  return [roundedPoint([x1, y1]), roundedPoint([x2, y1]), roundedPoint([x2, y2]), roundedPoint([x1, y2])];
}

// defaultBoxAt returns a `size`-wide square centered on `center`, nudged fully
// inside the frame when the click lands near an edge.
function defaultBoxAt(center, size) {
  const half = size / 2;
  const cx = Math.min(Math.max(center[0], half), 1 - half);
  const cy = Math.min(Math.max(center[1], half), 1 - half);
  return rectPolygon([cx - half, cy - half], [cx + half, cy + half]);
}

// DrawToolbox is the mode-aware floating tool palette overlaid on the drawing
// surface (Paint/Photoshop style): a grip + a vertical column of square icon
// buttons split into groups by a separator. `groups` is an array of item arrays;
// each item is { key, icon, label, onClick, disabled, active?, toggle? }. Tools
// (toggle:true) show a pressed/active state so the current interaction mode is
// visible; actions (undo/clear/…) are plain buttons. The tool set is supplied by
// the caller, so switching detection mode (zone vs line) swaps the whole palette.
function DrawToolbox({ groups, ariaLabel, moveLabel }) {
  const ref = useRef(null);
  // Once the user drags the palette we pin it to an absolute {left,top} (px,
  // relative to the drawing surface); until then it uses the CSS default corner.
  const [pos, setPos] = useState(null);
  const dragRef = useRef(null);

  function onGripDown(event) {
    const el = ref.current;
    if (!el || event.button !== 0) {
      return;
    }
    event.preventDefault();
    const box = el.getBoundingClientRect();
    dragRef.current = { dx: event.clientX - box.left, dy: event.clientY - box.top };
    event.currentTarget.setPointerCapture?.(event.pointerId);
  }

  function onGripMove(event) {
    const el = ref.current;
    const parent = el?.offsetParent;
    if (!dragRef.current || !parent) {
      return;
    }
    const prect = parent.getBoundingClientRect();
    const maxLeft = Math.max(0, prect.width - el.offsetWidth);
    const maxTop = Math.max(0, prect.height - el.offsetHeight);
    const left = Math.min(Math.max(0, event.clientX - prect.left - dragRef.current.dx), maxLeft);
    const top = Math.min(Math.max(0, event.clientY - prect.top - dragRef.current.dy), maxTop);
    setPos({ left, top });
  }

  function onGripUp(event) {
    dragRef.current = null;
    event.currentTarget.releasePointerCapture?.(event.pointerId);
  }

  return (
    <div
      ref={ref}
      className="draw-toolbox"
      role="toolbar"
      aria-label={ariaLabel}
      aria-orientation="vertical"
      style={pos ? { left: pos.left, top: pos.top, right: 'auto', bottom: 'auto' } : undefined}
    >
      <span
        className="draw-toolbox-grip"
        title={moveLabel}
        aria-label={moveLabel}
        onPointerDown={onGripDown}
        onPointerMove={onGripMove}
        onPointerUp={onGripUp}
        onPointerCancel={onGripUp}
      />
      {groups.filter((items) => items.length).map((items, gi) => (
        <div className="draw-toolbox-group" key={gi}>
          {items.map((it) => (
            <button
              key={it.key}
              type="button"
              className={`draw-tool${it.active ? ' active' : ''}`}
              onClick={it.onClick}
              disabled={it.disabled}
              title={it.label}
              aria-label={it.label}
              aria-pressed={it.toggle ? !!it.active : undefined}
            >
              <Ico n={it.icon} sz={16} />
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}

// rotatePointsAround rotates normalized [x,y] points by `deg` degrees (clockwise)
// about their centroid. `aspect` (frame width/height) corrects for the non-square
// display so the shape rotates as it looks on screen rather than shearing. Results
// are clamped to the [0,1] frame.
function rotatePointsAround(points, deg, aspect = 1) {
  if (!Array.isArray(points) || points.length < 2) return points;
  const cx = points.reduce((s, p) => s + p[0], 0) / points.length;
  const cy = points.reduce((s, p) => s + p[1], 0) / points.length;
  const rad = (deg * Math.PI) / 180;
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  const clamp = (v) => Math.max(0, Math.min(1, v));
  return points.map(([x, y]) => {
    const dx = (x - cx) * aspect;
    const dy = y - cy;
    const rx = dx * cos - dy * sin;
    const ry = dx * sin + dy * cos;
    return [clamp(cx + rx / aspect), clamp(cy + ry)];
  });
}

export function ZoneDrawingPreview({ camera, polygonValue, onPolygon, authHeader, streamConfig, disabled }) {
  const t = useT();
  const overlayRef = useRef(null);
  const [draggingIndex, setDraggingIndex] = useState(null);
  // Interaction tool mode: 'select' (drag points / drag whole zone / switch zone),
  // 'add' (click to insert a point), 'delete' (click a point to remove it). Defaults
  // to 'add' so a brand-new zone is drawn by clicking straight away.
  const [tool, setTool] = useState('add');
  const [snap, setSnap] = useState(false);
  const [selected, setSelected] = useState(null);
  const [bodyDragging, setBodyDragging] = useState(false);
  const [frameRefresh, setFrameRefresh] = useState(0);
  // Rubber-band zone creation: after "Add zone", the next press-drag-release on the
  // frame draws a rectangle (a plain click drops a default box). awaitingRect gates
  // it; rectPreview drives the live outline; rectRef holds the drag anchor.
  const [awaitingRect, setAwaitingRect] = useState(false);
  const [rectPreview, setRectPreview] = useState(null);
  const rectRef = useRef(null);
  // A rule can hold several zones (union — an object counts if it is inside ANY).
  // `zones` is the parsed list; `active` is the one being edited. Setting activeZone
  // to zones.length starts a NEW zone (the next placed point creates it).
  const [activeZone, setActiveZone] = useState(0);
  const zones = useMemo(() => parseZonePolygons(polygonValue), [polygonValue]);
  const active = Math.min(activeZone, zones.length);
  const points = zones[active] || [];
  const polygonPoints = points.map((point) => `${point[0] * 100},${point[1] * 100}`).join(' ');
  const totalCoverage = Math.min(100, Math.round(zones.reduce((sum, z) => sum + polygonArea(z), 0) * 100));

  // Undo history snapshots the WHOLE zone set before a mutating action, so undo/redo
  // step across point edits, zone add/delete and moves alike. lastValueRef tracks the
  // value WE committed, so an external change (switching rule/camera) clears history.
  const [history, setHistory] = useState([]);
  const [redoStack, setRedoStack] = useState([]);
  const lastValueRef = useRef(polygonValue);
  const draggedRef = useRef(false);
  const bodyDragRef = useRef(null);
  useEffect(() => {
    if (polygonValue !== lastValueRef.current) {
      lastValueRef.current = polygonValue;
      setHistory([]);
      setRedoStack([]);
      setSelected(null);
      setActiveZone(0);
    }
  }, [polygonValue]);

  function commitZones(nextZones) {
    const text = zonePolygonsText(nextZones);
    lastValueRef.current = text;
    onPolygon(text);
  }

  // commit replaces the ACTIVE zone's points (or appends a new zone when the active
  // index points past the end, i.e. a new zone is being started).
  function commit(nextPoints) {
    if (active >= zones.length) {
      commitZones(nextPoints.length ? [...zones, nextPoints] : zones);
      return;
    }
    commitZones(zones.map((z, i) => (i === active ? nextPoints : z)));
  }

  // pushHistory snapshots the current zone set before a mutating action. A fresh
  // action invalidates the redo stack (can't redo past a divergent edit).
  function pushHistory() {
    setHistory((h) => [...h, zones]);
    setRedoStack([]);
  }

  function undo() {
    if (!history.length) {
      return;
    }
    const prev = history[history.length - 1];
    setHistory(history.slice(0, -1));
    setRedoStack((r) => [...r, zones]);
    setSelected(null);
    commitZones(prev);
  }

  function redo() {
    if (!redoStack.length) {
      return;
    }
    const next = redoStack[redoStack.length - 1];
    setRedoStack(redoStack.slice(0, -1));
    setHistory((h) => [...h, zones]);
    setSelected(null);
    commitZones(next);
  }

  function addZone() {
    if (disabled) {
      return;
    }
    // Point the active index past the end and arm rubber-band creation — the next
    // press-drag-release draws the new zone (see the overlay handlers + stopDrag).
    setActiveZone(zones.length);
    setTool('add');
    setAwaitingRect(true);
    setSelected(null);
  }

  // chooseTool switches the interaction tool and cancels a pending zone-rectangle
  // (so picking Select/Add/Delete abandons an un-started "Add zone").
  function chooseTool(next) {
    setTool(next);
    setAwaitingRect(false);
    rectRef.current = null;
    setRectPreview(null);
  }

  function deleteZone() {
    if (disabled || !zones.length) {
      return;
    }
    pushHistory();
    const nextZones = zones.filter((_, i) => i !== active);
    commitZones(nextZones);
    setActiveZone((a) => Math.max(0, Math.min(a, nextZones.length - 1)));
    setSelected(null);
  }

  // rotateActive spins the active zone 15° clockwise about its centroid (aspect-
  // corrected so it rotates as drawn). Repeat clicks accumulate; undo reverts.
  function rotateActive() {
    if (disabled || points.length < 2) {
      return;
    }
    const rect = overlayRef.current?.getBoundingClientRect();
    const aspect = rect && rect.height > 0 ? rect.width / rect.height : 1;
    pushHistory();
    commit(rotatePointsAround(points, 15, aspect));
  }

  // rawPointFromEvent is the un-snapped normalized pointer position (for delta math
  // when dragging the whole shape); pointFromEvent adds grid-snap for placing points.
  function rawPointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return [(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height];
  }

  function pointFromEvent(event) {
    return snapToGrid(roundedPoint(rawPointFromEvent(event)), snap);
  }

  function addPoint(event) {
    if (disabled || !camera) {
      return;
    }
    pushHistory();
    commit(insertPointOnNearestEdge(points, pointFromEvent(event)));
    setSelected(null);
  }

  function deletePoint(index) {
    if (disabled) {
      return;
    }
    pushHistory();
    commit(points.filter((_, i) => i !== index));
    setSelected(null);
  }

  function movePoint(event) {
    if (disabled) {
      return;
    }
    // Live rubber-band rectangle while creating a new zone.
    if (rectRef.current) {
      setRectPreview({ start: rectRef.current.startSnap, current: pointFromEvent(event) });
      return;
    }
    // Dragging the zone body translates every point rigidly (see translatePoints).
    if (bodyDragRef.current) {
      const p = rawPointFromEvent(event);
      const dx = p[0] - bodyDragRef.current.last[0];
      const dy = p[1] - bodyDragRef.current.last[1];
      if (!draggedRef.current) {
        pushHistory();
        draggedRef.current = true;
      }
      commit(translatePoints(points, dx, dy));
      bodyDragRef.current.last = p;
      return;
    }
    if (draggingIndex === null) {
      return;
    }
    // Snapshot once per drag (on the first actual move) so the whole drag is a
    // single undo step rather than one per pointermove.
    if (!draggedRef.current) {
      pushHistory();
      draggedRef.current = true;
    }
    const nextPoints = [...points];
    nextPoints[draggingIndex] = pointFromEvent(event);
    commit(nextPoints);
  }

  function stopDrag(event) {
    // Finalize rubber-band zone creation: a real drag sizes the box, a bare click
    // drops a default square centered on the cursor.
    if (rectRef.current) {
      if (overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
        overlayRef.current.releasePointerCapture(event.pointerId);
      }
      const { startRaw, startSnap } = rectRef.current;
      const endRaw = rawPointFromEvent(event);
      const dragged = Math.hypot(endRaw[0] - startRaw[0], endRaw[1] - startRaw[1]) >= RECT_CLICK_THRESHOLD;
      const poly = dragged ? rectPolygon(startSnap, pointFromEvent(event)) : defaultBoxAt(startSnap, DEFAULT_BOX_SIZE);
      pushHistory();
      commit(poly);
      rectRef.current = null;
      setRectPreview(null);
      setAwaitingRect(false);
      setSelected(null);
      // Drop into Select so the fresh box can be moved/resized straight away.
      setTool('select');
      return;
    }
    if ((draggingIndex !== null || bodyDragRef.current) && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDraggingIndex(null);
    bodyDragRef.current = null;
    setBodyDragging(false);
    draggedRef.current = false;
  }

  // nudge/delete/undo/redo via keyboard when the overlay is focused. Arrow keys move
  // the selected vertex (Shift = coarse), Delete removes it, Ctrl/Cmd+Z / +Y undo/redo.
  function onKeyDown(event) {
    if (disabled) {
      return;
    }
    const mod = event.ctrlKey || event.metaKey;
    if (mod && (event.key === 'z' || event.key === 'Z')) {
      event.preventDefault();
      if (event.shiftKey) redo(); else undo();
      return;
    }
    if (mod && (event.key === 'y' || event.key === 'Y')) {
      event.preventDefault();
      redo();
      return;
    }
    if (selected === null || selected >= points.length) {
      return;
    }
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      deletePoint(selected);
      return;
    }
    const step = event.shiftKey ? NUDGE_STEP_COARSE : NUDGE_STEP;
    const deltas = { ArrowUp: [0, -step], ArrowDown: [0, step], ArrowLeft: [-step, 0], ArrowRight: [step, 0] };
    const d = deltas[event.key];
    if (!d) {
      return;
    }
    event.preventDefault();
    pushHistory();
    const nextPoints = [...points];
    nextPoints[selected] = clampPoint([points[selected][0] + d[0], points[selected][1] + d[1]]);
    commit(nextPoints);
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>{t('prev.detectionZone')}</h3>
        <div className="zone-drawer-pills">
          {zones.length > 1 ? <span className="status-pill">{t('prev.zoneIndex', { i: Math.min(active + 1, zones.length), n: zones.length })}</span> : null}
          <span className="status-pill">{t('prev.points', { n: points.length })}</span>
          {zones.length ? <span className="status-pill">{t('prev.coverage', { pct: totalCoverage })}</span> : null}
        </div>
      </header>
      <p className="zone-drawer-hint">{t('prev.zoneHint')}</p>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <DetectionFrameBackdrop cameraId={camera.id} paused={draggingIndex !== null || bodyDragging || rectPreview !== null} refreshSignal={frameRefresh} />
            <div
              ref={overlayRef}
              className={`zone-overlay tool-${tool}${awaitingRect ? ' tool-rect' : ''}`}
              role="button"
              tabIndex={0}
              aria-label={t('prev.drawZone')}
              onKeyDown={onKeyDown}
              onPointerDown={(event) => {
                if (event.button !== 0) {
                  return;
                }
                // Rubber-band new-zone creation takes precedence once armed.
                if (awaitingRect) {
                  overlayRef.current?.setPointerCapture?.(event.pointerId);
                  const snap = pointFromEvent(event);
                  rectRef.current = { startRaw: rawPointFromEvent(event), startSnap: snap };
                  setRectPreview({ start: snap, current: snap });
                  return;
                }
                if (tool === 'add') {
                  overlayRef.current?.setPointerCapture?.(event.pointerId);
                  addPoint(event);
                  return;
                }
                // Select mode: a press inside the active zone body grabs the whole shape.
                if (tool === 'select' && pointInPolygon(rawPointFromEvent(event), points)) {
                  overlayRef.current?.setPointerCapture?.(event.pointerId);
                  bodyDragRef.current = { last: rawPointFromEvent(event) };
                  setBodyDragging(true);
                  setSelected(null);
                }
              }}
              onPointerMove={movePoint}
              onPointerUp={stopDrag}
              onPointerCancel={stopDrag}
            >
              <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                {zones.map((zonePts, zi) => {
                  if (zi === active) {
                    return null;
                  }
                  // Inactive zones render dimmed; clicking one in select mode makes it
                  // the active (editable) zone.
                  const poly = zonePts.map((p) => `${p[0] * 100},${p[1] * 100}`).join(' ');
                  return (
                    <g key={`zone-${zi}`}>
                      {zonePts.length >= 3 ? (
                        <polygon
                          points={poly}
                          className="zone-shape inactive"
                          style={tool === 'select' ? { pointerEvents: 'fill', cursor: 'pointer' } : { pointerEvents: 'none' }}
                          onPointerDown={(event) => {
                            if (disabled || event.button !== 0 || tool !== 'select') {
                              return;
                            }
                            event.stopPropagation();
                            setActiveZone(zi);
                            setSelected(null);
                          }}
                        />
                      ) : null}
                      {zonePts.length >= 2 ? <polyline points={poly} className="zone-line inactive" /> : null}
                      {zonePts.length ? (
                        <text x={zonePts[0][0] * 100} y={zonePts[0][1] * 100 - 1} className="zone-index-label">{zi + 1}</text>
                      ) : null}
                    </g>
                  );
                })}
                {points.length >= 3 ? <polygon points={polygonPoints} className={`zone-shape${tool === 'select' ? ' grabbable' : ''}`} /> : null}
                {points.length >= 2 ? <polyline points={polygonPoints} className="zone-line" /> : null}
                {rectPreview ? (
                  <polygon
                    points={rectPolygon(rectPreview.start, rectPreview.current).map((p) => `${p[0] * 100},${p[1] * 100}`).join(' ')}
                    className="zone-shape zone-rect-preview"
                  />
                ) : null}
                {points.map((point, index) => (
                  <circle
                    key={`${point[0]}-${point[1]}-${index}`}
                    cx={point[0] * 100}
                    cy={point[1] * 100}
                    r="2.3"
                    className={`zone-point${tool === 'delete' ? ' deletable' : ''}${index === selected ? ' selected' : ''}`}
                    vectorEffect="non-scaling-stroke"
                    onPointerDown={(event) => {
                      if (disabled || event.button !== 0) {
                        return;
                      }
                      event.stopPropagation();
                      if (tool === 'delete') {
                        deletePoint(index);
                        return;
                      }
                      setSelected(index);
                      overlayRef.current?.setPointerCapture?.(event.pointerId);
                      setDraggingIndex(index);
                    }}
                  />
                ))}
              </svg>
            </div>
            <DrawToolbox
              ariaLabel={t('prev.tools')}
              moveLabel={t('prev.moveToolbox')}
              groups={[
                [
                  { key: 'select', icon: 'cursor', label: t('prev.toolSelect'), toggle: true, active: tool === 'select' && !awaitingRect, disabled, onClick: () => chooseTool('select') },
                  { key: 'add', icon: 'edit-2', label: t('prev.toolAddPoint'), toggle: true, active: tool === 'add' && !awaitingRect, disabled, onClick: () => chooseTool('add') },
                  { key: 'delete', icon: 'eraser', label: t('prev.toolDeletePoint'), toggle: true, active: tool === 'delete', disabled: disabled || !points.length, onClick: () => chooseTool('delete') },
                ],
                [
                  { key: 'add-zone', icon: 'plus', label: t('prev.addZone'), toggle: true, active: awaitingRect, disabled, onClick: addZone },
                  { key: 'del-zone', icon: 'trash', label: t('prev.deleteZone'), disabled: disabled || !zones.length, onClick: deleteZone },
                  { key: 'rotate', icon: 'rotate-cw', label: t('prev.rotate'), disabled: disabled || points.length < 2, onClick: rotateActive },
                ],
                [
                  { key: 'undo', icon: 'undo', label: t('prev.undo'), disabled: disabled || !history.length, onClick: undo },
                  { key: 'redo', icon: 'redo', label: t('prev.redo'), disabled: disabled || !redoStack.length, onClick: redo },
                ],
                [
                  { key: 'full', icon: 'maximize', label: t('prev.fullFrame'), disabled, onClick: () => { pushHistory(); commit([[0, 0], [1, 0], [1, 1], [0, 1]]); setSelected(null); } },
                  { key: 'box', icon: 'box', label: t('prev.centerBox'), disabled, onClick: () => { pushHistory(); commit([[0.25, 0.25], [0.75, 0.25], [0.75, 0.75], [0.25, 0.75]]); setSelected(null); } },
                  { key: 'flip-h', icon: 'flip-h', label: t('prev.flipH'), disabled: disabled || !points.length, onClick: () => { pushHistory(); commit(points.map((p) => roundedPoint([1 - p[0], p[1]]))); } },
                  { key: 'flip-v', icon: 'flip-v', label: t('prev.flipV'), disabled: disabled || !points.length, onClick: () => { pushHistory(); commit(points.map((p) => roundedPoint([p[0], 1 - p[1]]))); } },
                ],
                [
                  { key: 'snap', icon: 'grid4', label: t('prev.snapGrid'), toggle: true, active: snap, disabled, onClick: () => setSnap((s) => !s) },
                  { key: 'refresh', icon: 'refresh', label: t('prev.refreshFrame'), disabled, onClick: () => setFrameRefresh((n) => n + 1) },
                ],
              ]}
            />
          </>
        ) : (
          <div className="zone-empty-state">{t('prev.selectCamera')}</div>
        )}
      </div>
    </section>
  );
}

// crossingDirectionArrows renders the direction indicator for a crossing line: an
// arrow perpendicular to the line pointing to the side an object must move TOWARD
// to trigger (a double-headed arrow for "both"). Matches the backend convention in
// infra/vision/line_crossing.go — "forward" fires when an object crosses from the
// negative to the POSITIVE signedArea side of A→B, and the positive side lies in
// the direction (-dy, dx). Computed in the 0–100 SVG space; the sign of the side
// is preserved under the preview's non-uniform stretch, so the arrow always points
// to the correct side.
function crossingDirectionArrows(first, second, direction) {
  const x1 = first[0] * 100;
  const y1 = first[1] * 100;
  const x2 = second[0] * 100;
  const y2 = second[1] * 100;
  const mx = (x1 + x2) / 2;
  const my = (y1 + y2) / 2;
  const dx = x2 - x1;
  const dy = y2 - y1;
  const len = Math.hypot(dx, dy) || 1;
  const nx = -dy / len; // unit normal toward the "forward" (positive signedArea) side
  const ny = dx / len;
  const shaft = 8;
  const head = 3.4;
  const arrows = [];
  const addArrow = (ux, uy, key) => {
    const tx = mx + ux * shaft;
    const ty = my + uy * shaft;
    const px = -uy; // perpendicular to the arrow, for the head width
    const py = ux;
    const points = [
      `${tx},${ty}`,
      `${tx - ux * head + px * head * 0.6},${ty - uy * head + py * head * 0.6}`,
      `${tx - ux * head - px * head * 0.6},${ty - uy * head - py * head * 0.6}`,
    ].join(' ');
    arrows.push(<line key={`${key}-shaft`} x1={mx} y1={my} x2={tx} y2={ty} className="crossing-arrow" vectorEffect="non-scaling-stroke" />);
    arrows.push(<polygon key={`${key}-head`} points={points} className="crossing-arrow-head" />);
  };
  if (direction === 'forward' || direction === 'both') {
    addArrow(nx, ny, 'fwd');
  }
  if (direction === 'reverse' || direction === 'both') {
    addArrow(-nx, -ny, 'rev');
  }
  return arrows;
}

export function LineDrawingPreview({ camera, config, detectionType, onConfig, authHeader, streamConfig, disabled }) {
  const t = useT();
  const overlayRef = useRef(null);
  const [dragging, setDragging] = useState(null);
  // Interaction tool mode mirrors the zone drawer: 'select' (drag endpoints only),
  // 'add' (click to drop a new line), 'delete' (click an endpoint to remove its line).
  const [tool, setTool] = useState('add');
  const [snap, setSnap] = useState(false);
  const [selected, setSelected] = useState(null);
  const [bodyDragging, setBodyDragging] = useState(false);
  const [frameRefresh, setFrameRefresh] = useState(0);
  const bodyDragRef = useRef(null);
  const maxLines = detectionType === 'multi_line_crossing' ? maxCrossingLines : 1;
  const normalizedLine = normalizeLineConfig(config, detectionType);
  const lines = normalizedLine.lines.slice(0, maxLines);
  const direction = normalizedLine.direction || 'both';
  // Which line the shape-level actions (extend to edges) target: the selected one,
  // or the only line when there is exactly one.
  const targetLineIndex = selected ? selected.lineIndex : (lines.length === 1 ? 0 : -1);

  // Undo history mirrors the zone drawer: each action (add/move/clear) snapshots
  // the pre-action lines so Undo steps back through them. The parent re-parses and
  // re-normalizes the config on every commit (so the round-tripped value won't byte-
  // match what we sent), so external changes are detected with a self-commit flag
  // rather than value comparison: after our own commit the next render is accepted,
  // any other change clears the now-stale history.
  const [history, setHistory] = useState([]);
  const [redoStack, setRedoStack] = useState([]);
  const linesKey = JSON.stringify(lines);
  const lastKeyRef = useRef(linesKey);
  const selfCommitRef = useRef(false);
  const draggedRef = useRef(false);
  useEffect(() => {
    if (selfCommitRef.current) {
      selfCommitRef.current = false;
      lastKeyRef.current = linesKey;
      return;
    }
    if (linesKey !== lastKeyRef.current) {
      lastKeyRef.current = linesKey;
      setHistory([]);
      setRedoStack([]);
      setSelected(null);
    }
  }, [linesKey]);

  function commit(nextLines) {
    selfCommitRef.current = true;
    onConfig({ lines: nextLines.slice(0, maxLines) });
  }

  // pushHistory snapshots the current lines before a mutating action. A fresh
  // action invalidates the redo stack (can't redo past a divergent edit).
  function pushHistory() {
    setHistory((h) => [...h, lines]);
    setRedoStack([]);
  }

  function undo() {
    if (!history.length) {
      return;
    }
    const prev = history[history.length - 1];
    setHistory(history.slice(0, -1));
    setRedoStack((r) => [...r, lines]);
    setSelected(null);
    commit(prev);
  }

  function redo() {
    if (!redoStack.length) {
      return;
    }
    const next = redoStack[redoStack.length - 1];
    setRedoStack(redoStack.slice(0, -1));
    setHistory((h) => [...h, lines]);
    setSelected(null);
    commit(next);
  }

  // rawPointFromEvent is the un-snapped normalized pointer position (for delta math
  // when dragging a whole line); pointFromEvent adds grid-snap for placing endpoints.
  function rawPointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return [(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height];
  }

  function pointFromEvent(event) {
    return snapToGrid(roundedPoint(rawPointFromEvent(event)), snap);
  }

  function addLine(point = null) {
    if (disabled || !camera || lines.length >= maxLines) {
      return;
    }
    const start = point || [0.5, 0.25 + lines.length * 0.12];
    const end = roundedPoint([start[0], start[1] + 0.25]);
    pushHistory();
    commit([...lines, { id: `line-${lines.length + 1}`, points: [roundedPoint(start), end] }]);
    setSelected(null);
  }

  function deleteLine(lineIndex) {
    if (disabled) {
      return;
    }
    pushHistory();
    commit(lines.filter((_, i) => i !== lineIndex));
    setSelected(null);
  }

  // extendTargetLine stretches the target line to the frame edges along its heading.
  function extendTargetLine() {
    if (disabled || targetLineIndex < 0 || !lines[targetLineIndex]) {
      return;
    }
    pushHistory();
    commit(lines.map((line, i) => (i !== targetLineIndex
      ? line
      : { ...line, points: extendLineToBox(line.points[0], line.points[1]) })));
  }

  // rotateTargetLine spins the selected line 15° clockwise about its midpoint
  // (aspect-corrected). Repeat clicks accumulate; undo reverts.
  function rotateTargetLine() {
    if (disabled || targetLineIndex < 0 || !lines[targetLineIndex]) {
      return;
    }
    const rect = overlayRef.current?.getBoundingClientRect();
    const aspect = rect && rect.height > 0 ? rect.width / rect.height : 1;
    pushHistory();
    commit(lines.map((line, i) => (i !== targetLineIndex
      ? line
      : { ...line, points: rotatePointsAround(line.points, 15, aspect) })));
  }

  // cycleDirection steps the crossing direction (both → forward → reverse). This is a
  // config-level property (applies to all lines) so it goes straight to onConfig; the
  // on-canvas green arrow reflects the new value.
  function cycleDirection() {
    if (disabled) {
      return;
    }
    const next = DIRECTION_ORDER[(DIRECTION_ORDER.indexOf(direction) + 1) % DIRECTION_ORDER.length];
    onConfig({ direction: next });
  }

  function movePoint(event) {
    if (disabled) {
      return;
    }
    // Dragging a line body translates both of its endpoints rigidly.
    if (bodyDragRef.current) {
      const p = rawPointFromEvent(event);
      const dx = p[0] - bodyDragRef.current.last[0];
      const dy = p[1] - bodyDragRef.current.last[1];
      if (!draggedRef.current) {
        pushHistory();
        draggedRef.current = true;
      }
      const li = bodyDragRef.current.lineIndex;
      commit(lines.map((line, i) => (i !== li ? line : { ...line, points: translatePoints(line.points, dx, dy) })));
      bodyDragRef.current.last = p;
      return;
    }
    if (!dragging) {
      return;
    }
    // Snapshot once per drag (on the first actual move) so the whole drag is one
    // undo step rather than one per pointermove.
    if (!draggedRef.current) {
      pushHistory();
      draggedRef.current = true;
    }
    const nextLines = lines.map((line, lineIndex) => {
      if (lineIndex !== dragging.lineIndex) {
        return line;
      }
      const nextPoints = [...line.points];
      // Shift-drag locks the segment to the nearest 45° (relative to the fixed end).
      const moved = pointFromEvent(event);
      nextPoints[dragging.pointIndex] = event.shiftKey
        ? constrainToAxis(line.points[1 - dragging.pointIndex], moved)
        : moved;
      return { ...line, points: nextPoints };
    });
    commit(nextLines);
  }

  function stopDrag(event) {
    if ((dragging || bodyDragRef.current) && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDragging(null);
    bodyDragRef.current = null;
    setBodyDragging(false);
    draggedRef.current = false;
  }

  // Keyboard: arrows nudge the selected endpoint (Shift = coarse), Delete removes its
  // line, Ctrl/Cmd+Z / +Y undo/redo.
  function onKeyDown(event) {
    if (disabled) {
      return;
    }
    const mod = event.ctrlKey || event.metaKey;
    if (mod && (event.key === 'z' || event.key === 'Z')) {
      event.preventDefault();
      if (event.shiftKey) redo(); else undo();
      return;
    }
    if (mod && (event.key === 'y' || event.key === 'Y')) {
      event.preventDefault();
      redo();
      return;
    }
    if (!selected || !lines[selected.lineIndex]) {
      return;
    }
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      deleteLine(selected.lineIndex);
      return;
    }
    const step = event.shiftKey ? NUDGE_STEP_COARSE : NUDGE_STEP;
    const deltas = { ArrowUp: [0, -step], ArrowDown: [0, step], ArrowLeft: [-step, 0], ArrowRight: [step, 0] };
    const d = deltas[event.key];
    if (!d) {
      return;
    }
    event.preventDefault();
    pushHistory();
    commit(lines.map((line, i) => {
      if (i !== selected.lineIndex) {
        return line;
      }
      const nextPoints = [...line.points];
      nextPoints[selected.pointIndex] = clampPoint([line.points[selected.pointIndex][0] + d[0], line.points[selected.pointIndex][1] + d[1]]);
      return { ...line, points: nextPoints };
    }));
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>{detectionType === 'multi_line_crossing' ? t('prev.crossingSequence') : t('prev.crossingLine')}</h3>
        <span className="status-pill">{t('prev.lines', { n: lines.length, max: maxLines })}</span>
      </header>
      <p className="zone-drawer-hint">{t('prev.lineHint')}</p>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <DetectionFrameBackdrop cameraId={camera.id} paused={dragging !== null || bodyDragging} refreshSignal={frameRefresh} />
            <div
              ref={overlayRef}
              className={`zone-overlay tool-${tool}`}
              role="button"
              tabIndex={0}
              aria-label={t('prev.drawLines')}
              onKeyDown={onKeyDown}
              onPointerDown={(event) => {
                if (event.button !== 0 || tool !== 'add' || lines.length >= maxLines) {
                  return;
                }
                overlayRef.current?.setPointerCapture?.(event.pointerId);
                addLine(pointFromEvent(event));
              }}
              onPointerMove={movePoint}
              onPointerUp={stopDrag}
              onPointerCancel={stopDrag}
            >
              <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                {lines.map((line, lineIndex) => {
                  const [first, second] = line.points;
                  return (
                    <g key={line.id || lineIndex}>
                      {/* Invisible fat hit-line: in select mode it grabs the whole line to
                          drag it; in other modes it lets clicks pass through to the overlay. */}
                      <line
                        x1={first[0] * 100}
                        y1={first[1] * 100}
                        x2={second[0] * 100}
                        y2={second[1] * 100}
                        className="crossing-hit"
                        style={{ pointerEvents: tool === 'select' ? 'stroke' : 'none' }}
                        vectorEffect="non-scaling-stroke"
                        onPointerDown={(event) => {
                          if (disabled || event.button !== 0 || tool !== 'select') {
                            return;
                          }
                          event.stopPropagation();
                          overlayRef.current?.setPointerCapture?.(event.pointerId);
                          bodyDragRef.current = { lineIndex, last: rawPointFromEvent(event) };
                          setBodyDragging(true);
                          setSelected({ lineIndex, pointIndex: 0 });
                        }}
                      />
                      <line
                        x1={first[0] * 100}
                        y1={first[1] * 100}
                        x2={second[0] * 100}
                        y2={second[1] * 100}
                        className="crossing-line"
                        vectorEffect="non-scaling-stroke"
                      />
                      <text x={(first[0] * 100 + second[0] * 100) / 2} y={(first[1] * 100 + second[1] * 100) / 2 - 2} className="crossing-label">
                        {lineIndex + 1}
                      </text>
                      {crossingDirectionArrows(first, second, direction)}
                      {line.points.map((point, pointIndex) => (
                        <circle
                          key={`${lineIndex}-${pointIndex}`}
                          cx={point[0] * 100}
                          cy={point[1] * 100}
                          r="2.3"
                          className={`zone-point${tool === 'delete' ? ' deletable' : ''}${selected && selected.lineIndex === lineIndex && selected.pointIndex === pointIndex ? ' selected' : ''}`}
                          vectorEffect="non-scaling-stroke"
                          onPointerDown={(event) => {
                            if (disabled || event.button !== 0) {
                              return;
                            }
                            event.stopPropagation();
                            if (tool === 'delete') {
                              deleteLine(lineIndex);
                              return;
                            }
                            setSelected({ lineIndex, pointIndex });
                            overlayRef.current?.setPointerCapture?.(event.pointerId);
                            setDragging({ lineIndex, pointIndex });
                          }}
                        />
                      ))}
                    </g>
                  );
                })}
              </svg>
            </div>
            <DrawToolbox
              ariaLabel={t('prev.tools')}
              moveLabel={t('prev.moveToolbox')}
              groups={[
                [
                  { key: 'select', icon: 'cursor', label: t('prev.toolSelect'), toggle: true, active: tool === 'select', disabled, onClick: () => setTool('select') },
                  { key: 'add', icon: 'plus', label: t('prev.toolAddLine'), toggle: true, active: tool === 'add', disabled: disabled || lines.length >= maxLines, onClick: () => setTool('add') },
                  { key: 'delete', icon: 'eraser', label: t('prev.toolDeleteLine'), toggle: true, active: tool === 'delete', disabled: disabled || !lines.length, onClick: () => setTool('delete') },
                ],
                [
                  { key: 'undo', icon: 'undo', label: t('prev.undo'), disabled: disabled || !history.length, onClick: undo },
                  { key: 'redo', icon: 'redo', label: t('prev.redo'), disabled: disabled || !redoStack.length, onClick: redo },
                ],
                [
                  { key: 'direction', icon: 'two-way', label: t('prev.directionCycle', { dir: t(`prev.dir_${direction}`) }), disabled: disabled || !lines.length, onClick: cycleDirection },
                  { key: 'extend', icon: 'maximize', label: t('prev.extendLine'), disabled: disabled || targetLineIndex < 0, onClick: extendTargetLine },
                  { key: 'rotate', icon: 'rotate-cw', label: t('prev.rotate'), disabled: disabled || targetLineIndex < 0, onClick: rotateTargetLine },
                  { key: 'clear', icon: 'trash', label: t('prev.clearLines'), disabled: disabled || !lines.length, onClick: () => { pushHistory(); commit([]); setSelected(null); } },
                ],
                [
                  { key: 'snap', icon: 'grid4', label: t('prev.snapGrid'), toggle: true, active: snap, disabled, onClick: () => setSnap((s) => !s) },
                  { key: 'refresh', icon: 'refresh', label: t('prev.refreshFrame'), disabled, onClick: () => setFrameRefresh((n) => n + 1) },
                ],
              ]}
            />
          </>
        ) : (
          <div className="zone-empty-state">{t('prev.selectCamera')}</div>
        )}
      </div>
    </section>
  );
}

