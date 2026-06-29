import { Ico } from './icons';
import { ThemeDropdown } from './ui';
import { sessionCanGet } from '../lib/helpers';

// BrandLogo mirrors the mymatasan mark (line-art shield + eye + check) with the
// rounded lowercase wordmark, so the control plane shares the product's identity.
export function BrandLogo({ size = 40, className = '' }) {
  return (
    <div className={`brand-logo${className ? ` ${className}` : ''}`} aria-label="myseliasan">
      <svg className="brand-mark" viewBox="0 0 64 64" width={size} height={size} role="img" aria-hidden="true">
        <g fill="none" stroke="#4e9d6e" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round">
          <path d="M19 13.5 H45 Q50 13.5 50 18 V33 Q50 45 32 54 Q14 45 14 33 V18 Q14 13.5 19 13.5 Z" />
          <path d="M20 30 Q31 21 42 30 Q31 39 20 30 Z" />
          <circle cx="31" cy="30" r="6" />
          <path d="M24 41 L31 48 L46 30" strokeWidth="3.4" />
        </g>
        <circle cx="31" cy="30" r="3" fill="#4e9d6e" />
        <circle cx="29.5" cy="28.6" r="1" fill="#eaf6ef" />
      </svg>
      <span className="brand-wordmark">myseliasan</span>
    </div>
  );
}

// SideNav is the standardized RBAC-app navigation rail: the myidsan dark side-nav
// (grouped menu, code tiles, tone accents) reused as the shared shell. Menu entries
// follow the same permission matrix that gates their APIs — a role needs GET on
// /api/nodes to see Nodes; Users & Roles is superadmin-only.
export function SideNav({ activeTab, busy, onTab, onLogout, theme, onThemeChange, session }) {
  const groups = [
    {
      label: 'Workspace',
      items: [{ id: 'dashboard', label: 'Dashboard', icon: 'monitor', tone: 'steel' }],
    },
    {
      label: 'Fleet',
      items: [
        ...(sessionCanGet(session, '/api/nodes')
          ? [{ id: 'nodes', label: 'Nodes', icon: 'shield', tone: 'blue' }]
          : []),
      ],
    },
    {
      label: 'Administration',
      items: session?.isSuperadmin
        ? [
            { id: 'users', label: 'Users', icon: 'user', tone: 'blue' },
            { id: 'roles', label: 'Roles', icon: 'key', tone: 'violet' },
            { id: 'rbac', label: 'RBAC', icon: 'lock', tone: 'green' },
          ]
        : [],
    },
  ].filter((group) => group.items.length > 0);

  return (
    <aside className="side-nav">
      <div className="side-brand">
        <BrandLogo />
        <div className="side-brand-sub">Control plane</div>
      </div>
      <nav aria-label="Main">
        {groups.map((group) => (
          <div className="nav-group" key={group.label}>
            <div className="nav-group-label">{group.label}</div>
            {group.items.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`nav-item tone-${item.tone}${item.id === activeTab ? ' active' : ''}`}
                onClick={() => onTab(item.id)}
              >
                <span className="nav-ico"><Ico n={item.icon} sz={17} /></span>
                <span className="nav-label">{item.label}</span>
              </button>
            ))}
          </div>
        ))}
      </nav>
      <div className="side-nav-foot">
        {session?.email ? <div className="side-brand-sub" title={session.email}>{session.email}</div> : null}
        <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
        <button type="button" className="logout-button" onClick={onLogout} disabled={busy}>
          Log out
        </button>
      </div>
    </aside>
  );
}
