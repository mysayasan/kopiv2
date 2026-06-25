import * as React from 'react';
import * as ReactDOM from 'react-dom/client';

const App = React.lazy(() => import('./views/App'));

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <React.Suspense fallback={null}>
      <App />
    </React.Suspense>
  </React.StrictMode>
);
