// Drive mymatasan's relay-output panel in headless Chrome, against a REAL ONVIF device (W3-5b).
//
// WHY THIS IS NOT THE API BENCH AGAIN. bench_w35b_events.py proves the appliance can switch
// a siren. This proves an OPERATOR can — and, more importantly, that the one control that
// must never be unavailable is never unavailable.
//
//   * THE OFF BUTTON IS THE POINT. A button that stops a siren must not be greyed out by the
//     appliance's own busy state, which is exactly what it would be during the request that
//     started the siren. The check clicks Switch on and then, WITHOUT WAITING, asserts the
//     off control is still enabled and hit-testable — and that a real click on it reaches
//     the device.
//   * THE RTL DEFECT W3-5a SHIPPED. A control positioned with a logical inset inside a
//     physically-anchored box rendered perfectly and landed outside its tile in Arabic. This
//     check asserts every new control is inside its tile and is what a click at its own
//     centre actually hits, in both directions.
//   * A held output has to SAY it is held: it is the one state where a restart of the
//     appliance leaves something in the building energised.
//
// It starts the ONVIF simulator itself.
//
// Usage (with fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_relay.js <output-dir> [lang] [password]
const { spawn, spawnSync } = require('child_process');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';
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

function startSim() {
  spawnSync('docker', ['rm', '-f', SIM], { stdio: 'ignore' });
  const script = path.join(__dirname, 'onvifsim.py');
  spawnSync('docker', ['run', '-d', '--name', SIM, '--network', 'benchnet',
    '-p', `${SIM_HOST_PORT}:${SIM_PORT}`, '-v', `${script}:/onvifsim.py:ro`,
    'python:3-slim', 'python', '/onvifsim.py', String(SIM_PORT)], { stdio: 'ignore' });
}

async function simReady() {
  for (let i = 0; i < 90; i++) {
    try { if ((await fetch(SIM_URL + '/journal')).ok) return true; } catch (_) { /* not up */ }
    await sleep(1000);
  }
  return false;
}

async function device() { return (await fetch(SIM_URL + '/journal')).json(); }
async function resetJournal() { await fetch(SIM_URL + '/journal/reset', { method: 'POST' }); }

function relayStates(journal) {
  return (journal.journal || [])
    .filter((e) => e.action === 'SetRelayOutputState' && !e.detail.refused)
    .map((e) => `${e.detail.token}:${e.detail.state}`);
}

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
  const profile = path.join(OUT, 'chrome-profile-relay-' + tag);
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
    // The first-run wizard is not in English on a fresh fleet — a skip pattern that only
    // knows English leaves the check staring at a setup screen and reporting that the
    // product has no navigation.
    const skipped = await evalJs(`(() => {
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

async function clickAt(cdp, evalJs, selector) {
  const box = JSON.parse(await evalJs(`(() => {
    const el = document.querySelector(${JSON.stringify(selector)});
    if (!el) return 'null';
    const r = el.getBoundingClientRect();
    return JSON.stringify({ x: Math.round(r.left + r.width / 2), y: Math.round(r.top + r.height / 2), w: r.width });
  })()`));
  if (!box || box === 'null' || !box.w) return null;
  for (const type of ['mousePressed', 'mouseReleased']) {
    await cdp.send('Input.dispatchMouseEvent', {
      type, x: box.x, y: box.y, button: 'left', clickCount: 1, buttons: type === 'mousePressed' ? 1 : 0,
    });
  }
  return box;
}

const CAM_NAME = 'Relay dome ' + Date.now();

(async () => {
  startSim();
  check('the simulated ONVIF device came up', await simReady());

  await session('a', 9233, async ({ cdp, evalJs }) => {
    let signedIn = await signIn(cdp, evalJs, BASE + '/');
    check('the bench admin can sign in', signedIn === 'submitted' || signedIn === 'ALREADY IN', signedIn);

    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-subtle','--danger-text'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token the relay panel uses resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted',
      !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + theme.bodyBg);

    const setup = JSON.parse(await evalJs(`(async () => {
      const r = await fetch('/api/cameras/discovered', { method: 'POST', credentials: 'include',
        headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
          name: ${JSON.stringify(CAM_NAME)}, host: '${SIM}', port: ${SIM_PORT},
          xAddr: 'http://${SIM}:${SIM_PORT}/onvif/device_service',
          mediaXAddr: 'http://${SIM}:${SIM_PORT}/onvif/media_service',
          rtspUrl: 'rtsp://ptzcam:8554/cam', username: '', password: '',
          description: 'w3-5b screen check',
        }) });
      const b = await r.json();
      const cam = b?.data?.result ?? b?.result ?? b;
      return JSON.stringify({ camId: typeof cam === 'number' ? cam : (cam?.id || cam?.cameraId || cam?.result) });
    })()`));
    check('an ONVIF camera with relay outputs exists to drive', !!setup.camId, JSON.stringify(setup));
    if (!setup.camId) return;

    // Re-sign-in rather than a bare navigate: the app renders its sign-in screen while it
    // re-checks the session, and a check that clicks during that window reports that the
    // product has no navigation.
    signedIn = await signIn(cdp, evalJs, BASE + '/');
    await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /live views|paparan langsung|实时|المباشر/i.test(e.textContent || ''));
      if (hit) hit.click();
      return 1;
    })()`);
    await sleep(3500);
    const added = await evalJs(`(async () => {
      const hit = [...document.querySelectorAll('.add-strip button')]
        .find((b) => (b.textContent || '').includes(${JSON.stringify(CAM_NAME)}));
      if (!hit && document.querySelector('.view-tile[data-camera-id]')) return 'already on the wall';
      if (!hit) return 'NOT IN THE ADD STRIP';
      hit.click();
      return 'added';
    })()`);
    check('the camera can be put on the wall', added === 'added' || added === 'already on the wall', added);
    await sleep(3000);

    // ---- THE RTL LESSON FROM W3-5a, applied to the new control ---------------------
    const hitTest = await evalJs(`(() => {
      const btn = document.querySelector(${JSON.stringify(`.view-tile[data-camera-id="${setup.camId}"] .relay-tile-button`)});
      if (!btn) return 'NO BUTTON';
      const r = btn.getBoundingClientRect();
      const at = document.elementFromPoint(Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2));
      const tile = btn.closest('.view-tile').getBoundingClientRect();
      return JSON.stringify({
        hits: at === btn || btn.contains(at),
        insideTile: r.left >= tile.left && r.right <= tile.right && r.top >= tile.top && r.bottom <= tile.bottom,
        at: at ? (at.tagName + '.' + (at.className || '').toString().split(' ')[0]) : 'none',
        dir: getComputedStyle(document.documentElement).direction,
      });
    })()`);
    const hit = hitTest.startsWith('{') ? JSON.parse(hitTest) : { hits: false, insideTile: false };
    check('the outputs button is inside its tile and is what a click at it hits',
      hit.hits && hit.insideTile, hitTest);

    await clickAt(cdp, evalJs, `.view-tile[data-camera-id="${setup.camId}"] .relay-tile-button`);
    await sleep(3000);
    const panel = JSON.parse(await evalJs(`(() => {
      const p = document.querySelector('.relay-panel');
      if (!p) return JSON.stringify({ open: false });
      return JSON.stringify({
        open: true,
        tokens: [...p.querySelectorAll('.relay-list li')].map((e) => e.dataset.relayToken),
        modes: [...p.querySelectorAll('[data-relay-mode]')].map((e) => e.dataset.relayMode),
        dir: getComputedStyle(document.documentElement).direction,
      });
    })()`));
    check('a REAL click opens the outputs panel', panel.open, hitTest + ' -> ' + JSON.stringify(panel).slice(0, 200));
    if (!panel.open) return;
    check('it lists the outputs the camera has',
      JSON.stringify(panel.tokens.slice().sort()) === JSON.stringify(['RELAY_1', 'RELAY_2']),
      JSON.stringify(panel.tokens));
    check('and says which ones stay on until switched off',
      panel.modes.includes('bistable'), JSON.stringify(panel.modes));

    // ---- THE OFF BUTTON MUST NEVER BE UNAVAILABLE ----------------------------------
    await resetJournal();
    // Switch RELAY_1 ON and then, WITHOUT waiting for anything to settle, look at the off
    // control. This is the moment the appliance is busiest and the moment a siren is
    // sounding — and a disabled Off button here is the whole failure.
    const onBtn = `.relay-list li[data-relay-token="RELAY_1"] .relay-actions button:nth-child(2)`;
    await clickAt(cdp, evalJs, onBtn);
    const offState = JSON.parse(await evalJs(`(() => {
      const li = document.querySelector('.relay-list li[data-relay-token="RELAY_1"]');
      const off = li && [...li.querySelectorAll('.relay-actions button')].pop();
      if (!off) return JSON.stringify({ found: false });
      const r = off.getBoundingClientRect();
      const at = document.elementFromPoint(Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2));
      return JSON.stringify({
        found: true, disabled: off.disabled,
        opacity: getComputedStyle(off).opacity,
        hits: at === off || off.contains(at),
      });
    })()`));
    check('the off control is never disabled, not even mid-request',
      offState.found && offState.disabled === false, JSON.stringify(offState));
    check('and it is not dimmed into looking unavailable',
      Number(offState.opacity) >= 0.99, JSON.stringify(offState.opacity));
    check('and a click at its own centre reaches it', offState.hits, JSON.stringify(offState));

    await sleep(2500);
    await clickAt(cdp, evalJs, `.relay-list li[data-relay-token="RELAY_1"] .relay-actions button:last-child`);
    await sleep(2500);
    const sent = relayStates(await device());
    check('switching on and then off both reached the DEVICE',
      sent.includes('RELAY_1:active') && sent.includes('RELAY_1:inactive'), JSON.stringify(sent));
    check('and the device really ended up off',
      (await device()).relays.RELAY_1.active === false, JSON.stringify((await device()).relays.RELAY_1));

    // ---- a held output says it is held ----------------------------------------------
    await resetJournal();
    await clickAt(cdp, evalJs, `.relay-list li[data-relay-token="RELAY_1"] .relay-actions button:first-child`);
    await sleep(2500);
    const held = JSON.parse(await evalJs(`(() => {
      const li = document.querySelector('.relay-list li[data-relay-token="RELAY_1"]');
      return JSON.stringify({
        marked: !!li && li.classList.contains('held'),
        text: (li && li.querySelector('.relay-held') || {}).textContent || '',
      });
    })()`));
    check('an output this appliance is holding says so on screen',
      held.marked && held.text.length > 20, JSON.stringify(held).slice(0, 240));

    await cdp.send('Page.captureScreenshot', { format: 'png' }).then((r) => {
      require('fs').writeFileSync(path.join(OUT, `relay-panel-${LANG}.png`), Buffer.from(r.data, 'base64'));
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
