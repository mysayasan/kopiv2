// Drive myseliasan's SIGN-IN SCREEN until it locks, in headless Chrome, in one language.
//
// WHY A SCREEN CHECK FOR THIS. The API bench (bench_myseliasan_lockout.py) proves the server
// locks and answers 429 with the remaining wait. It cannot see the half a locked-out operator
// actually experiences, and that half is where this suite's screen checks keep finding things:
//
//   * the server's refusal is ONE English sentence and this app ships in four languages, so
//     the countdown has to be rendered client-side from `retryAfterSeconds`. A wrong field
//     name, or an envelope unwrap that swallows the body, degrades silently to the generic
//     "Invalid username or password" — which tells a locked-out user to keep typing, and every
//     API assertion in the world still passes;
//   * a missing dictionary key renders as its own name ("auth.lockedSeconds"), which is
//     invisible from the server side by definition;
//   * `api()` bounces 401/403 to the SSO login unless `noRedirect` is set. A 429 must not be
//     bounced either, or the screen navigates away instead of showing anything.
//
// So this types REAL keystrokes into the REAL form (Input.dispatchKeyEvent, not value=), reads
// the message the user would read, and asserts three things about it: the countdown number is
// there, the text is not the untranslated key, and — in a non-English run — it is not the
// server's English sentence.
//
// Usage (with a myseliasan instance up; the port is the API bench's instance A):
//
//   node tools/fleetbench/uicheck_sel_lockout.js <output-dir> [lang] [base-url]
//
// Prints a JSON summary to assert on and writes a screenshot. ASSERT ON THE JSON — a
// screenshot you have to squint at is not an assertion.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const OUT = path.resolve(process.argv[2] || '.');
const LANG = (process.argv[3] || 'en').toLowerCase();
const BASE = process.argv[4] || 'https://127.0.0.1:18471';
// One more than the shipped maxAttempts, so the last submit is refused by the lockout rather
// than being the attempt that engages it — the guard is consulted before the credential, so
// the attempt that crosses the threshold still gets the ordinary credential error.
const ATTEMPTS = 10;

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail || '' });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail ? '   ' + detail : ''));
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
        setTimeout(() => rej(new Error(method + ' timed out')), 30000);
      });
    },
  };
}

(async () => {
  const port = 9231;
  const profile = path.join(OUT, 'chrome-profile-sel-lockout-' + LANG);
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1400,1000', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    const evalJs = async (expression) => {
      const r = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    // Pin the language BEFORE the shell renders, using the app's own key (App.js LANG_KEY).
    // A made-up key silently changes nothing and the run reports "renders in ar" having never
    // switched — passing for exactly the reason it was written to catch. The assertion at the
    // end proves the switch happened rather than trusting the write.
    await evalJs(`localStorage.setItem('myseliasan_lang', ${JSON.stringify(LANG)}), 1`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);

    const onLogin = await evalJs(`(() => {
      const pw = document.querySelector('input[type=password]');
      const user = document.querySelector('input[autocomplete=username]');
      return { hasForm: !!(pw && user), lang: document.documentElement.lang || '',
               dir: document.documentElement.dir || getComputedStyle(document.body).direction };
    })()`);
    check('the sign-in form is on screen', onLogin.hasForm, JSON.stringify(onLogin));
    if (!onLogin.hasForm) throw new Error('no login form to drive');

    // REAL keystrokes. Setting .value directly does not fire React's onChange, so the
    // component state stays empty and the form submits blank — which would fail for a reason
    // that has nothing to do with the lockout.
    const type = async (selector, text) => {
      await evalJs(`(() => { const el = document.querySelector(${JSON.stringify(selector)}); el.focus(); return 1; })()`);
      await evalJs(`(() => {
        const el = document.querySelector(${JSON.stringify(selector)});
        const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
        setter.call(el, '');
        el.dispatchEvent(new Event('input', { bubbles: true }));
        return 1;
      })()`);
      for (const ch of text) {
        // ONLY the char event carries `text`. Chrome inserts the character for a keyDown
        // that has `text` AND again for the char event, so the obvious three-event sequence
        // types everything TWICE — the first run of this check filled the username with
        // "aaddmmiinn" and still passed, because a nonexistent username is also a failed
        // sign-in and the per-IP key locked out anyway. It proved the lockout against an
        // account nobody was attacking. Hence the assertion below.
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown' });
        await cdp.send('Input.dispatchKeyEvent', { type: 'char', text: ch });
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp' });
      }
      const got = await evalJs(`document.querySelector(${JSON.stringify(selector)}).value`);
      if (got !== text) throw new Error(`typed ${JSON.stringify(text)} but the field holds ${JSON.stringify(got)}`);
    };

    const messageText = () => evalJs(`(() => {
      const el = document.querySelector('.login-panel .msg, .login-panel [class*=message], .login-panel [class*=msg]');
      if (el && el.textContent.trim()) return el.textContent.trim();
      // Fall back to the whole card: the point is what a human READS, not which node holds it.
      const panel = document.querySelector('.login-panel');
      return panel ? panel.innerText.replace(/\\s+/g, ' ').trim() : '';
    })()`);

    let seen = '';
    for (let i = 0; i < ATTEMPTS; i++) {
      await type('input[autocomplete=username]', 'admin');
      await type('input[type=password]', 'screencheck-wrong-' + i);
      await evalJs(`(() => { document.querySelector('.login-form button[type=submit]').click(); return 1; })()`);
      // The server delays every failed attempt on purpose (failedDelayMs), so this waits for
      // that plus the render rather than racing it.
      await sleep(1600);
      seen = await messageText();
      if (/\d/.test(seen) && !/auth\./.test(seen)) break;
    }

    const digits = (seen.match(/\d+/g) || []).map(Number);
    check('a locked-out user is shown a countdown, not "invalid username or password"',
      digits.length > 0 && digits.some((n) => n > 1), JSON.stringify(seen).slice(0, 300));
    check('the message is translated, not a raw dictionary key',
      !/auth\.locked|auth\.invalid/.test(seen), JSON.stringify(seen).slice(0, 300));
    if (LANG !== 'en') {
      // The server sends ONE English sentence. If it reaches the screen verbatim in another
      // language, the client-side translation never ran and three of four locales are broken.
      check('a non-English run does not show the server\'s English sentence',
        !/too many failed/i.test(seen), JSON.stringify(seen).slice(0, 300));
      check('the page really did switch language',
        (onLogin.lang || '').toLowerCase().startsWith(LANG) || /[^\x00-\x7F]/.test(seen),
        JSON.stringify({ htmlLang: onLogin.lang, seen: seen.slice(0, 120) }));
    }
    check('the screen did not navigate away — a 429 must not bounce to the SSO login',
      await evalJs(`!!document.querySelector('.login-panel')`), '');

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    const file = path.join(OUT, `sel-lockout-${LANG}.png`);
    fs.writeFileSync(file, Buffer.from(shot.data, 'base64'));
    console.log('screenshot:', file);

    const passed = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${passed}/${CHECKS.length} checks passed (${LANG})`);
    process.exitCode = passed === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
