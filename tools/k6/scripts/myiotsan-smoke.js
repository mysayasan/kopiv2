// myiotsan-smoke.js — 1 VU, a handful of iterations. Confirms the target is up,
// the JSON login works, and every endpoint the load scripts hit returns 2xx
// before you invest in a longer run. Run this first.
//
//   ./run.ps1 -App myiotsan            (smoke is the default)
//
// NOTE: this exercises the HTTP console surface only. It does NOT drive the MQTT
// broker, which is where myiotsan's real throughput risk lives (SQLite write
// amplification under a chatty estate — see the deadband gate + batched writer).
// Load-testing the ingest path needs an MQTT publisher, not k6.

import { sleep } from 'k6';
import { ensureLogin, getJSON, BASE_URL } from './lib/session.js';

// myiotsan's appliance auth stack logs in at /api/auth/login (myseliasan, on the
// federated JWT stack, uses /api/auth/local-login).
const LOGIN_PATH = '/api/auth/login';

export const options = {
  vus: 1,
  iterations: 5,
  insecureSkipTLSVerify: true,
  thresholds: {
    checks: ['rate==1.0'], // every check (login + every GET) must pass
  },
};

export default function () {
  ensureLogin(LOGIN_PATH);
  getJSON('auth_session', '/api/auth/session');
  getJSON('devices', '/api/devices?limit=25&offset=0');
  getJSON('device_stats', '/api/devices/stats');
  getJSON('profiles', '/api/profiles');
  getJSON('rules', '/api/rules');
  getJSON('alerts', '/api/alerts?limit=25&offset=0');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('discovery_window', '/api/discovery/window');
  getJSON('settings_roles', '/api/settings/roles');
  sleep(0.5);
}

export function setup() {
  console.log(`Smoke target: ${BASE_URL}`);
}
