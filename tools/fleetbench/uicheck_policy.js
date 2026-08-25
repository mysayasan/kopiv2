// Drive myseliasan's Fleet Policy screen in headless Chrome (W2-1), one language per run.
//
// W2-1 SHIPPED WITHOUT EVER BEING DRIVEN IN A BROWSER. Its API was benched; its screen was
// not. Every screen pass in this programme so far has found something the API bench could not,
// twice by nothing more than opening the PNG, so this is the debt being paid.
//
// What it proves, and why each one:
//
//   * A POLICY MADE THROUGH THE FORM CHANGES A VERDICT. The check ticks a real setting, types
//     a value node-a does not hold, saves, presses Check now, and reads the node's badge. That
//     is the whole feature: a fleet-wide statement, compared against a real appliance over the
//     real tunnel.
//   * ...AND CLEARS AGAIN. Editing the value to what the appliance actually holds must turn
//     the same node compliant. A screen that can only go red proves half a feature.
//   * DRIFTED, UNKNOWN AND UNMANAGED ARE THREE DIFFERENT ANSWERS. "We could not ask this
//     appliance" is not "this appliance agrees", and "no policy covers it" is neither. The
//     badge STATE is read from the DOM and the rendered text is asserted NOT to be that state
//     token — which is what lets the same check run in four languages.
//   * REPORT-ONLY VS ENFORCING IS VISIBLE. A policy that only reports and one that changes
//     appliances must not look alike, and the warning beside the enforce switch must be on
//     screen rather than in a manual.
//   * EVERY ACTION IS PRESSABLE AT ITS OWN CENTRE, in both directions. W3-5a, W3-5b and W3-6b
//     each shipped a control that rendered perfectly and could not be clicked, in Arabic only.
//   * THE W3-1 REGRESSION GUARD: a build where every design token resolved to nothing once
//     passed every screen check there was. The tokens are read back out of the live page.
//
// Usage (with fleet_harness.py up and both nodes adopted):
//
//   node tools/fleetbench/uicheck_policy.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

// The setting under test. An integer with a wide range, held by every camera appliance, and
// nothing else in the fleet depends on it — so moving it proves the comparison without
// changing how anything behaves. The value is deliberately one no appliance ships with.
const SECTION = 'machineHealth';
const FIELD = 'cpu.warnPercent';
const KEY = SECTION + '.' + FIELD;
const WANTED = '63';

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
  const port = 9240;
  const profile = path.join(OUT, 'chrome-profile-policy-' + LANG);
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
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
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };

    // A REAL click: a mouse event at the control's own centre, then a read of what
    // document.elementFromPoint returns there. `.click()` succeeds on a control buried under
    // another element, which is exactly the defect that has shipped three times.
    const clickSel = async (selector, containerSel) => {
      const box = JSON.parse(await evalJs(`(() => {
        const el = document.querySelector(${JSON.stringify(selector)});
        if (!el) return JSON.stringify({ missing: true });
        el.scrollIntoView({ block: 'center' });
        const r = el.getBoundingClientRect();
        const x = Math.round(r.left + r.width / 2), y = Math.round(r.top + r.height / 2);
        const hit = document.elementFromPoint(x, y);
        const box = ${JSON.stringify(containerSel || '')} ? el.closest(${JSON.stringify(containerSel || 'body')}) : null;
        const br = box ? box.getBoundingClientRect() : null;
        return JSON.stringify({
          x, y, w: Math.round(r.width), h: Math.round(r.height),
          disabled: !!el.disabled,
          reaches: !!(hit && (hit === el || el.contains(hit) || (el.tagName === 'INPUT' && hit.contains(el)))),
          hitTag: hit ? hit.tagName + (typeof hit.className === 'string' && hit.className ? '.' + hit.className.split(' ')[0] : '') : null,
          // A logical inset inside a physically-anchored box renders perfectly and lands
          // outside its own card once the page is mirrored.
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

    // Type into a controlled React input the way a person does: set through the native
    // setter so React's own onChange fires.
    const setInput = async (selector, value) => evalJs(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)});
      if (!el) return 'MISSING';
      const proto = el.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${JSON.stringify(value)});
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return el.value;
    })()`);

    const nodeState = () => evalJs(`(() => {
      const el = document.querySelector('[data-fp-node="node-a"]');
      if (!el) return 'NONE';
      const badge = el.querySelector('[data-fp-badge]');
      return el.getAttribute('data-fp-status') + '|' + (badge ? badge.textContent.trim() : '');
    })()`);

    const openPolicyScreen = async () => {
      await evalJs(`(() => {
        const rail = document.querySelector('.side-nav, nav, aside') || document;
        const hit = [...rail.querySelectorAll('button, a')]
          .find((e) => /fleet policy|dasar armada|机群策略|سياسة الأسطول/i.test(e.textContent || ''));
        if (hit) hit.click();
        return !!hit;
      })()`);
      await sleep(2500);
    };

    // ---- sign in, in the language under test ------------------------------------------
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

    // Clear anything a previous run left, so the run starts from "no policy at all" — which
    // is also the state that proves `unmanaged` renders.
    const cleared = await evalJs(`(async () => {
      const csrf = decodeURIComponent((document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1] || '');
      const b = await (await fetch('/api/fleet-policies', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      for (const d of items) {
        const id = d?.policy?.id;
        if (id) await fetch('/api/fleet-policies/' + id, { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrf } });
      }
      return items.length;
    })()`);

    // THE KEY MUST BE THE APP'S OWN. A made-up key changes nothing and the run reports
    // "renders in ar" from an English page.
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

    // ---- reach the screen the way a person would ----------------------------------------
    await openPolicyScreen();
    const landed = await evalJs(`(() => !!document.querySelector('[data-fp-tile]'))()`);
    check('the Fleet policy entry is in the nav and reaches the screen', landed === true,
      'cleared ' + cleared + ' leftover policies');

    // ---- with no policy at all, a node is UNMANAGED, not compliant ----------------------
    await clickSel('[data-fp-act="check"]');
    await sleep(6000);
    const bare = await nodeState();
    check('a node no policy covers reads UNMANAGED — not compliant, which would be a claim '
      + 'nobody made', bare.startsWith('unmanaged|'), bare);
    check('and the badge shows words, not the state token',
      bare.split('|')[1] && bare.split('|')[1] !== 'unmanaged', bare.split('|')[1]);

    // ---- build a policy THROUGH THE FORM ------------------------------------------------
    const openedEditor = await clickSel('[data-fp-act="new"]');
    await sleep(1500);
    check('an administrator can open the policy editor',
      openedEditor.reaches === true && await evalJs(`(() => !!document.querySelector('[data-fp-input="name"]'))()`),
      JSON.stringify(openedEditor));

    await setInput('[data-fp-input="name"]', 'Screen check policy');
    const picked = await clickSel(`[data-fp-pick="${KEY}"]`, '[data-fp-field]');
    check('a setting can be ticked at its own centre', picked.reaches === true && picked.inside !== false,
      JSON.stringify(picked));
    await sleep(600);
    const typed = await setInput(`[data-fp-value="${FIELD}"]`, WANTED);
    check('and given a value', typed === WANTED, 'input holds ' + typed);

    // The one warning on this screen worth reading has to be ON the screen.
    const enforceNote = await evalJs(`(() => {
      const el = document.querySelector('.fp-enforce-note');
      return el ? el.textContent.trim() : '';
    })()`);
    check('the difference between reporting and CHANGING appliances is stated beside the switch',
      !!enforceNote && enforceNote.length > 20, (enforceNote || '').slice(0, 90));

    const saved = await clickSel('[data-fp-act="save"]');
    await sleep(3000);
    const savedOk = await evalJs(`(() => document.querySelectorAll('[data-fp-policy]').length)()`);
    check('saving through the form creates the policy',
      saved.reaches === true && savedOk === 1, JSON.stringify(saved) + ', cards=' + savedOk);
    const reportTag = await evalJs(`(() => !!document.querySelector('[data-fp-policy] .fp-scope-tag--report'))()`);
    check('a policy that only reports says so on its card, so it is never mistaken for one '
      + 'that changes appliances', reportTag === true);

    // ---- the verdict has to MOVE ---------------------------------------------------------
    const checkBtn = await clickSel('[data-fp-act="check"]');
    check('Check now can be pressed at its own centre', checkBtn.reaches === true, JSON.stringify(checkBtn));
    await sleep(8000);
    const drifted = await nodeState();
    check('a policy the appliance disagrees with turns the node DRIFTED — the fleet asked a '
      + 'real appliance over the real tunnel', drifted.startsWith('drifted|'), drifted);

    // ...and it must name WHICH setting, with both numbers. A verdict with no detail is a
    // verdict nobody can act on.
    await clickSel('[data-fp-node="node-a"] [data-fp-act="detail"]', '[data-fp-node="node-a"]');
    await sleep(1200);
    const row = JSON.parse(await evalJs(`(() => {
      const tr = document.querySelector('[data-fp-row="${KEY}"]');
      if (!tr) return JSON.stringify({ missing: true });
      const cells = [...tr.querySelectorAll('td')].map((td) => td.textContent.trim());
      return JSON.stringify({ status: tr.getAttribute('data-fp-rowstatus'), cells });
    })()`));
    check('the detail names the setting, what the fleet asked for and what the appliance holds',
      !row.missing && row.status === 'drift' && row.cells.length >= 3
      && row.cells[1] === WANTED && row.cells[2] && row.cells[2] !== WANTED,
      JSON.stringify(row));
    const actual = row.missing ? '' : row.cells[2];

    // A second capture, at the moment the screen is actually SAYING something. The final
    // shot is of an empty screen after the cleanup, and it was only by opening a screenshot
    // that the stale-verdict defect was found at all — so the run leaves behind the frame
    // worth looking at, not just the last one.
    const midShot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'policy-drift-' + LANG + '.png'), Buffer.from(midShot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'policy-drift-' + LANG + '.png'));

    // ---- and it has to CLEAR -------------------------------------------------------------
    // A screen that can only go red proves half a feature.
    await clickSel('[data-fp-policy] [data-fp-act="edit"]', '[data-fp-policy]');
    await sleep(1500);
    const retyped = await setInput(`[data-fp-value="${FIELD}"]`, actual);
    check('the policy can be edited to what the appliance actually holds', retyped === actual,
      'set to ' + retyped);
    await clickSel('[data-fp-act="save"]');
    await sleep(3000);

    // BEFORE the re-sweep, and this is the moment that matters. The verdicts on screen were
    // reached against the OLD rule; the "last checked" time beside them is perfectly true,
    // which is exactly what makes leaving them uncoloured-as-current misleading. The screen
    // has to say what they predate.
    const staleBanner = await evalJs(`(() => {
      const el = document.querySelector('[data-fp-stale]');
      return el ? el.textContent.trim() : '';
    })()`);
    check('a policy edit makes the screen say the verdicts predate it, rather than leaving '
      + 'them looking current', !!staleBanner && staleBanner.length > 25,
      (staleBanner || '').slice(0, 110));

    await clickSel('[data-fp-act="check"]');
    await sleep(8000);
    const cleared2 = await nodeState();
    check('agreeing with the appliance turns the same node COMPLIANT — the verdict tracks the '
      + 'fleet rather than latching', cleared2.startsWith('compliant|'), cleared2);
    // ...and the warning has to GO. A banner that never clears is a banner people stop
    // reading, which costs the next one its meaning.
    const stillStale = await evalJs(`(() => !!document.querySelector('[data-fp-stale]'))()`);
    check('and once the fleet has been re-checked the out-of-date warning clears',
      stillStale === false);

    // ---- the tiles are the summary, and they must agree with the cards --------------------
    const tiles = JSON.parse(await evalJs(`(() => {
      const out = {};
      document.querySelectorAll('[data-fp-tile]').forEach((el) => {
        out[el.getAttribute('data-fp-tile')] = Number((el.querySelector('.fp-tile-n') || {}).textContent || 0);
      });
      const cards = {};
      document.querySelectorAll('[data-fp-node]').forEach((el) => {
        const s = el.getAttribute('data-fp-status');
        cards[s] = (cards[s] || 0) + 1;
      });
      return JSON.stringify({ tiles: out, cards });
    })()`));
    const agree = Object.keys(tiles.cards).every((k) => tiles.tiles[k] === tiles.cards[k]);
    check('the counters at the top agree with the cards below them', agree, JSON.stringify(tiles));
    check('all four verdicts have a counter, including the two that mean "we do not know"',
      ['drifted', 'unknown', 'compliant', 'unmanaged'].every((s) => s in tiles.tiles),
      Object.keys(tiles.tiles).join(', '));

    // ---- language, layout, and the raw-key guard ------------------------------------------
    const page = JSON.parse(await evalJs(`(() => {
      const text = document.body.innerText || '';
      return JSON.stringify({
        rawKeys: [...new Set(text.match(/\\bfp\\.[a-zA-Z0-9_.-]+/g) || [])].slice(0, 10),
        overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
        tileLabels: [...document.querySelectorAll('.fp-tile-label')].map((e) => e.textContent.trim()),
      });
    })()`));
    check('no untranslated key reached the screen', page.rawKeys.length === 0,
      page.rawKeys.join(', ') || 'none');
    check('the page does not scroll sideways', page.overflow <= 1, 'overflow=' + page.overflow);
    if (LANG !== 'en') {
      check('the page really switched language',
        page.tileLabels.some((l) => !/^[\x20-\x7e]*$/.test(l)),
        JSON.stringify(page.tileLabels).slice(0, 160));
      if (LANG === 'ar') check('Arabic puts the page in RTL', page.dir === 'rtl', 'dir=' + page.dir);
    }

    // ---- and it can be taken away again ---------------------------------------------------
    await evalJs(`window.confirm = () => true, 1`);
    const del = await clickSel('[data-fp-policy] [data-fp-act="delete"]', '[data-fp-policy]');
    check('Delete can be pressed at its own centre', del.reaches === true && del.inside !== false,
      JSON.stringify(del));
    await sleep(3000);
    const after = JSON.parse(await evalJs(`(async () => {
      const b = await (await fetch('/api/fleet-policies', { credentials: 'same-origin' })).json();
      const items = (b?.result?.items) || (b?.data?.result?.items) || [];
      return JSON.stringify({ server: items.length, shown: document.querySelectorAll('[data-fp-policy]').length });
    })()`));
    check('deleting through the screen really removes it, on the server and on the page',
      after.server === 0 && after.shown === 0, JSON.stringify(after));

    // THE ONE THAT SHIPPED. With the last policy gone there is nothing to be compliant WITH,
    // and the fleet went on reporting every node compliant — on the page AND from the API —
    // until somebody happened to press Check now. Found by opening the screenshot of a run
    // in which every other assertion had passed.
    const orphaned = await nodeState();
    check('deleting the last policy stops the fleet claiming to be compliant with rules that '
      + 'no longer exist', orphaned.startsWith('unmanaged|'), orphaned);
    const api = JSON.parse(await evalJs(`(async () => {
      const b = await (await fetch('/api/fleet-policies/compliance', { credentials: 'same-origin' })).json();
      const r = (b?.result) || (b?.data?.result) || {};
      return JSON.stringify({ counts: r.counts || {}, stale: !!r.stale });
    })()`));
    check('and the API says the same thing, so it is not a repaint on one screen',
      (api.counts.compliant || 0) === 0 && (api.counts.unmanaged || 0) > 0, JSON.stringify(api));

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, 'policy-' + LANG + '.png'), Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + path.join(OUT, 'policy-' + LANG + '.png'));
  } finally {
    chrome.kill();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
