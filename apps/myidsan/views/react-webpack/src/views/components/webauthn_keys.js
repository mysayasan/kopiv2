import React, { useCallback, useEffect, useState } from 'react'
import { Ico, useT } from '@shared'
import { apiRequest, resultOf, formatDateTime } from '../../lib/api'
import { createCredential, describeCeremonyError, webauthnSupported } from '../../lib/webauthn'

// WebAuthnKeys is the Profile page's "Security keys" card: enrol a FIDO2 authenticator
// (hardware key, Windows Hello, Touch ID), rename one, remove one.
//
// It sits ALONGSIDE the TOTP card rather than replacing it, and that is the point — a user
// may hold either or both, and the login gate accepts whichever they present. Keys are
// listed rather than shown as a single on/off state (the way TOTP is) because registering a
// second key and storing it elsewhere is the whole recovery story for a lost one.

export function WebAuthnKeys({ onToast }) {
  const t = useT()
  const [enabled, setEnabled] = useState(true)
  const [keys, setKeys] = useState([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [label, setLabel] = useState('')
  // Removal re-proves identity, so it opens an inline form rather than acting immediately.
  const [removing, setRemoving] = useState(null) // { id, label, password, code }
  const supported = webauthnSupported()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = resultOf(await apiRequest('/api/mfa/webauthn')) || {}
      setEnabled(res.enabled !== false)
      setKeys(Array.isArray(res.keys) ? res.keys : [])
      setError('')
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { load() }, [load])

  // add runs the two-leg registration ceremony: fetch options, let the authenticator sign
  // them, post the result back. The browser call is the only part that can hang, so the
  // busy flag covers the whole sequence.
  const add = async () => {
    setBusy(true)
    setError('')
    try {
      const options = resultOf(await apiRequest('/api/mfa/webauthn/register/begin', { method: 'POST' }))
      const credential = await createCredential(options)
      const view = resultOf(await apiRequest('/api/mfa/webauthn/register/finish', {
        method: 'POST',
        body: { label, credential }
      }))
      setLabel('')
      await load()
      onToast?.(t('webauthn.addedToast', { label: view?.label || t('webauthn.defaultLabel') }), 'success')
    } catch (err) {
      // A ceremony rejection is a DOMException with an opaque name; translate it. An API
      // failure is already a plain message.
      setError(err?.name ? describeCeremonyError(err, t) : err.message)
    } finally {
      setBusy(false)
    }
  }

  const rename = async key => {
    const next = window.prompt(t('webauthn.renamePrompt'), key.label || '')
    if (next === null) return
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/mfa/webauthn/${key.id}`, { method: 'PUT', body: { label: next } })
      await load()
      onToast?.(t('webauthn.renamedToast'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/mfa/webauthn/${removing.id}`, {
        method: 'DELETE',
        body: { password: removing.password, code: removing.code }
      })
      setRemoving(null)
      await load()
      onToast?.(t('webauthn.removedToast'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="profile-card">
      <div className="profile-card-head">
        <span className="profile-card-icon"><Ico n="key" sz={16} /></span>
        <h2>{t('webauthn.title')}</h2>
      </div>
      <p className="auth-hint" style={{ marginTop: 0 }}>{t('webauthn.subtitle')}</p>

      {error && <div className="message danger">{error}</div>}

      {!enabled ? (
        <div className="message warning">{t('webauthn.serverDisabled')}</div>
      ) : !supported ? (
        // Most often this is not a browser-age problem but a scheme one: WebAuthn needs a
        // secure context, so plain http on a real hostname has no API to call.
        <div className="message warning">{t('webauthn.unsupported')}</div>
      ) : loading ? (
        <p className="auth-hint">{t('common.loading')}</p>
      ) : (
        <div className="record-form">
          {keys.length === 0 ? (
            <div className="message warning">{t('webauthn.noneEnrolled')}</div>
          ) : (
            <ul className="webauthn-list">
              {keys.map(key => (
                <li key={key.id} className="webauthn-row">
                  <span className="webauthn-row-ico" aria-hidden="true"><Ico n="key" sz={15} /></span>
                  <div className="webauthn-row-main">
                    <div className="webauthn-row-name">
                      {key.label || t('webauthn.defaultLabel')}
                      {key.backedUp && (
                        <span className="webauthn-tag" title={t('webauthn.syncedTip')}>{t('webauthn.synced')}</span>
                      )}
                      {key.cloneWarning && (
                        <span className="webauthn-tag danger" title={t('webauthn.cloneTip')}>{t('webauthn.cloneFlag')}</span>
                      )}
                    </div>
                    <div className="webauthn-row-meta">
                      {t('webauthn.addedOn', { date: formatDateTime(key.createdAt) })}
                      {key.lastUsedAt
                        ? ` · ${t('webauthn.lastUsed', { date: formatDateTime(key.lastUsedAt) })}`
                        : ` · ${t('webauthn.neverUsed')}`}
                      {key.transports ? ` · ${key.transports}` : ''}
                    </div>
                  </div>
                  <div className="webauthn-row-actions">
                    <button className="quiet-link" type="button" disabled={busy} onClick={() => rename(key)}>
                      {t('webauthn.rename')}
                    </button>
                    <button
                      className="quiet-link danger"
                      type="button"
                      disabled={busy}
                      onClick={() => { setRemoving({ id: key.id, label: key.label, password: '', code: '' }); setError('') }}
                    >
                      {t('webauthn.remove')}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}

          {removing ? (
            <form className="record-form" onSubmit={remove} style={{ marginTop: 12, borderTop: '1px solid var(--line)', paddingTop: 12 }}>
              <p className="auth-hint">{t('webauthn.removeHint', { label: removing.label || t('webauthn.defaultLabel') })}</p>
              <label>
                {t('cpw.current')}
                <input
                  type="password"
                  autoComplete="current-password"
                  value={removing.password}
                  onChange={e => setRemoving({ ...removing, password: e.target.value })}
                />
              </label>
              <label>
                {t('webauthn.orCode')}
                <input
                  autoComplete="one-time-code"
                  inputMode="numeric"
                  value={removing.code}
                  onChange={e => setRemoving({ ...removing, code: e.target.value })}
                />
              </label>
              <div>
                <button className="secondary-button danger" type="submit" disabled={busy}>
                  {busy ? t('auth.working') : t('webauthn.confirmRemove')}
                </button>
                <button className="quiet-link" type="button" onClick={() => setRemoving(null)}>{t('common.cancel')}</button>
              </div>
            </form>
          ) : (
            <>
              <label>
                {t('webauthn.nameLabel')}
                <input value={label} placeholder={t('webauthn.nameHint')} onChange={e => setLabel(e.target.value)} />
              </label>
              <div>
                <button className="primary-button" type="button" disabled={busy} onClick={add}>
                  {busy ? t('webauthn.waiting') : t('webauthn.addKey')}
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </section>
  )
}

export default WebAuthnKeys
