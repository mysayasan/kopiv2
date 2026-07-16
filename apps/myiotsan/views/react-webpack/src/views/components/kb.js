import { useCallback, useEffect, useMemo, useState } from 'react';
import { useT } from '@shared';
import { api, errorMessage } from '../lib/helpers';
import { EmptyState, Panel } from './ui';

// KbPage: the in-app knowledge base, the shipped setup guides for the solar / Modbus device
// profiles. Content is served by /api/kb (markdown compiled into the binary), so it works with no
// network access. This renderer is deliberately dependency-free: the markdown is trusted, shipped
// content, not user input, so a small subset renderer is enough and avoids pulling in a library.

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// inline handles the span-level markdown inside a line: code, bold, links. Order matters: code
// spans are protected first (with a sentinel that cannot occur in prose) so their contents are not
// re-parsed as bold/links, then restored last.
function inline(text) {
  const codes = [];
  let s = text.replace(/`([^`]+)`/g, (_, c) => {
    codes.push(c);
    return `@@CODE${codes.length - 1}@@`;
  });
  s = esc(s);
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, href) => {
    if (/^[a-z]+:\/\//i.test(href) || href.startsWith('mailto:')) {
      return `<a href="${esc(href)}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    }
    // An internal KB link: a bare slug, intercepted to switch article.
    return `<a href="#" data-kb="${esc(href)}">${label}</a>`;
  });
  s = s.replace(/@@CODE(\d+)@@/g, (_, i) => `<code>${esc(codes[Number(i)])}</code>`);
  return s;
}

// renderMarkdown converts the supported block-level subset to an HTML string: headings, hr,
// blockquotes, fenced code, ordered/unordered lists, GitHub tables, and paragraphs.
function renderMarkdown(md) {
  const lines = (md || '').replace(/\r\n/g, '\n').split('\n');
  let html = '';
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    // Fenced code block.
    if (/^```/.test(line)) {
      const buf = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++]);
      i++; // closing fence
      html += `<pre class="kb-code"><code>${esc(buf.join('\n'))}</code></pre>`;
      continue;
    }
    // Horizontal rule.
    if (/^---+\s*$/.test(line)) {
      html += '<hr />';
      i++;
      continue;
    }
    // Heading.
    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      const level = h[1].length;
      html += `<h${level}>${inline(h[2])}</h${level}>`;
      i++;
      continue;
    }
    // Blockquote (one or more consecutive > lines).
    if (/^>\s?/.test(line)) {
      const buf = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) buf.push(lines[i++].replace(/^>\s?/, ''));
      html += `<blockquote>${inline(buf.join(' '))}</blockquote>`;
      continue;
    }
    // Table (a header row followed by a |---| separator).
    if (/^\|.*\|\s*$/.test(line) && i + 1 < lines.length && /^\|[\s:|-]+\|\s*$/.test(lines[i + 1])) {
      const cells = (row) => row.replace(/^\||\|\s*$/g, '').split('|').map((c) => c.trim());
      const head = cells(line);
      i += 2;
      let body = '';
      while (i < lines.length && /^\|.*\|\s*$/.test(lines[i])) {
        const row = cells(lines[i++]);
        body += `<tr>${row.map((c) => `<td>${inline(c)}</td>`).join('')}</tr>`;
      }
      html += `<div class="kb-tablewrap"><table class="kb-table"><thead><tr>${head.map((c) => `<th>${inline(c)}</th>`).join('')}</tr></thead><tbody>${body}</tbody></table></div>`;
      continue;
    }
    // Unordered list.
    if (/^\s*[-*]\s+/.test(line)) {
      let items = '';
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items += `<li>${inline(lines[i++].replace(/^\s*[-*]\s+/, ''))}</li>`;
      }
      html += `<ul>${items}</ul>`;
      continue;
    }
    // Ordered list.
    if (/^\s*\d+\.\s+/.test(line)) {
      let items = '';
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items += `<li>${inline(lines[i++].replace(/^\s*\d+\.\s+/, ''))}</li>`;
      }
      html += `<ol>${items}</ol>`;
      continue;
    }
    // Blank line.
    if (/^\s*$/.test(line)) {
      i++;
      continue;
    }
    // Paragraph (gather until a blank line or a block starter).
    const buf = [];
    while (
      i < lines.length &&
      !/^\s*$/.test(lines[i]) &&
      !/^(#{1,6}\s|>\s?|```|---+\s*$|\s*[-*]\s+|\s*\d+\.\s+|\|)/.test(lines[i])
    ) {
      buf.push(lines[i++]);
    }
    html += `<p>${inline(buf.join(' '))}</p>`;
  }
  return html;
}

export function KbPage({ onToast }) {
  const t = useT();
  const [items, setItems] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [slug, setSlug] = useState('');
  const [article, setArticle] = useState(null);
  const [loadingArticle, setLoadingArticle] = useState(false);

  useEffect(() => {
    let live = true;
    (async () => {
      setBusy(true);
      try {
        const r = await api('/api/kb');
        if (!live) return;
        const list = r.body?.items || [];
        setItems(list);
        setError('');
        if (list.length) setSlug((cur) => cur || list[0].slug);
      } catch (e) {
        if (live) setError(errorMessage(e));
      } finally {
        if (live) setBusy(false);
      }
    })();
    return () => { live = false; };
  }, []);

  useEffect(() => {
    if (!slug) return;
    let live = true;
    setLoadingArticle(true);
    (async () => {
      try {
        const r = await api(`/api/kb/${slug}`);
        if (live) setArticle(r.body?.article || null);
      } catch (e) {
        if (live && onToast) onToast(errorMessage(e), 'error');
      } finally {
        if (live) setLoadingArticle(false);
      }
    })();
    return () => { live = false; };
  }, [slug, onToast]);

  const groups = useMemo(() => {
    const by = new Map();
    for (const a of items) {
      const cat = a.category || 'General';
      if (!by.has(cat)) by.set(cat, []);
      by.get(cat).push(a);
    }
    return Array.from(by.entries());
  }, [items]);

  const onBodyClick = useCallback((e) => {
    const link = e.target.closest('[data-kb]');
    if (link) {
      e.preventDefault();
      const target = link.getAttribute('data-kb');
      if (items.some((a) => a.slug === target)) setSlug(target);
    }
  }, [items]);

  const bodyHtml = useMemo(() => (article ? renderMarkdown(article.body) : ''), [article]);

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>{t('nav.help')}</h1>
          <p className="page-sub">{t('kb.subtitle')}</p>
        </div>
      </div>
      {busy ? (
        <Panel><EmptyState icon="info" title={t('ui.loading')} /></Panel>
      ) : error ? (
        <Panel><EmptyState icon="info" title={error} /></Panel>
      ) : !items.length ? (
        <Panel><EmptyState icon="info" title={t('kb.empty')} /></Panel>
      ) : (
        <div className="kb-layout">
          <nav className="kb-index">
            {groups.map(([cat, arts]) => (
              <div className="kb-index-group" key={cat}>
                <div className="kb-index-cat">{cat}</div>
                {arts.map((a) => (
                  <button
                    key={a.slug}
                    className={`kb-index-item${a.slug === slug ? ' active' : ''}`}
                    onClick={() => setSlug(a.slug)}
                  >
                    {a.title || a.slug}
                  </button>
                ))}
              </div>
            ))}
          </nav>
          <article className="kb-article">
            {loadingArticle && !article ? (
              <EmptyState icon="info" title={t('ui.loading')} />
            ) : (
              <div className="kb-body" onClick={onBodyClick} dangerouslySetInnerHTML={{ __html: bodyHtml }} />
            )}
          </article>
        </div>
      )}
    </div>
  );
}
