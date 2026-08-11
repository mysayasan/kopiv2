// Minimal inline SVG icon set (stroke-based, inherits currentColor).
const paths = {
  eye: (
    <>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  car: (
    <>
      <path d="M5 13l1.5-4.5A2 2 0 0 1 8.4 7h7.2a2 2 0 0 1 1.9 1.5L19 13" />
      <path d="M4 13h16v4a1 1 0 0 1-1 1h-1a2 2 0 0 1-4 0H10a2 2 0 0 1-4 0H5a1 1 0 0 1-1-1v-4Z" />
    </>
  ),
  record: (
    <>
      <circle cx="12" cy="12" r="8" />
      <circle cx="12" cy="12" r="3" fill="currentColor" stroke="none" />
    </>
  ),
  play: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M10 9l5 3-5 3V9Z" fill="currentColor" stroke="none" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="11" width="14" height="9" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </>
  ),
  brain: (
    <>
      <path d="M9 4a3 3 0 0 0-3 3 3 3 0 0 0-1 5.8A3 3 0 0 0 8 18a3 3 0 0 0 4 0 3 3 0 0 0 4 0 3 3 0 0 0 3-5.2A3 3 0 0 0 18 7a3 3 0 0 0-3-3 3 3 0 0 0-3 1.5A3 3 0 0 0 9 4Z" />
      <path d="M12 5.5v13" />
    </>
  ),
  bell: (
    <>
      <path d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6Z" />
      <path d="M10 19a2 2 0 0 0 4 0" />
    </>
  ),
  link: (
    <>
      <path d="M9 13a4 4 0 0 0 6 0l2-2a4 4 0 0 0-6-6l-1 1" />
      <path d="M15 11a4 4 0 0 0-6 0l-2 2a4 4 0 0 0 6 6l1-1" />
    </>
  ),
  shield: (
    <>
      <path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3Z" />
      <path d="M9 12l2 2 4-4" />
    </>
  ),
  chart: (
    <>
      <path d="M4 20V4M4 20h16" />
      <rect x="7" y="12" width="3" height="5" rx="0.5" fill="currentColor" stroke="none" />
      <rect x="12" y="8" width="3" height="9" rx="0.5" fill="currentColor" stroke="none" />
      <rect x="17" y="5" width="3" height="12" rx="0.5" fill="currentColor" stroke="none" />
    </>
  ),
  activity: <path d="M3 12h4l2.5 7 4-15 2.5 8h5" />,
  map: (
    <>
      <path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2Z" />
      <path d="M9 4v14M15 6v14" />
    </>
  ),
  face: (
    <>
      <path d="M4 8V6a2 2 0 0 1 2-2h2" />
      <path d="M16 4h2a2 2 0 0 1 2 2v2" />
      <path d="M20 16v2a2 2 0 0 1-2 2h-2" />
      <path d="M8 20H6a2 2 0 0 1-2-2v-2" />
      <circle cx="12" cy="11" r="2" />
      <path d="M8.5 16.2a3.5 3.5 0 0 1 7 0" />
    </>
  ),
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4.3-4.3" />
    </>
  ),
  door: (
    <>
      <path d="M6 21V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v17" />
      <path d="M4 21h16" />
      <circle cx="14.5" cy="12" r="1" fill="currentColor" stroke="none" />
    </>
  ),
  sensor: (
    <>
      <circle cx="12" cy="12" r="2.5" />
      <path d="M7.8 7.8a6 6 0 0 0 0 8.4M16.2 16.2a6 6 0 0 0 0-8.4" />
      <path d="M4.9 4.9a10 10 0 0 0 0 14.2M19.1 19.1a10 10 0 0 0 0-14.2" />
    </>
  ),
  sparkle: (
    <>
      <path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9L12 3Z" />
      <path d="M18.5 15.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7.7-1.8Z" />
    </>
  ),
  network: (
    <>
      <circle cx="12" cy="5" r="2.2" />
      <circle cx="5" cy="18" r="2.2" />
      <circle cx="19" cy="18" r="2.2" />
      <path d="M10.6 6.8 6.4 16M13.4 6.8 17.6 16M7.2 18h9.6" />
    </>
  ),
  check: <path d="M5 12l5 5 9-11" />,
  coffee: (
    <>
      <path d="M4 8h13v5a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5V8Z" />
      <path d="M17 9h2a2.5 2.5 0 0 1 0 5h-2" />
      <path d="M8 3v2M11.5 3v2" />
    </>
  ),
  arrow: <path d="M5 12h14M13 6l6 6-6 6" />,
  download: (
    <>
      <path d="M12 3v12" />
      <path d="M8 11l4 4 4-4" />
      <path d="M5 21h14" />
    </>
  ),
  windows: (
    <>
      <rect x="3.5" y="4.5" width="7" height="7" rx="0.5" />
      <rect x="13.5" y="4.5" width="7" height="7" rx="0.5" />
      <rect x="3.5" y="12.5" width="7" height="7" rx="0.5" />
      <rect x="13.5" y="12.5" width="7" height="7" rx="0.5" />
    </>
  ),
  terminal: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 9l3 3-3 3" />
      <path d="M13 15h4" />
    </>
  ),
  docker: (
    <>
      <rect x="3" y="11" width="18" height="7" rx="1" />
      <path d="M6 11V8M10 11V8M14 11V8" />
      <path d="M3 15c3 2 15 2 18 0" />
    </>
  ),
};

export default function Icon({ name, size = 24, className }) {
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name] || null}
    </svg>
  );
}
