// myiotsan-load.js — realistic read-heavy load on the authenticated device hub.
//
// Models an operations console open in many browsers: each virtual user logs in
// once, then loops the read endpoints the SPA polls (device list + stats, the
// alert log, the notifications feed) with a short think time. Ramps up, holds,
// ramps down.
//
//   ./run.ps1 -App myiotsan -Script load
//
// Tune the shape with env vars (all optional):
//   TARGET_VUS   peak concurrent users     (default 50)
//   RAMP         ramp-up/-down duration     (default 30s)
//   HOLD         steady-state duration      (default 1m)
//
// SCOPE, and it matters: this loads the HTTP CONSOLE, not the ingest path. The
// console is the easy half. myiotsan's throughput risk is telemetry — hundreds of
// devices publishing to the embedded MQTT broker into SQLite — which k6 does not
// speak and which this script therefore says nothing about. A green run here does
// not mean the box keeps up with the estate. It means the UI stays responsive.
// Read it alongside a real MQTT publish test, not instead of one.

import { sleep } from 'k6';
import { ensureLogin, getJSON } from './lib/session.js';

const LOGIN_PATH = '/api/auth/login';

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
    // Fail the run if the app is slow or erroring. These are the numbers to argue
    // about in review — they encode "acceptable" for a LAN appliance console.
    // (Login excluded from the global latency gate: it bcrypts once per VU.)
    'http_req_failed': ['rate<0.01'],          // <1% errors
    'endpoint_duration': ['p(95)<800', 'p(99)<2000'],
    // The device list and stats query the readings table, which grows without
    // bound as telemetry lands — they are the first things to degrade, so they get
    // their own looser gate rather than being hidden in the global one.
    'endpoint_duration{endpoint:devices}': ['p(95)<1200'],
    'endpoint_duration{endpoint:device_stats}': ['p(95)<1200'],
  },
};

export default function () {
  ensureLogin(LOGIN_PATH);

  // Weighted to mirror the SPA: the device list + stats tiles and the alert log
  // poll most.
  getJSON('devices', '/api/devices?limit=25&offset=0');
  getJSON('device_stats', '/api/devices/stats');
  getJSON('alerts', '/api/alerts?limit=25&offset=0');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');

  // Heavier/rarer calls every few iterations (config surfaces the operator opens
  // occasionally, not on a poll loop).
  if (Math.random() < 0.34) getJSON('rules', '/api/rules');
  if (Math.random() < 0.2) getJSON('profiles', '/api/profiles');

  sleep(Math.random() * 1.5 + 0.5); // 0.5–2s think time
}
