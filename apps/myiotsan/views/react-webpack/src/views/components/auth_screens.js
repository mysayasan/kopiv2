import { useState } from 'react';
import { Ico, useT, PasswordField, LanguageDropdown } from '@shared';
import { BrandLogo } from './layout';
import { FormBusyOverlay, Message } from './ui';
import { api } from '../lib/helpers';

// The pre-app screens (sign-in, forced password change) wear the suite's standard
// login chrome: a centered .login-panel card under the large brand mark, with the
// language switcher pinned to the top corner so a user can pick their language before
// they can read anything else. AuthShell is that chrome.
function AuthShell({ subtitle, hint, lang, onLangChange, busy, message, children }) {
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
        <FormBusyOverlay busy={busy} />
      </section>
    </main>
  );
}

// LoginScreen is the local username/password sign-in. myiotsan authenticates against
// its own account store (the session cookie is set by POST /api/auth/login).
export function LoginScreen({ onLoggedIn, lang, onLangChange }) {
  const t = useT();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function login(e) {
    e.preventDefault();
    if (!username.trim() || !password) { setErr(t('auth.enterCreds')); return; }
    setBusy(true); setErr('');
    const r = await api('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username: username.trim(), password }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) onLoggedIn();
    else setErr(r.message || t('auth.invalidCreds'));
  }

  return (
    <AuthShell subtitle={t('auth.subSignin')} lang={lang} onLangChange={onLangChange} busy={busy} message={err}>
      <form className="login-form" onSubmit={login}>
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

// NoAccessScreen is what a signed-in account sees when the server will not tell it who it is.
//
// It exists because the alternative is a LIE THAT LOOPS. The shell decides between "signed in"
// and "signed out" from one probe, and a probe that is REFUSED is not the same as a probe that
// says nobody is here — but folding them together renders the sign-in card to somebody who has
// just signed in successfully. They retype the password, succeed again, and meet the same card:
// an infinite loop with no error anywhere, on either side of the wire. That really shipped, for
// every non-admin account this app has ever had.
//
// So a refusal gets its own screen, and it says the true thing: you are signed in, and this
// account is not permitted to use this application. It offers sign-out, because the one useful
// action is to come back as somebody else.
export function NoAccessScreen({ onLogout, lang, onLangChange }) {
  const t = useT();
  return (
    <AuthShell subtitle={t('auth.subNoAccess')} lang={lang} onLangChange={onLangChange}>
      <p className="login-note">{t('auth.noAccessBody')}</p>
      <div className="login-form">
        <button type="button" onClick={onLogout}>
          <span className="btn-icon"><Ico n="logout" sz={14} /> {t('auth.logout')}</span>
        </button>
      </div>
    </AuthShell>
  );
}

// ChangePasswordScreen forces an account flagged must-change (the stock admin on first
// login) to pick a new password before reaching the app.
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
      method: 'POST',
      body: JSON.stringify({ currentPassword: current, newPassword: next1 }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) { if (onToast) onToast(t('auth.passwordUpdated')); onDone(); }
    else setErr(r.message || t('auth.couldNotChange'));
  }

  return (
    <AuthShell
      subtitle={t('auth.setNewPassword')}
      hint={t('auth.changeHint')}
      lang={lang}
      onLangChange={onLangChange}
      busy={busy}
      message={err}
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
