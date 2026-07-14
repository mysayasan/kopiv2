import { useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';

// Theme set, standardized with the rest of the suite. THEME_LABELS are English
// fallbacks; the dropdown renders localized labels via t(`theme.${code}`).
export const THEMES = ['light', 'dark', 'contrast'];
export const THEME_LABELS = { light: 'Light', dark: 'Dark', contrast: 'High contrast' };
export const THEME_ICONS = { light: 'sun', dark: 'moon', contrast: 'contrast' };

export function ThemeDropdown({ theme, onThemeChange }) {
  const tr = useT();
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
          {tr('theme.label')}
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="theme-menu" role="listbox" aria-label={tr('theme.select')}>
          {THEMES.map((code) => (
            <button
              key={code}
              type="button"
              role="option"
              aria-selected={code === theme}
              className={`theme-menu-item${code === theme ? ' active' : ''}`}
              onClick={() => { onThemeChange(code); setOpen(false); }}
            >
              <Ico n={THEME_ICONS[code]} sz={14} /> {tr(`theme.${code}`)}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function FormBusyOverlay({ busy }) {
  const tr = useT();
  if (!busy) return null;
  return (
    <div className="form-busy-overlay" aria-live="polite" aria-label={tr('ui.loading')}>
      <div className="form-busy-spinner" />
    </div>
  );
}

export function Message({ value, floating = false }) {
  if (!value) return null;
  return <div className={`status-line${floating ? ' status-line--floating' : ''}`}>{value}</div>;
}

// Toast / ToastStack live in the shared module (@shared) so every app shares one
// notification design — import { ToastStack } from '@shared'.
