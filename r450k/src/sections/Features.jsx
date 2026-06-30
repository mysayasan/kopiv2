import Icon from '../components/Icon.jsx';
import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import { features } from '../content.js';

export default function Features() {
  return (
    <section className="section" id="features">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">Capabilities</p>
          <h2 className="section__title">Everything an edge AI camera node needs.</h2>
          <p className="section__lead">
            Detection, recording, live view, security, and fleet management — built to run on small
            devices, entirely on your own network.
          </p>
        </Reveal>
        <Reveal className="grid grid--features" stagger>
          {features.map((f, i) => (
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
