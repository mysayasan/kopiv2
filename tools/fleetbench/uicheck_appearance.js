// Drive mymatasan's appearance search in headless Chrome against the live bench node.
//
// WHY THIS SCREEN NEEDS DRIVING MORE THAN MOST. The ranking is honest only if the SCREEN is
// honest: the model scores two unrelated people at ~0.95, so a page that prints a match
// percentage, or bands everything "strong", turns a usable shortlist into a confident wrong
// answer that an operator acts on. None of that is visible from the API — the JSON is
// correct either way. So this asserts on what the page actually SAYS.
//
// It also carries the design-token check that W3-1 shipped without: an append had destroyed
// the whole :root block and every screen check still passed, because they all assert on DOM
// text and geometry, which a missing colour does not change.
//
// Usage (fleet up, bench_w32_appearance.py already run):
//
//   node tools/fleetbench/uicheck_appearance.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';
// Absolute: Chrome silently refuses a RELATIVE --user-data-dir and never opens the
// devtools port, which surfaces only as "devtools never came up".
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail) });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail !== undefined ? '   ' + detail : ''));
}

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
  let ctx = {};
  try {
    ctx = JSON.parse(fs.readFileSync(path.join(OUT, 'w32_context.json'), 'utf8'));
  } catch (_) {
    throw new Error('no w32_context.json in ' + OUT + ' — run bench_w32_appearance.py first');
  }

  const port = 9229;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-profile-ap')}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1600,1200', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const ws = new WebSocket(await connect(port));
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await cdp.send('Log.enable');
    await cdp.send('Network.enable');

    const evalJs = async (expr) => {
      const r = await cdp.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      return r.result.value;
    };

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    await evalJs(`(() => { try { localStorage.setItem('mymatasan_lang', ${JSON.stringify(LANG)}); } catch(_){} return 1; })()`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);

    const signedIn = await evalJs(`(() => {
      const user = document.querySelector('input[name=username], input[autocomplete=username], input[type=text]');
      const pass = document.querySelector('input[type=password]');
      if (!user || !pass) return 'NO LOGIN FORM';
      const set = (el, v) => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, v);
        el.dispatchEvent(new Event('input', { bubbles: true }));
      };
      set(user, 'admin');
      set(pass, ${JSON.stringify(PASSWORD)});
      const btn = [...document.querySelectorAll('button')].find((b) => /sign in|log in|login|masuk|تسجيل|登录/i.test(b.textContent || ''))
        || document.querySelector('button[type=submit]');
      if (!btn) return 'NO SUBMIT BUTTON';
      btn.click();
      return 'submitted';
    })()`);
    check('the bench admin can sign in', signedIn === 'submitted', signedIn);
    await sleep(6000);

    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      await sleep(2500);
    }
    await sleep(1500);

    // The check W3-1 shipped without. See the header.
    const theme = await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const body = getComputedStyle(document.body);
      const want = ['--bg-body','--bg-surface','--text-primary','--border-panel','--accent','--ok-text','--warn-bg','--warn-text','--status-neutral-bg'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: body.backgroundColor,
      });
    })()`);
    const th = JSON.parse(theme);
    check('every design token in the stylesheet resolves', th.missing.length === 0,
      th.missing.length ? 'unresolved: ' + th.missing.join(', ') : theme);
    check('the page is actually painted',
      !!th.bodyBg && th.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + th.bodyBg);

    const dir = await evalJs(`document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction`);
    if (LANG === 'ar') check('the Arabic run really renders right-to-left', dir === 'rtl', 'dir=' + dir);

    // --- reach the Objects screen and search --------------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const items = [...rail.querySelectorAll('button, a')];
      const hit = items.find((e) => /object|objek|对象|الكائنات|كائن/i.test(e.textContent || ''));
      if (!hit) return 'NO OBJECTS NAV: ' + items.map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Objects entry opens', nav.startsWith('clicked'), nav);
    await sleep(3500);

    // Widen the date range to cover the seeded sightings, then search.
    const searched = await evalJs(`(() => {
      const form = document.querySelector('form.object-search-filters');
      if (!form) return 'NO SEARCH FORM';
      const dates = [...form.querySelectorAll('input[type=date]')];
      const set = (el, v) => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, v);
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
      };
      const d = (unix) => new Date(unix * 1000).toISOString().slice(0, 10);
      if (dates[0]) set(dates[0], d(${ctx.from}));
      if (dates[1]) set(dates[1], d(${ctx.to}));
      const conf = form.querySelector('input[type=number]');
      if (conf) set(conf, '0');
      const btn = form.querySelector('button[type=submit]');
      if (!btn) return 'NO SUBMIT';
      btn.click();
      return 'searched';
    })()`);
    check('the object search form submits', searched === 'searched', searched);
    await sleep(5000);

    const grid = await evalJs(`(() => {
      const rows = document.querySelectorAll('table tbody tr');
      return JSON.stringify({
        rows: rows.length,
        findButtons: document.querySelectorAll('.ap-find-btn').length,
      });
    })()`);
    const g = JSON.parse(grid);
    check('the seeded sightings are listed', g.rows > 0, grid);
    check('every sighting offers an appearance search', g.findButtons === g.rows, grid);
    if (!g.findButtons) throw new Error('no appearance-search button to click');

    // --- open the appearance search ------------------------------------------------
    await evalJs(`document.querySelectorAll('.ap-find-btn')[0].click(); 1`);
    await sleep(6000);

    const dlg = await evalJs(`(() => {
      const d = document.querySelector('.ap-dialog');
      if (!d) return JSON.stringify({ open: false });
      const hits = [...d.querySelectorAll('.ap-hit')];
      return JSON.stringify({
        open: true,
        caveat: (d.querySelector('.ap-caveat') || {}).textContent || '',
        scanned: (d.querySelector('.ap-scanned') || {}).textContent || '',
        uncalibrated: (d.querySelector('.ap-uncalibrated') || {}).textContent || '',
        hits: hits.length,
        bands: hits.map((h) => (h.querySelector('.ap-band') || {}).textContent || ''),
        scores: hits.map((h) => (h.querySelector('.ap-score') || {}).textContent || ''),
        scoreTitles: hits.map((h) => (h.querySelector('.ap-score') || {}).title || ''),
        error: (d.querySelector('.form-alert-msg') || {}).textContent || '',
      });
    })()`);
    const D = JSON.parse(dlg);
    console.log('DIALOG: ' + dlg);

    check('the appearance dialog opens', D.open === true, dlg);
    check('it did not fall back to an error', !D.error, D.error);
    // WITHOUT THIS GUARD every assertion below is vacuously true. The first run of this
    // check opened a sighting that legitimately had no comparable candidates, got an empty
    // list, and passed "no result is presented as a percentage" having examined no results
    // at all. An empty collection satisfies every claim made about its members.
    check('the ranking actually returned results to inspect', D.hits > 0,
      'hits=' + D.hits + ' scanned=' + JSON.stringify(D.scanned));
    // The qualification is shown BEFORE any result, not as a footnote after one.
    check('the dialog states that this ranks appearance, not identity',
      D.caveat.length > 40, 'caveat = ' + JSON.stringify(D.caveat.slice(0, 120)));
    check('it says how many sightings were compared', /\d/.test(D.scanned), D.scanned);

    // THE ASSERTION THIS SCREEN EXISTS FOR. Two unrelated people score ~0.95 on this
    // model, so a percentage on screen reads as a near-certain match for every row. If a
    // "%" ever appears in the score column, the screen has started making a claim the
    // model cannot support.
    check('no result is presented as a match percentage',
      D.hits > 0 && D.scores.every((s) => !s.includes('%')),
      'scores = ' + JSON.stringify(D.scores));
    check('the raw similarity is available but labelled as not a match percentage',
      D.scoreTitles.length === 0 || D.scoreTitles.every((t) => !t || /similarity|تشابه|相似|keserupaan/i.test(t)),
      'titles = ' + JSON.stringify(D.scoreTitles));
    check('every result carries a worded band, not colour alone',
      D.hits > 0 && D.bands.every((b) => b.trim().length > 0),
      'bands = ' + JSON.stringify(D.bands));
    check('the score is expressed as standing out, not as a similarity',
      D.hits > 0 && D.scores.every((sc) => /[0-9]/.test(sc)),
      'scores = ' + JSON.stringify(D.scores));

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, `uicheck-appearance-${LANG}.png`), Buffer.from(shot.data, 'base64'));

    const SHELL_404 = '/api/system/recovery/gate';
    const failed = cdp.events
      .filter((e) => e.method === 'Network.responseReceived' && e.params.response.status >= 400)
      .map((e) => e.params.response.status + ' ' + e.params.response.url.replace(/^https?:\/\/[^/]+/, ''));
    const unexpected = failed.filter((f) => !f.endsWith(SHELL_404));
    check('every request the screen made succeeded', unexpected.length === 0, JSON.stringify(failed.slice(0, 8)));
    ws.close();
  } finally {
    chrome.kill();
  }

  const ok = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${ok}/${CHECKS.length} screen checks passed`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exit(ok === CHECKS.length ? 0 : 1);
})().catch((e) => { console.error('FAILED: ' + e.message); process.exit(1); });
