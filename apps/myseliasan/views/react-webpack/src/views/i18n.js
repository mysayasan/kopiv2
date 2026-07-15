// App translations, code-split by locale.
//
// English ships in the main bundle (it is the fallback for every missing key, so it must be
// present on first paint). ms/zh/ar are each their own chunk, fetched only when that language is
// actually selected — which took the bulk of the translation tables out of the initial
// download for the single-language user who never switches.
import en from './i18n/en';

// enBundle is the message bundle before any other locale has loaded: English only.
export const enBundle = { en };

const LOADERS = {
  ms: () => import(/* webpackChunkName: "i18n-ms" */ './i18n/ms'),
  zh: () => import(/* webpackChunkName: "i18n-zh" */ './i18n/zh'),
  ar: () => import(/* webpackChunkName: "i18n-ar" */ './i18n/ar'),
};

// loadLocaleDict fetches one locale's dictionary on demand. Returns null for English (already
// loaded) or an unknown code, so the caller keeps whatever it had.
export async function loadLocaleDict(lang) {
  const load = LOADERS[lang];
  if (!load) return null;
  const mod = await load();
  return mod.default;
}
