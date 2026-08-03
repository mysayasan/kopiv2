// Browser-side WebAuthn (FIDO2 security key) helpers.
//
// The whole reason this file exists is a type mismatch that has no way of announcing itself:
// the WebAuthn browser API speaks ArrayBuffer, and JSON does not have one. The server sends
// every binary field (challenge, credential ids, user handle) as UNPADDED base64url, and
// expects the same back. Get a conversion wrong and the failure surfaces as an opaque
// "NotAllowedError" or a server-side signature mismatch, with nothing pointing at the cause
// — so the conversions live here once, rather than inline at each call site.

// b64urlToBytes decodes unpadded base64url (what Go's protocol.URLEncodedBase64 emits).
// Padding is re-added because atob() requires it.
function b64urlToBytes(value) {
  const normalized = String(value || '').replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4)
  const raw = window.atob(padded)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i += 1) out[i] = raw.charCodeAt(i)
  return out
}

// bytesToB64url encodes an ArrayBuffer as unpadded base64url for the trip back.
function bytesToB64url(buffer) {
  const bytes = new Uint8Array(buffer)
  let raw = ''
  for (let i = 0; i < bytes.length; i += 1) raw += String.fromCharCode(bytes[i])
  return window.btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// supported reports whether this browser can do WebAuthn at all. Checked before offering
// the option, so an unsupported browser gets an explanation instead of a dead button.
//
// Note the secure-context requirement: WebAuthn is unavailable over plain http except on
// localhost, which is why an install served over http on a real hostname will find the whole
// feature missing rather than merely failing.
export function webauthnSupported() {
  return typeof window !== 'undefined' &&
    !!window.PublicKeyCredential &&
    !!(navigator.credentials && navigator.credentials.create)
}

// platformAuthenticatorLikely resolves true when the device has a built-in authenticator
// (Windows Hello, Touch ID). Used only to word the prompt; never to gate anything.
export async function platformAuthenticatorLikely() {
  try {
    if (!window.PublicKeyCredential?.isUserVerifyingPlatformAuthenticatorAvailable) return false
    return await window.PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable()
  } catch {
    return false
  }
}

// createCredential runs the registration ceremony. `options` is the server's
// CredentialCreation payload verbatim (it carries a `publicKey` wrapper).
export async function createCredential(options) {
  const publicKey = { ...(options?.publicKey || options) }
  publicKey.challenge = b64urlToBytes(publicKey.challenge)
  publicKey.user = { ...publicKey.user, id: b64urlToBytes(publicKey.user?.id) }
  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map(c => ({ ...c, id: b64urlToBytes(c.id) }))
  }

  const credential = await navigator.credentials.create({ publicKey })
  if (!credential) throw new Error('no credential was created')

  // getTransports() tells the server how this key was reachable, so a later sign-in can
  // hint the right prompt. Not universally implemented, hence the guard.
  let transports = []
  try {
    if (typeof credential.response.getTransports === 'function') {
      transports = credential.response.getTransports() || []
    }
  } catch { /* optional */ }

  return {
    id: credential.id,
    rawId: bytesToB64url(credential.rawId),
    type: credential.type,
    // Present on the response but not in the base dictionary in every browser.
    authenticatorAttachment: credential.authenticatorAttachment || undefined,
    clientExtensionResults: credential.getClientExtensionResults?.() || {},
    response: {
      clientDataJSON: bytesToB64url(credential.response.clientDataJSON),
      attestationObject: bytesToB64url(credential.response.attestationObject),
      transports
    }
  }
}

// getAssertion runs the authentication ceremony. `options` is the server's
// CredentialAssertion payload verbatim.
export async function getAssertion(options) {
  const publicKey = { ...(options?.publicKey || options) }
  publicKey.challenge = b64urlToBytes(publicKey.challenge)
  if (Array.isArray(publicKey.allowCredentials)) {
    publicKey.allowCredentials = publicKey.allowCredentials.map(c => ({ ...c, id: b64urlToBytes(c.id) }))
  }

  const assertion = await navigator.credentials.get({ publicKey })
  if (!assertion) throw new Error('no assertion was produced')

  return {
    id: assertion.id,
    rawId: bytesToB64url(assertion.rawId),
    type: assertion.type,
    authenticatorAttachment: assertion.authenticatorAttachment || undefined,
    clientExtensionResults: assertion.getClientExtensionResults?.() || {},
    response: {
      clientDataJSON: bytesToB64url(assertion.response.clientDataJSON),
      authenticatorData: bytesToB64url(assertion.response.authenticatorData),
      signature: bytesToB64url(assertion.response.signature),
      // userHandle is legitimately absent for a non-resident credential.
      userHandle: assertion.response.userHandle ? bytesToB64url(assertion.response.userHandle) : ''
    }
  }
}

// describeCeremonyError turns a DOMException from the browser into something a user can act
// on. The raw names are opaque ("NotAllowedError" covers both "you cancelled" and "it timed
// out"), and the untranslated default would otherwise reach the UI.
//
// `t` is the i18n lookup; keys live under webauthn.err.*.
export function describeCeremonyError(err, t) {
  const name = err?.name || ''
  switch (name) {
    case 'NotAllowedError':
      // Genuinely ambiguous per spec — the browser deliberately does not distinguish a
      // cancellation from a timeout, to avoid leaking whether a key was present.
      return t('webauthn.err.notAllowed')
    case 'InvalidStateError':
      // The authenticator recognised itself in excludeCredentials: already enrolled here.
      return t('webauthn.err.alreadyRegistered')
    case 'SecurityError':
      // Almost always an RP ID / origin mismatch, which is a server configuration problem.
      return t('webauthn.err.security')
    case 'NotSupportedError':
      return t('webauthn.err.notSupported')
    case 'AbortError':
      return t('webauthn.err.aborted')
    case 'ConstraintError':
      // Typically user verification was required and the key cannot do it.
      return t('webauthn.err.constraint')
    default:
      return err?.message || t('webauthn.err.generic')
  }
}
