import { useMemo, useState, useRef, useEffect } from 'react';
import { Ico } from './icons';
import { ThemeDropdown, FormBusyOverlay, Message } from './ui';
import {formatTimestamp,parseMetadata,cameraTitle } from '../lib/helpers';

// BrandLogo is the MyMataSan mark: a line-art shield holding a watchful eye and a
// checkmark, with the rounded lowercase wordmark underneath. Self-contained inline
// SVG so it scales without a binary asset.
export function BrandLogo({ size = 40 }) {
  return (
    <div className="brand-logo" aria-label="mymatasan">
      <svg className="brand-mark" viewBox="0 0 64 64" width={size} height={size} role="img" aria-hidden="true">
        <g fill="none" stroke="#4e9d6e" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round">
          {/* shield */}
          <path d="M19 13.5 H45 Q50 13.5 50 18 V33 Q50 45 32 54 Q14 45 14 33 V18 Q14 13.5 19 13.5 Z" />
          {/* eye almond */}
          <path d="M20 30 Q31 21 42 30 Q31 39 20 30 Z" />
          {/* iris */}
          <circle cx="31" cy="30" r="6" />
          {/* checkmark */}
          <path d="M24 41 L31 48 L46 30" strokeWidth="3.4" />
        </g>
        {/* pupil + highlight */}
        <circle cx="31" cy="30" r="3" fill="#4e9d6e" />
        <circle cx="29.5" cy="28.6" r="1" fill="#eaf6ef" />
      </svg>
      <span className="brand-wordmark">mymatasan</span>
    </div>
  );
}

export function LoginPage({ credentials, busy, message, onChange, onSubmit }) {
  return (
    <main className="login-screen">
      <form className="login-panel" onSubmit={onSubmit}>
        <div>
          <h1>MyMataSan</h1>
          <p>Standalone camera monitor</p>
        </div>
        <label>
          Username
          <input
            value={credentials.username}
            onChange={(event) => onChange({ ...credentials, username: event.target.value })}
            autoComplete="username"
            autoFocus
          />
        </label>
        <label>
          Password
          <input
            value={credentials.password}
            onChange={(event) => onChange({ ...credentials, password: event.target.value })}
            type="password"
            autoComplete="current-password"
          />
        </label>
        <button type="submit" disabled={busy}>
          <span className="btn-icon"><Ico n="login" /> Sign In</span>
        </button>
        <Message value={message} />
        <FormBusyOverlay busy={busy} />
      </form>
    </main>
  );
}

// ModuleDropdown collapses the primary module tabs into a single dropdown (same
// look as the theme dropdown) so the topbar stays uncluttered. The trigger shows
// the active module.
function ModuleDropdown({ tabs, activeTab, onTab }) {
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
  const active = tabs.find((t) => t.id === activeTab) || tabs[0];
  return (
    <div className="module-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={`quiet module-toggle${open ? ' active' : ''}`}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon">
          <Ico n={active.icon} sz={14} />
          {active.label}
          <Ico n="chev-down" sz={11} />
        </span>
      </button>
      {open && (
        <div className="module-menu" role="listbox" aria-label="Select module">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="option"
              aria-selected={tab.id === activeTab}
              className={`module-menu-item${tab.id === activeTab ? ' active' : ''}`}
              onClick={() => { onTab(tab.id); setOpen(false); }}
            >
              <Ico n={tab.icon} sz={14} /> {tab.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function TopBar({ activeTab, busy, onTab, onRefresh, onLogout, alerts, savedDevices, notifOpen, notifUnread, onNotifToggle, onNotifClick, theme, onThemeChange }) {
  const tabs = [
    { id: 'views',     label: 'Live Views', icon: 'monitor' },
    { id: 'cameras',   label: 'Cameras',    icon: 'camera'  },
    { id: 'ai',        label: 'AI',         icon: 'cpu'     },
    { id: 'training',  label: 'Training',   icon: 'folder'  },
    { id: 'recording', label: 'Recording',  icon: 'film'    },
    { id: 'settings',  label: 'Settings',   icon: 'sliders' },
  ];
  const notifAlerts = useMemo(
    () => (alerts || []).filter((a) => !a.isAcknowledged && !parseMetadata(a.metadata).diagnostic).slice(0, 20),
    [alerts],
  );
  return (
    <header className="topbar">
      <BrandLogo />
      <nav className="primary-tabs" aria-label="Main">
        <ModuleDropdown tabs={tabs} activeTab={activeTab} onTab={onTab} />
      </nav>
      <div className="topbar-actions">
        <div className="notif-wrap">
          <button
            type="button"
            className={`quiet notif-btn${notifOpen ? ' active' : ''}`}
            onClick={onNotifToggle}
            aria-label={`Events${notifUnread > 0 ? `, ${notifUnread} unread` : ''}`}
          >
            <span className="btn-icon">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" style={{verticalAlign:'middle',flexShrink:0}}>
                <path d="M12 22c1.1 0 2-.9 2-2h-4c0 1.1.9 2 2 2zm6-6V11c0-3.07-1.63-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68C7.63 5.36 6 7.92 6 11v5l-2 2v1h16v-1l-2-2z"/>
              </svg>
              Events
              <span className={`notif-badge${notifUnread > 0 ? ' notif-badge--visible' : ''}`}>
                {notifUnread > 99 ? '99+' : notifUnread || ''}
              </span>
            </span>
          </button>
          {notifOpen && (
            <div className="notif-panel" role="dialog" aria-label="Recent events">
              <div className="notif-panel-header">Recent events</div>
              {notifAlerts.length === 0 ? (
                <p className="notif-empty">No recent events.</p>
              ) : (
                notifAlerts.map((alert) => {
                  const cam = (savedDevices || []).find((d) => Number(d.id) === Number(alert.cameraId));
                  return (
                    <button
                      key={alert.id}
                      type="button"
                      className="notif-item"
                      onClick={() => onNotifClick(alert.cameraId, alert.id)}
                    >
                      <span className="notif-label">{alert.label || alert.detectionType || 'Event'}</span>
                      <span className="notif-camera">{cam ? cameraTitle(cam) : `Camera ${alert.cameraId}`}</span>
                      <span className="notif-time">{formatTimestamp(alert.createdAt)}</span>
                    </button>
                  );
                })
              )}
            </div>
          )}
        </div>
        <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
        <button type="button" className="quiet" onClick={onRefresh} disabled={busy}>
          <span className="btn-icon"><Ico n="refresh" /> Refresh</span>
        </button>
        <button type="button" className="quiet danger-text" onClick={onLogout} disabled={busy}>
          <span className="btn-icon"><Ico n="lock" /> Lock</span>
        </button>
      </div>
    </header>
  );
}

