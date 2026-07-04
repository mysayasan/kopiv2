import { useEffect, useRef, useState } from 'react';
import Screenshot from '../components/Screenshot.jsx';
import Reveal from '../components/Reveal.jsx';
import { LiveGridMockup, RuleEditorMockup, NotificationsMockup } from '../components/Mockups.jsx';
import { useContent } from '../i18n/index.jsx';

const fallbacks = {
  live: <LiveGridMockup />,
  rules: <RuleEditorMockup />,
  notifications: <NotificationsMockup />,
  // The dashboard/destinations/security shots are real captures; reuse a generic
  // mockup only as a last-resort fallback if the image fails to load.
  dashboard: <NotificationsMockup />,
  destinations: <NotificationsMockup />,
  security: <RuleEditorMockup />,
};

const reduce =
  typeof window !== 'undefined' &&
  window.matchMedia &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export default function Showcase() {
  const { showcase } = useContent();
  const shots = showcase.shots;
  const [active, setActive] = useState(0);
  const [paused, setPaused] = useState(false);
  const tiltRef = useRef(null);

  const onTilt = (e) => {
    if (reduce) return;
    const el = tiltRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const rx = ((e.clientY - r.top) / r.height - 0.5) * -5;
    const ry = ((e.clientX - r.left) / r.width - 0.5) * 6;
    el.style.transform = `rotateX(${rx}deg) rotateY(${ry}deg)`;
  };
  const onTiltLeave = () => {
    if (tiltRef.current) tiltRef.current.style.transform = '';
  };

  // Auto-advance every 5s; pauses on hover/focus or after a manual pick.
  useEffect(() => {
    if (paused) return;
    const t = setInterval(() => setActive((a) => (a + 1) % shots.length), 5000);
    return () => clearInterval(t);
  }, [paused, shots.length]);

  const current = shots[active];

  return (
    <section className="section section--alt" id="showcase">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{showcase.kicker}</p>
          <h2 className="section__title">{showcase.title}</h2>
          <p className="section__lead">{showcase.lead}</p>
        </Reveal>

        <Reveal className="showcase">
          <div className="showcase__tabs" role="tablist" aria-label="Product views">
            {shots.map((s, i) => (
              <button
                key={s.key}
                role="tab"
                aria-selected={i === active}
                className={`showcase__tab ${i === active ? 'is-active' : ''}`}
                onClick={() => {
                  setActive(i);
                  setPaused(true);
                }}
              >
                {showcase.tabs[s.key] || s.key}
                {i === active && !paused && <span className="showcase__progress" />}
              </button>
            ))}
          </div>

          <div
            className="showcase__stage"
            onMouseEnter={() => setPaused(true)}
            onMouseLeave={() => {
              setPaused(false);
              onTiltLeave();
            }}
            onMouseMove={onTilt}
            onFocus={() => setPaused(true)}
            onBlur={() => setPaused(false)}
          >
            <div className="showcase__tilt" ref={tiltRef}>
              <Screenshot
                key={current.key}
                src={current.src}
                alt={current.alt}
                url={current.url}
                fallback={fallbacks[current.key]}
              />
            </div>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
