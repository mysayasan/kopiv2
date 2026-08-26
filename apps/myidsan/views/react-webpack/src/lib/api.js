import config from 'config'

const unsafeMethods = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
export const STEP_UP_REQUIRED = 'step_up_required'

export const apiBase = (config && config.apiUrl ? config.apiUrl : '').replace(/\/$/, '')

export const ACCESS_TIERS = [
  { value: 0, label: 'DevOnly' },
  { value: 1, label: 'AuthOnly' },
  { value: 2, label: 'Public' }
]

export function getCookie(name) {
  return document.cookie
    .split(';')
    .map(part => part.trim())
    .find(part => part.startsWith(`${name}=`))
    ?.split('=')
    .slice(1)
    .join('=') || ''
}

export function setCookie(name, value, options = {}) {
  const days = options.days ?? 14
  const maxAge = Math.max(0, Math.floor(days * 24 * 60 * 60))
  const sameSite = options.sameSite || 'Lax'
  const secure = window.location.protocol === 'https:' ? '; Secure' : ''
  document.cookie = `${name}=${encodeURIComponent(value)}; Path=/; Max-Age=${maxAge}; SameSite=${sameSite}${secure}`
}

export function clearCookie(name) {
  document.cookie = `${name}=; Path=/; Max-Age=0; SameSite=Lax`
}

export function csrfToken() {
  return getCookie('__Host-kopiv2_csrf') || getCookie('kopiv2_csrf')
}

// Session-lost subscribers. The app registers one that returns the SPA to the sign-in screen;
// this module owns no UI, so it only announces. Fired at most once per lost session so a page
// that fans out several parallel requests does not bounce the user several times.
let sessionLostHandlers = []
let sessionLostAnnounced = false

export function onSessionLost(handler) {
  sessionLostHandlers.push(handler)
  return () => { sessionLostHandlers = sessionLostHandlers.filter(h => h !== handler) }
}

// Called by the app once a fresh session is established, so a later loss is announced again.
export function resetSessionLost() {
  sessionLostAnnounced = false
}

function notifySessionLost() {
  if (sessionLostAnnounced) return
  sessionLostAnnounced = true
  for (const handler of sessionLostHandlers) {
    try { handler() } catch (_) { /* a broken subscriber must not swallow the original error */ }
  }
}

export async function apiRequest(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase()
  const headers = {
    Accept: 'application/json',
    ...(options.headers || {})
  }

  if (options.body !== undefined && !(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  if (unsafeMethods.has(method)) {
    const token = csrfToken()
    if (token) {
      headers['X-CSRF-Token'] = token
    }
  }

  const response = await fetch(`${apiBase}${path}`, {
    method,
    credentials: 'include',
    headers,
    body: options.body instanceof FormData
      ? options.body
      : options.body !== undefined
        ? JSON.stringify(options.body)
        : undefined
  })

  const contentType = response.headers.get('content-type') || ''
  const payload = contentType.includes('application/json') ? await response.json() : null

  if (!response.ok) {
    const message = payload?.message || `Request failed with ${response.status}`
    const err = new Error(message)
    err.status = response.status
    err.payload = payload
    // The server answers a step-up-gated action with this sentinel. Flag it here so
    // callers can open the re-authentication prompt instead of surfacing a dead-end
    // "forbidden" for an action that IS theirs to take.
    err.stepUpRequired = message === STEP_UP_REQUIRED
    // 401 means the SESSION IS GONE — expired, revoked here, or ended by an administrator.
    // Every page used to catch this like any other error and render its own empty state, so
    // the operator was left sitting in a fully drawn admin console with a small red "session
    // not active" and every screen reporting no data. The first screen check this app ever
    // had caught it on the Audit log, which cheerfully said "No events match these filters"
    // when the truth was that nobody was signed in.
    //
    // 403 is deliberately NOT included: that is "you are signed in and may not do this",
    // which belongs inline on the page — and it is how the step-up sentinel above arrives.
    if (response.status === 401) {
      notifySessionLost()
    }
    throw err
  }

  return payload
}

export function resultOf(payload) {
  return payload?.result
}

export function rowsOf(payload) {
  if (Array.isArray(payload?.data?.result)) {
    return payload.data.result
  }
  if (Array.isArray(payload?.result)) {
    return payload.result
  }
  return []
}

export function pageOf(payload) {
  return payload?.data || {
    limit: 0,
    offset: 0,
    resCnt: 0,
    totalCnt: 0,
    hasNext: false,
    nextOffset: 0
  }
}

export function queryString(params) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') {
      return
    }
    search.set(key, typeof value === 'string' ? value : JSON.stringify(value))
  })
  const value = search.toString()
  return value ? `?${value}` : ''
}

export function emptyToZero(value) {
  if (value === '' || value === null || value === undefined) {
    return 0
  }
  return Number(value)
}

export function formatDateTime(value) {
  if (!value) {
    return ''
  }
  const date = new Date(Number(value) * 1000)
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString()
}
