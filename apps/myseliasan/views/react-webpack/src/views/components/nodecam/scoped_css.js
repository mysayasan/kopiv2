import { mymatasanCss } from './mymatasan_css';

// The embedded node pages are styled by mymatasan's REAL stylesheet, but scoped so it only
// applies under `.nodecam-embed` — never leaking into myseliasan's own shell. We inject the
// stylesheet once, then walk the parsed CSSOM and prefix every rule's selector with the
// scope. This is a Shadow-DOM alternative that keeps standard React (events/inputs just
// work) and, because it pulls from mymatasan's source, auto-follows mymatasan design changes.
export const NODECAM_SCOPE_CLASS = 'nodecam-embed';
const SCOPE = `.${NODECAM_SCOPE_CLASS}`;
let injected = false;

export function ensureScopedMymatasanCss() {
  if (injected || typeof document === 'undefined') return;
  injected = true;
  const style = document.createElement('style');
  style.setAttribute('data-nodecam-scope', '');
  style.textContent = mymatasanCss;
  document.head.appendChild(style);
  try {
    if (style.sheet) scopeRules(style.sheet.cssRules);
  } catch (_) {
    // If the CSSOM rewrite is blocked, the styles are still present (unscoped) — but that
    // could bleed into the shell, so drop them rather than risk clobbering myseliasan.
    style.remove();
    injected = false;
  }
}

// scopeSelector prefixes a single comma-separated selector list with the scope. Root/theme
// selectors collapse onto the scope element itself (so mymatasan's :root design tokens and
// theme variants land on the wrapper); everything else becomes a descendant of the scope.
function scopeSelector(selectorText) {
  return selectorText.split(',').map((raw) => {
    const s = raw.trim();
    if (!s) return s;
    if (s === ':root' || s === 'html' || s === 'body') return SCOPE;
    if (s.startsWith(':root')) return SCOPE + s.slice(5);
    if (s.startsWith('html')) return SCOPE + s.slice(4);
    if (s.startsWith('body')) return SCOPE + s.slice(4);
    // Theme variants (.theme-light/.theme-dark/…) apply to the wrapper element itself.
    if (s.startsWith('.theme-')) return SCOPE + s;
    return `${SCOPE} ${s}`;
  }).join(', ');
}

function scopeRules(rules) {
  for (let i = 0; i < rules.length; i += 1) {
    const rule = rules[i];
    if (rule.type === CSSRule.STYLE_RULE) {
      try { rule.selectorText = scopeSelector(rule.selectorText); } catch (_) { /* skip unrewritable rule */ }
    } else if (rule.type === CSSRule.MEDIA_RULE || rule.type === CSSRule.SUPPORTS_RULE) {
      scopeRules(rule.cssRules);
    }
    // @keyframes / @font-face / @import: leave untouched.
  }
}
