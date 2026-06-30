// Browser-chrome frame around a product screenshot. If a real image exists at
// `src` (drop PNGs into public/screenshots/), it renders that; otherwise it falls
// back to the inline SVG mockup so the section never looks empty before real
// captures are available.
import { useState } from 'react';

export default function Screenshot({ src, alt, fallback, url = 'mymatasan.local' }) {
  const [failed, setFailed] = useState(false);
  const showImage = src && !failed;
  return (
    <figure className="shot">
      <div className="shot__chrome">
        <span className="shot__dot" />
        <span className="shot__dot" />
        <span className="shot__dot" />
        <span className="shot__url">{url}</span>
      </div>
      <div className="shot__body">
        {showImage ? (
          <img src={src} alt={alt} loading="lazy" onError={() => setFailed(true)} />
        ) : (
          fallback
        )}
      </div>
      <figcaption className="shot__cap">{alt}</figcaption>
    </figure>
  );
}
