// A deliberately tiny ESLint config with ONE job: catch identifiers that are used but never
// imported or declared.
//
// Why this exists. Webpack compiles an undefined identifier without a word of complaint - it is a
// runtime ReferenceError, not a build error - so `make web` stays green and the break only shows
// up when the exact line runs. Splitting fleet_map.js into components/map/* produced four of them
// in two commits (KIND_POINT, SEV_RANK, markerShape, and three in popups.js), and each one hid
// behind a condition a passing bench never reached: a point-asset marker, a site with alerts, a
// critical node's beacon ring, an opened device card.
//
// Run it before trusting a green build, especially after moving code between modules:
//
//     npm run lint:undef        (from apps/myseliasan/views/react-webpack)
//
// It is intentionally NOT a full lint setup - no style rules, no React rules, no plugins to keep
// in step. Adding those is a separate decision; this is the cheap guard for the one failure mode
// that has actually bitten.
export default [
  {
    files: ['src/**/*.js'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
      globals: {
        window: 'readonly',
        document: 'readonly',
        console: 'readonly',
        navigator: 'readonly',
        location: 'readonly',
        fetch: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
        requestAnimationFrame: 'readonly',
        cancelAnimationFrame: 'readonly',
        localStorage: 'readonly',
        sessionStorage: 'readonly',
        ResizeObserver: 'readonly',
        WebSocket: 'readonly',
        Image: 'readonly',
        URL: 'readonly',
        Event: 'readonly',
        CustomEvent: 'readonly',
        HTMLInputElement: 'readonly',
        Blob: 'readonly',
        FormData: 'readonly',
        RTCPeerConnection: 'readonly',
        MediaStream: 'readonly',
        AbortController: 'readonly',
        performance: 'readonly',
        alert: 'readonly',
        confirm: 'readonly',
        prompt: 'readonly',
        EventSource: 'readonly',
        // Service-worker globals (src/pwa/sw.js).
        self: 'readonly',
        caches: 'readonly',
        clients: 'readonly',
      },
    },
    rules: { 'no-undef': 'error' },
  },
];
