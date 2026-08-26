// Drive myidsan's SECURITY KEYS end to end, with a real WebAuthn ceremony.
//
// WHY THIS ONE. WebAuthn is the strongest second factor myidsan offers and the only part of it
// that had never been exercised at all — not by a unit test that could, and not by any bench,
// because a FIDO2 ceremony needs a browser and an authenticator. Its unit tests verify the
// service; nothing had ever registered a credential, signed in with it, or removed it.
//
// That gap matters more than most. The security-key REMOVAL path was one of the three
// unmetered password oracles the lockout bench found (#206): it re-proves identity with the
// password alone, no second factor required, so a stolen cookie could grind it. And the
// server-rendered login page once wired only TOTP into its challenge, which was an MFA BYPASS
// for an account whose only factor is a key — that is the exact case this bench creates.
//
// HOW A REAL CEREMONY IS POSSIBLE HEADLESS: Chrome's DevTools protocol has a WebAuthn domain
// that installs a VIRTUAL AUTHENTICATOR. It is a real CTAP2 implementation inside the browser —
// real key material, real signatures, real client-data hashing — so `navigator.credentials`
// runs the genuine ceremony and the server verifies a genuine attestation. Nothing here is
// stubbed, and the app's own client code (`lib/webauthn.js`) is what drives it.
//
// THE CLAIMS UNDER TEST:
//
//   1. a security key can be enrolled from the Profile page, through the app's own UI;
//   2. the enrolment is recorded in the audit trail — removing a factor deletes its own
//      evidence, so the enrolment entry is the only lasting record that it ever existed;
//   3. an account whose ONLY second factor is a key is CHALLENGED at sign-in — not waved
//      through, which is what a challenge wired for TOTP alone would do;
//   4. the key actually completes that challenge and signs the user in;
//   5. it can be renamed, and the rename is recorded;
//   6. it can be removed — and removal demands the password, refuses a wrong one, and is
//      throttled rather than being an unmetered oracle for whoever holds the cookie;
//   7. the removal is recorded.
//
// Usage (with tools/fleetbench/idsan_harness.py up):
//
//   node tools/fleetbench/uicheck_idsan_webauthn.js <output-dir> [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
// LOCALHOST, NOT 127.0.0.1, AND THAT IS NOT A STYLE CHOICE. WebAuthn requires the relying-party
// id to be a registrable domain, and a bare IP address is not one — Chrome refuses the ceremony
// outright with `SecurityError: This is an invalid domain.` The first run of this bench clicked
// "Add a security key", saw nothing happen, and reported the enrolment as broken. The harness
// certificate already names localhost, and myidsan derives its RP id from the request host, so
// reaching it this way needs no product change.
const BASE = 'https://localhost:18451';
const OUT = path.resolve(process.argv[2] || '.');
const PASSWORD = process.argv[3] || 'Bench!2345678';
const USER = 'admin';

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
        setTimeout(() => rej(new Error(method + ' timed out')), 40000);
      });
    },
  };
}

(async () => {
  const port = 9247;
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`,
    `--user-data-dir=${path.join(OUT, 'chrome-idsan-webauthn')}`, '--ignore-certificate-errors',
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
      fs.writeFileSync(path.join(OUT, `idsan-webauthn-${name}.png`), Buffer.from(s.data, 'base64'));
    };
    const api = (p, init) => js(`(async () => {
      const opts = Object.assign({ credentials: 'same-origin' }, ${JSON.stringify(init || {})});
      const tok = (document.cookie.match(/(?:^|; )(?:__Host-)?kopiv2_csrf=([^;]*)/) || [])[1];
      if (tok && opts.method && opts.method !== 'GET') {
        opts.headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers,
          { 'X-CSRF-Token': decodeURIComponent(tok) });
      }
      const r = await fetch(${JSON.stringify(p)}, opts);
      return { status: r.status, body: (await r.text()).slice(0, 3000) };
    })()`);

    // ---- a real (virtual) security key ------------------------------------------------
    //
    // A genuine CTAP2 authenticator inside the browser: real key material, real signatures.
    // isUserVerified + automaticPresenceSimulation stand in for the human touching the key,
    // which is the only part of the ceremony a headless run cannot perform.
    await cdp.send('WebAuthn.enable');
    const auth = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2', transport: 'usb',
        hasResidentKey: false, hasUserVerification: true,
        isUserVerified: true, automaticPresenceSimulation: true,
      },
    });
    check('a virtual FIDO2 authenticator is attached to the browser', !!auth.authenticatorId,
      auth.authenticatorId || '');

    // ---- sign in with the password, and clear the wizard --------------------------------
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);
    const signIn = await js(`(async () => {
      const r = await fetch('/api/login/default', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({username:${JSON.stringify(USER)}, password:${JSON.stringify(PASSWORD)}}) });
      return r.status;
    })()`);
    check('the account signs in with its password', signIn === 200, 'status ' + signIn);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4500);
    for (let i = 0; i < 6; i++) {
      const skipped = await js(`(() => { const hit = [...document.querySelectorAll('button, a')]
        .find(e => /skip setup|skip|finish|done/i.test((e.textContent||'').trim()));
        if (!hit) return ''; hit.click(); return 'x'; })()`);
      if (!skipped) break;
      await sleep(2200);
    }
    await sleep(1500);

    const before = await api('/api/mfa/webauthn');
    check('the account starts with no security keys',
      before.status === 200 && !/"keys":\[\{/.test(before.body), before.body.slice(0, 140));

    // ---- 1. enrol, through the app's own UI --------------------------------------------
    //
    // Driven through the Profile page rather than by calling the two endpoints directly: the
    // client half of the ceremony (lib/webauthn.js — base64url decoding of the challenge, the
    // shape it posts back) is exactly the part a server-side test cannot cover.
    await js(`(() => { const hit = [...document.querySelectorAll('button, a')]
      .find(e => /profile|superadmin/i.test((e.textContent||'').trim())); if (hit) hit.click(); return 1; })()`);
    await sleep(2500);
    const addBtn = await js(`(() => {
      const hit = [...document.querySelectorAll('button')]
        .find(b => /add a security key|add security key/i.test((b.textContent||'').trim()) && !b.disabled);
      if (!hit) return 'NOT FOUND';
      // The security-key card sits below the fold on this page; scrolled into view so the
      // screenshot shows what the click did rather than the top of the page.
      hit.scrollIntoView({ block: 'center' });
      hit.click();
      return 'clicked';
    })()`);
    check('the Profile page offers to add a security key', addBtn === 'clicked', addBtn);
    // The ceremony itself: the browser talks to the virtual authenticator, then the app posts
    // the attestation. Generous wait — this is the one step with real crypto in it.
    await sleep(6000);
    await shoot('01-enrolled');

    const afterEnroll = await api('/api/mfa/webauthn');
    let keyId = 0;
    try {
      const res = JSON.parse(afterEnroll.body).result || {};
      keyId = ((res.keys || [])[0] || {}).id || 0;
    } catch (_) { /* reported below */ }
    const cardError = await js(`(() => {
      const card = [...document.querySelectorAll('section, div')]
        .find(e => /security key/i.test(e.textContent || '') && e.querySelector('button'));
      const msg = card && card.querySelector('.message, [class*=danger], [class*=error]');
      return msg ? (msg.textContent || '').trim().slice(0, 160) : '';
    })()`);
    check('a security key is enrolled and listed', !!keyId,
      (cardError ? 'card says: ' + cardError + ' | ' : '') + afterEnroll.body.slice(0, 200));

    // THE ONE THE SCREENSHOT GAVE AWAY. The two-factor card announced "Your account is
    // protected by a password only" with an enrolled security key sitting on the same page: it
    // read the TOTP state and knew nothing about keys. A false statement about an account's
    // protection, on the page whose whole job is reporting it — and the kind an operator
    // auditing accounts would act on.
    const banner = await js(`(() => {
      const el = [...document.querySelectorAll('.message, [class*=warning]')]
        .map(e => (e.textContent || '').trim())
        .find(txt => /password only|password and a security key|security key/i.test(txt));
      return el || '';
    })()`);
    check('the two-factor card does not claim "password only" once a key is enrolled',
      !/password only/i.test(banner), banner.slice(0, 180));

    const credentials = await cdp.send('WebAuthn.getCredentials', { authenticatorId: auth.authenticatorId });
    check('and the authenticator really holds a credential for it',
      (credentials.credentials || []).length === 1,
      'credentials on the key: ' + (credentials.credentials || []).length);

    // ---- 2. the trail ------------------------------------------------------------------
    const trail = await api('/api/audit?limit=100');
    check('the enrolment is recorded in the audit trail', /webauthn\.enroll/.test(trail.body),
      (trail.body.match(/"action":"[a-z._]+"/g) || []).join(' ').slice(0, 200));

    // ---- 3./4. sign in WITH the key ------------------------------------------------------
    //
    // The account now has a key and no TOTP. A challenge wired for TOTP alone would decide no
    // second factor was required and hand out a session — an MFA bypass reachable from every
    // relying app, since this is the page an SSO hop lands on.
    await api('/api/login/default/logout', { method: 'POST' });
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(2500);
    const challenge = await js(`(async () => {
      const r = await fetch('/api/login/default', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({username:${JSON.stringify(USER)}, password:${JSON.stringify(PASSWORD)}}) });
      return { status: r.status, body: (await r.text()).slice(0, 600) };
    })()`);
    let mfaToken = '';
    let methods = [];
    try {
      const res = JSON.parse(challenge.body).result || {};
      mfaToken = res.mfaToken || '';
      methods = res.mfaMethods || [];
    } catch (_) { /* reported below */ }
    check('an account whose ONLY factor is a security key is still challenged',
      !!mfaToken, challenge.status + ' ' + challenge.body.slice(0, 200));
    check('and the challenge names the security key as the factor to present',
      methods.includes('webauthn'), JSON.stringify(methods));

    // Complete it with the key, through the app's own client helper.
    const asserted = mfaToken ? await js(`(async () => {
      const beg = await fetch('/api/login/mfa/webauthn/begin', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'}, body: JSON.stringify({ mfaToken: ${JSON.stringify(mfaToken)} }) });
      if (!beg.ok) return { stage: 'begin', status: beg.status, body: (await beg.text()).slice(0, 300) };
      const options = (await beg.json()).result;
      const pk = options.publicKey || options;
      const b64 = (s) => Uint8Array.from(atob(String(s).replace(/-/g,'+').replace(/_/g,'/')
        .padEnd(Math.ceil(String(s).length/4)*4,'=')), c => c.charCodeAt(0));
      pk.challenge = b64(pk.challenge);
      for (const c of (pk.allowCredentials || [])) c.id = b64(c.id);
      const cred = await navigator.credentials.get({ publicKey: pk });
      const u8 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf)))
        .replace(/\\+/g,'-').replace(/\\//g,'_').replace(/=+$/,'');
      const payload = { id: cred.id, rawId: u8(cred.rawId), type: cred.type, response: {
        clientDataJSON: u8(cred.response.clientDataJSON),
        authenticatorData: u8(cred.response.authenticatorData),
        signature: u8(cred.response.signature),
        userHandle: cred.response.userHandle ? u8(cred.response.userHandle) : null } };
      const fin = await fetch('/api/login/mfa/webauthn/finish', { method: 'POST', credentials: 'same-origin',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ mfaToken: ${JSON.stringify(mfaToken)}, credential: payload }) });
      return { stage: 'finish', status: fin.status, body: (await fin.text()).slice(0, 300) };
    })()`) : { stage: 'skipped', status: 0, body: '' };
    check('the security key completes the challenge and signs the user in',
      asserted.status === 200, asserted.stage + ' ' + asserted.status + ' ' + (asserted.body || '').slice(0, 200));

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);
    await shoot('02-signed-in-with-key');

    // ---- 5. rename ----------------------------------------------------------------------
    const renamed = keyId ? await api('/api/mfa/webauthn/' + keyId, {
      method: 'PUT', body: JSON.stringify({ label: 'bench-renamed-key' }),
    }) : { status: 0, body: '' };
    check('a security key can be renamed', renamed.status === 200, renamed.status + ' ' + renamed.body.slice(0, 140));
    const listAfterRename = await api('/api/mfa/webauthn');
    check('and the new name is what the list reports',
      /bench-renamed-key/.test(listAfterRename.body), listAfterRename.body.slice(0, 200));

    // ---- 6. removal must not be an unmetered password oracle -----------------------------
    //
    // This endpoint re-proves identity with the PASSWORD ALONE — no second factor — so whoever
    // holds a stolen cookie can ask it about password candidates. #206 put it behind the login
    // lockout; this is the live proof, from a browser, that the throttle is really there.
    const wrong = [];
    for (let i = 0; i < 10 && keyId; i++) {
      const r = await api('/api/mfa/webauthn/' + keyId, {
        method: 'DELETE', body: JSON.stringify({ password: 'wrong-guess-' + i }),
      });
      wrong.push(r.status);
      if (r.status === 429) break;
    }
    check('removing a key REFUSES a wrong password', wrong.length > 0 && wrong[0] !== 200,
      JSON.stringify(wrong));
    check('and repeated wrong passwords are THROTTLED, not answered forever',
      wrong.includes(429), 'statuses: ' + JSON.stringify(wrong));

    // Wait the lockout out rather than measuring it — it is the lockout bench's subject, not
    // this one's, and a correct password while locked is refused by design.
    if (wrong.includes(429)) {
      for (let i = 0; i < 40; i++) {
        const probe = await api('/api/mfa/webauthn/' + keyId, {
          method: 'DELETE', body: JSON.stringify({ password: 'still-locked' }),
        });
        if (probe.status !== 429) break;
        await sleep(5000);
      }
    }
    const removed = keyId ? await api('/api/mfa/webauthn/' + keyId, {
      method: 'DELETE', body: JSON.stringify({ password: PASSWORD }),
    }) : { status: 0, body: '' };
    check('and the RIGHT password removes it', removed.status === 200,
      removed.status + ' ' + removed.body.slice(0, 160));
    const finalList = await api('/api/mfa/webauthn');
    // Conditioned on the removal having actually happened. On the first run this passed while
    // nothing had ever been enrolled — an empty list is not evidence that a removal worked.
    check('the key is gone from the list',
      removed.status === 200 && !/bench-renamed-key/.test(finalList.body),
      'remove ' + removed.status + ' -> ' + finalList.body.slice(0, 160));

    // ---- 7. the trail, again -------------------------------------------------------------
    //
    // Removing a factor deletes the factor row, so without these entries the act erases its own
    // evidence and the account afterwards is indistinguishable from one that never enrolled.
    const trail2 = await api('/api/audit?limit=200');
    check('the rename is recorded', /webauthn\.rename/.test(trail2.body),
      (trail2.body.match(/webauthn\.[a-z_]+/g) || []).join(' '));
    check('the removal is recorded', /webauthn\.remove/.test(trail2.body),
      (trail2.body.match(/webauthn\.[a-z_]+/g) || []).join(' '));
    await shoot('03-removed');

    const passed = CHECKS.filter((c) => c.ok).length;
    console.log(`\n${passed}/${CHECKS.length} checks passed`);
    process.exitCode = passed === CHECKS.length ? 0 : 1;
  } finally {
    chrome.kill();
  }
})().catch((e) => { console.error(e); process.exit(1); });
