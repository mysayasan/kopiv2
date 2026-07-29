// myidsan-smoke.js — 1 VU, a handful of iterations. Confirms the target is up, the JSON
// login works, and every endpoint the load scripts hit returns 2xx before you invest in a
// longer run. Run this first.
//
//   ./run.ps1 -App myidsan            (smoke is the default)
//
// myidsan's login path is its own: POST /api/login/default. It is neither myseliasan's
// /api/auth/local-login nor myiotsan's /api/auth/login.
//
// Point this at an account that is PAST must-change-password. While that flag is set,
// login succeeds but every other authenticated endpoint returns 401
// password_change_required, and every check below fails for a reason that has nothing to
// do with load.

import { sleep } from 'k6';
import { ensureLogin, getJSON, BASE_URL } from './lib/session.js';

export const LOGIN_PATH = '/api/login/default';

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
  getJSON('identity_status', '/api/identity-status');
  getJSON('session_self', '/api/session');
  getJSON('profile', '/api/profile');
  getJSON('users', '/api/user-credential?limit=25&offset=0');
  getJSON('apps', '/api/app-registry?limit=25&offset=0');
  getJSON('audit', '/api/audit?limit=25&offset=0');
  getJSON('roles', '/api/access-rbac/roles');
  sleep(0.5);
}

export function setup() {
  console.log(`Smoke target: ${BASE_URL}`);
}
