// Drive mymatasan's video wall in headless Chrome against the live bench node (W3-3b).
//
// WHY THIS EXISTS. The API bench proves a wall is stored, shared and governed. None of that
// is the feature. The feature is what the wall DOES while nobody is touching it, and every
// part of that can only fail in a browser:
//
//   * a saved wall must come back IN A DIFFERENT BROWSER. The thing it replaces was a
//     cookie, so a check run in the profile that created it proves nothing — this one uses a
//     fresh Chrome profile on purpose.
//   * CYCLING has to actually advance the page, on a timer, without being touched.
//   * AUTO-POP has to bring a camera that is NOT on screen onto the screen when it alerts,
//     and mark it, and then give the wall back. This raises a REAL alert through the API and
//     watches the wall react.
//   * the SECOND-MONITOR window has to render the wall and nothing else.
//
// Usage (with fleet_harness.py up and bench_w33b_walls.py already run):
//
//   node tools/fleetbench/uicheck_wall.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';   // node-a (mymatasan)
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
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
  });
  return {
    send(method, params = {}) {
      const mid = ++id;
      ws.send(JSON.stringify({ id: mid, method, params }));
      return new Promise((res, rej) => {
        pending.set(mid, (m) => (m.error ? rej(new Error(method + ': ' + JSON.stringify(m.error))) : res(m.result)));
        setTimeout(() => rej(new Error(method + ' timed out')), 60000);
      });
    },
  };
}

const WALL_NAME = 'Screen wall ' + Date.now();

// session opens a Chrome with its OWN profile directory. Two of them run in this check, and
// the second one is the point: a wall that only comes back in the profile that made it is a
// cookie with extra steps.
async function session(tag, port, fn) {
  const profile = path.join(OUT, 'chrome-profile-wall-' + tag);
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1600,1200', 'about:blank',
  ], { stdio: 'ignore' });
  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    const evalJs = async (expr) => {
      const r = await cdp.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      return r.result.value;
    };
    return await fn({ cdp, evalJs });
  } finally {
    chrome.kill();
  }
}

async function signIn(cdp, evalJs, url) {
  await cdp.send('Page.navigate', { url });
  await sleep(3000);
  await evalJs(`(() => { try { localStorage.setItem('mymatasan_lang', ${JSON.stringify(LANG)}); } catch(_){} return 1; })()`);
  await cdp.send('Page.navigate', { url });
  await sleep(4000);
  const signedIn = await evalJs(`(() => {
    const user = document.querySelector('input[name=username], input[autocomplete=username], input[type=text]');
    const pass = document.querySelector('input[type=password]');
    if (!user || !pass) return 'ALREADY IN';
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
  return signedIn;
}

async function openLiveViews(evalJs) {
  return evalJs(`(() => {
    const rail = document.querySelector('.side-nav, nav, aside') || document;
    const hit = [...rail.querySelectorAll('button, a')]
      .find((e) => /live views|paparan langsung|实时|المباشر/i.test(e.textContent || ''));
    if (!hit) return 'NO LIVE VIEWS NAV: ' + [...rail.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,20).join(' | ');
    hit.click();
    return 'clicked';
  })()`);
}

(async () => {
  // ---- session one: build and save a wall ---------------------------------------
  const wallId = await session('a', 9229, async ({ cdp, evalJs }) => {
    const signedIn = await signIn(cdp, evalJs, BASE + '/');
    check('the bench admin can sign in', signedIn === 'submitted' || signedIn === 'ALREADY IN', signedIn);

    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-soft','--accent'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token the wall uses resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted',
      !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + theme.bodyBg);

    const nav = await openLiveViews(evalJs);
    check('Live Views opens', nav === 'clicked', nav);
    await sleep(3000);

    const bar = JSON.parse(await evalJs(`(() => JSON.stringify({
      bar: !!document.querySelector('.wall-bar'),
      picker: !!document.querySelector('.wall-pick select'),
      options: [...document.querySelectorAll('.wall-pick option')].map(o => o.textContent.trim()),
      missing: (document.querySelector('.wall-missing')||{}).textContent || '',
      cycle: (document.querySelector('.wall-num input')||{}).value,
    }))()`));
    check('the wall bar is on the Live Views screen', bar.bar && bar.picker, JSON.stringify(bar).slice(0, 200));

    // The API bench deletes a camera that a wall names. The screen has to SAY so rather than
    // quietly rendering one tile fewer — and the check picks that wall deliberately instead
    // of hoping it is the one the picker happened to open with, which is how this check
    // passed on one run and failed on the next for reasons that had nothing to do with the
    // product.
    const missingWall = await evalJs(`(async () => {
      const r = await fetch('/api/walls', { credentials: 'include' });
      const body = await r.json();
      const rows = (body?.data?.result?.walls) || (body?.result?.walls) || [];
      const hit = rows.find((w) => (w.missingCameras || []).length > 0);
      if (!hit) return 'NO WALL WITH A DELETED CAMERA - run bench_w33b_walls.py first';
      const sel = document.querySelector('.wall-pick select');
      if (!sel) return 'NO PICKER';
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(sel, String(hit.id));
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      await new Promise((r2) => setTimeout(r2, 1500));
      return 'selected:' + hit.name;
    })()`);
    const missingText = await evalJs(`((document.querySelector('.wall-missing')||{}).textContent||'').trim()`);
    check('a wall with a deleted camera says so on screen',
      missingWall.startsWith('selected') && /[1-9]/.test(missingText),
      missingWall + ' -> ' + JSON.stringify(missingText));

    // Put three cameras on the grid and shrink it to 1x1, so the wall has pages to cycle.
    const added = await evalJs(`(async () => {
      const buttons = [...document.querySelectorAll('.add-strip button')];
      let n = 0;
      for (const b of buttons.slice(0, 3)) { b.click(); n++; await new Promise(r => setTimeout(r, 900)); }
      return n;
    })()`);
    await sleep(2500);
    const picked = await evalJs(`(async () => {
      const toggle = document.querySelector('.layout-toggle');
      if (!toggle) return 'NO LAYOUT CONTROL';
      toggle.click();
      await new Promise(r => setTimeout(r, 400));
      const one = [...document.querySelectorAll('.layout-menu-item')].find(b => /1.?1/.test(b.textContent || ''));
      if (!one) return 'NO 1x1 OPTION';
      one.click();
      return 'picked';
    })()`);
    check('the grid can be set to a single tile', picked === 'picked', `${picked} (added ${added})`);
    await sleep(2000);

    const tiles = await evalJs(`document.querySelectorAll('.view-tile').length`);
    const pager = await evalJs(`((document.querySelector('.view-pager-label')||{}).textContent || '').trim()`);
    check('a 1x1 grid over several cameras pages rather than dropping them',
      tiles === 1 && /\/\s*[2-9]/.test(pager), `tiles=${tiles} pager=${pager}`);

    // Cycle every 3 seconds, pop for 15.
    await evalJs(`(() => {
      const nums = [...document.querySelectorAll('.wall-num input')];
      const set = (el, v) => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, String(v));
        el.dispatchEvent(new Event('input', { bubbles: true }));
      };
      if (nums[0]) set(nums[0], 3);
      if (nums[1]) set(nums[1], 15);
      return 1;
    })()`);
    await sleep(600);

    const saved = await evalJs(`(async () => {
      const saveAs = [...document.querySelectorAll('.wall-bar button')]
        .find(b => /save as new|simpan sebagai|另存|احفظ كجدار/i.test(b.textContent || ''));
      if (!saveAs) return 'NO SAVE AS';
      saveAs.click();
      await new Promise(r => setTimeout(r, 400));
      const input = document.querySelector('.wall-naming input');
      if (!input) return 'NO NAME FIELD';
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, ${JSON.stringify(WALL_NAME)});
      input.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise(r => setTimeout(r, 300));
      const ok = [...document.querySelectorAll('.wall-naming button')][0];
      if (!ok || ok.disabled) return 'SAVE DISABLED';
      ok.click();
      return 'saved';
    })()`);
    check('a wall can be saved from the screen', saved === 'saved', saved);
    await sleep(3000);

    const after = JSON.parse(await evalJs(`(() => JSON.stringify({
      options: [...document.querySelectorAll('.wall-pick option')].map(o => o.textContent.trim()),
      selected: (document.querySelector('.wall-pick select')||{}).value,
    }))()`));
    // startsWith, not equality: the picker appends "(default)" to whichever wall opens by
    // default, and comparing the whole label made a passing case look like a failure.
    check('the saved wall is offered in the picker',
      after.options.some((o) => o.startsWith(WALL_NAME)), JSON.stringify(after.options).slice(0, 200));
    check('saving a copy does not steal the default from the wall it was copied from',
      !after.options.some((o) => o.startsWith(WALL_NAME) && /default|lalai|默认|افتراضي/i.test(o)),
      JSON.stringify(after.options).slice(0, 200));

    // --- CYCLING ---------------------------------------------------------------
    // Read the pager, wait longer than one dwell, read it again. Nothing is touched in
    // between: that is the whole claim.
    const first = await evalJs(`((document.querySelector('.view-pager-label')||{}).textContent||'').trim()`);
    await sleep(4500);
    const second = await evalJs(`((document.querySelector('.view-pager-label')||{}).textContent||'').trim()`);
    check('the wall cycles through its pages on its own',
      !!first && !!second && first !== second, `${first} then ${second}`);

    // --- AUTO-POP ---------------------------------------------------------------
    // Raise a REAL alert on a camera that is not the one on screen, through the API the
    // detector uses, and watch the wall come to it.
    const popped = await evalJs(`(async () => {
      const onScreen = Number((document.querySelector('.view-tile')||{}).dataset?.cameraId || 0);
      const r = await fetch('/api/cameras?limit=20', { credentials: 'include' });
      const body = await r.json();
      const cams = (body?.data?.result || []).map(c => Number(c.id));
      const target = cams.find(id => id && id !== onScreen) || cams[0];
      if (!target) return 'NO CAMERA';
      const post = await fetch('/api/vision/alerts', {
        method: 'POST', credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ruleId: 1, cameraId: target, detectionType: 'object',
          label: 'person', confidence: 0.91,
        }),
      });
      if (!post.ok) return 'ALERT REFUSED ' + post.status + ' ' + (await post.text()).slice(0, 120);
      return 'raised:' + target;
    })()`);
    check('a real alert can be raised on a camera', popped.startsWith('raised'), popped);

    let popState = '';
    if (popped.startsWith('raised')) {
      const target = popped.split(':')[1];
      for (let i = 0; i < 30; i++) {
        await sleep(1500);
        popState = await evalJs(`(() => {
          const tile = document.querySelector('.view-tile.popped');
          if (!tile) return '';
          return 'popped:' + (tile.dataset?.cameraId || '?') + ' pager=' +
            ((document.querySelector('.view-pager-label')||{}).textContent||'').trim();
        })()`);
        if (popState) break;
      }
      check('the alerting camera is brought onto the wall and marked',
        popState.startsWith('popped'), popState || `never popped for camera ${target}`);
    }

    const link = await evalJs(`(() => {
      const btn = [...document.querySelectorAll('.wall-bar button')]
        .find(b => /second monitor|monitor kedua|第二显示器|شاشة ثانية/i.test(b.textContent || ''));
      return btn ? String(!btn.disabled) : 'NO BUTTON';
    })()`);
    check('the wall offers a second-monitor window', link === 'true', link);

    // The id of the wall just saved, for the kiosk check.
    return evalJs(`Number((document.querySelector('.wall-pick select')||{}).value || 0)`);
  });

  check('the saved wall has an id to open elsewhere', Number(wallId) > 0, String(wallId));

  // ---- session two: A DIFFERENT BROWSER --------------------------------------------
  //
  // The arrangement this replaces lived in a cookie. A wall that only comes back in the
  // profile that created it has changed nothing, so this profile has never seen the app.
  if (Number(wallId) > 0) {
    await session('b', 9230, async ({ cdp, evalJs }) => {
      await signIn(cdp, evalJs, BASE + '/');
      const nav = await openLiveViews(evalJs);
      check('Live Views opens in a browser that has never seen this app', nav === 'clicked', nav);
      await sleep(3500);
      const seen = JSON.parse(await evalJs(`(() => JSON.stringify({
        options: [...document.querySelectorAll('.wall-pick option')].map(o => o.textContent.trim()),
      }))()`));
      check('the wall saved in the other browser is offered in this one',
        seen.options.some((o) => o.startsWith(WALL_NAME)), JSON.stringify(seen.options).slice(0, 200));

      // ---- the second-monitor window ----------------------------------------------
      //
      // Signed in ON THAT URL: the SPA holds its credentials in memory, so a new window is
      // a new sign-in, and navigating away to log in would drop the ?wall= parameter. This
      // is what a real operator meets when they drag the window to the other screen.
      await signIn(cdp, evalJs, `${BASE}/?wall=${wallId}`);
      await sleep(7000);
      const kiosk = JSON.parse(await evalJs(`(() => JSON.stringify({
        shell: !!document.querySelector('.app-shell.wall-window'),
        nav: !!document.querySelector('.side-nav'),
        addStrip: !!document.querySelector('.add-strip'),
        wallBar: !!document.querySelector('.wall-bar'),
        title: (document.querySelector('.wall-title')||{}).textContent || '',
        tiles: document.querySelectorAll('.view-tile').length,
        href: window.location.href,
      }))()`));
      check('the second-monitor window renders the wall', kiosk.shell && kiosk.tiles >= 1,
        JSON.stringify(kiosk));
      check('and nothing else — no navigation, no picker, no add strip',
        !kiosk.nav && !kiosk.wallBar && !kiosk.addStrip, JSON.stringify(kiosk));
      check('it names the wall it is showing', kiosk.title === WALL_NAME, kiosk.title);

      await cdp.send('Page.captureScreenshot', { format: 'png' }).then((shot) => {
        fs.writeFileSync(path.join(OUT, 'uicheck_wall_' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
      }).catch(() => {});
      return null;
    });
  }

  const ok = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${ok}/${CHECKS.length} checks passed (${LANG})`);
  CHECKS.filter((c) => !c.ok).forEach((c) => console.log('  FAILED: ' + c.name + '   ' + c.detail));
  process.exitCode = ok === CHECKS.length ? 0 : 1;
})().catch((err) => {
  console.error('uicheck_wall failed: ' + err.message);
  process.exitCode = 1;
});
