import Nav from './sections/Nav.jsx';
import Hero from './sections/Hero.jsx';
import Features from './sections/Features.jsx';
import HowItWorks from './sections/HowItWorks.jsx';
import Tiers from './sections/Tiers.jsx';
import Showcase from './sections/Showcase.jsx';
import UseCases from './sections/UseCases.jsx';
import Apps from './sections/Apps.jsx';
import FinalCta from './sections/FinalCta.jsx';
import Footer from './sections/Footer.jsx';
import ScrollProgress from './components/ScrollProgress.jsx';
import BackToTop from './components/BackToTop.jsx';
import Backdrop from './components/Backdrop.jsx';
import ContactWidget from './components/ContactWidget.jsx';

export default function App() {
  return (
    <>
      <a className="skip-link" href="#features">Skip to content</a>
      <Backdrop />
      <ScrollProgress />
      <span id="top" />
      <Nav />
      <main>
        <Hero />
        <Features />
        <HowItWorks />
        <Tiers />
        <Showcase />
        <UseCases />
        <Apps />
        <FinalCta />
      </main>
      <Footer />
      <BackToTop />
      <ContactWidget />
    </>
  );
}
