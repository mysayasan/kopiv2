import Icon from '../components/Icon.jsx';
import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import { useContent } from '../i18n/index.jsx';

export default function Features() {
  const { features } = useContent();
  return (
    <section className="section" id="features">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{features.kicker}</p>
          <h2 className="section__title">{features.title}</h2>
          <p className="section__lead">{features.lead}</p>
        </Reveal>
        <Reveal className="grid grid--features" stagger>
          {features.items.map((f, i) => (
            <SpotlightCard className="card" key={f.title} tilt style={{ '--i': i }}>
              <div className="card__icon"><Icon name={f.icon} size={22} /></div>
              <h3 className="card__title">{f.title}</h3>
              <p className="card__body">{f.body}</p>
            </SpotlightCard>
          ))}
        </Reveal>
      </div>
    </section>
  );
}
