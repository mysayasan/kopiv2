import { useState } from 'react';
import Reveal from '../components/Reveal.jsx';
import SpotlightCard from '../components/SpotlightCard.jsx';
import Icon from '../components/Icon.jsx';
import TallyModal from '../components/TallyModal.jsx';
import { useContent } from '../i18n/index.jsx';

export default function Pricing() {
  const { pricing: t } = useContent();
  // A tier with `tallyForm` opens its form in a modal (kept on-site) instead of
  // navigating away — used by "Contact us" so it no longer opens a mail client.
  const [tallyForm, setTallyForm] = useState(null);
  return (
    <section className="section" id="pricing">
      <div className="container">
        <Reveal className="section__head">
          <p className="kicker">{t.kicker}</p>
          <h2 className="section__title">{t.title}</h2>
          <p className="section__lead">{t.lead}</p>
        </Reveal>

        <Reveal className="grid grid--pricing" stagger>
          {t.tiers.map((tier, i) => (
            <SpotlightCard
              className={`price ${tier.featured ? 'price--featured' : ''}`}
              key={tier.name}
              style={{ '--i': i }}
            >
              {tier.featured ? <span className="price__flag">{t.popular}</span> : null}
              <h3 className="price__name">{tier.name}</h3>
              <div className="price__amount">
                <span className="price__num">{tier.price}</span>
                {tier.period ? <span className="price__per">{tier.period}</span> : null}
              </div>
              <ul className="price__list">
                {tier.features.map((f) => (
                  <li key={f}>
                    <Icon name="check" size={17} />
                    {f}
                  </li>
                ))}
              </ul>
              {tier.tallyForm ? (
                <button
                  type="button"
                  className={`btn ${tier.featured ? 'btn--primary' : 'btn--ghost'} price__cta`}
                  onClick={() => setTallyForm(tier.tallyForm)}
                >
                  {tier.cta}
                </button>
              ) : (
                <a
                  className={`btn ${tier.featured ? 'btn--primary' : 'btn--ghost'} price__cta`}
                  href={tier.href}
                >
                  {tier.cta}
                </a>
              )}
            </SpotlightCard>
          ))}
        </Reveal>

        {t.note ? (
          <Reveal className="pricing__note">
            <p>{t.note}</p>
          </Reveal>
        ) : null}
      </div>

      <TallyModal
        open={Boolean(tallyForm)}
        onClose={() => setTallyForm(null)}
        src={tallyForm ? `${tallyForm}?hideTitle=1` : ''}
        title={t.contactTitle}
        blurb={t.contactBlurb}
        closeLabel={t.contactClose}
      />
    </section>
  );
}
