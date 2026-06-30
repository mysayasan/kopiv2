import Icon from '../components/Icon.jsx';
import Reveal from '../components/Reveal.jsx';
import RotatingWord from '../components/RotatingWord.jsx';
import MagneticButton from '../components/MagneticButton.jsx';
import { hero } from '../content.js';

export default function Hero() {
  return (
    <section className="hero">
      <div className="hero__glow" aria-hidden="true" />
      <div className="hero__grid" aria-hidden="true" />
      <Reveal className="container hero__inner" stagger>
        <p className="eyebrow" style={{ '--i': 0 }}>
          <span className="eyebrow__pulse" /> {hero.eyebrow}
        </p>
        <h1 className="hero__title" style={{ '--i': 1 }}>
          <span className="hero__title-lead">{hero.titleLead} </span>
          <RotatingWord words={hero.rotate} />
        </h1>
        <p className="hero__subtitle" style={{ '--i': 2 }}>{hero.subtitle}</p>
        <div className="hero__cta" style={{ '--i': 3 }}>
          <MagneticButton className="btn btn--primary" href={hero.primaryCta.href}>
            {hero.primaryCta.label} <Icon name="arrow" size={18} />
          </MagneticButton>
          <MagneticButton className="btn btn--ghost" href={hero.secondaryCta.href} strength={0.2}>
            {hero.secondaryCta.label}
          </MagneticButton>
        </div>
        <div className="hero__stats" style={{ '--i': 4 }}>
          {hero.stats.map((s) => (
            <div className="stat" key={s.label}>
              <div className="stat__value">{s.value}</div>
              <div className="stat__label">{s.label}</div>
            </div>
          ))}
        </div>
        {hero.note && <p className="hero__note" style={{ '--i': 5 }}>{hero.note}</p>}
      </Reveal>
    </section>
  );
}
