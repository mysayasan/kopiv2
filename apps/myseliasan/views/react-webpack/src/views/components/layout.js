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

// TopBar is the control-plane header: brand, primary tabs, and the theme / refresh
// / lock actions — the same shell pattern mymatasan uses.
export function TopBar({ activeTab, busy, onTab, onRefresh, onLogout, theme, onThemeChange, session }) {
  const tabs = [
    // Dashboard is the always-available landing tab. The Mymatasan tab follows the
    // same permission matrix as its APIs — a role needs GET on /api/nodes to see it.
    { id: 'dashboard', label: 'Dashboard', icon: 'monitor' },
    ...(sessionCanGet(session, '/api/nodes') ? [{ id: 'nodes', label: 'Mymatasan', icon: 'shield' }] : []),
    // Users & Roles is the myseliasan RBAC admin surface — superadmin only.
    ...(session?.isSuperadmin ? [{ id: 'access', label: 'Users & Roles', icon: 'user' }] : []),
  ];
  return (
    <header className="topbar">
      <BrandLogo />
      <nav className="primary-tabs" aria-label="Main">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`primary-tab${tab.id === activeTab ? ' active' : ''}`}
            onClick={() => onTab(tab.id)}
          >
            <span className="btn-icon"><Ico n={tab.icon} /> {tab.label}</span>
          </button>
        ))}
      </nav>
      <div className="topbar-actions">
        {session?.email ? <span className="session-pill">{session.email}</span> : null}
        <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
        <button type="button" className="quiet" onClick={onRefresh} disabled={busy}>
          <span className="btn-icon"><Ico n="refresh" /> Refresh</span>
        </button>
        <button type="button" className="quiet danger-text" onClick={onLogout} disabled={busy}>
          <span className="btn-icon"><Ico n="lock" /> Log out</span>
        </button>
      </div>
    </header>
  );
}
