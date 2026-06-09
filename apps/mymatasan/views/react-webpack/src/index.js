import * as React from "react";
import * as ReactDOM from "react-dom/client";

import {
  createBrowserRouter,
  RouterProvider,
} from "react-router-dom";

const Home = React.lazy(() => import('./views/Home'));
const App = React.lazy(() => import('./views/App'));

const routes = [
  {
    path: "/",
    element: <Home />,
  },
  {
    path: "/app",
    element: <App />,
  },
]

const router = createBrowserRouter(routes);

ReactDOM.createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>
);
