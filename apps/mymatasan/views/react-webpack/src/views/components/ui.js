import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';
import { THEMES, THEME_LABELS, THEME_ICONS, liveViewLayouts } from '../lib/constants';
import { parseTracks } from '../lib/helpers';

export function ThemeDropdown({ theme, onThemeChange }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  return (
    <div className="theme-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet theme-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={THEME_ICONS[theme]} sz={13} />
          Theme
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="theme-menu" role="listbox" aria-label="Select theme">
          {THEMES.map((t) => (
            <button
              key={t}
              type="button"
              role="option"
              aria-selected={t === theme}
              className={`theme-menu-item${t === theme ? ' active' : ''}`}
              onClick={() => { onThemeChange(t); setOpen(false); }}
            >
              <Ico n={THEME_ICONS[t]} sz={14} /> {THEME_LABELS[t]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function LayoutDropdown({ layout, onLayout }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  const current = liveViewLayouts.find((o) => o.id === layout) || liveViewLayouts[0];
  const iconFor = (cols) => (cols <= 2 ? 'grid2' : 'grid4');
  return (
    <div className="layout-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet layout-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={iconFor(current.cols)} sz={13} />
          {current.label}
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="layout-menu" role="listbox" aria-label="Select grid layout">
          {liveViewLayouts.map((o) => (
            <button
              key={o.id}
              type="button"
              role="option"
              aria-selected={o.id === layout}
              className={`layout-menu-item${o.id === layout ? ' active' : ''}`}
              onClick={() => { onLayout(o.id); setOpen(false); }}
            >
              <Ico n={iconFor(o.cols)} sz={14} /> {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function FormBusyOverlay({ busy }) {
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label="Loading">
      <div className="form-busy-spinner" />
    </div>
  );
}

export function InfoButton({ text }) {
  return (
    <button type="button" className="info-button" title={text} aria-label={text}>
      i
    </button>
  );
}

export function FieldTitle({ children, info }) {
  return (
    <span className="label-row">
      <span>{children}</span>
      <InfoButton text={info} />
    </span>
  );
}

export function Tracks({ value }) {
  const tracks = parseTracks(value);
  if (!tracks.length) {
    return <span>-</span>;
  }
  return (
    <ul className="track-list">
      {tracks.map((track, idx) => (
        <li key={`${track.codec || 'track'}-${idx}`}>
          {track.mediaType || 'media'} / {track.codec || 'codec'} {track.clockRate ? `@ ${track.clockRate}` : ''}
        </li>
      ))}
    </ul>
  );
}

export function Message({ value }) {
  if (!value) {
    return null;
  }
  return <div className="status-line">{value}</div>;
}

