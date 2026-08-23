// Drive mymatasan's Settings → Notifications → email destination in headless Chrome.
//
// WHY THIS ONE TYPES. Every other check here reads a rendered page. This one dispatches
// REAL keystrokes into the recipients textarea, because the bug it exists to catch cannot
// be seen any other way:
//
//   The recipient list is stored as an array and edited as text. A controlled textarea whose
//   value is derived as `to.join('\n')` is IMPOSSIBLE TO TYPE A SECOND ADDRESS INTO — parsing
//   drops the empty entry the instant you press Enter, the re-render removes the newline, and
//   the caret jumps. The component renders, the API accepts, every unit test passes, and the
//   feature is unusable. Only real input finds it.
//
// Usage (with the fleet from fleet_harness.py up):
//
//   node tools/fleetbench/uicheck_mail_dest.js <output-dir> [password]
//
// Prints a JSON summary to assert on, and writes a screenshot for a human afterwards.
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';   // node-a (mymatasan)
// Absolute: Chrome silently refuses a relative --user-data-dir and never opens the
// devtools port, which surfaces only as "devtools never came up".
const OUT = path.resolve(process.argv[2] || '.');
const PASSWORD = process.argv[3] || 'Bench!2345';

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
  const port = 9225;
  const profile = path.join(OUT, 'chrome-profile-maildest');
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--window-size=1500,1200', 'about:blank',
  ], { stdio: 'ignore' });

  const bad = [];
  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await cdp.send('Input.enable').catch(() => {});

    const evalJs = async (expr) => {
      const r = await cdp.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) {
        throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      }
      return r.result.value;
    };

    // Type into whatever is focused. insertText is how a paste/IME commit arrives and is
    // what React's onChange sees; Enter needs a real key event, which is the whole point.
    const typeText = (text) => cdp.send('Input.insertText', { text });
    const pressEnter = async () => {
      for (const type of ['keyDown', 'char', 'keyUp']) {
        await cdp.send('Input.dispatchKeyEvent', {
          type, key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13,
          nativeVirtualKeyCode: 13, text: type === 'char' ? '\r' : undefined,
        });
      }
    };

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4500);

    // mymatasan's shell is a login FORM (it holds Basic credentials client-side), so
    // there is no cookie to plant — fill it the way a person would.
    const signedIn = await evalJs(`(() => {
      const user = document.querySelector('input[name=username], input[autocomplete=username], input[type=text]');
      const pass = document.querySelector('input[type=password]');
      if (!user || !pass) return 'NO LOGIN FORM';
      const set = (el, v) => {
        const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
        Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
        el.dispatchEvent(new Event('input', { bubbles: true }));
      };
      set(user, 'admin');
      set(pass, ${JSON.stringify(PASSWORD)});
      const btn = [...document.querySelectorAll('button')].find((b) => /sign in|log in|login|masuk/i.test(b.textContent || ''))
        || document.querySelector('button[type=submit]');
      if (!btn) return 'NO SUBMIT BUTTON';
      btn.click();
      return 'submitted';
    })()`);
    console.log('login: ' + signedIn);
    await sleep(6000);

    for (let i = 0; i < 4; i++) {
      const skipped = await evalJs(`(() => {
        const hit = [...document.querySelectorAll('button')].find((e) => /skip setup|skip|finish|done/i.test(e.textContent || ''));
        if (!hit) return '';
        hit.click();
        return hit.textContent.trim();
      })()`);
      if (!skipped) break;
      console.log('wizard: clicked ' + skipped);
      await sleep(2500);
    }
    await sleep(1500);

    const nav = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('button, a')].find((e) => /^\\s*settings\\s*$/i.test(e.textContent || ''))
        || [...document.querySelectorAll('button, a')].find((e) => /setting/i.test(e.textContent || ''));
      if (!hit) return 'NO SETTINGS NAV: ' + [...document.querySelectorAll('button,a')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log(nav);
    await sleep(3000);

    // Scope to the settings tab bar — a page-wide search finds the notification FEED
    // in the top bar and navigates away from Settings entirely.
    const tab = await evalJs(`(() => {
      const bar = document.querySelector('.ui-tabs[role=tablist]');
      if (!bar) return 'NO SETTINGS TAB BAR — not on the settings page';
      const tabs = [...bar.querySelectorAll('[role=tab]')];
      const hit = tabs.find((e) => /notification/i.test(e.textContent || ''));
      if (!hit) return 'NO NOTIFICATION TAB: ' + tabs.map(e=>e.textContent.trim()).filter(Boolean).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log(tab);
    if (!/^clicked:/.test(tab)) bad.push('never reached the notification tab: ' + tab);
    await sleep(2500);

    // Add an email destination and open its card.
    const added = await evalJs(`(() => {
      const hit = [...document.querySelectorAll('button')].find((e) => /add email/i.test(e.textContent || ''));
      if (!hit) return 'NO ADD-EMAIL BUTTON: ' + [...document.querySelectorAll('button')].map(e=>e.textContent.trim()).filter(Boolean).slice(0,25).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    console.log('add: ' + added);
    if (!/^clicked:/.test(added)) bad.push('the Add email control is absent: ' + added);
    await sleep(2000);

    // Focus the recipients textarea and TYPE two addresses separated by Enter.
    const focused = await evalJs(`(() => {
      const ta = document.querySelector('.dest-card textarea');
      if (!ta) return 'NO RECIPIENTS TEXTAREA';
      ta.focus();
      return 'focused';
    })()`);
    console.log('textarea: ' + focused);
    if (focused !== 'focused') {
      bad.push('the recipients textarea is absent: ' + focused);
    } else {
      await typeText('security@example.com');
      await sleep(300);
      await pressEnter();
      await sleep(300);
      await typeText('nightshift@example.com');
      await sleep(600);
    }

    const summary = await evalJs(`(() => {
      const text = document.body.innerText || '';
      // A key that reached the screen instead of a translation.
      const rawKeys = [...new Set(text.match(/\\bst\\.[a-zA-Z0-9_.]+/g) || [])].slice(0, 10);
      const ta = document.querySelector('.dest-card textarea');
      const relayHeading = [...document.querySelectorAll('h2')]
        .map((e) => e.textContent.trim()).find((s) => /mail relay/i.test(s)) || null;
      const smtpLabels = [...document.querySelectorAll('label')]
        .map((e) => (e.textContent || '').split(String.fromCharCode(10))[0].trim())
        .filter((s) => /smtp|starttls|from address/i.test(s));
      return {
        rawKeysOnScreen: rawKeys,
        recipientsValue: ta ? ta.value : null,
        relayHeading,
        smtpLabels,
        summaryRow: [...document.querySelectorAll('.accordion-muted')].map((e) => e.textContent.trim()).slice(0, 5),
        typeBadges: [...document.querySelectorAll('.class-source-badge')].map((e) => e.textContent.trim()).slice(0, 5),
      };
    })()`);
    console.log('SUMMARY ' + JSON.stringify(summary, null, 2));

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    const file = path.join(OUT, 'settings-email-destination.png');
    fs.writeFileSync(file, Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + file);

    const val = summary.recipientsValue || '';
    if (summary.rawKeysOnScreen.length) bad.push('untranslated keys on screen: ' + summary.rawKeysOnScreen.join(', '));
    if (!summary.relayHeading) bad.push('the mail relay panel did not render');
    if (!summary.smtpLabels.length) bad.push('no SMTP fields rendered');
    // THE ASSERTION THIS FILE EXISTS FOR.
    if (!val.includes('security@example.com')) bad.push('the first recipient was not accepted: ' + JSON.stringify(val));
    if (!val.includes('nightshift@example.com')) bad.push('a SECOND recipient could not be typed — the newline was eaten: ' + JSON.stringify(val));

    if (bad.length) {
      console.log('FAIL ' + bad.join(' | '));
      process.exitCode = 1;
    } else {
      console.log('OK the email destination card renders and accepts a typed recipient list');
    }
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
