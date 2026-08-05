import { useState } from 'react'
import { useT, BrandLogo, PasswordField, LanguageDropdown } from '@shared'
import * as api from '../lib/access'

// The standard suite login card, with one addition this app needs: the must-change-password step is
// handled inline rather than bounced to another screen. The bootstrap admin password is printed on
// a console during install, so the very first thing a real operator does here is replace it.
export default function Login({ onSignedIn, lang, onLang }) {
  const t = useT()
  const [form, setForm] = useState({ username: '', password: '' })
  const [mustChange, setMustChange] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async e => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const user = await api.login(form.username, form.password)
      if (user && user.mustChangePassword) {
        setMustChange(user)
        return
      }
      onSignedIn(user)
    } catch {
      // Deliberately one message for a bad username and a bad password. Telling them apart is a
      // free account-enumeration oracle.
      setError(t('login.failed'))
    } finally {
      setBusy(false)
    }
  }

  if (mustChange) {
    return (
      <ChangePassword
        current={form.password}
        lang={lang}
        onLang={onLang}
        onDone={onSignedIn}
      />
    )
  }

  return (
    <LoginShell lang={lang} onLang={onLang}>
      <form onSubmit={submit} className="form">
        <label>
          <span>{t('login.username')}</span>
          <input
            required autoFocus autoComplete="username"
            value={form.username}
            onChange={e => setForm({ ...form, username: e.target.value })}
          />
        </label>
        <label>
          <span>{t('login.password')}</span>
          <PasswordField
            value={form.password}
            autoComplete="current-password"
            onChange={v => setForm({ ...form, password: v })}
          />
        </label>
        {error ? <p className="form-error">{error}</p> : null}
        <button type="submit" className="btn btn-primary" disabled={busy}>{t('login.submit')}</button>
      </form>
    </LoginShell>
  )
}

function ChangePassword({ current, lang, onLang, onDone }) {
  const t = useT()
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async e => {
    e.preventDefault()
    if (next !== confirm) {
      setError(t('login.mismatch'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const user = await api.changePassword(current, next)
      onDone(user)
    } catch (err) {
      setError(err && err.message ? err.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <LoginShell lang={lang} onLang={onLang}>
      <form onSubmit={submit} className="form">
        <p className="muted">{t('login.mustChange')}</p>
        <label>
          <span>{t('login.newPassword')}</span>
          <PasswordField value={next} autoComplete="new-password" onChange={setNext} />
        </label>
        <label>
          <span>{t('login.confirmPassword')}</span>
          <PasswordField value={confirm} autoComplete="new-password" onChange={setConfirm} />
        </label>
        {error ? <p className="form-error">{error}</p> : null}
        <button type="submit" className="btn btn-primary" disabled={busy}>{t('login.changeSubmit')}</button>
      </form>
    </LoginShell>
  )
}

function LoginShell({ children, lang, onLang }) {
  const t = useT()
  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <BrandLogo size={34} />
          <div>
            <strong>{t('app.name')}</strong>
            <span>{t('app.tagline')}</span>
          </div>
        </div>
        <h1>{t('login.title')}</h1>
        {children}
        <div className="login-foot">
          <LanguageDropdown lang={lang} onLang={onLang} />
        </div>
      </div>
    </div>
  )
}
