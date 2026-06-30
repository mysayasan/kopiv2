import Logo from '../components/Logo.jsx';
import { footer } from '../content.js';

export default function Footer() {
  const year = new Date().getFullYear();
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <div className="footer__brand">
          <Logo />
          <p className="footer__tagline">{footer.tagline}</p>
          <p className="footer__note">{footer.note}</p>
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
        <div className="container">© {year} r450k. All rights reserved.</div>
      </div>
    </footer>
  );
}
