(async function () {
  const pill = document.getElementById('session-pill')
  const detail = document.getElementById('session-detail')
  const claims = document.getElementById('claims')

  async function loadSession() {
    const response = await fetch('/api/session/me', { credentials: 'same-origin' })
    if (response.status === 401 || response.status === 403) {
      window.location.href = '/api/auth/start'
      return
    }
    if (!response.ok) {
      throw new Error('Session check failed')
    }
    const data = await response.json()
    const session = data.result || {}
    pill.textContent = session.email || 'Authenticated'
    detail.textContent = `${session.name || session.email || 'Authenticated user'} is signed in through ${session.issuer || 'myidsan'}.`
    claims.textContent = JSON.stringify(session, null, 2)
  }

  document.getElementById('logout').addEventListener('click', async function () {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' })
    window.location.href = '/api/auth/start'
  })

  try {
    await loadSession()
  } catch (err) {
    pill.textContent = 'Session error'
    detail.textContent = err.message
  }
})()
