import { useMemo, useState, useRef, useEffect } from 'react';
import { Ico } from './icons';
import { useT } from '@shared/i18n';
import { ThemeDropdown, FormBusyOverlay, Message } from './ui';
import { LanguageDropdown } from '@shared/LanguageDropdown';
import {formatTimestamp,notificationKind } from '../lib/helpers';

// BrandLogo is the MyMataSan mark: a line-art shield holding a watchful eye and a
// checkmark, with the rounded lowercase wordmark underneath. Self-contained inline
// SVG so it scales without a binary asset.
export function BrandLogo({ size = 40, className = '' }) {
  return (
    <div className={`brand-logo${className ? ` ${className}` : ''}`} aria-label="mymatasan">
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

// PasswordField is a password input with an in-field reveal (eye) toggle. The
// app standard for any password entry — used on the login, forced-change, and
// settings screens — so the control looks and behaves identically everywhere.
export function PasswordField({ value, onChange, autoComplete = 'off', autoFocus = false, placeholder, disabled = false }) {
  const t = useT();
  const [show, setShow] = useState(false);
  return (
    <div className="password-field">
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        type={show ? 'text' : 'password'}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        placeholder={placeholder}
        disabled={disabled}
      />
      <button
        type="button"
        className="password-reveal"
        onClick={() => setShow((s) => !s)}
        aria-label={show ? t('login.hidePassword') : t('login.showPassword')}
        aria-pressed={show}
        tabIndex={-1}
        disabled={disabled}
      >
        <Ico n={show ? 'eye-off' : 'eye'} sz={16} />
      </button>
    </div>
  );
}

// useCountdown returns the whole seconds remaining until `untilMs`, ticking every
// second while active and settling to 0 when the deadline passes.
function useCountdown(untilMs) {
  const remainingNow = () => Math.max(0, Math.ceil((untilMs - Date.now()) / 1000));
  const [remaining, setRemaining] = useState(remainingNow);
  useEffect(() => {
    setRemaining(remainingNow());
    if (!untilMs || untilMs <= Date.now()) return undefined;
    const id = setInterval(() => {
      const left = Math.max(0, Math.ceil((untilMs - Date.now()) / 1000));
      setRemaining(left);
      if (left <= 0) clearInterval(id);
    }, 1000);
    return () => clearInterval(id);
  }, [untilMs]);
  return remaining;
}

function formatCountdown(totalSeconds) {
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}

export function LoginPage({ credentials, busy, message, lockoutUntil, onChange, onSubmit }) {
  const t = useT();
  const lockRemaining = useCountdown(lockoutUntil || 0);
  const locked = lockRemaining > 0;
  return (
    <main className="login-screen">
      <form className="login-panel" onSubmit={onSubmit}>
        <div className="login-brand">
          <BrandLogo size={104} className="brand-logo--login" />
          <p>{t('login.subtitle')}</p>
        </div>
        <label>
          {t('login.username')}
          <input
            value={credentials.username}
            onChange={(event) => onChange({ ...credentials, username: event.target.value })}
            autoComplete="username"
            autoFocus
            disabled={locked}
          />
        </label>
        <label>
          {t('login.password')}
          <PasswordField
            value={credentials.password}
            onChange={(password) => onChange({ ...credentials, password })}
            autoComplete="current-password"
            disabled={locked}
          />
        </label>
        <button type="submit" disabled={busy || locked}>
          <span className="btn-icon"><Ico n="login" /> {t('login.signIn')}</span>
        </button>
        {locked ? (
          <div className="login-lockout" role="alert">
            <Ico n="lock" sz={15} />
            <span>{t('login.lockout', { time: formatCountdown(lockRemaining) })}</span>
          </div>
        ) : null}
        <Message value={message} />
        <FormBusyOverlay busy={busy} />
      </form>
    </main>
  );
}

// ChangePasswordPage is the forced first-login password change. It reuses the
// login-panel chrome so the transition from sign-in feels seamless. Validation
// (match + length) happens here; the server re-checks and is the source of truth.
export function ChangePasswordPage({ busy, message, onSubmit, onCancel }) {
  const t = useT();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [localError, setLocalError] = useState('');

  function submit(event) {
    event.preventDefault();
    if (newPassword.length < 8) {
      setLocalError(t('cpw.min'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setLocalError(t('cpw.noMatch'));
      return;
    }
    setLocalError('');
    onSubmit({ currentPassword, newPassword });
  }

  return (
    <main className="login-screen">
      <form className="login-panel" onSubmit={submit}>
        <div className="login-brand">
          <BrandLogo size={104} className="brand-logo--login" />
          <p>{t('cpw.subtitle')}</p>
        </div>
        <p className="login-note">{t('cpw.note')}</p>
        <label>
          {t('cpw.current')}
          <PasswordField
            value={currentPassword}
            onChange={setCurrentPassword}
            autoComplete="current-password"
            autoFocus
          />
        </label>
        <label>
          {t('cpw.new')}
          <PasswordField
            value={newPassword}
            onChange={setNewPassword}
            autoComplete="new-password"
          />
        </label>
        <label>
          {t('cpw.confirm')}
          <PasswordField
            value={confirmPassword}
            onChange={setConfirmPassword}
            autoComplete="new-password"
          />
        </label>
        <button type="submit" disabled={busy}>
          <span className="btn-icon"><Ico n="key" /> {t('cpw.submit')}</span>
        </button>
        <button type="button" className="quiet" onClick={onCancel} disabled={busy}>
          {t('cpw.differentUser')}
        </button>
        <Message value={localError || message} />
        <FormBusyOverlay busy={busy} />
      </form>
    </main>
  );
}

// ModuleDropdown collapses the primary module tabs into a single dropdown (same
// look as the theme dropdown) so the topbar stays uncluttered. The trigger shows
// the active module.
function ModuleDropdown({ tabs, activeTab, onTab }) {
  const t = useT();
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
        <div className="module-menu" role="listbox" aria-label={t('topbar.selectModule')}>
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

export function TopBar({ activeTab, isAdmin, busy, onTab, onRefresh, onLogout, notifications, notifOpen, notifUnread, onNotifToggle, onNotifClick, theme, onThemeChange, lang, onLangChange }) {
  const t = useT();
  // Non-admins are view-only: they get live views, recording playback, and the
  // notification feed; camera/AI/training/settings management is admin-only.
  const allTabs = [
    { id: 'views',     label: t('tab.views'),         icon: 'monitor', adminOnly: false },
    { id: 'cameras',   label: t('tab.cameras'),       icon: 'camera',  adminOnly: true  },
    { id: 'ai',        label: t('tab.ai'),            icon: 'cpu',     adminOnly: true  },
    { id: 'training',  label: t('tab.training'),      icon: 'folder',  adminOnly: true  },
    { id: 'recording', label: t('tab.recording'),     icon: 'film',    adminOnly: false },
    { id: 'notifications', label: t('tab.notifications'), icon: 'bell', adminOnly: false },
    { id: 'settings',  label: t('tab.settings'),      icon: 'sliders', adminOnly: true  },
  ];
  const tabs = allTabs.filter((tab) => isAdmin || !tab.adminOnly);
  const notifItems = useMemo(() => (notifications || []).slice(0, 20), [notifications]);
  return (
    <header className="topbar">
      <BrandLogo />
      <nav className="primary-tabs" aria-label="Main">
        <ModuleDropdown tabs={tabs} activeTab={activeTab} onTab={onTab} />
      </nav>
      <div className="topbar-actions">
        <LanguageDropdown lang={lang} onLang={onLangChange} />
        <div className="topbar-action-buttons">
        <div className="notif-wrap">
          <button
            type="button"
            className={`quiet notif-btn${notifOpen ? ' active' : ''}`}
            onClick={onNotifToggle}
            aria-label={notifUnread > 0 ? t('topbar.notifAriaUnread', { n: notifUnread }) : t('topbar.notifAria')}
          >
            <span className="btn-icon">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" style={{verticalAlign:'middle',flexShrink:0}}>
                <path d="M12 22c1.1 0 2-.9 2-2h-4c0 1.1.9 2 2 2zm6-6V11c0-3.07-1.63-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68C7.63 5.36 6 7.92 6 11v5l-2 2v1h16v-1l-2-2z"/>
              </svg>
              {t('topbar.notifications')}
              <span className={`notif-badge${notifUnread > 0 ? ' notif-badge--visible' : ''}`}>
                {notifUnread > 99 ? '99+' : notifUnread || ''}
              </span>
            </span>
          </button>
          {notifOpen && (
            <div className="notif-panel" role="dialog" aria-label={t('topbar.notifications')}>
              <div className="notif-panel-header">{t('topbar.notifications')}</div>
              {notifItems.length === 0 ? (
                <p className="notif-empty">{t('topbar.noNotifs')}</p>
              ) : (
                notifItems.map((notif) => {
                  const kind = notificationKind(notif);
                  return (
                    <button
                      key={notif.id}
                      type="button"
                      className="notif-item"
                      onClick={() => onNotifClick(notif)}
                    >
                      <span className="notif-topic">
                        <span className={`notif-sev notif-sev--${notif.severity || 'info'}`} aria-hidden="true" />
                        {notif.title || kind}
                      </span>
                      {notif.body ? <span className="notif-detail">{notif.body}</span> : null}
                      <span className="notif-meta-row">
                        <span className="notif-camera">{kind}</span>
                        <span className="notif-time">{formatTimestamp(notif.createdAt)}</span>
                      </span>
                    </button>
                  );
                })
              )}
            </div>
          )}
        </div>
        <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
        <button type="button" className="quiet" onClick={onRefresh} disabled={busy}>
          <span className="btn-icon"><Ico n="refresh" /> {t('topbar.refresh')}</span>
        </button>
        <button type="button" className="quiet danger-text" onClick={onLogout} disabled={busy}>
          <span className="btn-icon"><Ico n="lock" /> {t('topbar.lock')}</span>
        </button>
        </div>
      </div>
    </header>
  );
}

