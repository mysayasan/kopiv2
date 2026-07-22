import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ACCESS_TIERS,
  apiBase,
  apiRequest,
  clearCookie,
  emptyToZero,
  formatDateTime,
  getCookie,
  pageOf,
  queryString,
  resultOf,
  rowsOf,
  setCookie
} from '../lib/api'
import { Ico, DataTable as ClientDataTable, ToastStack, SideNav, LangProvider, LanguageDropdown, AppFooter, normalizeLang, useT } from '@shared'
import { enBundle, loadLocaleDict } from './i18n'

const ACTIVE_SECTION_COOKIE = 'myidsan_active_section'
const STOCK_SUPERADMIN_EMAIL = 'superadmin'
const TABLE_STATE_PREFIX = 'myidsan_table_'
const TABLE_STATE_VERSION = 1
const THEME_KEY = 'myidsan_theme'
const THEMES = ['light', 'dark', 'contrast']
const THEME_LABELS = { light: 'Light', dark: 'Dark', contrast: 'High contrast' }
const THEME_ICONS = { light: 'sun', dark: 'moon', contrast: 'contrast' }

const dashboardSection = {
  id: 'dashboard',
  label: 'Dashboard',
  group: 'Workspace',
  order: 0,
  tone: 'steel',
  code: 'DA',
  icon: 'monitor',
  paths: []
}

const routeCatalog = [
  // Identity/RBAC management is privilege-escalation-sensitive, so it stays
  // superadminOnly (its APIs are superadmin-gated server-side too): Users (role
  // assignment), Groups, Roles + RBAC (the permission matrix). Apps and Endpoints are
  // ordinary RBAC-governed sections — their APIs are matrix-gated, so a role granted
  // GET on /api/app-registry or /api/endpoint sees the menu and the list.
  { id: 'users', label: 'Users', group: 'Administration', order: 10, tone: 'blue', code: 'US', icon: 'user', paths: ['/api/user-credential'], summary: 'Maintain credentials, profile details, and role assignments.', superadminOnly: true },
  { id: 'groups', label: 'Groups', group: 'Administration', order: 20, tone: 'teal', code: 'GR', icon: 'folder', paths: ['/api/user-group'], summary: 'Organize identity ownership and hierarchy roots.', superadminOnly: true },
  { id: 'roles', label: 'Roles', group: 'Administration', order: 30, tone: 'violet', code: 'RO', icon: 'key', paths: ['/api/access-rbac'], summary: 'Create, edit, and remove accessrbac roles (shared module).', superadminOnly: true },
  { id: 'rbac', label: 'RBAC', group: 'Administration', order: 35, tone: 'green', code: 'RB', icon: 'lock', paths: ['/api/access-rbac'], summary: 'Manage accessrbac roles (shared module). Superadmin bypasses; viewer is read-only.', superadminOnly: true },
  { id: 'apps', label: 'Apps', group: 'Federation', order: 40, tone: 'indigo', code: 'AP', icon: 'grid2', paths: ['/api/app-registry'], summary: 'Manage registered relying apps and audiences.' },
  { id: 'directory', label: 'Directory', group: 'Federation', order: 45, tone: 'amber', code: 'DI', icon: 'key', paths: ['/api/directory-config', '/api/federated-group-mapping'], summary: 'Connect an LDAP/Active Directory server and map groups to roles.' },
  { id: 'endpoints', label: 'Endpoints', group: 'Access Control', order: 50, tone: 'steel', code: 'EP', icon: 'list', paths: ['/api/endpoint'], summary: 'Maintain the protected endpoint catalog.' }
]

const routeCatalogById = routeCatalog.reduce((acc, section) => {
  acc[section.id] = section
  return acc
}, {})

const FILTER_OPERATORS = [
  { value: 1, label: '=' },
  { value: 2, label: '!=' },
  { value: 3, label: '>' },
  { value: 4, label: '<' },
  { value: 5, label: '>=' },
  { value: 6, label: '<=' }
]

const TEXT_FILTER_OPERATORS = FILTER_OPERATORS.filter(operator => [1, 2].includes(operator.value))
const BOOLEAN_FILTER_OPERATORS = FILTER_OPERATORS.filter(operator => [1, 2].includes(operator.value))

const emptyUser = {
  id: 0,
  email: '',
  userpwd: '',
  firstName: '',
  lastName: '',
  picUrl: '',
  userRoleId: 0,
  isActive: true
}

const emptyGroup = {
  id: 0,
  title: '',
  description: '',
  parentId: 0,
  isActive: true
}

const emptyRole = {
  id: 0,
  title: '',
  description: '',
  parentId: 0,
  groupId: 0,
  isActive: true
}

const emptyApp = {
  id: 0,
  code: '',
  title: '',
  description: '',
  baseUrl: '',
  audience: '',
  clientSecret: '',
  isActive: true
}

const emptyEndpoint = {
  id: 0,
  title: '',
  description: '',
  metadata: '',
  appCode: 'myidsan',
  host: '*',
  path: '/api/',
  accessTier: 0,
  isActive: true
}

const emptyRbac = {
  id: 0,
  apiEndpointId: 0,
  userRoleId: 0,
  canGet: true,
  canPost: false,
  canPut: false,
  canDelete: false,
  isActive: true
}

const normalizeUser = data => ({
  ...emptyUser,
  ...data,
  userRoleId: emptyToZero(data?.userRoleId)
})

const normalizeGroup = data => ({
  ...emptyGroup,
  ...data,
  parentId: emptyToZero(data?.parentId)
})

const normalizeRole = data => ({
  ...emptyRole,
  ...data,
  description: typeof data?.description === 'object'
    ? data.description.String || ''
    : data?.description || '',
  parentId: emptyToZero(data?.parentId),
  groupId: emptyToZero(data?.groupId)
})

const normalizeApp = data => ({
  ...emptyApp,
  ...data,
  clientSecret: ''
})

const normalizeEndpoint = data => ({
  ...emptyEndpoint,
  ...data,
  metadata: formatMetadataForEdit(data?.metadata),
  accessTier: emptyToZero(data?.accessTier)
})

const normalizeRbac = data => ({
  ...emptyRbac,
  ...data,
  apiEndpointId: emptyToZero(data?.apiEndpointId),
  userRoleId: emptyToZero(data?.userRoleId)
})

const emptyAccessRole = {
  id: 0,
  name: '',
  description: '',
  isSuperadmin: false,
  builtin: false
}

const normalizeAccessRole = data => ({
  ...emptyAccessRole,
  ...data
})

// BrandMark is myidsan's identity glyph (line-art shield + keyhole) shown inside the
// blue brand tile in the side-nav and on the login screen, replacing the old "ID"
// text so the app has a real logo like myseliasan.
function BrandMark({ size = 24 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3 5 6v5c0 4.4 3 7.6 7 9 4-1.4 7-4.6 7-9V6z" />
      <circle cx="12" cy="10" r="1.7" />
      <path d="M12 11.7V15" />
    </svg>
  )
}

function AppInner({ lang, onLangChange }) {
  const t = useT()
  const [session, setSession] = useState(() => localStorage.getItem('myidsan.session') === 'active')
  const [sessionReady, setSessionReady] = useState(false)
  const [active, setActive] = useState(() => getCookie(ACTIVE_SECTION_COOKIE) || 'dashboard')
  const [accessList, setAccessList] = useState([])
  const [sessionError, setSessionError] = useState('')
  const [handoffPending, setHandoffPending] = useState(false)
  const [currentEmail, setCurrentEmail] = useState('')
  const [stockEmail, setStockEmail] = useState(STOCK_SUPERADMIN_EMAIL)
  const [mustChange, setMustChange] = useState(false)
  const [pending, setPending] = useState(false)
  const [isSuperadmin, setIsSuperadmin] = useState(false)
  const [toasts, setToasts] = useState([])

  const pushToast = useCallback((text, kind = 'info') => {
    if (!text) return
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`
    setToasts(list => [{ id, text, kind }, ...list].slice(0, 5))
  }, [])
  const dismissToast = useCallback(id => setToasts(list => list.filter(t => t.id !== id)), [])
  const [theme, setTheme] = useState(() => {
    try { return localStorage.getItem(THEME_KEY) || 'light' } catch { return 'light' }
  })

  useEffect(() => {
    const root = document.documentElement
    THEMES.forEach(t => root.classList.remove(`theme-${t}`))
    root.classList.add(`theme-${theme}`)
  }, [theme])

  const changeTheme = useCallback(next => {
    setTheme(next)
    try { localStorage.setItem(THEME_KEY, next) } catch { /* ignore */ }
  }, [])

  const refreshHandoff = useCallback(async () => {
    // Best-effort + isolated so a failure never blocks the session.
    try {
      const status = resultOf(await apiRequest('/api/identity-status'))
      setHandoffPending(Boolean(status?.superadminHandoffPending))
      setStockEmail(status?.stockEmail || STOCK_SUPERADMIN_EMAIL)
    } catch {
      setHandoffPending(false)
    }
  }, [])

  const refreshSession = useCallback(async () => {
    try {
      // The shared accessrbac module reports the caller's own role + permission matrix
      // at /me. A superadmin gets a single wildcard access entry (sees every section +
      // CRUD); any other role's menu + actions are computed from its permission rows,
      // so the same matrix that gates the APIs also drives the navigation.
      const me = resultOf(await apiRequest('/api/access-rbac/me'))
      setCurrentEmail(me?.email || '')
      setMustChange(Boolean(me?.mustChangePassword))
      // pending = authenticated but no role assigned yet — gate the whole app behind a
      // clearance screen until an admin grants a role.
      setPending(Boolean(me?.pending))
      // Admin sections are superadmin-only (their APIs are superadmin-gated server-side);
      // the SPA hides them from everyone else.
      setIsSuperadmin(Boolean(me?.isSuperadmin))
      localStorage.setItem('myidsan.session', 'active')
      if (me && me.isSuperadmin) {
        setAccessList([{ path: '', canGet: true, canPost: true, canPut: true, canDelete: true, isActive: true, metadata: '' }])
      } else {
        const perms = me && Array.isArray(me.permissions) ? me.permissions : []
        setAccessList(perms.map(p => ({
          path: p.path,
          canGet: !!p.canGet,
          canPost: !!p.canPost,
          canPut: !!p.canPut,
          canDelete: !!p.canDelete,
          isActive: true,
          metadata: ''
        })))
      }
      setSession(true)
      setSessionError('')
      refreshHandoff()
    } catch (err) {
      localStorage.removeItem('myidsan.session')
      setAccessList([])
      setSession(false)
      setMustChange(false)
      setPending(false)
      setIsSuperadmin(false)
      if (err.status && err.status !== 401 && err.status !== 403) {
        setSessionError(err.message)
      }
    } finally {
      setSessionReady(true)
    }
  }, [refreshHandoff])

  const visibleSections = useMemo(() => {
    return buildVisibleSections(accessList, isSuperadmin)
  }, [accessList, isSuperadmin])

  const navGroups = useMemo(() => groupNavSections(visibleSections), [visibleSections])
  const activeAllowed = visibleSections.some(section => section.id === active)
  const activeKnown = active === 'dashboard' || Boolean(routeCatalogById[active])

  const setActiveSection = useCallback(sectionId => {
    setActive(sectionId)
    setCookie(ACTIVE_SECTION_COOKIE, sectionId)
  }, [])

  useEffect(() => {
    if (!activeKnown) {
      setActiveSection('dashboard')
    }
  }, [activeKnown, setActiveSection])

  useEffect(() => {
    refreshSession()
  }, [refreshSession])

  const handleAuthed = () => {
    localStorage.setItem('myidsan.session', 'active')
    setSession(true)
    setSessionReady(true)
    setSessionError('')
    refreshSession()
  }

  const handleLogout = async () => {
    await apiRequest('/api/login/default/logout', { method: 'POST' })
    localStorage.removeItem('myidsan.session')
    setAccessList([])
    setSession(false)
    setActiveSection('dashboard')
  }

  if (!sessionReady) {
    return <div className="boot-screen">{t('common.checkingSession')}</div>
  }

  if (!session) {
    return <AuthScreen onAuthed={handleAuthed} sessionError={sessionError} />
  }

  if (mustChange) {
    return <ChangePasswordScreen onDone={refreshSession} onLogout={handleLogout} />
  }

  if (pending) {
    return <PendingClearanceScreen email={currentEmail} onRefresh={refreshSession} onLogout={handleLogout} />
  }

  return (
    <div className="app-shell">
      <ToastStack toasts={toasts} onDismiss={dismissToast} />
      <SideNav
        brand={(
          <div className="brand-block">
            <div className="brand-mark"><BrandMark /></div>
            <div>
              <div className="brand-name">MyIDSan</div>
              <div className="brand-subtitle">{t('brand.subtitle')}</div>
            </div>
          </div>
        )}
        groups={navGroups.map(group => ({
          label: t(`grp.${group.label}`),
          items: group.items.map(section => ({
            id: section.id,
            label: t(`nav.${section.id}`),
            icon: section.icon,
            tone: section.tone,
            active: active === section.id,
            onClick: () => setActiveSection(section.id)
          }))
        }))}
        footer={(
          <>
            <ThemeDropdown theme={theme} onThemeChange={changeTheme} />
            <button className="logout-button" onClick={handleLogout} type="button">{t('common.logout')}</button>
          </>
        )}
      />
      <main className="main-workspace">
        <div className="shared-lang-bar"><LanguageDropdown lang={lang} onLang={onLangChange} /></div>
        {handoffPending && (
          <div className="handoff-banner" role="alert">
            <span className="handoff-banner-text">{t('handoff.text')}</span>
            <button type="button" className="handoff-banner-action" onClick={() => setActiveSection('users')}>{t('handoff.goToUsers')}</button>
          </div>
        )}
        {active === 'dashboard' && <Dashboard onNavigate={setActiveSection} sections={visibleSections} />}
        {!activeAllowed && activeKnown && active !== 'dashboard' && <UnauthorizedPage section={routeCatalogById[active]} onNavigate={() => setActiveSection('dashboard')} />}
        {active === 'users' && sectionAllowedById('users', accessList, isSuperadmin) && <UsersPage accessList={accessList} currentEmail={currentEmail} stockEmail={stockEmail} onChanged={refreshHandoff} onToast={pushToast} />}
        {active === 'groups' && sectionAllowedById('groups', accessList, isSuperadmin) && <GroupsPage accessList={accessList} onToast={pushToast} />}
        {active === 'roles' && sectionAllowedById('roles', accessList, isSuperadmin) && <RolesPage accessList={accessList} onToast={pushToast} />}
        {active === 'apps' && sectionAllowedById('apps', accessList, isSuperadmin) && <AppsPage accessList={accessList} onToast={pushToast} />}
        {active === 'directory' && sectionAllowedById('directory', accessList, isSuperadmin) && <DirectoryPage accessList={accessList} onToast={pushToast} />}
        {active === 'endpoints' && sectionAllowedById('endpoints', accessList, isSuperadmin) && <EndpointsPage accessList={accessList} onToast={pushToast} />}
        {active === 'rbac' && sectionAllowedById('rbac', accessList, isSuperadmin) && <RbacPage accessList={accessList} onToast={pushToast} />}
        <AppFooter appName="MyIDSan" apiBase={apiBase} />
      </main>
    </div>
  )
}

// Toast / ToastStack now live in the shared module (@shared) so both control planes
// share one notification design — imported at the top of this file.

// ThemeDropdown mirrors myseliasan's light/dark selector, sitting at the foot of
// the side-nav. The menu opens upward (see .theme-menu) so it is never clipped.
function ThemeDropdown({ theme, onThemeChange }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const wrapRef = useRef(null)

  useEffect(() => {
    if (!open) {
      return undefined
    }
    const onDown = event => {
      if (wrapRef.current && !wrapRef.current.contains(event.target)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div className="theme-drop-wrap" ref={wrapRef}>
      <button
        type="button"
        className={open ? 'theme-toggle active' : 'theme-toggle'}
        onClick={() => setOpen(value => !value)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="btn-icon"><Ico n={THEME_ICONS[theme]} sz={15} /> {t('theme.label')}</span>
        <Ico n="chev-down" sz={13} />
      </button>
      {open && (
        <div className="theme-menu" role="listbox" aria-label={t('theme.select')}>
          {THEMES.map(option => (
            <button
              key={option}
              type="button"
              role="option"
              aria-selected={option === theme}
              className={option === theme ? 'theme-menu-item active' : 'theme-menu-item'}
              onClick={() => { onThemeChange(option); setOpen(false) }}
            >
              <Ico n={THEME_ICONS[option]} sz={15} /> {t(`theme.${option}`)}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ChangePasswordScreen forces the seeded stock superadmin (must-change-password) to
// set its own password before reaching the app — mirrors myseliasan's first-login flow.
function ChangePasswordScreen({ onDone, onLogout }) {
  const t = useT()
  const [form, setForm] = useState({ current: '', next: '', confirm: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async event => {
    event.preventDefault()
    if (form.next.length < 8) {
      setError(t('cpw.min'))
      return
    }
    if (form.next !== form.confirm) {
      setError(t('cpw.noMatch'))
      return
    }
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/login/default/change-password', { method: 'POST', body: { currentPassword: form.current, newPassword: form.next } })
      onDone()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-layout">
      <section className="auth-panel">
        <div className="brand-block auth-brand">
          <div className="brand-mark"><BrandMark /></div>
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">{t('cpw.subtitle')}</div>
          </div>
        </div>
        <div className="message warning">{t('cpw.securityNote')}</div>
        <form className="auth-form" onSubmit={submit}>
          {error && <div className="message danger">{error}</div>}
          <label>
            {t('cpw.current')}
            <input type="password" autoComplete="current-password" value={form.current} onChange={event => setForm({ ...form, current: event.target.value })} />
          </label>
          <label>
            {t('cpw.new')}
            <input type="password" autoComplete="new-password" value={form.next} onChange={event => setForm({ ...form, next: event.target.value })} />
          </label>
          <label>
            {t('cpw.confirm')}
            <input type="password" autoComplete="new-password" value={form.confirm} onChange={event => setForm({ ...form, confirm: event.target.value })} />
          </label>
          <button className="primary-button" disabled={busy} type="submit">{busy ? t('cpw.saving') : t('cpw.change')}</button>
          <button className="quiet-link" onClick={onLogout} type="button">{t('common.logout')}</button>
        </form>
      </section>
    </div>
  )
}

// PendingClearanceScreen gates a freshly-provisioned account (authenticated but with
// no role yet) out of the app until an administrator grants it access. It offers only
// a re-check and a log-out — there is nothing else the account may do.
function PendingClearanceScreen({ email, onRefresh, onLogout }) {
  const t = useT()
  const [busy, setBusy] = useState(false)
  const recheck = async () => {
    setBusy(true)
    try { await onRefresh() } finally { setBusy(false) }
  }
  return (
    <div className="auth-layout">
      <section className="auth-panel">
        <div className="brand-block auth-brand">
          <div className="brand-mark"><BrandMark /></div>
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">{t('pend.subtitle')}</div>
          </div>
        </div>
        <div className="message warning">{t('pend.hint', { email: email ? ` (${email})` : '' })}</div>
        <p className="auth-hint">{t('pend.note')}</p>
        <div className="auth-form">
          <button className="primary-button" disabled={busy} type="button" onClick={recheck}>{busy ? t('pend.checking') : t('pend.checkAgain')}</button>
          <button className="quiet-link" onClick={onLogout} type="button">{t('common.logout')}</button>
        </div>
      </section>
    </div>
  )
}

function AuthScreen({ onAuthed, sessionError }) {
  const t = useT()
  const [mode, setMode] = useState('login')
  const [form, setForm] = useState({
    username: '',
    password: '',
    firstName: '',
    lastName: ''
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [providers, setProviders] = useState([])
  // 'local' or a form-provider key ('ldap' when directory login is enabled).
  const [accountType, setAccountType] = useState('local')

  // Only offer buttons for providers myidsan actually has configured, so a dead
  // provider link never shows (it would just return 'not configured'). `list` is the
  // registry view (key + label + kind); the boolean fields are the legacy
  // two-provider shape, kept as a fallback against an older backend. Kind
  // 'redirect' renders a link; kind 'form' (the directory login) renders an
  // account-type toggle on the credential form itself.
  useEffect(() => {
    let active = true
    apiRequest('/api/login/providers')
      .then(payload => {
        const p = resultOf(payload) || {}
        let list = Array.isArray(p.list) ? p.list.filter(item => item && item.key) : []
        if (!list.length) {
          list = [
            p.google ? { key: 'google', displayName: 'Google', kind: 'redirect' } : null,
            p.github ? { key: 'github', displayName: 'GitHub', kind: 'redirect' } : null
          ].filter(Boolean)
        }
        if (active) setProviders(list)
      })
      .catch(() => { /* leave the buttons off */ })
    return () => { active = false }
  }, [])

  const redirectProviders = providers.filter(p => p.kind !== 'form')
  const formProviders = providers.filter(p => p.kind === 'form')
  const domainSelected = mode === 'login' && formProviders.some(p => p.key === accountType)

  const submit = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      const path = domainSelected
        ? `/api/login/${encodeURIComponent(accountType)}`
        : mode === 'login'
          ? '/api/login/default'
          : '/api/login/default/register'
      const payload = mode === 'login'
        ? { username: form.username, password: form.password }
        : form
      await apiRequest(path, { method: 'POST', body: payload })
      onAuthed()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-layout">
      <section className="auth-panel">
        <div className="brand-block auth-brand">
          <div className="brand-mark"><BrandMark /></div>
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">{t('auth.subAdmin')}</div>
          </div>
        </div>
        <div className="segmented">
          <button className={mode === 'login' ? 'selected' : ''} onClick={() => setMode('login')} type="button">{t('auth.login')}</button>
          <button className={mode === 'register' ? 'selected' : ''} onClick={() => setMode('register')} type="button">{t('auth.register')}</button>
        </div>
        <form className="auth-form" onSubmit={submit}>
          {sessionError && <div className="message warning">{sessionError}</div>}
          {error && <div className="message danger">{error}</div>}
          {mode === 'login' && formProviders.length > 0 && (
            <label>
              {t('auth.accountType')}
              <select value={accountType} onChange={event => setAccountType(event.target.value)}>
                <option value="local">{t('auth.localAccount')}</option>
                {formProviders.map(p => (
                  <option key={p.key} value={p.key}>{p.displayName || p.key}</option>
                ))}
              </select>
            </label>
          )}
          <label>
            {t('auth.username')}
            <input autoComplete="username" value={form.username} onChange={event => setForm({ ...form, username: event.target.value })} />
          </label>
          <label>
            {t('auth.password')}
            <input autoComplete={mode === 'login' ? 'current-password' : 'new-password'} type="password" value={form.password} onChange={event => setForm({ ...form, password: event.target.value })} />
          </label>
          {mode === 'register' && (
            <div className="two-col">
              <label>
                {t('auth.firstName')}
                <input value={form.firstName} onChange={event => setForm({ ...form, firstName: event.target.value })} />
              </label>
              <label>
                {t('auth.lastName')}
                <input value={form.lastName} onChange={event => setForm({ ...form, lastName: event.target.value })} />
              </label>
            </div>
          )}
          <button className="primary-button" disabled={busy} type="submit">{busy ? t('auth.working') : mode === 'login' ? t('auth.login') : t('auth.createAccount')}</button>
          {redirectProviders.length > 0 && (
            <div className="oauth-row">
              {redirectProviders.map(p => (
                <a key={p.key} className="quiet-link" href={`/api/login/${encodeURIComponent(p.key)}`}>{p.displayName || p.key}</a>
              ))}
            </div>
          )}
        </form>
      </section>
    </div>
  )
}

function Dashboard({ onNavigate, sections: visibleSections }) {
  const t = useT()
  const cards = visibleSections
    .filter(section => section.id !== 'dashboard')
    .map(section => ({
      id: section.id,
      title: t(`nav.${section.id}`),
      group: t(`grp.${section.group}`),
      tone: section.tone,
      body: section.summary ? t(`sum.${section.id}`) : dashboardBody(section.id)
    }))

  return (
    <PageFrame title={t('dash.title')} subtitle={t('dash.subtitle')}>
      <div className="dashboard-grid">
        {cards.map(card => (
          <button className={`dashboard-card tone-${card.tone}`} key={card.id} onClick={() => onNavigate(card.id)} type="button">
            <em>{card.group}</em>
            <span>{card.title}</span>
            <small>{card.body}</small>
          </button>
        ))}
        {cards.length === 0 && (
          <div className="empty-state">
            {t('dash.noMenus')}
          </div>
        )}
      </div>
    </PageFrame>
  )
}

function UnauthorizedPage({ section, onNavigate }) {
  const t = useT()
  const title = section ? t(`nav.${section.id}`) : t('unauth.restricted')
  return (
    <PageFrame title={t('unauth.title')} subtitle={t('unauth.subtitle', { title })}>
      <div className="empty-state unauthorized-state">
        <strong>{title}</strong>
        <span>{t('unauth.hidden')}</span>
        <button className="primary-button" onClick={onNavigate} type="button">{t('unauth.goDashboard')}</button>
      </div>
    </PageFrame>
  )
}

// UsersPage mirrors myseliasan's inline user admin: a filterable table with a per-row
// role dropdown, status pill, a "Make superadmin" action, and Enable/Disable — no modal
// editor. Role/active changes PUT the user back (password preserved server-side). The
// stock superadmin (email "superadmin") is flagged and can only be disabled once a real
// superadmin is active, and you can never disable your own account.
function UsersPage({ accessList, currentEmail, stockEmail = STOCK_SUPERADMIN_EMAIL, onChanged, onToast }) {
  const t = useT()
  const [users, setUsers] = useState([])
  const [roles, setRoles] = useState([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const toast = (text, kind = 'success') => { if (onToast) onToast(text, kind) }

  const canEdit = hasEndpointAccess(accessList, '/api/user-credential', 'PUT')

  const load = useCallback(async () => {
    setError('')
    try {
      const [u, r] = await Promise.all([
        apiRequest('/api/user-credential'),
        apiRequest('/api/access-rbac/roles')
      ])
      setUsers((rowsOf(u) || []).map(normalizeUser))
      setRoles(resultOf(r) || [])
    } catch (err) {
      setError(err.message)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const superRole = roles.find(role => role.isSuperadmin)
  const isSuperRole = id => superRole && Number(id) === Number(superRole.id)
  const realSuperadminActive = users.some(u => u.email !== stockEmail && u.isActive && isSuperRole(u.userRoleId))

  const saveUser = async (user, changes, successMsg = t('user.saved')) => {
    setBusy(true)
    try {
      // userpwd is left blank — the service keeps the stored password on update.
      await apiRequest('/api/user-credential', { method: 'PUT', body: { ...user, userpwd: '', ...changes } })
      await load()
      toast(successMsg, 'success')
      if (onChanged) onChanged()
    } catch (err) {
      toast(err.message, 'error')
    } finally {
      setBusy(false)
    }
  }

  const roleLabel = roleId => {
    const id = Number(roleId)
    if (!id) return t('user.noRolePending')
    return roles.find(r => Number(r.id) === id)?.name || `role ${id}`
  }
  const setUserRole = (user, roleId) => {
    const who = user.email || `user ${user.id}`
    saveUser(user, { userRoleId: Number(roleId) }, t('user.roleSetTo', { who, role: roleLabel(roleId) }))
  }
  const toggleActive = user => saveUser(user, { isActive: !user.isActive }, t(user.isActive ? 'user.disabledMsg' : 'user.enabledMsg', { user: user.email || t('user.userWord') }))
  const makeSuperadmin = user => {
    if (!superRole) {
      toast(t('user.noSuperRole'), 'error')
      return
    }
    const label = user.email || `user ${user.id}`
    if (!window.confirm(t('user.confirmSuperadmin', { label }))) {
      return
    }
    saveUser(user, { userRoleId: superRole.id }, t('user.nowSuperadmin', { label }))
  }

  const columns = [
    { key: 'id', label: t('f.id') },
    {
      key: 'email',
      label: t('user.colUser'),
      render: (value, u) => (
        <>
          {value || `#${u.id}`}
          {u.email === stockEmail ? <span className="status-pill off" style={{ marginLeft: 6 }}>{t('user.stock')}</span> : null}
        </>
      )
    },
    {
      key: 'userRoleId',
      label: t('user.colRole'),
      filterable: false,
      render: (_value, u) => (
        <select value={u.userRoleId || 0} onChange={event => setUserRole(u, event.target.value)} disabled={busy || !canEdit}>
          {/* value 0 = no role yet (pending). Without this option the browser would show
              the first role for a role-less user, making them look like a superadmin. */}
          <option value={0}>{t('user.noRolePendingOpt')}</option>
          {roles.map(role => <option key={role.id} value={role.id}>{role.name}</option>)}
        </select>
      )
    },
    { key: 'isActive', label: t('user.colStatus'), render: value => <span className={value ? 'status-pill on' : 'status-pill off'}>{value ? t('user.active') : t('user.inactive')}</span> },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_value, u) => {
        const self = currentEmail && u.email === currentEmail
        const stock = u.email === stockEmail
        const showDisable = !self && (!stock || realSuperadminActive)
        return (
          <div className="row-actions">
            {!stock && !isSuperRole(u.userRoleId)
              ? <button type="button" className="secondary-button" onClick={() => makeSuperadmin(u)} disabled={busy || !canEdit} title={t('user.makeSuperadminTip')}>{t('user.makeSuperadmin')}</button> : null}
            {showDisable
              ? <button type="button" className="secondary-button danger" onClick={() => toggleActive(u)} disabled={busy || !canEdit}>{u.isActive ? t('user.disable') : t('user.enable')}</button> : null}
          </div>
        )
      }
    }
  ]

  return (
    <PageFrame title={t('user.title')} subtitle={t('user.subtitle')}>
      {error && <div className="message danger">{error}</div>}
      <section className="data-region">
        <ClientDataTable rows={users} columns={columns} busy={busy} emptyText={t('user.empty')} />
      </section>
    </PageFrame>
  )
}

function GroupsPage({ accessList }) {
  const t = useT()
  const boolCell = v => t(v ? 'common.yes' : 'common.no')
  return (
    <CrudPage
      accessList={accessList}
      title={t('group.title')}
      subtitle={t('group.subtitle')}
      resource="/api/user-group"
      emptyItem={emptyGroup}
      normalize={normalizeGroup}
      columns={[
        { key: 'id', label: t('f.id') },
        { key: 'title', label: t('f.title') },
        { key: 'description', label: t('f.description') },
        { key: 'parentId', label: t('group.colParent') },
        { key: 'isActive', label: t('f.active'), render: boolCell }
      ]}
      fields={[
        { name: 'title', label: t('f.title'), required: true },
        { name: 'description', label: t('f.description') },
        { name: 'parentId', label: t('group.fParentId'), type: 'number' },
        { name: 'isActive', label: t('f.active'), type: 'checkbox' }
      ]}
    />
  )
}

// Roles manages the shared accessrbac roles (create/edit/remove) — the same surface
// as myseliasan's Roles page. Per-path permissions for each role live on the RBAC
// page. (The old group-scoped /api/user-credential/group/{id} listing had no backing
// route and 404'd, so it was replaced with the accessrbac roles resource.)
function RolesPage({ accessList }) {
  const t = useT()
  const boolCell = v => t(v ? 'common.yes' : 'common.no')
  return (
    <CrudPage
      accessList={accessList}
      title={t('role.title')}
      subtitle={t('role.subtitle')}
      resource="/api/access-rbac/roles"
      updateWithId
      emptyItem={emptyAccessRole}
      normalize={normalizeAccessRole}
      columns={[
        { key: 'id', label: t('f.id') },
        { key: 'name', label: t('f.name') },
        { key: 'description', label: t('f.description') },
        { key: 'isSuperadmin', label: t('role.colSuperadmin'), render: boolCell },
        { key: 'builtin', label: t('role.colBuiltin'), render: boolCell }
      ]}
      fields={[
        { name: 'name', label: t('role.fName'), required: true },
        { name: 'description', label: t('f.description') }
      ]}
    />
  )
}

// downloadText saves text content as a file via a transient object URL — used to
// hand the admin the one-time SSO client key/cert and the CA certificate.
function downloadText(filename, text) {
  const blob = new Blob([text || ''], { type: 'application/x-pem-file' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

// Built-in relying apps that the platform seeds and federation depends on; their
// code/audience must stay canonical, so the UI locks them from rename/delete.
const SYSTEM_APP_CODES = ['myidsan', 'mymatasan', 'myseliasan', 'myiotsan']

// randomSecret returns a URL-safe, high-entropy client secret generated in the
// browser (so the plaintext only ever exists client-side; the server stores a hash).
function randomSecret() {
  const bytes = new Uint8Array(24)
  crypto.getRandomValues(bytes)
  return btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// AppsPage is a master-detail manager: pick (or add) a relying app on the left and
// edit everything for it inline on the right — the registry fields AND its SSO client
// (credentials, PKCE, token lifetimes, redirect URLs). No modal editor and no separate
// "choose an app" step: the selected app is the context.
function AppsPage({ accessList }) {
  const t = useT()
  const [apps, setApps] = useState([])
  const [selectedId, setSelectedId] = useState(null) // null | 'new' | number
  const [error, setError] = useState('')

  const loadApps = useCallback(async selectCode => {
    try {
      const list = rowsOf(await apiRequest('/api/app-registry'))
      setApps(list)
      if (selectCode) {
        const found = list.find(app => app.code === selectCode)
        if (found) {
          setSelectedId(found.id)
        }
      }
    } catch (err) {
      setError(err.message)
    }
  }, [])

  useEffect(() => { loadApps() }, [loadApps])

  const selectedApp = typeof selectedId === 'number' ? apps.find(app => app.id === selectedId) : null

  return (
    <PageFrame title={t('app.title')} subtitle={t('app.subtitle')}>
      <div className="apps-layout">
        <aside className="apps-sidebar">
          <button className="primary-button apps-new" onClick={() => setSelectedId('new')} type="button">{t('app.newApp')}</button>
          <div className="apps-nav">
            {apps.length === 0 && <p className="message">{t('app.noApps')}</p>}
            {apps.map(app => (
              <button
                key={app.id}
                className={selectedId === app.id ? 'apps-nav-item active' : 'apps-nav-item'}
                onClick={() => setSelectedId(app.id)}
                type="button"
              >
                <strong>{app.title || app.code}</strong>
                <span>{app.code}{app.isActive ? '' : t('app.inactiveSuffix')}</span>
              </button>
            ))}
          </div>
        </aside>
        <section className="apps-detail">
          {error && <div className="message danger">{error}</div>}
          {selectedId === 'new' || selectedApp ? (
            <AppDetail
              key={selectedId}
              accessList={accessList}
              app={selectedApp || null}
              onCreated={code => loadApps(code)}
              onSaved={() => loadApps()}
              onDeleted={() => { setSelectedId(null); loadApps() }}
            />
          ) : (
            <div className="empty-state">{t('app.selectPrompt')}</div>
          )}
        </section>
      </div>
    </PageFrame>
  )
}

const emptyAuthConfig = {
  id: 0,
  appRegistryId: 0,
  clientId: '',
  clientSecret: '',
  authCodeTtlSeconds: 300,
  accessTokenTtlSeconds: 900,
  sessionTtlSeconds: 259200,
  refreshTokenTtlSeconds: 0,
  requirePkce: false,
  allowRefreshToken: false,
  isActive: true
}

// AppDetail is the right pane of the Apps master-detail: it edits one relying app
// inline (registry fields) plus its SSO client (auth config + redirect URIs). It is
// remounted (keyed by selection) when a different app is picked, so its forms reset
// cleanly. The client secret is write-only — the API returns only whether one is set.
function AppDetail({ accessList, app, onCreated, onSaved, onDeleted }) {
  const t = useT()
  const isNew = !app
  // Built-in apps power SSO/federation and the seeders key on their codes, so their
  // code/audience are locked and they can't be deleted from the UI.
  const isSystem = !isNew && SYSTEM_APP_CODES.includes(app.code)
  const [appForm, setAppForm] = useState(() => ({
    id: app?.id || 0,
    code: app?.code || '',
    title: app?.title || '',
    description: app?.description || '',
    baseUrl: app?.baseUrl || '',
    audience: app?.audience || '',
    isActive: app ? Boolean(app.isActive) : true
  }))
  const [config, setConfig] = useState(null)
  const [authForm, setAuthForm] = useState(emptyAuthConfig)
  const [uris, setUris] = useState([])
  const [newUri, setNewUri] = useState('')
  const [secretRevealed, setSecretRevealed] = useState(false)
  const [showCertHelp, setShowCertHelp] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const canEdit = hasEndpointAccess(accessList, '/api/app-registry', isNew ? 'POST' : 'PUT')
  const canDelete = hasEndpointAccess(accessList, '/api/app-registry', 'DELETE')
  const canSso = hasEndpointAccess(accessList, '/api/app-auth-config', 'POST') || hasEndpointAccess(accessList, '/api/app-auth-config', 'PUT')

  const generateSecret = () => {
    setAuthForm(current => ({ ...current, clientSecret: randomSecret() }))
    setSecretRevealed(true)
    setNotice(t('app.secretGenerated'))
  }

  const downloadCA = async () => {
    setError('')
    try {
      const res = resultOf(await apiRequest('/api/sso-ca'))
      downloadText('myidsan-ca.crt', res?.caCertPem)
    } catch (err) {
      setError(err.message)
    }
  }

  const generateCert = async () => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const res = resultOf(await apiRequest(`/api/sso-ca/issue/${config.id}`, { method: 'POST' }))
      const base = res?.clientId || 'client'
      downloadText(`${base}-client.crt`, res?.clientCertPem)
      downloadText(`${base}-client.key`, res?.clientKeyPem)
      downloadText('myidsan-ca.crt', res?.caCertPem)
      setNotice(t('app.certIssued'))
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const loadSso = useCallback(async () => {
    if (!app?.id) {
      return
    }
    const cfgQs = queryString({ filters: [{ fieldName: 'appRegistryId', compare: 1, value: Number(app.id) }] })
    const cfgs = rowsOf(await apiRequest(`/api/app-auth-config${cfgQs}`))
    const cfg = cfgs.find(row => Number(row.appRegistryId) === Number(app.id)) || null
    setConfig(cfg)
    setAuthForm(cfg
      ? { ...emptyAuthConfig, ...cfg, clientSecret: '' }
      : { ...emptyAuthConfig, appRegistryId: app.id, clientId: app.code })
    if (cfg) {
      const uriQs = queryString({ filters: [{ fieldName: 'appAuthConfigId', compare: 1, value: Number(cfg.id) }] })
      setUris(rowsOf(await apiRequest(`/api/app-redirect-uri${uriQs}`)))
    } else {
      setUris([])
    }
  }, [app])

  useEffect(() => { loadSso().catch(err => setError(err.message)) }, [loadSso])

  const saveApp = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      if (isNew) {
        await apiRequest('/api/app-registry', { method: 'POST', body: { ...appForm } })
        onCreated(appForm.code)
      } else {
        await apiRequest('/api/app-registry', { method: 'PUT', body: { ...appForm, id: app.id } })
        setNotice(t('app.appSaved'))
        onSaved()
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const deleteApp = async () => {
    if (!window.confirm(t('app.confirmDelete', { name: appForm.title || appForm.code }))) {
      return
    }
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/app-registry/${app.id}`, { method: 'DELETE' })
      onDeleted()
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  const saveConfig = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const isUpdate = Boolean(config && config.id)
      if (!isUpdate && !String(authForm.clientSecret).trim()) {
        throw new Error(t('app.secretRequired'))
      }
      const payload = {
        ...(isUpdate ? { id: config.id } : {}),
        appRegistryId: app.id,
        clientId: authForm.clientId,
        clientSecret: authForm.clientSecret,
        authCodeTtlSeconds: emptyToZero(authForm.authCodeTtlSeconds),
        accessTokenTtlSeconds: emptyToZero(authForm.accessTokenTtlSeconds),
        sessionTtlSeconds: emptyToZero(authForm.sessionTtlSeconds),
        refreshTokenTtlSeconds: emptyToZero(authForm.refreshTokenTtlSeconds),
        requirePkce: Boolean(authForm.requirePkce),
        allowRefreshToken: Boolean(authForm.allowRefreshToken),
        isActive: Boolean(authForm.isActive)
      }
      await apiRequest('/api/app-auth-config', { method: isUpdate ? 'PUT' : 'POST', body: payload })
      setNotice(isUpdate ? t('app.ssoUpdated') : t('app.ssoCreated'))
      await loadSso()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const addUri = async () => {
    if (!config?.id || !newUri.trim()) {
      return
    }
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/app-redirect-uri', { method: 'POST', body: { appAuthConfigId: config.id, redirectUri: newUri.trim(), isActive: true } })
      setNewUri('')
      await loadSso()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const removeUri = async row => {
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/app-redirect-uri/${row.id}`, { method: 'DELETE' })
      await loadSso()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="app-detail">
      <h2>{isNew ? t('app.newApp') : (appForm.title || appForm.code)}</h2>
      {isSystem && <p className="cert-hint">{t('app.systemNote')}</p>}
      {error && <div className="message danger">{error}</div>}
      {notice && <div className="message success">{notice}</div>}

      <form className="record-form" onSubmit={saveApp}>
        <div className="two-col">
          <label>
            {t('app.code')}
            <input value={appForm.code} onChange={event => setAppForm({ ...appForm, code: event.target.value })} required readOnly={isSystem} disabled={isSystem} />
          </label>
          <label>
            {t('app.audience')}
            <input value={appForm.audience} onChange={event => setAppForm({ ...appForm, audience: event.target.value })} required readOnly={isSystem} disabled={isSystem} />
          </label>
        </div>
        <label>
          {t('f.title')}
          <input value={appForm.title} onChange={event => setAppForm({ ...appForm, title: event.target.value })} required />
        </label>
        <label>
          {t('f.description')}
          <input value={appForm.description} onChange={event => setAppForm({ ...appForm, description: event.target.value })} />
        </label>
        <label>
          {t('app.baseUrl')}
          <input value={appForm.baseUrl} onChange={event => setAppForm({ ...appForm, baseUrl: event.target.value })} placeholder="https://app.example.com" />
        </label>
        <label className="checkbox-field">
          <input type="checkbox" checked={Boolean(appForm.isActive)} onChange={event => setAppForm({ ...appForm, isActive: event.target.checked })} />
          {t('f.active')}
        </label>
        <div className="form-actions">
          <button className="primary-button" type="submit" disabled={busy || !canEdit}>{isNew ? t('app.createApp') : t('app.saveApp')}</button>
          {!isNew && !isSystem && <button className="secondary-button danger" type="button" onClick={deleteApp} disabled={busy || !canDelete}>{t('app.deleteApp')}</button>}
        </div>
      </form>

      {isNew ? (
        <p className="message">{t('app.saveFirst')}</p>
      ) : (
        <>
          <hr className="detail-divider" />
          <div className="detail-heading">
            <h3>{t('app.ssoClient')}</h3>
            <span className={config ? 'status-pill on' : 'status-pill off'}>{config ? t('app.ssoConfigured') : t('app.noSsoClient')}</span>
          </div>
          <form className="record-form" onSubmit={saveConfig}>
            <div className="two-col">
              <label>
                {t('app.clientId')}
                <input value={authForm.clientId} onChange={event => setAuthForm({ ...authForm, clientId: event.target.value })} required />
              </label>
              <label>
                {t('app.clientSecret')}
                <div className="secret-row">
                  <input type={secretRevealed ? 'text' : 'password'} value={authForm.clientSecret} onChange={event => setAuthForm({ ...authForm, clientSecret: event.target.value })} placeholder={config?.hasClientSecret ? t('app.secretKeep') : t('app.secretSet')} />
                  <button className="secondary-button" type="button" onClick={generateSecret} disabled={busy} title={t('app.generateTip')}>{t('app.generate')}</button>
                </div>
              </label>
            </div>
            <div className="two-col">
              <label>
                {t('app.authCodeTtl')}
                <input type="number" value={authForm.authCodeTtlSeconds} onChange={event => setAuthForm({ ...authForm, authCodeTtlSeconds: event.target.value })} />
              </label>
              <label>
                {t('app.accessTtl')}
                <input type="number" value={authForm.accessTokenTtlSeconds} onChange={event => setAuthForm({ ...authForm, accessTokenTtlSeconds: event.target.value })} />
              </label>
            </div>
            <div className="two-col">
              <label>
                {t('app.sessionTtl')}
                <input type="number" value={authForm.sessionTtlSeconds} onChange={event => setAuthForm({ ...authForm, sessionTtlSeconds: event.target.value })} />
              </label>
              <label>
                {t('app.refreshTtl')}
                <input type="number" value={authForm.refreshTokenTtlSeconds} onChange={event => setAuthForm({ ...authForm, refreshTokenTtlSeconds: event.target.value })} />
              </label>
            </div>
            <label className="checkbox-field">
              <input type="checkbox" checked={Boolean(authForm.requirePkce)} onChange={event => setAuthForm({ ...authForm, requirePkce: event.target.checked })} />
              {t('app.requirePkce')}
            </label>
            <label className="checkbox-field">
              <input type="checkbox" checked={Boolean(authForm.allowRefreshToken)} onChange={event => setAuthForm({ ...authForm, allowRefreshToken: event.target.checked })} />
              {t('app.allowRefresh')}
            </label>
            <label className="checkbox-field">
              <input type="checkbox" checked={Boolean(authForm.isActive)} onChange={event => setAuthForm({ ...authForm, isActive: event.target.checked })} />
              {t('f.active')}
            </label>
            <button className="primary-button" type="submit" disabled={busy || !canSso}>{config ? t('app.saveSso') : t('app.createSso')}</button>
          </form>

          <div className="sso-uris">
            <h3>{t('app.redirectUris')}</h3>
            {!config ? (
              <p className="message">{t('app.createSsoFirst')}</p>
            ) : (
              <>
                <div className="permission-add">
                  <input value={newUri} onChange={event => setNewUri(event.target.value)} placeholder="https://app.example.com/api/auth/callback" disabled={busy} />
                  <button className="secondary-button" onClick={addUri} type="button" disabled={busy}>{t('app.addUri')}</button>
                </div>
                {uris.length === 0 ? (
                  <p className="message">{t('app.noUris')}</p>
                ) : (
                  <table className="permission-table">
                    <thead>
                      <tr><th>{t('app.colRedirectUri')}</th><th>{t('f.active')}</th><th /></tr>
                    </thead>
                    <tbody>
                      {uris.map(uri => (
                        <tr key={uri.id}>
                          <td><code>{uri.redirectUri}</code></td>
                          <td>{t(uri.isActive ? 'common.yes' : 'common.no')}</td>
                          <td><button className="secondary-button danger" onClick={() => removeUri(uri)} disabled={busy} type="button">{t('common.remove')}</button></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </>
            )}
          </div>

          <div className="sso-certs">
            <div className="detail-heading">
              <h3>{t('app.clientCert')}</h3>
              <button className="info-button" type="button" onClick={() => setShowCertHelp(value => !value)} title={t('app.certHelpTip')} aria-label={t('app.certHelpAria')}>?</button>
            </div>
            {showCertHelp && (
              <div className="cert-help">
                <strong>{t('app.certHelpTitle')}</strong>
                <ol>
                  <li>{t('app.certStep1')}</li>
                  <li>{t('app.certStep2')}</li>
                  <li>{t('app.certStep3')}</li>
                  <li>{t('app.certStep4')}</li>
                </ol>
                <p>{t('app.certNote')}</p>
              </div>
            )}
            {!config ? (
              <p className="message">{t('app.createSsoCertFirst')}</p>
            ) : (
              <>
                <p className="cert-hint">{t('app.certIssuerNote')}</p>
                <div className="form-actions">
                  <button className="secondary-button" type="button" onClick={downloadCA} disabled={busy}>{t('app.downloadCa')}</button>
                  <button className="primary-button" type="button" onClick={generateCert} disabled={busy || !canSso}>{t('app.generateCert')}</button>
                </div>
              </>
            )}
          </div>
        </>
      )}
    </div>
  )
}

function EndpointsPage({ accessList }) {
  const t = useT()
  const boolCell = v => t(v ? 'common.yes' : 'common.no')
  return (
    <CrudPage
      accessList={accessList}
      title={t('ep.title')}
      subtitle={t('ep.subtitle')}
      resource="/api/endpoint"
      emptyItem={emptyEndpoint}
      normalize={normalizeEndpoint}
      columns={[
        { key: 'id', label: t('f.id') },
        { key: 'host', label: t('ep.colHost') },
        { key: 'path', label: t('ep.colPath') },
        { key: 'metadata', label: t('ep.colMenu'), render: menuMetadataLabel },
        { key: 'accessTier', label: t('ep.colTier'), render: tierLabel },
        { key: 'isActive', label: t('f.active'), render: boolCell }
      ]}
      fields={[
        { name: 'title', label: t('f.title'), required: true },
        { name: 'description', label: t('f.description') },
        // Endpoints are app-local: appCode is auto-stamped to this app (myidsan) and
        // hidden from the form. It is kept in the payload only to satisfy the shared
        // API's required field; the catalog never spans apps anymore.
        { name: 'appCode', label: t('ep.fAppCode'), hidden: true },
        { name: 'host', label: t('ep.colHost'), required: true },
        { name: 'path', label: t('ep.colPath'), required: true },
        { name: 'metadata', label: t('ep.fMenuMeta'), type: 'textarea', rows: 8, placeholder: '{"menu":{"enabled":true,"id":"users","label":"Users","group":"Identity","order":10,"summary":"Maintain user access.","tone":"blue"}}' },
        { name: 'accessTier', label: t('ep.fAccessTier'), type: 'select', options: ACCESS_TIERS },
        { name: 'isActive', label: t('f.active'), type: 'checkbox' }
      ]}
    />
  )
}

// RBAC is matrix-only (matches myseliasan): pick a role, then grant per-path/verb
// access. Role records themselves are created/removed on the Roles page. The matrix
// path prefixes drive BOTH API authorization and menu visibility (a role with no GET
// on a section's path neither sees the menu nor can call its APIs). Superadmin bypasses.
function RbacPage() {
  return <RolePermissions />
}

const PERMISSION_VERBS = [['canGet', 'GET'], ['canPost', 'POST'], ['canPut', 'PUT'], ['canDelete', 'DELETE']]

// MENU_SECTIONS maps each nav section to the API path whose GET permission reveals it
// (Roles and RBAC share the accessrbac path). The "Menu access" toggles below grant or
// revoke GET on these paths, which is exactly what drives menu visibility for a role.
const MENU_SECTIONS = [
  { labelKey: 'nav.users', path: '/api/user-credential' },
  { labelKey: 'nav.groups', path: '/api/user-group' },
  { labelKey: 'rbac.menuRolesRbac', path: '/api/access-rbac' },
  { labelKey: 'nav.apps', path: '/api/app-registry' },
  { labelKey: 'nav.directory', path: '/api/directory-config' },
  { labelKey: 'nav.endpoints', path: '/api/endpoint' }
]

// RolePermissions edits the per-role endpoint permission matrix (path prefix ×
// GET/POST/PUT/DELETE). Longest matching prefix wins; no rule means denied. The
// same rows govern menu visibility — granting GET on a section's path reveals it.
function RolePermissions() {
  const t = useT()
  const [roles, setRoles] = useState([])
  const [roleId, setRoleId] = useState(0)
  const [perms, setPerms] = useState([])
  const [path, setPath] = useState('/api')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const loadRoles = useCallback(async () => {
    try {
      const list = resultOf(await apiRequest('/api/access-rbac/roles')) || []
      setRoles(list)
      setRoleId(prev => prev || (list.find(role => !role.isSuperadmin)?.id ?? list[0]?.id ?? 0))
    } catch (err) {
      setError(err.message)
    }
  }, [])

  const loadPerms = useCallback(async rid => {
    if (!rid) {
      setPerms([])
      return
    }
    try {
      const list = resultOf(await apiRequest(`/api/access-rbac/permissions?roleId=${rid}`)) || []
      setPerms(list)
    } catch (err) {
      setError(err.message)
    }
  }, [])

  useEffect(() => { loadRoles() }, [loadRoles])
  useEffect(() => { loadPerms(roleId) }, [roleId, loadPerms])

  const selectedRole = roles.find(role => role.id === Number(roleId))

  const save = async body => {
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/access-rbac/permissions', { method: 'POST', body })
      await loadPerms(roleId)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const toggle = (row, key) => {
    // Optimistic: flip just this row's cell immediately so the checkbox responds at
    // once (the server reload then confirms; rows stay put thanks to the stable sort).
    setPerms(cur => cur.map(p => (p.id === row.id ? { ...p, [key]: !p[key] } : p)))
    save({
      roleId: Number(roleId),
      path: row.path,
      canGet: !!row.canGet,
      canPost: !!row.canPost,
      canPut: !!row.canPut,
      canDelete: !!row.canDelete,
      [key]: !row[key]
    })
  }

  const addPath = () => {
    if (!path.trim().startsWith('/')) {
      setError(t('rbac.pathStartSlash'))
      return
    }
    save({ roleId: Number(roleId), path: path.trim(), canGet: true, canPost: false, canPut: false, canDelete: false })
  }

  // toggleSection flips GET on a section's path (preserving the other verbs), which is
  // what shows/hides that menu for the role.
  const toggleSection = section => {
    const existing = perms.find(row => row.path === section.path)
    save({
      roleId: Number(roleId),
      path: section.path,
      canGet: !(existing && existing.canGet),
      canPost: !!(existing && existing.canPost),
      canPut: !!(existing && existing.canPut),
      canDelete: !!(existing && existing.canDelete)
    })
  }

  const remove = async row => {
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/access-rbac/permissions/${row.id}`, { method: 'DELETE' })
      await loadPerms(roleId)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="data-region rbac-permissions">
      <header className="page-header">
        <div>
          <h1>{t('rbac.title')}</h1>
          <p>{t('rbac.subtitle')}</p>
        </div>
      </header>
      <div className="permission-controls">
        <label className="permission-role-select">
          <span>{t('rbac.role')}</span>
          <select value={roleId} onChange={event => setRoleId(Number(event.target.value))} disabled={busy}>
            {roles.length === 0 && <option value={0}>{t('rbac.noRoles')}</option>}
            {roles.map(role => (
              <option key={role.id} value={role.id}>{role.name}{role.isSuperadmin ? t('rbac.superadminParen') : ''}</option>
            ))}
          </select>
        </label>
      </div>
      {error && <div className="message danger">{error}</div>}
      {selectedRole?.isSuperadmin ? (
        <p className="message">{t('rbac.superadminBypass')}</p>
      ) : (
        <>
          <div className="menu-access">
            <h2>{t('rbac.menuAccess')}</h2>
            <p className="menu-access-hint">{t('rbac.menuAccessHint')}</p>
            <div className="menu-access-grid">
              {MENU_SECTIONS.map(section => {
                const on = !!perms.find(row => row.path === section.path)?.canGet
                return (
                  <label className="menu-access-item" key={section.path}>
                    <input type="checkbox" checked={on} onChange={() => toggleSection(section)} disabled={busy} />
                    <span>{t(section.labelKey)}</span>
                  </label>
                )
              })}
            </div>
          </div>
          <div className="permission-add">
            <input value={path} onChange={event => setPath(event.target.value)} placeholder="/api/user-credential" disabled={busy} />
            <button className="secondary-button" onClick={addPath} disabled={busy || !roleId} type="button">{t('rbac.addPath')}</button>
          </div>
          {perms.length === 0 ? (
            <p className="message">{t('rbac.noRules')}</p>
          ) : (
            <table className="permission-table">
              <thead>
                <tr>
                  <th>{t('rbac.colPath')}</th>
                  {PERMISSION_VERBS.map(([, label]) => <th key={label}>{label}</th>)}
                  <th />
                </tr>
              </thead>
              <tbody>
                {perms.map(row => (
                  <tr key={row.id}>
                    <td><code>{row.path}</code></td>
                    {PERMISSION_VERBS.map(([key, label]) => (
                      <td key={label} className="permission-cell">
                        <input type="checkbox" checked={!!row[key]} onChange={() => toggle(row, key)} disabled={busy} aria-label={`${label} ${row.path}`} />
                      </td>
                    ))}
                    <td>
                      <button className="secondary-button danger" onClick={() => remove(row)} disabled={busy} type="button">{t('rbac.remove')}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </section>
  )
}

const emptyDirectoryConfig = {
  enabled: false,
  displayLabel: '',
  host: '',
  port: 636,
  useStartTls: false,
  caCertPem: '',
  bindDn: '',
  bindPassword: '',
  baseDn: '',
  userFilter: '',
  groupAttr: '',
  subjectAttr: '',
  authoritative: false,
  hasBindPassword: false
}

// DirectoryPage is Federation → Directory: the LDAP/Active Directory connection
// (singleton config), a test-connection probe that runs against the UNSAVED form,
// and the group→role mappings. The bind password is write-only (hasBindPassword
// mirrors the SSO client-secret pattern).
function DirectoryPage({ accessList, onToast }) {
  const t = useT()
  const [form, setForm] = useState(emptyDirectoryConfig)
  const [roles, setRoles] = useState([])
  const [mappings, setMappings] = useState([])
  const [newMapping, setNewMapping] = useState({ provider: 'ldap', groupName: '', roleId: 0, priority: 0 })
  const [sampleUser, setSampleUser] = useState('')
  const [testResult, setTestResult] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const canEdit = hasEndpointAccess(accessList, '/api/directory-config', 'PUT')
  const canMap = hasEndpointAccess(accessList, '/api/federated-group-mapping', 'POST')

  const load = useCallback(async () => {
    try {
      const cfg = resultOf(await apiRequest('/api/directory-config')) || {}
      setForm(current => ({ ...current, ...cfg, bindPassword: '' }))
      const rows = rowsOf(await apiRequest('/api/federated-group-mapping?limit=200&offset=0'))
      setMappings(rows)
    } catch (err) {
      setError(err.message)
    }
    try {
      setRoles(resultOf(await apiRequest('/api/access-rbac/roles')) || [])
    } catch {
      // Role names are a nicety; mappings still render with raw role ids.
    }
  }, [])

  useEffect(() => { load() }, [load])

  const save = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      const saved = resultOf(await apiRequest('/api/directory-config', { method: 'PUT', body: form }))
      setForm(current => ({ ...current, ...saved, bindPassword: '' }))
      onToast?.(t('dir.saved'), 'success')
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const test = async () => {
    setBusy(true)
    setError('')
    setTestResult(null)
    try {
      setTestResult(resultOf(await apiRequest('/api/directory-config/test', {
        method: 'POST',
        body: { ...form, sampleUsername: sampleUser }
      })))
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const addMapping = async event => {
    event.preventDefault()
    if (!newMapping.groupName.trim() || !Number(newMapping.roleId)) {
      return
    }
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/federated-group-mapping', {
        method: 'POST',
        body: {
          provider: (newMapping.provider || 'ldap').trim(),
          groupName: newMapping.groupName.trim(),
          roleId: Number(newMapping.roleId),
          priority: Number(newMapping.priority) || 0
        }
      })
      setNewMapping({ provider: newMapping.provider || 'ldap', groupName: '', roleId: 0, priority: 0 })
      await load()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const removeMapping = async id => {
    setBusy(true)
    setError('')
    try {
      await apiRequest(`/api/federated-group-mapping/${id}`, { method: 'DELETE' })
      await load()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const roleName = id => roles.find(role => role.id === Number(id))?.name || `#${id}`

  return (
    <PageFrame title={t('dir.title')} subtitle={t('dir.subtitle')}>
      <div className="app-detail">
        {error && <div className="message danger">{error}</div>}

        <form className="record-form" onSubmit={save}>
          <label className="checkbox-field">
            <input type="checkbox" checked={Boolean(form.enabled)} onChange={event => setForm({ ...form, enabled: event.target.checked })} />
            {t('dir.enabled')}
          </label>
          <div className="two-col">
            <label>
              {t('dir.host')}
              <input value={form.host} onChange={event => setForm({ ...form, host: event.target.value })} placeholder="dc1.corp.local" />
            </label>
            <label>
              {t('dir.port')}
              <input type="number" value={form.port} onChange={event => setForm({ ...form, port: Number(event.target.value) })} />
            </label>
          </div>
          <label className="checkbox-field">
            <input type="checkbox" checked={Boolean(form.useStartTls)} onChange={event => setForm({ ...form, useStartTls: event.target.checked })} />
            {t('dir.startTls')}
          </label>
          <div className="two-col">
            <label>
              {t('dir.bindDn')}
              <input value={form.bindDn} onChange={event => setForm({ ...form, bindDn: event.target.value })} placeholder="CN=svc-myidsan,OU=Service,DC=corp,DC=local" />
            </label>
            <label>
              {t('dir.bindPassword')}
              <input type="password" value={form.bindPassword} onChange={event => setForm({ ...form, bindPassword: event.target.value })} placeholder={form.hasBindPassword ? t('dir.passwordKeep') : t('dir.passwordSet')} />
            </label>
          </div>
          <label>
            {t('dir.baseDn')}
            <input value={form.baseDn} onChange={event => setForm({ ...form, baseDn: event.target.value })} placeholder="DC=corp,DC=local" />
          </label>
          <label>
            {t('dir.userFilter')}
            <input value={form.userFilter} onChange={event => setForm({ ...form, userFilter: event.target.value })} placeholder="(&(objectClass=user)(sAMAccountName=%s))" />
          </label>
          <div className="two-col">
            <label>
              {t('dir.groupAttr')}
              <input value={form.groupAttr} onChange={event => setForm({ ...form, groupAttr: event.target.value })} placeholder="memberOf" />
            </label>
            <label>
              {t('dir.subjectAttr')}
              <input value={form.subjectAttr} onChange={event => setForm({ ...form, subjectAttr: event.target.value })} placeholder="objectGUID" />
            </label>
          </div>
          <label>
            {t('dir.caCert')}
            <textarea rows={4} value={form.caCertPem} onChange={event => setForm({ ...form, caCertPem: event.target.value })} placeholder="-----BEGIN CERTIFICATE-----" />
          </label>
          <label>
            {t('dir.displayLabel')}
            <input value={form.displayLabel} onChange={event => setForm({ ...form, displayLabel: event.target.value })} placeholder={t('dir.displayLabelHint')} />
          </label>
          <label className="checkbox-field">
            <input type="checkbox" checked={Boolean(form.authoritative)} onChange={event => setForm({ ...form, authoritative: event.target.checked })} />
            {t('dir.authoritative')}
          </label>
          <div className="form-actions">
            <button className="primary-button" type="submit" disabled={busy || !canEdit}>{t('dir.save')}</button>
          </div>
        </form>

        <hr className="detail-divider" />
        <div className="detail-heading">
          <h3>{t('dir.testHeading')}</h3>
        </div>
        <div className="two-col">
          <label>
            {t('dir.sampleUser')}
            <input value={sampleUser} onChange={event => setSampleUser(event.target.value)} placeholder="alice" />
          </label>
          <div className="form-actions">
            <button className="secondary-button" type="button" onClick={test} disabled={busy}>{t('dir.test')}</button>
          </div>
        </div>
        {testResult && (
          <div className={testResult.ok ? 'message success' : 'message danger'}>
            <div>{testResult.message}</div>
            {testResult.matchedDn && <div><strong>DN:</strong> {testResult.matchedDn}</div>}
            {testResult.email && <div><strong>{t('auth.username')}:</strong> {testResult.email} · <strong>Subject:</strong> {testResult.subject}</div>}
            {testResult.groupCount > 0 && <div><strong>{t('dir.groups')}:</strong> {testResult.groupCount} ({(testResult.sampleGroups || []).join(', ')})</div>}
          </div>
        )}

        <hr className="detail-divider" />
        <div className="detail-heading">
          <h3>{t('dir.mappingsHeading')}</h3>
        </div>
        <p className="cert-hint">{t('dir.mappingsHint')}</p>
        {mappings.length > 0 && (
          <table className="plain-table">
            <thead>
              <tr><th>{t('dir.provider')}</th><th>{t('dir.group')}</th><th>{t('dir.role')}</th><th>{t('dir.priority')}</th><th></th></tr>
            </thead>
            <tbody>
              {mappings.map(row => (
                <tr key={row.id}>
                  <td>{row.provider}</td>
                  <td>{row.groupName}</td>
                  <td>{roleName(row.roleId)}</td>
                  <td>{row.priority}</td>
                  <td>
                    <button className="secondary-button danger" type="button" onClick={() => removeMapping(row.id)} disabled={busy || !canMap}>{t('dir.remove')}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <form className="record-form" onSubmit={addMapping}>
          <div className="two-col">
            <label>
              {t('dir.provider')}
              <input value={newMapping.provider} onChange={event => setNewMapping({ ...newMapping, provider: event.target.value })} placeholder="ldap | oidc:keycloak" />
            </label>
            <label>
              {t('dir.group')}
              <input value={newMapping.groupName} onChange={event => setNewMapping({ ...newMapping, groupName: event.target.value })} placeholder="CN=Kopiv2-Admins,OU=Groups,DC=corp,DC=local" />
            </label>
          </div>
          <div className="two-col">
            <label>
              {t('dir.role')}
              <select value={newMapping.roleId} onChange={event => setNewMapping({ ...newMapping, roleId: Number(event.target.value) })}>
                <option value={0}>{t('dir.pickRole')}</option>
                {roles.map(role => (
                  <option key={role.id} value={role.id}>{role.name}</option>
                ))}
              </select>
            </label>
          </div>
          <div className="two-col">
            <label>
              {t('dir.priority')}
              <input type="number" value={newMapping.priority} onChange={event => setNewMapping({ ...newMapping, priority: Number(event.target.value) })} />
            </label>
            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={busy || !canMap}>{t('dir.addMapping')}</button>
            </div>
          </div>
        </form>
      </div>
    </PageFrame>
  )
}

function CrudPage({
  accessList = [],
  title,
  subtitle,
  resource,
  listResource,
  emptyItem,
  normalize,
  columns,
  fields,
  canCreate = true,
  listMode = 'paging',
  updateWithId = false,
  toolbar
}) {
  const t = useT()
  const effectiveListResource = listResource || resource
  const tableStateKey = tableStateCookieName(resource, effectiveListResource)
  const restoredTableState = useMemo(() => readTableState(tableStateKey), [tableStateKey])
  const initialLoad = useRef(true)
  const tableStateKeyReady = useRef(false)
  const [rows, setRows] = useState([])
  const [page, setPage] = useState(() => ({ limit: 10, offset: restoredTableState.offset, totalCnt: 0, hasNext: false, nextOffset: 0 }))
  const [selected, setSelected] = useState(() => normalize(emptyItem))
  const [selectedIds, setSelectedIds] = useState([])
  const [editorOpen, setEditorOpen] = useState(false)
  const [editorItems, setEditorItems] = useState([])
  const [editorIndex, setEditorIndex] = useState(0)
  const tableColumns = useMemo(() => filterFieldsFromColumns(columns), [columns])
  const [columnFilters, setColumnFilters] = useState(() => restoredTableState.columnFilters)
  const [sorters, setSorters] = useState(() => restoredTableState.sorters)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [tableResetVersion, setTableResetVersion] = useState(0)

  const actionAccess = useMemo(() => ({
    canCreate: canCreate && hasEndpointAccess(accessList, resource, 'POST'),
    canEdit: hasEndpointAccess(accessList, resource, 'PUT'),
    canDelete: hasEndpointAccess(accessList, resource, 'DELETE')
  }), [accessList, canCreate, resource])

  useEffect(() => {
    if (!tableStateKeyReady.current) {
      tableStateKeyReady.current = true
      return
    }
    const restored = readTableState(tableStateKey)
    initialLoad.current = true
    setColumnFilters(restored.columnFilters)
    setSorters(restored.sorters)
    setPage(current => ({ ...current, offset: restored.offset }))
  }, [tableStateKey])

  useEffect(() => {
    setColumnFilters(current => Object.fromEntries(
      Object.entries(current).filter(([key]) => tableColumns.some(column => column.key === key))
    ))
    setSorters(current => current.filter(sorter => tableColumns.some(column => column.key === sorter.fieldName)))
  }, [tableColumns])

  const filters = useMemo(() => tableColumns
    .flatMap(column => normalizeFilterDrafts(columnFilters[column.key], column)
      .map(filter => {
        const value = String(filter?.value ?? '').trim()
        if (value === '') {
          return null
        }
        return {
          fieldName: column.key,
          compare: normalizeFilterCompare(filter?.compare, column),
          value: coerceFilterValue(value, column)
        }
      })
      .filter(Boolean)), [columnFilters, tableColumns])

  const updateColumnFilter = (fieldName, criteria) => {
    setColumnFilters(current => {
      const column = tableColumns.find(item => item.key === fieldName)
      if (!column) {
        return current
      }
      const next = normalizeFilterDrafts(criteria, column)
        .filter(filter => String(filter.value ?? '').trim() !== '')
      if (next.length === 0) {
        const { [fieldName]: _removed, ...rest } = current
        return rest
      }
      return { ...current, [fieldName]: next }
    })
  }

  const toggleSorter = fieldName => {
    setSorters(current => {
      const existing = current.find(sorter => sorter.fieldName === fieldName)
      if (!existing) {
        return [...current, { fieldName, sort: 1 }]
      }
      if (Number(existing.sort) === 1) {
        return current.map(sorter => sorter.fieldName === fieldName ? { ...sorter, sort: 2 } : sorter)
      }
      return current.filter(sorter => sorter.fieldName !== fieldName)
    })
  }

  const clearTableControls = () => {
    clearCookie(tableStateKey)
    initialLoad.current = false
    setColumnFilters({})
    setSorters([])
    setPage(current => ({ ...current, offset: 0 }))
    setTableResetVersion(current => current + 1)
  }

  const load = useCallback(async (offset = 0) => {
    setBusy(true)
    setError('')
    try {
      const qs = listMode === 'paging'
        ? queryString({ limit: 10, offset, filters, sorters })
        : ''
      const payload = await apiRequest(`${effectiveListResource}${qs}`)
      const rawRows = listMode === 'paging' ? rowsOf(payload) : resultOf(payload)
      const nextRows = listMode === 'paging' ? rawRows : applyClientSorters(applyClientFilters(rawRows, filters, tableColumns), sorters, tableColumns)
      const nextPage = listMode === 'paging' ? pageOf(payload) : { limit: 0, offset, totalCnt: Array.isArray(nextRows) ? nextRows.length : 0, hasNext: false, nextOffset: 0 }
      setRows((Array.isArray(nextRows) ? nextRows : []).map(normalize))
      setPage(nextPage)
      writeTableState(tableStateKey, {
        columnFilters,
        sorters,
        offset: Number(nextPage.offset || offset || 0)
      })
    } catch (err) {
      setRows([])
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }, [columnFilters, effectiveListResource, filters, listMode, normalize, sorters, tableColumns, tableStateKey])

  useEffect(() => {
    const offset = initialLoad.current ? restoredTableState.offset : 0
    initialLoad.current = false
    load(offset)
  }, [load, restoredTableState.offset, tableResetVersion])

  useEffect(() => {
    setSelectedIds(current => current.filter(id => rows.some(row => String(row.id) === String(id))))
  }, [rows])

  const resetForm = () => {
    setSelected(normalize(emptyItem))
    setNotice('')
    setError('')
  }

  const openCreate = () => {
    setSelected(normalize(emptyItem))
    setEditorItems([])
    setEditorIndex(0)
    setNotice('')
    setError('')
    setEditorOpen(true)
  }

  const openEditSelected = () => {
    const items = rows.filter(row => selectedIds.includes(String(row.id))).map(normalize)
    if (!items.length) {
      return
    }
    setEditorItems(items)
    setEditorIndex(0)
    setSelected(items[0])
    setNotice('')
    setError('')
    setEditorOpen(true)
  }

  const closeEditor = () => {
    setEditorOpen(false)
    setSelected(normalize(emptyItem))
    setEditorItems([])
    setEditorIndex(0)
  }

  const navigateEditor = nextIndex => {
    const boundedIndex = Math.min(editorItems.length - 1, Math.max(0, nextIndex))
    setEditorIndex(boundedIndex)
    setSelected(normalize(editorItems[boundedIndex]))
  }

  const save = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const isUpdate = Number(selected.id) > 0
      if (!isUpdate && !actionAccess.canCreate) {
        throw new Error(t('crud.cantCreate'))
      }
      if (isUpdate && !actionAccess.canEdit) {
        throw new Error(t('crud.cantEdit'))
      }
      const method = isUpdate ? 'PUT' : 'POST'
      const payload = preparePayload(selected, fields)
      // Some resources (e.g. the shared accessrbac roles) update at /{id}; others
      // (the legacy CRUD modules) take the id in the body and PUT to the base path.
      const target = isUpdate && updateWithId ? `${resource}/${selected.id}` : resource
      await apiRequest(target, { method, body: payload })
      if (isUpdate && editorItems.length > 1) {
        setEditorItems(current => current.map((item, index) => index === editorIndex ? normalize({ ...item, ...payload }) : item))
        setNotice(t('crud.savedNofM', { i: editorIndex + 1, n: editorItems.length }))
      } else {
        setNotice(isUpdate ? t('crud.updated') : t('crud.created'))
        closeEditor()
      }
      await load(page.offset || 0)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  const removeSelected = async () => {
    const items = rows.filter(row => selectedIds.includes(String(row.id)))
    if (!items.length) {
      return
    }
    if (!actionAccess.canDelete) {
      setError(t('crud.cantDelete'))
      return
    }
    setBusy(true)
    setError('')
    setNotice('')
    try {
      for (const item of items) {
        await apiRequest(`${resource}/${item.id}`, { method: 'DELETE' })
      }
      setNotice(items.length === 1 ? t('crud.deleted') : t('crud.deletedN', { n: items.length }))
      setSelectedIds([])
      closeEditor()
      await load(0)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <PageFrame title={title} subtitle={subtitle}>
      <div className="work-grid">
        <section className="data-region">
          <div className="table-toolbar">
            {toolbar}
            {selectedIds.length > 0 && <span className="selection-count">{t('crud.selected', { n: selectedIds.length })}</span>}
            {canCreate && <button className="toolbar-icon icon-new primary" onClick={openCreate} disabled={busy || !actionAccess.canCreate} type="button" title={t('crud.newRecord')} aria-label={t('crud.newRecord')} />}
            <button className="toolbar-icon icon-edit" onClick={openEditSelected} disabled={busy || !actionAccess.canEdit || selectedIds.length === 0} type="button" title={t('crud.editSelected')} aria-label={t('crud.editSelected')} />
            <button className="toolbar-icon icon-delete danger" onClick={removeSelected} disabled={busy || !actionAccess.canDelete || selectedIds.length === 0} type="button" title={t('crud.deleteSelected')} aria-label={t('crud.deleteSelected')} />
            {(filters.length > 0 || sorters.length > 0 || Number(page.offset || 0) > 0) && (
              <button className="toolbar-icon icon-clear" onClick={clearTableControls} disabled={busy} type="button" title={t('crud.clearTable')} aria-label={t('crud.clearTable')} />
            )}
            <button className={busy ? 'toolbar-icon icon-refresh spinning' : 'toolbar-icon icon-refresh'} onClick={() => load(page.offset || 0)} disabled={busy} type="button" title={t('crud.refresh')} aria-label={t('crud.refresh')} />
          </div>
          {error && <div className="message danger">{error}</div>}
          {notice && <div className="message success">{notice}</div>}
          <DataTable
            rows={rows}
            columns={tableColumns}
            busy={busy}
            columnFilters={columnFilters}
            page={page}
            selectedIds={selectedIds}
            sorters={sorters}
            onFilterChange={updateColumnFilter}
            onPage={load}
            onSelectionChange={setSelectedIds}
            onSort={toggleSorter}
          />
        </section>
      </div>
      {editorOpen && (
        <EditorModal
          busy={busy}
          canCreate={actionAccess.canCreate}
          canEdit={actionAccess.canEdit}
          fields={fields}
          itemCount={editorItems.length}
          itemIndex={editorIndex}
          onChange={setSelected}
          onClear={resetForm}
          onClose={closeEditor}
          onNavigate={navigateEditor}
          onSubmit={save}
          value={selected}
        />
      )}
    </PageFrame>
  )
}

function PageFrame({ title, subtitle, children }) {
  return (
    <>
      <header className="page-header">
        <div>
          <h1>{title}</h1>
          <p>{subtitle}</p>
        </div>
      </header>
      {children}
    </>
  )
}

function EditorModal({ busy, canCreate, canEdit, fields, itemCount, itemIndex, onChange, onClear, onClose, onNavigate, onSubmit, value }) {
  const t = useT()
  const hasMultipleItems = itemCount > 1

  return (
    <div className="modal-layer">
      <button className="modal-backdrop" onClick={onClose} type="button" aria-label={t('crud.closeEditor')} />
      <section className="editor-modal" role="dialog" aria-modal="true" aria-labelledby="editor-title">
        <div className="modal-heading">
          <div>
            <h2 id="editor-title">{value.id ? t('crud.editRecord') : t('crud.newRecord')}</h2>
            {hasMultipleItems && <p>{itemIndex + 1} / {itemCount}</p>}
          </div>
          <div className="modal-actions">
            {hasMultipleItems && (
              <div className="modal-record-pager">
                <button className="pager-icon previous" onClick={() => onNavigate(itemIndex - 1)} disabled={busy || itemIndex <= 0} type="button" title={t('crud.prevRecord')} aria-label={t('crud.prevRecord')} />
                <button className="pager-icon next" onClick={() => onNavigate(itemIndex + 1)} disabled={busy || itemIndex >= itemCount - 1} type="button" title={t('crud.nextRecord')} aria-label={t('crud.nextRecord')} />
              </div>
            )}
            <button className="secondary-button" onClick={onClear} disabled={busy} type="button">{t('common.clear')}</button>
            <button className="icon-button" onClick={onClose} disabled={busy} type="button" title={t('common.close')}>{t('common.close')}</button>
          </div>
        </div>
        <div className="modal-body">
          <RecordForm fields={fields} value={value} onChange={onChange} onSubmit={onSubmit} busy={busy} canCreate={canCreate} canEdit={canEdit} />
        </div>
      </section>
    </div>
  )
}

function ColumnHeader({ column, filter, sort, onFilterOpen, onSort }) {
  const t = useT()
  const filterCount = normalizeFilterDrafts(filter, column)
    .filter(item => String(item.value ?? '').trim() !== '')
    .length
  const sortLabel = sort?.sort === 1 ? t('table.sortAsc') : sort?.sort === 2 ? t('table.sortDesc') : t('table.sort')
  const filterActive = filterCount > 0

  return (
    <div className="column-head">
      <div className="column-title-row">
        <span>{column.label}</span>
        <div className="column-actions">
          {column.filterable !== false && (
            <button
              aria-label={t('table.filterCol', { col: column.label })}
              className={filterActive ? 'filter-button active' : 'filter-button'}
              onClick={event => onFilterOpen(column, event.currentTarget)}
              title={t('table.filterCol', { col: column.label })}
              type="button"
            >
              {filterCount > 1 && <span className="filter-count">{filterCount}</span>}
            </button>
          )}
          <button className={sort ? 'sort-button active' : 'sort-button'} onClick={() => onSort(column.key)} type="button" title={t('table.sortByCol', { col: column.label })}>
            {sortLabel}
            {sort?.index && <span>{sort.index}</span>}
          </button>
        </div>
      </div>
    </div>
  )
}

function DataTable({ rows, columns, busy, columnFilters, page, selectedIds, sorters, onFilterChange, onPage, onSelectionChange, onSort }) {
  const t = useT()
  const [openFilter, setOpenFilter] = useState(null)
  const rowIds = rows.map(row => String(row.id))
  const selectedSet = new Set(selectedIds)
  const allSelected = rowIds.length > 0 && rowIds.every(id => selectedSet.has(id))
  const initialLoading = busy && rows.length === 0
  const refreshing = busy && rows.length > 0

  const openColumnFilter = (column, anchor) => {
    const rect = anchor.getBoundingClientRect()
    setOpenFilter({
      column,
      left: Math.max(12, Math.min(rect.left, window.innerWidth - 260)),
      top: rect.bottom + 8
    })
  }

  const closeColumnFilter = () => {
    setOpenFilter(null)
  }

  const toggleAllRows = checked => {
    onSelectionChange(checked ? rowIds : [])
  }

  const toggleRow = (row, checked) => {
    const id = String(row.id)
    onSelectionChange(checked
      ? Array.from(new Set([...selectedIds, id]))
      : selectedIds.filter(item => item !== id))
  }

  return (
    <div className={busy ? 'table-surface table-loading' : 'table-surface'} aria-busy={busy}>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th className="select-col">
                <input checked={allSelected} disabled={busy || rowIds.length === 0} onChange={event => toggleAllRows(event.target.checked)} type="checkbox" title={t('tbl.selectAll')} aria-label={t('tbl.selectAll')} />
              </th>
              {columns.map(column => (
                <th key={column.key}>
                  <ColumnHeader
                    column={column}
                    filter={columnFilters[column.key]}
                    sort={sortWithIndex(sorters, column.key)}
                    onFilterOpen={openColumnFilter}
                    onSort={onSort}
                />
              </th>
            ))}
            </tr>
          </thead>
          <tbody>
            {!busy && rows.length === 0 && (
              <tr>
                <td className="empty-cell" colSpan={columns.length + 1}>{t('table.empty')}</td>
              </tr>
            )}
            {initialLoading && (
              <TableSkeleton columns={columns.length + 1} />
            )}
            {rows.map(row => (
              <tr className={selectedSet.has(String(row.id)) ? 'selected-row' : ''} key={`${row.id}-${row.email || row.title || row.path || row.code}`}>
                <td className="select-col">
                  <input checked={selectedSet.has(String(row.id))} disabled={busy} onChange={event => toggleRow(row, event.target.checked)} type="checkbox" title={t('tbl.selectRow')} aria-label={t('tbl.selectRow')} />
                </td>
                {columns.map(column => (
                  <td key={column.key}>{column.render ? column.render(row[column.key], row) : printable(row[column.key])}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {refreshing && (
          <div className="table-refresh-overlay">
            <span className="table-spinner" />
            <span>{t('tbl.refreshing')}</span>
          </div>
        )}
      </div>
      {page && onPage && <Pager page={page} onPage={onPage} busy={busy} />}
      {openFilter && (
        <>
          <button className="filter-popover-backdrop" onClick={closeColumnFilter} type="button" aria-label={t('tbl.closeFilter')} />
          <ColumnFilterPopover
            column={openFilter.column}
            filter={columnFilters[openFilter.column.key]}
            left={openFilter.left}
            top={openFilter.top}
            onApply={(fieldName, patch) => {
              onFilterChange(fieldName, patch)
              closeColumnFilter()
            }}
            onClear={() => {
              onFilterChange(openFilter.column.key, [])
              closeColumnFilter()
            }}
            onClose={closeColumnFilter}
          />
        </>
      )}
    </div>
  )
}

function TableSkeleton({ columns }) {
  return (
    <>
      {Array.from({ length: 5 }).map((_, rowIndex) => (
        <tr className="skeleton-row" key={`skeleton-${rowIndex}`}>
          {Array.from({ length: columns }).map((__, columnIndex) => (
            <td key={`skeleton-${rowIndex}-${columnIndex}`}>
              <span className={columnIndex === 0 ? 'skeleton-check' : 'skeleton-line'} />
            </td>
          ))}
        </tr>
      ))}
    </>
  )
}

function ColumnFilterPopover({ column, filter, left, top, onApply, onClear, onClose }) {
  const t = useT()
  const [draft, setDraft] = useState(() => normalizeFilterDrafts(filter, column))
  const operators = filterOperatorsForField(column)

  useEffect(() => {
    setDraft(normalizeFilterDrafts(filter, column))
  }, [column, filter])

  const submit = event => {
    event.preventDefault()
    onApply(column.key, draft)
  }

  const updateDraft = (index, patch) => {
    setDraft(current => current.map((item, itemIndex) => itemIndex === index
      ? { ...item, ...patch, compare: normalizeFilterCompare(patch.compare ?? item.compare, column) }
      : item))
  }

  const addDraft = () => {
    setDraft(current => [...current, createColumnFilter(column)])
  }

  const removeDraft = index => {
    setDraft(current => {
      const next = current.filter((_, itemIndex) => itemIndex !== index)
      return next.length > 0 ? next : [createColumnFilter(column)]
    })
  }

  return (
    <div className="filter-popover" style={{ left, top }}>
      <div className="filter-popover-head">
        <span>{column.label}</span>
        <button className="mini-button" onClick={onClose} type="button">{t('common.close')}</button>
      </div>
      <form className="filter-popover-body" onSubmit={submit}>
        {draft.map((item, index) => (
          <div className="filter-condition" key={`filter-${column.key}-${index}`}>
            <div className="filter-condition-head">
              <span>{t('table.condition', { n: index + 1 })}</span>
              {draft.length > 1 && <button className="mini-button" onClick={() => removeDraft(index)} type="button">{t('common.remove')}</button>}
            </div>
            <label>
              {t('table.operator')}
              <select value={normalizeFilterCompare(item.compare, column)} onChange={event => updateDraft(index, { compare: Number(event.target.value) })}>
                {operators.map(operator => <option key={operator.value} value={operator.value}>{operator.label}</option>)}
              </select>
            </label>
            <label>
              {t('table.value')}
              {column.filterType === 'boolean' ? (
                <select value={item.value} onChange={event => updateDraft(index, { value: event.target.value })}>
                  <option value="">{t('common.any')}</option>
                  <option value="true">{t('common.yes')}</option>
                  <option value="false">{t('common.no')}</option>
                </select>
              ) : (
                <input
                  autoFocus={index === 0}
                  value={item.value}
                  onChange={event => updateDraft(index, { value: event.target.value })}
                  type={column.filterType === 'number' ? 'number' : 'text'}
                />
              )}
            </label>
          </div>
        ))}
        <div className="filter-popover-actions">
          <button className="secondary-button" onClick={addDraft} type="button">{t('common.add')}</button>
          <button className="secondary-button" onClick={onClear} type="button">{t('common.clear')}</button>
          <button className="primary-button" type="submit">{t('common.apply')}</button>
        </div>
      </form>
    </div>
  )
}

function RecordForm({ fields, value, onChange, onSubmit, busy, canCreate, canEdit = true }) {
  const t = useT()
  const update = (name, nextValue) => onChange({ ...value, [name]: nextValue })
  const canSubmit = value.id ? canEdit : canCreate

  return (
    <form className="record-form" onSubmit={onSubmit}>
      {Number(value.id) > 0 && (
        <label>
          {t('crud.id')}
          <input value={value.id} disabled />
        </label>
      )}
      {fields.map(field => (
        <Field key={field.name} field={field} value={value[field.name]} onChange={nextValue => update(field.name, nextValue)} />
      ))}
      <button className="primary-button" disabled={busy || !canSubmit} type="submit">
        {busy ? t('crud.saving') : value.id ? t('crud.saveChanges') : t('crud.create')}
      </button>
    </form>
  )
}

function Field({ field, value, onChange }) {
  // Hidden fields are still submitted (kept in the payload) but never rendered —
  // used for app-local constants like an endpoint's auto-stamped appCode.
  if (field.hidden) {
    return null
  }
  if (field.type === 'checkbox') {
    return (
      <label className="checkbox-field">
        <input checked={Boolean(value)} onChange={event => onChange(event.target.checked)} type="checkbox" />
        {field.label}
      </label>
    )
  }

  if (field.type === 'select') {
    return (
      <label>
        {field.label}
        <select value={value ?? ''} onChange={event => onChange(emptyToZero(event.target.value))} required={field.required}>
          {field.options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </label>
    )
  }

  if (field.type === 'textarea') {
    return (
      <label>
        {field.label}
        <textarea
          rows={field.rows || 5}
          value={value ?? ''}
          onChange={event => onChange(event.target.value)}
          placeholder={field.placeholder || ''}
          required={field.required}
        />
      </label>
    )
  }

  return (
    <label>
      {field.label}
      <input
        value={value ?? ''}
        onChange={event => onChange(field.type === 'number' ? emptyToZero(event.target.value) : event.target.value)}
        placeholder={field.placeholder || ''}
        required={field.required}
        type={field.type || 'text'}
      />
    </label>
  )
}

function Pager({ page, onPage, busy }) {
  const t = useT()
  const offset = Number(page.offset || 0)
  const limit = Number(page.limit || 10)
  const total = Number(page.totalCnt || 0)
  const pageCount = Math.max(1, Math.ceil(total / limit))
  const currentPage = Math.min(pageCount, Math.floor(offset / limit) + 1)
  const prev = Math.max(0, offset - limit)
  const last = Math.max(0, (pageCount - 1) * limit)
  const [pageDraft, setPageDraft] = useState(String(currentPage))

  useEffect(() => {
    setPageDraft(String(currentPage))
  }, [currentPage])

  const goToDraftPage = () => {
    const target = Math.min(pageCount, Math.max(1, Number(pageDraft) || 1))
    onPage((target - 1) * limit)
  }

  const submitDraftPage = event => {
    if (event.key === 'Enter') {
      event.preventDefault()
      goToDraftPage()
    }
  }

  return (
    <div className="pager">
      <span>{t('pager.summary', { current: currentPage, count: pageCount, total })}</span>
      <div className="pager-controls">
        <button className="pager-icon first" disabled={busy || offset <= 0} onClick={() => onPage(0)} title={t('pager.first')} type="button" aria-label={t('pager.first')} />
        <button className="pager-icon previous" disabled={busy || offset <= 0} onClick={() => onPage(prev)} title={t('pager.previous')} type="button" aria-label={t('pager.previous')} />
        <label className="pager-jump">
          <input
            min="1"
            max={pageCount}
            value={pageDraft}
            onChange={event => setPageDraft(event.target.value)}
            onKeyDown={submitDraftPage}
            title={t('pager.pageNumber')}
            type="number"
          />
        </label>
        <button className="pager-icon go" disabled={busy} onClick={goToDraftPage} title={t('pager.goto')} type="button" aria-label={t('pager.goto')} />
        <button className="pager-icon next" disabled={busy || !page.hasNext} onClick={() => onPage(page.nextOffset)} title={t('pager.next')} type="button" aria-label={t('pager.next')} />
        <button className="pager-icon last" disabled={busy || offset >= last} onClick={() => onPage(last)} title={t('pager.last')} type="button" aria-label={t('pager.last')} />
      </div>
    </div>
  )
}

function tableStateCookieName(resource, listResource) {
  const raw = `${resource || ''}:${listResource || ''}`
  return `${TABLE_STATE_PREFIX}${raw.replace(/[^a-zA-Z0-9]+/g, '_').replace(/^_+|_+$/g, '')}`
}

function readTableState(cookieName) {
  const fallback = {
    version: TABLE_STATE_VERSION,
    columnFilters: {},
    sorters: [],
    offset: 0
  }
  const raw = getCookie(cookieName)
  if (!raw) {
    return fallback
  }
  try {
    const parsed = JSON.parse(decodeURIComponent(raw))
    if (parsed?.version !== TABLE_STATE_VERSION) {
      return fallback
    }
    return {
      version: TABLE_STATE_VERSION,
      columnFilters: isPlainObject(parsed.columnFilters) ? parsed.columnFilters : {},
      sorters: Array.isArray(parsed.sorters) ? parsed.sorters : [],
      offset: Math.max(0, Number(parsed.offset || 0))
    }
  } catch {
    return fallback
  }
}

function writeTableState(cookieName, state) {
  const columnFilters = state.columnFilters || {}
  const sorters = Array.isArray(state.sorters) ? state.sorters : []
  const offset = Math.max(0, Number(state.offset || 0))
  if (Object.keys(columnFilters).length === 0 && sorters.length === 0 && offset === 0) {
    clearCookie(cookieName)
    return
  }
  setCookie(cookieName, JSON.stringify({
    version: TABLE_STATE_VERSION,
    columnFilters,
    sorters,
    offset
  }))
}

function isPlainObject(value) {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function createColumnFilter(field) {
  return {
    compare: filterOperatorsForField(field)[0]?.value || 1,
    value: ''
  }
}

function normalizeFilterDrafts(value, field) {
  const source = Array.isArray(value) ? value : value ? [value] : [createColumnFilter(field)]
  return source.map(item => ({
    compare: normalizeFilterCompare(item?.compare, field),
    value: item?.value ?? ''
  }))
}

function sortWithIndex(sorters, fieldName) {
  const index = sorters.findIndex(sorter => sorter.fieldName === fieldName)
  return index >= 0 ? { ...sorters[index], index: index + 1 } : null
}

function filterFieldsFromColumns(columns) {
  return columns
    .filter(column => column.filterable !== false)
    .map(column => ({
      ...column,
      filterType: column.filterType || inferFilterType(column.key)
    }))
}

function inferFilterType(key) {
  if (['isActive', 'canGet', 'canPost', 'canPut', 'canDelete'].includes(key)) {
    return 'boolean'
  }
  if (key === 'id' || key.endsWith('Id') || ['createdAt', 'updatedAt', 'accessTier', 'endpointTier'].includes(key)) {
    return 'number'
  }
  return 'text'
}

function filterOperatorsForField(field) {
  if (field?.filterType === 'boolean') {
    return BOOLEAN_FILTER_OPERATORS
  }
  if (field?.filterType === 'number') {
    return FILTER_OPERATORS
  }
  return TEXT_FILTER_OPERATORS
}

function normalizeFilterCompare(compare, field) {
  const value = Number(compare || 1)
  const operators = filterOperatorsForField(field)
  return operators.some(operator => operator.value === value) ? value : operators[0].value
}

function coerceFilterValue(value, field) {
  if (field?.filterType === 'boolean') {
    return String(value).toLowerCase() === 'true'
  }
  if (field?.filterType === 'number') {
    return Number(value)
  }
  return value
}

function applyClientFilters(rows, filters, fields) {
  if (!Array.isArray(rows) || filters.length === 0) {
    return rows
  }
  return rows.filter(row => filters.every(filter => {
    const field = fields.find(item => item.key === filter.fieldName)
    return compareFilterValue(row?.[filter.fieldName], filter.value, Number(filter.compare), field)
  }))
}

function applyClientSorters(rows, sorters, fields) {
  if (!Array.isArray(rows) || sorters.length === 0) {
    return rows
  }
  const activeSorters = sorters
    .map(sorter => ({
      ...sorter,
      field: fields.find(item => item.key === sorter.fieldName)
    }))
    .filter(sorter => sorter.field)
  if (activeSorters.length === 0) {
    return rows
  }

  return [...rows].sort((a, b) => {
    for (const sorter of activeSorters) {
      const left = sortComparableValue(a?.[sorter.fieldName], sorter.field)
      const right = sortComparableValue(b?.[sorter.fieldName], sorter.field)
      if (left === right) {
        continue
      }
      const direction = Number(sorter.sort) === 2 ? -1 : 1
      return left > right ? direction : -direction
    }
    return 0
  })
}

function sortComparableValue(value, field) {
  if (field?.filterType === 'number') {
    const numeric = Number(value)
    return Number.isNaN(numeric) ? 0 : numeric
  }
  if (field?.filterType === 'boolean') {
    return value ? 1 : 0
  }
  return String(value ?? '').toLowerCase()
}

function compareFilterValue(actual, expected, compare, field) {
  if (field?.filterType === 'number') {
    const left = Number(actual)
    const right = Number(expected)
    if (Number.isNaN(left) || Number.isNaN(right)) {
      return false
    }
    return compareValues(left, right, compare)
  }
  if (field?.filterType === 'boolean') {
    return compareValues(Boolean(actual), Boolean(expected), compare)
  }
  return compareValues(String(actual ?? ''), String(expected ?? ''), compare)
}

function compareValues(left, right, compare) {
  switch (compare) {
    case 2:
      return left !== right
    case 3:
      return left > right
    case 4:
      return left < right
    case 5:
      return left >= right
    case 6:
      return left <= right
    case 1:
    default:
      return left === right
  }
}

function preparePayload(value, fields) {
  const payload = {}
  if (Number(value?.id) > 0) {
    payload.id = emptyToZero(value.id)
  }
  fields.forEach(field => {
    payload[field.name] = value?.[field.name]
    if (field.type === 'number' || field.type === 'select') {
      payload[field.name] = emptyToZero(payload[field.name])
    }
    if (field.type === 'checkbox') {
      payload[field.name] = Boolean(payload[field.name])
    }
    if (field.dtoType === 'nullableString') {
      payload[field.name] = toNullableString(payload[field.name])
    }
    if (field.name === 'metadata') {
      payload[field.name] = normalizeMetadataText(payload[field.name])
    }
  })
  return payload
}

function toNullableString(value) {
  const text = String(value || '').trim()
  return {
    String: text,
    Valid: text.length > 0
  }
}

function boolLabel(value) {
  return <span className={value ? 'status-pill on' : 'status-pill off'}>{value ? 'Yes' : 'No'}</span>
}

function tierLabel(value) {
  return ACCESS_TIERS.find(tier => Number(tier.value) === Number(value))?.label || value
}

function menuMetadataLabel(value) {
  const items = menuItemsFromMetadata(value)
  const enabled = items.filter(item => item.enabled !== false)
  if (enabled.length === 0) {
    return <span className="status-pill off">Hidden</span>
  }
  return <span className="status-pill on">{enabled.map(item => item.label || item.id).join(', ')}</span>
}

function printable(value) {
  if (typeof value === 'boolean') {
    return value ? 'Yes' : 'No'
  }
  if (typeof value === 'object' && value !== null) {
    return value.String || JSON.stringify(value)
  }
  if (String(value || '').length > 18 && Number(value) > 1000000000) {
    return formatDateTime(value)
  }
  return value ?? ''
}

function sectionAllowedById(sectionId, accessList, isSuperadmin = false) {
  return buildVisibleSections(accessList, isSuperadmin).some(section => section.id === sectionId)
}

function sectionAllowed(section, accessList) {
  if (section.id === 'dashboard') {
    return true
  }
  return section.paths.some(path => hasEndpointAccess(accessList, path, 'GET'))
}

function buildVisibleSections(accessList, isSuperadmin = false) {
  const sectionsById = new Map()
  let hasMenuConfig = false

  accessList.forEach(access => {
    if (!hasAccessMethod(access, 'GET')) {
      return
    }
    const items = menuItemsFromMetadata(access?.metadata)
    if (items.length > 0) {
      hasMenuConfig = true
    }
    items.forEach(item => {
      if (item.enabled === false) {
        return
      }
      const catalog = routeCatalogById[item.id]
      if (!catalog || !catalog.paths.some(path => pathMatches(access?.path, path))) {
        return
      }
      // Admin sections are superadmin-only — never surface them to anyone else, even if
      // a broad matrix grant (e.g. a viewer's GET /api wildcard) would otherwise match.
      if (catalog.superadminOnly && !isSuperadmin) {
        return
      }
      const merged = {
        ...catalog,
        ...cleanMenuItem(item),
        paths: catalog.paths,
        code: item.code || catalog.code,
        group: item.group || catalog.group,
        label: item.label || catalog.label,
        order: Number.isFinite(Number(item.order)) ? Number(item.order) : catalog.order,
        summary: item.summary || catalog.summary,
        tone: item.tone || catalog.tone
      }
      sectionsById.set(merged.id, merged)
    })
  })

  const allowed = hasMenuConfig
    ? Array.from(sectionsById.values())
    : routeCatalog.filter(section => sectionAllowed(section, accessList) && (!section.superadminOnly || isSuperadmin))

  allowed.sort((a, b) => {
    const orderDiff = Number(a.order || 0) - Number(b.order || 0)
    return orderDiff || String(a.label).localeCompare(String(b.label))
  })

  return [dashboardSection, ...allowed]
}

function groupNavSections(visibleSections) {
  const groups = []
  visibleSections.forEach(section => {
    const label = section.group || 'Workspace'
    let group = groups.find(item => item.label === label)
    if (!group) {
      group = { label, items: [] }
      groups.push(group)
    }
    group.items.push(section)
  })
  return groups
}

function hasEndpointAccess(accessList, path, method) {
  return accessList.some(access => hasAccessMethod(access, method) && pathMatches(access.path, path))
}

function hasAccessMethod(access, method) {
  const methodKey = {
    GET: 'canGet',
    POST: 'canPost',
    PUT: 'canPut',
    DELETE: 'canDelete'
  }[method.toUpperCase()]

  return Boolean(access?.isActive && access[methodKey])
}

function pathMatches(allowed, target) {
  const allowedPath = String(allowed || '').replace(/\/$/, '')
  const targetPath = String(target || '').replace(/\/$/, '')
  return allowedPath === targetPath || targetPath.startsWith(`${allowedPath}/`)
}

function menuItemsFromMetadata(value) {
  const metadata = parseEndpointMetadata(value)
  if (!metadata) {
    return []
  }
  if (Array.isArray(metadata.menus)) {
    return metadata.menus.filter(Boolean)
  }
  if (metadata.menu) {
    return [metadata.menu]
  }
  return []
}

function parseEndpointMetadata(value) {
  if (!value) {
    return null
  }
  if (typeof value === 'object') {
    return value
  }
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function cleanMenuItem(item) {
  return {
    id: String(item.id || '').trim(),
    label: String(item.label || '').trim(),
    group: String(item.group || '').trim(),
    order: item.order,
    summary: String(item.summary || '').trim(),
    tone: String(item.tone || '').trim(),
    code: String(item.code || '').trim()
  }
}

function formatMetadataForEdit(value) {
  if (!value) {
    return ''
  }
  const metadata = parseEndpointMetadata(value)
  return metadata ? JSON.stringify(metadata, null, 2) : String(value)
}

function normalizeMetadataText(value) {
  const text = String(value || '').trim()
  if (!text) {
    return ''
  }
  try {
    return JSON.stringify(JSON.parse(text))
  } catch {
    throw new Error('Menu metadata must be valid JSON text.')
  }
}

function initials(value) {
  return String(value || '')
    .split(/\s+/)
    .filter(Boolean)
    .map(part => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

function dashboardBody(sectionId) {
  switch (sectionId) {
    case 'users':
      return 'Maintain credentials, profile details, and role assignments.'
    case 'groups':
      return 'Organize role ownership and hierarchy roots.'
    case 'roles':
      return 'Create group-scoped roles and parent role chains.'
    case 'apps':
      return 'Manage registered relying apps and audiences.'
    case 'endpoints':
      return 'Maintain the protected endpoint catalog.'
    case 'rbac':
      return 'Map endpoints to role-specific HTTP permissions.'
    default:
      return ''
  }
}

const LANG_KEY = 'myidsan_lang'

// App owns the active locale and wraps the tree in the shared LangProvider so the
// shared SideNav, DataTable, and ToastStack translate. Persists in localStorage like
// the theme; defaults to the browser language → English.
function App() {
  const [lang, setLang] = useState(() => {
    try { return normalizeLang(localStorage.getItem(LANG_KEY) || navigator.language) } catch { return 'en' }
  })
  // English is always present (it is every key's fallback and must be there on first paint); other
  // locales are fetched on demand — see ./i18n — and accumulated here so a switch back is instant.
  const [appMessages, setAppMessages] = useState(enBundle)
  // A returning non-English user must not flash English app strings, so gate the first paint until
  // their locale chunk has loaded. English users never wait.
  const [langReady, setLangReady] = useState(lang === 'en')

  useEffect(() => {
    let alive = true
    if (lang === 'en' || appMessages[lang]) { setLangReady(true); return undefined }
    loadLocaleDict(lang).then(dict => {
      if (!alive) return
      if (dict) setAppMessages(prev => ({ ...prev, [lang]: dict }))
      setLangReady(true)
    })
    return () => { alive = false }
    // appMessages intentionally omitted: including it would re-run the effect after the very
    // setState it triggers. The `appMessages[lang]` guard above already handles a loaded locale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lang])

  const changeLang = useCallback(async next => {
    try { localStorage.setItem(LANG_KEY, next) } catch { /* ignore */ }
    // Load the locale's chunk BEFORE switching, so the UI never flashes English on the way to the
    // new language. For English or an already-loaded locale this resolves immediately.
    if (next !== 'en' && !appMessages[next]) {
      const dict = await loadLocaleDict(next)
      if (dict) setAppMessages(prev => ({ ...prev, [next]: dict }))
    }
    setLang(next)
  }, [appMessages])

  if (!langReady) {
    // Brief, and only for a returning non-English user on cold load.
    return <div className="boot-screen" />
  }
  return (
    <LangProvider lang={lang} messages={appMessages}>
      <AppInner lang={lang} onLangChange={changeLang} />
    </LangProvider>
  )
}

export default App
