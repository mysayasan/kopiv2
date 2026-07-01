import Reveal from '../components/Reveal.jsx';
import { useContent } from '../i18n/index.jsx';

export default function HowItWorks() {
  const { how } = useContent();
  return (
    <section className="section section--alt" id="how">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{how.kicker}</p>
          <h2 className="section__title">{how.title}</h2>
        </Reveal>
        <div className="steps-wrap">
          <div className="flowline" aria-hidden="true"><span className="flowline__dot" /></div>
          <Reveal as="ol" className="steps" stagger>
            {how.steps.map((s, i) => (
              <li className="step" key={s.n} style={{ '--i': i }}>
                <div className="step__n">{s.n}</div>
                <h3 className="step__title">{s.title}</h3>
                <p className="step__body">{s.body}</p>
              </li>
            ))}
          </Reveal>
        </div>
      </div>
    </section>
  );
}
