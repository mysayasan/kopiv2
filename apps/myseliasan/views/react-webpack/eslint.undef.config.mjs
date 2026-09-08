import globals from 'globals';

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
// The source carries `eslint-disable-next-line` comments naming rules from a fuller lint setup
// (react-hooks/exhaustive-deps, react/no-array-index-key). This config does not define those, and
// an unknown rule in a disable comment is an ERROR - which would make this check fail on files
// that have nothing wrong with them. Declaring them as inert no-ops keeps the exit code honest:
// non-zero means a real undefined identifier, and nothing else.
const inert = { create: () => ({}) };
const stubs = {
  rules: { 'exhaustive-deps': inert, 'no-array-index-key': inert },
};

export default [
  {
    files: ['src/**/*.js'],
    plugins: { 'react-hooks': stubs, react: stubs },
    // These files predate this config and use disable comments freely; flagging the unused ones
    // would be noise from a check that is not about lint hygiene.
    linterOptions: { reportUnusedDisableDirectives: 'off' },
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
      // The maintained lists rather than a hand-written one: enumerating browser globals by hand
      // gets you a check that fails on URLSearchParams and TextDecoder and teaches people to
      // ignore it. serviceworker covers src/pwa/sw.js (self, caches, clients).
      globals: { ...globals.browser, ...globals.serviceworker },
    },
    rules: { 'no-undef': 'error' },
  },
];
