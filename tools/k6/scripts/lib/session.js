// Shared helpers for the COOKIE-SESSION k6 scripts: myseliasan and myiotsan.
//
// Unlike mymatasan (HTTP Basic replayed on every request — see common.js), both
// of these authenticate ONCE via a JSON login and then ride a cookie session:
// the login sets a session cookie, and every subsequent request is authorized by
// validating that cookie (cheap) rather than re-running bcrypt. So bcrypt costs
// one hash per VU, not one per request, and the ceiling these scripts find is
// TLS + JSON + the DB read path — not the KDF. k6 keeps a per-VU cookie jar, so
// once a VU has logged in its http.get calls are authenticated for the rest of
// the run. The self-signed dev cert is ignored via `insecureSkipTLSVerify` in
// each script's options.
//
// The two apps differ only in their login path, so ensureLogin() takes it as an
// argument and defaults to myseliasan's:
//   myseliasan  POST /api/auth/local-login   (federated JWT stack)
//   myiotsan    POST /api/auth/login         (appliance local-auth stack)
// Both take {username,password} and both set an HttpOnly session cookie.
//
// CSRF: myseliasan only requires the X-CSRF-Token header on state-changing
// verbs (POST/PUT/PATCH/DELETE); myiotsan's appliance cookie is SameSite=Lax and
// carries no CSRF token at all. These load scripts are read-only (GET), so
// neither needs CSRF handling. Login itself is CSRF-exempt.
//
// Cookie handling: k6 RESETS the per-VU cookie jar between iterations, so the
// session cookie set by login (iteration 1) would be gone by iteration 2 and
// every later read would 401. Module variables DO persist across iterations, so
// we capture the login cookies once into `cookieHeader` and attach them as an
// explicit Cookie header on every request — login stays once-per-VU (one bcrypt)
// while the session rides every iteration.
//
// NOTE: authenticated reads require the account to be PAST must-change. While a
// user is flagged must-change-password, login succeeds and /api/session/me works
// but every other authed endpoint returns 401 password_change_required — so
// point this at an account whose password has already been changed.

import { check, fail } from 'k6';
import http from 'k6/http';
import { Trend, Rate } from 'k6/metrics';

export const BASE_URL = (__ENV.BASE_URL || 'https://host.docker.internal:3002').replace(/\/$/, '');
const AUTH_USER = __ENV.AUTH_USER || '';
const AUTH_PASS = __ENV.AUTH_PASS || '';

// Per-endpoint latency + error tracking so the summary breaks down by route,
// not just one global blob.
export const endpointTrend = new Trend('endpoint_duration', true);
export const endpointErrors = new Rate('endpoint_errors');
export const loginTrend = new Trend('login_duration', true);

// Captured session cookies as a ready-to-send Cookie header. Module scope, so it
// survives k6's per-iteration cookie-jar reset. Empty until the VU logs in (and
// stays empty on an anonymous run — only public endpoints will 2xx).
let cookieHeader = '';

// Default login path (myseliasan). myiotsan's scripts pass '/api/auth/login'.
const DEFAULT_LOGIN_PATH = '/api/auth/local-login';

// Log in this VU exactly once and capture its session cookies. Anonymous run
// (no creds) is a no-op.
export function ensureLogin(loginPath) {
  if (cookieHeader !== '' || AUTH_USER === '') return;
  const res = http.post(
    `${BASE_URL}${loginPath || DEFAULT_LOGIN_PATH}`,
    JSON.stringify({ username: AUTH_USER, password: AUTH_PASS }),
    { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'login' } }
  );
  loginTrend.add(res.timings.duration);
  const ok = check(res, {
    'login status 200': (r) => r.status === 200,
  });
  if (!ok) {
    // A VU that can't authenticate would just spew 401s and pollute the run;
    // fail loudly instead so the cause (bad creds, must-change, target down) is
    // obvious in the summary.
    fail(`login failed (status ${res.status}): ${String(res.body).slice(0, 200)}`);
  }
  // Build a Cookie header from the login response's Set-Cookie(s). k6 exposes
  // them (incl. HttpOnly) on res.cookies as name -> [{ value, ... }].
  const parts = [];
  for (const name in res.cookies) {
    const arr = res.cookies[name];
    if (arr && arr.length && arr[0].value) parts.push(`${name}=${arr[0].value}`);
  }
  cookieHeader = parts.join('; ');
  if (cookieHeader === '') fail('login succeeded but set no cookies');
}

// loginFresh performs an UNCONDITIONAL login and deliberately does not cache the result.
//
// ensureLogin() above is once-per-VU on purpose, so a read test pays bcrypt once and then
// measures the read path. This is the opposite tool, and it exists for one case: measuring
// an identity provider's LOGIN throughput, where every request must pay a full bcrypt
// because that is what a Monday-morning sign-in storm actually does. Using ensureLogin for
// that would measure nothing but the cookie check.
//
// Returns the response so the caller can decide what a failure means. It does NOT fail()
// on a bad status, because in a stress run at the knee, failures ARE the measurement.
export function loginFresh(loginPath) {
  if (AUTH_USER === '') return null;
  const res = http.post(
    `${BASE_URL}${loginPath || DEFAULT_LOGIN_PATH}`,
    JSON.stringify({ username: AUTH_USER, password: AUTH_PASS }),
    { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'login_fresh' } }
  );
  loginTrend.add(res.timings.duration);
  endpointTrend.add(res.timings.duration, { endpoint: 'login_fresh' });
  endpointErrors.add(res.status !== 200, { endpoint: 'login_fresh' });
  return res;
}

export function authParams(extra) {
  const headers = {};
  if (cookieHeader) headers['Cookie'] = cookieHeader;
  return Object.assign({ headers }, extra || {});
}

// GET a path, tag it by name, record per-endpoint latency + error rate, and
// assert a 2xx. Returns the response so callers can inspect the body.
export function getJSON(name, path, expectStatus) {
  const res = http.get(`${BASE_URL}${path}`, authParams({ tags: { endpoint: name } }));
  const want = expectStatus || 200;
  const ok = check(res, {
    [`${name} status ${want}`]: (r) => r.status === want,
  });
  endpointTrend.add(res.timings.duration, { endpoint: name });
  endpointErrors.add(!ok, { endpoint: name });
  return res;
}

export { http, check };
