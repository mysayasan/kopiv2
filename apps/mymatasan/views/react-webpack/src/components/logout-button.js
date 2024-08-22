import React from 'react';

const LogoutButton = () => {
  return (
    <button
      className="btn btn-danger btn-block"
      onClick={() =>
        console.log("logout")
      }
    >
      Log Out
    </button>
  );
};

export default LogoutButton;