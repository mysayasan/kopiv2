import React from 'react'

const SignupButton = () => {
  return (
    <button
      className='btn btn-primary btn-block'
      onClick={() => console.log('signup')}
    >
      Sign Up
    </button>
  )
}

export default SignupButton
