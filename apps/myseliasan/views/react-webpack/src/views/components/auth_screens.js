import { useState } from 'react';
import { Ico, useT } from '@shared';
import { BrandLogo } from './layout';
import { api } from '../lib/helpers';

// LoginScreen offers both sign-in paths: federated (myidsan SSO) and the local
// bootstrap stock-superadmin username/password.
export function LoginScreen({ onLoggedIn }) {
  const t = useT();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

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
    else setErr(r.message || t('auth.invalidCreds'));
  }

  return (
    <main className="login-shell">
      <section className="login-card">
        <BrandLogo size={52} />
        <p className="login-sub">{t('auth.subSignin')}</p>
        <button type="button" className="login-sso" onClick={() => { window.location.href = '/api/auth/start'; }}>
          <span className="btn-icon"><Ico n="login" /> {t('auth.continueMyidsan')}</span>
        </button>
        <div className="login-divider"><span>{t('auth.orStock')}</span></div>
        <form className="login-form" onSubmit={localLogin}>
          <label>{t('auth.username')}
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" disabled={busy} />
          </label>
          <label>{t('auth.password')}
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" disabled={busy} />
          </label>
          {err ? <p className="login-err">{err}</p> : null}
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="lock" /> {busy ? t('auth.signingIn') : t('auth.signIn')}</span>
          </button>
        </form>
      </section>
    </main>
  );
}

// ChangePasswordScreen forces the stock superadmin (must-change) to pick a new
// password before reaching the app.
export function ChangePasswordScreen({ onDone, onToast, onLogout }) {
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
    else setErr(r.message || t('auth.couldNotChange'));
  }

  return (
    <main className="login-shell">
      <section className="login-card">
        <BrandLogo size={48} />
        <p className="login-sub">{t('auth.setNewPassword')}</p>
        <p className="login-hint">{t('auth.changeHint')}</p>
        <form className="login-form" onSubmit={submit}>
          <label>{t('auth.currentPassword')}
            <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" disabled={busy} />
          </label>
          <label>{t('auth.newPassword')}
            <input type="password" value={next1} onChange={(e) => setNext1(e.target.value)} autoComplete="new-password" disabled={busy} />
          </label>
          <label>{t('auth.confirmNewPassword')}
            <input type="password" value={next2} onChange={(e) => setNext2(e.target.value)} autoComplete="new-password" disabled={busy} />
          </label>
          {err ? <p className="login-err">{err}</p> : null}
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="check-ok" /> {busy ? t('auth.saving') : t('auth.setPassword')}</span>
          </button>
          {onLogout ? <button type="button" className="quiet" onClick={onLogout}>{t('auth.logout')}</button> : null}
        </form>
      </section>
    </main>
  );
}

// PendingClearanceScreen gates a freshly-provisioned account (authenticated but with
// no role yet) out of the control plane until a superadmin assigns it a role on the
// RBAC page. It offers only a re-check and a log-out.
export function PendingClearanceScreen({ email, onRefresh, onLogout }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  async function recheck() {
    setBusy(true);
    try { await onRefresh(); } finally { setBusy(false); }
  }
  return (
    <main className="login-shell">
      <section className="login-card">
        <BrandLogo size={48} />
        <p className="login-sub">{t('auth.accessPending')}</p>
        <p className="login-hint">{t('auth.pendingHint', { email: email ? ` (${email})` : '' })}</p>
        <div className="login-form">
          <button type="button" disabled={busy} onClick={recheck}>
            <span className="btn-icon"><Ico n="reload" /> {busy ? t('auth.checking') : t('auth.checkAgain')}</span>
          </button>
          {onLogout ? <button type="button" className="quiet" onClick={onLogout}>{t('auth.logout')}</button> : null}
        </div>
      </section>
    </main>
  );
}
