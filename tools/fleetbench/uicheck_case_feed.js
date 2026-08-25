// Drive "add this feed entry to a case" on mymatasan's Notifications screen (W3-3c),
// one language per run.
//
// W3-3a shipped with the case dialog claiming to be "shared by every screen that can produce
// evidence — the timeline, the object grid, the alert log". Only the timeline used it. This is
// the alert-log half, and this check is what makes the claim true rather than aspirational.
//
// What it proves:
//
//   * THE BUTTON IS ON THE ROW WHERE SOMEBODY NOTICES SOMETHING, and it can be pressed at its
//     own centre — the RTL defect W3-5a, W3-5b and W3-6b each shipped.
//   * PRESSING IT REALLY PUTS THE ENTRY IN A CASE, read back from the server rather than from
//     the DOM the click just re-rendered — and with the PROVENANCE, so the case says which
//     feed row it came from.
//   * THE OPERATOR'S NOTE TRAVELS. That is the half that turns a pile of rows into an
//     argument, and a dialog that quietly dropped it would look identical.
//   * THE W3-1 REGRESSION GUARD: tokens read back out of the live page.
//
// Usage (with fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_case_feed.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

// Unique per run, so a re-run cannot pass by finding the case the LAST run made. A screen
// check that can be satisfied by stale state is not one.
const TITLE = 'Feed case ' + Date.now();
const NOTE = 'noted from the feed ' + Date.now();

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
  const port = 9244;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-profile-casefeed-' + LANG)}`,
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

    // A REAL click: dispatched at the control's own centre, after checking what
    // document.elementFromPoint returns there. `.click()` succeeds on a control buried under
    // another element, which is the defect that has shipped three times.
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

    // ---- sign in, in the language under test ------------------------------------------
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    await evalJs(`(() => { try { localStorage.setItem('mymatasan_lang', ${JSON.stringify(LANG)}); } catch(_){} return 1; })()`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);
    const signedIn = await evalJs(`(() => {
      const user = document.querySelector('input[name=username], input[autocomplete=username], input[type=text]');
      const pass = document.querySelector('input[type=password]');
      if (!user || !pass) return 'NO LOGIN FORM';
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
    check('the bench admin can sign in', signedIn === 'submitted', signedIn);
    await sleep(6000);
    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done|langkau|跳过|完成|تخطّي|تخطي|إنهاء/i.test(e.textContent || ''));
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

    // ---- reach the Notifications screen the way a person would ---------------------------
    const nav = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('a, button')]
        .find((e) => /^\\s*(notifications|pemberitahuan|通知|الإشعارات|إشعارات)/i.test((e.textContent || '').trim()));
      if (!hit) return 'NO NAV: ' + [...document.querySelectorAll('a,button')].map((e) => e.textContent.trim()).filter(Boolean).slice(0, 25).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Notifications entry is in the nav and reaches the screen', /^clicked:/.test(nav), nav);
    await sleep(3500);

    const rows = await evalJs(`(() => document.querySelectorAll('[data-notif-act=case]').length)()`);
    check('every feed row offers to put itself into a case — the screen where somebody '
      + 'notices something is the screen the button belongs on', rows > 0, rows + ' rows');

    // Which entry is being added? Read it off the row so the assertion afterwards is about
    // THAT entry rather than about "a case got something".
    const target = JSON.parse(await evalJs(`(() => {
      const btn = document.querySelector('[data-notif-act=case]');
      const row = btn ? btn.closest('article') : null;
      const strong = row ? row.querySelector('strong') : null;
      return JSON.stringify({ title: strong ? strong.textContent.trim() : '' });
    })()`));

    const opened = await clickSel('[data-notif-act=case]', 'article');
    check('the Add-to-case control can be pressed at its own centre',
      opened.reaches === true && opened.inside !== false, JSON.stringify(opened));
    await sleep(1500);
    const dialog = await evalJs(`(() => !!document.querySelector('.case-dialog'))()`);
    check('and it opens the case dialog', dialog === true);

    // ---- make a NEW case through the dialog, and add the entry to it ----------------------
    // Choose "a new case" FIRST. The title field only exists when no existing case is
    // selected, so checking for it before making that choice reports "no text input" on a
    // run that happens to find a case left by an earlier one — a check that fails on
    // correct output, which wastes a run exactly like one that passes on broken output.
    await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      const sel = d ? d.querySelector('select') : null;
      if (sel) {
        const blank = [...sel.options].find((o) => !Number(o.value));
        if (blank) {
          Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(sel, blank.value);
          sel.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }
      return 1;
    })()`);
    await sleep(800);
    const titleInput = await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      if (!d) return 'NO DIALOG';
      const inputs = [...d.querySelectorAll('input[type=text], input:not([type])')];
      return inputs.length ? 'ok:' + inputs.length : 'NO TEXT INPUT';
    })()`);
    check('the dialog offers a new case as well as the open ones', /^ok:/.test(titleInput), titleInput);

    const setTitle = await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      const inputs = [...d.querySelectorAll('input[type=text], input:not([type])')];
      if (!inputs.length) return 'MISSING';
      const el = inputs[0];
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(TITLE)});
      el.dispatchEvent(new Event('input', { bubbles: true }));
      return el.value;
    })()`);
    check('a new case can be named in the dialog', setTitle === TITLE, setTitle);

    const setNote = await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      const ta = d.querySelector('textarea');
      if (!ta) return 'NO NOTE FIELD';
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(ta, ${JSON.stringify(NOTE)});
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      return ta.value;
    })()`);
    check('and a note typed against it — the half that turns a pile of rows into an argument',
      setNote === NOTE, setNote);

    // The dialog must SAY what it is about to file. An empty list above the note is a dialog
    // asking the operator to confirm something it will not name.
    const named = await evalJs(`(() => {
      const el = document.querySelector('[data-case-evidence=notification]');
      return el ? el.textContent.trim() : '';
    })()`);
    check('the dialog names the entry it is about to file', !!named, named);

    // A REAL press, at the button's own centre. `.click()` would have reported success on a
    // button that was DISABLED — which is exactly what it was, because the guard only counted
    // the timeline's `items` and a feed entry carries none.
    const submitted = await clickSel('[data-case-act=add]', '.case-dialog');
    check('the Add button is enabled and can be pressed at its own centre',
      submitted.reaches === true && submitted.disabled === false, JSON.stringify(submitted));
    await sleep(4000);
    // A dialog that stayed open is a dialog that failed, and its own error text is a better
    // diagnosis than anything this check could infer from an empty list afterwards.
    const dialogAfter = await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      if (!d) return '';
      // Whatever the dialog is saying, verbatim. Guessing the error element's class is how
      // a diagnosis turns into "STILL OPEN" and nothing else.
      return 'STILL OPEN: ' + (d.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 300);
    })()`);
    check('and it closes, which is the only sign the operator gets that it worked',
      dialogAfter === '', dialogAfter);

    // ---- read it back FROM THE SERVER -----------------------------------------------------
    // Not from the DOM the click just re-rendered: a dialog that closed cheerfully having
    // stored nothing looks exactly the same.
    // mymatasan authenticates with BASIC, held in React state rather than in a cookie, so a
    // plain same-origin fetch from the page carries nothing and answers 401 with an empty
    // list — which looks exactly like "the case was never created". The header goes in
    // explicitly, the way the SPA's own client does.
    const stored = JSON.parse(await evalJs(`(async () => {
      const h = { Authorization: 'Basic ' + btoa('admin:' + ${JSON.stringify(PASSWORD)}) };
      const res = await fetch('/api/cases?limit=100', { headers: h });
      const raw = await res.text();
      if (!res.ok) return JSON.stringify({ found: false, status: res.status, raw: raw.slice(0, 200) });
      const list = JSON.parse(raw);
      const cases = (list?.result?.cases) || (list?.data?.result?.cases) || [];
      const mine = cases.find((c) => c.title === ${JSON.stringify(TITLE)});
      if (!mine) return JSON.stringify({ found: false, titles: cases.map((c) => c.title).slice(0, 5), raw: raw.slice(0, 200) });
      const d = await (await fetch('/api/cases/' + mine.id, { headers: h })).json();
      const detail = (d?.result) || (d?.data?.result) || {};
      return JSON.stringify({ found: true, id: mine.id, items: detail.items || [], hold: detail.hold || {} });
    })()`));
    check('the case the operator named really exists on the server', stored.found === true,
      JSON.stringify(stored).slice(0, 200));
    const item = (stored.items || [])[0];
    check('and the feed entry is in it', !!item, JSON.stringify(stored.items || []).slice(0, 200));
    if (item) {
      check('with the provenance that says which feed row it came from',
        Number(item.sourceId) > 0, 'sourceId=' + item.sourceId);
      check('and the note the operator typed, which is what a dialog silently dropping it '
        + 'would look identical without', item.note === NOTE, JSON.stringify(item.note));
      check('labelled with what the operator actually saw on the row',
        !!item.label && (!target.title || item.label === target.title),
        'row said ' + JSON.stringify(target.title) + ', item says ' + JSON.stringify(item.label));
    }

    const layout = JSON.parse(await evalJs(`(() => JSON.stringify({
      overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
      labels: [...document.querySelectorAll('[data-notif-act=case]')].map((e) => e.textContent.trim()).slice(0, 3),
    }))()`));
    check('the page does not scroll sideways', layout.overflow <= 1, 'overflow=' + layout.overflow);
    if (LANG !== 'en') {
      check('the button really switched language',
        layout.labels.some((l) => !/^[\x20-\x7e]*$/.test(l)), JSON.stringify(layout.labels));
      if (LANG === 'ar') check('Arabic puts the page in RTL', layout.dir === 'rtl', 'dir=' + layout.dir);
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'casefeed-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'casefeed-' + LANG + '.png'));
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
