import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/manual.css';

// The built-in user manual, shared by every app in the suite.
//
// An app supplies CONTENT (markdown compiled into its binary, served by /api/manual) and mounts
// <ManualProvider> once; everything else — the reader, the search, the contextual drawer, the
// printed book — lives here so a second app gets the whole surface for the price of its articles.
//
// The markdown renderer is deliberately dependency-free. The content is shipped, reviewed prose,
// not user input, so a small well-understood subset beats pulling a parser into every app's
// bundle — and an air-gapped appliance cannot fetch one anyway. It is the renderer myiotsan's
// knowledge base proved, extended with the things a MANUAL needs that a KB did not: stable
// heading anchors for deep links, figures, callouts, and print-safe code blocks.

// ---------------------------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------------------------

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// Attribute-escaping is stricter than text-escaping: quotes must go too, or a crafted alt text
// or href could break out of the attribute it sits in.
const escAttr = (s) => esc(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');

const isExternalHref = (href) => /^[a-z][a-z0-9+.-]*:\/\//i.test(href) || href.startsWith('mailto:');

// explicitAnchor pulls the `{#id}` suffix off a heading. Anchors are written by hand and kept
// identical across translations precisely so a contextual "?" button lands in the right place in
// every language — a slug derived from the heading TEXT could not do that.
const ANCHOR_RE = /\s*\{#([A-Za-z0-9._-]+)\}\s*$/;

function splitAnchor(text) {
  const m = ANCHOR_RE.exec(text);
  if (!m) return { text: text.trim(), id: '' };
  return { text: text.slice(0, m.index).trim(), id: m[1] };
}

// CALLOUTS maps the GitHub-style blockquote marker to the tone the stylesheet knows.
const CALLOUTS = { NOTE: 'note', TIP: 'tip', IMPORTANT: 'note', WARNING: 'warn', CAUTION: 'warn' };

// inline renders the span-level markdown inside one line: code, images, links, bold, italic.
// Order matters. Code spans are protected first behind a sentinel that cannot occur in prose, so
// their contents are never re-parsed as emphasis or a link, and are restored last.
function inline(text, opts) {
  const codes = [];
  let s = String(text).replace(/`([^`]+)`/g, (_, c) => {
    codes.push(c);
    return `@@CODE${codes.length - 1}@@`;
  });
  s = esc(s);

  // Images before links — `![alt](src)` contains a `[...](...)` a link pattern would claim.
  s = s.replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (_, alt, src) => {
    const url = isExternalHref(src) ? '' : `${opts.assetBase}/${src.replace(/^assets\//, '')}`;
    // An external image would break the air-gap posture the whole manual exists to keep, so it
    // is dropped rather than requested.
    if (!url) return `<span class="manual-figure-missing">${escAttr(alt)}</span>`;
    return `<img class="manual-figure" src="${escAttr(url)}" alt="${escAttr(alt)}" loading="lazy" />`;
  });

  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');

  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, label, href) => {
    if (isExternalHref(href)) {
      return `<a href="${escAttr(href)}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    }
    // A bare `#anchor` stays inside the current article; `slug` or `slug#anchor` switches to
    // another one. Both are intercepted by the reader rather than followed by the browser.
    if (href.startsWith('#')) {
      return `<a href="#" class="manual-link" data-manual-anchor="${escAttr(href.slice(1))}">${label}</a>`;
    }
    const [slug, anchor] = href.split('#');
    const anchorAttr = anchor ? ` data-manual-anchor="${escAttr(anchor)}"` : '';
    return `<a href="#" class="manual-link" data-manual="${escAttr(slug)}"${anchorAttr}>${label}</a>`;
  });

  return s.replace(/@@CODE(\d+)@@/g, (_, i) => `<code>${esc(codes[Number(i)])}</code>`);
}

const BULLET_RE = /^\s*[-*]\s+/;
const ORDERED_RE = /^\s*\d+\.\s+/;
const BLOCK_START_RE = /^\s*(#{1,6}\s|>|```|\||---+\s*$)/;

// collectList gathers consecutive list items, folding each item's WRAPPED continuation lines back
// into it. Manual prose is hard-wrapped at ~100 columns, so a list item longer than one line is
// the common case, not the exception — without this the tail of every such item broke out of the
// list and rendered as a stray paragraph underneath it.
function collectList(lines, start, itemRe) {
  const items = [];
  let i = start;
  while (i < lines.length && itemRe.test(lines[i])) {
    let text = lines[i++].replace(itemRe, '');
    while (
      i < lines.length
      && /^\s+\S/.test(lines[i])           // indented under the item…
      && !BULLET_RE.test(lines[i])         // …and not the next item…
      && !ORDERED_RE.test(lines[i])
      && !BLOCK_START_RE.test(lines[i])    // …and not a new block.
    ) {
      text += ` ${lines[i++].trim()}`;
    }
    items.push(text);
  }
  return { items, next: i };
}

// renderManualMarkdown converts the supported block-level subset to an HTML string and returns it
// alongside the heading outline, which the reader uses for in-page navigation and the printed
// book uses for its table of contents.
//
// opts.idPrefix namespaces heading ids. It is empty in the reader (so `#credentials` deep links
// work verbatim) and set per article when the whole manual is printed into one document, where
// the same anchor legitimately appears in several articles.
export function renderManualMarkdown(md, opts = {}) {
  const o = { assetBase: '/api/manual/assets', idPrefix: '', ...opts };
  const lines = String(md || '').replace(/\r\n/g, '\n').split('\n');
  const outline = [];
  let html = '';
  let i = 0;
  let autoId = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code. Locked to LTR: an Arabic page must not mirror a command or a file path.
    if (/^```/.test(line)) {
      const buf = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++]);
      i++;
      html += `<pre class="manual-code" dir="ltr"><code>${esc(buf.join('\n'))}</code></pre>`;
      continue;
    }

    if (/^---+\s*$/.test(line)) {
      html += '<hr />';
      i++;
      continue;
    }

    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      const level = h[1].length;
      const { text, id } = splitAnchor(h[2]);
      // A heading with no explicit anchor still needs SOME id so the outline can scroll to it,
      // but it is positional and therefore not safe to deep-link from the UI. That is the whole
      // reason contextual help targets only hand-written `{#id}` anchors.
      const anchorId = `${o.idPrefix}${id || `s${++autoId}`}`;
      if (level >= 2) outline.push({ id: anchorId, level, text });
      html += `<h${level} id="${escAttr(anchorId)}" class="manual-h">${inline(text, o)}</h${level}>`;
      i++;
      continue;
    }

    // Blockquote, which doubles as the callout syntax when it opens with `> [!NOTE]`.
    if (/^>\s?/.test(line)) {
      const buf = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) buf.push(lines[i++].replace(/^>\s?/, ''));
      const marker = /^\[!([A-Z]+)\]\s*$/.exec(buf[0] || '');
      const tone = marker ? CALLOUTS[marker[1]] : null;
      if (tone) {
        const body = buf.slice(1).filter((l) => l.trim() !== '').join(' ');
        html += `<div class="manual-callout manual-callout-${tone}"><p>${inline(body, o)}</p></div>`;
      } else {
        html += `<blockquote>${inline(buf.join(' '), o)}</blockquote>`;
      }
      continue;
    }

    // Table: a header row followed by a |---| separator.
    if (/^\|.*\|\s*$/.test(line) && i + 1 < lines.length && /^\|[\s:|-]+\|\s*$/.test(lines[i + 1])) {
      const cells = (row) => row.replace(/^\||\|\s*$/g, '').split('|').map((c) => c.trim());
      const head = cells(line);
      i += 2;
      let body = '';
      while (i < lines.length && /^\|.*\|\s*$/.test(lines[i])) {
        const row = cells(lines[i++]);
        body += `<tr>${row.map((c) => `<td>${inline(c, o)}</td>`).join('')}</tr>`;
      }
      html += `<div class="manual-tablewrap"><table class="manual-table"><thead><tr>${
        head.map((c) => `<th>${inline(c, o)}</th>`).join('')
      }</tr></thead><tbody>${body}</tbody></table></div>`;
      continue;
    }

    if (BULLET_RE.test(line)) {
      const list = collectList(lines, i, BULLET_RE);
      i = list.next;
      html += `<ul>${list.items.map((it) => `<li>${inline(it, o)}</li>`).join('')}</ul>`;
      continue;
    }

    if (ORDERED_RE.test(line)) {
      const list = collectList(lines, i, ORDERED_RE);
      i = list.next;
      html += `<ol>${list.items.map((it) => `<li>${inline(it, o)}</li>`).join('')}</ol>`;
      continue;
    }

    if (/^\s*$/.test(line)) {
      i++;
      continue;
    }

    const buf = [];
    while (
      i < lines.length
      && !/^\s*$/.test(lines[i])
      && !/^(#{1,6}\s|>\s?|```|---+\s*$|\s*[-*]\s+|\s*\d+\.\s+|\|)/.test(lines[i])
    ) {
      buf.push(lines[i++]);
    }
    html += `<p>${inline(buf.join(' '), o)}</p>`;
  }

  return { html, outline };
}

// stripMarkdown reduces a body to plain text for searching and for the search-result snippet.
function stripMarkdown(md) {
  return String(md || '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(ANCHOR_RE, '')
    .replace(/\{#[A-Za-z0-9._-]+\}/g, '')
    .replace(/^[>#\s|*-]+/gm, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

// ---------------------------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------------------------

const ManualContext = createContext(null);

// useManual gives any screen access to the manual. openHelp(slug, anchor) is the one an app calls
// from a "?" button; the rest is what the reader itself needs.
export function useManual() {
  return useContext(ManualContext) || EMPTY_MANUAL;
}

// Without a provider the hooks still resolve, so a "?" button in a not-yet-wired app renders and
// does nothing instead of crashing the page it sits on.
const EMPTY_MANUAL = {
  ready: false, error: '', articles: [], languages: [], language: 'en',
  openHelp: () => {}, close: () => {}, print: () => {}, get: () => null,
};

// ManualProvider fetches the whole manual once per language and owns the two surfaces that must
// escape the page they were opened from: the contextual drawer and the print document.
//
// The whole book is fetched rather than one article at a time because full-text search and
// whole-manual printing both need every body, and the content is small, embedded and unchanging.
// One request buys both, and both then work with no further round trips.
export function ManualProvider({ apiBase = '', lang = 'en', appName = '', children }) {
  const [state, setState] = useState({ ready: false, error: '', articles: [], languages: [], language: lang });
  const [version, setVersion] = useState('');
  const [drawer, setDrawer] = useState(null);   // { slug, anchor }
  const [printJob, setPrintJob] = useState(null); // { scope: 'article' | 'all', slug }

  // The running version is stamped into the printed header. A printed manual outlives the
  // software it describes, so the paper has to say which version it described — the alternative
  // is a reader trusting a page that stopped being true two upgrades ago.
  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const res = await fetch(`${apiBase}/api/version`, { credentials: 'same-origin' });
        if (!res.ok) return;
        const json = await res.json();
        const body = json?.data?.result ?? json?.result ?? json;
        if (live && body?.appVersion) setVersion(String(body.appVersion));
      } catch (_) { /* the header simply omits it */ }
    })();
    return () => { live = false; };
  }, [apiBase]);

  useEffect(() => {
    let live = true;
    setState((s) => ({ ...s, ready: false, error: '' }));
    (async () => {
      try {
        const res = await fetch(`${apiBase}/api/manual/bundle?lang=${encodeURIComponent(lang)}`, {
          headers: { Accept: 'application/json' },
          credentials: 'same-origin',
        });
        if (!res.ok) throw new Error(String(res.status));
        const json = await res.json();
        const payload = json?.result || json || {};
        if (!live) return;
        setState({
          ready: true,
          error: '',
          articles: Array.isArray(payload.items) ? payload.items : [],
          languages: Array.isArray(payload.languages) ? payload.languages : [],
          language: payload.language || lang,
        });
      } catch (_) {
        if (live) setState({ ready: true, error: 'load', articles: [], languages: [], language: lang });
      }
    })();
    return () => { live = false; };
  }, [apiBase, lang]);

  // Printing renders into a portal on <body> and marks the document, because the print
  // stylesheet hides the application chrome — a print container nested INSIDE that chrome would
  // be hidden along with it.
  useEffect(() => {
    if (!printJob || typeof window === 'undefined') return undefined;
    document.body.classList.add('manual-printing');
    const done = () => setPrintJob(null);
    window.addEventListener('afterprint', done);
    // One frame for the portal to lay out before the dialog freezes the page.
    const open = setTimeout(() => { try { window.print(); } catch (_) { done(); } }, 80);
    // Not every browser fires afterprint. The fallback only clears state; the container is
    // display:none on screen either way, so a missed event is invisible rather than broken.
    const bail = setTimeout(done, 20000);
    return () => {
      clearTimeout(open);
      clearTimeout(bail);
      window.removeEventListener('afterprint', done);
      document.body.classList.remove('manual-printing');
    };
  }, [printJob]);

  const get = useCallback((slug) => state.articles.find((a) => a.slug === slug) || null, [state.articles]);

  const value = useMemo(() => ({
    ...state,
    appName,
    version,
    assetBase: `${apiBase}/api/manual/assets`,
    get,
    openHelp: (slug, anchor) => setDrawer({ slug, anchor: anchor || '' }),
    close: () => setDrawer(null),
    print: (scope, slug) => setPrintJob({ scope: scope || 'all', slug: slug || '' }),
  }), [state, appName, version, apiBase, get]);

  const body = typeof document !== 'undefined' ? document.body : null;

  return (
    <ManualContext.Provider value={value}>
      {children}
      {body && drawer ? createPortal(<ManualDrawer slug={drawer.slug} anchor={drawer.anchor} />, body) : null}
      {body && printJob ? createPortal(<ManualPrintable job={printJob} />, body) : null}
    </ManualContext.Provider>
  );
}

// ---------------------------------------------------------------------------------------------
// Article rendering + navigation
// ---------------------------------------------------------------------------------------------

// useArticleNavigation wires the click interception every rendered article body needs: an
// internal link switches article or scrolls to an anchor instead of navigating the browser away
// from the single-page app.
function useArticleNavigation(onSelect, containerRef) {
  return useCallback((event) => {
    const link = event.target.closest('[data-manual], [data-manual-anchor]');
    if (!link) return;
    event.preventDefault();
    const slug = link.getAttribute('data-manual');
    const anchor = link.getAttribute('data-manual-anchor') || '';
    if (slug) {
      onSelect(slug, anchor);
      return;
    }
    const target = containerRef.current?.querySelector(`#${CSS.escape(anchor)}`);
    if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [onSelect, containerRef]);
}

// ArticleBody renders one article and scrolls to `anchor` whenever it changes.
function ArticleBody({ article, anchor, onSelect, assetBase, scrollRoot }) {
  const ref = useRef(null);
  const onClick = useArticleNavigation(onSelect, ref);
  const rendered = useMemo(
    () => renderManualMarkdown(article?.body, { assetBase }),
    [article?.body, assetBase],
  );

  useEffect(() => {
    if (!article) return;
    const root = scrollRoot?.current || ref.current;
    if (!anchor) {
      if (root) root.scrollTop = 0;
      return;
    }
    const target = ref.current?.querySelector(`#${CSS.escape(anchor)}`);
    if (target) target.scrollIntoView({ block: 'start' });
    else if (root) root.scrollTop = 0;
  }, [article, anchor, scrollRoot]);

  if (!article) return null;

  return (
    <div
      ref={ref}
      className="manual-body"
      onClick={onClick}
      dangerouslySetInnerHTML={{ __html: rendered.html }}
    />
  );
}

// UntranslatedNotice tells the reader when they are looking at English because their language has
// no translation of this page yet. Saying so is the honest alternative to silently serving a
// different language than the one they picked.
function UntranslatedNotice({ article, language }) {
  const t = useT();
  if (!article || !language || article.language === language) return null;
  return (
    <div className="manual-untranslated" role="status">
      <Ico n="info" sz={14} />
      <span>{t('manual.untranslated')}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------
// The full-page library
// ---------------------------------------------------------------------------------------------

// groupByCategory groups articles on the STABLE category id while displaying the translated
// label, which is what keeps the section list identical across languages.
function groupByCategory(articles) {
  const groups = [];
  const index = new Map();
  for (const a of articles) {
    const key = a.category || 'general';
    if (!index.has(key)) {
      const g = { key, label: a.categoryLabel || a.category || '', items: [] };
      index.set(key, g);
      groups.push(g);
    }
    index.get(key).items.push(a);
  }
  return groups;
}

// matchScore ranks one article against the query, preferring a title hit over a summary hit over
// a body hit, and returns the snippet to show. `plain` is the pre-stripped body: stripping the
// markdown of every article on every keystroke is the difference between search that feels
// instant and search that stutters.
function matchScore(article, plain, needle) {
  const title = (article.title || '').toLowerCase();
  const summary = (article.summary || '').toLowerCase();
  const body = plain;
  if (title.includes(needle)) return { rank: 0, where: article.summary || '' };
  if (summary.includes(needle)) return { rank: 1, where: article.summary || '' };
  const at = body.indexOf(needle);
  if (at < 0) return null;
  const from = Math.max(0, at - 60);
  return { rank: 2, where: `${from > 0 ? '…' : ''}${body.slice(from, at + needle.length + 80).trim()}…` };
}

// ManualLibrary is the full Help page: sections and search on the left, the article on the right.
export function ManualLibrary({ initialSlug = '' }) {
  const t = useT();
  const manual = useManual();
  const [slug, setSlug] = useState(initialSlug);
  const [anchor, setAnchor] = useState('');
  const [query, setQuery] = useState('');
  const scrollRoot = useRef(null);

  // Settle on the first article as soon as the manual arrives, so the page is never an empty
  // pane waiting for a click.
  useEffect(() => {
    if (!slug && manual.articles.length) setSlug(manual.articles[0].slug);
  }, [manual.articles, slug]);

  const select = useCallback((nextSlug, nextAnchor) => {
    setSlug(nextSlug);
    setAnchor(nextAnchor || '');
  }, []);

  // Stripped once per manual load, not once per keystroke.
  const plainBodies = useMemo(
    () => manual.articles.map((a) => stripMarkdown(a.body).toLowerCase()),
    [manual.articles],
  );

  const needle = query.trim().toLowerCase();
  const results = useMemo(() => {
    if (needle.length < 2) return null;
    return manual.articles
      .map((a, n) => {
        const m = matchScore(a, plainBodies[n], needle);
        return m ? { article: a, ...m } : null;
      })
      .filter(Boolean)
      .sort((a, b) => a.rank - b.rank);
  }, [manual.articles, plainBodies, needle]);

  const groups = useMemo(() => groupByCategory(manual.articles), [manual.articles]);
  const article = manual.get(slug);
  const position = manual.articles.findIndex((a) => a.slug === slug);
  const previous = position > 0 ? manual.articles[position - 1] : null;
  const next = position >= 0 && position < manual.articles.length - 1 ? manual.articles[position + 1] : null;

  if (!manual.ready) return <div className="manual-page manual-state">{t('common.loading')}</div>;
  if (manual.error) return <div className="manual-page manual-state">{t('manual.loadFailed')}</div>;
  if (!manual.articles.length) return <div className="manual-page manual-state">{t('manual.empty')}</div>;

  return (
    <div className="manual-page">
      <nav className="manual-index" aria-label={t('manual.contents')}>
        <div className="manual-search">
          <Ico n="search" sz={14} />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('manual.searchPlaceholder')}
            aria-label={t('manual.searchAria')}
          />
        </div>

        {results ? (
          <div className="manual-results">
            <div className="manual-index-cat">{t('manual.results', { n: results.length })}</div>
            {results.map((r) => (
              <button
                key={r.article.slug}
                type="button"
                className={`manual-index-item${r.article.slug === slug ? ' active' : ''}`}
                onClick={() => { select(r.article.slug, ''); }}
              >
                <span className="manual-index-title">{r.article.title}</span>
                <span className="manual-index-snippet">{r.where}</span>
              </button>
            ))}
            {results.length === 0 ? <div className="manual-index-empty">{t('manual.noResults')}</div> : null}
          </div>
        ) : (
          groups.map((g) => (
            <div className="manual-index-group" key={g.key}>
              <div className="manual-index-cat">{g.label}</div>
              {g.items.map((a) => (
                <button
                  key={a.slug}
                  type="button"
                  className={`manual-index-item${a.slug === slug ? ' active' : ''}`}
                  onClick={() => select(a.slug, '')}
                >
                  <span className="manual-index-title">{a.title}</span>
                </button>
              ))}
            </div>
          ))
        )}
      </nav>

      <article className="manual-article" ref={scrollRoot}>
        <div className="manual-toolbar">
          <button type="button" className="manual-btn" onClick={() => manual.print('article', slug)}>
            <Ico n="printer" sz={14} /> {t('manual.printPage')}
          </button>
          <button type="button" className="manual-btn" onClick={() => manual.print('all')}>
            <Ico n="book" sz={14} /> {t('manual.printAll')}
          </button>
        </div>
        <UntranslatedNotice article={article} language={manual.language} />
        <ArticleBody
          article={article}
          anchor={anchor}
          onSelect={select}
          assetBase={manual.assetBase}
          scrollRoot={scrollRoot}
        />
        <div className="manual-pager">
          {previous ? (
            <button type="button" className="manual-btn" onClick={() => select(previous.slug, '')}>
              <Ico n="arr-left" sz={14} /> {previous.title}
            </button>
          ) : <span />}
          {next ? (
            <button type="button" className="manual-btn" onClick={() => select(next.slug, '')}>
              {next.title} <Ico n="arr-right" sz={14} />
            </button>
          ) : <span />}
        </div>
      </article>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------
// The contextual drawer
// ---------------------------------------------------------------------------------------------

// ManualDrawer shows one article beside whatever the reader was doing. It is mounted by the
// provider, not by pages — a page that wants help calls openHelp() and keeps its own state.
export function ManualDrawer({ slug, anchor }) {
  const t = useT();
  const manual = useManual();
  const [current, setCurrent] = useState({ slug, anchor: anchor || '' });
  const scrollRoot = useRef(null);

  useEffect(() => { setCurrent({ slug, anchor: anchor || '' }); }, [slug, anchor]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') manual.close(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [manual]);

  const article = manual.get(current.slug);
  const select = useCallback((nextSlug, nextAnchor) => setCurrent({ slug: nextSlug, anchor: nextAnchor || '' }), []);

  return (
    <div className="manual-drawer-scrim" role="presentation" onClick={manual.close}>
      <aside
        className="manual-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={t('manual.title')}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="manual-drawer-head">
          <Ico n="book" sz={16} />
          <h2>{article ? article.title : t('manual.title')}</h2>
          <button type="button" className="manual-icon-btn" onClick={() => manual.print('article', current.slug)} title={t('manual.printPage')} aria-label={t('manual.printPage')}>
            <Ico n="printer" sz={16} />
          </button>
          <button type="button" className="manual-icon-btn" onClick={manual.close} title={t('common.close')} aria-label={t('common.close')}>
            <Ico n="x" sz={16} />
          </button>
        </header>
        <div className="manual-drawer-body" ref={scrollRoot}>
          {!manual.ready ? (
            <p className="manual-state">{t('common.loading')}</p>
          ) : !article ? (
            <p className="manual-state">{t('manual.missing')}</p>
          ) : (
            <>
              <UntranslatedNotice article={article} language={manual.language} />
              <ArticleBody
                article={article}
                anchor={current.anchor}
                onSelect={select}
                assetBase={manual.assetBase}
                scrollRoot={scrollRoot}
              />
            </>
          )}
        </div>
      </aside>
    </div>
  );
}

// HelpButton is the small "?" an app puts next to a section heading. `anchor` must be a
// hand-written `{#id}` from the article, never a generated one — those are positional and drift.
export function HelpButton({ slug, anchor, label, size = 15, className = '' }) {
  const t = useT();
  const manual = useManual();
  const title = label || t('manual.helpFor');
  return (
    <button
      type="button"
      className={`manual-help-btn${className ? ` ${className}` : ''}`}
      onClick={(e) => { e.stopPropagation(); manual.openHelp(slug, anchor); }}
      title={title}
      aria-label={title}
    >
      <Ico n="help" sz={size} />
    </button>
  );
}

// ---------------------------------------------------------------------------------------------
// The printed book
// ---------------------------------------------------------------------------------------------

// ManualPrintable is the document that actually goes to the printer. It is a separate rendering
// rather than a restyling of the reader because the two want different things: the reader shows
// one article and hides the rest, while the book wants all of them, numbered, behind a table of
// contents, with every heading id namespaced so the same anchor can appear in several chapters.
function ManualPrintable({ job }) {
  const t = useT();
  const manual = useManual();
  const printed = job.scope === 'article'
    ? manual.articles.filter((a) => a.slug === job.slug)
    : manual.articles;

  const chapters = printed.map((a) => ({
    article: a,
    html: renderManualMarkdown(a.body, { assetBase: manual.assetBase, idPrefix: `${a.slug}--` }).html,
  }));

  const stamp = new Date().toLocaleDateString();

  return (
    <div className="manual-print" lang={manual.language}>
      <header className="manual-print-head">
        <strong>{manual.appName || t('manual.title')}</strong>
        <span>{t('manual.title')}</span>
        {manual.version ? <span>v{manual.version}</span> : null}
        <span>{stamp}</span>
      </header>

      {chapters.length > 1 ? (
        <section className="manual-print-toc">
          <h1>{t('manual.contents')}</h1>
          <ol>
            {chapters.map((c) => (
              <li key={c.article.slug}>
                <span className="manual-print-toc-title">{c.article.title}</span>
                {c.article.summary ? <span className="manual-print-toc-sub">{c.article.summary}</span> : null}
              </li>
            ))}
          </ol>
        </section>
      ) : null}

      {chapters.map((c) => (
        <section className="manual-print-chapter" key={c.article.slug}>
          <div className="manual-body" dangerouslySetInnerHTML={{ __html: c.html }} />
        </section>
      ))}
    </div>
  );
}
