import React from 'react'

import LoginButton from './login-button'
import LogoutButton from './logout-button'

const AuthenticationButton = () => {
  const isAuthenticated = false

  return isAuthenticated ? <LogoutButton /> : <LoginButton />
}

export default AuthenticationButton
