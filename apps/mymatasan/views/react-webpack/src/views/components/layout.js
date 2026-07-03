import { useMemo, useState, useEffect } from 'react';
import { Ico } from './icons';
import { SideNav as SharedSideNav } from '@shared/SideNav';
import { useT } from '@shared/i18n';
import { ThemeDropdown, FormBusyOverlay, Message } from './ui';
import { LanguageDropdown } from '@shared/LanguageDropdown';
import {cameraTitle,cameraDescription,orderedSavedCameras } from '../lib/helpers';

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
export function PasswordField({ value, onChange, autoComplete = 'off', autoFocus = false, placeholder, disabled = false, error = false }) {
  const t = useT();
  const [show, setShow] = useState(false);
  return (
    <div className={`password-field${error ? ' input-error' : ''}`}>
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

export function LoginPage({ credentials, busy, message, lockoutUntil, onChange, onSubmit, lang, onLangChange }) {
  const t = useT();
  const lockRemaining = useCountdown(lockoutUntil || 0);
  const locked = lockRemaining > 0;
  return (
    <main className="login-screen">
      {onLangChange ? (
        <div className="login-lang-switch">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
        </div>
      ) : null}
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
export function ChangePasswordPage({ busy, message, onSubmit, onCancel, lang, onLangChange }) {
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
      {onLangChange ? (
        <div className="login-lang-switch">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
        </div>
      ) : null}
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

// RecoveryGatePage is the pre-login screen shown when the backend is in recovery mode
// (encryption on, master key missing but a key existed here before). It reuses the login
// chrome — including the language switcher — and lets the operator upload their exported
// recovery key + passphrase to restore access. The raw key file is read as bytes and
// base64-encoded for the unlock request; nothing here needs a session.
export function RecoveryGatePage({ keyId, busy, restarting, message, onSubmit, lang, onLangChange }) {
  const t = useT();
  const [passphrase, setPassphrase] = useState('');
  const [fileName, setFileName] = useState('');
  const [keyBase64, setKeyBase64] = useState('');
  const [localError, setLocalError] = useState('');

  async function onPickFile(event) {
    const file = event.target.files && event.target.files[0];
    if (!file) return;
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      setKeyBase64(btoa(bin));
      setFileName(file.name);
      setLocalError('');
    } catch (_) {
      setLocalError(t('rg.readFail'));
    }
  }

  function submit(event) {
    event.preventDefault();
    if (!keyBase64) { setLocalError(t('rg.needFile')); return; }
    if (!passphrase) { setLocalError(t('rg.needPass')); return; }
    setLocalError('');
    onSubmit({ keyBase64, passphrase });
  }

  return (
    <main className="login-screen">
      {onLangChange ? (
        <div className="login-lang-switch">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
        </div>
      ) : null}
      <form className="login-panel" onSubmit={submit}>
        <div className="login-brand">
          <BrandLogo size={104} className="brand-logo--login" />
          <p>{t('rg.subtitle')}</p>
        </div>
        <p className="login-note">{t('rg.note')}</p>
        {keyId ? (
          <p className="login-note login-keyid">{t('rg.keyId')}: <code>{keyId}</code></p>
        ) : null}
        <label>
          {t('rg.file')}
          <input type="file" accept=".atrestkey,application/octet-stream" onChange={onPickFile} disabled={busy || restarting} />
        </label>
        {fileName ? <p className="login-note">{fileName}</p> : null}
        <label>
          {t('rg.passphrase')}
          <PasswordField
            value={passphrase}
            onChange={setPassphrase}
            autoComplete="off"
            disabled={busy || restarting}
          />
        </label>
        <button type="submit" disabled={busy || restarting}>
          <span className="btn-icon"><Ico n="key" /> {restarting ? t('rg.restarting') : t('rg.unlock')}</span>
        </button>
        <Message value={localError || message} />
        <FormBusyOverlay busy={busy || restarting} />
      </form>
    </main>
  );
}

// WorkspaceHeader is the slim action strip at the top of the main workspace: the
// language switcher plus the compact theme picker sitting just after it (top-right).
// Primary navigation and the account/logout block live in the side rail (SideNav).
export function WorkspaceHeader({ lang, onLangChange, theme, onThemeChange }) {
  return (
    <div className="workspace-header">
      <LanguageDropdown lang={lang} onLang={onLangChange} />
      <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
    </div>
  );
}

// AccountCard is the slim sign-out pill under the brand at the top of the rail: a
// role chip (avatar + role) and a ghost logout icon button. The username is
// deliberately NOT shown (avoids exposing the signed-in identity in the UI).
function AccountCard({ roleLabel, onLogout, busy }) {
  const t = useT();
  return (
    <div className="side-account">
      <span className="side-account-avatar" aria-hidden="true"><Ico n="user" sz={15} /></span>
      <span className="side-account-role">{roleLabel}</span>
      <button
        type="button"
        className="side-account-logout"
        onClick={onLogout}
        disabled={busy}
        title={t('auth.logout')}
        aria-label={t('auth.logout')}
      >
        <Ico n="logout" sz={16} />
      </button>
    </div>
  );
}

// Past this many cameras the branch gets a filter box and becomes a height-capped
// scroll area, so the rail stays usable with dozens of cameras.
const CAMERA_FILTER_THRESHOLD = 8;

// CamerasNavItem renders the Cameras entry as an expandable tree: the root row opens
// the probe/discovery page, while each saved camera is a child that jumps straight to
// that camera's properties (details/access/stream/recording/onvif). A caret toggles
// the branch without leaving the current view. The status dot mirrors the camera's
// live reachability (online/offline). Large sets get a search filter + scroll.
function CamerasNavItem({ cameras, active, managingCameraId, onSelectRoot, onSelectCamera }) {
  const t = useT();
  const [open, setOpen] = useState(active);
  const [query, setQuery] = useState('');
  const list = useMemo(() => orderedSavedCameras(Array.isArray(cameras) ? cameras : []), [cameras]);
  const rootActive = active && !managingCameraId;

  const big = list.length > CAMERA_FILTER_THRESHOLD;
  const q = query.trim().toLowerCase();
  const shown = q
    ? list.filter((c) => `${cameraTitle(c)} ${cameraDescription(c) || ''} ${c.host || c.xAddr || ''}`.toLowerCase().includes(q))
    : list;

  return (
    <div className="nav-tree">
      <div className="nav-tree-rootrow">
        <button
          type="button"
          className="nav-tree-caret"
          onClick={() => setOpen((o) => !o)}
          aria-label={open ? t('cam.collapse') : t('cam.expand')}
          aria-expanded={open}
        >
          <Ico n="chev-down" sz={14} style={open ? undefined : { transform: 'rotate(-90deg)' }} />
        </button>
        <button
          type="button"
          className={`nav-item tone-blue nav-tree-main${rootActive ? ' active' : ''}`}
          onClick={() => { onSelectRoot(); setOpen(true); }}
        >
          <span className="nav-ico"><Ico n="camera" sz={17} /></span>
          <span className="nav-label">{t('nav.cameras')}</span>
          {list.length > 0 ? <span className="nav-tree-count">{list.length}</span> : null}
        </button>
      </div>
      {open ? (
        <div className="nav-tree-branch">
          {big ? (
            <div className="nav-tree-search">
              <Ico n="search" sz={13} />
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t('cam.filter', { n: list.length })}
                aria-label={t('cam.filterAria')}
              />
            </div>
          ) : null}
          <div className={`nav-tree-children${big ? ' scrolling' : ''}`}>
            {list.length === 0 ? (
              <div className="nav-tree-empty">{t('cam.none')}</div>
            ) : shown.length === 0 ? (
              <div className="nav-tree-empty">{t('cam.noMatches')}</div>
            ) : (
              shown.map((c) => {
                const isActive = active && Number(managingCameraId) === Number(c.id);
                const status = (c.healthStatus || '').toLowerCase() === 'online'
                  ? 'online'
                  : (c.healthStatus || '').toLowerCase() === 'offline'
                    ? 'offline'
                    : 'unknown';
                const description = cameraDescription(c);
                return (
                  <button
                    key={c.id || c.xAddr}
                    type="button"
                    className={`nav-item tone-blue nav-tree-child${isActive ? ' active' : ''}`}
                    onClick={() => onSelectCamera(c.id)}
                    title={description ? undefined : cameraTitle(c)}
                  >
                    <span className="nav-tree-ico" data-status={status}>
                      <Ico n="camera" sz={16} />
                    </span>
                    <span className="nav-label">{cameraTitle(c)}</span>
                    {description ? (
                      <span className="nav-tip" role="tooltip">
                        <span className="nav-tip-title">{cameraTitle(c)}</span>
                        <span className="nav-tip-body">{description}</span>
                      </span>
                    ) : null}
                  </button>
                );
              })
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

// SideNav is mymatasan's standardized navigation rail (shared SideNav shell). Modules
// are grouped like myseliasan's; the Cameras entry is a bespoke tree whose root opens
// the probe page and whose children open per-camera properties. Non-admins see only
// the view-only surfaces (Dashboard, Live Views, Recordings, Notifications).
export function SideNav({ activeTab, isAdmin, busy, cameras, managingCameraId, notifUnread = 0, onTab, onSelectCameraRoot, onSelectCamera, onLogout, pinned = true, onTogglePinned }) {
  const t = useT();
  const navItem = (id, label, icon, tone) => ({ id, label, icon, tone, active: id === activeTab, onClick: () => onTab(id) });
  // The Notifications entry doubles as the notifications control now that the topbar
  // bell is gone: it opens the full list (no dropdown) and carries the unread badge.
  const notificationsItem = {
    id: 'notifications',
    render: () => (
      <button
        type="button"
        className={`nav-item nav-item-badged tone-green${activeTab === 'notifications' ? ' active' : ''}`}
        onClick={() => onTab('notifications')}
        aria-label={notifUnread > 0 ? t('topbar.notifAriaUnread', { n: notifUnread }) : t('topbar.notifAria')}
      >
        <span className="nav-ico"><Ico n="bell" sz={17} /></span>
        <span className="nav-label">{t('tab.notifications')}</span>
        {notifUnread > 0 ? (
          <span className="nav-item-badge">{notifUnread > 99 ? '99+' : notifUnread}</span>
        ) : null}
      </button>
    ),
  };
  const groups = [
    {
      label: t('group.workspace'),
      items: [
        navItem('dashboard', t('nav.dashboard'), 'monitor', 'steel'),
        navItem('views', t('tab.views'), 'grid2', 'teal'),
      ],
    },
    {
      label: t('group.surveillance'),
      items: [
        isAdmin
          ? { id: 'cameras', render: () => (
              <CamerasNavItem
                cameras={cameras}
                active={activeTab === 'cameras'}
                managingCameraId={managingCameraId}
                onSelectRoot={onSelectCameraRoot}
                onSelectCamera={onSelectCamera}
              />
            ) }
          : null,
      ],
    },
    {
      label: t('group.intelligence'),
      items: isAdmin
        ? [navItem('training', t('tab.training'), 'folder', 'amber')]
        : [],
    },
    {
      label: t('group.system'),
      items: [
        notificationsItem,
        isAdmin ? navItem('settings', t('tab.settings'), 'sliders', 'blue') : null,
      ],
    },
  ];

  const brand = (
    <div className="side-brand">
      {onTogglePinned ? (
        <button
          type="button"
          className="nav-pin-toggle"
          onClick={(e) => { onTogglePinned(); e.currentTarget.blur(); }}
          aria-pressed={pinned}
          title={pinned ? t('nav.autohide') : t('nav.pin')}
          aria-label={pinned ? t('nav.autohide') : t('nav.pin')}
        >
          <Ico n={pinned ? 'pin' : 'pin-off'} sz={16} />
        </button>
      ) : null}
      <BrandLogo />
      <div className="side-brand-sub">{t('brand.subtitle')}</div>
      <AccountCard
        roleLabel={isAdmin ? t('role.admin') : t('role.viewer')}
        onLogout={onLogout}
        busy={busy}
      />
    </div>
  );

  return <SharedSideNav brand={brand} groups={groups} footer={null} />;
}

