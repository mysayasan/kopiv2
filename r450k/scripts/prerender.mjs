// Post-build prerender: renders the app once per locale to static HTML at distinct,
// indexable URLs (/, /ms/, /zh/, /ar/), each with translated <title>/meta, the
// correct <html lang>/dir, hreflang alternates, and canonical/og:url. Run after the
// client build + the SSR build (see package.json "build").
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, LANG_CODES } from '../dist-ssr/entry-server.js';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const SITE = 'https://r450k.com';
const pathFor = (l) => (l === 'en' ? '/' : `/${l}/`);

const template = readFileSync(resolve(root, 'dist/index.html'), 'utf8');

const esc = (s) =>
  String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const setMeta = (html, attr, name, value) =>
  html.replace(new RegExp(`(<meta ${attr}="${name}" content=")[^"]*(")`), `$1${esc(value)}$2`);

const alternates =
  [...LANG_CODES.map((l) => `<link rel="alternate" hreflang="${l}" href="${SITE}${pathFor(l)}" />`),
    `<link rel="alternate" hreflang="x-default" href="${SITE}/" />`].join('\n    ');

for (const lang of LANG_CODES) {
  const { html, dir, title, description } = render(lang);
  const url = SITE + pathFor(lang);
  let out = template;

  out = out.replace(/<html lang="[^"]*"[^>]*>/, `<html lang="${lang}" dir="${dir}">`);
  out = out.replace(/<title>[\s\S]*?<\/title>/, `<title>${esc(title)}</title>`);
  out = setMeta(out, 'name', 'description', description);
  out = setMeta(out, 'property', 'og:title', title);
  out = setMeta(out, 'property', 'og:description', description);
  out = setMeta(out, 'name', 'twitter:title', title);
  out = setMeta(out, 'name', 'twitter:description', description);
  out = setMeta(out, 'property', 'og:locale', lang);
  out = out.replace(/(<link rel="canonical" href=")[^"]*(")/, `$1${url}$2`);
  out = out.replace(/(<meta property="og:url" content=")[^"]*(")/, `$1${url}$2`);
  out = out.replace('</head>', `    ${alternates}\n  </head>`);
  out = out.replace('<div id="root"></div>', `<div id="root">${html}</div>`);

  const dest = lang === 'en' ? resolve(root, 'dist/index.html') : resolve(root, `dist/${lang}/index.html`);
  mkdirSync(dirname(dest), { recursive: true });
  writeFileSync(dest, out);
  console.log(`prerendered ${url} -> ${dest.slice(root.length + 1)}`);
}
