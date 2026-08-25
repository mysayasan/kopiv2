// Drive mymatasan's evidence-export dialog in headless Chrome (W3-6b), one language a run.
//
// WHY THIS IS NOT bench_w36b_faceredact.py AGAIN. That bench proves the pixels go black. This
// proves an OPERATOR is told the truth about them — which for this feature is most of the
// product:
//
//   * THE LIMIT MUST BE ON THE SCREEN, NEXT TO THE CONTROL. A privacy zone is a guarantee; a
//     face pass is a detector's best effort and misses faces that are turned away, distant,
//     partly hidden or motion-blurred. Somebody about to hand a file to a journalist has to
//     read that BEFORE they make it. A checkbox with no sentence beside it is the feature
//     lying by omission.
//   * ...AND AGAIN WHEN THE FILE IS HANDED OVER, beside the counts, because that is the
//     moment it matters and the moment a manifest nobody opens does not help.
//   * THE COUNTS MUST BE WHAT HAPPENED. "Faces obscured" with no frames scanned beside it is
//     unreadable; a count of detections presented as a count of people is worse.
//   * THE RTL DEFECT W3-5a AND W3-5b BOTH SHIPPED — a control that renders perfectly and
//     cannot be pressed. The checkbox is hit-tested with elementFromPoint at its own centre.
//   * THE REGRESSION W3-1 SHIPPED — every design token resolving to nothing while every
//     screen check passed. The tokens are read back out of the live page.
//
// It needs a camera with recorded footage on node-a: run bench_w36b_faceredact.py first, or
// seed one the same way.
//
//   node tools/fleetbench/uicheck_faceredact.js <output-dir> [lang] [password]
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
  const port = 9238;
  const profile = path.join(OUT, 'chrome-profile-faceredact-' + LANG);
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1600,1300', 'about:blank',
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

    // A REAL click at the control's own centre, and a read of what elementFromPoint returns
    // there. `.click()` succeeds on a control buried under another element — the defect that
    // shipped twice in this programme.
    const clickReal = async (selector) => {
      const box = JSON.parse(await evalJs(`(() => {
        const el = document.querySelector(${JSON.stringify(selector)});
        if (!el) return JSON.stringify({ missing: true });
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width / 2), y = Math.round(r.top + r.height / 2);
        const hit = document.elementFromPoint(x, y);
        return JSON.stringify({
          x, y, w: Math.round(r.width), h: Math.round(r.height), disabled: !!el.disabled,
          reaches: !!(hit && (hit === el || el.contains(hit) || hit.contains(el))),
          hitTag: hit ? hit.tagName : null,
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

    // ---- sign in, in the language under test ----------------------------------------
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
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done|langkau|lompat|跳过|略过|完成|تخطي|إنهاء/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      await sleep(2500);
    }
    await sleep(1500);

    // ---- the W3-1 regression guard ---------------------------------------------------
    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-subtle'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      });
    })()`));
    check('every design token the dialog relies on resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted', !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)',
      'body background = ' + theme.bodyBg);

    // ---- reach the export dialog the way a person would ------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const cams = [...rail.querySelectorAll('button, a')]
        .filter((e) => /camera|kamera|摄像|الكاميرات/i.test(e.textContent || ''));
      if (!cams.length) return 'NO CAMERAS NAV';
      cams[0].click();
      return 'clicked';
    })()`);
    await sleep(2500);
    const picked = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      // The camera tree lists the cameras under the Cameras entry; the bench leaves exactly
      // one behind.
      const items = [...rail.querySelectorAll('button, a')].filter((e) => /Reception|Lobby/i.test(e.textContent || ''));
      if (!items.length) return 'NO CAMERA IN THE TREE';
      items[items.length - 1].click();
      return 'clicked: ' + items[items.length - 1].textContent.trim();
    })()`);
    check('a camera with footage can be reached from the rail', /^clicked/.test(picked), nav + ' / ' + picked);
    await sleep(3000);

    const openedTab = await evalJs(`(() => {
      const bar = document.querySelector('.ui-tabs[role=tablist]');
      const tabs = bar ? [...bar.querySelectorAll('[role=tab]')] : [];
      const hit = tabs.find((e) => /recording|rakaman|录像|التسجيل/i.test(e.textContent || ''));
      if (!hit) return 'NO RECORDINGS TAB: ' + tabs.map((e) => e.textContent.trim()).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    await sleep(2500);
    const openedDialog = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('button')]
        .find((e) => /export footage|eksport rakaman|导出录像|تصدير التسجيل/i.test(e.textContent || ''));
      if (!hit) return 'NO EXPORT BUTTON';
      hit.click();
      return 'clicked';
    })()`);
    check('the export dialog opens', openedDialog === 'clicked', openedTab + ' / ' + openedDialog);
    await sleep(2000);

    // ---- THE CONTROL, AND THE SENTENCE BESIDE IT --------------------------------------
    const box = await clickReal('input[data-ev=blurFaces]');
    check('the hide-faces control exists and a click at its centre reaches it',
      box && !box.missing && box.reaches, JSON.stringify(box));
    await sleep(800);

    const shown = JSON.parse(await evalJs(`(() => {
      const cb = document.querySelector('input[data-ev=blurFaces]');
      const row = cb ? cb.closest('label') : null;
      const text = document.body.innerText || '';
      const hints = [...document.querySelectorAll('.field-hint')].map((e) => e.textContent.trim());
      return JSON.stringify({
        checked: !!(cb && cb.checked),
        label: row ? row.textContent.trim() : null,
        hints,
        // The limitation has to be VISIBLE text, not a title attribute nobody hovers.
        rawKeys: [...new Set(text.match(/\\bev\\.[a-zA-Z0-9_.]+/g) || [])],
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
      });
    })()`));
    check('ticking it actually ticks it', shown.checked, JSON.stringify(shown.checked));
    // THE ASSERTION THE WHOLE SCREEN EXISTS FOR: the limit is rendered next to the control,
    // as visible text.
    const limitHint = (shown.hints || []).find((h) =>
      /best effort|not a guarantee|usaha terbaik|bukan jaminan|尽力而为|不是保证|أقصى جهد|لا ضمان/i.test(h));
    check('the limit is stated NEXT TO the control, in visible text', !!limitHint,
      (limitHint || '(none)').slice(0, 140));
    check('...and it names the cases a detector misses',
      !!limitHint && /turned away|profile|berpaling|侧脸|مُدار جانبًا|جانبًا/i.test(limitHint),
      (limitHint || '').slice(0, 160));
    const slowHint = (shown.hints || []).find((h) => /every frame|setiap bingkai|逐帧|كل إطار/i.test(h));
    check('the operator is warned it is slow before they start it', !!slowHint,
      (slowHint || '(none)').slice(0, 120));
    check('no untranslated key reached the dialog', (shown.rawKeys || []).length === 0,
      (shown.rawKeys || []).join(', ') || 'none');

    // ---- run a real export from the browser -------------------------------------------
    // STABLE HANDLES, not translated labels. The Arabic run of this check reached the dialog
    // and then silently did nothing, because the regexes that matched "Check" and the build
    // button in English matched neither in Arabic — so it reported "no result panel" for a
    // feature that was never asked to run. Naming the controls is the same fix W3-7 landed
    // on for exactly the same reason.
    const filled = await evalJs(`(() => {
      const el = document.querySelector('input[data-ev=reason]');
      if (!el) return 'NO REASON FIELD';
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, 'screen check disclosure copy');
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return 'ok';
    })()`);
    check('the export reason can be typed', filled === 'ok', filled);
    await sleep(500);
    const checked = await clickReal('button[data-ev=check]');
    check('the range can be checked before building', checked && checked.reaches && !checked.disabled,
      JSON.stringify(checked));
    await sleep(5000);
    const built = await clickReal('button[data-ev=build]');
    check('the build control is enabled and reachable once the range checks out',
      built && built.reaches && !built.disabled, JSON.stringify(built));

    // The face pass reads every frame, so this is genuinely slow — the dialog polls.
    let result = null;
    for (let i = 0; i < 60; i++) {
      await sleep(5000);
      result = JSON.parse(await evalJs(`(() => {
        const panel = document.querySelector('[data-ev=faceResult]');
        const alerts = [...document.querySelectorAll('.form-alert, [role=alert]')].map((e) => e.textContent.trim());
        return JSON.stringify({
          panel: panel ? panel.innerText.trim() : null,
          alerts,
        });
      })()`));
      if (result.panel) break;
    }
    check('the finished export reports the face pass ON THE SCREEN', !!(result && result.panel),
      JSON.stringify(result).slice(0, 300));
    if (result && result.panel) {
      // Counts that mean something: frames scanned beside faces found, and the reader told
      // that a detection is not a person.
      check('it says how many frames were scanned, not just that it ran',
        /[0-9]/.test(result.panel), result.panel.slice(0, 160));
      check('it repeats the limit at the moment the file is handed over',
        /may still be visible|mungkin masih|仍可能|قد تظل/i.test(result.panel + ' ' + result.alerts.join(' ')),
        result.panel.slice(0, 200));
      check('...and warns that a count of detections is not a count of people',
        /not of people|bukan kiraan orang|不是人数|لا عدد الأشخاص/i.test(result.panel),
        result.panel.slice(0, 200));
    }

    if (LANG !== 'en') {
      const ascii = /^[\x20-\x7e\s]*$/.test((limitHint || '') + ((result && result.panel) || ''));
      check('the page really switched language', !ascii,
        ((limitHint || '') + ' | ' + ((result && result.panel) || '')).slice(0, 160));
      if (LANG === 'ar') {
        check('Arabic puts the dialog in RTL', shown.dir === 'rtl', 'dir=' + shown.dir);
      }
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'faceredact-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'faceredact-' + LANG + '.png'));
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
