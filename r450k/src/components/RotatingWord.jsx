import { useEffect, useState } from 'react';

// Cycles through words with a slide/fade swap. Used in the hero headline.
const reduce =
  typeof window !== 'undefined' &&
  window.matchMedia &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export default function RotatingWord({ words, interval = 2600 }) {
  const [i, setI] = useState(0);
  useEffect(() => {
    if (reduce || words.length < 2) return;
    const t = setInterval(() => setI((v) => (v + 1) % words.length), interval);
    return () => clearInterval(t);
  }, [words.length, interval]);

  return (
    <span className="rotw" aria-live="polite">
      {/* invisible sizer keeps layout stable at the widest word */}
      <span className="rotw__sizer">
        {words.reduce((a, b) => (b.length > a.length ? b : a), '')}
      </span>
      <span key={i} className="rotw__word">
        {words[i]}
      </span>
    </span>
  );
}
