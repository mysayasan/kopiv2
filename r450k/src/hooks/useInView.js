import { useEffect, useRef, useState } from 'react';

// Toggles `inView` true the first time the element scrolls into the viewport.
// Used to drive scroll-reveal animations. Initial state is always `false` so the
// server-prerendered markup matches the first client render (hydration-safe);
// reduced-motion users are revealed immediately in the effect below.
export default function useInView({ threshold = 0.18, once = true } = {}) {
  const ref = useRef(null);
  const [inView, setInView] = useState(false);

  useEffect(() => {
    const reduce =
      window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const el = ref.current;
    if (reduce || !el || typeof IntersectionObserver === 'undefined') {
      setInView(true);
      return;
    }
    const obs = new IntersectionObserver(
      (entries) => {
        entries.forEach((e) => {
          if (e.isIntersecting) {
            setInView(true);
            if (once) obs.unobserve(e.target);
          } else if (!once) {
            setInView(false);
          }
        });
      },
      { threshold }
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [threshold, once]);

  return [ref, inView];
}
