import React, { useCallback, useEffect, useState } from 'react'
import { FactoryResetSection, DeploymentPanel, Ico, useT, Tabs } from '@shared'
import { apiRequest, resultOf, apiBase } from '../../lib/api'
import '../styles/settings.css'

// SettingsPage is myidsan's in-app editor for the SAFE SUBSET of config.json (localAuth,
// sso, security, storage, logging) plus a System tab that reports the running build and
// restarts the process. Superadmin-only — the nav entry, the settings API and the restart
// API all gate on it independently.
//
// Ported from myseliasan's equivalent page (the backend was ported the same way), with
// four myidsan-specific differences:
//
//  1. NO file/folder browse. myseliasan pairs its path fields with a server-side picker;
//     myidsan has no /api/settings/fs/browse endpoint on purpose — it is the identity
//     provider, and a directory-enumeration surface is not worth it on the one server whose
//     compromise reaches every other app. Paths are typed.
//  2. The SSO section is the PROVIDER side. myidsan issues tokens, so it exposes the
//     issuer/audience/TTLs, not a relying app's clientId/redirectUri. There is
//     correspondingly no "import an SSO bundle" action — that belongs on the consumer.
//  3. THREE extra policy groups in security: lockout, password policy and MFA policy.
//     These are the reason this page matters more here than elsewhere — they are absent
//     from the shipped config.json entirely and resolve through Effective*() defaults, so
//     before this page an operator could not even SEE that the lockout was on. They are
//     rendered with an explicit "values in force" note for that reason.
//  4. No `pairing` section (that is myseliasan's node fleet).
//
// Every editable block is read by the shared host only at boot, so a save writes
// config.json and reports needsRestart; the page surfaces that as a banner with a Restart
// action rather than leaving the operator to guess.

// Redis database numbers (0–15) as a dropdown, so the field is a pick, not a type-in.
const DB_OPTIONS = Array.from({ length: 16 }, (_, i) => ({ v: i, label: String(i) }))

// SECTIONS drives both the tab bar and each form. A field's `path` is relative to the
// section payload root (the same shape the API returns/accepts); `k` is its i18n label key
// under settings.field.*. Group rows (type 'group') render a subheading only, and open a
// new card; `note: 'effective'` adds the "these are the values in force" callout.
const SECTIONS = [
  {
    id: 'localAuth', icon: 'user', tone: 'blue',
    fields: [
      { path: 'localAuth.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'localAuth.username', k: 'username', type: 'text' },
      { path: 'localAuth.password', k: 'password', type: 'password' }
    ]
  },
  {
    id: 'sso', icon: 'lock', tone: 'violet',
    fields: [
      { path: 'sso.issuer', k: 'issuer', type: 'text' },
      { path: 'sso.audience', k: 'audience', type: 'text' },
      { path: 'sso.internalToken', k: 'internalToken', type: 'password' },
      { type: 'group', g: 'ssoTtl' },
      { path: 'sso.sessionTtlSeconds', k: 'sessionTtl', type: 'number', suggest: [3600, 28800, 86400, 259200] },
      { path: 'sso.accessTokenTtlSeconds', k: 'accessTokenTtl', type: 'number', suggest: [900, 3600, 28800] },
      { path: 'sso.authCodeTtlSeconds', k: 'authCodeTtl', type: 'number', suggest: [60, 120, 300] },
      { path: 'sso.policyCacheTtlSeconds', k: 'policyCacheTtl', type: 'number' }
    ]
  },
  {
    id: 'security', icon: 'shield', tone: 'amber',
    fields: [
      { path: 'jwt.secret', k: 'jwtSecret', type: 'password' },
      { path: 'allowOrigins', k: 'allowOrigins', type: 'text' },
      { type: 'group', g: 'tls' },
      { path: 'tls.certPath', k: 'certPath', type: 'text' },
      { path: 'tls.keyPath', k: 'keyPath', type: 'text' },
      { type: 'group', g: 'securityHeaders' },
      { path: 'securityHeaders.contentSecurityPolicy', k: 'csp', type: 'textarea' },
      // --- the three identity policies (myidsan-specific) ---
      { type: 'group', g: 'loginSecurity', note: 'effective' },
      { path: 'loginSecurity.enabled', k: 'lockoutEnabled', type: 'checkbox' },
      { path: 'loginSecurity.maxAttempts', k: 'maxAttempts', type: 'number', suggest: [5, 10] },
      { path: 'loginSecurity.windowSeconds', k: 'lockoutWindow', type: 'number' },
      { path: 'loginSecurity.lockoutSeconds', k: 'lockoutSeconds', type: 'number' },
      { path: 'loginSecurity.lockoutMaxSeconds', k: 'lockoutMaxSeconds', type: 'number' },
      { path: 'loginSecurity.failedDelayMs', k: 'failedDelayMs', type: 'number' },
      { path: 'loginSecurity.notifyOnLockout', k: 'notifyOnLockout', type: 'checkbox' },
      { type: 'group', g: 'passwordPolicy', note: 'effective' },
      { path: 'passwordPolicy.minLength', k: 'minLength', type: 'number', suggest: [12, 14, 16] },
      { path: 'passwordPolicy.requireUpper', k: 'requireUpper', type: 'checkbox' },
      { path: 'passwordPolicy.requireLower', k: 'requireLower', type: 'checkbox' },
      { path: 'passwordPolicy.requireDigit', k: 'requireDigit', type: 'checkbox' },
      { path: 'passwordPolicy.requireSymbol', k: 'requireSymbol', type: 'checkbox' },
      { path: 'passwordPolicy.blockCommon', k: 'blockCommon', type: 'checkbox' },
      { type: 'group', g: 'mfa', note: 'effective' },
      { path: 'mfa.policy', k: 'mfaPolicy', type: 'select', options: [
        { v: 'off', labelKey: 'settings.opt.mfa.off' },
        { v: 'optional', labelKey: 'settings.opt.mfa.optional' },
        { v: 'required', labelKey: 'settings.opt.mfa.required' }
      ] },
      { path: 'mfa.applyToDirectory', k: 'applyToDirectory', type: 'checkbox' },
      // Read-only: the save path deliberately does not write this leaf (see
      // services/settings_apply.go — applyToConfig sets policy + applyToDirectory only),
      // so offering an editor for it would be a control that appears to work and does not.
      // It still round-trips untouched, so saving this section never clears it.
      { path: 'mfa.requiredRoleIds', k: 'requiredRoleIds', type: 'readonly-list' },
      { type: 'group', g: 'rateLimit' },
      { path: 'rateLimit.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.defaultWindowSeconds', k: 'defaultWindow', type: 'number' },
      { type: 'group', g: 'devOnly' },
      { path: 'rateLimit.devOnly.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.devOnly.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.devOnly.windowSeconds', k: 'windowSeconds', type: 'number' },
      { type: 'group', g: 'authOnly' },
      { path: 'rateLimit.authOnly.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.authOnly.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.authOnly.windowSeconds', k: 'windowSeconds', type: 'number' },
      { type: 'group', g: 'public' },
      { path: 'rateLimit.public.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'rateLimit.public.requests', k: 'requests', type: 'number' },
      { path: 'rateLimit.public.windowSeconds', k: 'windowSeconds', type: 'number' }
    ]
  },
  {
    id: 'storage', icon: 'server', tone: 'steel',
    fields: [
      { type: 'group', g: 'fileStorage' },
      { path: 'fileStorage.path', k: 'path', type: 'text' },
      { path: 'fileStorage.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'fileStorage.cleanup.frequencySeconds', k: 'frequencySeconds', type: 'number' },
      { path: 'fileStorage.cleanup.batchSize', k: 'batchSize', type: 'number' },
      { type: 'group', g: 'cache' },
      { path: 'cache.provider', k: 'provider', type: 'select', options: [
        { v: 'default', labelKey: 'settings.opt.provider.default' },
        { v: 'redis', labelKey: 'settings.opt.provider.redis' }
      ] },
      { path: 'cache.ttlSeconds', k: 'ttlSeconds', type: 'number' },
      { path: 'cache.keyPrefix', k: 'keyPrefix', type: 'text' },
      // The Redis connection fields only matter when the provider is Redis, so the whole
      // card is hidden otherwise and carries a live connection Test.
      { type: 'group', g: 'redis', when: f => getAt(f, 'cache.provider') === 'redis', action: 'testCache' },
      { path: 'cache.redis.address', k: 'address', type: 'text', suggest: ['localhost:6379'] },
      { path: 'cache.redis.password', k: 'password', type: 'password' },
      { path: 'cache.redis.db', k: 'db', type: 'select', numeric: true, options: DB_OPTIONS },
      { path: 'cache.redis.useTls', k: 'useTls', type: 'checkbox' },
      { path: 'cache.redis.connectTimeoutMs', k: 'connectTimeout', type: 'number' },
      { path: 'cache.redis.operationTimeoutMs', k: 'operationTimeout', type: 'number' }
    ]
  },
  {
    id: 'logging', icon: 'list', tone: 'steel',
    fields: [
      { type: 'group', g: 'logging' },
      { path: 'logging.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'logging.path', k: 'path', type: 'text' },
      { path: 'logging.maxLineBytes', k: 'maxLineBytes', type: 'number' },
      { path: 'logging.maxFileSizeMb', k: 'maxFileSizeMb', type: 'number' },
      { path: 'logging.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'logging.cleanup.maxRetentionDays', k: 'maxRetentionDays', type: 'number' },
      { path: 'logging.cleanup.frequencyMinutes', k: 'frequencyMinutes', type: 'number' },
      { type: 'group', g: 'apiLog' },
      { path: 'apiLog.cleanup.enabled', k: 'cleanupEnabled', type: 'checkbox' },
      { path: 'apiLog.cleanup.maxRetentionDays', k: 'maxRetentionDays', type: 'number' },
      { path: 'apiLog.cleanup.frequencyMinutes', k: 'frequencyMinutes', type: 'number' },
      { type: 'group', g: 'telemetry' },
      { path: 'telemetry.enabled', k: 'enabled', type: 'checkbox' },
      { path: 'telemetry.prometheus.enabled', k: 'prometheusEnabled', type: 'checkbox' },
      { path: 'telemetry.prometheus.metricsPath', k: 'metricsPath', type: 'text', suggest: ['/metrics'] },
      { path: 'telemetry.prometheus.apiDurationThresholdMs', k: 'apiDurationThreshold', type: 'number' }
    ]
  }
]

// --- api adapter -------------------------------------------------------------
// myidsan's apiRequest THROWS on a non-2xx and returns the envelope; this page was ported
// from a codebase whose helper returns {ok, body, message}. Adapt once here rather than
// wrapping every call site in try/catch.
//
// The unwrap is the same defensive chain @shared/AppFooter uses, because the endpoints this
// page reads do NOT agree on a shape: the settings routes answer the standard
// {result: …} envelope, while the probes on the System tab (/api/version, /health, /ready)
// are host-level handlers that answer a bare object — and /health may not be JSON at all,
// in which case apiRequest yields null and ok-ness is the whole signal.
// The operator checklist for myidsan: short, because this app is a database, a Redis and
// however many stateless copies you like — no fleet listeners to pass through, no uploads
// to share. Duplicated from setup.js rather than imported to keep the wizard and the
// settings page from importing each other.
function settingsDeploymentSteps(t) {
  return [
    t('setup.deployLbHttps'),
    t('setup.deployKeyFile'),
    t('setup.deployVipUrls'),
    t('setup.deploySameConfig'),
  ]
}

async function call(path, options) {
  try {
    const payload = await apiRequest(path, options)
    const body = (payload && payload.data && payload.data.result) ?? resultOf(payload) ?? payload
    return { ok: true, body }
  } catch (err) {
    return { ok: false, message: err.message }
  }
}

// --- nested value helpers (immutable) ---------------------------------------
function getAt(obj, path) {
  return path.split('.').reduce((o, k) => (o == null ? undefined : o[k]), obj)
}
function setAt(obj, path, value) {
  const parts = path.split('.')
  const root = { ...(obj || {}) }
  let cur = root
  for (let i = 0; i < parts.length - 1; i += 1) {
    cur[parts[i]] = { ...(cur[parts[i]] || {}) }
    cur = cur[parts[i]]
  }
  cur[parts[parts.length - 1]] = value
  return root
}

export function SettingsPage({ isSuperadmin, onToast }) {
  const t = useT()
  const [active, setActive] = useState('localAuth')
  const [values, setValues] = useState({}) // section id -> loaded payload
  const [form, setForm] = useState(null) // active section's working copy
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [restartNeeded, setRestartNeeded] = useState(false)
  const [restarting, setRestarting] = useState(false)

  const loadAll = useCallback(async () => {
    setLoading(true)
    const r = await call('/api/settings')
    if (r.ok && r.body && r.body.values) setValues(r.body.values)
    setLoading(false)
  }, [])
  useEffect(() => { loadAll() }, [loadAll])

  // Re-seed the working copy whenever the active tab or the loaded values change.
  useEffect(() => {
    if (active === 'system') { setForm(null); return }
    setForm(values[active] ? JSON.parse(JSON.stringify(values[active])) : null)
  }, [active, values])

  const save = useCallback(async () => {
    if (!form) return
    setBusy(true)
    const r = await call(`/api/settings/${active}`, { method: 'PUT', body: form })
    setBusy(false)
    if (!r.ok) {
      onToast && onToast(r.message || t('settings.saveFailed'), 'error')
      return
    }
    // Refresh the masked view (so secret placeholders reflect the new "set" state).
    await loadAll()
    if (r.body && r.body.needsRestart) setRestartNeeded(true)
    onToast && onToast(t('settings.saved'), 'success')
  }, [active, form, loadAll, onToast, t])

  const reset = useCallback(async () => {
    if (!window.confirm(t('settings.resetConfirm'))) return
    setBusy(true)
    const r = await call(`/api/settings/${active}/reset`, { method: 'POST' })
    setBusy(false)
    if (!r.ok) {
      onToast && onToast(r.message || t('settings.saveFailed'), 'error')
      return
    }
    await loadAll()
    if (r.body && r.body.needsRestart) setRestartNeeded(true)
    onToast && onToast(t('settings.resetDone'), 'success')
  }, [active, loadAll, onToast, t])

  // restart POSTs the relaunch, then polls the public health endpoint until the fresh
  // process answers, then reloads the page.
  const restart = useCallback(async () => {
    setRestarting(true)
    onToast && onToast(t('settings.restarting'), 'info')
    await call('/api/system/restart', { method: 'POST' })
    const deadline = Date.now() + 120000
    const tick = async () => {
      if (Date.now() > deadline) { window.location.reload(); return }
      try {
        const res = await fetch(`${apiBase}/api/health`, { credentials: 'same-origin' })
        if (res.ok) { window.location.reload(); return }
      } catch (_) { /* server still down */ }
      window.setTimeout(tick, 1500)
    }
    window.setTimeout(tick, 2500)
  }, [onToast, t])

  if (!isSuperadmin) {
    return <div className="settings-panel"><p className="settings-hint">{t('settings.superadminOnly')}</p></div>
  }

  const tabs = SECTIONS.map(s => ({ id: s.id, label: t(`settings.sec.${s.id}.title`) }))
    .concat([{ id: 'system', label: t('settings.sec.system.title') }])
  const activeSection = SECTIONS.find(s => s.id === active)
  // The Save button is enabled only when the working copy differs from what was loaded.
  // Both are the same masked shape (secrets blank + "<field>Set" flags), so a plain deep
  // compare is accurate: touching any field makes them differ, saving reloads and re-seeds
  // the form so it goes clean again.
  const dirty = form && values[active] ? JSON.stringify(form) !== JSON.stringify(values[active]) : false

  return (
    <div className="settings-panel">
      <div className="settings-header">
        <h2>{t('settings.title')}</h2>
      </div>
      <p className="settings-hint">{t('settings.hint')}</p>

      {restartNeeded ? (
        <div className="settings-restart-banner" role="alert">
          <span><Ico n="warning" sz={16} /> {t('settings.restartRequired')}</span>
          <button type="button" className="settings-btn settings-btn-primary" onClick={restart} disabled={restarting}>
            <Ico n={restarting ? 'reload' : 'refresh'} sz={15} />
            <span>{restarting ? t('settings.restarting') : t('settings.restartNow')}</span>
          </button>
        </div>
      ) : null}

      <Tabs tabs={tabs} active={active} onChange={setActive} ariaLabel={t('settings.title')} />

      {loading ? (
        <div className="settings-loading"><Ico n="reload" sz={18} /><span>{t('common.loading')}</span></div>
      ) : active === 'system' ? (
        <SystemTab t={t} onRestart={restart} restarting={restarting} onToast={onToast} />
      ) : !form ? (
        <p className="settings-hint settings-hint--error">{t('settings.loadFailed')}</p>
      ) : (
        <div className="settings-body">
          <SectionHero section={activeSection} t={t} />
          <div className="settings-cards">
            {toGroups(activeSection.fields, t).filter(g => !g.when || g.when(form)).map((g, i) => (
              <section key={g.title || `g${i}`} className="settings-card">
                {g.action === 'testCache' ? (
                  <div className="settings-card-head">
                    <h3 className="settings-card-title settings-card-title--flush">{g.title}</h3>
                    <CacheTestButton form={form} t={t} onToast={onToast} />
                  </div>
                ) : g.title ? <h3 className="settings-card-title">{g.title}</h3> : null}
                {g.note === 'effective' ? (
                  <p className="settings-policy-note">
                    <Ico n="info" sz={14} />
                    <span>{t('settings.effectiveNote')}</span>
                  </p>
                ) : null}
                <div className="settings-grid">
                  {g.fields.map(f => <Field key={f.path} field={f} form={form} setForm={setForm} t={t} />)}
                </div>
              </section>
            ))}
          </div>
          <div className="settings-actions">
            <button type="button" className="settings-btn settings-btn-primary" onClick={save} disabled={busy || !dirty}>
              <Ico n="save" sz={15} /><span>{t('settings.save')}</span>
            </button>
            <button type="button" className="settings-btn" onClick={reset} disabled={busy}>
              <Ico n="reload" sz={15} /><span>{t('settings.reset')}</span>
            </button>
            {dirty ? <span className="settings-dirty">{t('settings.unsaved')}</span> : null}
          </div>
        </div>
      )}
    </div>
  )
}

// toGroups folds a flat field list into cards, opening a new card at each 'group' marker.
// Fields before the first marker land in an untitled lead card.
function toGroups(fields, t) {
  const groups = []
  let cur = { title: null, fields: [] }
  for (const f of fields) {
    if (f.type === 'group') {
      if (cur.fields.length || cur.title) groups.push(cur)
      cur = { title: t(`settings.group.${f.g}`), when: f.when, action: f.action, note: f.note, fields: [] }
    } else {
      cur.fields.push(f)
    }
  }
  if (cur.fields.length || cur.title) groups.push(cur)
  return groups
}

// CacheTestButton pings Redis with the values currently in the form (a blank password
// falls back to the stored one, resolved server-side). It shows an inline result pill and
// toasts, so an operator can verify connectivity before saving.
function CacheTestButton({ form, t, onToast }) {
  const [state, setState] = useState({ status: 'idle', msg: '' })
  const run = useCallback(async () => {
    setState({ status: 'testing', msg: '' })
    const redis = getAt(form, 'cache.redis') || {}
    const r = await call('/api/settings/cache/test', { method: 'POST', body: redis })
    if (r.ok) {
      setState({ status: 'ok', msg: '' })
      onToast && onToast(t('settings.cacheTest.okToast'), 'success')
    } else {
      const msg = r.message || t('settings.cacheTest.fail')
      setState({ status: 'fail', msg })
      onToast && onToast(t('settings.cacheTest.failToast', { msg }), 'error')
    }
  }, [form, onToast, t])

  return (
    <div className="settings-cache-test">
      {state.status === 'ok' ? (
        <span className="settings-status-pill ok"><span className="settings-status-dot" />{t('settings.cacheTest.ok')}</span>
      ) : null}
      {state.status === 'fail' ? (
        <span className="settings-status-pill bad" title={state.msg}><span className="settings-status-dot" />{t('settings.cacheTest.fail')}</span>
      ) : null}
      <button type="button" className="settings-btn settings-btn-quiet" onClick={run} disabled={state.status === 'testing'}>
        <Ico n={state.status === 'testing' ? 'reload' : 'zap'} sz={14} />
        <span>{state.status === 'testing' ? t('settings.cacheTest.testing') : t('settings.cacheTest.test')}</span>
      </button>
    </div>
  )
}

// SectionHero is the tinted banner atop each section: an icon chip in the section's tone,
// the section title, and its one-line description.
function SectionHero({ section, t }) {
  return (
    <div className={`settings-hero tone-${section.tone}`}>
      <span className="settings-hero-ico"><Ico n={section.icon} sz={22} /></span>
      <div className="settings-hero-text">
        <h3>{t(`settings.sec.${section.id}.title`)}</h3>
        <p>{t(`settings.sec.${section.id}.desc`)}</p>
      </div>
    </div>
  )
}

// InfoTip is an (i) affordance whose hint appears on hover/focus. It keeps the field's
// helper text OUT of the layout flow, so every input is the same height and rows align.
function InfoTip({ text }) {
  return (
    <span className="settings-info">
      <button type="button" className="settings-info-btn" aria-label={text}>
        <Ico n="info" sz={13} />
      </button>
      <span className="settings-info-bubble" role="tooltip">{text}</span>
    </span>
  )
}

function FieldLabel({ text, info }) {
  return (
    <span className="settings-field-label">
      <span className="settings-field-label-text">{text}</span>
      {info ? <InfoTip text={info} /> : null}
    </span>
  )
}

function Field({ field, form, setForm, t }) {
  const val = getAt(form, field.path)
  const label = t(`settings.field.${field.k}`)
  // Per-field help lives at settings.info.<path>. t() echoes the key when absent, so an
  // unmatched key means "no help". Secret fields also carry the keep-blank note.
  const helpKey = `settings.info.${field.path}`
  let help = t(helpKey)
  if (help === helpKey) help = ''
  const info = field.type === 'password'
    ? [help, t('settings.secretHint')].filter(Boolean).join(' ')
    : help
  const onChange = v => setForm(setAt(form, field.path, v))

  if (field.type === 'checkbox') {
    return (
      <label className={`settings-toggle${val ? ' on' : ''}`}>
        <input type="checkbox" checked={!!val} onChange={e => onChange(e.target.checked)} />
        <span className="settings-toggle-track" aria-hidden="true"><span className="settings-toggle-thumb" /></span>
        <span className="settings-toggle-label">{label}</span>
        {info ? <InfoTip text={info} /> : null}
      </label>
    )
  }
  // readonly-list renders an array leaf the save path does not write, so it is shown
  // rather than edited. The value still round-trips in the payload untouched.
  if (field.type === 'readonly-list') {
    const list = Array.isArray(val) ? val : []
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <p className="settings-field-note">
          {list.length ? list.join(', ') : t('settings.emptyList')}
        </p>
      </div>
    )
  }
  if (field.type === 'password') {
    const isSet = !!getAt(form, `${field.path}Set`)
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <input
          type="password"
          value={val || ''}
          placeholder={isSet ? '••••••••' : t('settings.notSet')}
          autoComplete="new-password"
          onChange={e => onChange(e.target.value)}
        />
      </div>
    )
  }
  if (field.type === 'select') {
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <select value={val == null ? '' : String(val)} onChange={e => onChange(field.numeric ? Number(e.target.value) : e.target.value)}>
          {(field.options || []).map(o => <option key={o.v} value={o.v}>{o.label != null ? o.label : t(o.labelKey)}</option>)}
        </select>
      </div>
    )
  }
  if (field.type === 'textarea') {
    return (
      <div className="settings-field settings-field-wide">
        <FieldLabel text={label} info={info} />
        <textarea value={val || ''} rows={3} onChange={e => onChange(e.target.value)} />
      </div>
    )
  }
  const listId = field.suggest ? `sug-${field.path}` : undefined
  const datalist = field.suggest ? (
    <datalist id={listId}>{field.suggest.map(s => <option key={s} value={s} />)}</datalist>
  ) : null
  if (field.type === 'number') {
    return (
      <div className="settings-field">
        <FieldLabel text={label} info={info} />
        <input
          type="number"
          list={listId}
          value={val === undefined || val === null ? '' : val}
          onChange={e => onChange(e.target.value === '' ? 0 : Number(e.target.value))}
        />
        {datalist}
      </div>
    )
  }
  return (
    <div className="settings-field">
      <FieldLabel text={label} info={info} />
      <input type="text" list={listId} value={val || ''} onChange={e => onChange(e.target.value)} />
      {datalist}
    </div>
  )
}

// SystemTab shows the running build and live service health by polling the public
// version/health/liveness/readiness endpoints, plus the process-restart control.
// Read-only except for Restart.
function SystemTab({ t, onRestart, restarting, onToast }) {
  const resetApi = useCallback(
    (path, opts) => call(path, opts && typeof opts.body === 'string' ? { ...opts, body: JSON.parse(opts.body) } : opts),
    [],
  )
  const [ver, setVer] = useState(null)       // /api/version body, or null
  const [api200, setApi200] = useState(null) // /api/health reachable
  const [live, setLive] = useState(null)     // /health liveness
  const [ready, setReady] = useState(null)   // /ready body {ok, db, cache, …}
  const [busy, setBusy] = useState(false)
  const [checkedAt, setCheckedAt] = useState(null)

  const load = useCallback(async () => {
    setBusy(true)
    const [v, h, l, r] = await Promise.all([
      call('/api/version'), call('/api/health'), call('/health'), call('/ready')
    ])
    setVer(v.ok && v.body && typeof v.body === 'object' ? v.body : null)
    setApi200(!!h.ok)
    setLive(!!l.ok)
    // A readiness probe that answered 200 but not JSON (a bare "ok" body) is still ready —
    // fall back to the status rather than reporting the server down on a parse detail.
    setReady(r.ok ? (r.body && typeof r.body === 'object' ? r.body : { ok: true }) : null)
    setCheckedAt(new Date())
    setBusy(false)
  }, [])
  useEffect(() => { load() }, [load])

  // Advisory readiness keys beyond the ok verdict (db, cache, …) so operators see detail.
  const readyExtras = ready ? Object.entries(ready).filter(([k]) => k !== 'ok') : []

  return (
    <div className="settings-body">
      <div className="settings-hero tone-steel">
        <span className="settings-hero-ico"><Ico n="monitor" sz={22} /></span>
        <div className="settings-hero-text">
          <h3>{t('settings.sec.system.title')}</h3>
          <p>{t('settings.sec.system.desc')}</p>
        </div>
      </div>

      <div className="settings-cards">
        {/* Software version */}
        <section className="settings-card">
          <div className="settings-card-head">
            <h3 className="settings-card-title settings-card-title--flush">{t('settings.system.software')}</h3>
            <button type="button" className="settings-btn settings-btn-quiet" onClick={load} disabled={busy}>
              <Ico n="reload" sz={14} /><span>{t('settings.system.refresh')}</span>
            </button>
          </div>
          {ver ? (
            <div className="settings-metrics">
              <MetricTile label={t('settings.system.application')} value={ver.app || '—'} sub={ver.appVersion ? `v${ver.appVersion}` : null} />
              <MetricTile label={t('settings.system.core')} value={ver.coreVersion ? `v${ver.coreVersion}` : '—'} />
              {ver.commit ? <MetricTile label={t('settings.system.commit')} value={String(ver.commit).slice(0, 12)} mono /> : null}
              {ver.updatedAt ? <MetricTile label={t('settings.system.built')} value={String(ver.updatedAt)} /> : null}
            </div>
          ) : (
            <p className="settings-field-note">{t('settings.system.versionUnavailable')}</p>
          )}
          {checkedAt ? <div className="settings-checked-at">{t('settings.system.checkedAt', { time: checkedAt.toLocaleTimeString() })}</div> : null}
        </section>

        {/* Service health */}
        <section className="settings-card">
          <h3 className="settings-card-title">{t('settings.system.health')}</h3>
          <div className="settings-health">
            <HealthRow label={t('settings.system.api')} ok={api200} t={t} />
            <HealthRow label={t('settings.system.liveness')} ok={live} t={t} />
            <HealthRow label={t('settings.system.readiness')} ok={!!(ready && ready.ok)} t={t} />
            {readyExtras.map(([k, v]) => (
              <HealthRow key={k} label={k} ok={v === true || v === 'ok' || v === 'up'} note={typeof v === 'string' ? v : null} t={t} />
            ))}
          </div>
        </section>

        {/* Restart */}
        <section className="settings-card settings-system-row">
          <div className="settings-system-meta">
            <span className="settings-system-ico"><Ico n="refresh" sz={18} /></span>
            <div>
              <div className="settings-system-label">{t('settings.system.restart')}</div>
              <div className="settings-field-note">{t('settings.system.restartDesc')}</div>
            </div>
          </div>
          <button type="button" className="settings-btn settings-btn-primary" onClick={onRestart} disabled={restarting}>
            <Ico n={restarting ? 'reload' : 'refresh'} sz={15} />
            <span>{restarting ? t('settings.restarting') : t('settings.restartNow')}</span>
          </button>
        </section>

        {/* Deployment shape + cluster-readiness checklist. Reachable here as well as in
            the first-run wizard, because an install grows into a second instance long
            after setup, and because this is the only place the encryption key fingerprint
            is shown — the value an operator compares between machines. */}
        <section className="settings-section">
          <h3>{t('settings.deploymentTitle')}</h3>
          <p className="settings-hint">{t('settings.deploymentHint')}</p>
          <DeploymentPanel
            api={resetApi}
            appLabel="MyIDSan"
            operatorSteps={settingsDeploymentSteps(t)}
            onToast={onToast}
          />
        </section>

        {/* Factory reset. Renders nothing unless bootstrap.allowReset is on, which
            myidsan ships false. resetApi exists because the shared component sends the
            fetch contract -- a JSON STRING body -- while myidsan's apiRequest stringifies
            whatever it is handed, which would double-encode it into a quoted string the
            server cannot decode. */}
        <FactoryResetSection api={resetApi} appLabel="MyIDSan" onToast={onToast} />
      </div>
    </div>
  )
}

function MetricTile({ label, value, sub, mono }) {
  return (
    <div className="settings-metric">
      <div className="settings-metric-label">{label}</div>
      <div className={`settings-metric-value${mono ? ' mono' : ''}`}>{value}</div>
      {sub ? <div className="settings-metric-sub">{sub}</div> : null}
    </div>
  )
}

function HealthRow({ label, ok, note, t }) {
  return (
    <div className="settings-health-row">
      <span className="settings-health-label">{label}</span>
      <span className={`settings-status-pill ${ok ? 'ok' : 'bad'}`}>
        <span className="settings-status-dot" />
        {note || (ok ? t('settings.system.online') : t('settings.system.offline'))}
      </span>
    </div>
  )
}

export default SettingsPage
