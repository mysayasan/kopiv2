import React from 'react';
import { createRoot, hydrateRoot } from 'react-dom/client';
import App from './App.jsx';
import { LangProvider } from './i18n/index.jsx';
import './styles.css';

const container = document.getElementById('root');
const tree = (
  <React.StrictMode>
    <LangProvider>
      <App />
    </LangProvider>
  </React.StrictMode>
);

// Prerendered pages (production) ship server HTML in #root → hydrate it.
// The Vite dev server ships an empty #root → mount fresh.
if (container.hasChildNodes()) {
  hydrateRoot(container, tree);
} else {
  createRoot(container).render(tree);
}
