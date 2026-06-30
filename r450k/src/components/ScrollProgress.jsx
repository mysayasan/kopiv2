import { useScrollProgress } from '../hooks/useScroll.js';

// Thin gradient bar at the very top showing read progress.
export default function ScrollProgress() {
  const p = useScrollProgress();
  return (
    <div className="scrollbar" aria-hidden="true">
      <div className="scrollbar__fill" style={{ transform: `scaleX(${p})` }} />
    </div>
  );
}
