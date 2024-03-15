import React from 'react'
import ReactDOM from 'react-dom/client'
import {
  Route,
  createBrowserRouter,
  RouterProvider,
  createRoutesFromElements
} from 'react-router-dom'
import Auth0ProviderWithHistory from './auth/auth0-provider-with-history'

const App = React.lazy(() => import('./app'))
const Home = React.lazy(() => import('./views/home'))
const Profile = React.lazy(() => import('./views/profile'))

const root = ReactDOM.createRoot(document.getElementById('app'))
const router = createBrowserRouter([
  {
    path: '/',
    element: <Auth0ProviderWithHistory />,
    children: [
      {
        path: 'home',
        element: <Home />
      },
      {
        path: 'profile',
        element: <Profile />
      }
    ]
  }
]);

// const router = createBrowserRouter(
//   createRoutesFromElements(<Route path='/' element={<App />}></Route>)
// )

root.render(
  <React.StrictMode>
    {/* <Auth0ProviderWithHistory> */}
      <RouterProvider router={router} />
    {/* </Auth0ProviderWithHistory> */}
  </React.StrictMode>
)
