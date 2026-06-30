import { Fragment } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';

// SideNav is the shared dark navigation rail for the RBAC apps (modelled on
// myseliasan's). The shell — the <aside>, the grouped .nav-item buttons, and the foot —
// is shared; each app supplies its own `brand` and `footer` (logo, theme toggle, logout,
// session line) as slots, plus its own nav `groups`.
//
//   groups: [{ label, items: [
//     { id, label, icon, tone, active, onClick }   // a standard nav button, OR
//     { id, render }                               // a custom item (e.g. a tree)
//   ]}]
//
// Empty groups (and falsy items) are dropped. The `render` escape hatch lets an app
// inject a bespoke item — e.g. myseliasan's Nodes tree — without the shell knowing about
// it; an app that never passes such an item simply never renders one.
export function SideNav({ brand, groups, footer, navLabel }) {
  const t = useT();
  const shown = (groups || []).filter((g) => g && Array.isArray(g.items) && g.items.some(Boolean));
  return (
    <aside className="side-nav">
      {brand}
      <nav aria-label={navLabel != null ? navLabel : t('nav.main')}>
        {shown.map((group) => (
          <div className="nav-group" key={group.label}>
            {group.label ? <div className="nav-group-label">{group.label}</div> : null}
            {group.items.filter(Boolean).map((item) => (
              item.render ? (
                <Fragment key={item.id}>{item.render()}</Fragment>
              ) : (
                <button
                  key={item.id}
                  type="button"
                  className={`nav-item tone-${item.tone || 'steel'}${item.active ? ' active' : ''}`}
                  onClick={item.onClick}
                >
                  <span className="nav-ico"><Ico n={item.icon || 'list'} sz={17} /></span>
                  <span className="nav-label">{item.label}</span>
                </button>
              )
            ))}
          </div>
        ))}
      </nav>
      {footer ? <div className="side-nav-foot">{footer}</div> : null}
    </aside>
  );
}
