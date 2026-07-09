// smoke.js — 1 VU, a handful of iterations. Confirms the target is up, the
// credentials work, and every endpoint the load scripts hit returns 2xx before
// you invest in a longer run. Run this first.
//
//   docker compose run --rm k6 run /scripts/smoke.js

import { sleep } from 'k6';
import { getJSON, BASE_URL } from './lib/common.js';

export const options = {
  vus: 1,
  iterations: 5,
  insecureSkipTLSVerify: true,
  thresholds: {
    checks: ['rate==1.0'], // every check must pass or the smoke fails
  },
};

export default function () {
  getJSON('setup_state', '/api/setup/state');
  getJSON('cameras', '/api/cameras?limit=50&offset=0');
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('dashboard_stats', '/api/notifications/stats?bucket=day');
  getJSON('heatmap', '/api/notifications/heatmap');
  getJSON('capacity', '/api/capacity');
  sleep(0.5);
}

export function setup() {
  console.log(`Smoke target: ${BASE_URL}`);
}
