// stress.js — push past comfortable load to find the breaking point.
//
// Steps VUs up in stages well beyond expected concurrency and watches where
// latency and error rate turn the corner (the "knee"). Use this to answer
// "how many operators / polling clients can one box actually take?".
//
//   docker compose run --rm k6 run /scripts/stress.js
//
//   MAX_VUS   top of the final step (default 300)
//   STEP      seconds held at each level (default 30s)

import { sleep } from 'k6';
import { getJSON } from './lib/common.js';

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
  getJSON('notifications', '/api/notifications?limit=25&offset=0');
  getJSON('dashboard_stats', '/api/notifications/stats?bucket=day');
  sleep(0.3);
}
