import { useEffect, useRef, useState } from 'react';

// Toggles `inView` true the first time the element scrolls into the viewport.
// Used to drive scroll-reveal animations. Honors prefers-reduced-motion by
// reporting visible immediately so nothing stays hidden.
export default function useInView({ threshold = 0.18, once = true } = {}) {
  const ref = useRef(null);
  const reduce =
    typeof window !== 'undefined' &&
    window.matchMedia &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const [inView, setInView] = useState(reduce);

  useEffect(() => {
    if (reduce) return;
    const el = ref.current;
    if (!el || typeof IntersectionObserver === 'undefined') {
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
  }, [threshold, once, reduce]);

  return [ref, inView];
}
