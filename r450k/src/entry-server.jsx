// SSR entry used at build time by scripts/prerender.mjs to generate one static
// HTML page per locale. Intentionally imports NO CSS (Node can't parse it).
import { renderToString } from 'react-dom/server';
import App from './App.jsx';
import { LangProvider, LOCALES, localePath } from './i18n/index.jsx';

export function render(lang) {
  const c = LOCALES[lang] || LOCALES.en;
  const html = renderToString(
    <LangProvider initialLang={lang}>
      <App />
    </LangProvider>
  );
  return {
    html,
    lang,
    dir: lang === 'ar' ? 'rtl' : 'ltr',
    path: localePath(lang),
    title: c.meta.title,
    description: c.meta.description,
  };
}

export const LANG_CODES = Object.keys(LOCALES);
