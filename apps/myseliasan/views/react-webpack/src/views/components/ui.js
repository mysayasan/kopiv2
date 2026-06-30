import { useEffect, useRef, useState } from 'react';
import { Ico } from '@shared';

// Light/dark theme set, mirroring mymatasan's theming pattern but trimmed to the
// two myseliasan ships with.
export const THEMES = ['light', 'dark', 'contrast'];
export const THEME_LABELS = { light: 'Light', dark: 'Dark', contrast: 'High contrast' };
export const THEME_ICONS = { light: 'sun', dark: 'moon', contrast: 'contrast' };

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

// IconDropdown is a compact glyph selector: a button showing the current icon, which
// opens a small grid of the pre-installed options. Used at node adoption so the icon
// choice is unambiguous (one control, one selection) rather than a row of buttons.
export function IconDropdown({ value, options, onChange, disabled }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);
  useEffect(() => {
    if (!open) return undefined;
    function onDown(e) {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);
  const current = value || (options && options[0]);
  return (
    <div className="icon-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`icon-drop-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
      >
        <span className="icon-drop-current"><Ico n={current} sz={16} /></span>
        <span className="icon-drop-name">{current}</span>
        <Ico n="chev-down" sz={12} />
      </button>
      {open && (
        <div className="icon-drop-menu" role="listbox" aria-label="Select icon">
          {options.map((name) => (
            <button
              key={name}
              type="button"
              role="option"
              aria-selected={name === current}
              className={`icon-drop-item${name === current ? ' active' : ''}`}
              onClick={() => { onChange(name); setOpen(false); }}
              title={name}
            >
              <Ico n={name} sz={18} />
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

// Toast / ToastStack now live in the shared module (@shared) so both control planes
// share one notification design — import { ToastStack } from '@shared'.
