// Shared helpers for the mymatasan k6 scripts.
//
// mymatasan authenticates with HTTP Basic (the SPA replays the credential on
// every request), so every script just attaches an Authorization header. The
// self-signed dev cert is ignored via `insecureSkipTLSVerify` in each script's
// options.

import { check } from 'k6';
import http from 'k6/http';
import encoding from 'k6/encoding';
import { Trend, Rate } from 'k6/metrics';

export const BASE_URL = (__ENV.BASE_URL || 'https://host.docker.internal:3000').replace(/\/$/, '');
const AUTH_USER = __ENV.AUTH_USER || '';
const AUTH_PASS = __ENV.AUTH_PASS || '';

// Basic auth header, computed once. Empty when no creds were supplied
// (anonymous run — only public endpoints will pass).
export const AUTH_HEADER =
  AUTH_USER !== '' ? `Basic ${encoding.b64encode(`${AUTH_USER}:${AUTH_PASS}`)}` : '';

// Per-endpoint latency + error tracking so the summary breaks down by route,
// not just one global blob.
export const endpointTrend = new Trend('endpoint_duration', true);
export const endpointErrors = new Rate('endpoint_errors');

export function authParams(extra) {
  const headers = {};
  if (AUTH_HEADER) headers['Authorization'] = AUTH_HEADER;
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

export { http, check, encoding };
