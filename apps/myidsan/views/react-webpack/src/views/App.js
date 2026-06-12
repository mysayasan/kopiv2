import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ACCESS_TIERS,
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

const ACTIVE_SECTION_COOKIE = 'myidsan_active_section'
const TABLE_STATE_PREFIX = 'myidsan_table_'
const TABLE_STATE_VERSION = 1

const dashboardSection = {
  id: 'dashboard',
  label: 'Dashboard',
  group: 'Workspace',
  order: 0,
  tone: 'steel',
  code: 'DA',
  paths: []
}

const routeCatalog = [
  { id: 'users', label: 'Users', group: 'Identity', order: 10, tone: 'blue', code: 'US', paths: ['/api/user-credential'], summary: 'Maintain credentials, profile details, and role assignments.' },
  { id: 'groups', label: 'Groups', group: 'Identity', order: 20, tone: 'teal', code: 'GR', paths: ['/api/user-group'], summary: 'Organize identity ownership and hierarchy roots.' },
  { id: 'roles', label: 'Roles', group: 'Identity', order: 30, tone: 'violet', code: 'RO', paths: ['/api/user-credential'], summary: 'Create group-scoped roles and parent role chains.' },
  { id: 'apps', label: 'Apps', group: 'Federation', order: 40, tone: 'indigo', code: 'AP', paths: ['/api/app-registry'], summary: 'Manage registered relying apps and audiences.' },
  { id: 'endpoints', label: 'Endpoints', group: 'Access Control', order: 50, tone: 'steel', code: 'EP', paths: ['/api/endpoint'], summary: 'Maintain the protected endpoint catalog.' },
  { id: 'rbac', label: 'RBAC', group: 'Access Control', order: 60, tone: 'green', code: 'RB', paths: ['/api/endpoint-rbac'], summary: 'Map endpoints to role-specific HTTP permissions.' }
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

function App() {
  const [session, setSession] = useState(() => localStorage.getItem('myidsan.session') === 'active')
  const [sessionReady, setSessionReady] = useState(false)
  const [active, setActive] = useState(() => getCookie(ACTIVE_SECTION_COOKIE) || 'dashboard')
  const [accessList, setAccessList] = useState([])
  const [sessionError, setSessionError] = useState('')

  const refreshSession = useCallback(async () => {
    try {
      const payload = await apiRequest('/api/endpoint-rbac/ep/me')
      const allowedEndpoints = rowsOf(payload)
      localStorage.setItem('myidsan.session', 'active')
      setAccessList(allowedEndpoints)
      setSession(true)
      setSessionError('')
    } catch (err) {
      localStorage.removeItem('myidsan.session')
      setAccessList([])
      setSession(false)
      if (err.status && err.status !== 401 && err.status !== 403) {
        setSessionError(err.message)
      }
    } finally {
      setSessionReady(true)
    }
  }, [])

  const visibleSections = useMemo(() => {
    return buildVisibleSections(accessList)
  }, [accessList])

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
    return <div className="boot-screen">Checking session</div>
  }

  if (!session) {
    return <AuthScreen onAuthed={handleAuthed} sessionError={sessionError} />
  }

  return (
    <div className="app-shell">
      <aside className="side-nav">
        <div className="brand-block">
          <div className="brand-mark">ID</div>
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">Identity control</div>
          </div>
        </div>
        <nav>
          {navGroups.map(group => (
            <div className="nav-group" key={group.label}>
              <div className="nav-group-label">{group.label}</div>
              {group.items.map(section => (
                <button
                  key={section.id}
                  className={active === section.id ? `nav-item active tone-${section.tone}` : `nav-item tone-${section.tone}`}
                  onClick={() => setActiveSection(section.id)}
                  type="button"
                >
                  <span className="nav-code">{section.code || initials(section.label)}</span>
                  <span className="nav-label">{section.label}</span>
                </button>
              ))}
            </div>
          ))}
        </nav>
        <button className="logout-button" onClick={handleLogout} type="button">Log out</button>
      </aside>
      <main className="main-workspace">
        {active === 'dashboard' && <Dashboard onNavigate={setActiveSection} sections={visibleSections} />}
        {!activeAllowed && activeKnown && active !== 'dashboard' && <UnauthorizedPage section={routeCatalogById[active]} onNavigate={() => setActiveSection('dashboard')} />}
        {active === 'users' && sectionAllowedById('users', accessList) && <UsersPage accessList={accessList} />}
        {active === 'groups' && sectionAllowedById('groups', accessList) && <GroupsPage accessList={accessList} />}
        {active === 'roles' && sectionAllowedById('roles', accessList) && <RolesPage accessList={accessList} />}
        {active === 'apps' && sectionAllowedById('apps', accessList) && <AppsPage accessList={accessList} />}
        {active === 'endpoints' && sectionAllowedById('endpoints', accessList) && <EndpointsPage accessList={accessList} />}
        {active === 'rbac' && sectionAllowedById('rbac', accessList) && <RbacPage accessList={accessList} />}
      </main>
    </div>
  )
}

function AuthScreen({ onAuthed, sessionError }) {
  const [mode, setMode] = useState('login')
  const [form, setForm] = useState({
    username: '',
    password: '',
    firstName: '',
    lastName: ''
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async event => {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      const path = mode === 'login'
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
          <div className="brand-mark">ID</div>
          <div>
            <div className="brand-name">MyIDSan</div>
            <div className="brand-subtitle">Identity and RBAC admin</div>
          </div>
        </div>
        <div className="segmented">
          <button className={mode === 'login' ? 'selected' : ''} onClick={() => setMode('login')} type="button">Log in</button>
          <button className={mode === 'register' ? 'selected' : ''} onClick={() => setMode('register')} type="button">Register</button>
        </div>
        <form className="auth-form" onSubmit={submit}>
          {sessionError && <div className="message warning">{sessionError}</div>}
          {error && <div className="message danger">{error}</div>}
          <label>
            Username
            <input autoComplete="username" value={form.username} onChange={event => setForm({ ...form, username: event.target.value })} />
          </label>
          <label>
            Password
            <input autoComplete={mode === 'login' ? 'current-password' : 'new-password'} type="password" value={form.password} onChange={event => setForm({ ...form, password: event.target.value })} />
          </label>
          {mode === 'register' && (
            <div className="two-col">
              <label>
                First name
                <input value={form.firstName} onChange={event => setForm({ ...form, firstName: event.target.value })} />
              </label>
              <label>
                Last name
                <input value={form.lastName} onChange={event => setForm({ ...form, lastName: event.target.value })} />
              </label>
            </div>
          )}
          <button className="primary-button" disabled={busy} type="submit">{busy ? 'Working' : mode === 'login' ? 'Log in' : 'Create account'}</button>
          <div className="oauth-row">
            <a className="quiet-link" href="/api/login/google">Google</a>
            <a className="quiet-link" href="/api/login/github">GitHub</a>
          </div>
        </form>
      </section>
    </div>
  )
}

function Dashboard({ onNavigate, sections: visibleSections }) {
  const cards = visibleSections
    .filter(section => section.id !== 'dashboard')
    .map(section => ({
      id: section.id,
      title: section.label,
      group: section.group,
      tone: section.tone,
      body: section.summary || dashboardBody(section.id)
    }))

  return (
    <PageFrame title="Identity Administration" subtitle="Configure accounts, role membership, applications, endpoints, and RBAC policy.">
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
            No RBAC-backed menu entries are available for this account.
          </div>
        )}
      </div>
    </PageFrame>
  )
}

function UnauthorizedPage({ section, onNavigate }) {
  const title = section?.label || 'Restricted Page'
  return (
    <PageFrame title="Unauthorized Access" subtitle={`Your current RBAC policy no longer allows access to ${title}.`}>
      <div className="empty-state unauthorized-state">
        <strong>{title}</strong>
        <span>This page is hidden until your role is granted the required GET permission again.</span>
        <button className="primary-button" onClick={onNavigate} type="button">Go to dashboard</button>
      </div>
    </PageFrame>
  )
}

function UsersPage({ accessList }) {
  return (
    <CrudPage
      accessList={accessList}
      title="Users"
      subtitle="User credential records and role assignments."
      resource="/api/user-credential"
      emptyItem={emptyUser}
      normalize={normalizeUser}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'email', label: 'Email' },
        { key: 'firstName', label: 'First' },
        { key: 'lastName', label: 'Last' },
        { key: 'userRoleId', label: 'Role' },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'email', label: 'Email', required: true },
        { name: 'userpwd', label: 'Password', type: 'password', placeholder: 'Set only when changing password' },
        { name: 'firstName', label: 'First name' },
        { name: 'lastName', label: 'Last name' },
        { name: 'picUrl', label: 'Picture URL' },
        { name: 'userRoleId', label: 'Role ID', type: 'number' },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
      canCreate={false}
    />
  )
}

function GroupsPage({ accessList }) {
  return (
    <CrudPage
      accessList={accessList}
      title="Groups"
      subtitle="Top-level and nested user groups."
      resource="/api/user-group"
      emptyItem={emptyGroup}
      normalize={normalizeGroup}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'title', label: 'Title' },
        { key: 'description', label: 'Description' },
        { key: 'parentId', label: 'Parent' },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'title', label: 'Title', required: true },
        { name: 'description', label: 'Description' },
        { name: 'parentId', label: 'Parent ID', type: 'number' },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
    />
  )
}

function RolesPage({ accessList }) {
  const [groupId, setGroupId] = useState('1')
  const listPath = `/api/user-credential/group/${groupId || 1}`

  return (
    <CrudPage
      accessList={accessList}
      title="Roles"
      subtitle="Role records are exposed through the current user-credential role routes."
      resource="/api/user-credential"
      listResource={listPath}
      emptyItem={emptyRole}
      normalize={normalizeRole}
      listMode={groupId ? 'default' : 'paging'}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'title', label: 'Title' },
        { key: 'groupId', label: 'Group' },
        { key: 'parentId', label: 'Parent' },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'title', label: 'Title', required: true },
        { name: 'description', label: 'Description', dtoType: 'nullableString' },
        { name: 'groupId', label: 'Group ID', type: 'number' },
        { name: 'parentId', label: 'Parent role ID', type: 'number' },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
      toolbar={(
        <label className="inline-filter">
          Group ID
          <input value={groupId} onChange={event => setGroupId(event.target.value)} placeholder="Optional" />
        </label>
      )}
    />
  )
}

function AppsPage({ accessList }) {
  return (
    <CrudPage
      accessList={accessList}
      title="Apps"
      subtitle="Registered SSO relying apps and audiences."
      resource="/api/app-registry"
      emptyItem={emptyApp}
      normalize={normalizeApp}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'code', label: 'Code' },
        { key: 'title', label: 'Title' },
        { key: 'audience', label: 'Audience' },
        { key: 'baseUrl', label: 'Base URL' },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'code', label: 'Code', required: true },
        { name: 'title', label: 'Title', required: true },
        { name: 'description', label: 'Description' },
        { name: 'baseUrl', label: 'Base URL' },
        { name: 'audience', label: 'Audience', required: true },
        { name: 'clientSecret', label: 'Client secret', type: 'password' },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
    />
  )
}

function EndpointsPage({ accessList }) {
  return (
    <CrudPage
      accessList={accessList}
      title="Endpoints"
      subtitle="App-scoped API endpoint catalog used by rate limiting and RBAC."
      resource="/api/endpoint"
      emptyItem={emptyEndpoint}
      normalize={normalizeEndpoint}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'appCode', label: 'App' },
        { key: 'host', label: 'Host' },
        { key: 'path', label: 'Path' },
        { key: 'metadata', label: 'Menu', render: menuMetadataLabel },
        { key: 'accessTier', label: 'Tier', render: tierLabel },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'title', label: 'Title', required: true },
        { name: 'description', label: 'Description' },
        { name: 'appCode', label: 'App code', required: true },
        { name: 'host', label: 'Host', required: true },
        { name: 'path', label: 'Path', required: true },
        { name: 'metadata', label: 'Menu metadata', type: 'textarea', rows: 8, placeholder: '{"menu":{"enabled":true,"id":"users","label":"Users","group":"Identity","order":10,"summary":"Maintain user access.","tone":"blue"}}' },
        { name: 'accessTier', label: 'Access tier', type: 'select', options: ACCESS_TIERS },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
    />
  )
}

function RbacPage({ accessList }) {
  return (
    <CrudPage
      accessList={accessList}
      title="RBAC"
      subtitle="HTTP method permissions by endpoint and user role."
      resource="/api/endpoint-rbac"
      emptyItem={emptyRbac}
      normalize={normalizeRbac}
      columns={[
        { key: 'id', label: 'ID' },
        { key: 'endpointAppCode', label: 'App', render: (value, row) => value || row.apiEndpointId },
        { key: 'endpointHost', label: 'Host', render: (value, row) => value || row.apiEndpointId },
        { key: 'endpointPath', label: 'Path', render: (value, row) => value || row.apiEndpointId },
        { key: 'roleTitle', label: 'Role', render: (value, row) => value || row.userRoleId },
        { key: 'canGet', label: 'GET', render: boolLabel },
        { key: 'canPost', label: 'POST', render: boolLabel },
        { key: 'canPut', label: 'PUT', render: boolLabel },
        { key: 'canDelete', label: 'DELETE', render: boolLabel },
        { key: 'isActive', label: 'Active', render: boolLabel }
      ]}
      fields={[
        { name: 'apiEndpointId', label: 'Endpoint ID', type: 'number', required: true },
        { name: 'userRoleId', label: 'Role ID', type: 'number', required: true },
        { name: 'canGet', label: 'Can GET', type: 'checkbox' },
        { name: 'canPost', label: 'Can POST', type: 'checkbox' },
        { name: 'canPut', label: 'Can PUT', type: 'checkbox' },
        { name: 'canDelete', label: 'Can DELETE', type: 'checkbox' },
        { name: 'isActive', label: 'Active', type: 'checkbox' }
      ]}
    />
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
  toolbar
}) {
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
        throw new Error('Your role cannot create records for this resource.')
      }
      if (isUpdate && !actionAccess.canEdit) {
        throw new Error('Your role cannot edit records for this resource.')
      }
      const method = isUpdate ? 'PUT' : 'POST'
      const payload = preparePayload(selected, fields)
      await apiRequest(resource, { method, body: payload })
      if (isUpdate && editorItems.length > 1) {
        setEditorItems(current => current.map((item, index) => index === editorIndex ? normalize({ ...item, ...payload }) : item))
        setNotice(`Saved ${editorIndex + 1} of ${editorItems.length}`)
      } else {
        setNotice(isUpdate ? 'Updated' : 'Created')
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
      setError('Your role cannot delete records for this resource.')
      return
    }
    setBusy(true)
    setError('')
    setNotice('')
    try {
      for (const item of items) {
        await apiRequest(`${resource}/${item.id}`, { method: 'DELETE' })
      }
      setNotice(items.length === 1 ? 'Deleted' : `Deleted ${items.length} records`)
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
            {selectedIds.length > 0 && <span className="selection-count">{selectedIds.length} selected</span>}
            {canCreate && <button className="toolbar-icon icon-new primary" onClick={openCreate} disabled={busy || !actionAccess.canCreate} type="button" title="New record" aria-label="New record" />}
            <button className="toolbar-icon icon-edit" onClick={openEditSelected} disabled={busy || !actionAccess.canEdit || selectedIds.length === 0} type="button" title="Edit selected" aria-label="Edit selected" />
            <button className="toolbar-icon icon-delete danger" onClick={removeSelected} disabled={busy || !actionAccess.canDelete || selectedIds.length === 0} type="button" title="Delete selected" aria-label="Delete selected" />
            {(filters.length > 0 || sorters.length > 0 || Number(page.offset || 0) > 0) && (
              <button className="toolbar-icon icon-clear" onClick={clearTableControls} disabled={busy} type="button" title="Clear table filters, sorting, and remembered page" aria-label="Clear table filters, sorting, and remembered page" />
            )}
            <button className={busy ? 'toolbar-icon icon-refresh spinning' : 'toolbar-icon icon-refresh'} onClick={() => load(page.offset || 0)} disabled={busy} type="button" title="Refresh" aria-label="Refresh" />
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
  const hasMultipleItems = itemCount > 1

  return (
    <div className="modal-layer">
      <button className="modal-backdrop" onClick={onClose} type="button" aria-label="Close editor" />
      <section className="editor-modal" role="dialog" aria-modal="true" aria-labelledby="editor-title">
        <div className="modal-heading">
          <div>
            <h2 id="editor-title">{value.id ? 'Edit record' : 'New record'}</h2>
            {hasMultipleItems && <p>{itemIndex + 1} / {itemCount}</p>}
          </div>
          <div className="modal-actions">
            {hasMultipleItems && (
              <div className="modal-record-pager">
                <button className="pager-icon previous" onClick={() => onNavigate(itemIndex - 1)} disabled={busy || itemIndex <= 0} type="button" title="Previous selected record" aria-label="Previous selected record" />
                <button className="pager-icon next" onClick={() => onNavigate(itemIndex + 1)} disabled={busy || itemIndex >= itemCount - 1} type="button" title="Next selected record" aria-label="Next selected record" />
              </div>
            )}
            <button className="secondary-button" onClick={onClear} disabled={busy} type="button">Clear</button>
            <button className="icon-button" onClick={onClose} disabled={busy} type="button" title="Close">Close</button>
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
  const filterCount = normalizeFilterDrafts(filter, column)
    .filter(item => String(item.value ?? '').trim() !== '')
    .length
  const sortLabel = sort?.sort === 1 ? 'ASC' : sort?.sort === 2 ? 'DESC' : 'Sort'
  const filterActive = filterCount > 0

  return (
    <div className="column-head">
      <div className="column-title-row">
        <span>{column.label}</span>
        <div className="column-actions">
          {column.filterable !== false && (
            <button
              aria-label={`Filter ${column.label}`}
              className={filterActive ? 'filter-button active' : 'filter-button'}
              onClick={event => onFilterOpen(column, event.currentTarget)}
              title={`Filter ${column.label}`}
              type="button"
            >
              {filterCount > 1 && <span className="filter-count">{filterCount}</span>}
            </button>
          )}
          <button className={sort ? 'sort-button active' : 'sort-button'} onClick={() => onSort(column.key)} type="button" title={`Sort by ${column.label}`}>
            {sortLabel}
            {sort?.index && <span>{sort.index}</span>}
          </button>
        </div>
      </div>
    </div>
  )
}

function DataTable({ rows, columns, busy, columnFilters, page, selectedIds, sorters, onFilterChange, onPage, onSelectionChange, onSort }) {
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
                <input checked={allSelected} disabled={busy || rowIds.length === 0} onChange={event => toggleAllRows(event.target.checked)} type="checkbox" title="Select all rows" aria-label="Select all rows" />
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
                <td className="empty-cell" colSpan={columns.length + 1}>No records</td>
              </tr>
            )}
            {initialLoading && (
              <TableSkeleton columns={columns.length + 1} />
            )}
            {rows.map(row => (
              <tr className={selectedSet.has(String(row.id)) ? 'selected-row' : ''} key={`${row.id}-${row.email || row.title || row.path || row.code}`}>
                <td className="select-col">
                  <input checked={selectedSet.has(String(row.id))} disabled={busy} onChange={event => toggleRow(row, event.target.checked)} type="checkbox" title="Select row" aria-label="Select row" />
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
            <span>Refreshing</span>
          </div>
        )}
      </div>
      {page && onPage && <Pager page={page} onPage={onPage} busy={busy} />}
      {openFilter && (
        <>
          <button className="filter-popover-backdrop" onClick={closeColumnFilter} type="button" aria-label="Close filter" />
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
        <button className="mini-button" onClick={onClose} type="button">Close</button>
      </div>
      <form className="filter-popover-body" onSubmit={submit}>
        {draft.map((item, index) => (
          <div className="filter-condition" key={`filter-${column.key}-${index}`}>
            <div className="filter-condition-head">
              <span>Condition {index + 1}</span>
              {draft.length > 1 && <button className="mini-button" onClick={() => removeDraft(index)} type="button">Remove</button>}
            </div>
            <label>
              Operator
              <select value={normalizeFilterCompare(item.compare, column)} onChange={event => updateDraft(index, { compare: Number(event.target.value) })}>
                {operators.map(operator => <option key={operator.value} value={operator.value}>{operator.label}</option>)}
              </select>
            </label>
            <label>
              Value
              {column.filterType === 'boolean' ? (
                <select value={item.value} onChange={event => updateDraft(index, { value: event.target.value })}>
                  <option value="">Any</option>
                  <option value="true">Yes</option>
                  <option value="false">No</option>
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
          <button className="secondary-button" onClick={addDraft} type="button">Add</button>
          <button className="secondary-button" onClick={onClear} type="button">Clear</button>
          <button className="primary-button" type="submit">Apply</button>
        </div>
      </form>
    </div>
  )
}

function RecordForm({ fields, value, onChange, onSubmit, busy, canCreate, canEdit = true }) {
  const update = (name, nextValue) => onChange({ ...value, [name]: nextValue })
  const canSubmit = value.id ? canEdit : canCreate

  return (
    <form className="record-form" onSubmit={onSubmit}>
      {Number(value.id) > 0 && (
        <label>
          ID
          <input value={value.id} disabled />
        </label>
      )}
      {fields.map(field => (
        <Field key={field.name} field={field} value={value[field.name]} onChange={nextValue => update(field.name, nextValue)} />
      ))}
      <button className="primary-button" disabled={busy || !canSubmit} type="submit">
        {busy ? 'Saving' : value.id ? 'Save changes' : 'Create'}
      </button>
    </form>
  )
}

function Field({ field, value, onChange }) {
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
      <span>Page {currentPage} / {pageCount} · {total} total</span>
      <div className="pager-controls">
        <button className="pager-icon first" disabled={busy || offset <= 0} onClick={() => onPage(0)} title="First page" type="button" aria-label="First page" />
        <button className="pager-icon previous" disabled={busy || offset <= 0} onClick={() => onPage(prev)} title="Previous page" type="button" aria-label="Previous page" />
        <label className="pager-jump">
          <input
            min="1"
            max={pageCount}
            value={pageDraft}
            onChange={event => setPageDraft(event.target.value)}
            onKeyDown={submitDraftPage}
            title="Page number"
            type="number"
          />
        </label>
        <button className="pager-icon go" disabled={busy} onClick={goToDraftPage} title="Go to page" type="button" aria-label="Go to page" />
        <button className="pager-icon next" disabled={busy || !page.hasNext} onClick={() => onPage(page.nextOffset)} title="Next page" type="button" aria-label="Next page" />
        <button className="pager-icon last" disabled={busy || offset >= last} onClick={() => onPage(last)} title="Last page" type="button" aria-label="Last page" />
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

function sectionAllowedById(sectionId, accessList) {
  return buildVisibleSections(accessList).some(section => section.id === sectionId)
}

function sectionAllowed(section, accessList) {
  if (section.id === 'dashboard') {
    return true
  }
  return section.paths.some(path => hasEndpointAccess(accessList, path, 'GET'))
}

function buildVisibleSections(accessList) {
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
    : routeCatalog.filter(section => sectionAllowed(section, accessList))

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

export default App
