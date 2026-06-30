import { useRef } from 'react';

// Card wrapper with a cursor-following spotlight glow and optional 3D tilt.
// Pointer position drives --mx/--my (the glow) and, when tilt is on, an inline
// transform so it overrides the scroll-reveal transform cleanly. Tilt is skipped
// when the user prefers reduced motion.
const reduce =
  typeof window !== 'undefined' &&
  window.matchMedia &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export default function SpotlightCard({
  as: Tag = 'article',
  tilt = false,
  className = '',
  children,
  ...rest
}) {
  const ref = useRef(null);

  const onMove = (e) => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const x = e.clientX - r.left;
    const y = e.clientY - r.top;
    el.style.setProperty('--mx', `${x}px`);
    el.style.setProperty('--my', `${y}px`);
    if (tilt && !reduce) {
      const rx = (y / r.height - 0.5) * -7;
      const ry = (x / r.width - 0.5) * 9;
      el.style.transform = `perspective(1000px) rotateX(${rx}deg) rotateY(${ry}deg) translateY(-4px)`;
    }
  };

  const onLeave = () => {
    const el = ref.current;
    if (el) el.style.transform = '';
  };

  return (
    <Tag
      ref={ref}
      className={`spot ${className}`}
      onMouseMove={onMove}
      onMouseLeave={onLeave}
      {...rest}
    >
      <span className="spot__glow" aria-hidden="true" />
      {children}
    </Tag>
  );
}
