import { useEffect, useRef, useState } from 'react';
import { Ico } from './icons';

// Light/dark theme set, mirroring mymatasan's theming pattern but trimmed to the
// two myseliasan ships with.
export const THEMES = ['light', 'dark'];
export const THEME_LABELS = { light: 'Light', dark: 'Dark' };
export const THEME_ICONS = { light: 'sun', dark: 'moon' };

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

export function FormBusyOverlay({ busy }) {
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label="Loading">
      <div className="form-busy-spinner" />
    </div>
  );
}

export function Message({ value, floating = false }) {
  if (!value) return null;
  return <div className={`status-line${floating ? ' status-line--floating' : ''}`}>{value}</div>;
}

// Toast is a single auto-dismissing notification (newest on top in the stack).
function Toast({ id, text, duration = 3000, onDismiss }) {
  const [leaving, setLeaving] = useState(false);
  const timerRef = useRef(null);
  const start = () => {
    clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => setLeaving(true), duration);
  };
  useEffect(() => {
    start();
    return () => clearTimeout(timerRef.current);
    // eslint-disable-next-line
  }, []);
  const handleEnd = () => { if (leaving) onDismiss(id); };
  return (
    <div
      className={`toast${leaving ? ' toast--leaving' : ''}`}
      role="status"
      aria-live="polite"
      onMouseEnter={() => clearTimeout(timerRef.current)}
      onMouseLeave={start}
      onTransitionEnd={handleEnd}
    >
      <span className="toast-text">{text}</span>
      <button type="button" className="toast-close" aria-label="Dismiss" onClick={() => setLeaving(true)}>
        <Ico n="x" sz={13} />
      </button>
    </div>
  );
}

export function ToastStack({ toasts, onDismiss }) {
  if (!toasts || toasts.length === 0) return null;
  return (
    <div className="toast-stack" aria-live="polite">
      {toasts.map((t) => (
        <Toast key={t.id} id={t.id} text={t.text} onDismiss={onDismiss} />
      ))}
    </div>
  );
}
