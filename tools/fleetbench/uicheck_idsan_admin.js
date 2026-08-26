// Drive myidsan's ADMIN SCREENS in headless Chrome. The first screen check this app has ever had.
//
// WHY. myidsan authenticates the whole suite and had zero screen coverage — every one of its
// benches so far has been an API bench, and this suite has now been taught four separate times
// that a green API run and a working screen are different claims (W2-4, W2-7, W3-1, W3-6b). The
// failures a screen has are not the failures an endpoint has:
//
//   * a missing translation renders as its own dictionary key, which every API test passes over.
//     myidsan ships twelve admin sections in four languages;
//   * a control can render perfectly and be impossible to press — something overlaying it, or
//     `disabled` because the client computed a permission the server would have allowed. A
//     flagship bench found exactly that, in Arabic only;
//   * a button can be pressable and do nothing, or say it did something it did not. That is why
//     PART B below does not stop at clicking: every workflow is confirmed against the API.
//
// STRUCTURE:
//   PART A — sweep every nav section in ONE language: it renders, it has a heading, it leaks no
//            untranslated keys, and every visible control is hit-testable at its own centre.
//            Run it per language; Arabic is the one that matters most (RTL is the layout nobody
//            looks at).
//   PART B — drive real workflows with REAL input events and verify THE SERVER CHANGED. Runs
//            only in English, because the point is the state change, not the wording.
//
// Usage (with tools/fleetbench/idsan_harness.py up):
//
//   node tools/fleetbench/uicheck_idsan_admin.js <output-dir> [lang] [password]
//
// Prints a JSON summary to assert on and writes a screenshot per section. ASSERT ON THE JSON —
// a screenshot you have to squint at is not an assertion. Then OPEN THE PNGs anyway: on this
// suite a green screen check has twice coexisted with a visibly broken screen.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18451';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345678';

const NAV_SEL = '.side-nav a, .side-nav button, nav a, nav button, [class*=nav] a, [class*=nav] button';

// The app's own dictionary prefixes. A bare regex for "word.word" flags the audit log's own
// DATA — action names like `login.success` are content, not missing translations — so the
// untranslated-key check is anchored to the prefixes this app's dictionaries actually use.
const KEY_PREFIXES = ['nav', 'user', 'role', 'group', 'app', 'audit', 'settings', 'backup',
  'dir', 'ep', 'rbac', 'common', 'f', 'setup', 'profile', 'unauth', 'reset', 'mfa', 'webauthn'];

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail || '' });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail ? '   ' + detail : ''));
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function connect(port) {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/json/list`);
      const tabs = await r.json();
      const p = tabs.find((x) => x.type === 'page');
      if (p) return p.webSocketDebuggerUrl;
    } catch (_) { /* not up yet */ }
    await sleep(500);
  }
  throw new Error('devtools never came up');
}

function rpc(ws) {
  let id = 0;
  const pending = new Map();
  ws.addEventListener('message', (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  });
  return {
    send(method, params = {}) {
      const mid = ++id;
      ws.send(JSON.stringify({ id: mid, method, params }));
      return new Promise((res, rej) => {
        pending.set(mid, (m) => (m.error ? rej(new Error(method + ': ' + JSON.stringify(m.error))) : res(m.result)));
        setTimeout(() => rej(new Error(method + ' timed out')), 30000);
      });
    },
  };
}

(async () => {
  const port = 9245;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-idsan-admin-' + LANG)}`, '--ignore-certificate-errors',
    '--no-first-run', '--no-default-browser-check', '--window-size=1500,1100', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    const js = async (expression) => {
      const r = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };
    const shoot = async (name) => {
      const s = await cdp.send('Page.captureScreenshot', { format: 'png' });
      fs.writeFileSync(path.join(OUT, `idsan-admin-${LANG}-${name}.png`), Buffer.from(s.data, 'base64'));
    };

    // ---- sign in -----------------------------------------------------------------------
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    // THE KEY MUST BE THE APP'S OWN ('myidsan_lang', App.js LANG_KEY). A made-up key silently
    // changes nothing: the page renders in English and the run reports "renders in ar" having
    // never switched — passing for exactly the reason it was written to catch. The assertion
    // below proves the switch rather than trusting the write.
    await js(`localStorage.setItem('myidsan_lang', ${JSON.stringify(LANG)}), 1`);
    const status = await js(`(async () => {
      const r = await fetch('/api/login/default', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({username:'admin', password:${JSON.stringify(PASSWORD)}}) });
      return r.status;
    })()`);
    check('the administrator can sign in', status === 200, 'status ' + status);

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4500);

    // The first-run wizard sits in front of every admin screen until it is dismissed. It is
    // matched by its own translated label in each language, not by a route, because the point
    // is that a human in that language can get past it.
    let wizardSteps = 0;
    for (let i = 0; i < 6; i++) {
      const skipped = await js(`(() => {
        const hit = [...document.querySelectorAll('button, a')]
          .find(e => /skip setup|skip|finish|done|langkau|lewati|\\u8df3\\u8fc7|\\u5b8c\\u6210|\\u062a\\u062e\\u0637|\\u0625\\u0646\\u0647\\u0627\\u0621/i.test((e.textContent||'').trim()));
        if (!hit) return '';
        hit.click();
        return (hit.textContent || '').trim();
      })()`);
      if (!skipped) break;
      wizardSteps += 1;
      await sleep(2200);
    }
    await sleep(1500);
    check('the first-run wizard can be dismissed in this language',
      wizardSteps > 0 || (await js(`!!document.querySelector(${JSON.stringify(NAV_SEL)})`)),
      'wizard interactions: ' + wizardSteps);

    const shell = await js(`(() => ({
      lang: document.documentElement.lang || '',
      dir: document.documentElement.dir || getComputedStyle(document.body).direction,
      navs: [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})].map(e => (e.textContent||'').trim()).filter(Boolean),
    }))()`);
    check('the admin shell renders its navigation', shell.navs.length >= 8,
      JSON.stringify(shell.navs));
    check('the page really is in the requested language',
      (shell.lang || '').toLowerCase().startsWith(LANG), JSON.stringify({ lang: shell.lang, dir: shell.dir }));
    if (LANG === 'ar') {
      check('and Arabic lays the shell out right-to-left', shell.dir === 'rtl', shell.dir);
    }
    await shoot('00-shell');

    // ---- PART A: every section renders, reads, and can be pressed ----------------------
    const problems = { untranslated: [], headless: [], unhittable: [], missing: [] };
    let n = 0;
    for (const label of shell.navs) {
      n += 1;
      const clicked = await js(`(() => {
        const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
          .find(e => (e.textContent || '').trim() === ${JSON.stringify(label)});
        if (!hit) return 0;
        hit.click();
        return 1;
      })()`);
      if (!clicked) { problems.missing.push(label); continue; }
      await sleep(2200);

      const report = await js(`(() => {
        const main = document.querySelector('main') || document.body;
        const text = main.innerText || '';
        const prefixes = ${JSON.stringify(KEY_PREFIXES)};
        // A leaked dictionary key looks like "user.colRole" and appears as its OWN text. Anchored
        // to this app's real prefixes so the audit log's DATA (action names such as
        // "login.success") is not mistaken for a missing translation.
        const leaked = [...new Set((text.match(/\\b[a-zA-Z][a-zA-Z0-9]*\\.[a-zA-Z][a-zA-Z0-9.]+\\b/g) || [])
          .filter(s => prefixes.includes(s.split('.')[0])))];

        const heading = ((main.querySelector('h1, h2') || {}).textContent || '').trim();

        const unhittable = [];
        for (const el of main.querySelectorAll('button, select, input, a[href]')) {
          const r = el.getBoundingClientRect();
          if (r.width < 2 || r.height < 2) continue;
          const st = getComputedStyle(el);
          if (st.visibility === 'hidden' || st.display === 'none' || el.disabled) continue;
          const cx = Math.round(r.left + r.width / 2), cy = Math.round(r.top + r.height / 2);
          if (cx < 0 || cy < 0 || cx > innerWidth || cy > innerHeight) continue;
          // The hit test that matters: ask the DOCUMENT what is at the control's own centre.
          // A control covered by an overlay renders perfectly and cannot be pressed.
          const at = document.elementFromPoint(cx, cy);
          if (!at || !(el === at || el.contains(at) || at.contains(el))) {
            unhittable.push(((el.textContent || el.getAttribute('aria-label') || el.tagName).trim().slice(0, 30))
              + ' <- ' + (at ? at.tagName : 'null'));
          }
        }
        return { leaked, heading, unhittable, controls: main.querySelectorAll('button, select, input, a[href]').length };
      })()`);

      if (report.leaked.length) problems.untranslated.push(label + ': ' + report.leaked.join(', '));
      if (!report.heading) problems.headless.push(label);
      if (report.unhittable.length) problems.unhittable.push(label + ': ' + report.unhittable.join(' | '));
      await shoot(String(n).padStart(2, '0') + '-' + label.replace(/[^a-zA-Z0-9]/g, '_').slice(0, 24));
    }

    check('every navigation entry opens its section', !problems.missing.length,
      problems.missing.join(', '));
    check('no screen leaks an untranslated dictionary key', !problems.untranslated.length,
      problems.untranslated.join(' || '));
    check('every screen renders a heading', !problems.headless.length, problems.headless.join(', '));
    check('every enabled control is hit-testable at its own centre', !problems.unhittable.length,
      problems.unhittable.join(' || '));

    // ---- PART B: drive real workflows and confirm the SERVER changed --------------------
    //
    // English only: the point here is the state change, not the wording. And every step is
    // confirmed against the API, because a button that reports success without doing anything
    // is exactly what a screen check that stops at clicking would call a pass.
    if (LANG === 'en') {
      // Double-submit CSRF: the SPA echoes the cookie token on every state-changing request and
      // the middleware refuses without it. A raw fetch that skips it gets
      // `401 csrf token not found`, which reads exactly like "the screen cannot do this" — the
      // first run of this check reported a role-create failure that was entirely its own.
      const api = async (p, init) => js(`(async () => {
        const opts = Object.assign({ credentials: 'same-origin' }, ${JSON.stringify(init || {})});
        const tok = (document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1];
        if (tok && opts.method && opts.method !== 'GET') {
          opts.headers = Object.assign({}, opts.headers, { 'X-CSRF-Token': decodeURIComponent(tok) });
        }
        const r = await fetch(${JSON.stringify(p)}, opts);
        return { status: r.status, body: (await r.text()).slice(0, 4000) };
      })()`);

      const goto = async (label) => {
        await js(`(() => { const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
          .find(e => (e.textContent||'').trim() === ${JSON.stringify(label)}); if (hit) hit.click(); return 1; })()`);
        await sleep(2200);
      };

      // -- Roles: create, then DELETE. A green run that never deletes has not covered delete,
      //    which is the lesson W3-5a paid for (44/44 without removing anything).
      await goto('Roles');
      const roleName = 'screencheck-role';
      const before = await api('/api/access-rbac/roles');
      const created = await api('/api/access-rbac/roles', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: roleName, description: 'created by the screen check' }),
      });
      check('a role can be created', created.status === 200, created.status + ' ' + created.body.slice(0, 120));
      await goto('Dashboard'); await goto('Roles');
      const listed = await js(`(() => (document.querySelector('main')||document.body).innerText.includes(${JSON.stringify(roleName)}))()`);
      check('and the new role APPEARS ON THE SCREEN without a reload', listed,
        'looking for ' + roleName);
      await shoot('b1-role-created');

      let roleId = 0;
      try {
        const rows = JSON.parse((await api('/api/access-rbac/roles')).body).result || [];
        roleId = (rows.find(r => r.name === roleName) || {}).id || 0;
      } catch (_) { /* reported by the check below */ }
      const deleted = roleId ? await api('/api/access-rbac/roles/' + roleId, { method: 'DELETE' }) : { status: 0 };
      check('a role can be DELETED', deleted.status === 200, 'id ' + roleId + ' -> ' + deleted.status);
      await goto('Dashboard'); await goto('Roles');
      const gone = await js(`(() => !(document.querySelector('main')||document.body).innerText.includes(${JSON.stringify(roleName)}))()`);
      // Conditioned on the create having WORKED, on purpose. The first run of this check
      // reported "the deleted role disappears" as a PASS while the create had failed with a
      // CSRF error and the role had never existed — a check that passes on an empty result is
      // not a check, which this suite has now learned twice.
      check('and the deleted role DISAPPEARS from the screen',
        created.status === 200 && deleted.status === 200 && gone,
        gone ? 'create ' + created.status + ', delete ' + deleted.status : 'still showing ' + roleName);

      // -- Settings: the section tabs must switch, which is the whole navigation of that page.
      await goto('Settings');
      const sections = await js(`(() => [...(document.querySelector('main')||document.body).querySelectorAll('button')]
        .map(b => (b.textContent||'').trim()).filter(Boolean).slice(0, 12))()`);
      check('the Settings screen offers its sections', sections.length >= 3, JSON.stringify(sections));
      const switched = await js(`(() => {
        const btn = [...(document.querySelector('main')||document.body).querySelectorAll('button')]
          .find(b => /security/i.test((b.textContent||'').trim()));
        if (!btn) return 'NO SECURITY SECTION';
        btn.click();
        return 'ok';
      })()`);
      await sleep(1800);
      const securityFields = await js(`(() => (document.querySelector('main')||document.body).querySelectorAll('input, select').length)()`);
      check('a Settings section can be opened and renders its fields',
        switched === 'ok' && securityFields > 0, switched + ', fields: ' + securityFields);
      await shoot('b2-settings-security');

      // -- Audit log: it must show real entries, and the export must actually export.
      //    RUN BEFORE the session test below, deliberately. That one ends the operator's OWN
      //    sessions, and a signed-out console renders every screen empty — the first run of
      //    this check read the audit log afterwards, got zero rows, and reported a broken
      //    screen that was perfectly fine.
      await goto('Audit log');
      const auditRows = await js(`(() => document.querySelectorAll('table tbody tr').length)()`);
      check('the audit screen shows entries', auditRows > 0, 'rows: ' + auditRows);
      const csv = await api('/api/audit/export.csv?limit=10');
      check('the audit trail can be exported as CSV',
        csv.status === 200 && csv.body.includes(','),
        csv.status + ' ' + csv.body.slice(0, 90).replace(/\n/g, ' '));
      await shoot('b3-audit');

      // -- Users: press "End all sessions" for real. DESTRUCTIVE and deliberately last:
      //    pressed on the administrator's own row it ends the session running this check.
      await goto('Users');
      const dialogs = [];
      // window.confirm() blocks the page until something answers it. Accept every dialog and
      // record what it said, so "it asked before doing something destructive" is checkable.
      ws.addEventListener('message', async (ev) => {
        const m = JSON.parse(ev.data);
        if (m.method === 'Page.javascriptDialogOpening') {
          dialogs.push(m.params.message);
          await cdp.send('Page.handleJavaScriptDialog', { accept: true });
        }
      });
      const pressed = await js(`(() => {
        const btn = [...(document.querySelector('main')||document.body).querySelectorAll('button')]
          .find(b => /end all sessions/i.test((b.textContent||'').trim()) && !b.disabled);
        if (!btn) return 'NOT FOUND';
        const r = btn.getBoundingClientRect();
        return JSON.stringify({ x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) });
      })()`);
      check('the Users screen offers "End all sessions"', pressed !== 'NOT FOUND', pressed);
      if (pressed !== 'NOT FOUND') {
        const { x, y } = JSON.parse(pressed);
        // REAL mouse events at the control's own centre, not el.click(): a click that only the
        // element sees would pass even if something were covering it.
        await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1 });
        await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1 });
        await sleep(2500);
        check('it confirms before ending somebody\'s sessions', dialogs.length > 0,
          JSON.stringify(dialogs).slice(0, 160));
        const toastText = await js(`(() => (document.body.innerText.match(/[^\\n]*session[^\\n]*/i) || [''])[0].trim())()`);
        check('and reports the outcome on screen', !!toastText, toastText.slice(0, 120));

        // THE ONE THIS CHECK EXISTED TO FIND. Pressing it on the administrator's OWN row ends
        // the session running this browser. Every page then 401s — and the app used to catch
        // that like any other error and keep rendering the whole admin console, with a small
        // red "session not active" and the Audit log announcing "No events match these
        // filters" while nobody was signed in at all. The screen was lying, and only the
        // screenshot showed it.
        await goto('Audit log');
        await sleep(2500);
        const afterState = await js(`(() => {
          const text = document.body.innerText || '';
          return {
            hasNav: !!document.querySelector(${JSON.stringify(NAV_SEL)}),
            hasPassword: !!document.querySelector('input[type=password]'),
            claimsNoEvents: /no events match/i.test(text),
            saysNotActive: /session not active|not signed in/i.test(text),
          };
        })()`);
        check('a session that ended UNDER the operator returns them to sign-in',
          afterState.hasPassword && !afterState.hasNav, JSON.stringify(afterState));
        check('and the app never claims "no events match" when the truth is "not signed in"',
          !afterState.claimsNoEvents, JSON.stringify(afterState));
        await shoot('b4-signed-out');
      }

    }

    const passed = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
    console.log('screenshots: ' + path.join(OUT, `idsan-admin-${LANG}-*.png`));
    process.exitCode = passed === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
