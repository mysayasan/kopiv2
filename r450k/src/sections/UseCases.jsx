import Reveal from '../components/Reveal.jsx';
import { useCases } from '../content.js';

export default function UseCases() {
  return (
    <section className="section" id="use-cases">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">Use cases</p>
          <h2 className="section__title">{useCases.title}</h2>
        </Reveal>
        <Reveal className="grid grid--use" stagger>
          {useCases.items.map((u, i) => (
            <article className="use" key={u.title} style={{ '--i': i }}>
              <h3 className="use__title">{u.title}</h3>
              <p className="use__body">{u.body}</p>
            </article>
          ))}
        </Reveal>
      </div>
    </section>
  );
}
