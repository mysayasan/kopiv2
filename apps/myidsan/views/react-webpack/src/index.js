import * as React from 'react'
import * as ReactDOM from 'react-dom/client'
import {
  createBrowserRouter,
  RouterProvider
} from 'react-router-dom'
import './styles.css'

const Home = React.lazy(() => import('./views/Home'))
const App = React.lazy(() => import('./views/App'))

const routes = [
  {
    path: '/',
    element: <Home />
  },
  {
    path: '/app',
    element: <App />
  }
]

const router = createBrowserRouter(routes)

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <React.Suspense fallback={<div className="boot-screen">Loading MyIDSan</div>}>
      <RouterProvider router={router} />
    </React.Suspense>
  </React.StrictMode>
)
