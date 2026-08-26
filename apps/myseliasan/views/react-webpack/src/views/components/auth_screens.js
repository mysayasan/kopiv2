import { useEffect, useState } from 'react';
import { Ico, useT, PasswordField, LanguageDropdown } from '@shared';
import { BrandLogo, LoginHelpLink } from './layout';
import { FormBusyOverlay, Message } from './ui';
import { api } from '../lib/helpers';

// A lockout answers 429 with the remaining wait in the body (retryAfterSeconds) as well as
// in Retry-After, because a browser cannot read the header without an explicit
// Access-Control-Expose-Headers and a countdown that shows nothing reads as a broken app.
//
// Translated HERE rather than shown as the server's sentence: this app ships in four
// languages and the server sends one. Falling back to the server's message keeps a future
// refusal we do not know about readable instead of blank.
function lockoutMessage(t, r, fallback) {
  const secs = Number(r?.body?.retryAfterSeconds);
  if (r?.status !== 429 || !Number.isFinite(secs) || secs < 1) return fallback;
  if (secs >= 90) return t('auth.lockedMinutes', { minutes: Math.ceil(secs / 60) });
  return t('auth.lockedSeconds', { seconds: secs });
}

// The three pre-app screens (sign-in, forced password change, pending clearance) all
// wear the same chrome as mymatasan's login: a centered .login-panel card under the
// large brand mark, with the language switcher pinned to the top corner so a user can
// pick their language before they can read anything else. AuthShell is that chrome.
// `help` is a <LoginHelpLink> rendered under the card. It is a prop rather than something
// AuthShell picks, because each pre-session screen raises a different question and the whole
// point of a help link here is that it answers the one in front of you.
function AuthShell({ subtitle, hint, lang, onLangChange, busy, message, help, children }) {
  return (
    <main className="login-screen">
      {onLangChange ? (
        <div className="login-lang-switch">
          <LanguageDropdown lang={lang} onLang={onLangChange} />
        </div>
      ) : null}
      <section className="login-panel">
        <div className="login-brand">
          <BrandLogo size={104} className="brand-logo--login" />
          <p>{subtitle}</p>
        </div>
        {hint ? <p className="login-note">{hint}</p> : null}
        {children}
        <Message value={message} />
        {help || null}
        <FormBusyOverlay busy={busy} />
      </section>
    </main>
  );
}

// LoginScreen offers both sign-in paths: federated (myidsan SSO) and the local
// bootstrap stock-superadmin username/password.
export function LoginScreen({ onLoggedIn, lang, onLangChange }) {
  const t = useT();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  // A standalone install (the shipped package) has no myidsan behind it, so offering
  // "Continue with myidsan" would be a button that cannot work. Ask the server. Default
  // to showing it, so a failed probe degrades to the previous behaviour rather than
  // hiding the primary sign-in path of a federated deployment.
  const [ssoEnabled, setSsoEnabled] = useState(true);
  useEffect(() => {
    let live = true;
    api('/api/auth/config', { noRedirect: true })
      .then((r) => { if (live && r.ok && r.body) setSsoEnabled(r.body.ssoEnabled !== false); })
      .catch(() => {});
    return () => { live = false; };
  }, []);

  async function localLogin(e) {
    e.preventDefault();
    if (!username.trim() || !password) { setErr(t('auth.enterCreds')); return; }
    setBusy(true); setErr('');
    const r = await api('/api/auth/local-login', {
      method: 'POST', noRedirect: true,
      body: JSON.stringify({ username: username.trim(), password }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) onLoggedIn();
    else setErr(lockoutMessage(t, r, r.message || t('auth.invalidCreds')));
  }

  return (
    <AuthShell
      subtitle={t('auth.subSignin')}
      lang={lang}
      onLangChange={onLangChange}
      busy={busy}
      message={err}
      help={<LoginHelpLink slug="first-sign-in" anchor="bootstrap" />}
    >
      {ssoEnabled ? (
        <>
          <button type="button" className="login-sso" onClick={() => { window.location.href = '/api/auth/start'; }} disabled={busy}>
            <span className="btn-icon"><Ico n="login" /> {t('auth.continueMyidsan')}</span>
          </button>
          <div className="login-divider"><span>{t('auth.orStock')}</span></div>
        </>
      ) : null}
      <form className="login-form" onSubmit={localLogin}>
        <label>
          {t('auth.username')}
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" disabled={busy} />
        </label>
        <label>
          {t('auth.password')}
          <PasswordField value={password} onChange={setPassword} autoComplete="current-password" disabled={busy} />
        </label>
        <button type="submit" disabled={busy}>
          <span className="btn-icon"><Ico n="lock" /> {busy ? t('auth.signingIn') : t('auth.signIn')}</span>
        </button>
      </form>
    </AuthShell>
  );
}

// ChangePasswordScreen forces the stock superadmin (must-change) to pick a new
// password before reaching the app.
export function ChangePasswordScreen({ onDone, onToast, onLogout, lang, onLangChange }) {
  const t = useT();
  const [current, setCurrent] = useState('');
  const [next1, setNext1] = useState('');
  const [next2, setNext2] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function submit(e) {
    e.preventDefault();
    if (next1.length < 8) { setErr(t('auth.passwordMin')); return; }
    if (next1 !== next2) { setErr(t('auth.passwordsNoMatch')); return; }
    setBusy(true); setErr('');
    const r = await api('/api/auth/change-password', {
      method: 'POST', noRedirect: true,
      body: JSON.stringify({ currentPassword: current, newPassword: next1 }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('auth.passwordUpdated')); onDone(); }
    else setErr(lockoutMessage(t, r, r.message || t('auth.couldNotChange')));
  }

  return (
    <AuthShell
      subtitle={t('auth.setNewPassword')}
      hint={t('auth.changeHint')}
      lang={lang}
      onLangChange={onLangChange}
      busy={busy}
      message={err}
      help={<LoginHelpLink slug="first-sign-in" anchor="bootstrap" />}
    >
      <form className="login-form" onSubmit={submit}>
        <label>
          {t('auth.currentPassword')}
          <PasswordField value={current} onChange={setCurrent} autoComplete="current-password" disabled={busy} />
        </label>
        <label>
          {t('auth.newPassword')}
          <PasswordField value={next1} onChange={setNext1} autoComplete="new-password" disabled={busy} />
        </label>
        <label>
          {t('auth.confirmNewPassword')}
          <PasswordField value={next2} onChange={setNext2} autoComplete="new-password" disabled={busy} />
        </label>
        <button type="submit" disabled={busy}>
          <span className="btn-icon"><Ico n="check-ok" /> {busy ? t('auth.saving') : t('auth.setPassword')}</span>
        </button>
        {onLogout ? <button type="button" className="quiet" onClick={onLogout}>{t('auth.logout')}</button> : null}
      </form>
    </AuthShell>
  );
}

// PendingClearanceScreen gates a freshly-provisioned account (authenticated but with
// no role yet) out of the control plane until a superadmin assigns it a role on the
// RBAC page. It offers only a re-check and a log-out.
export function PendingClearanceScreen({ email, onRefresh, onLogout, lang, onLangChange }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  async function recheck() {
    setBusy(true);
    try { await onRefresh(); } finally { setBusy(false); }
  }
  return (
    <AuthShell
      subtitle={t('auth.accessPending')}
      hint={t('auth.pendingHint', { email: email ? ` (${email})` : '' })}
      lang={lang}
      onLangChange={onLangChange}
      busy={busy}
      help={<LoginHelpLink slug="first-sign-in" anchor="troubleshooting" />}
    >
      <div className="login-form">
        <button type="button" disabled={busy} onClick={recheck}>
          <span className="btn-icon"><Ico n="reload" /> {busy ? t('auth.checking') : t('auth.checkAgain')}</span>
        </button>
        {onLogout ? <button type="button" className="quiet" onClick={onLogout}>{t('auth.logout')}</button> : null}
      </div>
    </AuthShell>
  );
}
