// Brand mark: green shield + eye + check, echoing the platform logo, with wordmark.
export default function Logo() {
  return (
    <a className="logo" href="#top" aria-label="r450k home">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <path
          d="M12 2.5l7.5 3.2v5.1c0 4.8-3.2 8.6-7.5 10.7C7.7 19.4 4.5 15.6 4.5 10.8V5.7L12 2.5Z"
          fill="var(--brand-green-soft)"
          stroke="var(--brand-green)"
          strokeWidth="1.4"
        />
        <ellipse cx="12" cy="11" rx="4" ry="2.7" stroke="var(--brand-green)" strokeWidth="1.3" fill="none" />
        <circle cx="12" cy="11" r="1.3" fill="var(--brand-green)" />
        <path d="M9.3 15.3l1.8 1.7 3.6-3.6" stroke="var(--brand-green)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" fill="none" />
      </svg>
      <span className="logo-word">r450k</span>
    </a>
  );
}
