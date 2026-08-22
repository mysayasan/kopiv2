// Drive a real myseliasan screen in headless Chrome against the live bench fleet.
//
// WHY THIS EXISTS. W2-4's API bench passed 36/36 against a real two-node fleet, and the
// screen it shipped still lied: every sighting from a camera that records nothing was
// labelled "Recording…" forever. A green backend and a screen that misleads look identical
// from the API side, so any flagship item that touches a screen owes a pass like this.
//
// Usage (with the fleet from fleet_harness.py already up):
//
//   node tools/fleetbench/uicheck.js <output-dir> [nav-label] [password]
//
// It signs in through the real local-login endpoint, skips the first-run wizard, clicks the
// nav entry whose text matches `nav-label` (default "object"), submits the first search form
// on the page, then prints a JSON summary of the rendered DOM and writes a screenshot.
//
// TRAPS, each of which cost a cycle:
//   * A fresh install lands on the FIRST-RUN WIZARD, not the app shell — click "Skip setup"
//     or every selector below finds nothing and the failure reads like a broken page.
//   * Assert on DOM TEXT, not on the screenshot. The screenshot is for a human afterwards;
//     an assertion you have to squint at is not an assertion.
//   * Chrome needs --ignore-certificate-errors for the bench's self-signed cert, and a
//     throwaway --user-data-dir, or a stale profile carries a previous run's session.
//   * Node's built-in WebSocket + fetch are enough; no puppeteer, no npm install.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
const OUT = process.argv[2] || '.';
// Which nav entry to open, and the bench admin password (the harness rotates to this).
const NAV = (process.argv[3] || 'object').toLowerCase();
const SLUG = NAV.replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'page';
const PASSWORD = process.argv[4] || 'Bench!2345';

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

async function connect(port) {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/json/list`);
      const tabs = await r.json();
      const page = tabs.find((t) => t.type === 'page');
      if (page) return page.webSocketDebuggerUrl;
    } catch (_) { /* not up yet */ }
    await sleep(500);
  }
  throw new Error('devtools never came up');
}

function rpc(ws) {
  let id = 0;
  const pending = new Map();
  const events = [];
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
    else if (msg.method) events.push(msg);
  });
  return {
    events,
    send(method, params = {}) {
      const mid = ++id;
      ws.send(JSON.stringify({ id: mid, method, params }));
      return new Promise((res, rej) => {
        pending.set(mid, (m) => (m.error ? rej(new Error(method + ': ' + JSON.stringify(m.error))) : res(m.result)));
        setTimeout(() => rej(new Error(method + ' timed out')), 30000);
      });
    },
  };
}

(async () => {
  const port = 9223;
  const profile = path.join(OUT, 'chrome-profile');
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1500,1100', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await cdp.send('Log.enable');

    const evalJs = async (expr) => {
      const r = await cdp.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      return r.result.value;
    };

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);

    // Sign in through the real local-login endpoint so the SPA has a session + CSRF cookie.
    const login = await evalJs(`(async () => {
      const r = await fetch('/api/auth/local-login', {
        method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({username:'admin', password:${JSON.stringify(PASSWORD)}}),
      });
      return r.status;
    })()`);
    console.log('login status:', login);

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(5000);

    // A fresh install lands on the first-run wizard; skip it so the app shell renders.
    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|finish|done/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      console.log('wizard: clicked ' + skipped);
      await sleep(2500);
    }
    await sleep(2000);

    // Click through to the target page by its nav label.
    const clicked = await evalJs(`(() => {
      const want = ${JSON.stringify(NAV)};
      const hit = [...document.querySelectorAll('button, a')]
        .find((e) => (e.textContent || '').toLowerCase().includes(want));
      if (!hit) return 'NO NAV ITEM: ' + [...document.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,25).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log(clicked);
    await sleep(2500);

    // Submit whatever search form the page offers, with its defaults.
    const searched = await evalJs(`(async () => {
      const form = document.querySelector('form');
      if (!form) return 'NO FORM ON PAGE';
      const btn = form.querySelector('button[type=submit]');
      if (!btn) return 'FORM HAS NO SUBMIT BUTTON';
      btn.click();
      return 'submitted';
    })()`);
    console.log(searched);
    await sleep(6000);

    const state = await evalJs(`(() => {
      const rows = document.querySelectorAll('table tbody tr');
      const cov = document.querySelector('.search-coverage');
      const tags = [...document.querySelectorAll('.object-tag')].slice(0, 8).map(e => e.textContent.trim());
      return JSON.stringify({
        heading: (document.querySelector('.workspace h2, .workspace h1, h2, h1') || {}).textContent?.trim() || null,
        rowCount: rows.length,
        firstRow: rows[0] ? [...rows[0].querySelectorAll('td')].map(td => td.textContent.trim()).slice(0,6) : null,
        coverageClass: cov ? cov.className : null,
        coverageText: cov ? cov.textContent.trim().slice(0, 220) : null,
        tags,
      });
    })()`);
    console.log('SCREEN STATE: ' + state);

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, `uicheck-${SLUG}.png`), Buffer.from(shot.data, 'base64'));

    const errors = cdp.events.filter((e) => e.method === 'Log.entryAdded' && e.params.entry.level === 'error')
      .map((e) => e.params.entry.text);
    console.log('CONSOLE ERRORS: ' + JSON.stringify(errors.slice(0, 8)));
    ws.close();
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error('FAILED: ' + e.message); process.exit(1); });
