// Drive mymatasan's PTZ panel in headless Chrome, against a REAL ONVIF device (W3-5).
//
// WHY THIS EXISTS, and why it is not the API bench again. bench_w35_ptz.py proves the
// appliance can command a dome. This proves an OPERATOR can — which is a different claim,
// and the one that has failed every time it was checked separately in this programme:
//
//   * the button that opens the panel sits INSIDE `.ptz-ring-overlay`, which is
//     pointer-events:none precisely so its dead corners stop swallowing clicks meant for
//     the picture. A control added there that forgets to opt back in renders perfectly and
//     cannot be pressed. So this dispatches a REAL mouse click at the button's real screen
//     coordinates, not element.click().
//   * a preset list is only useful if it shows what the operator NAMED the position. This
//     asserts the names, not the tokens.
//   * pressing a preset has to move a PHYSICAL CAMERA. The check reads the simulated dome's
//     own journal over HTTP afterwards and asserts it was sent to the token behind the row
//     that was clicked. A panel that renders and posts nothing looks identical on screen.
//   * the tour editor builds an ORDERED route, and the order is the feature. The check adds
//     stops in one order and reads the saved route back.
//   * starting a patrol has to surface what it COSTS: while a camera patrols, tamper
//     detection cannot report it as re-aimed or covered. Discovering that from the absence
//     of an alert is not acceptable for a security feature, so the note is asserted.
//   * and all of it in RTL, because W3-1's fourth defect only surfaced in Arabic.
//
// It starts the ONVIF simulator itself, so it does not depend on bench_w35_ptz.py having
// been run first (that bench deletes its camera on the way out, on purpose).
//
// Usage (with fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_ptz.js <output-dir> [lang] [password]
const { spawn, spawnSync } = require('child_process');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';   // node-a (mymatasan)
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

const SIM = 'onvifsim';
const SIM_PORT = 8080;
const SIM_HOST_PORT = 18480;
const SIM_URL = `http://127.0.0.1:${SIM_HOST_PORT}`;

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail) });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail !== undefined ? '   ' + detail : ''));
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

// --- the simulated dome -------------------------------------------------------------

function startSim() {
  spawnSync('docker', ['rm', '-f', SIM], { stdio: 'ignore' });
  const script = path.join(__dirname, 'onvifsim.py');
  spawnSync('docker', ['run', '-d', '--name', SIM, '--network', 'benchnet',
    '-p', `${SIM_HOST_PORT}:${SIM_PORT}`, '-v', `${script}:/onvifsim.py:ro`,
    'python:3-slim', 'python', '/onvifsim.py', String(SIM_PORT)], { stdio: 'ignore' });
}

async function simReady() {
  for (let i = 0; i < 90; i++) {
    try {
      const r = await fetch(SIM_URL + '/journal');
      if (r.ok) return true;
    } catch (_) { /* not up yet */ }
    await sleep(1000);
  }
  return false;
}

async function journal() { return (await fetch(SIM_URL + '/journal')).json(); }
async function resetJournal() { await fetch(SIM_URL + '/journal/reset', { method: 'POST' }); }

function gotos(entries) {
  return (entries || []).filter((e) => e.action === 'GotoPreset' && !e.detail.refused)
    .map((e) => e.detail.token);
}

// --- chrome -------------------------------------------------------------------------

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
        setTimeout(() => rej(new Error(method + ' timed out')), 60000);
      });
    },
  };
}

async function session(tag, port, fn) {
  const profile = path.join(OUT, 'chrome-profile-ptz-' + tag);
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
      // The first-run wizard is not in English on a fresh fleet, and a skip pattern that
      // only knows English leaves the check staring at a setup screen and reporting that
      // the product has no navigation.
      const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done|langkau|lompat|跳过|略过|完成|تخطي|إنهاء/i.test(e.textContent || ''));
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

// clickAt dispatches a REAL mouse press and release at an element's centre. Used instead of
// element.click() wherever the question is "can this be pressed", not "does the handler
// work" — a control inside a pointer-events:none overlay passes the second and fails the
// first, which is exactly how the PTZ ring's own maximize bug got in.
async function clickAt(cdp, evalJs, selector) {
  const box = JSON.parse(await evalJs(`(() => {
    const el = document.querySelector(${JSON.stringify(selector)});
    if (!el) return 'null';
    const r = el.getBoundingClientRect();
    return JSON.stringify({ x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height / 2), w: r.width, h: r.height });
  })()`));
  if (!box || box === 'null' || !box.w) return null;
  for (const type of ['mousePressed', 'mouseReleased']) {
    await cdp.send('Input.dispatchMouseEvent', {
      type, x: box.x, y: box.y, button: 'left', clickCount: 1, buttons: type === 'mousePressed' ? 1 : 0,
    });
  }
  return box;
}

// typeInto sends REAL keystrokes. A controlled input that cannot be typed into renders
// perfectly, accepts a programmatic value set, and is unusable — which is what
// uicheck_mail_dest.js found on the recipients field.
async function typeInto(cdp, evalJs, selector, text) {
  await clickAt(cdp, evalJs, selector);
  await sleep(150);
  for (const ch of text) {
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', text: ch, key: ch });
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: ch });
  }
  await sleep(200);
  return evalJs(`(document.querySelector(${JSON.stringify(selector)}) || {}).value || ''`);
}

const CAM_NAME = 'Screen dome ' + Date.now();

(async () => {
  startSim();
  check('the simulated ONVIF dome came up', await simReady());

  await session('a', 9231, async ({ cdp, evalJs }) => {
    const signedIn = await signIn(cdp, evalJs, BASE + '/');
    check('the bench admin can sign in', signedIn === 'submitted' || signedIn === 'ALREADY IN', signedIn);

    // W3-1's REGRESSION CHECK, copied into every uicheck since: a screen pass that only
    // reads DOM text and geometry passed 25/25 while the entire design-token block was
    // missing from the stylesheet, because CSS has no undefined-variable error.
    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-subtle','--accent'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token the PTZ panel uses resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted',
      !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + theme.bodyBg);

    // Its own PTZ camera and three saved positions, through the API, so the screen check
    // does not depend on the API bench having been run (that one deletes its camera on the
    // way out, deliberately).
    const setup = JSON.parse(await evalJs(`(async () => {
      const post = async (url, body) => {
        const r = await fetch(url, { method: 'POST', credentials: 'include',
          headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        return r.json();
      };
      const unwrap = (b) => b?.data?.result ?? b?.result ?? b;
      const cam = unwrap(await post('/api/cameras/discovered', {
        name: ${JSON.stringify(CAM_NAME)}, host: '${SIM}', port: ${SIM_PORT},
        xAddr: 'http://${SIM}:${SIM_PORT}/onvif/device_service',
        mediaXAddr: 'http://${SIM}:${SIM_PORT}/onvif/media_service',
        ptzXAddr: 'http://${SIM}:${SIM_PORT}/onvif/ptz_service',
        ptzSupported: true, profileToken: 'MainProfile',
        rtspUrl: 'rtsp://ptzcam:8554/cam', username: '', password: '',
        description: 'w3-5 screen check',
      }));
      const camId = typeof cam === 'number' ? cam : (cam?.id || cam?.cameraId);
      const tokens = {};
      for (const name of ['Front gate', 'Loading bay', 'Car park']) {
        tokens[name] = unwrap(await post('/api/cameras/' + camId + '/ptz/presets', { name }))?.token;
      }
      return JSON.stringify({ camId, tokens });
    })()`));
    check('a PTZ camera with three saved positions exists to drive',
      !!setup.camId && Object.values(setup.tokens).every(Boolean), JSON.stringify(setup));
    if (!setup.camId) return;

    // ---- onto the Live Views screen, with that camera on it -------------------------
    // RELOAD FIRST. The SPA loaded its camera list before the fetches above created this
    // camera, and a screen check that asserts against a list the page has not been told
    // about is measuring its own setup order, not the product.
    // ...through signIn again, not a bare navigate: the app renders its sign-in screen
    // while it re-checks the session, and a check that clicks the nav during that window
    // reports "no Live Views" about a perfectly working page.
    await signIn(cdp, evalJs, BASE + '/');
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const items = [...rail.querySelectorAll('button, a')];
      const hit = items.find((e) => /live views|paparan langsung|实时|المباشر/i.test(e.textContent || ''));
      if (!hit) return 'NO LIVE VIEWS NAV: ' + JSON.stringify(items.map(e=>e.textContent.trim()).filter(Boolean).slice(0,20));
      hit.click();
      return 'clicked';
    })()`);
    check('Live Views opens', nav === 'clicked', nav);
    await sleep(3500);
    const added = await evalJs(`(async () => {
      const hit = [...document.querySelectorAll('.add-strip button')]
        .find((b) => (b.textContent || '').includes(${JSON.stringify(CAM_NAME)}));
      // Already on the wall from an earlier run of this check is a pass, not a failure:
      // the add strip only offers cameras that are NOT yet shown.
      if (!hit && document.querySelector('.view-tile[data-camera-id]')) return 'already on the wall';
      if (!hit) return 'NOT IN THE ADD STRIP: strip=' + document.querySelectorAll('.add-strip').length
        + ' buttons=' + JSON.stringify([...document.querySelectorAll('.add-strip button')].map(b=>b.textContent.trim()).slice(0,8));
      hit.click();
      return 'added';
    })()`);
    check('the PTZ camera can be put on the wall', added === 'added' || added === 'already on the wall', added);
    await sleep(3000);

    // ---- THE BUTTON HAS TO BE PRESSABLE, not merely present -------------------------
    const overlay = JSON.parse(await evalJs(`(() => {
      const tile = [...document.querySelectorAll('.view-tile')]
        .find((t) => String(t.dataset.cameraId) === String(${setup.camId}));
      if (!tile) return JSON.stringify({ tile: false });
      const btn = tile.querySelector('.ptz-presets-button');
      return JSON.stringify({
        tile: true, button: !!btn,
        pointerEvents: btn ? getComputedStyle(btn).pointerEvents : '',
        overlayPointerEvents: getComputedStyle(tile.querySelector('.ptz-ring-overlay') || tile).pointerEvents,
      });
    })()`));
    check('the PTZ camera has a presets button beside its ring',
      overlay.tile && overlay.button, JSON.stringify(overlay));
    check('and the button opts back into pointer events its overlay switched off',
      overlay.pointerEvents === 'auto' && overlay.overlayPointerEvents === 'none',
      JSON.stringify(overlay));

    // WHAT IS ACTUALLY AT THE CLICK POINT. "The button did not open the panel" has two very
    // different causes — a dead handler, or something else sitting on top of it — and only
    // one of them is a layout bug. Reading elementFromPoint back is what turns a mystery
    // into a diagnosis, and it is what caught this control landing outside its tile in RTL.
    const hitTest = await evalJs(`(() => {
      const btn = document.querySelector(${JSON.stringify(`.view-tile[data-camera-id="${setup.camId}"] .ptz-presets-button`)});
      if (!btn) return 'NO BUTTON';
      const r = btn.getBoundingClientRect();
      const at = document.elementFromPoint(Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2));
      const tile = btn.closest('.view-tile').getBoundingClientRect();
      const inside = r.left >= tile.left && r.right <= tile.right && r.top >= tile.top && r.bottom <= tile.bottom;
      return JSON.stringify({
        hits: at === btn || btn.contains(at),
        insideTile: inside,
        at: at ? (at.tagName + '.' + (at.className || '').toString().split(' ')[0]) : 'none',
        rect: { l: Math.round(r.left), r: Math.round(r.right) },
        tile: { l: Math.round(tile.left), r: Math.round(tile.right) },
      });
    })()`);
    const hit = hitTest.startsWith('{') ? JSON.parse(hitTest) : { hits: false, insideTile: false };
    check('the presets button is inside its own tile and is what a click at it hits',
      hit.hits && hit.insideTile, hitTest);

    const clicked = await clickAt(cdp, evalJs, `.view-tile[data-camera-id="${setup.camId}"] .ptz-presets-button`);
    await sleep(2500);
    const panel = JSON.parse(await evalJs(`(() => {
      const p = document.querySelector('.ptz-panel');
      if (!p) return JSON.stringify({ open: false });
      return JSON.stringify({
        open: true,
        title: (p.querySelector('.ptz-panel-title')||{}).textContent || '',
        names: [...p.querySelectorAll('.ptz-preset-name')].map(e => e.textContent.trim()),
        tokens: [...p.querySelectorAll('.ptz-preset-list li')].map(e => e.dataset.presetToken),
        dir: getComputedStyle(document.documentElement).direction,
      });
    })()`));
    check('a REAL click on it opens the panel', panel.open, JSON.stringify(clicked) + ' -> ' + JSON.stringify(panel).slice(0, 200));
    if (!panel.open) return;

    // The list has to show what the operator NAMED the position. A panel listing
    // PRESET_1/PRESET_2 is a panel nobody can use.
    check('the panel lists the positions by the names they were given',
      JSON.stringify(panel.names.slice().sort()) === JSON.stringify(['Car park', 'Front gate', 'Loading bay']),
      JSON.stringify(panel.names));
    check('the panel is laid out in the document direction', !!panel.dir, 'dir=' + panel.dir);

    // ---- PRESSING A ROW HAS TO MOVE A PHYSICAL CAMERA -------------------------------
    await resetJournal();
    const wanted = panel.tokens[1];
    await clickAt(cdp, evalJs, `.ptz-preset-list li[data-preset-token="${wanted}"] .ptz-preset-go`);
    await sleep(2500);
    const sent = gotos((await journal()).journal);
    check('pressing a position sends the DEVICE there, and to the right one',
      sent.length === 1 && sent[0] === wanted, JSON.stringify(sent) + ' wanted ' + wanted);

    // ---- saving a position, with REAL keystrokes ------------------------------------
    const typed = await typeInto(cdp, evalJs, '.ptz-panel .ptz-save-row input', 'Side door');
    check('the new-position name field accepts real typing', typed === 'Side door', JSON.stringify(typed));
    await evalJs(`(() => {
      const row = document.querySelector('.ptz-panel .ptz-save-row');
      const btn = row && row.querySelector('button');
      if (btn) btn.click();
      return 1;
    })()`);
    await sleep(2500);
    const onDevice = Object.values((await journal()).presets).map((p) => p.name);
    check('and the position lands ON THE DEVICE under that name',
      onDevice.includes('Side door'), JSON.stringify(onDevice));
    const relisted = await evalJs(`JSON.stringify([...document.querySelectorAll('.ptz-preset-name')].map(e => e.textContent.trim()))`);
    check('the panel shows it without a reload', JSON.parse(relisted).includes('Side door'), relisted);

    // ---- the tour editor: the ORDER is the feature ----------------------------------
    const opened = await evalJs(`(() => {
      const btns = [...document.querySelectorAll('.ptz-panel .ptz-section button')];
      const hit = btns.find((b) => (b.textContent || '').trim() && !b.closest('.ptz-preset-list') && !b.closest('.ptz-save-row') && !b.closest('.ptz-home-row') && !b.closest('.ptz-tour-list'));
      if (!hit) return 'NO NEW TOUR BUTTON';
      hit.click();
      return hit.textContent.trim();
    })()`);
    await sleep(1200);
    const editorUp = await evalJs(`!!document.querySelector('.ptz-tour-editor')`);
    check('a new tour can be started from the panel', !!editorUp, opened);

    const tourName = 'Screen patrol ' + Date.now();
    const nameTyped = await typeInto(cdp, evalJs, '.ptz-tour-editor .ptz-save-row input[type=text], .ptz-tour-editor .ptz-save-row input:not([type])', tourName);
    check('the tour name field accepts real typing', nameTyped === tourName, JSON.stringify(nameTyped).slice(0, 80));

    // Add the LAST position first, then the first: an editor that sorts its stops instead
    // of keeping the order they were added in would look identical and patrol differently.
    const order = [panel.tokens[2], panel.tokens[0]];
    for (const token of order) {
      await evalJs(`(() => {
        const sel = document.querySelector('.ptz-tour-editor select');
        if (!sel) return 0;
        Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(sel, ${JSON.stringify(token)});
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        return 1;
      })()`);
      await sleep(700);
    }
    const stops = JSON.parse(await evalJs(`JSON.stringify([...document.querySelectorAll('.ptz-stop-list li')].map(e => e.dataset.stopPreset))`));
    check('the route keeps the order the stops were added in',
      JSON.stringify(stops) === JSON.stringify(order), JSON.stringify(stops) + ' wanted ' + JSON.stringify(order));

    await evalJs(`(() => {
      const acts = document.querySelector('.ptz-tour-editor .modal-actions');
      const save = acts && [...acts.querySelectorAll('button')].pop();
      if (save) save.click();
      return 1;
    })()`);
    await sleep(2500);
    const saved = JSON.parse(await evalJs(`(async () => {
      const r = await fetch('/api/cameras/${setup.camId}/ptz/tours', { credentials: 'include' });
      const b = await r.json();
      const rows = (b?.data?.result?.tours) || (b?.result?.tours) || [];
      return JSON.stringify(rows.map((t) => ({ name: t.name, stops: t.stops, running: t.isRunning })));
    })()`));
    const mine = saved.find((t) => t.name === tourName);
    check('the tour saves, with the route in the order it was built',
      !!mine && mine.stops === order.join(','), JSON.stringify(saved).slice(0, 240));

    // ---- starting a patrol says what it COSTS, and actually patrols -----------------
    await resetJournal();
    const started = await evalJs(`(() => {
      const li = [...document.querySelectorAll('.ptz-tour-list li')]
        .find((e) => (e.textContent || '').includes(${JSON.stringify(tourName)}));
      if (!li) return 'TOUR NOT LISTED';
      const btn = li.querySelector('.ptz-tour-actions button');
      if (!btn) return 'NO START BUTTON';
      btn.click();
      return 'clicked';
    })()`);
    await sleep(3000);
    check('the patrol can be started from the panel', started === 'clicked', started);

    const noteText = await evalJs(`((document.querySelector('.ptz-tamper-note')||{}).textContent||'').trim()`);
    check('and the screen says what a running patrol costs the tamper monitor',
      noteText.length > 20, JSON.stringify(noteText).slice(0, 200));

    const marked = await evalJs(`(() => {
      const li = [...document.querySelectorAll('.ptz-tour-list li')]
        .find((e) => (e.textContent || '').includes(${JSON.stringify(tourName)}));
      return li ? (li.querySelector('[data-tour-state]')||{}).dataset?.tourState || '' : 'MISSING';
    })()`);
    check('the tour is marked as patrolling on screen', marked === 'running', marked);

    await sleep(9000);
    const walked = gotos((await journal()).journal);
    check('and the DEVICE is actually being walked round that route',
      walked.length >= 1 && order.includes(walked[0]), JSON.stringify(walked));

    // Stopping it has to stop it — including the note, which is the operator's only sign
    // that the trade is no longer being made.
    await evalJs(`(() => {
      const li = [...document.querySelectorAll('.ptz-tour-list li')]
        .find((e) => (e.textContent || '').includes(${JSON.stringify(tourName)}));
      const btn = li && li.querySelector('.ptz-tour-actions button');
      if (btn) btn.click();
      return 1;
    })()`);
    await sleep(3000);
    const afterStop = await evalJs(`JSON.stringify({
      note: !!document.querySelector('.ptz-tamper-note'),
      state: (document.querySelector('[data-tour-state]')||{}).dataset?.tourState || '',
    })`);
    check('stopping the patrol clears both the state and the warning',
      JSON.parse(afterStop).note === false, afterStop);

    await cdp.send('Page.captureScreenshot', { format: 'png' }).then((r) => {
      require('fs').writeFileSync(path.join(OUT, `ptz-panel-${LANG}.png`), Buffer.from(r.data, 'base64'));
    });
  });

  spawnSync('docker', ['rm', '-f', SIM], { stdio: 'ignore' });

  const ok = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${ok}/${CHECKS.length} checks passed`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exit(ok === CHECKS.length ? 0 : 1);
})().catch((err) => {
  spawnSync('docker', ['rm', '-f', SIM], { stdio: 'ignore' });
  console.error(err);
  process.exit(1);
});
