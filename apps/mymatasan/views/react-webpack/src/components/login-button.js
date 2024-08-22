import React from 'react';

const LoginButton = () => {
  return (
    <button
      className="btn btn-primary btn-block"
      onClick={() => console.log("login")}
    >
      Log In
    </button>
  );
};

export default LoginButton;