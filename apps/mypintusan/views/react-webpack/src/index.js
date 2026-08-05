import * as React from 'react'
import * as ReactDOM from 'react-dom/client'
import './styles.css'

const App = React.lazy(() => import('./views/App'))

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <React.Suspense fallback={<div className="boot-screen">Loading MyPintuSan</div>}>
      <App />
    </React.Suspense>
  </React.StrictMode>
)
