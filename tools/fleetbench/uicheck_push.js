// Drive myseliasan's "Alerts on this device" panel in headless Chrome (W3-9), one language
// per run.
//
// WHY THIS IS NOT bench_w39_push.py AGAIN. That bench proves the appliance really encrypts,
// really POSTs, and really reacts to what a push service answers. This proves an OPERATOR is
// told the truth about it — which is a different claim, and the one this whole feature is:
//
//   * THE VERDICT IS THE PRODUCT. An install with no route to a push service must SAY SO, in
//     the operator's own language, in a sentence that does not read as a fault to chase. The
//     check seeds devices that cannot be reached and then reads what the screen says about
//     them, so a panel that painted "notifications: on" would fail here and nowhere else.
//   * THE ACTIONS MUST BE PRESSABLE. W3-5a, W3-5b and W3-6b each shipped a control that
//     rendered perfectly and could not be clicked, in Arabic only. Every action is hit-tested
//     with document.elementFromPoint AT ITS OWN CENTRE and asserted to be inside its own row.
//   * AND THEY MUST DO SOMETHING. Pressing Test has to produce a NEW attempt, and pressing
//     Remove has to remove the row — read back from the server, not from the DOM that the
//     click just re-rendered.
//   * A REFUSAL MUST BE VISIBLE. Headless Chrome cannot really subscribe to a push service.
//     That is the same position as a browser on a locked-down phone, and pressing the button
//     there must produce a translated sentence rather than nothing at all.
//   * THE REGRESSION W3-1 SHIPPED — a build in which every design token resolved to nothing
//     and every screen check still passed. The tokens are read back out of the live page.
//   * THE SERVICE WORKER REGISTERS. Serving /sw.js with the right content type is checked by
//     the API bench; that a real browser will actually install it at the root scope is only
//     answerable from a real browser.
//
// Usage (with fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_push.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

// Endpoints that fail INSTANTLY rather than after a twenty-second timeout: port 9 on the
// appliance's own loopback refuses the connection outright. The row that results is the one
// an air-gapped operator sees, which is the screen this check exists to look at.
const SEED = [
  { path: '/push/screen-phone', label: 'Screen phone', min: 'critical' },
  { path: '/push/screen-laptop', label: 'Screen laptop', min: 'info' },
];

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
        setTimeout(() => rej(new Error(method + ' timed out')), 120000);
      });
    },
  };
}

(async () => {
  const port = 9238;
  const profile = path.join(OUT, 'chrome-profile-push-' + LANG);
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1500,1200', 'about:blank',
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
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };

    // A REAL click: a mouse event dispatched at the control's own centre, then a read of what
    // document.elementFromPoint returns there. `.click()` succeeds on a control buried under
    // another element, which is exactly the defect that has now shipped three times.
    const clickIn = async (rowSelector, act) => {
      const sel = JSON.stringify(rowSelector + ' [data-push=' + JSON.stringify(act).slice(1, -1) + ']');
      const box = JSON.parse(await evalJs(`(() => {
        const el = document.querySelector(${sel});
        if (!el) return JSON.stringify({ missing: true });
        el.scrollIntoView({ block: 'center' });
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width / 2), y = Math.round(r.top + r.height / 2);
        const hit = document.elementFromPoint(x, y);
        const row = el.closest('.push-device') || el.closest('.push-panel');
        const rr = row ? row.getBoundingClientRect() : null;
        return JSON.stringify({
          x, y, w: Math.round(r.width), h: Math.round(r.height),
          disabled: !!el.disabled,
          reaches: !!(hit && (hit === el || el.contains(hit))),
          hitTag: hit ? hit.tagName + (typeof hit.className === 'string' && hit.className ? '.' + hit.className.split(' ')[0] : '') : null,
          // A logical inset inside a physically-anchored box renders perfectly and lands
          // outside its own row once the page is mirrored.
          insideRow: rr ? (r.left >= rr.left - 1 && r.right <= rr.right + 1 && r.top >= rr.top - 1 && r.bottom <= rr.bottom + 1) : null,
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

    // ---- sign in, in the language under test --------------------------------------------
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);
    const login = await evalJs(`(async () => {
      const r = await fetch('/api/auth/local-login', {
        method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({username:'admin', password:${JSON.stringify(PASSWORD)}}),
      });
      return r.status;
    })()`);
    check('the bench admin can sign in', login === 200, 'status ' + login);

    // ---- a known state: exactly the two devices this check seeds ---------------------------
    const seeded = JSON.parse(await evalJs(`(async () => {
      const csrf = decodeURIComponent((document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1] || '');
      const h = { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf };
      const list = async () => {
        const b = await (await fetch('/api/push/devices', { credentials: 'same-origin' })).json();
        return (b?.result?.items) || (b?.data?.result?.items) || [];
      };
      for (const d of await list()) {
        await fetch('/api/push/devices/' + d.id, { method: 'DELETE', credentials: 'same-origin', headers: h });
      }
      const made = [];
      for (const s of ${JSON.stringify(SEED)}) {
        const r = await fetch('/api/push/devices', {
          method: 'POST', credentials: 'same-origin', headers: h,
          body: JSON.stringify({
            endpoint: 'https://127.0.0.1:9' + s.path,
            p256dh: 'BKJojqbb7aQGaXYYQARTwFh792CxoPZWFEiEKapRwo9sO6mWv-3VFgAORRIzaYgl8jzR4A8KbTGeWAzgiArebqo',
            auth: 'ytBPrsxcRCEmq6x5dGIh0g',
            label: s.label, minSeverity: s.min,
          }),
        });
        made.push(r.status);
      }
      return JSON.stringify({ made, count: (await list()).length });
    })()`));
    check('two devices could be seeded for the screen to render',
      seeded.count === 2, JSON.stringify(seeded));

    // THE KEY MUST BE THE APP'S OWN. A made-up key changes nothing and the run reports
    // "renders in ar" from an English page.
    await evalJs(`localStorage.setItem('myseliasan_lang', ${JSON.stringify(LANG)}), 1`);
    // Notifications granted up front, so pressing "Turn on" gets past the browser prompt and
    // reaches the part that can actually fail — which is the part worth looking at.
    await cdp.send('Browser.grantPermissions', { origin: BASE, permissions: ['notifications'] }).catch(() => {});
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

    // ---- the W3-1 regression guard ---------------------------------------------------------
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

    // ---- the service worker, from a real browser -------------------------------------------
    const sw = JSON.parse(await evalJs(`(async () => {
      try {
        const reg = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
        await navigator.serviceWorker.ready;
        return JSON.stringify({ ok: true, scope: reg.scope, secure: window.isSecureContext });
      } catch (e) {
        return JSON.stringify({ ok: false, err: String(e && e.message || e), secure: window.isSecureContext });
      }
    })()`));
    check('a real browser installs the service worker at the ROOT scope, which is what lets it '
      + 'show a notification for any page of the app',
      sw.ok && /\/$/.test(sw.scope || ''), JSON.stringify(sw));

    const manifest = JSON.parse(await evalJs(`(async () => {
      const link = document.querySelector('link[rel="manifest"]');
      if (!link) return JSON.stringify({ linked: false });
      const r = await fetch(link.href, { credentials: 'same-origin' });
      const b = await r.json().catch(() => null);
      return JSON.stringify({ linked: true, status: r.status, name: b && b.name, icons: (b && b.icons || []).length });
    })()`));
    check('the page links a manifest the browser can actually fetch',
      manifest.linked && manifest.status === 200 && !!manifest.name && manifest.icons >= 2,
      JSON.stringify(manifest));

    // ---- reach the screen the way a person would --------------------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /notification|pemberitahuan|通知|الإشعارات|إشعارات/i.test(e.textContent || ''));
      if (!hit) return 'NO NOTIFICATIONS NAV: ' + [...rail.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Notifications entry is in the nav and reaches the screen', /^clicked:/.test(nav), nav);
    await sleep(3000);

    // ---- what the panel actually says --------------------------------------------------------
    const panel = JSON.parse(await evalJs(`(() => {
      const p = document.querySelector('[data-push=panel]');
      if (!p) return JSON.stringify({ missing: true });
      const txt = (sel) => { const e = p.querySelector(sel); return e ? e.textContent.trim() : null; };
      const rows = [...p.querySelectorAll('[data-push=device]')].map((e) => ({
        id: e.getAttribute('data-push-device'),
        outcome: e.getAttribute('data-push-outcome'),
        outcomeText: (e.querySelector('.push-device-outcome') || {}).textContent || '',
        meta: (e.querySelector('.push-device-meta') || {}).textContent || '',
        label: (e.querySelector('.push-device-label') || {}).textContent || '',
      }));
      return JSON.stringify({
        delivery: p.getAttribute('data-push-delivery'),
        browserState: p.getAttribute('data-push-state'),
        verdict: txt('[data-push=verdict]'),
        vendors: txt('[data-push=vendors]'),
        privacy: txt('[data-push=privacy]'),
        rows,
        rawKeys: [...new Set((p.innerText || '').match(/\\bpush\\.[a-zA-Z0-9_.-]+/g) || [])],
        overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
      });
    })()`));
    check('the panel is on the Notifications screen, where the person who needs waking will '
      + 'be looking', !panel.missing, JSON.stringify(panel).slice(0, 120));

    // THE ONE THAT MATTERS. Everything reachable from this appliance refused the connection,
    // so the screen must say the push service could not be REACHED — the sentence that stops
    // an operator on an intranet site from hunting a bug that is not there.
    check('an install that cannot reach a push service says so, rather than reporting itself on',
      panel.delivery === 'unreachable', 'delivery=' + panel.delivery);
    check('and it says it in a sentence, not as a state token',
      !!panel.verdict && panel.verdict.length > 25 && !/^[a-z-]+$/.test(panel.verdict),
      (panel.verdict || '').slice(0, 110));
    check('it names the hosts a firewall would have to allow', !!panel.vendors,
      (panel.vendors || '').slice(0, 90));
    // Stated on the SCREEN, not only in the manual: turning this on means the appliance starts
    // talking to a company outside the building.
    check('what leaves the building is stated beside the control', !!panel.privacy,
      (panel.privacy || '').slice(0, 100));
    check('no untranslated key reached the screen', (panel.rawKeys || []).length === 0,
      (panel.rawKeys || []).join(', ') || 'none');
    check('both seeded devices are listed', (panel.rows || []).length === 2,
      JSON.stringify((panel.rows || []).map((r) => r.label)));
    check('each row says what actually happened, in words',
      (panel.rows || []).every((r) => r.outcome === 'unreachable' && r.outcomeText.trim().length > 8),
      JSON.stringify((panel.rows || []).map((r) => r.outcome + ': ' + r.outcomeText.trim().slice(0, 40))));
    check('and which service it would be reached through',
      (panel.rows || []).every((r) => /127\.0\.0\.1:9/.test(r.meta)),
      JSON.stringify((panel.rows || []).map((r) => r.meta.trim().slice(0, 60))));
    check('the page does not scroll sideways', panel.overflow <= 1, 'overflow=' + panel.overflow);

    // ---- the actions: pressable, and they do something -----------------------------------------
    const first = (panel.rows || [])[0];
    const rowSel = first ? `[data-push-device="${first.id}"]` : '[data-push=device]';

    const attemptOf = async (id) => evalJs(`(async () => {
      const b = await (await fetch('/api/push/devices', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      const d = items.find((x) => String(x.id) === ${JSON.stringify(String(first ? first.id : ''))});
      return d ? String(d.lastAttemptAt) : 'GONE';
    })()`);

    const beforeAttempt = await attemptOf();
    const testBox = await clickIn(rowSel, 'test');
    check('the Test control can be pressed at its own centre',
      testBox.reaches === true && testBox.insideRow !== false, JSON.stringify(testBox));
    // A toast lives 3.5 seconds. Reading it after a four-second wait finds an empty stack and
    // reports a silent screen on one that spoke — the same class of mistake as a check that
    // passes on broken output, wearing the other hat.
    await sleep(2200);
    const toast = await evalJs(`(() => {
      const t = [...document.querySelectorAll('.toast-text')].map((e) => e.textContent.trim()).filter(Boolean);
      return t.join(' | ');
    })()`);
    check('and it says out loud what the attempt found', !!toast, (toast || '').slice(0, 110));
    await sleep(2000);
    const afterAttempt = await attemptOf();
    // A button that renders and is clickable but changes nothing on the server is the same
    // failure as one that cannot be clicked — it just looks better.
    check('pressing Test really makes a new attempt, and the server records it',
      afterAttempt !== 'GONE' && afterAttempt !== beforeAttempt,
      beforeAttempt + ' -> ' + afterAttempt);

    // Remove: the verb the API bench never pressed through the screen.
    const beforeCount = (panel.rows || []).length;
    await evalJs(`window.confirm = () => true, 1`);
    const removeBox = await clickIn(rowSel, 'remove');
    check('the Remove control can be pressed at its own centre',
      removeBox.reaches === true && removeBox.insideRow !== false, JSON.stringify(removeBox));
    await sleep(3500);
    const afterRemove = JSON.parse(await evalJs(`(async () => {
      const b = await (await fetch('/api/push/devices', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      const shown = document.querySelectorAll('[data-push=device]').length;
      return JSON.stringify({ server: items.length, shown });
    })()`));
    check('pressing Remove really removes the device, on the server and on the screen',
      afterRemove.server === beforeCount - 1 && afterRemove.shown === beforeCount - 1,
      JSON.stringify(afterRemove) + ' (was ' + beforeCount + ')');

    // ---- a refusal must be visible ---------------------------------------------------------------
    // Headless Chrome has no push service, so subscribing here fails — the same position as a
    // browser on a locked-down phone. Silence would be the worst possible answer.
    await evalJs(`[...document.querySelectorAll('.toast-close')].forEach((b) => b.click()), 1`);
    await sleep(800);
    const enableBox = await clickIn('.push-panel', 'enable');
    if (enableBox.missing) {
      check('the enrol control is offered when the browser could take it', false,
        'no [data-push=enable] rendered; state=' + panel.browserState);
    } else {
      check('the enrol control can be pressed at its own centre',
        enableBox.reaches === true, JSON.stringify(enableBox));
      await sleep(6000);
      const answer = JSON.parse(await evalJs(`(() => {
        const p = document.querySelector('[data-push=panel]');
        return JSON.stringify({
          toasts: [...document.querySelectorAll('.toast-text')].map((e) => e.textContent.trim()).filter(Boolean),
          state: p ? p.getAttribute('data-push-state') : null,
        });
      })()`));
      // Either it enrolled (some browsers can) or it refused — but it must SAY which.
      check('a browser that cannot be enrolled is told so, out loud, rather than nothing '
        + 'happening at all',
        (answer.toasts || []).length > 0 || answer.state === 'enabled',
        JSON.stringify(answer).slice(0, 200));
      if ((answer.toasts || []).length && LANG !== 'en') {
        check('and the refusal is in the operator\'s language',
          answer.toasts.some((t) => !/^[\x20-\x7e]*$/.test(t)),
          JSON.stringify(answer.toasts).slice(0, 160));
      }
    }

    if (LANG !== 'en') {
      const labels = await evalJs(`(() => {
        const p = document.querySelector('[data-push=panel]');
        return p ? JSON.stringify([...p.querySelectorAll('h3, .push-btn span')].map((e) => e.textContent.trim()).filter(Boolean)) : '[]';
      })()`);
      const parsed = JSON.parse(labels);
      check('the panel really switched language', parsed.some((l) => !/^[\x20-\x7e]*$/.test(l)),
        JSON.stringify(parsed).slice(0, 160));
      if (LANG === 'ar') {
        check('Arabic puts the page in RTL', panel.dir === 'rtl', 'dir=' + panel.dir);
      }
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'push-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'push-' + LANG + '.png'));
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
