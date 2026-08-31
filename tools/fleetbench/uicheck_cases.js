// Drive mymatasan's Cases screen in headless Chrome against the live bench node.
//
// WHY THIS EXISTS. Every screen-bearing item in this programme that shipped a browser pass
// found a defect the API bench could not: a label that lied about recording, a hint that
// listed every transport except the one being configured, a scrub bar that seeked five per
// cent of the window late. A green API and a screen that lies look identical from the API
// side.
//
// What can only fail in a browser here:
//
//   * the FOOTAGE HOLD is a claim made on this screen, in a sentence with a number in it.
//     If the panel renders "Holding 0 clips" over a case that is holding two, the operator
//     closes it and the evidence goes. The API bench cannot see that sentence.
//   * "Add to case" from the Timeline crosses two screens and posts several items at once.
//   * the close dialog's warning is the last thing between an operator and released
//     footage, and it is generated from numbers this page fetched.
//   * an export is start, poll, download — three requests the SPA drives itself.
//
// Usage (with fleet_harness.py up and bench_w33_cases.py already run):
//
//   node tools/fleetbench/uicheck_cases.js <output-dir> [lang] [password]
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

// TITLE is unique per run so a re-run against the same node cannot pass by finding the
// case the LAST run made. A screen check that can be satisfied by stale state is not one.
const TITLE = 'Screen case ' + Date.now();

(async () => {
  const port = 9227;
  const profile = path.join(OUT, 'chrome-profile-cases');
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
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      await sleep(2500);
    }
    await sleep(1500);

    // The stylesheet check W3-1's regression earned. Every assertion below is on DOM text,
    // and DOM text is exactly what a destroyed token block does not change.
    const theme = JSON.parse(await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const body = getComputedStyle(document.body);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-soft','--accent'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: body.backgroundColor,
      });
    })()`));
    check('every design token the case screen uses resolves', theme.missing.length === 0,
      theme.missing.join(', ') || 'all present');
    check('the page is actually painted',
      !!theme.bodyBg && theme.bodyBg !== 'rgba(0, 0, 0, 0)' && theme.bodyBg !== 'transparent',
      'body background = ' + theme.bodyBg);

    const dir = await evalJs(`document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction`);
    if (LANG === 'ar') {
      check('the Arabic run really renders right-to-left', dir === 'rtl', 'dir=' + dir);
    }

    // --- reach the screen ---------------------------------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const items = [...rail.querySelectorAll('button, a')];
      const hit = items.find((e) => /^\\s*(cases|kes|案件|القضايا)\\s*$/i.test((e.textContent || '').trim()));
      if (!hit) return 'NO CASES NAV: ' + items.map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Cases entry is in the navigation and opens', nav.startsWith('clicked'), nav);
    await sleep(2500);

    const empty = JSON.parse(await evalJs(`(() => JSON.stringify({
      panel: !!document.querySelector('.cases-page'),
      newBtn: !!document.querySelector('[data-case-act=new]'),
      detail: (document.querySelector('.cases-detail')||{}).textContent || '',
      error: (document.querySelector('.form-alert-msg')||{}).textContent || null,
    }))()`));
    check('the case screen rendered', empty.panel, JSON.stringify(empty).slice(0, 200));
    check('it did not fall back to an error banner', !empty.error, String(empty.error));
    // "Pick a case" and "no cases" are DIFFERENT statements and a blank panel is neither.
    check('with nothing selected the screen says so rather than showing a blank panel',
      (empty.detail || '').trim().length > 0, JSON.stringify(empty.detail).slice(0, 120));

    // --- open a case through the UI -------------------------------------------------
    const created = await evalJs(`(async () => {
      const btn = document.querySelector('[data-case-act=new]');
      if (!btn) return 'NO NEW BUTTON';
      btn.click();
      await new Promise(r => setTimeout(r, 400));
      const input = document.querySelector('.cases-new input');
      if (!input) return 'NO TITLE FIELD';
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, ${JSON.stringify(TITLE)});
      input.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise(r => setTimeout(r, 200));
      // The primary action is LAST in the form, after the quiet Cancel — same order as
      // every dialog on this screen. Taking [0] would press Cancel and report it as a save.
      const save = [...document.querySelectorAll('.cases-new button')].pop();
      if (!save || save.disabled) return 'SAVE DISABLED';
      save.click();
      return 'saved';
    })()`);
    check('a case can be opened from the screen', created === 'saved', created);
    await sleep(2500);

    const listed = JSON.parse(await evalJs(`(() => JSON.stringify({
      titles: [...document.querySelectorAll('.cases-list-title')].map(e => e.textContent.trim()),
      selected: (document.querySelector('.cases-detail h4')||{}).textContent || '',
      status: (document.querySelector('.cases-detail .case-status')||{}).textContent || '',
      hold: (document.querySelector('.case-hold-panel')||{}).textContent || '',
    }))()`));
    check('the new case appears in the list and is selected',
      listed.titles.includes(TITLE) && listed.selected === TITLE,
      JSON.stringify(listed).slice(0, 240));
    check('an empty case says it is holding nothing, rather than saying nothing',
      /0/.test(listed.hold), JSON.stringify(listed.hold));
    check('the case shows its status', (listed.status || '').trim().length > 0, listed.status);

    // --- bookmark a moment from the Timeline ---------------------------------------
    const toTimeline = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /timeline|garis masa|时间轴|الخط الزمني/i.test(e.textContent || ''));
      if (!hit) return 'NO TIMELINE NAV';
      hit.click();
      return 'clicked';
    })()`);
    check('the Timeline is reachable from the rail', toTimeline === 'clicked', toTimeline);
    await sleep(4000);

    // The button is disabled until a camera is on the bar, which is the honest state: a
    // bookmark of no cameras is nothing.
    const beforePick = await evalJs(`(() => {
      const btn = [...document.querySelectorAll('.tl-transport button')]
        .find(b => /add to case|tambah ke kes|加入案件|أضف إلى قضية/i.test(b.textContent || ''));
      return btn ? String(btn.disabled) : 'NO BOOKMARK BUTTON';
    })()`);
    check('the Timeline offers "Add to case"', beforePick !== 'NO BOOKMARK BUTTON', beforePick);

    // Turn on two cameras with real mouse clicks on the pill — the checkbox inside it is
    // pointer-events:none, so driving the input directly would keep passing if the pill
    // ever stopped forwarding.
    for (let i = 0; i < 2; i++) {
      const box = await evalJs(`(() => {
        const off = [...document.querySelectorAll('.tl-camera-chip')].find(c => !c.classList.contains('is-on'));
        if (!off) return '';
        const r = off.getBoundingClientRect();
        return JSON.stringify({ x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2) });
      })()`);
      if (!box) break;
      const at = JSON.parse(box);
      for (const type of ['mousePressed', 'mouseReleased']) {
        await cdp.send('Input.dispatchMouseEvent', {
          type, x: at.x, y: at.y, button: 'left', clickCount: 1,
        });
      }
      await sleep(1200);
    }
    const on = await evalJs(`document.querySelectorAll('.tl-camera-chip.is-on').length`);
    check('cameras can be put on the bar', on >= 1, 'on=' + on);

    const opened = await evalJs(`(() => {
      const btn = [...document.querySelectorAll('.tl-transport button')]
        .find(b => /add to case|tambah ke kes|加入案件|أضف إلى قضية/i.test(b.textContent || ''));
      if (!btn) return 'NO BOOKMARK BUTTON';
      if (btn.disabled) return 'STILL DISABLED';
      btn.click();
      return 'clicked';
    })()`);
    check('"Add to case" opens with cameras on the bar', opened === 'clicked', opened);
    await sleep(1800);

    const dialog = JSON.parse(await evalJs(`(() => {
      const d = document.querySelector('.case-dialog');
      return JSON.stringify({
        open: !!d,
        lines: [...document.querySelectorAll('.case-evidence-lines li')].map(e => e.textContent.trim()),
        options: [...document.querySelectorAll('.case-dialog select option')].map(e => e.textContent.trim()),
        hint: (document.querySelector('.case-dialog .case-hint')||{}).textContent || '',
      });
    })()`));
    check('the dialog lists one piece of evidence per camera on the bar',
      dialog.open && dialog.lines.length === on,
      'cameras=' + on + ' lines=' + JSON.stringify(dialog.lines));
    check('the case just created is offered to add to',
      dialog.options.some(o => o === TITLE), JSON.stringify(dialog.options).slice(0, 200));
    // The hold is the consequence of pressing this button, said at the moment of pressing.
    check('the dialog says the footage will be kept while the case is open',
      (dialog.hint || '').trim().length > 20, JSON.stringify(dialog.hint).slice(0, 160));

    const added = await evalJs(`(async () => {
      const sel = document.querySelector('.case-dialog select');
      if (!sel) return 'NO SELECT';
      const opt = [...sel.options].find(o => o.textContent.trim() === ${JSON.stringify(TITLE)});
      if (!opt) return 'CASE NOT LISTED';
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(sel, opt.value);
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      await new Promise(r => setTimeout(r, 300));
      const btn = [...document.querySelectorAll('.case-dialog-actions button')].pop();
      if (!btn || btn.disabled) return 'ADD DISABLED';
      btn.click();
      return 'added';
    })()`);
    check('the evidence can be added to the chosen case', added === 'added', added);
    await sleep(3000);

    // --- back to the case -----------------------------------------------------------
    await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /^\\s*(cases|kes|案件|القضايا)\\s*$/i.test((e.textContent || '').trim()));
      if (hit) hit.click();
      return 1;
    })()`);
    await sleep(2500);
    await evalJs(`(() => {
      const hit = [...document.querySelectorAll('.cases-list-item')]
        .find(e => (e.textContent || '').includes(${JSON.stringify(TITLE)}));
      if (hit) hit.click();
      return 1;
    })()`);
    await sleep(3000);

    const withEvidence = JSON.parse(await evalJs(`(() => JSON.stringify({
      items: [...document.querySelectorAll('.case-item')].map(e => ({
        where: (e.querySelector('.case-item-where')||{}).textContent || '',
        missing: !!e.querySelector('.case-item-missing'),
        thumb: !!e.querySelector('img.case-thumb'),
        play: [...e.querySelectorAll('button')].some(b => /play|main|播放|تشغيل/i.test(b.textContent||'')),
      })),
      hold: (document.querySelector('.case-hold-panel strong')||{}).textContent || '',
      holdAll: (document.querySelector('.case-hold-panel')||{}).textContent || '',
    }))()`));
    check('the evidence added from the Timeline is on the case',
      withEvidence.items.length === on,
      'expected ' + on + ', got ' + JSON.stringify(withEvidence.items).slice(0, 300));
    // THE CLAIM THE SCREEN MAKES. "Holding 0 clips" over held footage is the failure this
    // whole check exists for, and only a browser can see the sentence.
    check('the hold panel names a non-zero number of held clips',
      /[1-9]/.test(withEvidence.hold), JSON.stringify(withEvidence.hold));
    check('each piece of evidence names its camera and time',
      withEvidence.items.every(i => i.where.trim().length > 5),
      JSON.stringify(withEvidence.items.map(i => i.where)));
    check('footage that is present is not labelled as missing',
      withEvidence.items.every(i => !i.missing),
      JSON.stringify(withEvidence.items.map(i => i.missing)));
    check('a thumbnail of the evidence renders',
      withEvidence.items.some(i => i.thumb),
      JSON.stringify(withEvidence.items.map(i => i.thumb)));

    // --- annotate --------------------------------------------------------------------
    const annotated = await evalJs(`(async () => {
      const row = document.querySelector('.case-item');
      if (!row) return 'NO ITEM';
      const btn = [...row.querySelectorAll('button')].find(b => /annotate|anotasi|批注|تعليق/i.test(b.textContent||''));
      if (!btn) return 'NO ANNOTATE';
      btn.click();
      await new Promise(r => setTimeout(r, 400));
      const ta = row.querySelector('.case-item-edit textarea');
      if (!ta) return 'NO EDITOR';
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(ta, 'the same jacket as the gate camera');
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise(r => setTimeout(r, 200));
      const save = [...row.querySelectorAll('.case-item-edit button')][0];
      if (!save) return 'NO SAVE';
      save.click();
      return 'saved';
    })()`);
    check('a piece of evidence can be annotated', annotated === 'saved', annotated);
    await sleep(3000);
    const noteShown = await evalJs(`(() => ((document.querySelector('.case-item .case-item-note')||{}).textContent || '').trim())()`);
    check('the annotation is shown back on the row',
      /same jacket/.test(noteShown), JSON.stringify(noteShown).slice(0, 160));

    // --- export ----------------------------------------------------------------------
    const exportStart = await evalJs(`(async () => {
      const field = document.querySelector('.case-export input');
      if (!field) return 'NO REASON FIELD';
      const btn = [...document.querySelectorAll('.case-export button')][0];
      if (!btn) return 'NO EXPORT BUTTON';
      if (!btn.disabled === false) { /* fallthrough */ }
      const before = btn.disabled;
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(field, 'screen check');
      field.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise(r => setTimeout(r, 300));
      if (btn.disabled) return 'STILL DISABLED';
      btn.click();
      return before ? 'started (was disabled without a reason)' : 'started';
    })()`);
    check('an export cannot start without a reason, and starts with one',
      exportStart.startsWith('started (was disabled'), exportStart);

    let ready = '';
    for (let i = 0; i < 60; i++) {
      await sleep(2000);
      ready = await evalJs(`(() => {
        const done = document.querySelector('.case-export-ready');
        if (done) return 'ready: ' + done.textContent.trim().slice(0, 120);
        const fail = document.querySelector('.case-export .form-alert-msg');
        if (fail) return 'failed: ' + fail.textContent.trim().slice(0, 160);
        return '';
      })()`);
      if (ready) break;
    }
    check('the bundle builds and the screen offers the download',
      ready.startsWith('ready'), ready || 'never finished');
    const link = await evalJs(`(() => {
      const a = document.querySelector('.case-export-ready a');
      return a ? a.getAttribute('href') : '';
    })()`);
    check('the download link points at the case export route',
      /\/api\/cases\/exports\/case-[^/]+\/download$/.test(link || ''), link);

    // --- closing tells the truth about what it releases -------------------------------
    const closeDialog = await evalJs(`(async () => {
      const btn = [...document.querySelectorAll('.case-detail-actions button')][0];
      if (!btn) return 'NO CLOSE BUTTON';
      btn.click();
      await new Promise(r => setTimeout(r, 800));
      const d = document.querySelector('.case-dialog');
      if (!d) return 'NO DIALOG';
      const confirm = [...d.querySelectorAll('.case-dialog-actions button')].pop();
      return JSON.stringify({
        warn: (d.querySelector('.form-alert-msg')||{}).textContent || '',
        confirmDisabled: !!(confirm && confirm.disabled),
      });
    })()`);
    let cd = {};
    try { cd = JSON.parse(closeDialog); } catch (_) { cd = {}; }
    check('closing asks for an outcome before it will proceed',
      cd.confirmDisabled === true, closeDialog);
    // The warning is the last thing between an operator and released footage, and it must
    // carry the count — "some footage" is not a number anybody can weigh.
    check('the close dialog says how many clips closing releases',
      /[1-9]/.test(cd.warn || ''), JSON.stringify(cd.warn));

    await cdp.send('Page.captureScreenshot', { format: 'png' }).then((shot) => {
      fs.writeFileSync(path.join(OUT, 'uicheck_cases_' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    }).catch(() => {});

    const ok = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${ok}/${CHECKS.length} checks passed (${LANG})`);
    CHECKS.filter((c) => !c.ok).forEach((c) => console.log('  FAILED: ' + c.name + '   ' + c.detail));
    process.exitCode = ok === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((err) => {
  console.error('uicheck_cases failed: ' + err.message);
  process.exitCode = 1;
});
