// The typed API surface for mypintusan.
//
// Everything the SPA does goes through here rather than calling apiRequest inline, so there is one
// place that knows the endpoint shapes and one place to look when the server contract changes.
import { apiRequest } from './api'

// unwrap pulls the payload out of the shared envelope. The Go side answers either
// {result: ...} for a single object or {data: {result: [...]}} for a paged list, and every caller
// in this app wants the thing itself.
function unwrap(res) {
  if (res && res.data && res.data.result !== undefined) return res.data.result
  if (res && res.result !== undefined) return res.result
  return res
}

async function get(path) {
  return unwrap(await apiRequest(path))
}

// NOTE: pass the OBJECT, not a JSON string. apiRequest stringifies the body itself, so encoding it
// here too produced a double-encoded payload and every write in this app failed with
// "cannot unmarshal string into Go value of type ..." — login, settings, doors, badges, all of it.
async function send(path, method, body) {
  return unwrap(await apiRequest(path, { method, body }))
}

// --- session ------------------------------------------------------------------------------------

export const login = (username, password) => send('/api/auth/login', 'POST', { username, password })
export const logout = () => send('/api/auth/logout', 'POST')
export const session = () => get('/api/auth/session')
export const changePassword = (currentPassword, newPassword) =>
  send('/api/auth/change-password', 'POST', { currentPassword, newPassword })

// --- doors --------------------------------------------------------------------------------------

export const listDoors = () => get('/api/doors')
export const getDoor = id => get(`/api/doors/${id}`)
// unlockDoor is the operator's remote-open. It is logged exactly like a badge, so the UI can be
// blunt about it: this is a real door opening, attributed to the signed-in user.
export const unlockDoor = id => send(`/api/doors/${id}/unlock`, 'POST')

// createDoor makes a door AND its entry reader in one call. A door with no reader is inert and a
// reader with no door drives nothing, so the server refuses to create them apart.
export const createDoor = door => send('/api/doors', 'POST', door)

export const listReaders = () => get('/api/readers')

// --- lockdown -----------------------------------------------------------------------------------

export const getLockdown = () => get('/api/lockdown')
export const setLockdown = on => send('/api/lockdown', 'POST', { lockdown: !!on })

// --- people and badges --------------------------------------------------------------------------

export const listHolders = () => get('/api/holders')
export const createHolder = holder => send('/api/holders', 'POST', holder)
export const listCredentials = holderId => get(`/api/holders/${holderId}/credentials`)
export const issueCredential = (holderId, credential) =>
  send(`/api/holders/${holderId}/credentials`, 'POST', credential)
export const revokeCredential = (holderId, credId, status, reason) =>
  send(`/api/holders/${holderId}/credentials/${credId}/revoke`, 'POST', { status, reason })

// --- the access log -----------------------------------------------------------------------------

// listEvents takes the same filters the server supports. Denials are never filtered out by
// default: a denied unknown card at 03:00 on a perimeter door is the most valuable row in the log.
export function listEvents({ limit = 200, offset = 0, doorId, holderId, decision, reason, since } = {}) {
  const q = new URLSearchParams()
  q.set('limit', String(limit))
  if (offset) q.set('offset', String(offset))
  if (doorId) q.set('doorId', String(doorId))
  if (holderId) q.set('holderId', String(holderId))
  if (decision) q.set('decision', decision)
  if (reason) q.set('reason', reason)
  if (since) q.set('since', String(since))
  return get(`/api/events?${q.toString()}`)
}

// --- settings -----------------------------------------------------------------------------------

// Settings come back with security keys REDACTED — each reader carries hasScbk/usingDefaultKey
// booleans instead of the key itself. Saving sends them back without a key, and the server carries
// the stored one forward, so the UI never has to hold a site key in memory.
export const getSettings = () => get('/api/settings/access')
export const saveSettings = settings => send('/api/settings/access', 'PUT', settings)
export const resetSettings = () => send('/api/settings/access/reset', 'POST')

// --- setup wizard -------------------------------------------------------------------------------

export const setupState = () => get('/api/setup/state')
export const completeSetup = () => send('/api/setup/complete', 'POST')

// --- display helpers ----------------------------------------------------------------------------

// DENIAL_REASONS maps the server's enumerated reasons to i18n keys. The set is closed on purpose so
// an operator, a report and an alert rule all agree on what a denial means.
export const REASON_KEYS = {
  ok: 'reason.ok',
  'unknown-credential': 'reason.unknownCredential',
  'credential-inactive': 'reason.credentialInactive',
  'credential-expired': 'reason.credentialExpired',
  'credential-revoked': 'reason.credentialRevoked',
  'holder-suspended': 'reason.holderSuspended',
  'holder-expired': 'reason.holderExpired',
  'no-grant': 'reason.noGrant',
  'out-of-schedule': 'reason.outOfSchedule',
  holiday: 'reason.holiday',
  lockdown: 'reason.lockdown',
  antipassback: 'reason.antipassback',
  'offline-cache-expired': 'reason.offlineStale',
  'offline-not-allowed': 'reason.offlineNotAllowed',
  'offline-denied': 'reason.offlineDenied',
  'secure-channel-failed': 'reason.secureChannel',
  'reader-offline': 'reason.readerOffline',
  'door-disabled': 'reason.doorDisabled',
  'bad-pin': 'reason.badPin',
  duress: 'reason.duress',
  'door-forced': 'reason.doorForced',
  'door-held-open': 'reason.doorHeldOpen',
  'door-closed': 'reason.doorClosed'
}

export function formatTime(unix) {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString()
}
