import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import { tiers } from '../content.js';

export default function Tiers() {
  return (
    <section className="section" id="tiers">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">Hardware</p>
          <h2 className="section__title">{tiers.title}</h2>
          <p className="section__lead">{tiers.lead}</p>
        </Reveal>
        <Reveal className="grid grid--tiers" stagger>
          {tiers.items.map((t, i) => (
            <SpotlightCard className={`tier ${t.featured ? 'tier--featured' : ''}`} key={t.name} style={{ '--i': i }}>
              {t.featured && <span className="tier__flag">Recommended</span>}
              <h3 className="tier__name">{t.name}</h3>
              <p className="tier__spec">{t.spec}</p>
              <p className="tier__load">{t.load}</p>
              <p className="tier__detail">{t.detail}</p>
            </SpotlightCard>
          ))}
        </Reveal>
      </div>
    </section>
  );
}
