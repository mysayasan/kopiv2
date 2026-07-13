import './styles/brand.css';

// BrandLogo is the house mark shared by every app in the suite: a line-art shield
// holding a watchful eye and a checkmark, with the rounded lowercase wordmark
// underneath. Self-contained inline SVG so it scales without a binary asset.
//
// The geometry is identical across apps — only the hue changes. The mark draws in
// `currentColor`, which brand.css takes from the `--brand-mark` token, so each app
// tints it by setting that one variable (and `--brand-tint` for the catchlight).
// Keep this in sync with each app's assets/favicon.svg, which is the filled variant
// of the same paths.
export function BrandLogo({ wordmark, size = 40, className = '' }) {
  return (
    <div className={`brand-logo${className ? ` ${className}` : ''}`} aria-label={wordmark}>
      <svg className="brand-mark" viewBox="0 0 64 64" width={size} height={size} role="img" aria-hidden="true">
        <g fill="none" stroke="currentColor" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round">
          {/* shield */}
          <path d="M19 13.5 H45 Q50 13.5 50 18 V33 Q50 45 32 54 Q14 45 14 33 V18 Q14 13.5 19 13.5 Z" />
          {/* eye almond */}
          <path d="M20 30 Q31 21 42 30 Q31 39 20 30 Z" />
          {/* iris */}
          <circle cx="31" cy="30" r="6" />
          {/* checkmark */}
          <path d="M24 41 L31 48 L46 30" strokeWidth="3.4" />
        </g>
        {/* pupil + highlight */}
        <circle cx="31" cy="30" r="3" fill="currentColor" />
        <circle cx="29.5" cy="28.6" r="1" className="brand-mark-glint" />
      </svg>
      <span className="brand-wordmark">{wordmark}</span>
    </div>
  );
}
