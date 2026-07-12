import { Ico } from './icons';
import './styles/tabs.css';

// Tabs is THE shared, standardized tab bar for every app (mymatasan / myseliasan /
// myidsan). It renders mymatasan's canonical tab look — icon + label, muted text that
// goes accent-coloured with a 3px accent underline when active — with ONE fixed
// bar→content gap (16px) and a uniform button height, so tab pages read identically
// across the suite instead of each surface hand-rolling its own spacing.
//
// Props:
//   tabs       array of { id, label, icon?, disabled?, title? }
//   active     id of the currently-selected tab
//   onChange   (id) => void, called when a non-disabled tab is clicked
//   ariaLabel  accessible name for the tablist
//   className  extra class(es) on the <nav> — use ONLY for per-container horizontal
//              alignment (e.g. dialog padding); never to change the vertical spacing,
//              which is owned here so it stays standard.
export function Tabs({ tabs, active, onChange, ariaLabel, className }) {
  return (
    <nav className={`ui-tabs${className ? ` ${className}` : ''}`} role="tablist" aria-label={ariaLabel}>
      {(tabs || []).map((tb) => {
        const isActive = tb.id === active;
        return (
          <button
            key={tb.id}
            type="button"
            role="tab"
            aria-selected={isActive}
            className={`ui-tab${isActive ? ' active' : ''}`}
            disabled={tb.disabled}
            title={tb.title}
            onClick={() => { if (!tb.disabled && onChange) onChange(tb.id); }}
          >
            {tb.icon ? <Ico n={tb.icon} sz={16} /> : null}
            <span className="ui-tab-label">{tb.label}</span>
          </button>
        );
      })}
    </nav>
  );
}
