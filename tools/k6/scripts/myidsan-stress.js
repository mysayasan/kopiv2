// myidsan-stress.js — push past comfortable load to find the breaking point.
//
//   ./run.ps1 -App myidsan -Script stress
//
//   MAX_VUS   top of the final step (default 300)
//   STEP      seconds held at each level (default 30s)
//   MODE      'read' (default) or 'login'
//
// TWO DIFFERENT CEILINGS, AND THEY ARE NOWHERE NEAR EACH OTHER.
//
// MODE=read behaves like the other apps' stress scripts: each VU bcrypts ONCE at login,
// then reads ride a cheap cookie-session check, so the knee is TLS + JSON + the database
// read path. This answers "how many admin consoles can one box hold?".
//
// MODE=login is the one that matters for an identity provider and has no equivalent in
// the other apps. Every iteration is a FRESH login, so every iteration pays a full bcrypt.
// That is a deliberate cost — the KDF is supposed to be slow, that is the entire point —
// which makes login throughput an order of magnitude lower than read throughput and CPU
// bound rather than IO bound. Measure it, because "the IdP fell over on Monday morning"
// is a login storm, not a read storm: everybody signs in within the same ten minutes, and
// sizing the box from the read number will be wrong by a factor you will not enjoy
// discovering in production.
//
// BEFORE RUNNING MODE=login: turn OFF the failed-login lockout on the throwaway target
// (loginSecurity.enabled=false). Repeated logins from one address are precisely what it
// exists to stop; leave it on and you will measure the lockout, not the KDF. Turn the
// rate limiter off too, for the same reason.

import { sleep } from 'k6';
import { ensureLogin, getJSON, loginFresh } from './lib/session.js';

const LOGIN_PATH = '/api/login/default';
const MAX_VUS = parseInt(__ENV.MAX_VUS || '300', 10);
const STEP = __ENV.STEP || '30s';
const MODE = (__ENV.MODE || 'read').toLowerCase();

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
  // No thresholds on purpose: a stress run is EXPECTED to fail eventually. Thresholds
  // here would just abort the run at the moment it starts producing the answer.
};

export default function () {
  if (MODE === 'login') {
    // A fresh credential verification every iteration: the bcrypt path.
    loginFresh(LOGIN_PATH);
    sleep(0.1);
    return;
  }
  ensureLogin(LOGIN_PATH);
  getJSON('identity_status', '/api/identity-status');
  getJSON('users', '/api/user-credential?limit=25&offset=0');
  getJSON('audit', '/api/audit?limit=25&offset=0');
  sleep(0.2);
}

export function setup() {
  console.log(`Stress mode: ${MODE} (login mode measures bcrypt; read mode measures the read path)`);
}
