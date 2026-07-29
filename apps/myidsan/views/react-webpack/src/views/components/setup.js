import React, { useEffect, useState } from 'react'
import { apiRequest, resultOf } from '../../lib/api'
import { useT } from '@shared'

// First-run setup wizard for the identity server. Every step is a thin form over
// APIs that already exist — the wizard adds sequence and explanation, not new
// capability, and every step is skippable (the same work can always be done later
// from the regular admin pages). Completion is a single server-side flag
// (POST /api/setup/complete), mirroring mymatasan's wizard contract.
const STEP_KEYS = ['welcome', 'app', 'signin', 'admin', 'done']

function randomSecret() {
  const raw = new Uint8Array(24)
  crypto.getRandomValues(raw)
  return btoa(String.fromCharCode(...raw)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export default function SetupWizard({ isSuperadmin, onDone, onToast }) {
  const t = useT()
  const [stepIndex, setStepIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Step 2 state: first relying app.
  const [appForm, setAppForm] = useState({ code: '', title: '', audience: '', baseUrl: '', redirectUri: '', clientSecret: '' })
  const [appDone, setAppDone] = useState(false)
  const [appSecretShown, setAppSecretShown] = useState('')

  // Step 3 state: directory (LDAP/AD) quick-enable.
  const [dirForm, setDirForm] = useState({ enabled: false, host: '', port: 636, useStartTls: false, bindDn: '', bindPassword: '', baseDn: '', displayLabel: '' })
  const [dirDone, setDirDone] = useState(false)

  // Step 4 state: personal superadmin account.
  const [roles, setRoles] = useState([])
  const [adminForm, setAdminForm] = useState({ firstName: '', lastName: '', email: '', password: '' })
  const [adminDone, setAdminDone] = useState(false)

  // Restore-instead-of-configure, offered on the welcome step. Someone rebuilding a lost
  // server should not be walked through registering apps and creating an admin by hand —
  // that is exactly the state a backup already captures. A successful restore marks setup
  // complete server-side, so the wizard does not reappear.
  const [showRestore, setShowRestore] = useState(false)
  const [restoreFile, setRestoreFile] = useState(null)
  const [restorePass, setRestorePass] = useState('')
  const [restoreManifest, setRestoreManifest] = useState(null)
  const [restoreDone, setRestoreDone] = useState(false)

  useEffect(() => {
    apiRequest('/api/access-rbac/roles')
      .then(payload => setRoles(resultOf(payload) || []))
      .catch(() => { /* step 4 shows a hint when the role list is unavailable */ })
  }, [])

  const step = STEP_KEYS[stepIndex]
  const next = () => { setError(''); setStepIndex(index => Math.min(index + 1, STEP_KEYS.length - 1)) }
  const back = () => { setError(''); setStepIndex(index => Math.max(index - 1, 0)) }

  const pickRestoreFile = async event => {
    const picked = event.target.files && event.target.files[0]
    setRestoreManifest(null); setError('')
    if (!picked) { setRestoreFile(null); return }
    const bytes = new Uint8Array(await picked.arrayBuffer())
    let binary = ''
    for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i])
    setRestoreFile({ name: picked.name, base64: btoa(binary) })
  }

  const previewRestore = async () => {
    if (!restoreFile) return
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/backup/preview', {
        method: 'POST',
        body: { dataBase64: restoreFile.base64, passphrase: restorePass }
      })
      setRestoreManifest(resultOf(res))
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const applyRestore = async () => {
    if (!restoreFile || !restoreManifest) return
    setBusy(true); setError('')
    try {
      await apiRequest('/api/backup/restore', {
        method: 'POST',
        body: { dataBase64: restoreFile.base64, passphrase: restorePass, mode: 'replace' }
      })
      // The restore invalidated every session, this one included, and already marked
      // setup complete. Tell the operator to sign in with an account from the backup
      // rather than dropping them into a shell that will 401 on its next request.
      setRestoreDone(true)
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const finish = async () => {
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/setup/complete', { method: 'POST', body: {} })
      onDone()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const registerApp = async event => {
    event.preventDefault()
    if (!appForm.code.trim() || !appForm.audience.trim() || !appForm.redirectUri.trim()) {
      setError(t('setup.appMissingFields'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const secret = appForm.clientSecret.trim() || randomSecret()
      const appRes = resultOf(await apiRequest('/api/app-registry', {
        method: 'POST',
        body: {
          code: appForm.code.trim(),
          title: appForm.title.trim() || appForm.code.trim(),
          description: '',
          baseUrl: appForm.baseUrl.trim(),
          audience: appForm.audience.trim(),
          isActive: true
        }
      }))
      const appId = Number(appRes?.id)
      const cfgRes = resultOf(await apiRequest('/api/app-auth-config', {
        method: 'POST',
        body: {
          appRegistryId: appId,
          clientId: appForm.code.trim(),
          clientSecret: secret,
          authCodeTtlSeconds: 300,
          accessTokenTtlSeconds: 900,
          sessionTtlSeconds: 259200,
          isActive: true
        }
      }))
      await apiRequest('/api/app-redirect-uri', {
        method: 'POST',
        body: {
          appAuthConfigId: Number(cfgRes?.id),
          redirectUri: appForm.redirectUri.trim(),
          isActive: true
        }
      })
      setAppSecretShown(secret)
      setAppDone(true)
      onToast?.(t('setup.appRegistered'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const saveDirectory = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/directory-config', {
        method: 'PUT',
        body: {
          enabled: dirForm.enabled,
          displayLabel: dirForm.displayLabel.trim(),
          host: dirForm.host.trim(),
          port: Number(dirForm.port) || 636,
          useStartTls: dirForm.useStartTls,
          caCertPem: '',
          bindDn: dirForm.bindDn.trim(),
          bindPassword: dirForm.bindPassword,
          baseDn: dirForm.baseDn.trim(),
          userFilter: '',
          groupAttr: '',
          subjectAttr: '',
          authoritative: false
        }
      })
      setDirDone(true)
      onToast?.(t('setup.dirSaved'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const createAdmin = async event => {
    event.preventDefault()
    const superRole = roles.find(role => role.isSuperadmin)
    if (!superRole) {
      setError(t('setup.adminNoRole'))
      return
    }
    if (!adminForm.email.trim() || adminForm.password.length < 8) {
      setError(t('setup.adminMissingFields'))
      return
    }
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/user-credential', {
        method: 'POST',
        body: {
          email: adminForm.email.trim(),
          userpwd: adminForm.password,
          firstName: adminForm.firstName.trim(),
          lastName: adminForm.lastName.trim(),
          userRoleId: superRole.id,
          isActive: true,
          mustChangePassword: false
        }
      })
      setAdminDone(true)
      onToast?.(t('setup.adminCreated'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  if (!isSuperadmin) {
    return null
  }

  return (
    <div className="auth-layout">
      <section className="auth-panel setup-panel">
        <div className="brand-block auth-brand">
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">{t('setup.subtitle')}</div>
          </div>
        </div>

        <div className="setup-progress">
          {STEP_KEYS.map((key, index) => (
            <span key={key} className={index === stepIndex ? 'setup-dot active' : index < stepIndex ? 'setup-dot done' : 'setup-dot'} />
          ))}
        </div>

        {error && <div className="message danger">{error}</div>}

        {step === 'welcome' && restoreDone && (
          <div className="setup-step">
            <h2>{t('setup.restoreDoneTitle')}</h2>
            <div className="message success">{t('setup.restoreDoneBody')}</div>
            <div className="form-actions">
              <button className="primary-button" type="button" onClick={() => window.location.reload()}>
                {t('setup.restoreSignIn')}
              </button>
            </div>
          </div>
        )}

        {step === 'welcome' && !restoreDone && (
          <div className="setup-step">
            <h2>{t('setup.welcomeTitle')}</h2>
            <p>{t('setup.welcomeBody')}</p>
            <p className="cert-hint">{t('setup.welcomePassword')}</p>

            {!showRestore ? (
              <>
                <div className="form-actions">
                  <button className="primary-button" type="button" onClick={next}>{t('setup.start')}</button>
                  <button className="secondary-button" type="button" onClick={finish} disabled={busy}>{t('setup.skipAll')}</button>
                </div>
                {/* Offered here rather than as a wizard step: someone rebuilding a lost
                    server should not walk through configuring it by hand first. */}
                <p className="cert-hint">
                  {t('setup.restorePrompt')}{' '}
                  <button className="link-button" type="button" onClick={() => setShowRestore(true)}>
                    {t('setup.restoreLink')}
                  </button>
                </p>
              </>
            ) : (
              <div className="setup-restore">
                <h3>{t('setup.restoreTitle')}</h3>
                <p>{t('setup.restoreBody')}</p>
                <label>
                  {t('setup.restoreFile')}
                  <input type="file" accept=".idbackup" onChange={pickRestoreFile} disabled={busy} />
                </label>
                {restoreFile && (
                  <label>
                    {t('setup.restorePassphrase')}
                    <input
                      type="password"
                      autoComplete="off"
                      value={restorePass}
                      onChange={event => setRestorePass(event.target.value)}
                    />
                  </label>
                )}
                {restoreManifest && (
                  <div className="message success">
                    <div>{t('setup.restoreFound', { version: restoreManifest.appVersion || '?' })}</div>
                    <div className="cert-hint">
                      {(restoreManifest.sections || []).map(s => t(`backup.section.${s}`)).join(', ')}
                    </div>
                  </div>
                )}
                <div className="form-actions">
                  {!restoreManifest ? (
                    <button className="primary-button" type="button" onClick={previewRestore} disabled={busy || !restoreFile || !restorePass}>
                      {t('setup.restoreInspect')}
                    </button>
                  ) : (
                    <button className="primary-button" type="button" onClick={applyRestore} disabled={busy}>
                      {t('setup.restoreApply')}
                    </button>
                  )}
                  <button className="secondary-button" type="button" onClick={() => { setShowRestore(false); setRestoreManifest(null) }} disabled={busy}>
                    {t('setup.back')}
                  </button>
                </div>
              </div>
            )}
          </div>
        )}

        {step === 'app' && (
          <div className="setup-step">
            <h2>{t('setup.appTitle')}</h2>
            <p>{t('setup.appBody')}</p>
            {appDone ? (
              <div className="message success">
                <div>{t('setup.appRegistered')}</div>
                {appSecretShown && <div><strong>{t('setup.appSecretLabel')}:</strong> <code>{appSecretShown}</code></div>}
                <div className="cert-hint">{t('setup.appSecretHint')}</div>
              </div>
            ) : (
              <form className="record-form" onSubmit={registerApp}>
                <div className="two-col">
                  <label>
                    {t('setup.appCode')}
                    <input value={appForm.code} onChange={event => setAppForm({ ...appForm, code: event.target.value })} placeholder="myseliasan" />
                  </label>
                  <label>
                    {t('setup.appAudience')}
                    <input value={appForm.audience} onChange={event => setAppForm({ ...appForm, audience: event.target.value })} placeholder="myseliasan" />
                  </label>
                </div>
                <label>
                  {t('setup.appTitleField')}
                  <input value={appForm.title} onChange={event => setAppForm({ ...appForm, title: event.target.value })} />
                </label>
                <div className="two-col">
                  <label>
                    {t('setup.appBaseUrl')}
                    <input value={appForm.baseUrl} onChange={event => setAppForm({ ...appForm, baseUrl: event.target.value })} placeholder="https://app.example.com" />
                  </label>
                  <label>
                    {t('setup.appRedirect')}
                    <input value={appForm.redirectUri} onChange={event => setAppForm({ ...appForm, redirectUri: event.target.value })} placeholder="https://app.example.com/api/auth/callback" />
                  </label>
                </div>
                <label>
                  {t('setup.appSecret')}
                  <input value={appForm.clientSecret} onChange={event => setAppForm({ ...appForm, clientSecret: event.target.value })} placeholder={t('setup.appSecretPlaceholder')} />
                </label>
                <button className="primary-button" type="submit" disabled={busy}>{t('setup.appRegister')}</button>
              </form>
            )}
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={back}>{t('setup.back')}</button>
              <button className="primary-button" type="button" onClick={next}>{appDone ? t('setup.next') : t('setup.skip')}</button>
            </div>
          </div>
        )}

        {step === 'signin' && (
          <div className="setup-step">
            <h2>{t('setup.signinTitle')}</h2>
            <p>{t('setup.signinBody')}</p>
            {dirDone ? (
              <div className="message success">{t('setup.dirSaved')}</div>
            ) : (
              <form className="record-form" onSubmit={saveDirectory}>
                <label className="checkbox-field">
                  <input type="checkbox" checked={dirForm.enabled} onChange={event => setDirForm({ ...dirForm, enabled: event.target.checked })} />
                  {t('setup.dirEnable')}
                </label>
                {dirForm.enabled && (
                  <>
                    <div className="two-col">
                      <label>
                        {t('dir.host')}
                        <input value={dirForm.host} onChange={event => setDirForm({ ...dirForm, host: event.target.value })} placeholder="dc1.corp.local" />
                      </label>
                      <label>
                        {t('dir.port')}
                        <input type="number" value={dirForm.port} onChange={event => setDirForm({ ...dirForm, port: Number(event.target.value) })} />
                      </label>
                    </div>
                    <div className="two-col">
                      <label>
                        {t('dir.bindDn')}
                        <input value={dirForm.bindDn} onChange={event => setDirForm({ ...dirForm, bindDn: event.target.value })} />
                      </label>
                      <label>
                        {t('dir.bindPassword')}
                        <input type="password" value={dirForm.bindPassword} onChange={event => setDirForm({ ...dirForm, bindPassword: event.target.value })} />
                      </label>
                    </div>
                    <label>
                      {t('dir.baseDn')}
                      <input value={dirForm.baseDn} onChange={event => setDirForm({ ...dirForm, baseDn: event.target.value })} placeholder="DC=corp,DC=local" />
                    </label>
                    <label>
                      {t('dir.displayLabel')}
                      <input value={dirForm.displayLabel} onChange={event => setDirForm({ ...dirForm, displayLabel: event.target.value })} placeholder={t('dir.displayLabelHint')} />
                    </label>
                  </>
                )}
                {dirForm.enabled && <button className="primary-button" type="submit" disabled={busy}>{t('setup.dirSave')}</button>}
              </form>
            )}
            <p className="cert-hint">{t('setup.signinMore')}</p>
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={back}>{t('setup.back')}</button>
              <button className="primary-button" type="button" onClick={next}>{dirDone || !dirForm.enabled ? t('setup.next') : t('setup.skip')}</button>
            </div>
          </div>
        )}

        {step === 'admin' && (
          <div className="setup-step">
            <h2>{t('setup.adminTitle')}</h2>
            <p>{t('setup.adminBody')}</p>
            {adminDone ? (
              <div className="message success">{t('setup.adminCreated')}</div>
            ) : (
              <form className="record-form" onSubmit={createAdmin}>
                <div className="two-col">
                  <label>
                    {t('auth.firstName')}
                    <input value={adminForm.firstName} onChange={event => setAdminForm({ ...adminForm, firstName: event.target.value })} />
                  </label>
                  <label>
                    {t('auth.lastName')}
                    <input value={adminForm.lastName} onChange={event => setAdminForm({ ...adminForm, lastName: event.target.value })} />
                  </label>
                </div>
                <div className="two-col">
                  <label>
                    {t('setup.adminEmail')}
                    <input value={adminForm.email} onChange={event => setAdminForm({ ...adminForm, email: event.target.value })} />
                  </label>
                  <label>
                    {t('setup.adminPassword')}
                    <input type="password" value={adminForm.password} onChange={event => setAdminForm({ ...adminForm, password: event.target.value })} />
                  </label>
                </div>
                <button className="primary-button" type="submit" disabled={busy}>{t('setup.adminCreate')}</button>
              </form>
            )}
            {adminDone && <p className="cert-hint">{t('setup.adminRetireHint')}</p>}
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={back}>{t('setup.back')}</button>
              <button className="primary-button" type="button" onClick={next}>{adminDone ? t('setup.next') : t('setup.skip')}</button>
            </div>
          </div>
        )}

        {step === 'done' && (
          <div className="setup-step">
            <h2>{t('setup.doneTitle')}</h2>
            <p>{t('setup.doneBody')}</p>
            <ul className="setup-summary">
              <li>{appDone ? '✓' : '·'} {t('setup.sumApp')}</li>
              <li>{dirDone ? '✓' : '·'} {t('setup.sumDir')}</li>
              <li>{adminDone ? '✓' : '·'} {t('setup.sumAdmin')}</li>
            </ul>
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={back}>{t('setup.back')}</button>
              <button className="primary-button" type="button" onClick={finish} disabled={busy}>{t('setup.finish')}</button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
