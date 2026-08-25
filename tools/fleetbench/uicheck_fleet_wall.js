// Drive myseliasan's Fleet video wall in headless Chrome (W3-3d), one language per run.
//
// W3-3b built the wall for ONE recorder. This is the cross-appliance half — the differentiator
// no appliance vendor can match, because a wall that spans machines needs something that can
// see all of them.
//
// What it proves, and why each one:
//
//   * A WALL CAN BE BUILT THROUGH THE FORM, picking cameras from more than one appliance, and
//     it comes back with the tiles it was given. The API bench proves the arrangement; this
//     proves an operator can produce one.
//   * AN OFFLINE TILE SAYS SO IN WORDS. This is the assertion that matters most on this
//     screen: rendering an appliance that is down as a black rectangle makes it
//     indistinguishable from a dark room, and a wall is watched by somebody who will not
//     investigate every dark square.
//   * EVERY ACTION IS PRESSABLE AT ITS OWN CENTRE in both directions — the RTL defect W3-5a,
//     W3-5b and W3-6b each shipped.
//   * THE W3-1 REGRESSION GUARD: design tokens read back out of the live page.
//
// NOT PROVED HERE: that the tiles PLAY. The relay behind them is W3-2's, and a fleet with a
// test pattern and no ffmpeg on the spare cannot show video in a headless browser.
//
//   node tools/fleetbench/uicheck_fleet_wall.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

// Unique per run, so a re-run cannot pass by finding the wall the LAST run built.
const NAME = 'Screen wall ' + Date.now();

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail) });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail !== undefined ? '   ' + detail : ''));
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

async function connect(port) {
  for (let i = 0; i < 60; i++) {
    try {
      const tabs = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
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
        setTimeout(() => rej(new Error(method + ' timed out')), 180000);
      });
    },
  };
}

(async () => {
  const port = 9248;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-profile-fleetwall-' + LANG)}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1600,1400', 'about:blank',
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

    // A REAL click at the control's own centre, then a read of what document.elementFromPoint
    // returns there. `.click()` succeeds on a control buried under another element.
    const clickSel = async (selector, containerSel) => {
      const box = JSON.parse(await evalJs(`(() => {
        const el = document.querySelector(${JSON.stringify(selector)});
        if (!el) return JSON.stringify({ missing: true });
        el.scrollIntoView({ block: 'center' });
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width / 2), y = Math.round(r.top + r.height / 2);
        const hit = document.elementFromPoint(x, y);
        const box = el.closest(${JSON.stringify(containerSel || 'body')});
        const br = box ? box.getBoundingClientRect() : null;
        return JSON.stringify({
          x, y, w: Math.round(r.width), h: Math.round(r.height), disabled: !!el.disabled,
          reaches: !!(hit && (hit === el || el.contains(hit))),
          hitTag: hit ? hit.tagName + (typeof hit.className === 'string' && hit.className ? '.' + hit.className.split(' ')[0] : '') : null,
          inside: br ? (r.left >= br.left - 1 && r.right <= br.right + 1 && r.top >= br.top - 1 && r.bottom <= br.bottom + 1) : null,
        });
      })()`));
      if (box.missing || box.disabled || !box.reaches) return box;
      for (const type of ['mousePressed', 'mouseReleased']) {
        await cdp.send('Input.dispatchMouseEvent', {
          type, x: box.x, y: box.y, button: 'left', clickCount: 1,
          buttons: type === 'mousePressed' ? 1 : 0,
        });
      }
      return box;
    };

    const setInput = async (selector, value) => evalJs(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)});
      if (!el) return 'MISSING';
      const proto = el.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${JSON.stringify(value)});
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return el.value;
    })()`);

    // ---- sign in, in the language under test -------------------------------------------
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);
    const login = await evalJs(`(async () => (await fetch('/api/auth/local-login', {
      method: 'POST', credentials: 'same-origin', headers: {'Content-Type':'application/json'},
      body: JSON.stringify({username:'admin', password:${JSON.stringify(PASSWORD)}}) })).status)()`);
    check('the bench admin can sign in', login === 200, 'status ' + login);

    // A clean slate, and it is also what proves the empty state renders.
    await evalJs(`(async () => {
      const csrf = decodeURIComponent((document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1] || '');
      const b = await (await fetch('/api/fleet-walls', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      for (const w of items) {
        await fetch('/api/fleet-walls/' + w.id, { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrf } });
      }
      return items.length;
    })()`);

    // SEED A CAMERA ON EVERY APPLIANCE THAT HAS NONE. Not decoration: the one thing this
    // screen exists to prove is a wall built from more than one machine, and a run against a
    // fleet where only one appliance happens to hold cameras proves the opposite by accident.
    // Seeded through the control plane's own node proxy, at an address that refuses instantly
    // (the appliance probes a camera before saving it) and with a distinct host per camera
    // (saving a discovered camera upserts BY HOST).
    const seeded = JSON.parse(await evalJs(`(async () => {
      const csrf = decodeURIComponent((document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1] || '');
      const nodesResp = await (await fetch('/api/nodes', { credentials: 'same-origin' })).json();
      const nodes = (nodesResp?.result) || (nodesResp?.data?.result) || [];
      const out = {};
      let host = 200;
      for (const node of nodes) {
        const id = node.nodeId;
        const camsResp = await fetch('/api/nodes/' + encodeURIComponent(id) + '/proxy/api/cameras?limit=5', { credentials: 'same-origin' });
        const cams = await camsResp.json().catch(() => null);
        const list = (cams?.data?.result) || (cams?.result) || [];
        out[id] = Array.isArray(list) ? list.length : 0;
        for (let i = out[id]; i < 2; i++) {
          const h = '127.0.0.' + (host++);
          await fetch('/api/nodes/' + encodeURIComponent(id) + '/proxy/api/cameras/discovered', {
            method: 'POST', credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
            body: JSON.stringify({ name: 'wall' + host, host: h, port: 8554,
              rtspUrl: 'rtsp://' + h + ':8554/wall', username: '', password: '', description: 'wall screen check' }),
          });
          out[id] = (out[id] || 0) + 1;
        }
      }
      return JSON.stringify(out);
    })()`));
    check('every appliance in the fleet has at least one camera to put on a wall',
      Object.values(seeded).length >= 2 && Object.values(seeded).every((n) => n >= 1),
      JSON.stringify(seeded));

    await evalJs(`localStorage.setItem('myseliasan_lang', ${JSON.stringify(LANG)}), 1`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(5000);
    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|finish|done|langkau|跳过|完成|تخطّي|تخطي|إنهاء/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      await sleep(2500);
    }
    await sleep(1500);

    // ---- the W3-1 regression guard -----------------------------------------------------
    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-subtle'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token the page relies on resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted', !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)',
      'body background = ' + theme.bodyBg);

    // ---- reach the screen the way a person would ---------------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /video wall|dinding video|电视墙|جدار الفيديو/i.test(e.textContent || ''));
      if (!hit) return 'NO NAV: ' + [...rail.querySelectorAll('button,a')].map((e) => e.textContent.trim()).filter(Boolean).slice(0, 30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Video wall entry is in the nav and reaches the screen', /^clicked:/.test(nav), nav);
    await sleep(2500);

    const empty = await evalJs(`(() => {
      const el = document.querySelector('[data-fw=empty]');
      return el ? el.textContent.trim() : '';
    })()`);
    check('with no wall built, the screen says so rather than showing an empty grid',
      !!empty && empty.length > 20, empty.slice(0, 100));
    const limit = await evalJs(`(() => {
      const el = document.querySelector('.fw-limit');
      return el ? el.textContent.trim() : '';
    })()`);
    check('and it states what a fleet wall costs — every tile is relayed from another machine',
      !!limit && limit.length > 40, limit.slice(0, 100));

    // ---- build one THROUGH THE FORM ------------------------------------------------------
    const opened = await clickSel('[data-fw-act=new]');
    check('an administrator can open the wall editor', opened.reaches === true, JSON.stringify(opened));
    await sleep(1500);
    check('and the editor appears',
      (await evalJs(`(() => !!document.querySelector('[data-fw=editor]'))()`)) === true);

    check('the name can be typed', (await setInput('[data-fw-input=name]', NAME)) === NAME);
    await setInput('[data-fw-input=grid]', '2x2');
    await setInput('[data-fw-input=cycle]', '5');
    await setInput('[data-fw-input=pop]', '15');

    // Cameras from BOTH appliances. That is the feature; one appliance is a Live Views page.
    const nodeIds = JSON.parse(await evalJs(`(() => JSON.stringify(
      [...document.querySelectorAll('[data-fw-node]')].map((e) => e.getAttribute('data-fw-node'))))()`));
    check('every appliance in the fleet is offered as a source of cameras', nodeIds.length >= 2,
      JSON.stringify(nodeIds));

    const picked = [];
    for (const id of nodeIds.slice(0, 2)) {
      await clickSel(`[data-fw-node="${id}"]`);
      await sleep(2500);
      const cams = JSON.parse(await evalJs(`(() => JSON.stringify(
        [...document.querySelectorAll('[data-fw-cam^="${id}::"]')].map((e) => e.getAttribute('data-fw-cam')).slice(0, 2)))()`));
      for (const cam of cams) {
        const box = await clickSel(`[data-fw-cam="${cam}"]`, '.fw-cam');
        if (box.reaches) picked.push(cam);
      }
      await clickSel(`[data-fw-node="${id}"]`);
      await sleep(500);
    }
    check('cameras can be ticked from MORE THAN ONE appliance, which is the whole point of a '
      + 'fleet wall', new Set(picked.map((p) => p.split('::')[0])).size >= 2,
      JSON.stringify(picked));

    const saved = await clickSel('[data-fw-act=save]');
    check('the wall can be saved from its own form', saved.reaches === true, JSON.stringify(saved));
    await sleep(3500);
    const err = await evalJs(`(() => { const e = document.querySelector('[data-fw=error]'); return e ? e.textContent.trim() : ''; })()`);
    check('and it saved without complaint', err === '', err);

    // ---- read it back FROM THE SERVER ------------------------------------------------------
    const stored = JSON.parse(await evalJs(`(async () => {
      const b = await (await fetch('/api/fleet-walls', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      const mine = items.find((w) => w.name === ${JSON.stringify(NAME)});
      return JSON.stringify(mine ? { found: true, tiles: mine.tileList || [], grid: mine.grid,
        cycle: mine.cycleSeconds, pop: mine.autoPopSeconds } : { found: false, names: items.map((w) => w.name) });
    })()`));
    check('the wall the operator built really exists on the server', stored.found === true,
      JSON.stringify(stored).slice(0, 200));
    if (stored.found) {
      check('with cameras from two different appliances on it',
        new Set((stored.tiles || []).map((t) => t.nodeId)).size >= 2,
        JSON.stringify((stored.tiles || []).map((t) => t.nodeId + ':' + t.cameraId)));
      check('and the rotation and alarm settings the form was given',
        stored.cycle === 5 && stored.pop === 15, JSON.stringify({ cycle: stored.cycle, pop: stored.pop }));
    }

    // ---- the tiles are on screen, and an offline one SAYS SO -------------------------------
    await sleep(2500);
    const grid = JSON.parse(await evalJs(`(() => {
      const tiles = [...document.querySelectorAll('[data-fw=tile]')].map((e) => {
        const r = e.getBoundingClientRect();
        return {
          key: e.getAttribute('data-fw-tile'), state: e.getAttribute('data-fw-state'),
          alarmed: e.getAttribute('data-fw-alarmed') === '1',
          head: (e.querySelector('.fw-tile-node') || {}).textContent || '',
          dark: (e.querySelector('[data-fw=tile-dark]') || {}).textContent || '',
          w: Math.round(r.width), h: Math.round(r.height),
        };
      });
      return JSON.stringify({
        tiles, gridAttr: (document.querySelector('[data-fw=grid]') || {}).getAttribute
          ? document.querySelector('[data-fw=grid]').getAttribute('data-fw-grid') : null,
        rawKeys: [...new Set((document.body.innerText || '').match(/\\bfw\\.[a-zA-Z0-9_.-]+/g) || [])],
        overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
      });
    })()`));
    check('the wall renders its tiles', (grid.tiles || []).length > 0,
      JSON.stringify((grid.tiles || []).map((x) => x.state)));
    // A WALL WHOSE TILES ARE 40 PIXELS TALL IS NOT A WALL, and the first build of this screen
    // rendered exactly that: every tile present, named and correctly addressed, every
    // assertion green, and strips of caption where the pictures should be. Found by opening
    // the screenshot. The size is now measured.
    const tooSmall = (grid.tiles || []).filter((x) => x.h < 120);
    check('the tiles are big enough to be watched — a wall of captions is not a video wall',
      (grid.tiles || []).length > 0 && tooSmall.length === 0,
      JSON.stringify((grid.tiles || []).map((x) => x.w + 'x' + x.h)));

    check('and every tile names the APPLIANCE it is on, not just a camera number',
      (grid.tiles || []).every((x) => x.head && x.head.trim().length > 0),
      JSON.stringify((grid.tiles || []).map((x) => x.head)));
    // THE ONE THAT MATTERS ON THIS SCREEN. An appliance that is down rendered as a black
    // rectangle is indistinguishable from a dark room.
    const dark = (grid.tiles || []).filter((x) => x.state !== 'online');
    if (dark.length) {
      check('a tile whose appliance cannot show a picture SAYS SO in words rather than going '
        + 'black', dark.every((x) => x.dark && x.dark.trim().length > 20),
        JSON.stringify(dark.map((x) => x.state + ': ' + x.dark.trim().slice(0, 60))));
    } else {
      console.log('   (note: every appliance was online this run, so the offline-tile wording '
        + 'was not exercised here; bench_w33d_fleet_wall.py stops a node and checks the count)');
    }
    // The frame worth looking at. The final shot is of an empty screen after the cleanup, and
    // the defects this programme keeps finding by LOOKING were all visible mid-run.
    const wallShot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'fleetwall-live-' + LANG + '.png'), Buffer.from(wallShot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'fleetwall-live-' + LANG + '.png'));

    // The differentiator, when the fleet happens to provide one: an alert on ANY appliance
    // pulls its camera onto the wall. Asserted opportunistically rather than by manufacturing
    // an alert, because the fleet generates real ones and a manufactured one would only prove
    // the wall reacts to something this check wrote.
    const alarmed = (grid.tiles || []).filter((x) => x.alarmed);
    if (alarmed.length) {
      check('an alert on an appliance pulled its camera onto the wall, and the wall says that '
        + 'is what it is showing',
        (await evalJs(`(() => !!document.querySelector('[data-fw=alarmed]'))()`)) === true,
        JSON.stringify(alarmed.map((x) => x.key)));
    } else {
      console.log('   (note: nothing raised an alarm during this run, so the auto-pop was not '
        + 'exercised here)');
    }

    check('no untranslated key reached the screen', (grid.rawKeys || []).length === 0,
      (grid.rawKeys || []).join(', ') || 'none');
    check('the page does not scroll sideways', grid.overflow <= 1, 'overflow=' + grid.overflow);

    if (LANG !== 'en') {
      const labels = JSON.parse(await evalJs(`(() => JSON.stringify(
        [...document.querySelectorAll('.fw-lead, .fw-limit, [data-fw-act]')].map((e) => e.textContent.trim()).filter(Boolean).slice(0, 5)))()`));
      check('the page really switched language', labels.some((l) => !/^[\x20-\x7e]*$/.test(l)),
        JSON.stringify(labels).slice(0, 160));
      if (LANG === 'ar') check('Arabic puts the page in RTL', grid.dir === 'rtl', 'dir=' + grid.dir);
    }

    // ---- and it can be taken away -----------------------------------------------------------
    await evalJs(`window.confirm = () => true, 1`);
    const del = await clickSel('[data-fw-act=delete]');
    check('Delete can be pressed at its own centre', del.reaches === true, JSON.stringify(del));
    await sleep(3000);
    const after = await evalJs(`(async () => {
      const b = await (await fetch('/api/fleet-walls', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      return items.filter((w) => w.name === ${JSON.stringify(NAME)}).length;
    })()`);
    check('deleting through the screen really removes it from the server', after === 0,
      'still present: ' + after);

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'fleetwall-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'fleetwall-' + LANG + '.png'));
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
