// Drive myseliasan's Settings → Notifications section in headless Chrome.
//
// WHY A SECOND SCREEN CHECK. uicheck.js drives the objects search page and is shaped around
// it. This one exists for the same reason that one does — W2-4 shipped a green API and a
// screen that lied — but the failures a SETTINGS screen has are different ones:
//
//   * A missing translation renders as its own key ("settings.field.smtpHost"), which every
//     API test in the world passes straight over. This item added 46 keys across four
//     dictionaries; one typo and a whole card reads as gibberish in one language only.
//   * A secret pre-filled into the form is a real leak, and it looks like nothing.
//   * A control that is simply absent (a group whose action never rendered) is invisible
//     from the API side by definition.
//
// Usage (with the fleet from fleet_harness.py up, and the notification section saved):
//
//   node tools/fleetbench/uicheck_settings.js <output-dir> [lang] [password]
//
// Prints a JSON summary to assert on, and writes a screenshot for a human afterwards.
// ASSERT ON THE JSON — a screenshot you have to squint at is not an assertion.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18443';
// Absolute: Chrome silently refuses a relative --user-data-dir and never opens the
// devtools port, which surfaces only as "devtools never came up".
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const PASSWORD = process.argv[4] || 'Bench!2345';

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
        setTimeout(() => rej(new Error(method + ' timed out')), 30000);
      });
    },
  };
}

(async () => {
  const port = 9224;
  const profile = path.join(OUT, 'chrome-profile-settings');
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
    console.log('login status:', login);

    // Pin the language before the shell renders, so the dictionary under test is the
    // one that loads. Arabic also flips the layout to RTL, which is the case the
    // suite's logical CSS properties exist for and the one nobody looks at.
    // THE KEY MUST BE THE APP'S OWN ('myseliasan_lang', App.js LANG_KEY). Writing a
    // made-up key silently changes nothing: the page renders in English and the run
    // reports "renders in ar" having never switched language — passing for exactly
    // the reason it was written to catch. The assertion below now proves the switch
    // actually happened rather than trusting the write.
    await evalJs(`localStorage.setItem('myseliasan_lang', ${JSON.stringify(LANG)}), 1`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(5000);

    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|finish|done|تخطّي|لنگکاو/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      console.log('wizard: clicked ' + skipped);
      await sleep(2500);
    }
    await sleep(1500);

    // Reach Settings, then the Notifications tab. Both are clicked by their RENDERED
    // label rather than a route, because the point is to prove a human can get there.
    const nav = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('button, a')]
        .find((e) => /setting|tetapan|设置|الإعدادات/i.test(e.textContent || ''));
      if (!hit) return 'NO SETTINGS NAV: ' + [...document.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,25).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log(nav);
    await sleep(3000);

    // SCOPE THE CLICK TO THE SETTINGS TAB BAR. A page-wide search for "Notifications"
    // finds the nav rail's notification FEED first (it renders as "Notifications10",
    // label plus unread count) and navigates away from Settings entirely — after which
    // every assertion below reports an empty page and reads as "the section did not
    // render". The settings tabs are the shared Tabs component: nav.ui-tabs > [role=tab].
    const tab = await evalJs(`(() => {
      const bar = document.querySelector('.ui-tabs[role=tablist]');
      if (!bar) return 'NO SETTINGS TAB BAR — not on the settings page';
      const tabs = [...bar.querySelectorAll('[role=tab]')];
      const hit = tabs.find((e) => /notification|pemberitahuan|通知|الإشعارات/i.test(e.textContent || ''));
      if (!hit) return 'NO NOTIFICATION TAB: ' + tabs.map(e=>e.textContent.trim()).filter(Boolean).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log(tab);
    await sleep(2500);

    const summary = await evalJs(`(() => {
      const text = document.body.innerText || '';
      // A key that reached the screen instead of a translation. This is the whole
      // reason this check exists: it is invisible to every API assertion.
      const rawKeys = (text.match(/\\bsettings\\.[a-zA-Z0-9_.]+/g) || []);
      // The label text is whatever the <label> renders ABOVE its control. Reading
      // childNodes[0] misses it whenever the label wraps a FieldTitle, which most of
      // these do — and an empty array here is a check that silently checks nothing.
      const labels = [...document.querySelectorAll('.settings-card label')]
        .map((e) => ((e.textContent || '').split(String.fromCharCode(10))[0] || '').trim())
        .filter(Boolean);
      const cardTitles = [...document.querySelectorAll('.settings-card-title')]
        .map((e) => e.textContent.trim()).filter(Boolean);
      const passwords = [...document.querySelectorAll('input[type=password]')]
        .map((e) => ({ hasValue: !!e.value, len: (e.value || '').length }));
      const testBtn = [...document.querySelectorAll('button')]
        .find((e) => /test email|e-mel ujian|测试邮件|بريد تجريبي/i.test(e.textContent || ''));
      return {
        dir: document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction,
        rawKeysOnScreen: [...new Set(rawKeys)].slice(0, 10),
        cardTitles,
        labels,
        passwordFields: passwords,
        testButton: testBtn ? testBtn.textContent.trim() : null,
        mentionsRelay: /smtp/i.test(text),
      };
    })()`);

    console.log('SUMMARY ' + JSON.stringify(summary, null, 2));

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    const file = path.join(OUT, 'settings-notification-' + LANG + '.png');
    fs.writeFileSync(file, Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + file);

    // Exit non-zero on the failures worth failing a run for.
    const bad = [];
    // A navigation that did not happen must fail here, not silently produce an empty
    // summary that reads like a rendering bug.
    if (!/^clicked:/.test(tab)) bad.push('never reached the notification tab: ' + tab);
    if (summary.rawKeysOnScreen.length) bad.push('untranslated keys on screen: ' + summary.rawKeysOnScreen.join(', '));
    if (!summary.mentionsRelay) bad.push('the mail relay card did not render');
    if (!summary.testButton) bad.push('the send-test-email control is absent');
    if (summary.passwordFields.some((p) => p.hasValue)) bad.push('a secret was pre-filled into the form');
    if (!summary.labels.length) bad.push('no field labels rendered — the section is empty or the selector is wrong');
    // Prove the language actually changed. Without this the non-English runs assert
    // nothing: an English page satisfies every check above.
    if (LANG !== 'en') {
      const ascii = summary.labels.every((l) => /^[ -]*$/.test(l));
      if (ascii) bad.push('the page is still in English — the language never switched');
      if (LANG === 'ar' && summary.dir !== 'rtl') bad.push('Arabic did not put the page in RTL (dir=' + summary.dir + ')');
    }
    if (bad.length) {
      console.log('FAIL ' + bad.join(' | '));
      process.exitCode = 1;
    } else {
      console.log('OK settings/notification renders in ' + LANG);
    }
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
