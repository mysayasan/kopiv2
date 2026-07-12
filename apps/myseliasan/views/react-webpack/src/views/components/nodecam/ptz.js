import { useEffect, useRef } from 'react';
import { useT } from '@shared/i18n';

// PTZRing — mymatasan's pan/tilt controller (copied verbatim so it looks and behaves the
// same on the node's Live View embedded in myseliasan). Press-and-hold pans continuously;
// release stops. A safety timer auto-stops if a release is ever missed. onMove(dir)/onStop
// are wired by the caller to the node's PTZ API over the control tunnel.
export function PTZRing({ busy, size, onMove, onStop }) {
  const t = useT();
  const sz = size || 140;
  const ro = 94;
  const ri = 35;
  const d = ro / Math.SQRT2;
  const di = ri / Math.SQRT2;
  const cx = 100, cy = 100;

  const UP = `M ${cx - di} ${cy - di} A ${ri} ${ri} 0 0 1 ${cx + di} ${cy - di} L ${cx + d} ${cy - d} A ${ro} ${ro} 0 0 0 ${cx - d} ${cy - d} Z`;
  const RIGHT = `M ${cx + di} ${cy - di} A ${ri} ${ri} 0 0 1 ${cx + di} ${cy + di} L ${cx + d} ${cy + d} A ${ro} ${ro} 0 0 0 ${cx + d} ${cy - d} Z`;
  const DOWN = `M ${cx + di} ${cy + di} A ${ri} ${ri} 0 0 1 ${cx - di} ${cy + di} L ${cx - d} ${cy + d} A ${ro} ${ro} 0 0 0 ${cx + d} ${cy + d} Z`;
  const LEFT = `M ${cx - di} ${cy + di} A ${ri} ${ri} 0 0 1 ${cx - di} ${cy - di} L ${cx - d} ${cy - d} A ${ro} ${ro} 0 0 0 ${cx - d} ${cy + d} Z`;

  const A_UP = 'M 100 24 L 112 40 L 106 40 L 106 48 L 94 48 L 94 40 L 88 40 Z';
  const A_RIGHT = 'M 176 100 L 160 88 L 160 94 L 152 94 L 152 106 L 160 106 L 160 112 Z';
  const A_DOWN = 'M 100 176 L 88 160 L 94 160 L 94 152 L 106 152 L 106 160 L 112 160 Z';
  const A_LEFT = 'M 24 100 L 40 112 L 40 106 L 48 106 L 48 94 L 40 94 L 40 88 Z';

  const cls = `ptz-sector${busy ? ' ptz-sector-busy' : ''}`;
  const holdingRef = useRef(false);
  const safetyRef = useRef(null);

  function startHold(dir) {
    if (busy || holdingRef.current) return;
    holdingRef.current = true;
    onMove(dir);
    if (safetyRef.current) clearTimeout(safetyRef.current);
    safetyRef.current = setTimeout(endHold, 20000);
  }

  function endHold() {
    if (safetyRef.current) { clearTimeout(safetyRef.current); safetyRef.current = null; }
    if (!holdingRef.current) return;
    holdingRef.current = false;
    onStop();
  }

  useEffect(() => {
    const onBlur = () => endHold();
    window.addEventListener('blur', onBlur);
    return () => { window.removeEventListener('blur', onBlur); endHold(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function sector(dPath, label, dir) {
    return (
      <path
        key={dir}
        d={dPath}
        className={cls}
        role="button"
        aria-label={label}
        tabIndex={busy ? -1 : 0}
        onPointerDown={(e) => { if (busy) return; e.preventDefault(); e.currentTarget.setPointerCapture?.(e.pointerId); startHold(dir); }}
        onPointerUp={(e) => { e.currentTarget.releasePointerCapture?.(e.pointerId); endHold(); }}
        onPointerCancel={endHold}
        onLostPointerCapture={endHold}
        onKeyDown={(e) => { if (!busy && (e.key === 'Enter' || e.key === ' ') && !e.repeat) { e.preventDefault(); startHold(dir); } }}
        onKeyUp={(e) => { if (e.key === 'Enter' || e.key === ' ') endHold(); }}
      />
    );
  }

  return (
    <svg viewBox="0 0 200 200" width={sz} height={sz} className={`ptz-ring${busy ? ' ptz-ring-busy' : ''}`} aria-label={t('cam.ptzControls')}>
      {sector(UP, t('cam.ptzUp'), 'up')}
      {sector(RIGHT, t('cam.ptzRight'), 'right')}
      {sector(DOWN, t('cam.ptzDown'), 'down')}
      {sector(LEFT, t('cam.ptzLeft'), 'left')}
      <circle cx={cx} cy={cy} r={ri} className={cls} role="button" aria-label={t('cam.ptzStop')} tabIndex={busy ? -1 : 0} onClick={busy ? undefined : onStop} onKeyDown={(e) => !busy && e.key === 'Enter' && onStop()} />
      <g pointerEvents="none" strokeLinecap="round" strokeLinejoin="round">
        <circle cx={cx} cy={cy} r={ro} fill="none" stroke="currentColor" strokeWidth="1.5" />
        <circle cx={cx} cy={cy} r={ri} fill="none" stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx - di} y1={cy - di} x2={cx - d} y2={cy - d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx + di} y1={cy - di} x2={cx + d} y2={cy - d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx + di} y1={cy + di} x2={cx + d} y2={cy + d} stroke="currentColor" strokeWidth="1.5" />
        <line x1={cx - di} y1={cy + di} x2={cx - d} y2={cy + d} stroke="currentColor" strokeWidth="1.5" />
        <path d={A_UP} fill="currentColor" />
        <path d={A_RIGHT} fill="currentColor" />
        <path d={A_DOWN} fill="currentColor" />
        <path d={A_LEFT} fill="currentColor" />
        <rect x="89" y="89" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.5" />
      </g>
    </svg>
  );
}

// onvifLocation extracts a human location from an ONVIF scopes string (…/location/…).
export function onvifLocation(scopes) {
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
