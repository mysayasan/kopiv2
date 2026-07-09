// load.js — realistic read-heavy load on the authenticated dashboard surface.
//
// Models a room full of operators watching the console: each virtual user loops
// the same read endpoints the SPA polls (notifications feed, dashboard stats,
// heatmap, camera list) with a short think time. Ramps up, holds, ramps down.
//
//   docker compose run --rm k6 run /scripts/load.js
//
// Tune the shape with env vars (all optional):
//   TARGET_VUS   peak concurrent users     (default 50)
//   RAMP         ramp-up/-down duration     (default 30s)
//   HOLD         steady-state duration      (default 1m)

import { sleep } from 'k6';
import { getJSON } from './lib/common.js';

const TARGET_VUS = parseInt(__ENV.TARGET_VUS || '50', 10);
const RAMP = __ENV.RAMP || '30s';
const HOLD = __ENV.HOLD || '1m';

export const options = {
  insecureSkipTLSVerify: true,
  scenarios: {
    dashboard: {
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
    // argue about in review — they encode "acceptable" for a LAN NVR console.
    http_req_failed: ['rate<0.01'],            // <1% errors
    http_req_duration: ['p(95)<800', 'p(99)<2000'],
    'endpoint_duration{endpoint:dashboard_stats}': ['p(95)<1200'],
  },
};

export default function () {
  // Weighted to mirror the SPA: the notifications feed + stats poll most often.
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('dashboard_stats', '/api/notifications/stats?bucket=day');
  getJSON('cameras', '/api/cameras?limit=50&offset=0');

  // Heavier/rarer call every few iterations.
  if (Math.random() < 0.34) getJSON('heatmap', '/api/notifications/heatmap');

  sleep(Math.random() * 1.5 + 0.5); // 0.5–2s think time
}
