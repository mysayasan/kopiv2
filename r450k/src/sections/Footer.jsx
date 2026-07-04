import Logo from '../components/Logo.jsx';
import Icon from '../components/Icon.jsx';
import { useContent } from '../i18n/index.jsx';

export default function Footer() {
  const { footer } = useContent();
  const year = new Date().getFullYear();
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <div className="footer__brand">
          <Logo />
          <p className="footer__tagline">{footer.tagline}</p>
          <p className="footer__note">{footer.note}</p>
          {footer.supportUrl ? (
            <div className="footer__support">
              <a className="bmc" href={footer.supportUrl} target="_blank" rel="noopener noreferrer">
                <Icon name="coffee" size={17} /> {footer.support}
              </a>
              {footer.supportBlurb ? <p className="footer__support-blurb">{footer.supportBlurb}</p> : null}
            </div>
          ) : null}
        </div>
        <div className="footer__cols">
          {footer.columns.map((c) => (
            <div className="footer__col" key={c.heading}>
              <h4>{c.heading}</h4>
              <ul>
                {c.links.map((l) => (
                  <li key={l.href}>
                    <a href={l.href}>{l.label}</a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </div>
      <div className="footer__bar">
        <div className="container">© {year} r450k. {footer.rights}</div>
      </div>
    </footer>
  );
}
