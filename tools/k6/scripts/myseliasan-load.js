// myseliasan-load.js — realistic read-heavy load on the authenticated control
// plane.
//
// Models a fleet-ops console open in many browsers: each virtual user logs in
// once, then loops the read endpoints the SPA polls (nodes list, notifications
// feed, session/RBAC) with a short think time. Ramps up, holds, ramps down.
//
//   ./run.ps1 -App myseliasan -Script load
//
// Tune the shape with env vars (all optional):
//   TARGET_VUS   peak concurrent users     (default 50)
//   RAMP         ramp-up/-down duration     (default 30s)
//   HOLD         steady-state duration      (default 1m)

import { sleep } from 'k6';
import { ensureLogin, getJSON } from './lib/session.js';

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
    // Fail the run if the app is slow or erroring. These are the numbers to
    // argue about in review — they encode "acceptable" for a LAN control plane.
    // (Login excluded from the global latency gate: it bcrypts once per VU.)
    'http_req_failed': ['rate<0.01'],          // <1% errors
    'endpoint_duration': ['p(95)<800', 'p(99)<2000'],
    'endpoint_duration{endpoint:nodes}': ['p(95)<1200'],
  },
};

export default function () {
  ensureLogin();

  // Weighted to mirror the SPA: the nodes list + notifications feed poll most.
  getJSON('nodes', '/api/nodes');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('session_me', '/api/session/me');

  // Heavier/rarer calls every few iterations (RBAC surfaces, superadmin-only).
  if (Math.random() < 0.34) getJSON('rbac_users', '/api/rbac/users');
  if (Math.random() < 0.2) getJSON('permissions', '/api/access-rbac/permissions?roleId=1');

  sleep(Math.random() * 1.5 + 0.5); // 0.5–2s think time
}
