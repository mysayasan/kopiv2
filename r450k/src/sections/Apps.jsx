import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import { useContent } from '../i18n/index.jsx';

export default function Apps() {
  const { apps } = useContent();
  return (
    <section className="section section--alt" id="apps">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{apps.kicker}</p>
          <h2 className="section__title">{apps.title}</h2>
          <p className="section__lead">{apps.subtitle}</p>
        </Reveal>
        <Reveal className="grid grid--apps" stagger>
          {apps.items.map((a, i) => (
            <SpotlightCard className={`appcard ${a.available ? 'appcard--live' : ''}`} key={a.name} style={{ '--i': i }}>
              <div className="appcard__head">
                <h3 className="appcard__name">{a.name}</h3>
                <span className={`badge ${a.available ? 'badge--live' : 'badge--muted'}`}>
                  {a.available ? apps.available : apps.platform}
                </span>
              </div>
              <p className="appcard__status">{a.status}</p>
              <p className="appcard__body">{a.body}</p>
            </SpotlightCard>
          ))}
        </Reveal>
      </div>
    </section>
  );
}
