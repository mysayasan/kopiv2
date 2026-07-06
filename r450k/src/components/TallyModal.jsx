import { useEffect, useRef } from 'react';

// TallyModal is a dismissible modal that hosts an embedded Tally form in an
// iframe. Shared by the post-download feedback prompt (Downloads) and the
// pricing "Contact us" CTA (Pricing). Closes on Escape, outside click, or the X,
// and locks page scroll while open. Renders nothing when `open` is false.
export default function TallyModal({ open, onClose, src, title, blurb, closeLabel = 'Close' }) {
  const panelRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e) => e.key === 'Escape' && onClose();
    const onDown = (e) => {
      if (panelRef.current && !panelRef.current.contains(e.target)) onClose();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="dlmodal" role="dialog" aria-modal="true" aria-label={title}>
      <div className="dlmodal__panel" ref={panelRef}>
        <div className="dlmodal__head">
          <div className="dlmodal__heading">
            <p className="dlmodal__title">{title}</p>
            {blurb ? <p className="dlmodal__blurb">{blurb}</p> : null}
          </div>
          <button type="button" className="dlmodal__x" aria-label={closeLabel} onClick={onClose}>
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M6 6l12 12M18 6L6 18" /></svg>
          </button>
        </div>
        <div className="dlmodal__body">
          <iframe className="dlmodal__frame" src={src} title={title} loading="lazy" />
        </div>
      </div>
    </div>
  );
}
