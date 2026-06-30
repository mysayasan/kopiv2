import { useState, useEffect, useRef, useMemo } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { maxCrossingLines } from '../lib/constants';
import {fallbackLiveSource,normalizeStreamConfig,roundedPoint,zonePolygonText,parseZonePolygon,normalizeLineConfig,waitForIceGathering,createWebRTCAnswer,shouldUseMJPEGForTracks,apiBase } from '../lib/helpers';

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
function DetectionFrameBackdrop({ cameraId, paused }) {
  const t = useT();
  const [src, setSrc] = useState('');
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

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
  }, [cameraId]);

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

export function LiveViewport({ deviceId, title, authHeader, streamConfig, rtspTracks, healthStatus, streamKey, startDelayMs = 0 }) {
  const t = useT();
  const videoRef = useRef(null);
  const audioRef = useRef(null);
  const [state, setState] = useState(() => '');
  const [fallbackSrc, setFallbackSrc] = useState('');
  const [hasAudio, setHasAudio] = useState(false);
  const [muted, setMuted] = useState(true);
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
        const answer = await createWebRTCAnswer(deviceId, pc.localDescription, authHeader);
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
  }, [deviceId, authHeader, streamConfig, rtspTracks, healthStatus, streamKey, startDelayMs]);

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

export function ZoneDrawingPreview({ camera, polygonValue, onPolygon, authHeader, streamConfig, disabled }) {
  const t = useT();
  const overlayRef = useRef(null);
  const [draggingIndex, setDraggingIndex] = useState(null);
  const points = useMemo(() => parseZonePolygon(polygonValue), [polygonValue]);
  const polygonPoints = points.map((point) => `${point[0] * 100},${point[1] * 100}`).join(' ');

  // Undo history: each user action (add/move/clear/full-frame) snapshots the
  // pre-action polygon so Undo steps back through them — not just the last point.
  // lastValueRef tracks the value WE committed, so an external change (switching
  // rule/camera, parent edit) is detected and clears the now-stale history.
  const [history, setHistory] = useState([]);
  const lastValueRef = useRef(polygonValue);
  const draggedRef = useRef(false);
  useEffect(() => {
    if (polygonValue !== lastValueRef.current) {
      lastValueRef.current = polygonValue;
      setHistory([]);
    }
  }, [polygonValue]);

  function commit(nextPoints) {
    const text = zonePolygonText(nextPoints);
    lastValueRef.current = text;
    onPolygon(text);
  }

  // pushHistory snapshots the current points before a mutating action.
  function pushHistory() {
    setHistory((h) => [...h, points]);
  }

  function undo() {
    if (!history.length) {
      return;
    }
    const prev = history[history.length - 1];
    setHistory(history.slice(0, -1));
    commit(prev);
  }

  function pointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return roundedPoint([(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height]);
  }

  function addPoint(event) {
    if (disabled || !camera) {
      return;
    }
    pushHistory();
    commit(insertPointOnNearestEdge(points, pointFromEvent(event)));
  }

  function movePoint(event) {
    if (disabled || draggingIndex === null) {
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
    if (draggingIndex !== null && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDraggingIndex(null);
    draggedRef.current = false;
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>{t('prev.detectionZone')}</h3>
        <span className="status-pill">{t('prev.points', { n: points.length })}</span>
      </header>
      <p className="zone-drawer-hint">{t('prev.zoneHint')}</p>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <DetectionFrameBackdrop cameraId={camera.id} paused={draggingIndex !== null} />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label={t('prev.drawZone')}
              onPointerDown={(event) => {
                if (event.button !== 0) {
                  return;
                }
                overlayRef.current?.setPointerCapture?.(event.pointerId);
                addPoint(event);
              }}
              onPointerMove={movePoint}
              onPointerUp={stopDrag}
              onPointerCancel={stopDrag}
            >
              <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                {points.length >= 3 ? <polygon points={polygonPoints} className="zone-shape" /> : null}
                {points.length >= 2 ? <polyline points={polygonPoints} className="zone-line" /> : null}
                {points.map((point, index) => (
                  <circle
                    key={`${point[0]}-${point[1]}-${index}`}
                    cx={point[0] * 100}
                    cy={point[1] * 100}
                    r="2.3"
                    className="zone-point"
                    vectorEffect="non-scaling-stroke"
                    onPointerDown={(event) => {
                      if (disabled || event.button !== 0) {
                        return;
                      }
                      event.stopPropagation();
                      overlayRef.current?.setPointerCapture?.(event.pointerId);
                      setDraggingIndex(index);
                    }}
                  />
                ))}
              </svg>
            </div>
          </>
        ) : (
          <div className="zone-empty-state">{t('prev.selectCamera')}</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={undo} disabled={disabled || !history.length}>
          <span className="btn-icon"><Ico n="undo" /> {t('prev.undo')}</span>
        </button>
        <button type="button" className="quiet" onClick={() => { pushHistory(); commit([]); }} disabled={disabled || !points.length}>
          <span className="btn-icon"><Ico n="trash" /> {t('prev.clearZone')}</span>
        </button>
        <button
          type="button"
          className="quiet"
          onClick={() => {
            pushHistory();
            commit([
              [0, 0],
              [1, 0],
              [1, 1],
              [0, 1],
            ]);
          }}
          disabled={disabled}
        >
          <span className="btn-icon"><Ico n="video" /> {t('prev.fullFrame')}</span>
        </button>
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
  const maxLines = detectionType === 'multi_line_crossing' ? maxCrossingLines : 1;
  const normalizedLine = normalizeLineConfig(config, detectionType);
  const lines = normalizedLine.lines.slice(0, maxLines);
  const direction = normalizedLine.direction || 'both';

  // Undo history mirrors the zone drawer: each action (add/move/clear) snapshots
  // the pre-action lines so Undo steps back through them. The parent re-parses and
  // re-normalizes the config on every commit (so the round-tripped value won't byte-
  // match what we sent), so external changes are detected with a self-commit flag
  // rather than value comparison: after our own commit the next render is accepted,
  // any other change clears the now-stale history.
  const [history, setHistory] = useState([]);
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
    }
  }, [linesKey]);

  function commit(nextLines) {
    selfCommitRef.current = true;
    onConfig({ lines: nextLines.slice(0, maxLines) });
  }

  // pushHistory snapshots the current lines before a mutating action.
  function pushHistory() {
    setHistory((h) => [...h, lines]);
  }

  function undo() {
    if (!history.length) {
      return;
    }
    const prev = history[history.length - 1];
    setHistory(history.slice(0, -1));
    commit(prev);
  }

  function pointFromEvent(event) {
    const rect = overlayRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) {
      return [0, 0];
    }
    return roundedPoint([(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height]);
  }

  function addLine(point = null) {
    if (disabled || !camera || lines.length >= maxLines) {
      return;
    }
    const start = point || [0.5, 0.25 + lines.length * 0.12];
    const end = roundedPoint([start[0], start[1] + 0.25]);
    pushHistory();
    commit([...lines, { id: `line-${lines.length + 1}`, points: [roundedPoint(start), end] }]);
  }

  function movePoint(event) {
    if (disabled || !dragging) {
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
      nextPoints[dragging.pointIndex] = pointFromEvent(event);
      return { ...line, points: nextPoints };
    });
    commit(nextLines);
  }

  function stopDrag(event) {
    if (dragging && overlayRef.current?.hasPointerCapture?.(event.pointerId)) {
      overlayRef.current.releasePointerCapture(event.pointerId);
    }
    setDragging(null);
    draggedRef.current = false;
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
            <DetectionFrameBackdrop cameraId={camera.id} paused={dragging !== null} />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label={t('prev.drawLines')}
              onPointerDown={(event) => {
                if (event.button !== 0 || lines.length >= maxLines) {
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
                          className="zone-point"
                          vectorEffect="non-scaling-stroke"
                          onPointerDown={(event) => {
                            if (disabled || event.button !== 0) {
                              return;
                            }
                            event.stopPropagation();
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
          </>
        ) : (
          <div className="zone-empty-state">{t('prev.selectCamera')}</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={() => addLine()} disabled={disabled || lines.length >= maxLines}>
          <span className="btn-icon"><Ico n="plus" /> {t('prev.addLine')}</span>
        </button>
        <button type="button" className="quiet" onClick={undo} disabled={disabled || !history.length}>
          <span className="btn-icon"><Ico n="undo" /> {t('prev.undo')}</span>
        </button>
        <button type="button" className="quiet" onClick={() => { pushHistory(); commit([]); }} disabled={disabled || !lines.length}>
          <span className="btn-icon"><Ico n="trash" /> {t('prev.clearLines')}</span>
        </button>
      </div>
    </section>
  );
}

