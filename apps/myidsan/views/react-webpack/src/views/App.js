import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ACCESS_TIERS,
  apiBase,
  apiRequest,
  clearCookie,
  emptyToZero,
  formatDateTime,
  getCookie,
  onSessionLost,
  pageOf,
  queryString,
  resetSessionLost,
  resultOf,
  rowsOf,
  setCookie
} from '../lib/api'
import { Ico, DataTable as ClientDataTable, ToastStack, SideNav, LangProvider, LanguageDropdown, AppFooter, BrandLogo, normalizeLang, useT } from '@shared'
import { enBundle, loadLocaleDict } from './i18n'
import SetupWizard from './components/setup'
import { SettingsPage } from './components/settings'
import { WebAuthnKeys } from './components/webauthn_keys'
import { getAssertion, describeCeremonyError, webauthnSupported } from '../lib/webauthn'

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
  { id: 'resetRequests', label: 'Reset requests', group: 'Administration', order: 15, tone: 'amber', code: 'PR', icon: 'key', paths: ['/api/password-reset'], summary: 'Review and resolve forgotten-password requests from local accounts.', superadminOnly: true },
  { id: 'groups', label: 'Groups', group: 'Administration', order: 20, tone: 'teal', code: 'GR', icon: 'folder', paths: ['/api/user-group'], summary: 'Organize identity ownership and hierarchy roots.', superadminOnly: true },
  { id: 'roles', label: 'Roles', group: 'Administration', order: 30, tone: 'violet', code: 'RO', icon: 'key', paths: ['/api/access-rbac'], summary: 'Create, edit, and remove accessrbac roles (shared module).', superadminOnly: true },
  { id: 'rbac', label: 'RBAC', group: 'Administration', order: 35, tone: 'green', code: 'RB', icon: 'lock', paths: ['/api/access-rbac'], summary: 'Manage accessrbac roles (shared module). Superadmin bypasses; viewer is read-only.', superadminOnly: true },
  { id: 'apps', label: 'Apps', group: 'Federation', order: 40, tone: 'indigo', code: 'AP', icon: 'grid2', paths: ['/api/app-registry'], summary: 'Manage registered relying apps and audiences.' },
  { id: 'directory', label: 'Directory', group: 'Federation', order: 45, tone: 'amber', code: 'DI', icon: 'key', paths: ['/api/directory-config', '/api/federated-group-mapping'], summary: 'Connect an LDAP/Active Directory server and map groups to roles.' },
  { id: 'endpoints', label: 'Endpoints', group: 'Access Control', order: 50, tone: 'steel', code: 'EP', icon: 'list', paths: ['/api/endpoint'], summary: 'Maintain the protected endpoint catalog.' },
  // Backup is superadminOnly for the same reason its API is: an export is the entire
  // identity store in one file, and a restore rewrites every account and role.
  // Audit is superadminOnly for the same reason its API is: the trail names who did what
  // from where, and on an identity server it also reveals which usernames exist.
  { id: 'audit', label: 'Audit log', group: 'System', order: 55, tone: 'steel', code: 'AU', icon: 'list', paths: ['/api/audit'], summary: 'Who signed in, what changed, and from where.', superadminOnly: true },
  { id: 'backup', label: 'Backup & restore', group: 'System', order: 60, tone: 'amber', code: 'BK', icon: 'folder', paths: ['/api/backup'], summary: 'Export an encrypted copy of this server, or rebuild it from one.', superadminOnly: true },
  // Settings is superadminOnly for the same reason its API is: the editable subset includes
  // the JWT secret, the SSO token lifetimes and the sign-in policy — values that can take
  // the whole control plane offline, so they are never delegated through the matrix.
  { id: 'settings', label: 'Settings', group: 'System', order: 90, tone: 'steel', code: 'SE', icon: 'shield', paths: ['/api/settings'], summary: 'Sign-in policy, SSO token lifetimes, storage and logging — applied on restart.', superadminOnly: true },
  // Profile is self-service (change password + second factor). It is NOT a nav item —
  // it is reached from the account chip in the side rail (chipOnly), so it never shows
  // in the nav or the dashboard, but it is still a "known + allowed" active section.
  { id: 'profile', label: 'Profile', group: 'Account', order: 5, tone: 'green', code: 'ME', icon: 'user', paths: [], summary: 'Your account: change your password and manage two-factor authentication.', chipOnly: true }
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

// LoginBrand is the centered brand block at the top of the auth cards (login,
// change-password, pending-clearance). It uses the suite's shared BrandLogo (shield
// + eye + check, tinted violet via --brand-mark) instead of a bespoke tile, matching
// mymatasan/myseliasan.
function LoginBrand({ subtitle }) {
  return (
    <div className="login-brand">
      <BrandLogo wordmark="myidsan" size={104} className="brand-logo--login" />
      <p className="brand-subtitle">{subtitle}</p>
    </div>
  )
}

// AccountCard is the slim role chip + ghost logout under the brand in the side rail,
// standardized with mymatasan/myseliasan. The avatar + role open the self-service
// Profile (change password, two-factor); the signed-in identity itself is shown there,
// not here.
function AccountCard({ roleLabel, onLogout, onOpenProfile }) {
  const t = useT()
  return (
    <div className="side-account">
      <button
        type="button"
        className="side-account-open"
        onClick={onOpenProfile}
        title={t('nav.profile')}
        aria-label={t('nav.profile')}
      >
        <span className="side-account-avatar" aria-hidden="true"><Ico n="user" sz={15} /></span>
        <span className="side-account-role">{roleLabel}</span>
      </button>
      <button
        type="button"
        className="side-account-logout"
        onClick={onLogout}
        title={t('common.logout')}
        aria-label={t('common.logout')}
      >
        <Ico n="logout" sz={16} />
      </button>
    </div>
  )
}

// WorkspaceHeader is the slim top strip of the main workspace: language switcher +
// theme picker, top-right. Primary navigation and the account/logout block live in
// the side rail. Standardized with mymatasan's shell.
function WorkspaceHeader({ lang, onLangChange, theme, onThemeChange }) {
  return (
    <div className="workspace-header">
      <LanguageDropdown lang={lang} onLang={onLangChange} />
      <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
    </div>
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
  const [mustEnrollMfa, setMustEnrollMfa] = useState(false)
  const [pending, setPending] = useState(false)
  const [isSuperadmin, setIsSuperadmin] = useState(false)
  const [roleName, setRoleName] = useState('')
  const [setupNeeded, setSetupNeeded] = useState(false)
  // Pinned (default) keeps the full rail in the layout; unpinned collapses it to an
  // icon strip that slides out on hover. Persisted per browser, mirroring mymatasan.
  const [navPinned, setNavPinned] = useState(() => {
    try { return localStorage.getItem('myidsan.navPinned') !== '0' } catch { return true }
  })
  const toggleNavPinned = useCallback(() => {
    setNavPinned(prev => {
      const next = !prev
      try { localStorage.setItem('myidsan.navPinned', next ? '1' : '0') } catch { /* ignore */ }
      return next
    })
  }, [])
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
      setRoleName(me?.roleName || '')
      setMustChange(Boolean(me?.mustChangePassword))
      setMustEnrollMfa(Boolean(me?.mustEnrollMfa))
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
      // First-run wizard: only the superadmin sees it, and only until it is
      // completed (a single server-side flag, shared across browsers).
      if (me && me.isSuperadmin && !me.mustChangePassword) {
        try {
          const setup = resultOf(await apiRequest('/api/setup/state'))
          setSetupNeeded(!setup?.completed)
        } catch {
          setSetupNeeded(false)
        }
      } else {
        setSetupNeeded(false)
      }
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
  // chipOnly sections (Profile) aren't in the nav/visible list but are still allowed —
  // they're opened from the account chip, so treat them as reachable.
  const activeAllowed = visibleSections.some(section => section.id === active) || Boolean(routeCatalogById[active]?.chipOnly)
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
    // Arm the session-lost announcement again: it fires at most once per lost session so a
    // page fanning out parallel requests cannot bounce the operator several times, which
    // means a NEW session has to re-arm it or the next loss would go unnoticed.
    resetSessionLost()
    setSession(true)
    setSessionReady(true)
    setSessionError('')
    refreshSession()
  }

  const handleLogout = async () => {
    await apiRequest('/api/login/default/logout', { method: 'POST' })
    endSessionLocally()
  }

  // Everything a lost session has to undo locally. Shared by the logout button and by the
  // 401 handler below, so a session that ENDS ON ITS OWN leaves the app in the same state as
  // one the operator chose to end — rather than in a drawn admin console full of empty data.
  const endSessionLocally = useCallback(() => {
    localStorage.removeItem('myidsan.session')
    setAccessList([])
    setSession(false)
    setActiveSection('dashboard')
  }, [])

  // A 401 from anywhere means the session is gone: it expired, an administrator ended it, or
  // the operator ended it themselves from the Users page (which includes their own row). The
  // first screen check this app ever had found the app rendering the whole console afterwards,
  // with the Audit log reporting "No events match these filters" while nobody was signed in.
  useEffect(() => onSessionLost(() => {
    setSessionError('')
    endSessionLocally()
  }), [endSessionLocally])

  if (!sessionReady) {
    return <div className="boot-screen">{t('common.checkingSession')}</div>
  }

  if (!session) {
    return <AuthScreen onAuthed={handleAuthed} sessionError={sessionError} />
  }

  if (mustChange) {
    return <ChangePasswordScreen onDone={refreshSession} onLogout={handleLogout} />
  }

  // Checked after the password gate so someone owing both is walked through them in a
  // sensible order: set a password you chose, then add a factor to it.
  if (mustEnrollMfa) {
    return <EnrollMfaScreen onDone={refreshSession} onLogout={handleLogout} />
  }

  if (pending) {
    return <PendingClearanceScreen email={currentEmail} onRefresh={refreshSession} onLogout={handleLogout} />
  }

  if (setupNeeded && isSuperadmin) {
    return (
      <SetupWizard
        isSuperadmin={isSuperadmin}
        onDone={() => { setSetupNeeded(false); refreshHandoff() }}
        onToast={pushToast}
      />
    )
  }

  const roleLabel = isSuperadmin ? t('role.superadmin') : (roleName || t('role.member'))

  return (
    <div className={`app-shell${navPinned ? '' : ' nav-autohide'}`}>
      <ToastStack toasts={toasts} onDismiss={dismissToast} />
      <SideNav
        brand={(
          <div className="side-brand">
            <button
              type="button"
              className="nav-pin-toggle"
              onClick={event => { toggleNavPinned(); event.currentTarget.blur() }}
              aria-pressed={navPinned}
              title={navPinned ? t('nav.autohide') : t('nav.pin')}
              aria-label={navPinned ? t('nav.autohide') : t('nav.pin')}
            >
              <Ico n={navPinned ? 'pin' : 'pin-off'} sz={16} />
            </button>
            <BrandLogo wordmark="myidsan" />
            <div className="side-brand-sub">{t('brand.subtitle')}</div>
            <AccountCard roleLabel={roleLabel} onLogout={handleLogout} onOpenProfile={() => setActiveSection('profile')} />
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
        footer={null}
      />
      <main className="main-workspace">
        <WorkspaceHeader lang={lang} onLangChange={onLangChange} theme={theme} onThemeChange={changeTheme} />
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
        {active === 'profile' && <ProfilePage currentEmail={currentEmail} roleLabel={roleLabel} onToast={pushToast} />}
        {active === 'resetRequests' && sectionAllowedById('resetRequests', accessList, isSuperadmin) && <ResetRequestsPage onToast={pushToast} />}
        {active === 'audit' && sectionAllowedById('audit', accessList, isSuperadmin) && <AuditPage onToast={pushToast} />}
        {active === 'backup' && sectionAllowedById('backup', accessList, isSuperadmin) && <BackupPage onToast={pushToast} />}
        {active === 'settings' && sectionAllowedById('settings', accessList, isSuperadmin) && <SettingsPage isSuperadmin={isSuperadmin} onToast={pushToast} />}
        <AppFooter appName="MyIDSan" apiBase={apiBase} />
      </main>
    </div>
  )
}

// Toast / ToastStack now live in the shared module (@shared) so both control planes
// share one notification design — imported at the top of this file.

// ThemeDropdown mirrors myseliasan's light/dark selector. In myidsan it sits in the
// right-aligned workspace header (top strip), so it is content-sized and its menu
// opens downward, right-aligned (see .theme-drop-wrap / .theme-menu).
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


// EnrollMfaScreen pins a user whose role requires a second factor but who has none.
//
// It appears AFTER a successful password sign-in, which is what makes it safe: the person
// is being made to ADD a factor, not to prove one they do not hold. Refusing the login
// outright would lock out every existing administrator the moment the policy is switched
// on — the opposite of what turning on MFA is meant to achieve.
//
// Sign out is deliberately offered: someone who cannot enrol right now (no phone to hand)
// must be able to leave rather than be trapped on a screen they cannot complete.
function EnrollMfaScreen({ onDone, onLogout }) {
  const t = useT()
  const [enroll, setEnroll] = useState(null)
  const [label, setLabel] = useState('')
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const begin = async () => {
    setBusy(true); setError('')
    try {
      setEnroll(resultOf(await apiRequest('/api/mfa/enroll', { method: 'POST', body: { label } })))
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const confirm = async event => {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/mfa/enroll/verify', { method: 'POST', body: { code } })
      // Recovery codes are shown ONCE. Hold the screen until they are acknowledged
      // rather than dropping straight into the app and losing them.
      setCodes((resultOf(res) || {}).recoveryCodes || [])
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  return (
    <div className="auth-layout">
      <section className="auth-panel">
        <div className="brand-block auth-brand">
          <BrandLogo wordmark="myidsan" />
        </div>
        <h2>{t('mfaReq.title')}</h2>
        <p className="guide-note">{t('mfaReq.body')}</p>
        {error && <div className="message danger">{error}</div>}

        {codes ? (
          <div className="record-form">
            <div className="message success">
              <strong>{t('mfa.recoveryTitle')}</strong>
              <p className="cert-hint">{t('mfa.recoveryHint')}</p>
              <ul>{codes.map(c => <li key={c}><code>{c}</code></li>)}</ul>
            </div>
            <button className="primary-button" type="button" onClick={onDone}>{t('mfaReq.continue')}</button>
          </div>
        ) : !enroll ? (
          <div className="record-form">
            <label>
              {t('mfa.deviceLabel')}
              <input value={label} placeholder={t('mfa.deviceLabelHint')} onChange={e => setLabel(e.target.value)} />
            </label>
            <div className="form-actions">
              <button className="primary-button" type="button" onClick={begin} disabled={busy}>
                {busy ? t('auth.working') : t('mfa.enable')}
              </button>
              <button className="secondary-button" type="button" onClick={onLogout}>{t('common.logout')}</button>
            </div>
          </div>
        ) : (
          <form className="record-form" onSubmit={confirm}>
            <p className="cert-hint">{t('mfa.scanHint')}</p>
            {enroll.qrPngBase64 && <img alt={t('mfa.qrAlt')} src={`data:image/png;base64,${enroll.qrPngBase64}`} />}
            <p className="cert-hint">{t('mfa.manualEntry')}: <code>{enroll.secret}</code></p>
            <label>
              {t('mfa.code')}
              <input value={code} inputMode="numeric" autoComplete="one-time-code" onChange={e => setCode(e.target.value)} required />
            </label>
            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={busy}>
                {busy ? t('auth.working') : t('mfa.confirmEnable')}
              </button>
              <button className="secondary-button" type="button" onClick={onLogout}>{t('common.logout')}</button>
            </div>
          </form>
        )}
      </section>
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
        <LoginBrand subtitle={t('cpw.subtitle')} />
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
        <LoginBrand subtitle={t('pend.subtitle')} />
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
  // Non-null once a password login is challenged for a second factor: { token }.
  // No session cookie exists yet — the token is the only pre-session state.
  const [mfa, setMfa] = useState(null)
  const [code, setCode] = useState('')
  // Account-recovery sub-view: showForgot toggles the request form; forgotDone holds
  // the generic confirmation once a request is submitted.
  const [showForgot, setShowForgot] = useState(false)
  const [forgotId, setForgotId] = useState('')
  const [forgotDone, setForgotDone] = useState('')

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
      const res = await apiRequest(path, { method: 'POST', body: payload })
      const outcome = resultOf(res) || {}
      if (outcome.mfaRequired && outcome.mfaToken) {
        // Password verified, but a second factor is required. No session was issued;
        // switch the card to the factor step and hold the one-time challenge token.
        //
        // mfaMethods says which factors this account actually holds. An older server
        // omits it, so absent means TOTP — the behaviour before security keys existed.
        // A browser that cannot do WebAuthn has 'webauthn' filtered out here rather than
        // being offered a button that throws.
        const offered = Array.isArray(outcome.mfaMethods) && outcome.mfaMethods.length
          ? outcome.mfaMethods.filter(m => m !== 'webauthn' || webauthnSupported())
          : ['totp']
        setMfa({
          token: outcome.mfaToken,
          methods: offered,
          // Pick automatically when there is only one way in; otherwise let the user
          // choose, since we cannot know which factor they have to hand.
          method: offered.length === 1 ? offered[0] : null
        })
        setCode('')
        setBusy(false)
        return
      }
      onAuthed()
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  // submitMfa completes a challenged login: exchange the token + code for the
  // session. A bad code keeps us on this step (the token survives) with an inline
  // error; an expired/exhausted token drops back to the password form.
  const submitMfa = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await apiRequest('/api/login/mfa', { method: 'POST', body: { mfaToken: mfa.token, code } })
      onAuthed()
    } catch (err) {
      setError(err.message)
      if (err.status === 401 && /expired/i.test(err.message || '')) {
        setMfa(null)
      }
    } finally {
      setBusy(false)
    }
  }

  // submitWebauthn completes a challenged login with a security key. Two legs, because the
  // authenticator can only sign a challenge the server issued: fetch it, sign it, return it.
  // The challenge token is spent server-side only once the assertion verifies, so a
  // cancelled or failed attempt leaves the user able to try again (or switch to a code).
  const submitWebauthn = async () => {
    setBusy(true)
    setError('')
    try {
      const options = resultOf(await apiRequest('/api/login/mfa/webauthn/begin', {
        method: 'POST',
        body: { mfaToken: mfa.token }
      }))
      const credential = await getAssertion(options)
      await apiRequest('/api/login/mfa/webauthn/finish', {
        method: 'POST',
        body: { mfaToken: mfa.token, credential }
      })
      onAuthed()
    } catch (err) {
      setError(err?.name ? describeCeremonyError(err, t) : err.message)
      // An expired challenge token cannot be retried — send them back to the password form
      // rather than leaving a key prompt that can no longer succeed.
      if (err.status === 401 && /expired/i.test(err.message || '')) {
        setMfa(null)
      }
    } finally {
      setBusy(false)
    }
  }

  // submitForgot posts an account-recovery request. The response is deliberately
  // generic (no account-enumeration oracle); `mailEnabled` only reflects whether the
  // deployment has an SMTP relay, so we can tailor the confirmation wording.
  const openForgot = () => { setShowForgot(true); setError(''); setForgotDone(''); setForgotId(form.username || '') }
  const closeForgot = () => { setShowForgot(false); setError(''); setForgotDone('') }
  const submitForgot = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await apiRequest('/api/login/forgot', { method: 'POST', body: { username: forgotId } })
      const mailEnabled = (resultOf(res) || {}).mailEnabled
      setForgotDone(mailEnabled ? t('forgot.doneMail') : t('forgot.doneAdmin'))
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-layout">
      <section className="auth-panel">
        <LoginBrand subtitle={t('auth.subAdmin')} />
        {mfa && !mfa.method ? (
          // Both factor kinds are enrolled and we cannot know which one the user has to
          // hand, so ask. Only reached when there is a genuine choice — a single available
          // method is selected automatically.
          <div className="auth-form">
            <p className="message warning">{t('mfa.choosePrompt')}</p>
            {error && <div className="message danger">{error}</div>}
            {mfa.methods.includes('webauthn') && (
              <button className="primary-button" type="button" onClick={() => { setError(''); setMfa({ ...mfa, method: 'webauthn' }) }}>
                <span className="btn-icon"><Ico n="key" sz={15} /> {t('mfa.useKey')}</span>
              </button>
            )}
            {mfa.methods.includes('totp') && (
              <button className="secondary-button" type="button" onClick={() => { setError(''); setMfa({ ...mfa, method: 'totp' }) }}>
                {t('mfa.useCode')}
              </button>
            )}
            <button className="quiet-link" type="button" onClick={() => { setMfa(null); setError(''); setCode('') }}>{t('mfa.backToLogin')}</button>
          </div>
        ) : mfa && mfa.method === 'webauthn' ? (
          <div className="auth-form">
            <p className="message warning">{t('mfa.keyPrompt')}</p>
            {error && <div className="message danger">{error}</div>}
            <button className="primary-button" disabled={busy} type="button" onClick={submitWebauthn}>
              {busy ? t('webauthn.waiting') : t('mfa.useKey')}
            </button>
            {mfa.methods.includes('totp') && (
              <button className="quiet-link" type="button" onClick={() => { setError(''); setMfa({ ...mfa, method: 'totp' }) }}>
                {t('mfa.useCodeInstead')}
              </button>
            )}
            <button className="quiet-link" type="button" onClick={() => { setMfa(null); setError(''); setCode('') }}>{t('mfa.backToLogin')}</button>
          </div>
        ) : mfa ? (
          <form className="auth-form" onSubmit={submitMfa}>
            <p className="message warning">{t('mfa.challengePrompt')}</p>
            {error && <div className="message danger">{error}</div>}
            <label>
              {t('mfa.code')}
              <input autoComplete="one-time-code" inputMode="numeric" autoFocus value={code} placeholder="123456" onChange={event => setCode(event.target.value)} />
            </label>
            <button className="primary-button" disabled={busy} type="submit">{busy ? t('auth.working') : t('mfa.verify')}</button>
            {mfa.methods?.includes('webauthn') && (
              <button className="quiet-link" type="button" onClick={() => { setError(''); setMfa({ ...mfa, method: 'webauthn' }) }}>
                {t('mfa.useKeyInstead')}
              </button>
            )}
            <button className="quiet-link" type="button" onClick={() => { setMfa(null); setError(''); setCode('') }}>{t('mfa.backToLogin')}</button>
          </form>
        ) : showForgot ? (
          <form className="auth-form" onSubmit={submitForgot}>
            <p className="message warning">{t('forgot.intro')}</p>
            {error && <div className="message danger">{error}</div>}
            {forgotDone ? (
              <>
                <div className="message success">{forgotDone}</div>
                <button className="primary-button" type="button" onClick={closeForgot}>{t('mfa.backToLogin')}</button>
              </>
            ) : (
              <>
                <label>
                  {t('auth.username')}
                  <input autoComplete="username" autoFocus value={forgotId} onChange={event => setForgotId(event.target.value)} />
                </label>
                <button className="primary-button" disabled={busy} type="submit">{busy ? t('auth.working') : t('forgot.submit')}</button>
                <button className="quiet-link" type="button" onClick={closeForgot}>{t('mfa.backToLogin')}</button>
              </>
            )}
          </form>
        ) : (
        <>
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
          {mode === 'login' && (
            <button className="quiet-link" type="button" onClick={openForgot}>{t('forgot.link')}</button>
          )}
          {redirectProviders.length > 0 && (
            <div className="oauth-row">
              {redirectProviders.map(p => (
                <a key={p.key} className="quiet-link" href={`/api/login/${encodeURIComponent(p.key)}`}>{p.displayName || p.key}</a>
              ))}
            </div>
          )}
        </form>
        </>
        )}
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
// RowAvatar renders a small circular avatar for a user row (uploaded picture, or an
// initials fallback) with an optional inline camera button that lets a superadmin set
// that user's picture. Each instance owns its cache-busting version so one upload
// refreshes only its own row.
function RowAvatar({ userId, email, canEdit, onToast }) {
  const t = useT()
  const [ver, setVer] = useState(() => Date.now())
  const [hasImg, setHasImg] = useState(true)
  const [busy, setBusy] = useState(false)
  const fileRef = useRef(null)
  const initial = ((email || '?').trim()[0] || '?').toUpperCase()

  const pick = () => fileRef.current && fileRef.current.click()
  const onFile = async event => {
    const file = event.target.files && event.target.files[0]
    event.target.value = ''
    if (!file) return
    setBusy(true)
    try {
      const dataUrl = await resizeImageToDataUrl(file, 256)
      await apiRequest(`/api/profile/avatar/${userId}`, { method: 'POST', body: { dataUrl } })
      setHasImg(true)
      setVer(Date.now())
      onToast?.(t('user.photoSet'), 'success')
    } catch (err) { onToast?.(err.message === 'IMAGE_READ_ERROR' ? t('profile.photoReadError') : err.message, 'error') } finally { setBusy(false) }
  }

  return (
    <span className="row-avatar-wrap">
      <span className="row-avatar">
        {hasImg
          ? <img className="row-avatar-img" src={`${apiBase}/api/profile/avatar/${userId}?v=${ver}`} alt="" onError={() => setHasImg(false)} />
          : <span className="row-avatar-initial">{initial}</span>}
      </span>
      {canEdit && (
        <button type="button" className="row-avatar-edit" onClick={pick} disabled={busy} title={t('user.setPhoto')} aria-label={t('user.setPhoto')}>
          <Ico n="camera" sz={10} />
        </button>
      )}
      <input ref={fileRef} type="file" accept="image/*" hidden onChange={onFile} />
    </span>
  )
}

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

  const endSessions = async u => {
    // Ending YOUR OWN sessions from this page includes the one you are using: the admin revoke
    // deliberately spares nothing (that is right when the account is somebody else's), unlike
    // the self-service "sign out everywhere else" on the Profile page, which spares the current
    // session. So the question has to say so. The old wording — "They will have to sign in
    // again" — quietly assumed the account belonged to somebody else, and a screen check
    // pressing it on the administrator's own row signed the operator straight out.
    const self = currentEmail && u.email === currentEmail
    if (!window.confirm(t(self ? 'user.endSessionsConfirmSelf' : 'user.endSessionsConfirm'))) return
    setBusy(true)
    try {
      const res = await apiRequest(`/api/session-admin/user/${u.id}/revoke`, { method: 'POST', body: {} })
      const count = (resultOf(res) || {}).revoked || 0
      onToast?.(t('user.sessionsEnded', { n: count }), 'success')
    } catch (err) {
      setError(err.message)
      onToast?.(err.message, 'error')
    } finally {
      setBusy(false)
    }
  }

  const columns = [
    { key: 'id', label: t('f.id') },
    {
      key: 'email',
      label: t('user.colUser'),
      render: (value, u) => (
        <div className="user-cell">
          <RowAvatar userId={u.id} email={u.email} canEdit={canEdit} onToast={onToast} />
          <span className="user-cell-text">
            {value || `#${u.id}`}
            {u.email === stockEmail ? <span className="status-pill off" style={{ marginLeft: 6 }}>{t('user.stock')}</span> : null}
          </span>
        </div>
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
            {/* Ending sessions without disabling the account is the right response to a
                lost laptop or a shared password: the person keeps their access, the
                stolen cookie stops working. Disabling already ends sessions on its own. */}
            <button type="button" className="secondary-button" onClick={() => endSessions(u)} disabled={busy || !canEdit} title={t('user.endSessions')}>{t('user.endSessions')}</button>
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

// ---------------------------------------------------------------------------
// Apps: registration guidance
//
// Registering a relying app is a four-step job spanning three tables
// (app_registry -> app_auth_config -> app_redirect_uri), and almost every value
// entered here has to be matched *exactly* by the other side — the relying app's
// own SSO config. Getting one of them wrong fails at runtime with a terse
// "client is not registered" / "redirect_uri is not registered", far away from
// this screen. So the editor below is written as a guided walkthrough rather than
// a bare CRUD sheet: each field explains what it is and where it surfaces at
// runtime, the values are validated as they are typed, and a live panel renders
// the exact endpoints and config block the operator has to hand to the app.
// ---------------------------------------------------------------------------

// App codes become the `appCode` claim in issued tokens and the default client_id,
// so they are held to a slug shape: leading letter, then lowercase/digits/dashes.
const APP_CODE_PATTERN = /^[a-z][a-z0-9-]{1,62}$/

// DEFAULT_CALLBACK_PATH is where the suite's own relying apps mount their OAuth
// callback (see myseliasan's sso.redirectPath), so it is the suggested default.
const DEFAULT_CALLBACK_PATH = '/api/auth/callback'

// Identity of the exported SSO bundle. A relying app's importer checks `kind` before
// touching its own config, so a stray JSON file cannot be mistaken for one of these;
// `version` lets a future field addition stay readable by an older importer.
const SSO_BUNDLE_KIND = 'myidsan.sso.client'
const SSO_BUNDLE_VERSION = 1

// slugifyAppCode converts free text (a pasted title) into a candidate app code.
function slugifyAppCode(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 63)
}

// providerBaseUrl is the base URL relying apps use to reach this myidsan instance.
// The admin SPA is served from the same origin as the API, so the browser's origin
// is the answer unless the build points the SPA at a separate API host.
function providerBaseUrl() {
  const base = apiBase || (typeof window === 'undefined' ? '' : window.location.origin)
  return String(base).replace(/\/+$/, '')
}

function trimTrailingSlash(value) {
  return String(value || '').trim().replace(/\/+$/, '')
}

// copyToClipboard writes a value to the clipboard, falling back to a hidden
// textarea + execCommand where the async clipboard API is blocked (plain http).
async function copyToClipboard(value) {
  const text = String(value == null ? '' : value)
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch (err) {
    try {
      const area = document.createElement('textarea')
      area.value = text
      area.setAttribute('readonly', 'readonly')
      area.style.position = 'fixed'
      area.style.opacity = '0'
      document.body.appendChild(area)
      area.select()
      const ok = document.execCommand('copy')
      area.remove()
      return ok
    } catch (fallbackErr) {
      return false
    }
  }
}

// CopyButton copies a value and flips to a short "copied" state so the operator
// gets feedback without a toast interrupting the form.
function CopyButton({ value, disabled }) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const timer = useRef(null)
  useEffect(() => () => clearTimeout(timer.current), [])
  const copy = async () => {
    const ok = await copyToClipboard(value)
    if (!ok) {
      return
    }
    setCopied(true)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setCopied(false), 1600)
  }
  return (
    <button className="copy-button" type="button" onClick={copy} disabled={disabled || !String(value || '').trim()} title={t('app.copyTip')}>
      {copied ? t('app.copied') : t('app.copy')}
    </button>
  )
}

// InfoTip is the (i) affordance that carries this screen's guidance. The explanations
// are long — deliberately, they are the whole point of the redesign — but a wall of
// permanent prose is a wall nobody reads, so each one hides behind its own icon and
// opens only when asked. Click to toggle; Escape or a click elsewhere closes it.
function InfoTip({ text, align = 'start' }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const holder = useRef(null)

  useEffect(() => {
    if (!open) {
      return undefined
    }
    const onDocDown = event => {
      if (holder.current && !holder.current.contains(event.target)) {
        setOpen(false)
      }
    }
    const onKey = event => {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDocDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!text) {
    return null
  }
  return (
    <span className="info-tip" ref={holder}>
      <button
        className={open ? 'info-tip-btn open' : 'info-tip-btn'}
        type="button"
        onClick={() => setOpen(value => !value)}
        aria-expanded={open}
        aria-label={t('app.infoTip')}
        title={t('app.infoTip')}
      >
        i
      </button>
      {open && <span className={`info-tip-bubble ${align}`} role="note">{text}</span>}
    </span>
  )
}

// GuideField wraps one input with the guidance that makes it answerable. The
// explanation lives behind an (i) next to the label; the worked example becomes the
// input's own placeholder, so the shape of a valid value is visible without costing
// a line of prose. Only validation stays permanently on screen — that has to be seen.
function GuideField({ id, label, hint, example, required, issue, children }) {
  const t = useT()
  // Push `example` down as the placeholder when the control is a plain input that
  // does not already set one (the client-secret row, for instance, brings its own).
  const control = example && React.isValidElement(children) && children.type === 'input' && !children.props.placeholder
    ? React.cloneElement(children, { placeholder: example })
    : children
  return (
    <div className={issue?.level === 'error' ? 'guide-field has-error' : 'guide-field'}>
      <label className="guide-label" htmlFor={id}>
        {label}
        {required && <span className="guide-required" title={t('app.requiredTip')}>*</span>}
        <InfoTip text={hint} />
      </label>
      {control}
      {issue && <p className={issue.level === 'error' ? 'guide-issue error' : 'guide-issue warn'}>{issue.text}</p>}
    </div>
  )
}

// GuideCheck is the checkbox equivalent of GuideField: the toggle and its label on
// one line, its explanation behind the same (i).
function GuideCheck({ label, hint, checked, onChange }) {
  return (
    <div className="guide-check">
      <label className="checkbox-field">
        <input type="checkbox" checked={checked} onChange={onChange} />
        {label}
      </label>
      <InfoTip text={hint} />
    </div>
  )
}

// GuideCallout is the collapsible primer at the top of the editor. It defaults to
// open while registering a new app (when the operator most needs it) and stays
// closed for an app that already exists.
function GuideCallout({ title, open, onToggle, children }) {
  return (
    <section className={open ? 'guide-callout open' : 'guide-callout'}>
      <button className="guide-callout-head" type="button" onClick={onToggle} aria-expanded={open}>
        <span className="guide-callout-icon" aria-hidden="true">{open ? '−' : '?'}</span>
        <strong>{title}</strong>
      </button>
      {open && <div className="guide-callout-body">{children}</div>}
    </section>
  )
}

// StepRail shows where the operator is in the four-step registration. Steps are
// derived from data (does an SSO client exist? are there redirect URIs?) rather
// than from a wizard cursor, so it stays honest for half-configured apps too.
function StepRail({ steps }) {
  return (
    <ol className="step-rail">
      {steps.map((step, index) => (
        <li key={step.key} className={`step-rail-item ${step.state}`}>
          <span className="step-rail-num" aria-hidden="true">{step.state === 'done' ? '✓' : index + 1}</span>
          <span className="step-rail-text">
            <strong>{step.title}</strong>
            <span>{step.hint}</span>
          </span>
        </li>
      ))}
    </ol>
  )
}

// PreviewRow is one copyable value in the integration panel. The panel is narrow, so
// its per-value notes hide behind the same (i) and open toward the label.
function PreviewRow({ label, value, hint, mono = true }) {
  return (
    <div className="preview-row">
      <div className="preview-row-head">
        <span className="preview-row-label">
          {label}
          <InfoTip text={hint} align="end" />
        </span>
        <CopyButton value={value} />
      </div>
      <code className={mono ? 'preview-value' : 'preview-value plain'}>{value}</code>
    </div>
  )
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
              apps={apps}
              onCreated={code => loadApps(code)}
              onSaved={() => loadApps()}
              onDeleted={() => { setSelectedId(null); loadApps() }}
            />
          ) : (
            <div className="apps-welcome">
              <h2>{t('app.welcomeTitle')}</h2>
              <p>{t('app.selectPrompt')}</p>
              <ol className="apps-welcome-steps">
                <li><strong>{t('app.step1Title')}</strong> {t('app.step1Hint')}</li>
                <li><strong>{t('app.step2Title')}</strong> {t('app.step2Hint')}</li>
                <li><strong>{t('app.step3Title')}</strong> {t('app.step3Hint')}</li>
                <li><strong>{t('app.step4Title')}</strong> {t('app.step4Hint')}</li>
              </ol>
              <button className="primary-button" type="button" onClick={() => setSelectedId('new')}>{t('app.newApp')}</button>
            </div>
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
  isActive: true
}

// AppDetail is the right pane of the Apps master-detail: it edits one relying app
// inline (registry fields) plus its SSO client (auth config + redirect URIs). It is
// remounted (keyed by selection) when a different app is picked, so its forms reset
// cleanly. The client secret is write-only — the API returns only whether one is set.
//
// `apps` is the sibling list, used to catch a duplicate code/audience here rather
// than letting the unique-key violation come back from the database as a 500.
function AppDetail({ accessList, app, apps = [], onCreated, onSaved, onDeleted }) {
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
  // While registering, the audience trails the code (the suite's convention is that
  // they match) until the operator edits it — an app whose `aud` differs from its
  // code is legal, it just has to be said explicitly.
  const [audienceLinked, setAudienceLinked] = useState(isNew)
  // The primer is the one long read on the page. It opens by itself only on a truly
  // first registration (no apps exist yet); after that the operator knows the drill
  // and it stays a one-click header rather than a wall to scroll past.
  const [showPrimer, setShowPrimer] = useState(isNew && apps.length === 0)
  const [showUriRules, setShowUriRules] = useState(false)
  const [showTrouble, setShowTrouble] = useState(false)
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

  // ---- validation ---------------------------------------------------------
  // Code and audience are unique keys on app_registry (ukey1/ukey2), so a clash is
  // caught here against the loaded list instead of surfacing as a database error.
  const codeIssue = useMemo(() => {
    const code = appForm.code.trim()
    if (!code) {
      return null
    }
    if (!APP_CODE_PATTERN.test(code)) {
      return { level: 'error', text: t('app.codeInvalid') }
    }
    const clash = apps.some(row => Number(row.id) !== Number(appForm.id) && String(row.code || '').toLowerCase() === code.toLowerCase())
    return clash ? { level: 'error', text: t('app.codeTaken') } : null
  }, [appForm.code, appForm.id, apps, t])

  const audienceIssue = useMemo(() => {
    const audience = appForm.audience.trim()
    if (!audience) {
      return null
    }
    if (/\s/.test(audience)) {
      return { level: 'error', text: t('app.audienceSpaces') }
    }
    const clash = apps.some(row => Number(row.id) !== Number(appForm.id) && String(row.audience || '').toLowerCase() === audience.toLowerCase())
    return clash ? { level: 'error', text: t('app.audienceTaken') } : null
  }, [appForm.audience, appForm.id, apps, t])

  const baseUrlIssue = useMemo(() => {
    const raw = appForm.baseUrl.trim()
    if (!raw) {
      return null
    }
    let parsed
    try {
      parsed = new URL(raw)
    } catch (err) {
      return { level: 'error', text: t('app.baseUrlInvalid') }
    }
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return { level: 'error', text: t('app.baseUrlScheme') }
    }
    if (parsed.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)) {
      return { level: 'warn', text: t('app.baseUrlInsecure') }
    }
    return null
  }, [appForm.baseUrl, t])

  const appFormBlocked = Boolean(codeIssue?.level === 'error' || audienceIssue?.level === 'error' || baseUrlIssue?.level === 'error')

  // ---- integration preview -------------------------------------------------
  // The values the *relying app* has to be configured with. Rendered live while the
  // form is being filled in, so the operator can see what they are committing to.
  const provider = providerBaseUrl()
  const previewClientId = (authForm.clientId || appForm.code || 'your-app').trim()
  const previewAudience = (appForm.audience || appForm.code || 'your-app').trim()
  const previewBase = trimTrailingSlash(appForm.baseUrl) || 'https://app.example.com'
  const suggestedUri = `${previewBase}${DEFAULT_CALLBACK_PATH}`
  const previewUri = uris.find(row => row.isActive)?.redirectUri || suggestedUri
  const authorizeUrl = `${provider}/api/auth/authorize?response_type=code&client_id=${encodeURIComponent(previewClientId)}`
    + `&audience=${encodeURIComponent(previewAudience)}&redirect_uri=${encodeURIComponent(previewUri)}&state=RANDOM`
  const tokenUrl = `${provider}/api/auth/token`
  const configSnippet = JSON.stringify({
    sso: {
      issuer: 'myidsan',
      audience: previewAudience,
      providerBaseUrl: provider,
      clientId: previewClientId,
      clientSecret: '<paste the generated secret>',
      redirectBaseUrl: previewBase,
      redirectPath: DEFAULT_CALLBACK_PATH,
      sessionTtlSeconds: Number(authForm.sessionTtlSeconds) || 259200
    }
  }, null, 2)

  // ---- export bundle -------------------------------------------------------
  // The same values as a machine-readable file a relying app can import, so an
  // operator never has to retype a client ID or an audience across two consoles.
  //
  // The plaintext secret exists ONLY in this browser tab and only right after
  // Generate — myidsan stores a hash and the API never returns it. So the export
  // can carry the secret when one was just generated, and must be honest about
  // its absence otherwise rather than shipping a file that silently fails to work.
  const secretForExport = String(authForm.clientSecret || '').trim()
  const buildExportBundle = () => JSON.stringify({
    kind: SSO_BUNDLE_KIND,
    version: SSO_BUNDLE_VERSION,
    exportedAt: new Date().toISOString(),
    appCode: appForm.code,
    appTitle: appForm.title,
    secretIncluded: Boolean(secretForExport),
    sso: {
      issuer: 'myidsan',
      audience: previewAudience,
      providerBaseUrl: provider,
      clientId: previewClientId,
      clientSecret: secretForExport,
      redirectBaseUrl: previewBase,
      redirectPath: DEFAULT_CALLBACK_PATH,
      sessionTtlSeconds: Number(authForm.sessionTtlSeconds) || 259200
    }
  }, null, 2)

  const exportBundle = () => {
    downloadText(`${appForm.code || 'app'}-sso.json`, buildExportBundle())
    setNotice(secretForExport ? t('app.exportedWithSecret') : t('app.exportedNoSecret'))
  }

  const steps = [
    {
      key: 'register',
      title: t('app.step1Title'),
      hint: t('app.step1Hint'),
      state: isNew ? 'current' : 'done'
    },
    {
      key: 'client',
      title: t('app.step2Title'),
      hint: t('app.step2Hint'),
      state: config ? 'done' : (isNew ? 'todo' : 'current')
    },
    {
      key: 'redirect',
      title: t('app.step3Title'),
      hint: t('app.step3Hint'),
      state: uris.length > 0 ? 'done' : (!isNew && config ? 'current' : 'todo')
    },
    {
      key: 'connect',
      title: t('app.step4Title'),
      hint: t('app.step4Hint'),
      state: !isNew && config && uris.length > 0 ? 'current' : 'todo'
    }
  ]

  const setCode = value => {
    setAppForm(current => ({
      ...current,
      code: value,
      audience: audienceLinked ? value : current.audience
    }))
  }

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

  const codeSuggestion = isNew && !appForm.code.trim() ? slugifyAppCode(appForm.title) : ''

  return (
    <div className="app-detail">
      <div className="app-detail-head">
        <h2>{isNew ? t('app.newApp') : (appForm.title || appForm.code)}</h2>
        <p className="app-detail-lede">{isNew ? t('app.newLede') : t('app.editLede')}</p>
      </div>
      {isSystem && <p className="cert-hint">{t('app.systemNote')}</p>}
      {error && <div className="message danger">{error}</div>}
      {notice && <div className="message success">{notice}</div>}

      <StepRail steps={steps} />

      <GuideCallout title={t('app.primerTitle')} open={showPrimer} onToggle={() => setShowPrimer(value => !value)}>
        <p>{t('app.primerIntro')}</p>
        <ol>
          <li>{t('app.primerFlow1')}</li>
          <li>{t('app.primerFlow2')}</li>
          <li>{t('app.primerFlow3')}</li>
          <li>{t('app.primerFlow4')}</li>
        </ol>
        <p>{t('app.primerMatch')}</p>
      </GuideCallout>

      <div className="app-guide-grid">
        <div className="app-guide-main">
          <div className="detail-heading">
            <h3>{t('app.sectionIdentity')}</h3>
            <span className="step-badge">{t('app.stepOf', { n: 1 })}</span>
            <InfoTip text={t('app.identityLede')} />
          </div>

          <form className="record-form" onSubmit={saveApp}>
            <GuideField id="app-title" label={t('f.title')} hint={t('app.titleHint')} example={t('app.titleExample')} required>
              <input id="app-title" value={appForm.title} onChange={event => setAppForm({ ...appForm, title: event.target.value })} required />
            </GuideField>

            <GuideField id="app-code" label={t('app.code')} hint={isSystem ? t('app.codeLockedHint') : t('app.codeHint')} required issue={codeIssue}>
              <input
                id="app-code"
                value={appForm.code}
                onChange={event => setCode(event.target.value)}
                placeholder="fleet-console"
                spellCheck="false"
                autoComplete="off"
                required
                readOnly={isSystem}
                disabled={isSystem}
              />
            </GuideField>
            {codeSuggestion && (
              <button className="ghost-button" type="button" onClick={() => setCode(codeSuggestion)}>
                {t('app.useSuggestion', { value: codeSuggestion })}
              </button>
            )}

            <GuideField
              id="app-audience"
              label={t('app.audience')}
              hint={isSystem ? t('app.audienceLockedHint') : (audienceLinked ? t('app.audienceLinkedHint') : t('app.audienceHint'))}
              example="fleet-console"
              required
              issue={audienceIssue}
            >
              <input
                id="app-audience"
                value={appForm.audience}
                onChange={event => { setAudienceLinked(false); setAppForm({ ...appForm, audience: event.target.value }) }}
                spellCheck="false"
                autoComplete="off"
                required
                readOnly={isSystem}
                disabled={isSystem}
              />
            </GuideField>

            <GuideField id="app-base-url" label={t('app.baseUrl')} hint={t('app.baseUrlHint')} issue={baseUrlIssue}>
              <input id="app-base-url" value={appForm.baseUrl} onChange={event => setAppForm({ ...appForm, baseUrl: event.target.value })} placeholder="https://fleet.example.com" spellCheck="false" autoComplete="off" />
            </GuideField>

            <GuideField id="app-description" label={t('f.description')} hint={t('app.descriptionHint')} example={t('app.descriptionExample')}>
              <input id="app-description" value={appForm.description} onChange={event => setAppForm({ ...appForm, description: event.target.value })} />
            </GuideField>

            <GuideCheck
              label={t('f.active')}
              hint={t('app.activeHint')}
              checked={Boolean(appForm.isActive)}
              onChange={event => setAppForm({ ...appForm, isActive: event.target.checked })}
            />

            <div className="form-actions">
              <button className="primary-button" type="submit" disabled={busy || !canEdit || appFormBlocked}>{isNew ? t('app.createApp') : t('app.saveApp')}</button>
              {!isNew && !isSystem && <button className="secondary-button danger" type="button" onClick={deleteApp} disabled={busy || !canDelete}>{t('app.deleteApp')}</button>}
            </div>
            {!canEdit && <p className="guide-issue warn">{t('app.noPermission')}</p>}
          </form>
        </div>

        <aside className="app-guide-side">
          <div className="preview-card">
            <div className="preview-card-head">
              <h4>{t('app.previewTitle')}</h4>
              <InfoTip text={t('app.previewLede')} align="end" />
              <button className="secondary-button export-button" type="button" onClick={exportBundle} title={t('app.exportTip')}>
                {t('app.exportBundle')}
              </button>
            </div>
            <p className={secretForExport ? 'export-note ok' : 'export-note warn'}>
              {secretForExport ? t('app.exportSecretIncluded') : t('app.exportSecretMissing')}
            </p>
            <PreviewRow label={t('app.previewProvider')} value={provider} hint={t('app.previewProviderHint')} />
            <PreviewRow label={t('app.previewAudience')} value={previewAudience} hint={t('app.previewAudienceHint')} />
            <PreviewRow label={t('app.previewCallback')} value={previewUri} hint={t('app.previewCallbackHint')} />
            <PreviewRow label={t('app.previewAuthorize')} value={authorizeUrl} hint={t('app.previewAuthorizeHint')} />
            <PreviewRow label={t('app.previewToken')} value={tokenUrl} hint={t('app.previewTokenHint')} />
            <div className="preview-row">
              <div className="preview-row-head">
                <span className="preview-row-label">
                  {t('app.previewConfig')}
                  <InfoTip text={t('app.previewConfigHint')} align="end" />
                </span>
                <CopyButton value={configSnippet} />
              </div>
              <pre className="preview-code">{configSnippet}</pre>
            </div>
          </div>
        </aside>
      </div>

      {isNew ? (
        <div className="next-steps">
          <div className="detail-heading">
            <h3>{t('app.nextTitle')}</h3>
            <InfoTip text={t('app.saveFirst')} />
          </div>
          <ol className="next-steps-list compact">
            <li><strong>{t('app.step2Title')}</strong><InfoTip text={t('app.step2Detail')} /></li>
            <li><strong>{t('app.step3Title')}</strong><InfoTip text={t('app.step3Detail')} /></li>
            <li><strong>{t('app.step4Title')}</strong><InfoTip text={t('app.step4Detail')} /></li>
          </ol>
        </div>
      ) : (
        <>
          <hr className="detail-divider" />
          <div className="detail-heading">
            <h3>{t('app.ssoClient')}</h3>
            <span className="step-badge">{t('app.stepOf', { n: 2 })}</span>
            <span className={config ? 'status-pill on' : 'status-pill off'}>{config ? t('app.ssoConfigured') : t('app.noSsoClient')}</span>
            <InfoTip text={t('app.ssoLede')} />
          </div>
          {!config && <div className="message">{t('app.ssoNext')}</div>}
          <form className="record-form" onSubmit={saveConfig}>
            <GuideField id="sso-client-id" label={t('app.clientId')} hint={t('app.clientIdHint')} example={appForm.code || 'fleet-console'} required>
              <input id="sso-client-id" value={authForm.clientId} onChange={event => setAuthForm({ ...authForm, clientId: event.target.value })} spellCheck="false" autoComplete="off" required />
            </GuideField>

            <GuideField id="sso-client-secret" label={t('app.clientSecret')} hint={config?.hasClientSecret ? t('app.secretRotateHint') : t('app.secretHint')}>
              <div className="secret-row">
                <input
                  id="sso-client-secret"
                  type={secretRevealed ? 'text' : 'password'}
                  value={authForm.clientSecret}
                  onChange={event => setAuthForm({ ...authForm, clientSecret: event.target.value })}
                  placeholder={config?.hasClientSecret ? t('app.secretKeep') : t('app.secretSet')}
                  spellCheck="false"
                  autoComplete="new-password"
                />
                <button className="secondary-button" type="button" onClick={generateSecret} disabled={busy} title={t('app.generateTip')}>{t('app.generate')}</button>
                {authForm.clientSecret && <CopyButton value={authForm.clientSecret} />}
              </div>
            </GuideField>

            <div className="two-col">
              <GuideField id="sso-code-ttl" label={t('app.authCodeTtl')} hint={t('app.authCodeTtlHint')} example="300">
                <input id="sso-code-ttl" type="number" min="0" value={authForm.authCodeTtlSeconds} onChange={event => setAuthForm({ ...authForm, authCodeTtlSeconds: event.target.value })} />
              </GuideField>
              <GuideField id="sso-access-ttl" label={t('app.accessTtl')} hint={t('app.accessTtlHint')} example="900">
                <input id="sso-access-ttl" type="number" min="0" value={authForm.accessTokenTtlSeconds} onChange={event => setAuthForm({ ...authForm, accessTokenTtlSeconds: event.target.value })} />
              </GuideField>
            </div>
            <GuideField id="sso-session-ttl" label={t('app.sessionTtl')} hint={t('app.sessionTtlHint')} example="259200">
              <input id="sso-session-ttl" type="number" min="0" value={authForm.sessionTtlSeconds} onChange={event => setAuthForm({ ...authForm, sessionTtlSeconds: event.target.value })} />
            </GuideField>

            {/* Require PKCE, Allow refresh tokens and the refresh-token TTL used to sit
                here. All three persisted a value that no code path read: the authorize
                endpoint never parsed code_challenge and the token endpoint rejects
                grant_type=refresh_token, so an operator ticking "Require PKCE" was told a
                security control was on when it was not. They return, wired, in the OIDC
                conformance phase — see docs/MYIDSAN_PRODUCTIZATION_PLAN.md phases 5.3/5.4. */}
            <GuideCheck
              label={t('f.active')}
              hint={t('app.ssoActiveHint')}
              checked={Boolean(authForm.isActive)}
              onChange={event => setAuthForm({ ...authForm, isActive: event.target.checked })}
            />
            <button className="primary-button" type="submit" disabled={busy || !canSso}>{config ? t('app.saveSso') : t('app.createSso')}</button>
            {!canSso && <p className="guide-issue warn">{t('app.noSsoPermission')}</p>}
          </form>

          <div className="sso-uris">
            <div className="detail-heading">
              <h3>{t('app.redirectUris')}</h3>
              <span className="step-badge">{t('app.stepOf', { n: 3 })}</span>
              <InfoTip text={t('app.redirectLede')} />
            </div>
            {!config ? (
              <p className="message">{t('app.createSsoFirst')}</p>
            ) : (
              <>
                <GuideCallout title={t('app.redirectRulesTitle')} open={showUriRules} onToggle={() => setShowUriRules(value => !value)}>
                  <ul>
                    <li>{t('app.redirectRule1')}</li>
                    <li>{t('app.redirectRule2')}</li>
                    <li>{t('app.redirectRule3')}</li>
                  </ul>
                </GuideCallout>
                <div className="permission-add">
                  <input value={newUri} onChange={event => setNewUri(event.target.value)} placeholder={suggestedUri} spellCheck="false" autoComplete="off" disabled={busy} />
                  <button className="secondary-button" onClick={addUri} type="button" disabled={busy || !newUri.trim()}>{t('app.addUri')}</button>
                </div>
                {!newUri.trim() && !uris.some(row => row.redirectUri === suggestedUri) && (
                  <button className="ghost-button" type="button" onClick={() => setNewUri(suggestedUri)}>{t('app.useSuggestion', { value: suggestedUri })}</button>
                )}
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

          <div className="handoff-block">
            <div className="detail-heading">
              <h3>{t('app.sectionConnect')}</h3>
              <span className="step-badge">{t('app.stepOf', { n: 4 })}</span>
              <InfoTip text={t('app.connectLede')} />
            </div>
            <ol className="next-steps-list compact">
              <li><strong>{t('app.connect1Title')}</strong><InfoTip text={t('app.connect1Detail')} /></li>
              <li><strong>{t('app.connect2Title')}</strong><InfoTip text={t('app.connect2Detail')} /></li>
              <li><strong>{t('app.connect3Title')}</strong><InfoTip text={t('app.connect3Detail')} /></li>
            </ol>
            <GuideCallout title={t('app.troubleTitle')} open={showTrouble} onToggle={() => setShowTrouble(value => !value)}>
              <ul>
                <li><code>client is not registered</code> — {t('app.trouble1')}</li>
                <li><code>redirect_uri is not registered</code> — {t('app.trouble2')}</li>
                <li><code>audience not registered for client</code> — {t('app.trouble3')}</li>
                <li><code>client secret not valid</code> — {t('app.trouble4')}</li>
              </ul>
            </GuideCallout>
          </div>

          <div className="sso-certs">
            <div className="detail-heading">
              <h3>{t('app.clientCert')}</h3>
              <span className="step-badge optional">{t('app.optional')}</span>
              <InfoTip text={t('app.certIssuerNote')} />
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
    // Deleting from the toolbar removed every selected row with no confirmation at all,
    // on Groups, Roles and Endpoints alike — while deleting a single app one screen over
    // did ask. The unguarded path was the multi-row one, and these rows are roles and
    // endpoint grants: removing them silently revokes access.
    if (!window.confirm(t('crud.confirmDeleteN', { n: items.length }))) {
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

// resizeImageToDataUrl loads a picked image file, center-crops it to a square and
// scales it down to `size`px, returning a compact JPEG data URL. Doing the resize in
// the browser keeps the stored avatar tiny and means the server needs no image codec
// (important for the air-gapped, dependency-light build).
function resizeImageToDataUrl(file, size = 256) {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      URL.revokeObjectURL(url)
      try {
        const canvas = document.createElement('canvas')
        canvas.width = size
        canvas.height = size
        const ctx = canvas.getContext('2d')
        const scale = Math.max(size / img.width, size / img.height) // cover
        const w = img.width * scale
        const h = img.height * scale
        ctx.drawImage(img, (size - w) / 2, (size - h) / 2, w, h)
        resolve(canvas.toDataURL('image/jpeg', 0.85))
      } catch (err) { reject(err) }
    }
    // Untranslated sentinel — the promise has no i18n context, so callers translate
    // this specific failure (via `profile.photoReadError`) at the catch site.
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('IMAGE_READ_ERROR')) }
    img.src = url
  })
}

// ProfilePage is the self-service account surface: it shows who is signed in and lets
// the user set a profile picture, change their own password and manage their second
// factor (TOTP). It acts entirely on the caller's own account (auth-only routes, never
// RBAC-gated), and is reachable from the account chip in the side rail.
function ProfilePage({ currentEmail, roleLabel, onToast }) {
  const t = useT()
  const [status, setStatus] = useState(null)      // {enrolled, confirmedAt, label, recoveryRemaining}
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [enroll, setEnroll] = useState(null)       // {secret, otpauthUri, qrPngBase64}
  const [label, setLabel] = useState('')
  const [code, setCode] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState(null) // shown ONCE after confirm/regenerate
  const [disarm, setDisarm] = useState({ open: false, password: '', code: '' })
  // Self-service change-password (distinct from the forced must-change screen).
  const [pw, setPw] = useState({ current: '', next: '', confirm: '' })
  const [pwBusy, setPwBusy] = useState(false)
  const [pwError, setPwError] = useState('')
  // Avatar: an <img> tries the endpoint; onError flips to an initials fallback. A
  // version counter busts the image cache after upload/remove.
  const [avatarVer, setAvatarVer] = useState(() => Date.now())
  const [hasAvatar, setHasAvatar] = useState(true)
  const [avatarBusy, setAvatarBusy] = useState(false)
  const fileRef = useRef(null)
  const initial = ((currentEmail || '?').trim()[0] || '?').toUpperCase()
  const displayName = (currentEmail || '').split('@')[0] || (currentEmail || '')

  const pickAvatar = () => fileRef.current && fileRef.current.click()
  const onAvatarFile = async event => {
    const file = event.target.files && event.target.files[0]
    event.target.value = ''
    if (!file) return
    setAvatarBusy(true)
    try {
      const dataUrl = await resizeImageToDataUrl(file, 256)
      await apiRequest('/api/profile/avatar', { method: 'POST', body: { dataUrl } })
      setHasAvatar(true)
      setAvatarVer(Date.now())
      onToast?.(t('profile.photoUpdated'), 'success')
    } catch (err) { onToast?.(err.message === 'IMAGE_READ_ERROR' ? t('profile.photoReadError') : (err.message || t('profile.photoError')), 'error') } finally { setAvatarBusy(false) }
  }
  const removeAvatar = async () => {
    setAvatarBusy(true)
    try {
      await apiRequest('/api/profile/avatar', { method: 'DELETE' })
      setHasAvatar(false)
      setAvatarVer(Date.now())
      onToast?.(t('profile.photoRemoved'), 'success')
    } catch (err) { onToast?.(err.message, 'error') } finally { setAvatarBusy(false) }
  }

  const changePassword = async event => {
    event.preventDefault()
    setPwError('')
    if (pw.next !== pw.confirm) { setPwError(t('cpw.noMatch')); return }
    setPwBusy(true)
    try {
      await apiRequest('/api/login/default/change-password', { method: 'POST', body: { currentPassword: pw.current, newPassword: pw.next } })
      setPw({ current: '', next: '', confirm: '' })
      onToast?.(t('cpw.changed'), 'success')
    } catch (err) { setPwError(err.message) } finally { setPwBusy(false) }
  }

  const load = async () => {
    setLoading(true)
    try {
      const res = await apiRequest('/api/mfa')
      setStatus(resultOf(res) || { enrolled: false })
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [])

  const beginEnroll = async () => {
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/mfa/enroll', { method: 'POST', body: { label } })
      setEnroll(resultOf(res))
      setCode('')
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const confirmEnroll = async event => {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/mfa/enroll/verify', { method: 'POST', body: { code } })
      setRecoveryCodes(resultOf(res)?.recoveryCodes || [])
      setEnroll(null); setCode('')
      onToast?.(t('mfa.enabledToast'), 'success')
      await load()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const regenerate = async () => {
    const entered = window.prompt(t('mfa.confirmCodePrompt'))
    if (!entered) return
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/mfa/recovery', { method: 'POST', body: { code: entered } })
      setRecoveryCodes(resultOf(res)?.recoveryCodes || [])
      await load()
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusy(false) }
  }

  const disable = async event => {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      await apiRequest('/api/mfa', { method: 'DELETE', body: { password: disarm.password, code: disarm.code } })
      setDisarm({ open: false, password: '', code: '' })
      setRecoveryCodes(null)
      onToast?.(t('mfa.disabledToast'), 'success')
      await load()
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusy(false) }
  }

  const enrolled = status?.enrolled

  return (
    <PageFrame title={t('profile.title')} subtitle={t('profile.subtitle')}>
      <div className="profile-grid">

        <section className="profile-hero">
          <div className="profile-avatar-wrap">
            <div className="profile-avatar">
              {hasAvatar ? (
                <img
                  className="profile-avatar-img"
                  src={`${apiBase}/api/profile/avatar?v=${avatarVer}`}
                  alt=""
                  onError={() => setHasAvatar(false)}
                />
              ) : (
                <span className="profile-avatar-initial">{initial}</span>
              )}
            </div>
            <button type="button" className="profile-avatar-edit" onClick={pickAvatar} disabled={avatarBusy} title={t('profile.changePhoto')} aria-label={t('profile.changePhoto')}>
              <Ico n="camera" sz={15} />
            </button>
            <input ref={fileRef} type="file" accept="image/*" hidden onChange={onAvatarFile} />
          </div>
          <div className="profile-identity">
            <div className="profile-name">{displayName}</div>
            <div className="profile-meta"><Ico n="mail" sz={14} /> {currentEmail || '—'}</div>
            <span className="profile-role-badge"><Ico n="shield" sz={13} /> {roleLabel || ''}</span>
            <div className="profile-photo-actions">
              <button type="button" className="quiet-link" onClick={pickAvatar} disabled={avatarBusy}>
                <Ico n="upload" sz={13} /> {t('profile.changePhoto')}
              </button>
              {hasAvatar && (
                <button type="button" className="quiet-link" onClick={removeAvatar} disabled={avatarBusy}>
                  <Ico n="trash" sz={13} /> {t('profile.removePhoto')}
                </button>
              )}
            </div>
          </div>
        </section>

        <section className="profile-card">
          {/* A password is a lock; the key glyph belongs to the Security keys card below. */}
          <div className="profile-card-head"><span className="profile-card-icon"><Ico n="lock" sz={16} /></span><h2>{t('cpw.change')}</h2></div>
          <form className="record-form" onSubmit={changePassword}>
            {pwError && <div className="message danger">{pwError}</div>}
            <label>
              {t('cpw.current')}
              <input type="password" autoComplete="current-password" value={pw.current} onChange={event => setPw({ ...pw, current: event.target.value })} />
            </label>
            <div className="two-col">
              <label>
                {t('cpw.new')}
                <input type="password" autoComplete="new-password" value={pw.next} onChange={event => setPw({ ...pw, next: event.target.value })} />
              </label>
              <label>
                {t('cpw.confirm')}
                <input type="password" autoComplete="new-password" value={pw.confirm} onChange={event => setPw({ ...pw, confirm: event.target.value })} />
              </label>
            </div>
            <div>
              <button className="primary-button" disabled={pwBusy} type="submit">{pwBusy ? t('cpw.saving') : t('cpw.change')}</button>
            </div>
          </form>
        </section>

        <section className="profile-card">
          <div className="profile-card-head"><span className="profile-card-icon"><Ico n="shield-check" sz={16} /></span><h2>{t('mfa.title')}</h2></div>
          <p className="auth-hint" style={{ marginTop: 0 }}>{t('mfa.subtitle')}</p>
        {error && <div className="message danger">{error}</div>}

        {recoveryCodes && (
          <div className="message warning" style={{ display: 'grid', gap: 8 }}>
            <strong>{t('mfa.recoveryTitle')}</strong>
            <span>{t('mfa.recoveryHint')}</span>
            <div className="mfa-recovery-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0,1fr))', gap: 6, fontFamily: 'monospace' }}>
              {recoveryCodes.map(rc => <span key={rc}>{rc}</span>)}
            </div>
            <div>
              <button className="quiet-link" type="button" onClick={() => navigator.clipboard?.writeText(recoveryCodes.join('\n'))}>{t('mfa.copyCodes')}</button>
              <button className="quiet-link" type="button" onClick={() => setRecoveryCodes(null)}>{t('mfa.dismiss')}</button>
            </div>
          </div>
        )}

        {loading ? (
          <p className="auth-hint">{t('common.loading')}</p>
        ) : enroll ? (
          <form className="record-form" onSubmit={confirmEnroll}>
            <p className="auth-hint">{t('mfa.scanHint')}</p>
            <img alt={t('mfa.qrAlt')} src={`data:image/png;base64,${enroll.qrPngBase64}`} width={200} height={200} style={{ background: '#fff', padding: 8, borderRadius: 8 }} />
            <p className="auth-hint">{t('mfa.manualEntry')}: <code>{enroll.secret}</code></p>
            <label>
              {t('mfa.code')}
              <input autoComplete="one-time-code" inputMode="numeric" value={code} placeholder="123456" onChange={event => setCode(event.target.value)} />
            </label>
            <div>
              <button className="primary-button" disabled={busy} type="submit">{busy ? t('auth.working') : t('mfa.confirmEnable')}</button>
              <button className="quiet-link" type="button" onClick={() => { setEnroll(null); setError('') }}>{t('common.cancel')}</button>
            </div>
          </form>
        ) : enrolled ? (
          <div className="record-form">
            <div className="message success">{t('mfa.activeStatus', { remaining: status.recoveryRemaining })}</div>
            <div>
              <button className="secondary-button" disabled={busy} type="button" onClick={regenerate}>{t('mfa.regenerate')}</button>
              <button className="secondary-button danger" disabled={busy} type="button" onClick={() => setDisarm({ open: true, password: '', code: '' })}>{t('mfa.disable')}</button>
            </div>
            {disarm.open && (
              <form className="record-form" onSubmit={disable} style={{ marginTop: 12, borderTop: '1px solid var(--ui-border, #d6dee7)', paddingTop: 12 }}>
                <p className="auth-hint">{t('mfa.disableHint')}</p>
                <label>
                  {t('cpw.current')}
                  <input type="password" autoComplete="current-password" value={disarm.password} onChange={event => setDisarm({ ...disarm, password: event.target.value })} />
                </label>
                <label>
                  {t('mfa.code')}
                  <input autoComplete="one-time-code" inputMode="numeric" value={disarm.code} onChange={event => setDisarm({ ...disarm, code: event.target.value })} />
                </label>
                <div>
                  <button className="secondary-button danger" disabled={busy} type="submit">{busy ? t('auth.working') : t('mfa.confirmDisable')}</button>
                  <button className="quiet-link" type="button" onClick={() => setDisarm({ open: false, password: '', code: '' })}>{t('common.cancel')}</button>
                </div>
              </form>
            )}
          </div>
        ) : (
          <div className="record-form">
            <div className="message warning">{t('mfa.notEnrolled')}</div>
            <label>
              {t('mfa.deviceLabel')}
              <input value={label} placeholder={t('mfa.deviceLabelHint')} onChange={event => setLabel(event.target.value)} />
            </label>
            <div>
              <button className="primary-button" disabled={busy} type="button" onClick={beginEnroll}>{busy ? t('auth.working') : t('mfa.enable')}</button>
            </div>
          </div>
        )}
        </section>

        <WebAuthnKeys onToast={onToast} />

        <SessionList onToast={onToast} />
      </div>
    </PageFrame>
  )
}

// SessionList is the self-service "where am I signed in" panel on the Profile page.
//
// This is the screen a person needs when a laptop goes missing, so it is deliberately NOT
// behind the RBAC permission matrix — /api/session is auth-only for the same reason.
// Sessions the server reports as no longer active are still listed, greyed out, because
// "this device signed in last Tuesday and that session has since ended" is useful history;
// only live ones offer a sign-out control.
function SessionList({ onToast }) {
  const t = useT()
  const [rows, setRows] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const res = await apiRequest('/api/session')
      setRows(resultOf(res) || [])
      setError('')
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [])

  const revoke = async row => {
    // Ending the session you are using logs you out immediately, so it is worth a
    // confirmation the other rows do not need.
    if (row.current && !window.confirm(t('session.confirmCurrent'))) return
    setBusyId(row.sessionId)
    try {
      await apiRequest(`/api/session/${encodeURIComponent(row.sessionId)}`, { method: 'DELETE' })
      if (row.current) {
        // The cookie backing this page is now dead; anything else would 401.
        window.location.reload()
        return
      }
      onToast?.(t('session.ended'), 'success')
      await load()
    } catch (err) {
      setError(err.message)
      onToast?.(err.message, 'error')
    } finally {
      setBusyId('')
    }
  }

  const revokeOthers = async () => {
    if (!window.confirm(t('session.confirmOthers'))) return
    setBusyId('all')
    try {
      const res = await apiRequest('/api/session/revoke-all', { method: 'POST', body: {} })
      const count = (resultOf(res) || {}).revoked || 0
      onToast?.(t('session.endedOthers', { n: count }), 'success')
      await load()
    } catch (err) {
      setError(err.message)
      onToast?.(err.message, 'error')
    } finally {
      setBusyId('')
    }
  }

  const live = rows.filter(row => row.active)

  return (
    <section className="profile-section">
      <h2>{t('session.title')}</h2>
      <p className="guide-note">{t('session.subtitle')}</p>
      {error && <div className="message danger">{error}</div>}
      {loading && <p className="guide-note">{t('common.loading')}</p>}

      {!loading && !rows.length && <p className="guide-note">{t('session.empty')}</p>}

      {!loading && rows.length > 0 && (
        <>
          <ul className="session-list">
            {rows.map(row => (
              <li key={row.sessionId} className={row.active ? 'session-row' : 'session-row ended'}>
                <div className="session-main">
                  <div className="session-device">
                    {row.userAgent || t('session.unknownDevice')}
                    {row.current && <span className="pill pill-ok">{t('session.current')}</span>}
                    {!row.active && <span className="pill">{t('session.endedLabel')}</span>}
                  </div>
                  <div className="session-meta">
                    {row.ipAddress || '—'}
                    {row.lastSeenAt ? ` · ${t('session.lastSeen')} ${new Date(row.lastSeenAt * 1000).toLocaleString()}` : ''}
                  </div>
                </div>
                {row.active && (
                  <button
                    className="secondary-button danger"
                    type="button"
                    disabled={Boolean(busyId)}
                    onClick={() => revoke(row)}
                  >
                    {row.current ? t('session.endThis') : t('session.end')}
                  </button>
                )}
              </li>
            ))}
          </ul>
          {live.length > 1 && (
            <div>
              <button className="secondary-button" type="button" disabled={Boolean(busyId)} onClick={revokeOthers}>
                {t('session.endOthers')}
              </button>
            </div>
          )}
        </>
      )}
    </section>
  )
}

// ResetRequestsPage is the superadmin account-recovery queue: pending forgot-password
// requests from local accounts. Resolving one issues a one-time temporary password
// (shown once, must-change on first use) for the operator to hand over out-of-band;
// dismissing closes a bogus request without touching the account.
function ResetRequestsPage({ onToast }) {
  const t = useT()
  const [rows, setRows] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState(0)
  const [issued, setIssued] = useState(null) // { email, temporaryPassword }

  const load = async () => {
    setLoading(true)
    try {
      const res = await apiRequest('/api/password-reset')
      setRows(rowsOf(res))
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [])

  const resolve = async row => {
    setBusyId(row.id); setError('')
    try {
      const res = await apiRequest(`/api/password-reset/${row.id}/resolve`, { method: 'POST' })
      const temp = (resultOf(res) || {}).temporaryPassword
      setIssued({ email: row.email, temporaryPassword: temp })
      await load()
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusyId(0) }
  }

  const dismiss = async row => {
    setBusyId(row.id); setError('')
    try {
      await apiRequest(`/api/password-reset/${row.id}/dismiss`, { method: 'POST' })
      onToast?.(t('reset.dismissed'), 'success')
      await load()
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusyId(0) }
  }

  return (
    <PageFrame title={t('reset.title')} subtitle={t('reset.subtitle')}>
      <div className="app-detail">
        {error && <div className="message danger">{error}</div>}

        {issued && (
          <div className="message warning" style={{ display: 'grid', gap: 8 }}>
            <strong>{t('reset.issuedTitle', { email: issued.email })}</strong>
            <span>{t('reset.issuedHint')}</span>
            <code style={{ fontSize: 16, letterSpacing: '0.05em' }}>{issued.temporaryPassword}</code>
            <div>
              <button className="quiet-link" type="button" onClick={() => navigator.clipboard?.writeText(issued.temporaryPassword)}>{t('mfa.copyCodes')}</button>
              <button className="quiet-link" type="button" onClick={() => setIssued(null)}>{t('mfa.dismiss')}</button>
            </div>
          </div>
        )}

        {loading ? (
          <p className="auth-hint">{t('common.loading')}</p>
        ) : rows.length === 0 ? (
          <div className="empty-state">{t('reset.empty')}</div>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th>{t('reset.colAccount')}</th>
                <th>{t('reset.colRequested')}</th>
                <th>{t('reset.colIp')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map(row => (
                <tr key={row.id}>
                  <td>{row.email}</td>
                  <td>{formatDateTime(row.requestedAt)}</td>
                  <td>{String(row.requestIp || '').replace(/^ip:/, '')}</td>
                  <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                    <button className="secondary-button" disabled={busyId === row.id} type="button" onClick={() => resolve(row)}>{t('reset.resolve')}</button>
                    <button className="quiet-link" disabled={busyId === row.id} type="button" onClick={() => dismiss(row)}>{t('reset.dismiss')}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </PageFrame>
  )
}

// BackupPage is the superadmin disaster-recovery surface. Export writes an encrypted
// archive of the identity store; restore rebuilds this server from one.
//
// The restore side is deliberately two-step — the file is previewed (manifest only,
// nothing written) before anything is applied — because a replace-mode restore removes
// every account currently on this server, including the operator running it.
// AuditPage is the superadmin security trail: who signed in, what changed, from where.
//
// It is read-only by construction — the API exposes no write route — so this page has no
// edit or delete affordance at all. Filters are applied server-side and the CSV export
// reuses exactly the same query string, so an export always contains what the screen was
// showing rather than a silently different set.
function AuditPage({ onToast }) {
  const t = useT()
  const [rows, setRows] = useState([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filters, setFilters] = useState({ action: '', outcome: '', actorEmail: '', from: '', to: '' })

  const pageSize = 50

  // One place builds the query, so the table and the export can never diverge.
  const buildQuery = (extra = {}) => {
    const params = new URLSearchParams()
    Object.entries({ ...filters, ...extra }).forEach(([key, value]) => {
      if (String(value || '').trim()) params.set(key, String(value).trim())
    })
    return params.toString()
  }

  const load = async (nextOffset = 0) => {
    setLoading(true)
    setError('')
    try {
      const res = await apiRequest(`/api/audit?limit=${pageSize}&offset=${nextOffset}&${buildQuery()}`)
      setRows(rowsOf(res))
      setTotal(Number(pageOf(res).totalCnt || 0))
      setOffset(nextOffset)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load(0) }, [])

  const exportCsv = () => {
    // A plain navigation rather than fetch+blob: the response is an attachment and the
    // session cookie rides along, so the browser saves it without buffering it in memory.
    window.location.href = `${apiBase}/api/audit/export.csv?${buildQuery()}`
    onToast?.(t('audit.exportStarted'), 'success')
  }

  const outcomeTone = outcome => (outcome === 'denied' ? 'danger' : outcome === 'error' ? 'warn' : 'ok')
  const outcomeLabel = outcome => (
    outcome === 'denied' ? t('audit.outcomeDenied') : outcome === 'error' ? t('audit.outcomeError') : outcome === 'success' ? t('audit.outcomeSuccess') : outcome
  )

  return (
    <PageFrame title={t('audit.title')} subtitle={t('audit.subtitle')}>
      <div className="app-detail">
        {error && <div className="message danger">{error}</div>}

        <section className="guide-block">
          <div className="audit-filters">
            <label>
              {t('audit.filterAction')}
              <input
                value={filters.action}
                onChange={e => setFilters({ ...filters, action: e.target.value })}
                placeholder="login.failure"
              />
            </label>
            <label>
              {t('audit.filterOutcome')}
              <select value={filters.outcome} onChange={e => setFilters({ ...filters, outcome: e.target.value })}>
                <option value="">{t('audit.any')}</option>
                <option value="success">{t('audit.outcomeSuccess')}</option>
                <option value="denied">{t('audit.outcomeDenied')}</option>
                <option value="error">{t('audit.outcomeError')}</option>
              </select>
            </label>
            <label>
              {t('audit.filterActor')}
              <input
                value={filters.actorEmail}
                onChange={e => setFilters({ ...filters, actorEmail: e.target.value })}
                placeholder="alice@corp.local"
              />
            </label>
            <label>
              {t('audit.filterFrom')}
              <input type="date" value={filters.from} onChange={e => setFilters({ ...filters, from: e.target.value })} />
            </label>
            <label>
              {t('audit.filterTo')}
              <input type="date" value={filters.to} onChange={e => setFilters({ ...filters, to: e.target.value })} />
            </label>
          </div>
          <div className="form-actions">
            <button className="primary-button" type="button" onClick={() => load(0)} disabled={loading}>
              {t('audit.apply')}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={() => { setFilters({ action: '', outcome: '', actorEmail: '', from: '', to: '' }); setTimeout(() => load(0), 0) }}
              disabled={loading}
            >
              {t('audit.clear')}
            </button>
            <button className="secondary-button" type="button" onClick={exportCsv} disabled={loading || !rows.length}>
              {t('audit.export')}
            </button>
          </div>
        </section>

        {loading && <p className="guide-note">{t('common.loading')}</p>}

        {!loading && !rows.length && <p className="guide-note">{t('audit.empty')}</p>}

        {!loading && rows.length > 0 && (
          <>
            <div className="table-wrap">
              <table className="data-table audit-table">
                <thead>
                  <tr>
                    <th>{t('audit.colWhen')}</th>
                    <th>{t('audit.colAction')}</th>
                    <th>{t('audit.colActor')}</th>
                    <th>{t('audit.colTarget')}</th>
                    <th>{t('audit.colWhere')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map(row => (
                    <tr key={row.id}>
                      <td className="nowrap">{row.createdAt ? new Date(row.createdAt * 1000).toLocaleString() : '—'}</td>
                      <td>
                        <code>{row.action}</code>{' '}
                        <span className={`pill pill-${outcomeTone(row.outcome)}`}>{outcomeLabel(row.outcome)}</span>
                        {row.detail && <div className="audit-detail">{row.detail}</div>}
                      </td>
                      <td>{row.actorEmail || <span className="muted">{t('audit.anonymous')}</span>}</td>
                      <td>{row.targetType ? `${row.targetType}${row.targetId ? ` · ${row.targetId}` : ''}` : '—'}</td>
                      <td className="nowrap">
                        {row.clientIp || '—'}
                        {row.userAgent && <div className="audit-detail" title={row.userAgent}>{row.userAgent.slice(0, 40)}</div>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={() => load(Math.max(0, offset - pageSize))} disabled={offset === 0 || loading}>
                {t('audit.prev')}
              </button>
              <span className="guide-note">
                {t('audit.range', { from: offset + 1, to: offset + rows.length, total })}
              </span>
              <button className="secondary-button" type="button" onClick={() => load(offset + pageSize)} disabled={offset + rows.length >= total || loading}>
                {t('audit.next')}
              </button>
            </div>
          </>
        )}
      </div>
    </PageFrame>
  )
}

function BackupPage({ onToast }) {
  const t = useT()
  const { runElevated, stepUpPending, confirmStepUp, cancelStepUp } = useStepUp()
  const [sections, setSections] = useState([])
  const [selected, setSelected] = useState([])
  const [passphrase, setPassphrase] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const [file, setFile] = useState(null)          // { name, base64 }
  const [manifest, setManifest] = useState(null)  // preview result
  const [restorePass, setRestorePass] = useState('')
  const [mode, setMode] = useState('replace')
  const [result, setResult] = useState(null)

  const load = async () => {
    try {
      const res = await apiRequest('/api/backup/sections')
      const list = resultOf(res) || []
      setSections(list)
      setSelected(list.filter(s => s.count > 0).map(s => s.id))
    } catch (err) { setError(err.message) }
  }
  useEffect(() => { load() }, [])

  const toggle = id => setSelected(cur => cur.includes(id) ? cur.filter(x => x !== id) : [...cur, id])

  const doExport = async event => {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      // Export is step-up gated: the wrapper re-runs this request once the operator
      // has re-authenticated, so the passphrase they typed is not lost.
      const res = await runElevated(() => apiRequest('/api/backup/export', {
        method: 'POST',
        body: { sections: selected, passphrase }
      }))
      const out = resultOf(res) || {}
      // Hand the file straight to the browser; it never touches disk server-side.
      const bytes = atob(out.dataBase64 || '')
      const buf = new Uint8Array(bytes.length)
      for (let i = 0; i < bytes.length; i += 1) buf[i] = bytes.charCodeAt(i)
      const url = URL.createObjectURL(new Blob([buf], { type: 'application/octet-stream' }))
      const a = document.createElement('a')
      a.href = url
      a.download = out.filename || 'myidsan-backup.idbackup'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
      setPassphrase('')
      onToast?.(t('backup.exported'), 'success')
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusy(false) }
  }

  const pickFile = async event => {
    const picked = event.target.files && event.target.files[0]
    setManifest(null); setResult(null); setError('')
    if (!picked) { setFile(null); return }
    const buf = await picked.arrayBuffer()
    let binary = ''
    const bytes = new Uint8Array(buf)
    for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i])
    setFile({ name: picked.name, base64: btoa(binary) })
  }

  const doPreview = async () => {
    if (!file) return
    setBusy(true); setError('')
    try {
      const res = await apiRequest('/api/backup/preview', {
        method: 'POST',
        body: { dataBase64: file.base64, passphrase: restorePass }
      })
      setManifest(resultOf(res))
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusy(false) }
  }

  const doRestore = async () => {
    if (!file || !manifest) return
    if (!window.confirm(t('backup.confirmRestore'))) return
    setBusy(true); setError('')
    try {
      const res = await runElevated(() => apiRequest('/api/backup/restore', {
        method: 'POST',
        body: { dataBase64: file.base64, passphrase: restorePass, mode }
      }))
      setResult(resultOf(res))
      // Every session was just dropped, this one included. Say so rather than letting
      // the next request fail with a confusing 401.
      onToast?.(t('backup.restoredSignOut'), 'success')
    } catch (err) { setError(err.message); onToast?.(err.message, 'error') } finally { setBusy(false) }
  }

  return (
    <PageFrame title={t('backup.title')} subtitle={t('backup.subtitle')}>
      <StepUpPrompt open={stepUpPending} onConfirm={confirmStepUp} onCancel={cancelStepUp} />
      <div className="app-detail">
        {error && <div className="message danger">{error}</div>}

        <section className="guide-block">
          <h2>{t('backup.exportHeading')}</h2>
          <p className="guide-note">{t('backup.exportIntro')}</p>
          <form onSubmit={doExport} className="record-form">
            <div className="backup-sections">
              {sections.map(s => (
                <label key={s.id} className="guide-check">
                  <input
                    type="checkbox"
                    checked={selected.includes(s.id)}
                    onChange={() => toggle(s.id)}
                    disabled={busy}
                  />
                  <span>{t(`backup.section.${s.id}`)} <em>({s.count})</em></span>
                </label>
              ))}
            </div>
            <GuideField id="backup-pass" label={t('backup.passphrase')} hint={t('backup.passphraseHint')}>
              <input
                id="backup-pass"
                type="password"
                autoComplete="new-password"
                value={passphrase}
                onChange={e => setPassphrase(e.target.value)}
                placeholder={t('backup.passphrasePlaceholder')}
              />
            </GuideField>
            <button className="primary-button" type="submit" disabled={busy || !selected.length || passphrase.length < 12}>
              {t('backup.exportButton')}
            </button>
          </form>
        </section>

        <section className="guide-block">
          <h2>{t('backup.restoreHeading')}</h2>
          <p className="guide-note warn">{t('backup.restoreWarning')}</p>

          <GuideField id="backup-file" label={t('backup.file')} hint={t('backup.fileHint')}>
            <input id="backup-file" type="file" accept=".idbackup" onChange={pickFile} disabled={busy} />
          </GuideField>

          {file && (
            <>
              <GuideField id="restore-pass" label={t('backup.passphrase')} hint={t('backup.restorePassHint')}>
                <input
                  id="restore-pass"
                  type="password"
                  autoComplete="off"
                  value={restorePass}
                  onChange={e => setRestorePass(e.target.value)}
                />
              </GuideField>
              <button className="secondary-button" type="button" onClick={doPreview} disabled={busy || !restorePass}>
                {t('backup.previewButton')}
              </button>
            </>
          )}

          {manifest && (
            <div className="backup-manifest">
              <h3>{t('backup.manifestHeading')}</h3>
              <dl>
                <dt>{t('backup.manifestVersion')}</dt><dd>{manifest.appVersion}</dd>
                <dt>{t('backup.manifestCreated')}</dt>
                <dd>{manifest.createdAt ? new Date(manifest.createdAt * 1000).toLocaleString() : '—'}</dd>
                <dt>{t('backup.manifestContents')}</dt>
                <dd>
                  {(manifest.sections || []).map(s => (
                    <span key={s} className="pill">{t(`backup.section.${s}`)} ({(manifest.counts || {})[s] || 0})</span>
                  ))}
                </dd>
              </dl>

              <GuideField id="restore-mode" label={t('backup.mode')} hint={t('backup.modeHint')}>
                <select id="restore-mode" value={mode} onChange={e => setMode(e.target.value)} disabled={busy}>
                  <option value="replace">{t('backup.modeReplace')}</option>
                  <option value="merge">{t('backup.modeMerge')}</option>
                </select>
              </GuideField>

              <button className="primary-button danger" type="button" onClick={doRestore} disabled={busy}>
                {t('backup.restoreButton')}
              </button>
            </div>
          )}

          {result && (
            <div className="message success">
              <strong>{t('backup.restoreDone')}</strong>
              <ul>
                {Object.entries(result.restored || {}).map(([k, v]) => (
                  <li key={k}>{t(`backup.section.${k}`)}: {v}</li>
                ))}
              </ul>
              {Object.keys(result.skipped || {}).length > 0 && (
                <p className="guide-note warn">
                  {t('backup.skippedNote')} {Object.entries(result.skipped).map(([k, v]) => `${t(`backup.section.${k}`)}: ${v}`).join(', ')}
                </p>
              )}
              {result.schemaWarning && <p className="guide-note warn">{result.schemaWarning}</p>}
              <p className="guide-note">{t('backup.restoredSignOut')}</p>
            </div>
          )}
        </section>
      </div>
    </PageFrame>
  )
}


// useStepUp wraps an action that the server may refuse until the operator re-proves their
// credential. On the sentinel it opens a prompt, re-authenticates, then re-runs the SAME
// action — so the person never loses the work they were mid-way through, which is what
// makes a re-authentication gate tolerable rather than something people route around.
function useStepUp() {
  const [pending, setPending] = useState(null) // { run, resolve, reject }

  const runElevated = useCallback(action => new Promise((resolve, reject) => {
    const attempt = async () => {
      try {
        resolve(await action())
      } catch (err) {
        if (err && err.stepUpRequired) {
          setPending({ run: action, resolve, reject })
          return
        }
        reject(err)
      }
    }
    attempt()
  }), [])

  const cancel = () => {
    if (pending) pending.reject(new Error('cancelled'))
    setPending(null)
  }

  const confirm = async (password, code) => {
    // Let the caller see the credential error rather than swallowing it: a mistyped
    // password must keep the prompt open, not silently drop the action.
    await apiRequest('/api/step-up', { method: 'POST', body: { password, code } })
    const current = pending
    setPending(null)
    try {
      current.resolve(await current.run())
    } catch (err) {
      current.reject(err)
    }
  }

  return { runElevated, stepUpPending: Boolean(pending), confirmStepUp: confirm, cancelStepUp: cancel }
}

// StepUpPrompt asks for the password (and a code when a factor is enrolled) before a
// sensitive action proceeds.
function StepUpPrompt({ open, onConfirm, onCancel }) {
  const t = useT()
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) { setPassword(''); setCode(''); setError('') }
  }, [open])

  if (!open) return null

  const submit = async event => {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      await onConfirm(password, code)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="modal-layer">
      {/* Matches the EditorModal structure: .modal-layer centres, .modal-backdrop dims.
          The backdrop deliberately does NOT dismiss on click — losing a half-typed
          password to a stray click, and silently dropping the action behind it, is worse
          than requiring the explicit Cancel. */}
      <div className="modal-backdrop" />
      <section className="stepup-modal" role="dialog" aria-modal="true" aria-label={t('stepup.title')}>
        <h2>{t('stepup.title')}</h2>
        <p className="guide-note">{t('stepup.body')}</p>
        {error && <div className="message danger">{error}</div>}
        <form className="record-form" onSubmit={submit}>
          <label>
            {t('auth.password')}
            <input type="password" autoComplete="current-password" autoFocus value={password} onChange={e => setPassword(e.target.value)} />
          </label>
          <label>
            {t('stepup.code')}
            <input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={e => setCode(e.target.value)} placeholder={t('stepup.codeHint')} />
          </label>
          <div className="form-actions">
            <button className="primary-button" type="submit" disabled={busy || !password}>{busy ? t('auth.working') : t('stepup.confirm')}</button>
            <button className="secondary-button" type="button" onClick={onCancel} disabled={busy}>{t('common.cancel')}</button>
          </div>
        </form>
      </section>
    </div>
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
