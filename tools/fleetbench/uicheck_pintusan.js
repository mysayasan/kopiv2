// Drive mypintusan's SCREENS in headless Chrome. The first screen check this app has ever had.
//
// WHY. This is the app that physically unlocks doors, and six live benches in it still had no
// screen check at all — the last app in the suite with none. Every one of those benches drove the
// API, and every one of them found the same shape: THE GATE WAS RIGHT AND NOTHING COULD REACH IT.
// A screen is one more thing that can fail to reach a gate that works, and it fails in ways an
// API bench structurally cannot see:
//
//   * a missing translation renders as its own dictionary key. This app ships 298 keys in four
//     languages, one of them right-to-left;
//   * a control can render perfectly and be impossible to press — covered by something, or
//     disabled because the CLIENT computed a permission the server would have allowed;
//   * a screen can OFFER something the app will refuse. An API bench only ever sends the calls it
//     already believes are allowed, so it never asks this;
//   * and the one that matters most here: App.js hides Access rules and Settings on a client-side
//     `user.isAdmin`, while the server decides with the deny-by-default matrix in
//     services/rbac.go. Two mechanisms, one intent, never checked against each other.
//
// STRUCTURE
//   PART A  every nav section renders, has a heading, leaks no dictionary key, and every visible
//           enabled control is hit-testable at its own centre. Every language, every role.
//   PART B  admin, English: real workflows with REAL input events, each confirmed against the
//           server — lockdown, a remote unlock, a badge issued and revoked, a grant changed.
//   PART C  operator and viewer: does what the screen OFFERS match what the server ALLOWS, in
//           both directions?
//
// Usage (bench_pintusan_screens.py drives this; it can also be run alone against a live app):
//
//   node tools/fleetbench/uicheck_pintusan.js <output-dir> [lang] [role]
//
// Prints a JSON summary to assert on and writes a screenshot per section. ASSERT ON THE JSON —
// a screenshot you have to squint at is not an assertion. Then OPEN THE PNGs anyway: on this
// suite a green screen check has twice coexisted with a visibly broken screen.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = process.env.PINTUSAN_BASE || 'https://127.0.0.1:18481';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const ROLE = (process.argv[4] || 'admin').toLowerCase();
const ACCOUNTS_READY = process.env.PINTUSAN_ACCOUNTS_READY === '1';

// Credentials must match bench_pintusan_screens.py. The admin's bootstrap password is rotated on
// first contact and the original stops working, so the UI signs in with the ROTATED one.
const ACCOUNTS = {
  admin: { username: 'admin', password: 'Bench!2345678' },
  operator: { username: 'bench-operator', password: 'Operator!2345' },
  viewer: { username: 'bench-viewer', password: 'Viewer!2345' },
};

// THE SIGN-OUT BUTTON IS A `.nav-item`. App.js renders it with `className="nav-item tone-steel"`
// inside the rail's foot, so a bare `button.nav-item` sweep picks it up, clicks it, and signs the
// session out somewhere in the middle of the section sweep — after which every remaining check
// fails against a login card. The selector therefore excludes the foot, and a check below asserts
// that the exclusion is doing something rather than silently matching nothing.
const NAV_SEL = '.side-nav .nav-item, nav .nav-item, button.nav-item';
const isNavLink = (e) => !e.closest('.side-nav-foot') && !e.closest('.nav-footer');

// This app's own dictionary prefixes, read off views/i18n/en.js. Anchoring the leaked-key detector
// to the real prefixes is what stops the estate's own DATA — a bus port like `tcp://10.0.0.5:4870`,
// a card number, a reader name — being reported as a missing translation.
const KEY_PREFIXES = ['app', 'nav', 'login', 'common', 'doors', 'lockdown', 'people', 'badge',
  'activity', 'readers', 'access', 'group', 'sched', 'grant', 'holiday', 'reason', 'settings',
  'wiz', 'pairing', 'users', 'role'];

const SEED = {
  doorName: 'Screen bench lobby',
  holderName: 'Screen Bench Person',
  holderRef: 'SB-0001',
  cardNumber: '00880040',
  groupName: 'Screen bench group',
  scheduleName: 'Screen bench office hours',
  holidayName: 'Screen bench holiday',
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
  const port = 9251;
  fs.mkdirSync(OUT, { recursive: true });
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-pintusan-' + LANG + '-' + ROLE)}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    // Tall on purpose: the door cards, the settings form and the access-rules screen are all
    // long, and captureBeyondViewport is off (see the note on `shoot`), so anything below the
    // fold is neither screenshot nor hit-tested. A short window under-reports both.
    '--window-size=1600,2400', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const ws = new WebSocket(await connect(port));
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    // window.confirm() and window.prompt() BLOCK the page until something answers. This app uses
    // both — lockdown confirms before engaging, and revoking a badge PROMPTS for the status — so
    // an unanswered dialog wedges the page and fails every check after it for the wrong reason.
    // Answering them here also makes "it asked before doing something drastic" checkable.
    const dialogs = [];
    let promptAnswer = '';
    ws.addEventListener('message', async (ev) => {
      const m = JSON.parse(ev.data);
      if (m.method === 'Page.javascriptDialogOpening') {
        dialogs.push(m.params.message);
        await cdp.send('Page.handleJavaScriptDialog',
          { accept: true, promptText: promptAnswer }).catch(() => {});
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
    // screenshot — nav gone, headings sliced mid-word. It renders a correct layout as an
    // obviously broken one, and "open the PNG" is worthless advice if the PNG lies.
    const shoot = async (name) => {
      const s = await cdp.send('Page.captureScreenshot', { format: 'png' });
      fs.writeFileSync(path.join(OUT, `pintusan-${ROLE}-${LANG}-${name}.png`), Buffer.from(s.data, 'base64'));
    };

    // The SPA's own fetch contract: same-origin cookie session plus a double-submit CSRF token
    // echoed from a deliberately non-HttpOnly cookie. A raw fetch that skips the header gets
    // "csrf token not found", which reads exactly like "the screen is not allowed to do this" —
    // and would make every PART C permission check pass for the wrong reason.
    //
    // THE CAP IS 400_000, AND THAT NUMBER IS LOAD-BEARING. The first draft of this file sliced the
    // body to 6000 characters, which is a sensible size for an error message and a disaster for a
    // list: a hundred access-log rows is ~30KB, so the JSON arrived cut in half, JSON.parse threw,
    // the unwrap returned null, and `items()` handed back an EMPTY ARRAY. Every check built on a
    // before/after diff then compared nothing with nothing and reported that pressing Unlock had
    // not reached the controller and that revoking a grant told nobody — two defects that did not
    // exist, in a bench written to find defects that do. Same family as the envelope trap: a
    // silent unwrap failure does not look like a failure, it looks like an empty estate.
    const api = async (p, init) => js(`(async () => {
      const opts = Object.assign({ credentials: 'same-origin' }, ${JSON.stringify(init || {})});
      const tok = (document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1];
      if (tok && opts.method && opts.method !== 'GET') {
        opts.headers = Object.assign({}, opts.headers, { 'X-CSRF-Token': decodeURIComponent(tok) });
      }
      const r = await fetch(${JSON.stringify(p)}, opts);
      const text = await r.text();
      return { status: r.status, body: text.slice(0, 400000), truncated: text.length > 400000 };
    })()`);

    const clickAt = async (x, y) => {
      // REAL mouse events at the control's own centre, not el.click(): a synthetic click that only
      // the element sees would pass even with something covering it.
      await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1, buttons: 1 });
      await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1, buttons: 0 });
    };
    // centre returns a control's centre ONLY if the document agrees that is what is there.
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
      await sleep(opts && opts.settle ? opts.settle : 2000);
      return true;
    };
    const setPrompt = (v) => { promptAnswer = v; };

    // ---- sign in -------------------------------------------------------------------------
    const acct = ACCOUNTS[ROLE];
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(2500);
    // THE KEY MUST BE THE APP'S OWN (App.js LANG_KEY = 'mypintusan.lang' — note the DOT, not an
    // underscore like myiotsan's). A made-up key silently changes nothing: the page renders in
    // English and the run reports "renders in ar" having never switched, passing for exactly the
    // reason it was written to catch. The assertion below proves the switch rather than trusting
    // the write.
    await js(`localStorage.setItem('mypintusan.lang', ${JSON.stringify(LANG)}), 1`);
    const status = await js(`(async () => {
      const r = await fetch('/api/auth/login', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify(${JSON.stringify(acct)}) });
      return r.status;
    })()`);
    const signedIn = check(`the ${ROLE} can sign in`, status === 200, 'status ' + status);
    if (!signedIn && ROLE !== 'admin' && !ACCOUNTS_READY) {
      // Not a mystery: the seed already reported that the account could not be created. Say so
      // once, here, rather than emitting a dozen derived failures that all trace to it.
      check(`the ${ROLE} role can be exercised at all`, false,
        'no ' + ROLE + ' account exists on this appliance — the seed could not create one, so '
        + 'everything below is UNPROVABLE rather than passing');
      fs.writeFileSync(path.join(OUT, `pintusan-${ROLE}-${LANG}.json`), JSON.stringify(CHECKS, null, 2));
      process.exitCode = 1;
      return;
    }

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);

    const shell = await js(`(() => {
      const links = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})];
      const inFoot = links.filter(e => e.closest('.side-nav-foot') || e.closest('.nav-footer'));
      return {
        lang: document.documentElement.lang || '',
        dir: document.documentElement.dir || getComputedStyle(document.body).direction,
        navs: links.filter(e => !e.closest('.side-nav-foot') && !e.closest('.nav-footer'))
                   .map(e => (e.textContent||'').trim()).filter(Boolean),
        footItems: inFoot.map(e => (e.textContent||'').trim()).filter(Boolean),
        wizard: /wizard|wiz-/.test(document.body.className + ' ' + (document.querySelector('main')||{}).className || ''),
      };
    })()`);

    check('the app shell renders its navigation', shell.navs.length >= 4, JSON.stringify(shell.navs));
    check('the page really is in the requested language',
      (shell.lang || '').toLowerCase().startsWith(LANG), JSON.stringify({ lang: shell.lang, dir: shell.dir }));
    if (LANG === 'ar') {
      check('and Arabic lays the shell out right-to-left', shell.dir === 'rtl', shell.dir);
    }
    // The sign-out exclusion, asserted rather than assumed. If App.js ever stops giving the
    // sign-out button the `nav-item` class this check goes quiet and the sweep is safe anyway;
    // if it keeps it and the exclusion breaks, the sweep signs itself out mid-run and every
    // later check fails against a login card — a failure that reads like a broken app.
    check('the section sweep excludes the rail foot, so it cannot sign itself out mid-run',
      shell.footItems.length > 0 && !shell.navs.some((n) => shell.footItems.includes(n)),
      'foot items held back: ' + JSON.stringify(shell.footItems));

    // Nothing may hang off either edge, in any language. RTL is where this breaks: a rule written
    // as `left`/`margin-left` rather than a logical property pushes the rail out of the viewport,
    // and the hit-test below cannot see it because it SKIPS anything already off-screen.
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
    check('the navigation rail is on screen', layout.navVisible, JSON.stringify(layout.navRect));
    await shoot('00-shell');

    // ---- PART A: every section renders, reads, and can be pressed ------------------------
    const problems = { untranslated: [], headless: [], unhittable: [], missing: [], empty: [] };
    let n = 0;
    for (const label of shell.navs) {
      n += 1;
      const clicked = await js(`(() => {
        const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
          .filter(e => !e.closest('.side-nav-foot') && !e.closest('.nav-footer'))
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
        // A leaked dictionary key looks like "doors.strikeTime" and appears as its OWN text.
        // Anchored to this app's real prefixes so a bus port or a card number is not mistaken
        // for a missing translation.
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
      await require('./uicheck_pintusan_partb.js')({
        js, api, press, centre, clickAt, shoot, sleep, check, cdp, dialogs, setPrompt, SEED, NAV_SEL,
      });
    }

    // ---- PART C: what the screen offers vs what the server allows ------------------------
    if (ROLE === 'operator' || ROLE === 'viewer') {
      await require('./uicheck_pintusan_partc.js')({
        js, api, press, centre, clickAt, shoot, sleep, check, dialogs, ROLE, SEED, NAV_SEL,
        navs: shell.navs,
      });
    }

    const passed = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${passed}/${CHECKS.length} checks passed (${ROLE}, ${LANG})`);
    console.log('screenshots: ' + path.join(OUT, `pintusan-${ROLE}-${LANG}-*.png`));
    fs.writeFileSync(path.join(OUT, `pintusan-${ROLE}-${LANG}.json`), JSON.stringify(CHECKS, null, 2));
    process.exitCode = passed === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
