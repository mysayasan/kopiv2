// myidsan-load.js — realistic read-heavy load on the identity server.
//
// Models an admin console open in many browsers: each virtual user logs in once, then
// loops the read endpoints the SPA polls, with a short think time.
//
//   ./run.ps1 -App myidsan -Script load
//
// Tune the shape with env vars (all optional):
//   TARGET_VUS   peak concurrent users      (default 50)
//   RAMP         ramp-up/-down duration     (default 30s)
//   HOLD         steady-state duration      (default 1m)
//
// WHAT THIS DOES AND DOES NOT MEASURE. Each VU logs in ONCE and then rides a cookie
// session, so this finds the ceiling of TLS + JSON + the database read path. It does NOT
// measure the thing most likely to actually saturate an identity provider, which is
// bcrypt on a login storm — every one of those is a deliberate KDF cost, and a hundred
// concurrent first-time logins is a completely different shape from a hundred users
// reading. That belongs in the stress script, where it is isolated on purpose.
//
// Do not run this against an instance with the failed-login lockout enabled: repeated
// logins from one address are exactly what it exists to stop, and the run turns into a
// wall of 429/423s that measures the lockout rather than the app.

import { sleep } from 'k6';
import { ensureLogin, getJSON } from './lib/session.js';

const LOGIN_PATH = '/api/login/default';
const TARGET_VUS = parseInt(__ENV.TARGET_VUS || '50', 10);
const RAMP = __ENV.RAMP || '30s';
const HOLD = __ENV.HOLD || '1m';

export const options = {
  insecureSkipTLSVerify: true,
  scenarios: {
    console: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: RAMP, target: TARGET_VUS },
        { duration: HOLD, target: TARGET_VUS },
        { duration: RAMP, target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    // An identity provider is on the critical path of every other app in the suite, so
    // its read latency budget is tighter than a normal console's: if sign-in is slow,
    // everything is slow.
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<800'],
    endpoint_errors: ['rate<0.01'],
  },
};

export default function () {
  ensureLogin(LOGIN_PATH);
  getJSON('identity_status', '/api/identity-status');
  getJSON('session_self', '/api/session');
  getJSON('users', '/api/user-credential?limit=25&offset=0');
  getJSON('apps', '/api/app-registry?limit=25&offset=0');
  getJSON('audit', '/api/audit?limit=25&offset=0');
  sleep(Math.random() * 2 + 0.5);
}
