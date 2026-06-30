import { useRef } from 'react';

// Anchor that subtly drifts toward the cursor on hover (magnetic effect),
// easing back on leave. No-op under prefers-reduced-motion.
const reduce =
  typeof window !== 'undefined' &&
  window.matchMedia &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export default function MagneticButton({ className = '', strength = 0.3, children, ...rest }) {
  const ref = useRef(null);

  const onMove = (e) => {
    if (reduce) return;
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const x = (e.clientX - r.left - r.width / 2) * strength;
    const y = (e.clientY - r.top - r.height / 2) * strength;
    el.style.transform = `translate(${x}px, ${y}px)`;
  };

  const onLeave = () => {
    const el = ref.current;
    if (el) el.style.transform = '';
  };

  return (
    <a ref={ref} className={`magnetic ${className}`} onMouseMove={onMove} onMouseLeave={onLeave} {...rest}>
      {children}
    </a>
  );
}
