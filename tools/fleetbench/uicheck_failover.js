// Drive myseliasan's Failover screen in headless Chrome (W3-7), in one language per run.
//
// WHY THIS IS NOT bench_w37_failover.py AGAIN. That bench proves the fleet can hand a
// building's cameras to a spare and that the spare writes footage that decodes. This proves
// an OPERATOR can — and, more to the point, that the screen never tells them something
// kinder than the truth:
//
//   * THE BADGE IS THE PRODUCT. A plan that has been COPIED but never DRILLED must read
//     "never tested" and must not read ready. Copying proves the two appliances can talk to
//     each other; only a drill proves the spare can reach the cameras. The check reads the
//     badge's STATE out of the DOM after each step, so a screen that painted every plan
//     green would fail here and nowhere else.
//   * THE PER-CAMERA OUTCOME OF A TAKEOVER. The appliance computes it and does not store it,
//     so it exists for one render. The live bench found the control plane dropping it; this
//     is the half that proves it reaches the SCREEN, which is where somebody has to read it
//     with an outage in progress.
//   * THE RTL DEFECT W3-5a AND W3-5b BOTH SHIPPED — a control that renders perfectly and
//     cannot be pressed. Every action control is hit-tested with document.elementFromPoint
//     AT ITS OWN CENTRE, and asserted to be inside its own card, in both directions.
//   * THE REGRESSION W3-1 SHIPPED — a build in which every design token resolved to nothing
//     and every screen check still passed. The tokens are read back out of the live page.
//
// It creates its own plan through the FORM, so the editor is exercised rather than seeded.
//
// Usage (with fleet_harness.py up and node-a holding at least one camera):
//
//   node tools/fleetbench/uicheck_failover.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
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
        setTimeout(() => rej(new Error(method + ' timed out')), 120000);
      });
    },
  };
}

(async () => {
  const port = 9236;
  const profile = path.join(OUT, 'chrome-profile-failover-' + LANG);
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
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };

    // A REAL click: a mouse event dispatched at the control's own centre, then a read of
    // what document.elementFromPoint returns there. `.click()` succeeds on a control buried
    // under another element, which is exactly the defect that shipped twice.
    const clickHandle = async (act) => {
      const box = JSON.parse(await evalJs(`(() => {
        const el = document.querySelector(${JSON.stringify('[data-fo-act=' + act + ']')});
        if (!el) return JSON.stringify({ missing: true });
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width / 2), y = Math.round(r.top + r.height / 2);
        const hit = document.elementFromPoint(x, y);
        const card = el.closest('.fo-card');
        const cr = card ? card.getBoundingClientRect() : null;
        return JSON.stringify({
          x, y, w: Math.round(r.width), h: Math.round(r.height),
          disabled: !!el.disabled,
          // Does a click AT THE CONTROL'S OWN CENTRE actually reach the control?
          reaches: !!(hit && (hit === el || el.contains(hit))),
          hitTag: hit ? (hit.tagName + (hit.className && typeof hit.className === 'string' ? '.' + hit.className.split(' ')[0] : '')) : null,
          // ...and is it inside the card it belongs to? A logical inset inside a
          // physically-anchored box renders perfectly and lands outside in RTL.
          insideCard: cr ? (r.left >= cr.left - 1 && r.right <= cr.right + 1 && r.top >= cr.top - 1 && r.bottom <= cr.bottom + 1) : null,
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

    const readyState = () => evalJs(`(() => {
      const b = document.querySelector('.fo-card [data-fo-ready]');
      return b ? b.getAttribute('data-fo-ready') + '|' + b.textContent.trim() : 'NONE';
    })()`);

    // ---- sign in, in the language under test -----------------------------------
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

    // THE KEY MUST BE THE APP'S OWN ('myseliasan_lang'). A made-up key changes nothing and
    // the run reports "renders in ar" from an English page.
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

    // ---- the W3-1 regression guard ---------------------------------------------
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

    // ---- reach the screen the way a person would --------------------------------
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /failover|ambil alih|故障接管|تجاوز الفشل/i.test(e.textContent || ''));
      if (!hit) return 'NO FAILOVER NAV: ' + [...rail.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Failover entry is in the nav and reaches the screen', /^clicked:/.test(nav), nav);
    await sleep(2500);

    // ---- clear anything a previous run left ------------------------------------
    await evalJs(`(async () => {
      const csrf = (document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1] || '';
      const r = await fetch('/api/failover-plans', { credentials: 'same-origin' });
      const b = await r.json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      for (const v of items) {
        const id = v?.plan?.id;
        if (!id) continue;
        if (v?.plan?.state === 'active') {
          await fetch('/api/failover-plans/' + id + '/release', { method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': decodeURIComponent(csrf) } });
        }
        await fetch('/api/failover-plans/' + id, { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': decodeURIComponent(csrf) } });
      }
      return items.length;
    })()`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4500);
    await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const hit = [...rail.querySelectorAll('button, a')]
        .find((e) => /failover|ambil alih|故障接管|تجاوز الفشل/i.test(e.textContent || ''));
      if (hit) hit.click();
      return 1;
    })()`);
    await sleep(2500);

    // ---- create a plan THROUGH THE FORM ----------------------------------------
    const opened = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('button')]
        .find((e) => /new plan|pelan baharu|新建方案|خطة جديدة/i.test(e.textContent || ''));
      if (!hit) return 'NO NEW-PLAN BUTTON';
      hit.click();
      return 'opened';
    })()`);
    check('an administrator can open the plan editor', opened === 'opened', opened);
    await sleep(1200);

    const filled = JSON.parse(await evalJs(`(() => {
      const setV = (el, v) => {
        const proto = el.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
        Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
        el.dispatchEvent(new Event('change', { bubbles: true }));
        el.dispatchEvent(new Event('input', { bubbles: true }));
      };
      const name = document.querySelector('[data-fo-field=name]');
      const prot = document.querySelector('[data-fo-field=protected]');
      const spare = document.querySelector('[data-fo-field=standby]');
      if (!name || !prot || !spare) return JSON.stringify({ missing: true });
      // The pickers must offer only RECORDERS, and the spare picker must never offer the
      // appliance already chosen as the protected one.
      const protOpts = [...prot.options].map((o) => o.value).filter(Boolean);
      if (!protOpts.length) return JSON.stringify({ noRecorders: true });
      setV(name, 'Screen check plan');
      setV(prot, protOpts[0]);
      const spareOpts = [...spare.options].map((o) => o.value).filter(Boolean);
      const pick = spareOpts.find((v) => v !== protOpts[0]);
      if (pick) setV(spare, pick);
      return JSON.stringify({ protOpts: protOpts.length, spareOpts: spareOpts.length, picked: !!pick, self: spareOpts.includes(protOpts[0]) });
    })()`));
    check('the editor offers recorders to pick from', !filled.missing && !filled.noRecorders,
      JSON.stringify(filled));
    check('the spare picker never offers the appliance being protected',
      filled.self === false, JSON.stringify(filled));
    await sleep(600);
    const saved = await clickHandle('save');
    check('the save control can actually be pressed', saved && saved.reaches && !saved.disabled,
      JSON.stringify(saved));
    await sleep(3500);

    let state = await readyState();
    check('a brand-new plan does NOT claim to be ready', /^not-staged\|/.test(state), state);

    // ---- copy, and refuse to call that readiness --------------------------------
    const stageBtn = await clickHandle('stage');
    check('the copy control is inside its card and hit-testable',
      stageBtn && stageBtn.reaches && stageBtn.insideCard !== false, JSON.stringify(stageBtn));
    await sleep(6000);
    state = await readyState();
    // THE ASSERTION THE SCREEN EXISTS FOR.
    check('after a successful copy the screen still says NEVER TESTED',
      /^untested\|/.test(state), state);

    // ---- the drill --------------------------------------------------------------
    const drillBtn = await clickHandle('drill');
    check('the test control is inside its card and hit-testable',
      drillBtn && drillBtn.reaches && drillBtn.insideCard !== false, JSON.stringify(drillBtn));
    await sleep(25000);
    state = await readyState();
    check('only after a drill does the screen say ready',
      /^(ready|partial|blind)\|/.test(state), state);
    const drilledOk = /^ready\|/.test(state);

    // ---- the takeover, and the per-camera outcome -------------------------------
    const overBtn = await clickHandle('activate');
    check('the take-over control is inside its card and hit-testable',
      overBtn && overBtn.reaches && overBtn.insideCard !== false, JSON.stringify(overBtn));
    await sleep(12000);

    const after = JSON.parse(await evalJs(`(() => {
      const card = document.querySelector('.fo-card');
      const rows = [...document.querySelectorAll('.fo-camera-table tbody tr')].map((tr) => {
        const cell = tr.querySelector('[data-fo-outcome]');
        return {
          cells: [...tr.children].map((td) => (td.textContent || '').trim()),
          // The STATE the appliance reported, read from the attribute rather than the text:
          // the text is a sentence composed in the operator's language, which is the point.
          outcome: cell ? cell.getAttribute('data-fo-outcome') : '',
          outcomeText: cell ? (cell.textContent || '').trim() : '',
        };
      });
      const summary = document.querySelector('.fo-outcome-line');
      const badge = document.querySelector('.fo-card [data-fo-ready]');
      return JSON.stringify({
        rows,
        summary: summary ? summary.textContent.trim() : null,
        badge: badge ? badge.getAttribute('data-fo-ready') : null,
        cameraTableOpenedItself: rows.length > 0,
        cardHasReleaseControl: !!(card && card.querySelector('[data-fo-act=release]')),
      });
    })()`));
    check('the takeover opens the camera list by itself', after.cameraTableOpenedItself,
      JSON.stringify(after).slice(0, 300));
    check('the screen shows what happened PER CAMERA',
      after.rows.some((r) => !!r.outcome && !!r.outcomeText),
      JSON.stringify(after.rows).slice(0, 300));
    // THE THIRD DEFECT THIS SCREEN PASS FOUND. The appliance used to return a finished
    // English sentence and the screen printed it, so an Arabic operator read "recording" in
    // English in a table whose every other cell was Arabic. The appliance now returns a
    // STATE and the screen composes the sentence — so the rendered text must never be the
    // raw state token, in any language.
    check('the outcome is rendered as a sentence, not the raw state token',
      // Case-SENSITIVE on purpose. When the dictionary lookup misses, the screen falls back
      // to printing the raw code, which is lowercase and hyphenated; every real label is
      // neither. A case-insensitive compare would fail on the English "Recording", which is
      // a legitimate translation that happens to be the same word.
      after.rows.every((r) => !r.outcome || r.outcomeText !== r.outcome),
      JSON.stringify(after.rows.map((r) => [r.outcome, r.outcomeText])).slice(0, 260));
    check('the screen summarises how many cameras are actually recording', !!after.summary,
      after.summary);
    // THE CONTRADICTION CHECK, and the reason this screen pass earned its place: the first
    // run showed a row whose drill cell said "could not be reached" and whose outcome cell
    // said "recording", because the appliance was reading "an ffmpeg process exists" as
    // "footage is being written". A card that says both things at once is worse than one
    // that says nothing.
    const contradictions = after.rows.filter((r) =>
      r.outcome === 'recording'
      && r.cells.some((c) => /could not be reached|rejected the login|\u062a\u0639\u0630|\u0631\u064f\u0641\u0636|tidak dapat dicapai|\u65e0\u6cd5\u8bbf\u95ee|\u767b\u5f55\u88ab\u62d2/i.test(c)));
    check('no camera is called RECORDING on a row that says it could not be reached',
      contradictions.length === 0, JSON.stringify(contradictions).slice(0, 300));
    check('a plan carrying the cameras offers HAND BACK, not take over',
      after.cardHasReleaseControl === true, JSON.stringify(after).slice(0, 200));

    // ---- hand back ---------------------------------------------------------------
    const backBtn = await clickHandle('release');
    check('the hand-back control is inside its card and hit-testable',
      backBtn && backBtn.reaches && backBtn.insideCard !== false, JSON.stringify(backBtn));
    await sleep(9000);

    // A HAND-BACK is not a failed takeover. Every camera reporting "stopped" is the correct
    // outcome of the button just pressed, and summarising it in the amber reserved for a
    // partial takeover turns a clean fail-back into an alarm. Found by LOOKING at the
    // screenshot on a run where every assertion passed — which is the argument for looking.
    const back = JSON.parse(await evalJs(`(() => {
      const line = document.querySelector('.fo-outcome-line');
      return JSON.stringify({
        text: line ? line.textContent.trim() : null,
        calm: !!(line && line.classList.contains('fo-outcome-line--all')),
        outcomes: [...document.querySelectorAll('[data-fo-outcome]')].map((e) => e.getAttribute('data-fo-outcome')),
      });
    })()`));
    check('a clean hand-back is not summarised as an alarm',
      !back.text || (back.calm && back.outcomes.every((o) => o === 'stopped')),
      JSON.stringify(back).slice(0, 220));

    // ---- what the language pass is actually for ----------------------------------
    const summary = JSON.parse(await evalJs(`(() => {
      const text = document.body.innerText || '';
      // A key that reached the screen instead of a translation — invisible to every API
      // assertion ever written.
      const rawKeys = [...new Set(text.match(/\\bfo\\.[a-zA-Z0-9_.-]+/g) || [])];
      const badges = [...document.querySelectorAll('[data-fo-ready]')].map((e) => e.textContent.trim());
      const capacity = [...document.querySelectorAll('[data-fo-capacity]')].map((e) => ({
        state: e.getAttribute('data-fo-capacity'), text: e.textContent.trim(),
      }));
      const hint = document.querySelector('.fo-ready-hint');
      const limits = [...document.querySelectorAll('.fo-pitch-limit')].map((e) => e.textContent.trim());
      const acts = [...document.querySelectorAll('[data-fo-act]')].map((e) => e.textContent.trim());
      const overflow = document.documentElement.scrollWidth - document.documentElement.clientWidth;
      return JSON.stringify({
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
        rawKeysOnScreen: rawKeys.slice(0, 10),
        badges, capacity, hint: hint ? hint.textContent.trim() : null,
        limits, acts, overflow,
      });
    })()`));
    check('no untranslated key reached the screen', summary.rawKeysOnScreen.length === 0,
      summary.rawKeysOnScreen.join(', ') || 'none');
    check('the badge is rendered as words, not a state token',
      summary.badges.length > 0 && !summary.badges.some((b) => /^[a-z-]+$/.test(b)),
      JSON.stringify(summary.badges));
    // W3-7 shipped answering only half of "would this work": a drill proves the spare can
    // REACH the cameras and says nothing about whether it could carry them. The capacity line
    // is the other half, and it has to be on the card rather than in a manual.
    check('the card says what the spare can carry, not only whether it can be reached',
      summary.capacity.length > 0, JSON.stringify(summary.capacity).slice(0, 200));
    check('and it says it in a sentence, not as a state token',
      summary.capacity.every((c) => c.text.length > 15 && !/^[a-z-]+$/.test(c.text)),
      JSON.stringify(summary.capacity.map((c) => c.state + ': ' + c.text.slice(0, 50))));

    check('the plain-language explanation under the badge is rendered', !!summary.hint,
      (summary.hint || '').slice(0, 90));
    check('the two things failover is NOT are stated on the screen', summary.limits.length === 2,
      JSON.stringify(summary.limits).slice(0, 160));
    check('the page does not scroll sideways', summary.overflow <= 1, 'overflow=' + summary.overflow);

    if (LANG !== 'en') {
      const ascii = summary.acts.every((l) => /^[\x20-\x7e]*$/.test(l));
      check('the page really switched language', !ascii, JSON.stringify(summary.acts).slice(0, 160));
      // The assertion the third defect needed. Every other cell in the row was translated;
      // this one was not, and nothing said so.
      const englishOutcomes = after.rows.filter((r) => r.outcome && /^[\x20-\x7e]+$/.test(r.outcomeText));
      check('the per-camera outcome is not left in English', englishOutcomes.length === 0,
        JSON.stringify(englishOutcomes.map((r) => r.outcomeText)).slice(0, 200));
      if (LANG === 'ar') {
        check('Arabic puts the page in RTL', summary.dir === 'rtl', 'dir=' + summary.dir);
      }
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'failover-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'failover-' + LANG + '.png'));
    if (!drilledOk) console.log('note: the drill did not come back fully reachable; see the row detail above');
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
