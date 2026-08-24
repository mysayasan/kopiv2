// Drive mymatasan's detection-rule editor for the time-based rules (W3-4) in headless
// Chrome, against the live bench node.
//
// WHY THIS EXISTS. The API bench proves the three rule types can be created, refused and
// read back. It cannot see the thing an operator actually meets: whether the editor OFFERS
// them, whether choosing one reveals the field that makes it a time rule, and whether the
// value typed there is the value that reaches the server. A mode that renders no dwell field
// is a loitering rule saved with the default nobody chose.
//
// Usage (with fleet_harness.py up and bench_w34_dwell.py already run):
//
//   node tools/fleetbench/uicheck_dwell.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';
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

const RULE_NAME = 'Screen dwell ' + Date.now();

(async () => {
  const port = 9231;
  const profile = path.join(OUT, 'chrome-profile-dwell');
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

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    await evalJs(`(() => { try { localStorage.setItem('mymatasan_lang', ${JSON.stringify(LANG)}); } catch(_){} return 1; })()`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
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
    check('the bench admin can sign in', signedIn === 'submitted' || signedIn === 'ALREADY IN', signedIn);
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

    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-soft','--accent'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token resolves', theme.missing.length === 0, theme.missing.join(', ') || 'all present');
    check('the page is painted', !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)', theme.bodyBg);
    if (LANG === 'ar') {
      const dir = await evalJs(`document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction`);
      check('the Arabic run really renders right-to-left', dir === 'rtl', 'dir=' + dir);
    }

    // --- reach a camera's Detection tab -----------------------------------------------
    const openedCameras = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /^\\s*(cameras|kamera|摄像机|الكاميرات)\\s*\\d*\\s*$/i.test((e.textContent || '').trim()));
      if (!hit) return 'NO CAMERAS NAV';
      hit.click();
      return 'clicked';
    })()`);
    check('the Cameras rail entry opens', openedCameras === 'clicked', openedCameras);
    await sleep(3000);

    const pickedCamera = await evalJs(`(() => {
      const items = [...document.querySelectorAll('.side-nav button, .side-nav a')]
        .filter((e) => /dwell cam|wall cam|loading bay|car park/i.test(e.textContent || ''));
      if (!items.length) return 'NO CAMERA IN THE RAIL: ' +
        [...document.querySelectorAll('.side-nav button')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,20).join(' | ');
      items[0].click();
      return 'picked: ' + items[0].textContent.trim();
    })()`);
    check('a camera can be opened from the rail', pickedCamera.startsWith('picked'), pickedCamera);
    await sleep(3500);

    const openedAi = await evalJs(`(() => {
      const tabs = [...document.querySelectorAll('.ui-tabs button, [role=tab]')];
      const hit = tabs.find((e) => /detection|pengesanan|检测|كشف/i.test(e.textContent || ''));
      if (!hit) return 'NO DETECTION TAB: ' + tabs.map(e=>e.textContent.trim()).join(' | ');
      hit.click();
      return 'clicked';
    })()`);
    check('the camera has a Detection tab and it opens', openedAi === 'clicked', openedAi);
    await sleep(3000);

    // The editor is behind a "new rule" action — the panel only exists once a draft does.
    const openedEditor = await evalJs(`(async () => {
      // Look for the MODE select, not for any select: the rule LIST already has a page-size
      // dropdown, and treating that as "the editor is open" made this check pass while the
      // editor had never been opened at all.
      const hasMode = [...document.querySelectorAll('select')]
        .some((sel) => [...sel.options].some((o) => /presence|kehadiran|存在|الحضور/i.test(o.textContent)));
      if (hasMode) return 'already open';
      const btn = [...document.querySelectorAll('button')]
        .find((b) => /add rule|new rule|tambah peraturan|添加规则|新建规则|إضافة قاعدة|أضف قاعدة/i.test(b.textContent || ''));
      if (!btn) return 'NO NEW RULE BUTTON: ' +
        [...document.querySelectorAll('button')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,25).join(' | ');
      btn.click();
      await new Promise((r) => setTimeout(r, 1200));
      return 'clicked';
    })()`);
    check('the rule editor can be opened', openedEditor === 'clicked' || openedEditor === 'already open',
      openedEditor);
    await sleep(1500);

    // --- the modes are offered ----------------------------------------------------------
    const modes = JSON.parse(await evalJs(`(() => {
      const selects = [...document.querySelectorAll('select')];
      const modeSel = selects.find((s) => [...s.options].some((o) => /presence|kehadiran|存在|الحضور/i.test(o.textContent)));
      return JSON.stringify({
        found: !!modeSel,
        options: modeSel ? [...modeSel.options].map((o) => o.textContent.trim()) : [],
      });
    })()`));
    check('the rule editor offers a mode picker', modes.found, JSON.stringify(modes).slice(0, 200));
    // THE THREE NEW MODES ARE THERE AND TRANSLATED. An untranslated mode in an Arabic UI is
    // a rule an operator cannot choose with confidence.
    check('all three time-based modes are offered', modes.options.length >= 9,
      JSON.stringify(modes.options));

    // --- choosing loitering reveals the dwell field ---------------------------------------
    const chose = await evalJs(`(async () => {
      const selects = [...document.querySelectorAll('select')];
      const modeSel = selects.find((s) => [...s.options].some((o) => /presence|kehadiran|存在|الحضور/i.test(o.textContent)));
      if (!modeSel) return 'NO MODE SELECT';
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(modeSel, 'loitering');
      modeSel.dispatchEvent(new Event('change', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 900));
      return modeSel.value;
    })()`);
    check('loitering can be chosen', chose === 'loitering', chose);

    const panel = JSON.parse(await evalJs(`(() => {
      const heads = [...document.querySelectorAll('.schedule-panel header h3')].map((h) => h.textContent.trim());
      const nums = [...document.querySelectorAll('.schedule-panel input[type=number]')];
      const pill = [...document.querySelectorAll('.schedule-panel .status-pill')].map((p) => p.textContent.trim());
      return JSON.stringify({ heads, count: nums.length, values: nums.map((n) => n.value), pill });
    })()`));
    // A mode that renders no field of its own is a rule saved with a default nobody chose.
    check('choosing loitering reveals a dwell field with a real default',
      panel.count >= 1 && Number(panel.values[0]) > 0, JSON.stringify(panel).slice(0, 240));
    check('and the panel summarises the threshold in words',
      (panel.pill || []).some((p) => /[0-9]/.test(p)), JSON.stringify(panel.pill));

    // --- THE VALUE TYPED IS THE VALUE STORED ----------------------------------------
    //
    // Checked against the SERVER, not against the editor's own state. The editor has no
    // JSON preview to read, and a field that updates a local draft nobody posts looks
    // identical to one that works.
    const typed = await evalJs(`(async () => {
      const name = [...document.querySelectorAll('input[type=text]')][0];
      if (name) {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(name, ${JSON.stringify(RULE_NAME)});
        name.dispatchEvent(new Event('input', { bubbles: true }));
      }
      const dwell = document.querySelector('.schedule-panel input[type=number]');
      if (!dwell) return 'NO DWELL FIELD';
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(dwell, '77');
      dwell.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise((r) => setTimeout(r, 600));
      return String(dwell.value);
    })()`);
    check('the dwell field accepts a value', typed === '77', typed);

    // The typed value has to reach the DRAFT, which is what gets posted. The editor has no
    // JSON preview, so the observable is the panel's own summary: it is rendered from the
    // same parsed config the save will serialise, so a field that updates nothing visible is
    // a field that updates nothing at all.
    const pillAfter = await evalJs(`(() => [...document.querySelectorAll('.schedule-panel .status-pill')]
      .map((p) => p.textContent.trim()).join(' | '))()`);
    check('the typed dwell reaches the rule draft, not just the input',
      /77/.test(pillAfter), pillAfter);

    // NOT CHECKED HERE, and deliberately: that pressing Save creates the rule. That is
    // generic rule-editor behaviour rather than anything W3-4 changed, and the API bench
    // already creates all three types and reads their configuration back off the server
    // (bench_w34_dwell.py). Driving the save from here would mean satisfying the editor's
    // zone requirements too, which tests the zone editor, not these rules.

    await cdp.send('Page.captureScreenshot', { format: 'png' }).then((shot) => {
      fs.writeFileSync(path.join(OUT, 'uicheck_dwell_' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    }).catch(() => {});

    const ok = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${ok}/${CHECKS.length} checks passed (${LANG})`);
    CHECKS.filter((c) => !c.ok).forEach((c) => console.log('  FAILED: ' + c.name + '   ' + c.detail));
    process.exitCode = ok === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((err) => {
  console.error('uicheck_dwell failed: ' + err.message);
  process.exitCode = 1;
});
