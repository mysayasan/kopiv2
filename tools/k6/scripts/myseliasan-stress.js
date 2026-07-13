// myseliasan-stress.js — push past comfortable load to find the breaking point.
//
// Steps VUs up in stages well beyond expected concurrency and watches where
// latency and error rate turn the corner (the "knee"). Because myseliasan rides
// a cookie session (each VU bcrypts ONCE at login, then reads validate the JWT
// cheaply), the ceiling here is TLS + JSON + the DB read path — NOT bcrypt like
// mymatasan. Use this to answer "how many console clients can one box take?".
//
//   ./run.ps1 -App myseliasan -Script stress
//
//   MAX_VUS   top of the final step (default 300)
//   STEP      seconds held at each level (default 30s)

import { sleep } from 'k6';
import { ensureLogin, getJSON } from './lib/session.js';

const MAX_VUS = parseInt(__ENV.MAX_VUS || '300', 10);
const STEP = __ENV.STEP || '30s';

// Five even steps up to MAX_VUS, then a hard ramp down.
function steps() {
  const out = [];
  for (let i = 1; i <= 5; i++) {
    out.push({ duration: '15s', target: Math.round((MAX_VUS * i) / 5) });
    out.push({ duration: STEP, target: Math.round((MAX_VUS * i) / 5) });
  }
  out.push({ duration: '20s', target: 0 });
  return out;
}

export const options = {
  insecureSkipTLSVerify: true,
  scenarios: {
    stress: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: steps(),
      gracefulRampDown: '10s',
    },
  },
  // No hard thresholds here — the point is to observe failure, not gate on it.
  // Watch the Grafana dashboard live to spot the knee.
};

export default function () {
  ensureLogin();
  getJSON('nodes', '/api/nodes');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  sleep(0.3);
}
