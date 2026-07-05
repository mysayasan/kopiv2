import { useEffect, useRef, useState } from 'react';
import { useContent } from '../i18n/index.jsx';
import Icon from './Icon.jsx';

const TgIcon = ({ size = 22 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    <path d="M21.94 4.6 18.9 19.2c-.23 1.02-.84 1.27-1.7.79l-4.7-3.46-2.27 2.18c-.25.25-.46.46-.94.46l.33-4.78 8.7-7.86c.38-.34-.08-.53-.59-.19L6.78 13.2 2.1 11.73c-1.02-.32-1.04-1.02.21-1.51l18.32-7.06c.85-.31 1.6.2 1.31 1.44Z" />
  </svg>
);

const initial = { name: '', message: '', website: '' }; // website = honeypot

export default function ContactWidget() {
  const { contact } = useContent();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(initial);
  const [state, setState] = useState('idle'); // idle | sending | sent | error
  const [error, setError] = useState('');
  const panelRef = useRef(null);

  // close on outside click / Escape
  useEffect(() => {
    if (!open) return;
    const onKey = (e) => e.key === 'Escape' && setOpen(false);
    const onClick = (e) => {
      if (panelRef.current && !panelRef.current.contains(e.target)) setOpen(false);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClick);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClick);
    };
  }, [open]);

  const submit = async (e) => {
    e.preventDefault();
    if (state === 'sending') return;
    if (!form.message.trim()) {
      setError(contact.errRequired);
      return;
    }
    setState('sending');
    setError('');
    try {
      const res = await fetch(contact.endpoint, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          name: form.name.trim(),
          message: form.message.trim(),
          website: form.website, // honeypot
          page: typeof location !== 'undefined' ? location.href : '',
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) throw new Error(data.error || `Request failed (${res.status})`);
      setState('sent');
      setForm(initial);
    } catch (err) {
      setState('error');
      setError(
        err.message?.includes('Failed to fetch') || err.message?.includes('404')
          ? contact.errUnreachable
          : err.message || contact.errGeneric
      );
    }
  };

  if (!contact.enabled) return null;

  return (
    <div className="cw" ref={panelRef}>
      {open && (
        <div className="cw__panel" role="dialog" aria-label="Contact">
          <div className="cw__head">
            <span className="cw__title">{contact.title}</span>
            <button className="cw__x" aria-label="Close" onClick={() => setOpen(false)}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M6 6l12 12M18 6L6 18" /></svg>
            </button>
          </div>
          <p className="cw__blurb">{contact.blurb}</p>

          {state === 'sent' ? (
            <div className="cw__sent">
              <div className="cw__check">
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
              </div>
              <p>{contact.sent}</p>
              <button className="cw__again" onClick={() => setState('idle')}>{contact.again}</button>
            </div>
          ) : (
            <form className="cw__form" onSubmit={submit}>
              <input
                className="cw__input"
                type="text"
                placeholder={contact.namePlaceholder}
                value={form.name}
                maxLength={100}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
              <textarea
                className="cw__input cw__textarea"
                placeholder={contact.messagePlaceholder}
                rows={3}
                maxLength={2000}
                value={form.message}
                onChange={(e) => setForm({ ...form, message: e.target.value })}
                required
              />
              {/* honeypot: hidden from humans, bots fill it */}
              <input
                className="cw__hp"
                tabIndex={-1}
                autoComplete="off"
                value={form.website}
                onChange={(e) => setForm({ ...form, website: e.target.value })}
                aria-hidden="true"
              />
              {error && <p className="cw__err">{error}</p>}
              <button className="cw__send" type="submit" disabled={state === 'sending'}>
                {state === 'sending' ? contact.sending : contact.send}
              </button>
            </form>
          )}

          {contact.supportUrl ? (
            <a
              className="cw__support"
              href={contact.supportUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              <span className="cw__coffee"><Icon name="coffee" size={16} /></span>
              <span className="cw__support-text">
                <b>{contact.support}</b>
                {contact.supportBlurb ? <span>{contact.supportBlurb}</span> : null}
              </span>
            </a>
          ) : null}
        </div>
      )}

      <button
        className={`cw__fab ${open ? 'is-open' : ''}`}
        aria-label={open ? 'Close contact' : 'Contact me on Telegram'}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <TgIcon />
        <span className="cw__fab-label">{contact.fabLabel}</span>
      </button>
    </div>
  );
}
