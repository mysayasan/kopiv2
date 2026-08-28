// Drive myiotsan's SCREENS in headless Chrome. The first screen check this app has ever had.
//
// WHY. myiotsan is the app in this suite that WRITES TO THE PHYSICAL WORLD, and until now every
// bench it has had was an API bench — four of them, all green on the wire. A green API run and a
// working screen are different claims, and this suite has now been taught that five separate
// times. The failures a screen has are not the failures an endpoint has:
//
//   * a missing translation renders as its own dictionary key. myiotsan ships eleven sections in
//     four languages, one of them right-to-left;
//   * a control can render perfectly and be impossible to press — covered by something, or
//     `disabled` because the CLIENT computed a permission the server would have allowed;
//   * a screen can OFFER something the app cannot do. That is the one an API bench structurally
//     cannot find, because an API bench only ever sends the values it already knows are good;
//   * and the one that matters most here: **the navigation rail hides Flows and Settings on a
//     client-side `session.isAdmin`, while the server decides with a permission matrix.** Two
//     mechanisms, one intent. Nothing had ever checked they agree — and services/rbac.go draws a
//     line no other app in the suite draws, ACTUATION IS ADMIN-ONLY, "because a bad relay write
//     is physically dangerous in a way a bad PTZ move is not".
//
// STRUCTURE:
//   PART A — every nav section renders, has a heading, leaks no dictionary key, and every visible
//            enabled control is hit-testable at its own centre. Runs in EVERY language and for
//            every role, because that is where translation and layout faults live.
//   PART B — admin, English: drive real workflows with REAL input events and confirm THE SERVER
//            CHANGED. Includes actuation, which is this app's whole reason to exist, and delete,
//            which a green run that never removes anything has not covered.
//   PART C — operator and viewer: does what the screen OFFERS match what the server ALLOWS, in
//            both directions? A menu entry that 403s is a lie; a hidden entry the server would
//            have allowed is a different lie.
//
// Usage (with iotsan_harness.py up and seed_iotsan_screens.py run):
//
//   node tools/fleetbench/uicheck_iotsan.js <output-dir> [lang] [role]
//
// Prints a JSON summary to assert on and writes a screenshot per section. ASSERT ON THE JSON —
// a screenshot you have to squint at is not an assertion. Then OPEN THE PNGs anyway: on this
// suite a green screen check has twice coexisted with a visibly broken screen.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = process.env.IOTSAN_BASE || 'https://127.0.0.1:18483';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const ROLE = (process.argv[4] || 'admin').toLowerCase();

// Credentials must match iotsan_harness.py / seed_iotsan_screens.py.
const ACCOUNTS = {
  admin: { username: 'admin', password: 'Bench!2345678' },
  operator: { username: 'bench-operator', password: 'Operator!2345' },
  viewer: { username: 'bench-viewer', password: 'Viewer!2345' },
};

// THE NAV LINK IS A `button.nav-item`. The nav GROUP HEADER has textContent "Devices" identically
// to the nav LINK — it only looks different because CSS uppercases it — so a bare exact-text match
// picks `div.nav-group-label`, the click goes nowhere, and the run reports a section that "would
// not open" when the app is fine. Cost two runs of #216 to spot.
const NAV_SEL = '.side-nav button.nav-item, .side-nav a.nav-item, nav button.nav-item, button.nav-item';

// myiotsan's own dictionary prefixes, read off i18n/en.js. Anchoring the leaked-key detector to
// the real prefixes is what stops a device's own DATA (a tag, a topic, a version string) being
// reported as a missing translation.
const KEY_PREFIXES = ['alerts', 'auth', 'brand', 'cmd', 'cmdKind', 'cmdStatus', 'common', 'cond',
  'dash', 'dataType', 'day', 'devices', 'disc', 'flows', 'group', 'health', 'ingest', 'kb',
  'modbusMode', 'nav', 'notif', 'page', 'parity', 'payload', 'profiles', 'protocol', 'range',
  'role', 'rules', 'scenes', 'sched', 'severity', 'st', 'theme', 'time', 'transport', 'twin',
  'ui', 'wiz'];

// What the seed created. Restated rather than discovered so a check cannot pass by matching
// something else that happens to be on screen.
const SEED = {
  deviceKey: 'screen-sensor-01',
  deviceName: 'Screen bench sensor',
  muteName: 'Screen bench sensor (no actuation)',
};

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail).slice(0, 400) });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail !== undefined ? '   ' + detail : ''));
  return !!ok;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function connect(port) {
  for (let i = 0; i < 80; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/json/list`);
      const tabs = await r.json();
      const p = tabs.find((x) => x.type === 'page');
      if (p) return p.webSocketDebuggerUrl;
    } catch (_) { /* not up yet */ }
    await sleep(500);
  }
  throw new Error('devtools never came up');
}

function rpc(ws) {
  let id = 0;
  const pending = new Map();
  ws.addEventListener('message', (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  });
  return {
    send(method, params = {}) {
      const mid = ++id;
      ws.send(JSON.stringify({ id: mid, method, params }));
      return new Promise((res, rej) => {
        pending.set(mid, (m) => (m.error ? rej(new Error(method + ': ' + JSON.stringify(m.error))) : res(m.result)));
        setTimeout(() => rej(new Error(method + ' timed out')), 45000);
      });
    },
  };
}

(async () => {
  const port = 9247;
  fs.mkdirSync(OUT, { recursive: true });
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-iotsan-' + LANG + '-' + ROLE)}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    // 1920 WIDE ON PURPOSE. A device's row actions — including the "Open" button that is the only
    // way into the device stage — sit in the last table column, and below about 1600px that column
    // is off-screen. A narrower window reports "no way to open a device" about a working app.
    '--window-size=1920,2200', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const ws = new WebSocket(await connect(port));
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    // window.confirm() BLOCKS the page until something answers it. Accept every dialog and record
    // what it said, so "it asked before doing something destructive" is checkable — and so an
    // unanswered dialog does not wedge the page and fail every check after it for the wrong reason.
    const dialogs = [];
    ws.addEventListener('message', async (ev) => {
      const m = JSON.parse(ev.data);
      if (m.method === 'Page.javascriptDialogOpening') {
        dialogs.push(m.params.message);
        await cdp.send('Page.handleJavaScriptDialog', { accept: true }).catch(() => {});
      }
    });

    const js = async (expression) => {
      const r = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };
    // captureBeyondViewport is DELIBERATELY OFF. In RTL it captures from the scrollable area's
    // origin rather than the visual one, which lops the right-hand side off every Arabic
    // screenshot — nav gone, headings sliced mid-word. It renders a perfectly correct layout as
    // an obviously broken one, and "open the PNG" is worthless advice if the PNG lies. The
    // window is sized tall enough that the viewport holds the whole of these screens.
    const shoot = async (name) => {
      const s = await cdp.send('Page.captureScreenshot', { format: 'png' });
      fs.writeFileSync(path.join(OUT, `iotsan-${ROLE}-${LANG}-${name}.png`), Buffer.from(s.data, 'base64'));
    };

    // The SPA's own API helper contract: same-origin cookie session plus a double-submit CSRF
    // token echoed from a deliberately non-HttpOnly cookie. A raw fetch that skips the header gets
    // "csrf token not found", which reads exactly like "the screen cannot do this".
    const api = async (p, init) => js(`(async () => {
      const opts = Object.assign({ credentials: 'same-origin' }, ${JSON.stringify(init || {})});
      const tok = (document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1];
      if (tok && opts.method && opts.method !== 'GET') {
        opts.headers = Object.assign({}, opts.headers, { 'X-CSRF-Token': decodeURIComponent(tok) });
      }
      const r = await fetch(${JSON.stringify(p)}, opts);
      return { status: r.status, body: (await r.text()).slice(0, 6000) };
    })()`);

    const clickAt = async (x, y) => {
      // REAL mouse events at the control's own centre, not el.click(): a synthetic click that only
      // the element sees would pass even with something covering it.
      await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1, buttons: 1 });
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1, buttons: 0 });
    };
    // centreOf returns a control's centre ONLY if the document agrees that is what is there.
    const centre = (text, opts = {}) => `(() => {
      const sel = ${JSON.stringify(opts.sel || 'button, a, [role=button], td, li')};
      const exact = ${opts.exact === false ? 'false' : 'true'};
      const els = [...(document.querySelector('main') || document.body).querySelectorAll(sel)]
        .filter(e => { const t = (e.textContent||'').trim();
                       return (exact ? t === ${JSON.stringify(text)} : t.includes(${JSON.stringify(text)}))
                              && e.getClientRects().length && !e.disabled; })
        .sort((a,b) => (a.textContent||'').length - (b.textContent||'').length);
      for (const el of els) {
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width/2), y = Math.round(r.top + r.height/2);
        if (x < 0 || y < 0 || x > innerWidth || y > innerHeight) continue;
        const at = document.elementFromPoint(x, y);
        if (at && (el === at || el.contains(at) || at.contains(el))) return { x, y };
      }
      return null;
    })()`;
    const press = async (text, opts) => {
      const p = await js(centre(text, opts));
      if (!p) return false;
      await clickAt(p.x, p.y);
      await sleep(opts && opts.settle ? opts.settle : 2200);
      return true;
    };

    // ---- sign in -------------------------------------------------------------------------
    const acct = ACCOUNTS[ROLE];
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    // THE KEY MUST BE THE APP'S OWN (App.js LANG_KEY = 'myiotsan_lang'). A made-up key silently
    // changes nothing: the page renders in English and the run reports "renders in ar" having
    // never switched — passing for exactly the reason it was written to catch. The assertion
    // below proves the switch rather than trusting the write.
    await js(`localStorage.setItem('myiotsan_lang', ${JSON.stringify(LANG)}), 1`);
    const status = await js(`(async () => {
      const r = await fetch('/api/auth/login', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify(${JSON.stringify(acct)}) });
      return r.status;
    })()`);
    check(`the ${ROLE} can sign in`, status === 200, 'status ' + status);

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4500);

    // The first-run wizard stands in front of every screen while the estate looks empty. It is
    // matched by its translated label in each language, not by a route, because the point is that
    // a human reading THAT language can get past it.
    let wizardSteps = 0;
    for (let i = 0; i < 8; i++) {
      const skipped = await js(`(() => {
        const hit = [...document.querySelectorAll('button, a')]
          .find(e => /skip|finish|done|later|langkau|lewati|selesai|kemudian|\\u8df3\\u8fc7|\\u5b8c\\u6210|\\u7a0d\\u540e|\\u062a\\u062e\\u0637|\\u0625\\u0646\\u0647\\u0627\\u0621|\\u0644\\u0627\\u062d\\u0642/i.test((e.textContent||'').trim()));
        if (!hit) return '';
        hit.click();
        return (hit.textContent || '').trim();
      })()`);
      if (!skipped) break;
      wizardSteps += 1;
      await sleep(2000);
    }
    await sleep(1200);

    const shell = await js(`(() => ({
      lang: document.documentElement.lang || '',
      dir: document.documentElement.dir || getComputedStyle(document.body).direction,
      navs: [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})].map(e => (e.textContent||'').trim()).filter(Boolean),
      groupLabels: [...document.querySelectorAll('.nav-group-label')].map(e => (e.textContent||'').trim()),
    }))()`);
    check('the app shell renders its navigation', shell.navs.length >= 5, JSON.stringify(shell.navs));
    check('the page really is in the requested language',
      (shell.lang || '').toLowerCase().startsWith(LANG), JSON.stringify({ lang: shell.lang, dir: shell.dir }));
    if (LANG === 'ar') {
      check('and Arabic lays the shell out right-to-left', shell.dir === 'rtl', shell.dir);
    }
    // Nothing may hang off either edge, in any language. RTL is where this breaks: a rule written
    // as `left`/`margin-left` rather than a logical property pushes the rail out of the viewport,
    // and the hit-test below cannot see it because it SKIPS anything already off-screen. Measured
    // rather than eyeballed — the Arabic screenshots looked clipped for a while and were not.
    const layout = await js(`(() => {
      const de = document.documentElement;
      const nav = document.querySelector('.side-nav, [class*=side-nav]');
      const r = nav && nav.getBoundingClientRect();
      const over = [...document.querySelectorAll('body *')].filter(el => {
        const b = el.getBoundingClientRect();
        return b.width > 4 && b.height > 4 && (b.right > innerWidth + 2 || b.left < -2);
      }).slice(0, 6).map(el => el.tagName + '.' + String(el.className).slice(0, 32));
      return { innerWidth, scrollWidth: de.scrollWidth,
               navVisible: !!r && r.right > 0 && r.left < innerWidth,
               navRect: r && { left: Math.round(r.left), right: Math.round(r.right) }, over };
    })()`);
    check('the shell fits its viewport — nothing is pushed off either edge',
      layout.scrollWidth <= layout.innerWidth + 2 && layout.over.length === 0,
      JSON.stringify(layout));
    check('the navigation rail is on screen',
      layout.navVisible, JSON.stringify(layout.navRect));
    // The nav-link vs group-label distinction, asserted rather than assumed. Note this app has a
    // group and a link that legitimately share a NAME ("Devices" inside the DEVICES group), which
    // is precisely why the sweep must select on the ELEMENT (button.nav-item) and not on text:
    // an exact-text match picks div.nav-group-label, the click goes nowhere, and the run reports
    // a section that "would not open" about a working app. This asserts the selector's guarantee.
    const headerLeak = await js(`(() => [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
      .filter(e => e.classList.contains('nav-group-label')).map(e => (e.textContent||'').trim()))()`);
    check('the nav sweep selects LINKS, never the section headers that share their names',
      shell.groupLabels.length > 0 && headerLeak.length === 0,
      'headers ' + JSON.stringify(shell.groupLabels) + '; headers caught by the link selector: '
        + JSON.stringify(headerLeak));
    await shoot('00-shell');

    // ---- PART A: every section renders, reads, and can be pressed ------------------------
    const problems = { untranslated: [], headless: [], unhittable: [], missing: [], empty: [] };
    let n = 0;
    for (const label of shell.navs) {
      n += 1;
      const clicked = await js(`(() => {
        const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
          .find(e => (e.textContent || '').trim() === ${JSON.stringify(label)});
        if (!hit) return 0;
        hit.click();
        return 1;
      })()`);
      if (!clicked) { problems.missing.push(label); continue; }
      await sleep(2400);

      const report = await js(`(() => {
        const main = document.querySelector('main') || document.body;
        const text = main.innerText || '';
        const prefixes = ${JSON.stringify(KEY_PREFIXES)};
        // A leaked dictionary key looks like "devices.colTag" and appears as its OWN text.
        // Anchored to this app's real prefixes so a device tag or a version string is not
        // mistaken for a missing translation.
        const leaked = [...new Set((text.match(/\\b[a-zA-Z][a-zA-Z0-9]*\\.[a-zA-Z][a-zA-Z0-9.]+\\b/g) || [])
          .filter(s => prefixes.includes(s.split('.')[0])))];
        const heading = ((main.querySelector('h1, h2') || {}).textContent || '').trim();

        const unhittable = [];
        for (const el of main.querySelectorAll('button, select, input, a[href]')) {
          const r = el.getBoundingClientRect();
          if (r.width < 2 || r.height < 2) continue;
          const st = getComputedStyle(el);
          if (st.visibility === 'hidden' || st.display === 'none' || el.disabled) continue;
          const cx = Math.round(r.left + r.width / 2), cy = Math.round(r.top + r.height / 2);
          if (cx < 0 || cy < 0 || cx > innerWidth || cy > innerHeight) continue;
          const at = document.elementFromPoint(cx, cy);
          if (!at || !(el === at || el.contains(at) || at.contains(el))) {
            unhittable.push(((el.textContent || el.getAttribute('aria-label') || el.tagName).trim().slice(0, 30))
              + ' <- ' + (at ? at.tagName + '.' + (at.className || '') : 'null'));
          }
        }
        return { leaked, heading, unhittable, chars: text.trim().length,
                 controls: main.querySelectorAll('button, select, input, a[href]').length };
      })()`);

      if (report.leaked.length) problems.untranslated.push(label + ': ' + report.leaked.join(', '));
      if (!report.heading) problems.headless.push(label);
      if (report.unhittable.length) problems.unhittable.push(label + ': ' + report.unhittable.join(' | '));
      // A screen that renders almost nothing is a screen that failed to load. The seed guarantees
      // every section has content, so this is a real signal rather than an empty-estate artifact.
      if (report.chars < 40) problems.empty.push(label + ' (' + report.chars + ' chars)');
      await shoot(String(n).padStart(2, '0') + '-' + label.replace(/[^a-zA-Z0-9]/g, '_').slice(0, 24));
    }

    check('every navigation entry opens its section', !problems.missing.length, problems.missing.join(', '));
    check('no screen leaks an untranslated dictionary key', !problems.untranslated.length,
      problems.untranslated.join(' || '));
    check('every screen renders a heading', !problems.headless.length, problems.headless.join(', '));
    check('every screen renders content', !problems.empty.length, problems.empty.join(', '));
    check('every enabled control is hit-testable at its own centre', !problems.unhittable.length,
      problems.unhittable.join(' || '));

    // ---- PART B: admin workflows, confirmed against the server ---------------------------
    if (ROLE === 'admin' && LANG === 'en') {
      await require('./uicheck_iotsan_partb.js')({ js, api, press, centre, clickAt, shoot, sleep, check, cdp, dialogs, SEED, NAV_SEL });
    }

    // ---- PART C: what the screen offers vs what the server allows ------------------------
    if (ROLE === 'operator' || ROLE === 'viewer') {
      await require('./uicheck_iotsan_partc.js')({ js, api, press, centre, clickAt, shoot, sleep, check, dialogs, ROLE, SEED, NAV_SEL, navs: shell.navs });
    }

    const passed = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${passed}/${CHECKS.length} checks passed (${ROLE}, ${LANG})`);
    console.log('screenshots: ' + path.join(OUT, `iotsan-${ROLE}-${LANG}-*.png`));
    fs.writeFileSync(path.join(OUT, `iotsan-${ROLE}-${LANG}.json`), JSON.stringify(CHECKS, null, 2));
    process.exitCode = passed === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
