import { useEffect, useState } from 'react';
import Logo from '../components/Logo.jsx';
import { nav } from '../content.js';
import { useScrollSpy } from '../hooks/useScroll.js';

const ids = nav.map((l) => l.href.replace('#', ''));

export default function Nav() {
  const [scrolled, setScrolled] = useState(false);
  const [open, setOpen] = useState(false);
  const active = useScrollSpy(ids);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  return (
    <header className={`nav ${scrolled ? 'nav--scrolled' : ''}`}>
      <div className="nav__inner">
        <Logo />
        <nav className={`nav__links ${open ? 'is-open' : ''}`}>
          {nav.map((l) => (
            <a
              key={l.href}
              href={l.href}
              className={active === l.href.replace('#', '') ? 'is-active' : ''}
              onClick={() => setOpen(false)}
            >
              {l.label}
            </a>
          ))}
          <a className="btn btn--sm" href="#apps" onClick={() => setOpen(false)}>
            Explore apps
          </a>
        </nav>
        <button
          className="nav__toggle"
          aria-label="Toggle menu"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <span /><span /><span />
        </button>
      </div>
    </header>
  );
}
