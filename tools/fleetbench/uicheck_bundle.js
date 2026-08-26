// Does this app's built SPA still MOUNT? A cheap check for the apps that have no live
// instance to drive.
//
// WHY THIS EXISTS. A frontend dependency bump changes the bundle, and a bundle can break in a
// way nothing else here notices: `npm audit` goes clean, webpack reports "compiled
// successfully", the files are written, the manifest is consistent — and the app throws at
// module scope in the browser and renders a white page. W3-1 shipped exactly that shape once
// (a build in which every design token resolved to nothing, with every screen check green),
// and the flagships have live screen checks to catch it. myidsan, myiotsan and mypintusan do
// not, because standing them up needs a database.
//
// So this serves the built static directory over plain HTTP, loads it in a real browser and
// asserts the three things that a broken bundle destroys:
//
//   1. nothing threw while the modules were evaluating;
//   2. React actually mounted something into #root;
//   3. the stylesheet loaded — the design tokens resolve.
//
// WHAT IT DOES NOT PROVE, and this matters: the app is served with NO BACKEND, so every API
// call fails and the screen reaches its signed-out or error state. That is expected and is not
// what is being measured. This says the bundle is loadable and the app boots; it says nothing
// about whether any feature works. The flagships' own screen checks are what say that.
//
//   node tools/fleetbench/uicheck_bundle.js <static-dir> <label>
const { spawn } = require('child_process');
const fs = require('fs');
const http = require('http');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const STATIC = path.resolve(process.argv[2] || '.');
const LABEL = process.argv[3] || path.basename(path.dirname(STATIC));
const PORT = Number(process.argv[4] || 8791);

const CHECKS = [];
function check(name, ok, detail) {
  CHECKS.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail) });
  console.log((ok ? 'PASS  ' : 'FAIL  ') + name + (detail !== undefined ? '   ' + detail : ''));
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

const TYPES = {
  '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png',
  '.ico': 'image/x-icon', '.woff2': 'font/woff2', '.woff': 'font/woff',
  '.map': 'application/json', '.txt': 'text/plain', '.webmanifest': 'application/json',
};

function serve() {
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const url = decodeURIComponent((req.url || '/').split('?')[0]);
      let file = path.join(STATIC, url);
      if (!file.startsWith(STATIC)) { res.writeHead(403).end(); return; }
      if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) {
        // The SPA fallback the app's own server does, so a deep link behaves the same here.
        file = path.join(STATIC, 'index.html');
      }
      if (!fs.existsSync(file)) { res.writeHead(404).end(); return; }
      res.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] || 'application/octet-stream' });
      fs.createReadStream(file).pipe(res);
    });
    srv.listen(PORT, '127.0.0.1', () => resolve(srv));
  });
}

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

function rpc(ws, onEvent) {
  let id = 0;
  const pending = new Map();
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); return; }
    if (msg.method && onEvent) onEvent(msg);
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

(async () => {
  const srv = await serve();
  const port = 9250;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(require('os').tmpdir(), 'chrome-bundle-' + LABEL)}`,
    '--no-first-run', '--no-default-browser-check', '--window-size=1400,1000', 'about:blank',
  ], { stdio: 'ignore' });

  // Everything the page threw or logged as an error, kept whole. A summarised console is a
  // console that hides the one line naming the missing export.
  const thrown = [];
  const consoleErrors = [];
  const failedRequests = [];

  try {
    const ws = new WebSocket(await connect(port));
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws, (msg) => {
      if (msg.method === 'Runtime.exceptionThrown') {
        const d = msg.params?.exceptionDetails || {};
        thrown.push(d.exception?.description || d.text || JSON.stringify(d).slice(0, 200));
      }
      if (msg.method === 'Runtime.consoleAPICalled' && msg.params?.type === 'error') {
        consoleErrors.push((msg.params.args || []).map((a) => a.value || a.description || '').join(' ').slice(0, 300));
      }
      if (msg.method === 'Network.loadingFailed') {
        failedRequests.push(msg.params?.type + ' ' + (msg.params?.errorText || ''));
      }
    });
    await cdp.send('Runtime.enable');
    await cdp.send('Page.enable');
    await cdp.send('Network.enable');

    await cdp.send('Page.navigate', { url: `http://127.0.0.1:${PORT}/` });
    await sleep(7000);

    // 1. Nothing threw while the modules were evaluating. A bad upgrade — a renamed export, a
    //    package that no longer ships a CJS entry — lands here and nowhere else.
    check('the bundle evaluates without throwing', thrown.length === 0,
      thrown.slice(0, 2).join(' | ').slice(0, 300) || 'no exceptions');

    // 2. React mounted. An app that throws inside its first render leaves an empty #root, and
    //    the page still returns 200 with a perfectly valid HTML document.
    const mounted = JSON.parse(await cdp.send('Runtime.evaluate', {
      expression: `JSON.stringify({
        children: (document.getElementById('root') || {}).childElementCount || 0,
        text: ((document.getElementById('root') || {}).innerText || '').trim().length,
        title: document.title,
      })`, returnByValue: true,
    }).then((r) => r.result.value));
    check('React mounted something into #root — a bundle that throws in its first render '
      + 'leaves a valid, empty page', mounted.children > 0, JSON.stringify(mounted));

    // 3. The design tokens resolve. THE W3-1 GUARD: that build had a working bundle and a
    //    destroyed :root token block, and every check that only read the DOM passed.
    //
    //    THE TOKEN NAMES ARE DISCOVERED, NOT ASSUMED. The first version of this check
    //    hardcoded `--bg-body`/`--bg-surface`/`--text-primary`, which is mymatasan's and
    //    myseliasan's vocabulary — myidsan and mypintusan use the rbac-standard palette and
    //    call the same thing `--bg`. So it failed on two perfectly good builds: a check that
    //    fails on correct output, which is the fourth time this programme has produced one.
    //    What actually matters is vocabulary-independent — a destroyed token block resolves
    //    NOTHING, whatever the names were.
    const styled = JSON.parse(await cdp.send('Runtime.evaluate', {
      expression: `(() => {
        const declared = new Set();
        for (const sheet of document.styleSheets) {
          let rules;
          try { rules = sheet.cssRules; } catch (_) { continue; } // cross-origin, not our CSS
          for (const rule of rules || []) {
            if (!rule.style || !rule.selectorText) continue;
            // Plain string comparison rather than a regex. A selector list is
            // comma-separated and each part is a whole selector, so splitting says exactly
            // what is meant — and it cannot be broken by an escape surviving one layer of
            // quoting but not the next, which is how the first attempt ended up testing
            // against a literal backspace character and finding nothing at all.
            const parts = rule.selectorText.split(',').map((p) => p.trim());
            if (!parts.some((p) => p === ':root' || p === 'html' || p === 'body')) continue;
            for (const prop of rule.style) if (prop.startsWith('--')) declared.add(prop);
          }
        }
        const root = getComputedStyle(document.documentElement);
        const names = [...declared];
        const resolved = names.filter((n) => root.getPropertyValue(n).trim());
        return JSON.stringify({
          sheets: document.styleSheets.length,
          declared: names.length,
          resolved: resolved.length,
          unresolved: names.filter((n) => !root.getPropertyValue(n).trim()).slice(0, 5),
          bodyBg: getComputedStyle(document.body).backgroundColor,
        });
      })()`, returnByValue: true,
    }).then((r) => r.result.value));
    check('the stylesheet loaded and every design token it declares resolves',
      styled.sheets > 0 && styled.declared > 0 && styled.unresolved.length === 0,
      JSON.stringify(styled));
    check('the page is actually painted',
      !!styled.bodyBg && styled.bodyBg !== 'rgba(0, 0, 0, 0)', 'body background = ' + styled.bodyBg);

    // Console errors are reported but only FAIL on the ones that mean a broken module. With no
    // backend behind it, the app legitimately logs failed API calls, and treating those as a
    // failure would make this check useless exactly when it is most needed.
    const moduleErrors = consoleErrors.filter((e) =>
      /is not a function|is not defined|Cannot read|undefined is not|Failed to resolve|does not provide an export|Unexpected token/i.test(e));
    check('no error that means a broken module reached the console', moduleErrors.length === 0,
      moduleErrors.slice(0, 2).join(' | ').slice(0, 300) || 'none');
    if (consoleErrors.length) {
      console.log('   (note: ' + consoleErrors.length + ' console error(s), expected with no '
        + 'backend behind the page: ' + consoleErrors[0].slice(0, 90) + ')');
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    const out = path.join(require('os').tmpdir(), 'bundle-' + LABEL + '.png');
    fs.writeFileSync(out, Buffer.from(shot.data, 'base64'));
    console.log('screenshot: ' + out);
  } finally {
    chrome.kill();
    srv.close();
  }

  const passed = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${passed}/${CHECKS.length} checks passed (${LABEL})`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exitCode = passed === CHECKS.length ? 0 : 1;
})().catch((e) => { console.error(e); process.exit(1); });
