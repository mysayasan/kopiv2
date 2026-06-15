import { useState, useEffect, useRef, useMemo } from 'react';
import { Ico } from './icons';
import { maxCrossingLines } from '../lib/constants';
import {cameraTitle,fallbackLiveSource,normalizeStreamConfig,roundedPoint,zonePolygonText,parseZonePolygon,normalizeLineConfig,waitForIceGathering,createWebRTCAnswer,shouldUseMJPEGForTracks } from '../lib/helpers';

export function LiveViewport({ deviceId, title, authHeader, streamConfig, rtspTracks, healthStatus, streamKey, startDelayMs = 0 }) {
  const videoRef = useRef(null);
  const audioRef = useRef(null);
  const [state, setState] = useState('Connecting');
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
      setState('Offline');
      return undefined;
    }
    const configValue = normalizeStreamConfig(streamConfig);
    const forceMJPEG = shouldUseMJPEGForTracks(rtspTracks);
    setFallbackSrc('');
    setHasAudio(false);
    setMuted(true);
    setState(forceMJPEG || !configValue.webrtc.enabled ? 'MJPEG' : 'Connecting');

    if (forceMJPEG) {
      if (configValue.mjpegFallback.enabled) {
        setState('MJPEG fallback');
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState('WebRTC needs H264');
      }
      return undefined;
    }

    if (!configValue.webrtc.enabled) {
      if (configValue.mjpegFallback.enabled) {
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState('Live view disabled');
      }
      return undefined;
    }
    if (typeof RTCPeerConnection === 'undefined') {
      if (configValue.mjpegFallback.enabled) {
        setState('MJPEG fallback');
        setFallbackSrc(fallbackLiveSource(deviceId));
      } else {
        setState('WebRTC unavailable');
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
            setState('Live');
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
            setState('Reconnecting…');
          } else if (cs === 'failed' || cs === 'closed') {
            if (configValue.mjpegFallback.enabled) {
              setState('MJPEG fallback');
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
            setState(err.message || 'WebRTC failed');
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
        <div className="live-offline" aria-label={`${title} offline`}>
          <Ico n="wifi" sz={28} />
          <span>Camera offline</span>
        </div>
      ) : fallbackSrc ? (
        <img src={fallbackSrc} alt={`${title} live view`} />
      ) : (
        <video ref={videoRef} autoPlay muted playsInline aria-label={`${title} live view`} />
      )}
      <audio ref={audioRef} playsInline style={{ display: 'none' }} />
      {!offline && <span className="stream-state">{state}</span>}
      {hasAudio && (
        <button
          type="button"
          className="audio-mute-btn"
          onClick={toggleMute}
          aria-label={muted ? 'Unmute audio' : 'Mute audio'}
          title={muted ? 'Unmute audio' : 'Mute audio'}
        >
          <Ico n={muted ? 'volume-x' : 'volume-2'} sz={14} />
        </button>
      )}
    </div>
  );
}

export function ZoneDrawingPreview({ camera, polygonValue, onPolygon, authHeader, streamConfig, disabled }) {
  const overlayRef = useRef(null);
  const [draggingIndex, setDraggingIndex] = useState(null);
  const points = useMemo(() => parseZonePolygon(polygonValue), [polygonValue]);
  const polygonPoints = points.map((point) => `${point[0] * 100},${point[1] * 100}`).join(' ');

  function commit(nextPoints) {
    onPolygon(zonePolygonText(nextPoints));
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
    commit([...points, pointFromEvent(event)]);
  }

  function movePoint(event) {
    if (disabled || draggingIndex === null) {
      return;
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
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>Detection Zone</h3>
        <span className="status-pill">{points.length} points</span>
      </header>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <LiveViewport
              key={`${camera.id}:${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
              deviceId={camera.id}
              title={cameraTitle(camera)}
              authHeader={authHeader}
              streamConfig={streamConfig}
              rtspTracks={camera.rtspTracks}
              streamKey={`${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
            />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label="Draw detection zone"
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
          <div className="zone-empty-state">Select camera</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={() => commit(points.slice(0, -1))} disabled={disabled || !points.length}>
          <span className="btn-icon"><Ico n="undo" /> Undo Point</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit([])} disabled={disabled}>
          <span className="btn-icon"><Ico n="trash" /> Clear Zone</span>
        </button>
        <button
          type="button"
          className="quiet"
          onClick={() =>
            commit([
              [0, 0],
              [1, 0],
              [1, 1],
              [0, 1],
            ])
          }
          disabled={disabled}
        >
          <span className="btn-icon"><Ico n="video" /> Full Frame</span>
        </button>
      </div>
    </section>
  );
}

export function LineDrawingPreview({ camera, config, detectionType, onConfig, authHeader, streamConfig, disabled }) {
  const overlayRef = useRef(null);
  const [dragging, setDragging] = useState(null);
  const maxLines = detectionType === 'multi_line_crossing' ? maxCrossingLines : 1;
  const lines = normalizeLineConfig(config, detectionType).lines.slice(0, maxLines);

  function commit(nextLines) {
    onConfig({ lines: nextLines.slice(0, maxLines) });
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
    commit([...lines, { id: `line-${lines.length + 1}`, points: [roundedPoint(start), end] }]);
  }

  function movePoint(event) {
    if (disabled || !dragging) {
      return;
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
  }

  return (
    <section className="zone-drawer">
      <header>
        <h3>{detectionType === 'multi_line_crossing' ? 'Crossing Sequence' : 'Crossing Line'}</h3>
        <span className="status-pill">{lines.length}/{maxLines} lines</span>
      </header>
      <div className={camera ? 'zone-live' : 'zone-live empty-zone'}>
        {camera ? (
          <>
            <LiveViewport
              key={`${camera.id}:${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
              deviceId={camera.id}
              title={cameraTitle(camera)}
              authHeader={authHeader}
              streamConfig={streamConfig}
              rtspTracks={camera.rtspTracks}
              streamKey={`${camera.rtspUrl || ''}:${camera.rtspTracks || ''}`}
            />
            <div
              ref={overlayRef}
              className="zone-overlay"
              role="button"
              tabIndex={0}
              aria-label="Draw crossing lines"
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
          <div className="zone-empty-state">Select camera</div>
        )}
      </div>
      <div className="action-row">
        <button type="button" className="quiet" onClick={() => addLine()} disabled={disabled || lines.length >= maxLines}>
          <span className="btn-icon"><Ico n="plus" /> Add Line</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit(lines.slice(0, -1))} disabled={disabled || !lines.length}>
          <span className="btn-icon"><Ico n="undo" /> Undo Line</span>
        </button>
        <button type="button" className="quiet" onClick={() => commit([])} disabled={disabled}>
          <span className="btn-icon"><Ico n="trash" /> Clear Lines</span>
        </button>
      </div>
    </section>
  );
}

