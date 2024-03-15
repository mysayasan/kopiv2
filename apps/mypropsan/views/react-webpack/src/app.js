import React from 'react';
import { Route, Routes } from 'react-router-dom';
import { useAuth0 } from '@auth0/auth0-react';

import { NavBar, Footer } from './components';
import { Home, Profile } from './views';
import AtomicSpinner from 'atomic-spinner';

// import './app.css';

const App = () => {
  const { isLoading } = useAuth0();

  if (isLoading) {
    return <AtomicSpinner />;
  }

  return (
    <div id="app" className="d-flex flex-column h-100">
      <NavBar />
      <div className="container flex-grow-1">
        <Routes>
          <Route path="/" exact element={ <Home /> } />
          <Route path="/profile" element={ <Profile /> } />
          {/* <Route path="/external-api" component={ExternalApi} /> */}
        </Routes>
      </div>
      <Footer />
    </div>
  );
};

export default App;