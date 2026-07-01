import { LANGS, localePath, useLang } from '../i18n/index.jsx';

// Inline one-line language switcher — "English | Melayu | 中文 | العربية" — matching
// the myidsan/shared concept: each language a clickable option, active one highlighted
// in the accent color, separated by a pipe. Rendered as real <a href> links to the
// per-locale URLs (crawlable + work without JS) with a JS intercept for instant,
// no-reload switching. Picking Arabic flips the layout to RTL.
export default function LangDropdown() {
  const { lang, setLang } = useLang();
  return (
    <div className="lang" role="group" aria-label="Select language">
      {LANGS.map((l) => (
        <a
          key={l.code}
          href={localePath(l.code)}
          hrefLang={l.code}
          className={`lang__opt${l.code === lang ? ' active' : ''}`}
          aria-current={l.code === lang ? 'true' : undefined}
          onClick={(e) => {
            // let modified clicks (new tab, etc.) behave normally
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            setLang(l.code);
          }}
        >
          {l.label}
        </a>
      ))}
    </div>
  );
}
