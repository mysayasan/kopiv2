import { createContext, useContext, useEffect, useState } from 'react';
import en from '../content/en.js';
import ms from '../content/ms.js';
import zh from '../content/zh.js';
import ar from '../content/ar.js';

export const LOCALES = { en, ms, zh, ar };

// Supported languages, in switcher order. `rtl: true` flips the whole layout.
export const LANGS = [
  { code: 'en', label: 'English' },
  { code: 'ms', label: 'Melayu' },
  { code: 'zh', label: '中文' },
  { code: 'ar', label: 'العربية', rtl: true },
];

export const SITE_URL = 'https://r450k.com';

// URL path is the source of truth for language (one indexable URL per locale).
// en is the root; others live under /<code>/.
export const localePath = (code) => (code === 'en' ? '/' : `/${code}/`);

export function localeFromPath(pathname) {
  const seg = (pathname || '/').split('/').filter(Boolean)[0];
  return LOCALES[seg] ? seg : 'en';
}

const dirOf = (lang) => (lang === 'ar' ? 'rtl' : 'ltr');

// Keep the document head in sync with the active language (used on client-side
// switches; the prerendered HTML already carries the correct tags on first load).
function syncHead(lang) {
  const c = LOCALES[lang] || en;
  const url = SITE_URL + localePath(lang);
  const set = (sel, attr, val) => {
    const el = document.head.querySelector(sel);
    if (el) el.setAttribute(attr, val);
  };
  document.documentElement.lang = lang;
  document.documentElement.dir = dirOf(lang);
  if (c.meta) {
    document.title = c.meta.title;
    set('meta[name="description"]', 'content', c.meta.description);
    set('meta[property="og:title"]', 'content', c.meta.title);
    set('meta[name="twitter:title"]', 'content', c.meta.title);
    set('meta[property="og:description"]', 'content', c.meta.description);
    set('meta[name="twitter:description"]', 'content', c.meta.description);
  }
  set('link[rel="canonical"]', 'href', url);
  set('meta[property="og:url"]', 'content', url);
  set('meta[property="og:locale"]', 'content', lang);
}

const LangContext = createContext(null);

export function LangProvider({ children, initialLang }) {
  const [lang, setLangState] = useState(
    () => initialLang || (typeof window !== 'undefined' ? localeFromPath(window.location.pathname) : 'en')
  );

  // Client-side language switch: update the URL + head without a full reload.
  const setLang = (code) => {
    if (code === lang) return;
    window.history.pushState({}, '', localePath(code));
    setLangState(code);
  };

  useEffect(() => {
    syncHead(lang);
  }, [lang]);

  // Keep in sync with browser back/forward between locale URLs.
  useEffect(() => {
    const onPop = () => setLangState(localeFromPath(window.location.pathname));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const value = { lang, dir: dirOf(lang), setLang, content: LOCALES[lang] || en };
  return <LangContext.Provider value={value}>{children}</LangContext.Provider>;
}

export const useLang = () => useContext(LangContext);
export const useContent = () => useContext(LangContext).content;
