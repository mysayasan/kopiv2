// myseliasan-smoke.js — 1 VU, a handful of iterations. Confirms the target is
// up, the JSON login works, and every endpoint the load scripts hit returns 2xx
// before you invest in a longer run. Run this first.
//
//   ./run.ps1 -App myseliasan            (smoke is the default)

import { sleep } from 'k6';
import { ensureLogin, getJSON, BASE_URL } from './lib/session.js';

export const options = {
  vus: 1,
  iterations: 5,
  insecureSkipTLSVerify: true,
  thresholds: {
    checks: ['rate==1.0'], // every check (login + every GET) must pass
  },
};

export default function () {
  ensureLogin();
  getJSON('session_me', '/api/session/me');
  getJSON('access_me', '/api/access-rbac/me');
  getJSON('nodes', '/api/nodes');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('roles', '/api/access-rbac/roles');
  getJSON('permissions', '/api/access-rbac/permissions?roleId=1');
  getJSON('rbac_users', '/api/rbac/users');
  sleep(0.5);
}

export function setup() {
  console.log(`Smoke target: ${BASE_URL}`);
}
