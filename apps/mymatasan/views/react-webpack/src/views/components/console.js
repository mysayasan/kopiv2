import { useEffect, useRef } from 'react';
import { useT } from '@shared/i18n';

// ConsoleLog renders streamed text in a terminal-style box: monospace, dark, and
// auto-scrolled to the newest output (like a real console). It's a generic,
// presentation-only component — feed it a growing `log` string from any source
// (dependency installer, training output, a shell command, …).
//
// Props:
//   log      growing text to show (only the last `maxChars` are rendered)
//   title    optional header label (omit for a bare box)
//   running  show a "running" indicator + blinking cursor while true
//   maxChars trailing characters to keep (default 4000) so the DOM stays light
//   empty    placeholder text when there's no log yet
export function ConsoleLog({ log = '', title, running = false, maxChars = 4000, empty = '' }) {
  const t = useT();
  const bodyRef = useRef(null);
  const text = log.length > maxChars ? `…\n${log.slice(-maxChars)}` : log;

  // Keep the view pinned to the bottom as new output streams in, but only when
  // the user is already near the bottom — so scrolling up to read isn't yanked
  // back down on the next update.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [text]);

  return (
    <div className={`console-log${running ? ' is-running' : ''}`}>
      {title ? (
        <div className="console-log-head">
          <span className="console-log-dots" aria-hidden="true"><i /><i /><i /></span>
          <span className="console-log-title">{title}</span>
          {running ? <span className="console-log-status">{t('console.running')}</span> : null}
        </div>
      ) : null}
      <pre className="console-log-body" ref={bodyRef}>
        {text || empty}
        {running ? <span className="console-log-cursor">▋</span> : null}
      </pre>
    </div>
  );
}

export default ConsoleLog;
