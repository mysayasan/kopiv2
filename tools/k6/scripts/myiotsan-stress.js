// myiotsan-stress.js — push past comfortable load to find the breaking point.
//
// Steps VUs up in stages well beyond expected concurrency and watches where
// latency and error rate turn the corner (the "knee"). Because myiotsan rides a
// cookie session (each VU bcrypts ONCE at login, then reads validate the session
// cookie cheaply), the ceiling here is TLS + JSON + the SQLite read path — NOT
// bcrypt like mymatasan. Use this to answer "how many console clients can one box
// take?".
//
//   ./run.ps1 -App myiotsan -Script stress
//
//   MAX_VUS   top of the final step (default 300)
//   STEP      seconds held at each level (default 30s)
//
// Worth being honest about what this does and doesn't find: it stresses the READ
// path while the box is otherwise idle. In production the same SQLite file is
// being written to continuously by the telemetry batcher, and readers contend
// with those writes. To see the interaction that actually bites, run this WHILE
// devices (or a synthetic MQTT publisher) are publishing — a clean-room stress run
// flatters the appliance.

import { sleep } from 'k6';
import { ensureLogin, getJSON } from './lib/session.js';

const LOGIN_PATH = '/api/auth/login';

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
  ensureLogin(LOGIN_PATH);
  getJSON('devices', '/api/devices?limit=25&offset=0');
  getJSON('device_stats', '/api/devices/stats');
  getJSON('alerts', '/api/alerts?limit=25&offset=0');
  sleep(0.3);
}
