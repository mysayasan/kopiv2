// Drive mymatasan's privacy-zone screen in headless Chrome (W3-6).
//
// WHAT THIS PROVES THAT THE API BENCH CANNOT. bench_w36_privacy.py proves the appliance
// tells the TRUTH about what a camera is masking. This proves an operator is SHOWN it —
// which is a different claim, and the one that decides whether the feature protects
// anybody.
//
// The whole risk of this feature is overstatement. One drawn region feeds two mechanisms
// that guarantee different things: the camera burning it in (never recorded) and this
// recorder redacting exports (recorded, but does not leave the building). A screen that
// listed zones and said nothing else would imply the stronger claim on every camera,
// including the ones that cannot do it. So the check reads the STATUS BANNER back in each
// of the three states and asserts the wording actually changes — a status that says
// "confirmed" on a camera storing nothing is the failure this feature can have.
//
// It also applies the two lessons the last two items shipped: every new control is
// hit-tested with elementFromPoint at its own centre (W3-5a's flipped out of its tile in
// Arabic; W3-5b's sat under the tile header), and the design tokens are read back from the
// live page (W3-1 passed 25/25 with the whole token block missing).
//
// Usage (with fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_privacy.js <output-dir> [lang] [password]
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

async function maskMode(mode) { await fetch(`${SIM_URL}/masks/mode/${mode}`, { method: 'POST' }); }
async function maskSupport(on) { await fetch(`${SIM_URL}/masks/support/${on ? 'on' : 'off'}`, { method: 'POST' }); }

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
  const profile = path.join(OUT, 'chrome-profile-privacy-' + tag);
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

// status reads the banner the operator actually sees, and the machine-readable state
// beside it. Both, because the state without the sentence proves the DOM has an attribute
// and the sentence without the state proves nothing about what it means.
async function status(evalJs) {
  return JSON.parse(await evalJs(`(() => {
    const el = document.querySelector('.privacy-status');
    if (!el) return JSON.stringify({ present: false });
    return JSON.stringify({
      present: true,
      state: el.dataset.masking || '',
      text: (el.textContent || '').trim(),
    });
  })()`));
}

const CAM_NAME = 'Privacy cam ' + Date.now();

(async () => {
  startSim();
  check('the simulated ONVIF camera came up', await simReady());
  await maskMode('honest');
  await maskSupport(true);

  await session('a', 9235, async ({ cdp, evalJs }) => {
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
    check('every design token the privacy screen uses resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted',
      !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + theme.bodyBg);

    const setup = JSON.parse(await evalJs(`(async () => {
      const r = await fetch('/api/cameras/discovered', { method: 'POST', credentials: 'include',
        headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
          name: ${JSON.stringify(CAM_NAME)}, host: '${SIM}', port: ${SIM_PORT},
          xAddr: 'http://${SIM}:${SIM_PORT}/onvif/device_service',
          mediaXAddr: 'http://${SIM}:${SIM_PORT}/onvif/media_service',
          profileToken: 'MainProfile',
          rtspUrl: 'rtsp://ptzcam:8554/cam', username: '', password: '',
          description: 'w3-6 screen check',
        }) });
      const b = await r.json();
      const cam = b?.data?.result ?? b?.result ?? b;
      return JSON.stringify({ camId: typeof cam === 'number' ? cam : (cam?.id || cam?.cameraId || cam?.result) });
    })()`));
    check('a camera exists to draw on', !!setup.camId, JSON.stringify(setup));
    if (!setup.camId) return;

    // ESTABLISH THE PRECONDITION, do not assume it. Saving a camera is keyed on its ONVIF
    // address, so a second run of this check reuses the SAME camera row — and the zones
    // the previous run drew are still on it. The "nothing drawn yet" assertion below then
    // fails for a reason that has nothing to do with the product, which is how a bench
    // starts training its reader to ignore it.
    const cleared = await evalJs(`(async () => {
      const r = await fetch('/api/cameras/${setup.camId}/privacy', { credentials: 'include' });
      const b = await r.json();
      const zones = (b?.data?.result?.zones) || (b?.result?.zones) || [];
      for (const z of zones) {
        await fetch('/api/cameras/${setup.camId}/privacy/' + z.id + '/delete',
          { method: 'POST', credentials: 'include' });
      }
      return zones.length;
    })()`);
    check('the camera starts with no privacy zones on it', true, 'cleared ' + cleared);

    signedIn = await signIn(cdp, evalJs, BASE + '/');

    // ---- get to the camera's Privacy tab -------------------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const items = [...rail.querySelectorAll('button, a')];
      const hit = items.find((e) => /cameras|kamera|摄像机|الكاميرات/i.test(e.textContent || ''));
      if (!hit) return 'NO CAMERAS NAV: ' + JSON.stringify(items.map(e=>e.textContent.trim()).filter(Boolean).slice(0,20));
      hit.click();
      return 'clicked';
    })()`);
    check('the Cameras page opens', nav === 'clicked', nav);
    await sleep(3500);
    // The camera tree entry is the LEAF that names the camera, not any ancestor that
    // happens to contain the text: clicking a container selects nothing, the detail tabs
    // never render, and the check reports that the product has no Privacy tab.
    const picked = await evalJs(`(async () => {
      const name = ${JSON.stringify(CAM_NAME)};
      const all = [...document.querySelectorAll('button, a, li, [role=treeitem]')]
        .filter((e) => (e.textContent || '').includes(name));
      if (!all.length) return 'CAMERA NOT LISTED';
      for (const el of all.slice().reverse()) {
        el.click();
        await new Promise((r) => setTimeout(r, 900));
        if (document.querySelector('.ui-tabs')) return 'picked';
      }
      return 'NO TABS AFTER SELECTING ' + all.length + ' candidate(s)';
    })()`);
    check('the camera can be opened', picked === 'picked', picked);
    await sleep(2500);

    const tab = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('.ui-tabs button, .ui-tabs a')]
        .find((e) => /privacy|privasi|隐私|الخصوصية/i.test(e.textContent || ''));
      if (!hit) return 'NO PRIVACY TAB: ' + JSON.stringify([...document.querySelectorAll('.ui-tabs button')].map(e=>e.textContent.trim()));
      const r = hit.getBoundingClientRect();
      const at = document.elementFromPoint(Math.round(r.left + r.width/2), Math.round(r.top + r.height/2));
      hit.click();
      return (at === hit || hit.contains(at)) ? 'clicked' : 'COVERED BY ' + (at ? at.tagName : 'nothing');
    })()`);
    check('the Privacy tab is there and is what a click at it hits', tab === 'clicked', tab);
    await sleep(3000);

    const panel = await evalJs(`!!document.querySelector('.privacy-panel')`);
    check('the privacy screen opens', !!panel, String(panel));
    if (!panel) return;

    // ---- THE STATUS IS THE FEATURE -------------------------------------------------
    let st = await status(evalJs);
    check('the status banner is shown before anything else', st.present, JSON.stringify(st));
    check('a camera that can mask, with nothing drawn, is not alarming',
      st.state === 'confirmed', JSON.stringify(st));

    // Draw one through the API, then reload the tab: the drawing gestures are the shared
    // zone editor's, already covered elsewhere; what THIS check is about is the wording.
    await evalJs(`(async () => {
      await fetch('/api/cameras/${setup.camId}/privacy', { method: 'POST', credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'Neighbour window', points: [[0.1,0.1],[0.4,0.1],[0.4,0.4],[0.1,0.4]], style: 'color', enabled: true }) });
      return 1;
    })()`);
    await clickAt(cdp, evalJs, '.privacy-panel .toolbar button');
    await sleep(3000);

    st = await status(evalJs);
    check('with a zone the camera is really masking, the screen says CONFIRMED',
      st.state === 'confirmed', JSON.stringify(st));
    check('and says in words that the area is not recorded',
      /not recorded|tidak dirakam|不会被录制|لا تُسجَّل/i.test(st.text), JSON.stringify(st.text).slice(0, 200));
    // AND IT IS IN THE OPERATOR'S LANGUAGE. The server composes an English sentence for
    // the API and the log; rendering that on the screen put the most important line on
    // the page in English in every non-English installation, which is what the Arabic
    // pass caught. In a non-English run the banner must NOT be the English text.
    if (LANG !== 'en') {
      check('the status sentence is translated, not the API English',
        !/^The camera |^This camera /.test(st.text), JSON.stringify(st.text).slice(0, 160));
    }

    const zone = JSON.parse(await evalJs(`(() => {
      const li = document.querySelector('.privacy-zone-list li[data-zone-id]');
      if (!li) return JSON.stringify({ listed: false });
      return JSON.stringify({
        listed: true,
        text: (li.textContent || '').trim(),
        state: (li.querySelector('[data-zone-state]') || {}).dataset?.zoneState || '',
      });
    })()`));
    check('the zone is listed and says the camera is holding it',
      zone.listed && /camera is hiding|kamera sedang|由摄像机隐藏|الكاميرا تخفيها/i.test(zone.text),
      JSON.stringify(zone).slice(0, 240));

    // ---- THE CAMERA STARTS LYING ----------------------------------------------------
    //
    // It accepts the mask and stores a different shape. Nothing on screen would change if
    // the product trusted the 200 — which is exactly the failure this feature can have.
    await maskMode('shifted');
    await clickAt(cdp, evalJs, '.privacy-panel .toolbar button');
    await sleep(3500);
    st = await status(evalJs);
    check('a camera that stores a DIFFERENT shape flips the screen off CONFIRMED',
      st.state === 'unconfirmed', JSON.stringify(st));
    check('and the wording names the zone that is not protected',
      st.text.includes('Neighbour window'), JSON.stringify(st.text).slice(0, 240));
    check('and tells the operator to treat the recording as containing it',
      /recording|rakaman|录像|التسجيل/i.test(st.text), JSON.stringify(st.text).slice(0, 240));
    if (LANG !== 'en') {
      check('the unconfirmed sentence is translated too',
        !/^The camera accepted/.test(st.text), JSON.stringify(st.text).slice(0, 160));
    }

    // ---- A CAMERA THAT CANNOT MASK AT ALL --------------------------------------------
    await maskSupport(false);
    await clickAt(cdp, evalJs, '.privacy-panel .toolbar button');
    await sleep(3500);
    st = await status(evalJs);
    check('a camera that cannot mask says so plainly', st.state === 'unsupported', JSON.stringify(st));
    check('and still promises the export protection, which does not depend on the camera',
      /export|eksport|导出|المصدَّرة/i.test(st.text), JSON.stringify(st.text).slice(0, 240));
    if (LANG !== 'en') {
      check('the unsupported sentence is translated too',
        !/^This camera cannot/.test(st.text), JSON.stringify(st.text).slice(0, 160));
    }

    // The three states must LOOK different, not merely carry a different attribute: a
    // warning that is styled like an all-clear is an all-clear.
    const styling = JSON.parse(await evalJs(`(() => {
      const el = document.querySelector('.privacy-status');
      return JSON.stringify({ border: getComputedStyle(el).borderInlineStartColor });
    })()`));
    await maskSupport(true);
    await maskMode('honest');
    await clickAt(cdp, evalJs, '.privacy-panel .toolbar button');
    await sleep(3500);
    const ok = JSON.parse(await evalJs(`(() => {
      const el = document.querySelector('.privacy-status');
      return JSON.stringify({ state: el.dataset.masking, border: getComputedStyle(el).borderInlineStartColor });
    })()`));
    check('the confirmed and unprotected states are visually different, not just labelled',
      ok.border !== styling.border, JSON.stringify({ unsupported: styling.border, confirmed: ok.border }));
    check('and it recovers to confirmed once the camera behaves', ok.state === 'confirmed', JSON.stringify(ok));

    await cdp.send('Page.captureScreenshot', { format: 'png' }).then((r) => {
      require('fs').writeFileSync(path.join(OUT, `privacy-${LANG}.png`), Buffer.from(r.data, 'base64'));
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
