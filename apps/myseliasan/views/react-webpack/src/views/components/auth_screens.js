import { useState } from 'react';
import { Ico } from './icons';
import { BrandLogo } from './layout';
import { api } from '../lib/helpers';

// LoginScreen offers both sign-in paths: federated (myidsan SSO) and the local
// bootstrap stock-superadmin username/password.
export function LoginScreen({ onLoggedIn }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function localLogin(e) {
    e.preventDefault();
    if (!username.trim() || !password) { setErr('Enter your username and password.'); return; }
    setBusy(true); setErr('');
    const r = await api('/api/auth/local-login', {
      method: 'POST', noRedirect: true,
      body: JSON.stringify({ username: username.trim(), password }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) onLoggedIn();
    else setErr(r.message || 'Invalid username or password.');
  }

  return (
    <main className="login-shell">
      <section className="login-card">
        <BrandLogo size={52} />
        <p className="login-sub">Sign in to the control plane</p>
        <button type="button" className="login-sso" onClick={() => { window.location.href = '/api/auth/start'; }}>
          <span className="btn-icon"><Ico n="login" /> Continue with myidsan</span>
        </button>
        <div className="login-divider"><span>or stock superadmin</span></div>
        <form className="login-form" onSubmit={localLogin}>
          <label>Username
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" disabled={busy} />
          </label>
          <label>Password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" disabled={busy} />
          </label>
          {err ? <p className="login-err">{err}</p> : null}
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="lock" /> {busy ? 'Signing in…' : 'Sign in'}</span>
          </button>
        </form>
      </section>
    </main>
  );
}

// ChangePasswordScreen forces the stock superadmin (must-change) to pick a new
// password before reaching the app.
export function ChangePasswordScreen({ onDone, onToast, onLogout }) {
  const [current, setCurrent] = useState('');
  const [next1, setNext1] = useState('');
  const [next2, setNext2] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  async function submit(e) {
    e.preventDefault();
    if (next1.length < 8) { setErr('New password must be at least 8 characters.'); return; }
    if (next1 !== next2) { setErr('New passwords do not match.'); return; }
    setBusy(true); setErr('');
    const r = await api('/api/auth/change-password', {
      method: 'POST', noRedirect: true,
      body: JSON.stringify({ currentPassword: current, newPassword: next1 }),
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (r.ok) { if (onToast) onToast('Password updated.'); onDone(); }
    else setErr(r.message || 'Could not change the password.');
  }

  return (
    <main className="login-shell">
      <section className="login-card">
        <BrandLogo size={48} />
        <p className="login-sub">Set a new password</p>
        <p className="login-hint">The stock superadmin must replace the default password before continuing.</p>
        <form className="login-form" onSubmit={submit}>
          <label>Current password
            <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" disabled={busy} />
          </label>
          <label>New password
            <input type="password" value={next1} onChange={(e) => setNext1(e.target.value)} autoComplete="new-password" disabled={busy} />
          </label>
          <label>Confirm new password
            <input type="password" value={next2} onChange={(e) => setNext2(e.target.value)} autoComplete="new-password" disabled={busy} />
          </label>
          {err ? <p className="login-err">{err}</p> : null}
          <button type="submit" disabled={busy}>
            <span className="btn-icon"><Ico n="check-ok" /> {busy ? 'Saving…' : 'Set password'}</span>
          </button>
          {onLogout ? <button type="button" className="quiet" onClick={onLogout}>Log out</button> : null}
        </form>
      </section>
    </main>
  );
}
