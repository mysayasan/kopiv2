import { LANGUAGES, useT } from './i18n';
import './styles/language.css';

// LanguageDropdown (kept name for stable imports) is now a plain inline one-line
// switcher: "English | Melayu | 中文 | தமிழ்" — each language a clickable option, the
// active one highlighted, no dropdown. It is fully SELF-CONTAINED: its own
// `shared-lang-*` classes + styles/language.css (tokenized with --ui-*), so it never
// depends on an app's theme/nav/topbar classes and a UI redesign can't break it. The
// caller owns the persisted `lang` value and the `onLang` setter.
export function LanguageDropdown({ lang, onLang }) {
  const t = useT();
  const active = LANGUAGES.find((l) => l.code === lang)?.code || LANGUAGES[0].code;
  return (
    <div className="shared-lang" role="group" aria-label={t('lang.select')}>
      {LANGUAGES.map((l) => (
        <button
          key={l.code}
          type="button"
          className={`shared-lang-opt${l.code === active ? ' active' : ''}`}
          aria-pressed={l.code === active}
          onClick={() => onLang(l.code)}
        >
          {l.label}
        </button>
      ))}
    </div>
  );
}
