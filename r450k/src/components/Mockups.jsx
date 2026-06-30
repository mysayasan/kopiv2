// Inline SVG mockups approximating the mymatasan UI. Used as Screenshot fallbacks
// until real PNG captures are dropped into public/screenshots/. Colors come from the
// site's dark palette so they read as authentic product shots.
const C = {
  bg: '#0d1117',
  panel: '#161c26',
  panel2: '#1a2231',
  video: '#060a0e',
  border: '#243044',
  text: '#e2eaf4',
  muted: '#8a9fb8',
  green: '#44aa80',
  blue: '#4a84e8',
  danger: '#e05252',
};

// 16:10 viewBox shared by all mockups.
const W = 640;
const H = 400;

function Frame({ children }) {
  return (
    <svg viewBox={`0 0 ${W} ${H}`} width="100%" role="img" preserveAspectRatio="xMidYMid slice">
      <rect width={W} height={H} fill={C.bg} />
      {/* sidebar */}
      <rect x="0" y="0" width="56" height={H} fill={C.panel} />
      <rect x="16" y="18" width="24" height="24" rx="6" fill={C.green} opacity="0.9" />
      {[70, 104, 138, 172].map((y, i) => (
        <rect key={y} x="16" y={y} width="24" height="10" rx="3" fill={C.muted} opacity={i === 0 ? 0.9 : 0.4} />
      ))}
      {/* topbar */}
      <rect x="56" y="0" width={W - 56} height="40" fill={C.panel} />
      <rect x="74" y="15" width="120" height="10" rx="3" fill={C.text} opacity="0.8" />
      <circle cx={W - 60} cy="20" r="6" fill={C.green} />
      <circle cx={W - 34} cy="20" r="10" fill={C.panel2} stroke={C.border} />
      {children}
    </svg>
  );
}

export function LiveGridMockup() {
  const cells = [
    [76, 56], [342, 56], [76, 232], [342, 232],
  ];
  return (
    <Frame>
      {cells.map(([x, y], i) => (
        <g key={i}>
          <rect x={x} y={y} width="248" height="160" rx="8" fill={C.video} stroke={C.border} />
          {/* fake camera scene */}
          <rect x={x} y={y + 120} width="248" height="40" rx="0" fill="#0c161e" />
          <rect x={x + 14} y={y + 12} width="60" height="9" rx="3" fill={C.muted} opacity="0.55" />
          {/* detection box (pulses) */}
          <rect
            className="mock-box"
            style={{ animationDelay: `${i * 0.5}s` }}
            x={x + 150}
            y={y + 44}
            width="58"
            height="86"
            rx="3"
            fill="none"
            stroke={i % 2 ? C.blue : C.green}
            strokeWidth="2"
          />
          <rect x={x + 150} y={y + 32} width="62" height="12" rx="2" fill={i % 2 ? C.blue : C.green} />
          <circle className="mock-rec" cx={x + 16} cy={y + 150} r="4" fill={C.danger} />
        </g>
      ))}
    </Frame>
  );
}

export function RuleEditorMockup() {
  return (
    <Frame>
      {/* left: video preview with zone polygon */}
      <rect x="76" y="60" width="320" height="300" rx="8" fill={C.video} stroke={C.border} />
      <polygon
        points="120,120 320,110 360,260 160,300"
        fill="rgba(74,132,232,0.14)"
        stroke={C.blue}
        strokeWidth="2"
      />
      {[[120, 120], [320, 110], [360, 260], [160, 300]].map(([x, y], i) => (
        <circle key={i} cx={x} cy={y} r="5" fill={C.blue} stroke="#fff" strokeWidth="1.5" />
      ))}
      <rect className="mock-box" x="232" y="180" width="40" height="64" rx="3" fill="none" stroke={C.green} strokeWidth="2" />
      <rect x="232" y="168" width="46" height="12" rx="2" fill={C.green} />
      {/* right: rule form */}
      <rect x="412" y="60" width="200" height="300" rx="8" fill={C.panel} stroke={C.border} />
      <rect x="428" y="78" width="90" height="11" rx="3" fill={C.text} opacity="0.85" />
      {[110, 156, 202, 248].map((y) => (
        <g key={y}>
          <rect x="428" y={y} width="60" height="8" rx="3" fill={C.muted} opacity="0.5" />
          <rect x="428" y={y + 14} width="168" height="22" rx="6" fill={C.panel2} stroke={C.border} />
        </g>
      ))}
      <rect x="428" y="306" width="80" height="26" rx="8" fill={C.green} />
    </Frame>
  );
}

export function NotificationsMockup() {
  const rows = [
    { c: C.danger, t: 'Intrusion · Front Gate' },
    { c: C.green, t: 'LPR · WMK 1234 (watchlist)' },
    { c: C.blue, t: 'Crowd · Lobby (4 people)' },
    { c: C.muted, t: 'Camera offline · Backyard' },
    { c: C.green, t: 'Line crossing · Driveway' },
  ];
  return (
    <Frame>
      <rect x="76" y="58" width="536" height="26" rx="6" fill={C.panel} stroke={C.border} />
      <rect x="90" y="66" width="120" height="10" rx="3" fill={C.text} opacity="0.8" />
      {rows.map((r, i) => {
        const y = 98 + i * 56;
        return (
          <g key={i}>
            <rect
              x="76"
              y={y}
              width="536"
              height="46"
              rx="8"
              fill={C.panel}
              stroke={i === 0 ? r.c : C.border}
              className={i === 0 ? 'mock-row' : undefined}
            />
            <rect x="76" y={y} width="4" height="46" rx="2" fill={r.c} />
            <rect x="92" y={y + 9} width="56" height="28" rx="4" fill={C.video} stroke={C.border} />
            <rect x="160" y={y + 12} width="150" height="9" rx="3" fill={C.text} opacity="0.85" />
            <rect x="160" y={y + 26} width="90" height="7" rx="3" fill={C.muted} opacity="0.6" />
            <rect x="520" y={y + 14} width="76" height="18" rx="9" fill="rgba(68,170,128,0.16)" stroke={r.c} opacity="0.9" />
          </g>
        );
      })}
    </Frame>
  );
}
