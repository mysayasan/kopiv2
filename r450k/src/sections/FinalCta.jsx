import Icon from '../components/Icon.jsx';
import { useContent } from '../i18n/index.jsx';

export default function FinalCta() {
  const { finalCta } = useContent();
  return (
    <section className="section">
      <div className="container">
        <div className="cta">
          <div className="cta__glow" aria-hidden="true" />
          <h2 className="cta__title">{finalCta.title}</h2>
          <p className="cta__body">{finalCta.body}</p>
          <a className="btn btn--primary" href={finalCta.cta.href}>
            {finalCta.cta.label} <Icon name="arrow" size={18} />
          </a>
        </div>
      </div>
    </section>
  );
}
