import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { useManual } from '@shared/Manual';
import { StatCard, ChartCard, TimeSeriesChart } from '@shared/charts';
import { api, apiBase, csrfToken, formatTimestamp, sessionCanPost } from '../lib/helpers';
import '../styles/insight.css';

// AIInsightPage is the fleet AI agent's home: the latest digest (structured
// findings the server computed deterministically, localized HERE through the
// i18n dictionary — the backend never bakes prose into findings), the activity
// chart with its expected-band overlay, digest history, and — when a language
// model is enabled and ready — the "Ask the fleet" chat.
//
// The chat streams over a POST fetch + ReadableStream reader (EventSource can't
// POST): CPU models emit ~10-25 tokens/s, so rendering tokens as they arrive is
// the difference between "thinking…" and a dead page.

const HISTORY_PAGE = 10;

// findingText renders one structured finding through i18n. Params are stringified
// and timestamps pre-formatted so dictionary entries stay plain `{param}` slots.
function useFindingText() {
  const t = useT();
  return useCallback((f) => {
    const params = {};
    Object.entries(f.params || {}).forEach(([k, v]) => {
      if (k === 'bucketStart' || k === 'lastSeenAt') params[k] = v ? formatTimestamp(v) : '—';
      else if (typeof v === 'boolean') params[k] = v ? t('common.yes') : t('common.no');
      else params[k] = String(v);
    });
    return t(`agent.finding.${f.code}`, params);
  }, [t]);
}

function severityDot(sev) {
  return <span className={`notif-sev notif-sev--${sev || 'info'}`} aria-hidden="true" />;
}

export function AIInsightPage({ session, onToast, onSuggestRule }) {
  const t = useT();
  const findingText = useFindingText();
  const canPost = sessionCanPost(session, '/api/agent');

  const [status, setStatus] = useState(null);
  const [latest, setLatest] = useState(null);
  const [history, setHistory] = useState([]);
  const [openHistoryId, setOpenHistoryId] = useState(null);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [stats, setStats] = useState(null);
  const [baseline, setBaseline] = useState(null);

  const loadAll = useCallback(async () => {
    setLoading(true);
    const now = Math.floor(Date.now() / 1000);
    const from = now - 7 * 86400;
    const tzOffset = -new Date().getTimezoneOffset();
    const chartParams = `from=${from}&to=${now}&bucket=day&tzOffset=${tzOffset}`;
    const [st, la, hi, sr, br] = await Promise.all([
      api('/api/agent/status', { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/agent/digests/latest', { noRedirect: true }).catch(() => ({ ok: false })),
      api(`/api/agent/digests?limit=${HISTORY_PAGE}`, { noRedirect: true }).catch(() => ({ ok: false })),
      api(`/api/notifications/stats?${chartParams}`, { noRedirect: true }).catch(() => ({ ok: false })),
      api(`/api/notifications/baseline?${chartParams}`, { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    setLoading(false);
    if (st.ok) setStatus(st.body || null);
    if (la.ok) setLatest(la.body || null);
    if (hi.ok) setHistory(Array.isArray(hi.body?.items) ? hi.body.items : []);
    if (sr.ok) setStats(sr.body || null);
    if (br.ok) setBaseline(br.body || null);
  }, []);
  useEffect(() => { loadAll(); }, [loadAll]);

  async function generateNow(kind) {
    setGenerating(true);
    const q = kind === 'weekly' ? '?kind=weekly' : '';
    const r = await api(`/api/agent/digests/generate${q}`, { method: 'POST', noRedirect: true }).catch(() => ({ ok: false }));
    setGenerating(false);
    if (r.ok) {
      onToast?.(t('agent.generated'), 'info');
      loadAll();
    } else {
      onToast?.(r.message || t('agent.generateFailed'), 'error');
    }
  }

  const llmReady = !!status?.llm?.ready;
  const formatDay = useCallback((unixSec) => {
    const d = new Date(unixSec * 1000);
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }, []);

  return (
    <section className="workspace insight-page">
      <header className="camera-tab-header insight-head">
        <div>
          <h1>{t('agent.title')}</h1>
          <p className="camera-tab-sub">{t('agent.subtitle')}</p>
        </div>
        {canPost ? (
          <div className="insight-generate-group">
            <button type="button" className="insight-generate" onClick={() => generateNow('manual')} disabled={generating}>
              <Ico n="zap" sz={15} />
              {generating ? t('agent.generating') : t('agent.generateNow')}
            </button>
            <button type="button" className="insight-generate insight-generate--quiet" onClick={() => generateNow('weekly')} disabled={generating}>
              {t('agent.generateWeekly')}
            </button>
          </div>
        ) : null}
      </header>

      {loading && !latest ? <div className="page-placeholder">{t('common.loading')}</div> : null}

      <div className="insight-grid">
        <div className="insight-kpis">
          <StatCard label={t('agent.kpiEvents')} value={stats?.total ?? '—'} icon="activity" />
          <StatCard label={t('agent.kpiCritical')} value={stats?.critical ?? '—'} icon="warning" tone={stats?.critical > 0 ? 'danger' : 'default'} />
          <StatCard
            label={t('agent.kpiLlm')}
            value={llmReady ? (status?.llm?.model || t('agent.llmReady')) : t(`agent.llmState.${status?.llm?.mode || 'off'}`)}
            icon="cpu"
            tone={llmReady ? 'accent' : 'default'}
            hint={status?.llm?.sidecarState ? t(`agent.sidecar.${status.llm.sidecarState}`) : undefined}
          />
          <StatCard
            label={t('agent.kpiNextDigest')}
            value={status?.digest?.enabled ? `${String(status.digest.localHour).padStart(2, '0')}:00` : t('common.disabled')}
            icon="bell"
            hint={[
              status?.digest?.weeklyEnabled
                ? t('agent.weeklyOn', { day: t(`settings.opt.weekday.${status.digest.weekday || 0}`) })
                : '',
              status?.digest?.lastRunDate ? t('agent.lastRun', { date: status.digest.lastRunDate }) : '',
            ].filter(Boolean).join(' · ') || undefined}
          />
        </div>

        <ChartCard title={t('agent.chartTitle')} subtitle={t('agent.chartSub')} className="insight-span-2">
          <TimeSeriesChart
            buckets={stats?.timeseries || []}
            categories={[]}
            formatX={formatDay}
            band={baseline?.buckets}
            emptyText={t('agent.chartEmpty')}
          />
        </ChartCard>

        <DigestCard digest={latest} status={status} findingText={findingText} onSuggestRule={onSuggestRule} className="insight-span-2" />

        {llmReady && canPost ? (
          <FleetChat model={status?.llm?.model} className="insight-span-2" />
        ) : (
          <ChartCard title={t('agent.chat.title')} className="insight-span-2">
            <div className="insight-chat-off">
              <Ico n="cpu" sz={18} />
              <p>
                {canPost ? t('agent.chat.llmOff') : t('agent.chat.noAccess')}
                {session?.isSuperadmin && canPost ? ` ${t('agent.chat.llmOffHintAdmin')}` : ''}
              </p>
            </div>
            {/* No model does not mean no answers. Searching the built-in manuals needs no
                inference at all, so the half of the assistant that explains the product still
                works on an appliance that has never been given a model. */}
            <ManualSearch />
          </ChartCard>
        )}

        {history.length > 1 ? (
          <ChartCard title={t('agent.history')} className="insight-span-2">
            <ul className="insight-history">
              {history.map((d) => (
                <li key={d.id}>
                  <button
                    type="button"
                    className="insight-history-row"
                    onClick={() => setOpenHistoryId(openHistoryId === d.id ? null : d.id)}
                    aria-expanded={openHistoryId === d.id}
                  >
                    {severityDot(d.severity)}
                    <span className="insight-history-when">{formatTimestamp(d.createdAt)}</span>
                    <span className="insight-history-kind">{t(`agent.kind.${d.kind || 'daily'}`)}</span>
                    <span className="insight-history-n">{t('agent.findingCount', { n: String((d.findings || []).length) })}</span>
                    <Ico n={openHistoryId === d.id ? 'chev-up' : 'chev-down'} sz={14} />
                  </button>
                  {openHistoryId === d.id ? (
                    <div className="insight-history-body">
                      {d.narrative ? <p className="insight-narrative">{d.narrative}</p> : null}
                      <FindingList findings={d.findings} findingText={findingText} />
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          </ChartCard>
        ) : null}
      </div>
    </section>
  );
}

// DigestCard renders the latest digest: header chips, optional LLM narrative,
// and the localized findings list.
function DigestCard({ digest, status, findingText, onSuggestRule, className }) {
  const t = useT();
  if (!digest) {
    return (
      <ChartCard title={t('agent.latest')} className={className}>
        <div className="insight-chat-off">
          <Ico n="zap" sz={18} />
          <p>{t('agent.noDigestYet')}</p>
        </div>
      </ChartCard>
    );
  }
  const period = `${formatTimestamp(digest.periodStart)} – ${formatTimestamp(digest.periodEnd)}`;
  return (
    <ChartCard
      title={t('agent.latest')}
      subtitle={period}
      className={className}
      actions={(
        <span className={`insight-chip${digest.narrativeSource === 'llm' ? ' insight-chip--llm' : ''}`}>
          {digest.narrativeSource === 'llm'
            ? t('agent.narrativeByLlm', { model: digest.model || 'LLM' })
            : t('agent.narratorOnly')}
        </span>
      )}
    >
      {digest.narrative ? <p className="insight-narrative">{digest.narrative}</p> : null}
      <FindingList findings={digest.findings} findingText={findingText} onSuggestRule={onSuggestRule} />
      {status?.digest?.enabled === false ? <p className="insight-note">{t('agent.scheduleOff')}</p> : null}
    </ChartCard>
  );
}

function FindingList({ findings, findingText, onSuggestRule }) {
  const t = useT();
  if (!Array.isArray(findings) || findings.length === 0) {
    return <p className="insight-note">{t('agent.noFindings')}</p>;
  }
  return (
    <ul className="anomaly-findings insight-findings">
      {findings.map((f, i) => (
        <li key={`${f.code}-${i}`} className={f.severity === 'critical' ? 'is-high' : (f.severity === 'warning' ? 'is-low' : '')}>
          {severityDot(f.severity)}
          <span className="insight-finding-text">{findingText(f)}</span>
          {f.code === 'suggested_rule' && onSuggestRule ? (
            <button
              type="button"
              className="insight-suggest-btn"
              onClick={() => onSuggestRule({
                name: f.params?.suggestedName || '',
                nodeId: f.params?.nodeId || '',
                category: f.params?.category || '',
                windowSeconds: Number(f.params?.windowSeconds) || 120,
                graceSeconds: Number(f.params?.graceSeconds) || 5,
                cooldownSeconds: Number(f.params?.cooldownSeconds) || 300,
              })}
            >
              <Ico n="git-branch" sz={13} /> {t('agent.createRule')}
            </button>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

// SourceChip is one cited manual section.
//
// A section from THIS app's manual opens in the help drawer; one from another product in the
// suite is labelled and inert, because this SPA can only render the manual its own server
// serves. Showing it anyway is deliberate — "MyMataSan manual › Adding cameras" tells the reader
// which product and which page to go to, which is most of the value of a citation.
function SourceChip({ source }) {
  const manual = useManual();
  const label = source.heading || source.title;
  if (source.local) {
    return (
      <button
        type="button"
        className="insight-source insight-source--local"
        onClick={() => manual.openHelp(source.slug, source.anchor)}
        title={source.path}
      >
        <Ico n="book" sz={12} />
        {label}
      </button>
    );
  }
  return (
    <span className="insight-source" title={source.path}>
      <Ico n="book" sz={12} />
      <span className="insight-source-app">{source.appTitle}</span>
      {label}
    </span>
  );
}

// SourceList shows what the answer was grounded in.
//
// Cited sections lead; the rest are offered separately as further reading. The distinction is
// the server's — it marks which refs the finished answer actually referenced — and keeping it
// visible is what stops a source list from implying the answer rests on a page the model never
// used.
function SourceList({ sources }) {
  const t = useT();
  const cited = sources.filter((s) => s.cited);
  const related = sources.filter((s) => !s.cited);
  return (
    <div className="insight-sources">
      {cited.length ? (
        <div className="insight-sources-row">
          <span className="insight-sources-label">{t('agent.chat.sources')}</span>
          {cited.map((s) => <SourceChip key={s.ref} source={s} />)}
        </div>
      ) : null}
      {related.length ? (
        <div className="insight-sources-row">
          <span className="insight-sources-label">{t('agent.chat.sourcesRelated')}</span>
          {related.map((s) => <SourceChip key={s.ref} source={s} />)}
        </div>
      ) : null}
    </div>
  );
}

// ManualSearch is the retrieval half of the assistant on its own, for when no language model is
// available. It answers with the sections that match rather than with prose.
function ManualSearch() {
  const t = useT();
  const lang = document.documentElement.lang || 'en';
  const [question, setQuestion] = useState('');
  const [items, setItems] = useState(null);
  const [busy, setBusy] = useState(false);

  async function search(e) {
    e?.preventDefault();
    const q = question.trim();
    if (!q || busy) return;
    setBusy(true);
    const res = await api(
      `/api/agent/docs?q=${encodeURIComponent(q)}&lang=${encodeURIComponent(lang)}`,
      { noRedirect: true },
    ).catch(() => ({ ok: false }));
    setBusy(false);
    setItems(res.ok && Array.isArray(res.body?.items) ? res.body.items : []);
  }

  return (
    <div className="insight-docs">
      <form className="insight-chat-form" onSubmit={search}>
        <input
          type="text"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder={t('agent.docs.placeholder')}
          maxLength={500}
          aria-label={t('agent.docs.search')}
        />
        <button type="submit" disabled={busy || !question.trim()} aria-label={t('agent.docs.search')}>
          <Ico n="search" sz={15} />
        </button>
      </form>
      {items === null ? (
        <p className="insight-note">{t('agent.docs.hint')}</p>
      ) : items.length === 0 ? (
        <p className="insight-note">{t('agent.docs.none')}</p>
      ) : (
        <ul className="insight-docs-list">
          {items.map((s) => (
            <li key={s.ref}>
              <SourceChip source={s} />
              {s.snippet ? <p className="insight-docs-snippet">{s.snippet}</p> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// FleetChat is the streaming ask-the-fleet box. One in-flight question at a
// time; a new question aborts the previous stream.
function FleetChat({ model, className }) {
  const t = useT();
  const lang = document.documentElement.lang || 'en';
  const [transcript, setTranscript] = useState([]); // {role, content, error?}
  const [question, setQuestion] = useState('');
  const [busy, setBusy] = useState(false);
  const abortRef = useRef(null);
  const scrollRef = useRef(null);

  useEffect(() => () => abortRef.current?.abort(), []);
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [transcript]);

  async function ask(e) {
    e?.preventDefault();
    const q = question.trim();
    if (!q || busy) return;
    setQuestion('');
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setBusy(true);

    // History = the prior turns (the server truncates further).
    const history = transcript
      .filter((m) => !m.error)
      .slice(-4)
      .map((m) => ({ role: m.role, content: m.content }));
    setTranscript((tr) => [...tr, { role: 'user', content: q }, { role: 'assistant', content: '' }]);

    const appendDelta = (text) => setTranscript((tr) => {
      const next = tr.slice();
      const last = { ...next[next.length - 1] };
      last.content += text;
      next[next.length - 1] = last;
      return next;
    });
    // Sources arrive AFTER the answer (they carry which excerpts it actually cited), so they
    // attach to the message that is already on screen rather than gating its render.
    const attachSources = (sources) => setTranscript((tr) => {
      const next = tr.slice();
      next[next.length - 1] = { ...next[next.length - 1], sources };
      return next;
    });
    const failLast = (message) => setTranscript((tr) => {
      const next = tr.slice();
      const last = { ...next[next.length - 1], error: true };
      if (!last.content) last.content = message;
      next[next.length - 1] = last;
      return next;
    });

    try {
      const headers = { 'Content-Type': 'application/json' };
      const token = csrfToken();
      if (token) headers['X-CSRF-Token'] = token;
      const res = await fetch(`${apiBase()}/api/agent/chat`, {
        method: 'POST',
        credentials: 'same-origin',
        headers,
        signal: ac.signal,
        body: JSON.stringify({ question: q, history, lang, tzOffset: -new Date().getTimezoneOffset() }),
      });
      if (!res.ok || !res.body) {
        let message = t('agent.chat.failed');
        try {
          const payload = await res.json();
          if (payload?.code === 'llm_starting') message = t('agent.chat.llmStarting');
          else if (payload?.code === 'llm_disabled') message = t('agent.chat.llmOff');
          else if (payload?.message) message = payload.message;
        } catch (_) { /* non-JSON error body */ }
        failLast(message);
        return;
      }

      // Parse the SSE frames off the raw stream: events separated by \n\n,
      // each with `event:` and `data:` lines.
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        let idx;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          let event = 'message';
          let data = '';
          frame.split('\n').forEach((line) => {
            if (line.startsWith('event:')) event = line.slice(6).trim();
            else if (line.startsWith('data:')) data += line.slice(5).trim();
          });
          if (!data) continue;
          let payload = null;
          try { payload = JSON.parse(data); } catch (_) { continue; }
          if (event === 'delta' && payload.text) appendDelta(payload.text);
          else if (event === 'sources' && Array.isArray(payload.items)) attachSources(payload.items);
          else if (event === 'error') failLast(payload.message || t('agent.chat.failed'));
        }
      }
      // An empty answer (model produced nothing) reads as a failure, not silence.
      setTranscript((tr) => {
        const last = tr[tr.length - 1];
        if (last && last.role === 'assistant' && !last.content && !last.error) {
          const next = tr.slice();
          next[next.length - 1] = { ...last, error: true, content: t('agent.chat.failed') };
          return next;
        }
        return tr;
      });
    } catch (err) {
      if (err?.name !== 'AbortError') failLast(t('agent.chat.failed'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <ChartCard
      title={t('agent.chat.title')}
      subtitle={model ? t('agent.chat.poweredBy', { model }) : undefined}
      className={className}
    >
      <div className="insight-chat" ref={scrollRef}>
        {transcript.length === 0 ? (
          <div className="insight-chat-hint">
            <p>{t('agent.chat.hint')}</p>
            <p className="insight-note">{t('agent.chat.grounding')}</p>
          </div>
        ) : transcript.map((m, i) => (
          <div key={i} className={`insight-msg insight-msg--${m.role}${m.error ? ' insight-msg--error' : ''}`}>
            {m.content || (busy && i === transcript.length - 1 ? t('agent.chat.thinking') : '')}
            {m.sources?.length ? <SourceList sources={m.sources} /> : null}
          </div>
        ))}
      </div>
      <form className="insight-chat-form" onSubmit={ask}>
        <input
          type="text"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder={t('agent.chat.placeholder')}
          maxLength={2000}
          aria-label={t('agent.chat.title')}
        />
        <button type="submit" disabled={busy || !question.trim()} aria-label={t('agent.chat.send')}>
          <Ico n="send" sz={15} />
        </button>
      </form>
    </ChartCard>
  );
}
