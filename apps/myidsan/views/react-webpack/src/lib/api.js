import config from 'config'

const unsafeMethods = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
const apiBase = (config && config.apiUrl ? config.apiUrl : '').replace(/\/$/, '')

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
