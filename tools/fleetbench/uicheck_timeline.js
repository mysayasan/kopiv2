// Drive mymatasan's Timeline playback screen in headless Chrome against the live bench node.
//
// WHY THIS EXISTS, AND WHY IT PLAYS RATHER THAN LOOKS. W2-4's API bench passed 36/36 and
// the screen it shipped still lied. W3-1's API bench passes 29/29 — and every one of those
// checks is satisfied by a page that renders a beautiful bar and never shows a frame. The
// things that can only fail in a browser are:
//
//   * the <video> src authenticating (the tiles rely on the session cookie, not on the
//     Basic credentials the SPA holds in memory — a 401 here is invisible to the API bench);
//   * currentTime actually applying (setting it before loadedmetadata is silently discarded
//     by every browser, which lands playback at the START of the segment — a scrub to 14:52
//     that plays 14:45 and looks entirely plausible);
//   * playbackRate surviving a source change (the element resets it to 1 on every src swap,
//     so a 4x review quietly drops to real time at the first segment boundary);
//   * the cursor advancing as frames decode.
//
// So this check clicks, seeks, plays, changes speed, and asserts on numbers read back out
// of the live elements.
//
// Usage (with fleet_harness.py up and bench_w31_timeline.py already run):
//
//   node tools/fleetbench/uicheck_timeline.js <output-dir> [lang] [password]
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const CHROME = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const BASE = 'https://127.0.0.1:18444';   // node-a (mymatasan)
// Absolute: Chrome silently refuses a RELATIVE --user-data-dir and never opens the
// devtools port, which surfaces only as "devtools never came up".
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
  const events = [];
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
    else if (msg.method) events.push(msg);
  });
  return {
    events,
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
  let ctx = {};
  try {
    ctx = JSON.parse(fs.readFileSync(path.join(OUT, 'w31_context.json'), 'utf8'));
  } catch (_) {
    throw new Error('no w31_context.json in ' + OUT + ' — run bench_w31_timeline.py first');
  }

  const port = 9225;
  const profile = path.join(OUT, 'chrome-profile-tl');
  const chrome = spawn(CHROME, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`,
    '--ignore-certificate-errors', '--no-first-run', '--no-default-browser-check',
    '--autoplay-policy=no-user-gesture-required',
    '--window-size=1600,1200', 'about:blank',
  ], { stdio: 'ignore' });

  try {
    const wsUrl = await connect(port);
    const ws = new WebSocket(wsUrl);
    await new Promise((r) => ws.addEventListener('open', r));
    const cdp = rpc(ws);
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    await cdp.send('Log.enable');
    await cdp.send('Network.enable');

    const evalJs = async (expr) => {
      const r = await cdp.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('JS: ' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails));
      return r.result.value;
    };

    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(3000);

    // A language switch must be PROVEN, not assumed — set the key the app actually reads
    // and reload, then assert the rendered direction below.
    await evalJs(`(() => { try { localStorage.setItem('mymatasan_lang', ${JSON.stringify(LANG)}); } catch(_){} return 1; })()`);
    await cdp.send('Page.navigate', { url: BASE + '/' });
    await sleep(4000);

    // mymatasan's shell is a login FORM (it holds Basic credentials client-side), so
    // there is no cookie to plant — fill it the way a person would. The session cookie the
    // <video> tiles rely on is set by the server on the first authenticated request.
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

    // THE STYLESHEET IS LOADED AND ITS TOKENS RESOLVE.
    //
    // This check exists because W3-1 shipped with the whole ":root, .theme-light" token
    // block destroyed — an append wrote over the head of app.css — and this very script
    // passed 25/25 and 26/26 while it was broken. Every assertion here is on DOM text,
    // element geometry and video state, and a missing colour changes none of them. CSS has
    // no undefined-variable error either, only an empty substitution, so nothing upstream
    // failed. A screenshot was written each run for a human to glance at; nobody glanced.
    //
    // So: read the tokens back out of the live page. It is the cheapest possible check for
    // an entire class of silent stylesheet damage.
    const theme = await evalJs(`(() => {
      const root = getComputedStyle(document.documentElement);
      const body = getComputedStyle(document.body);
      const want = ['--bg-body','--bg-surface','--text-primary','--text-muted','--border-panel','--accent','--ok-text','--warn-bg','--ui-surface'];
      return JSON.stringify({
        missing: want.filter((t) => !root.getPropertyValue(t).trim()),
        bodyBg: body.backgroundColor,
        bodyColor: body.color,
      });
    })()`);
    const th = JSON.parse(theme);
    check('every design token in the stylesheet resolves', th.missing.length === 0,
      th.missing.length ? 'unresolved: ' + th.missing.join(', ') : theme);
    check('the page is actually painted', 
      !!th.bodyBg && th.bodyBg !== 'rgba(0, 0, 0, 0)' && th.bodyBg !== 'transparent',
      'body background = ' + th.bodyBg);

    const dir = await evalJs(`document.documentElement.getAttribute('dir') || getComputedStyle(document.body).direction`);
    if (LANG === 'ar') {
      check('the Arabic run really renders right-to-left', dir === 'rtl', 'dir=' + dir);
    }

    // --- reach the screen ---------------------------------------------------------
    // Scoped to the side nav: a page-wide text search can hit an unrelated control that
    // happens to carry the same word.
    const nav = await evalJs(`(() => {
      const rail = document.querySelector('.side-nav, nav, aside') || document;
      const items = [...rail.querySelectorAll('button, a')];
      const hit = items.find((e) => /timeline|garis masa|时间轴|الخط الزمني/i.test(e.textContent || ''));
      if (!hit) return 'NO TIMELINE NAV: ' + items.map(e=>e.textContent.trim()).filter(Boolean).slice(0,30).join(' | ');
      hit.click();
      return 'clicked: ' + hit.textContent.trim();
    })()`);
    check('the Timeline entry is in the navigation and opens', nav.startsWith('clicked'), nav);
    await sleep(4000);

    // --- the bar ------------------------------------------------------------------
    // The default window is one hour ending now, which is exactly where the bench's
    // footage is, so the bar should have something on it without any date fiddling.
    let bar = await evalJs(`(() => {
      const tracks = [...document.querySelectorAll('.tl-track')];
      return JSON.stringify({
        screen: !!document.querySelector('.tl-screen'),
        chips: [...document.querySelectorAll('.tl-camera-chip')].map(e=>e.textContent.trim()),
        tracks: tracks.length,
        spans: tracks.map(tr => tr.querySelectorAll('.tl-span').length),
        rowLabels: [...document.querySelectorAll('.tl-row-label')].map(e=>e.textContent.trim()),
        ticks: document.querySelectorAll('.tl-tick').length,
        error: (document.querySelector('.form-alert-msg')||{}).textContent || null,
      });
    })()`);
    let b = JSON.parse(bar);
    check('the timeline screen rendered', b.screen, bar);
    check('it did not fall back to an error banner', !b.error, String(b.error));
    check('the ruler is labelled', b.ticks >= 3, 'ticks=' + b.ticks);
    check('the first camera is on the bar with footage shaded on it',
      b.tracks >= 1 && b.spans[0] > 0, 'tracks=' + b.tracks + ' spans=' + JSON.stringify(b.spans));
    check('every configured camera is offered as a chip', b.chips.length >= 2, JSON.stringify(b.chips));

    // Add the second (gappy) camera — this is the multi-camera half.
    await evalJs(`(() => {
      const chips = [...document.querySelectorAll('.tl-camera-chip')];
      const off = chips.find(c => !c.classList.contains('is-on'));
      if (off) off.querySelector('input').click();
      return 1;
    })()`);
    await sleep(4000);

    bar = await evalJs(`(() => {
      const tracks = [...document.querySelectorAll('.tl-track')];
      return JSON.stringify({
        tracks: tracks.length,
        spans: tracks.map(tr => tr.querySelectorAll('.tl-span').length),
        tiles: document.querySelectorAll('.tl-tile').length,
        coverage: [...document.querySelectorAll('.tl-row-label em')].map(e=>e.textContent.trim()),
      });
    })()`);
    b = JSON.parse(bar);
    check('a second camera adds a second track and a second tile',
      b.tracks === 2 && b.tiles === 2, bar);
    // The whole point of the shading: the camera that lost its source must be drawn with
    // MORE THAN ONE run of footage, because the hole between them is the finding.
    check('the camera that lost its source is drawn with a visible hole in its footage',
      Math.max.apply(null, b.spans) >= 2, 'spans per track = ' + JSON.stringify(b.spans));
    check('each row states its own coverage', b.coverage.length === 2 && b.coverage.every(Boolean),
      JSON.stringify(b.coverage));

    // --- seek by clicking the bar --------------------------------------------------
    // A real click through CDP at real coordinates, not element.click(): the handler reads
    // clientX against the track's bounding box, so a synthetic click with no coordinates
    // would "pass" while landing at time zero.
    // Aim at the middle of a drawn RUN OF FOOTAGE, not at a fixed fraction of the window.
    // A fixed fraction lands in dead air whenever the footage does not fill the window,
    // and then this measures the gap-snap instead of the seek — which is a different
    // claim that is already checked further down.
    const box = await evalJs(`(() => {
      const tr = document.querySelector('.tl-track');
      const spans = [...tr.querySelectorAll('.tl-span')].map(s => s.getBoundingClientRect());
      if (!spans.length) return '';
      spans.sort((a, b) => b.width - a.width);
      const s0 = spans[0];
      return JSON.stringify({x: s0.left, y: s0.top, w: s0.width, h: s0.height});
    })()`);
    if (!box) throw new Error('no footage drawn on the first track to click into');
    const bb = JSON.parse(box);
    // Halfway along the widest run: far enough from either edge that a seek landing at
    // the segment start is unmistakable.
    const cx = Math.round(bb.x + bb.w * 0.5);
    const cy = Math.round(bb.y + bb.h / 2);
    for (const type of ['mousePressed', 'mouseReleased']) {
      await cdp.send('Input.dispatchMouseEvent', {
        type, x: cx, y: cy, button: 'left', clickCount: 1,
      });
    }
    await sleep(6000);

    let play = await evalJs(`(() => {
      const vids = [...document.querySelectorAll('video.tl-video')];
      return JSON.stringify({
        videos: vids.length,
        srcs: vids.map(v => (v.currentSrc || v.src || '').replace(/^https?:\\/\\/[^/]+/, '')),
        readyStates: vids.map(v => v.readyState),
        currentTimes: vids.map(v => Number(v.currentTime.toFixed(2))),
        durations: vids.map(v => (Number.isFinite(v.duration) ? Number(v.duration.toFixed(2)) : null)),
        errors: vids.map(v => (v.error ? v.error.code : 0)),
        networkStates: vids.map(v => v.networkState),
        readout: (document.querySelector('.tl-readout')||{}).textContent || null,
        notes: [...document.querySelectorAll('.tl-tile-note')].map(e=>e.textContent.trim()),
        empties: document.querySelectorAll('.tl-tile-empty').length,
      });
    })()`);
    let p = JSON.parse(play);
    console.log('AFTER SEEK: ' + play);

    check('clicking the bar points a tile at a segment',
      p.srcs.some(s => /\/api\/recording\/segments\/\d+\/download/.test(s)), JSON.stringify(p.srcs));
    // THE ONE THE API BENCH CANNOT MAKE. networkState 3 (NO_SOURCE) or a MEDIA_ERR means
    // the browser could not fetch the segment at all — which is what a cookie-auth failure
    // looks like from here, and which the API bench passes straight through.
    check('the browser can actually fetch the footage (no media error)',
      p.errors.every(e => !e), 'error codes = ' + JSON.stringify(p.errors));
    check('the footage decoded far enough to have metadata',
      p.readyStates.some(rs => rs >= 1), 'readyStates = ' + JSON.stringify(p.readyStates));
    // Setting currentTime before loadedmetadata is silently discarded, landing playback at
    // the start of the segment. A non-zero offset is the proof it was not.
    check('the seek landed inside the segment rather than at its start',
      p.currentTimes.some(ct => ct > 0.5), 'currentTimes = ' + JSON.stringify(p.currentTimes));
    check('the clock readout names the moment being shown', !!p.readout, String(p.readout));

    // THE ONE THAT CAUGHT THE SCRUB BUG. Everything above passes just as happily when the
    // click maps to the WRONG moment: the seek resolves, a segment loads, currentTime is
    // non-zero, the readout shows a time. The only way to see it is to ask whether the
    // cursor came to rest where the operator actually clicked — the bar hit-tested against
    // the wrapper while the spans and the cursor are positioned inside the track, which is
    // inset by the row label, so every scrub landed a fixed fraction of the window late.
    // 5.6% of the visible span here: three minutes on an hour, eighty on a day.
    const align = await evalJs(`(() => {
      const c = document.querySelector('.tl-cursor');
      if (!c) return '';
      const r = c.getBoundingClientRect();
      return JSON.stringify({ cursorX: r.left + r.width / 2 });
    })()`);
    check('the cursor comes to rest where the bar was clicked',
      !!align && Math.abs(JSON.parse(align).cursorX - cx) <= 4,
      align ? 'clicked x=' + cx + ' cursor x=' + Math.round(JSON.parse(align).cursorX)
              + ' off by ' + Math.round(JSON.parse(align).cursorX - cx) + 'px'
            : 'no cursor rendered');

    // --- play ---------------------------------------------------------------------
    const before = p.currentTimes.slice();
    const beforeReadout = p.readout;
    await evalJs(`(() => {
      const btn = [...document.querySelectorAll('.tl-transport button')]
        .find(b => b.classList.contains('primary'));
      if (btn) btn.click();
      return 1;
    })()`);
    await sleep(6000);
    play = await evalJs(`(() => {
      const vids = [...document.querySelectorAll('video.tl-video')];
      return JSON.stringify({
        currentTimes: vids.map(v => Number(v.currentTime.toFixed(2))),
        paused: vids.map(v => v.paused),
        rates: vids.map(v => v.playbackRate),
        readout: (document.querySelector('.tl-readout')||{}).textContent || null,
      });
    })()`);
    p = JSON.parse(play);
    console.log('AFTER PLAY: ' + play);
    // A tile that crosses a segment boundary during these six seconds restarts near zero
    // on the next clip, which is playback WORKING — so "currentTime went up" is the wrong
    // question. The claim is that the wall clock advances, and the readout is where that
    // is stated; currentTime rising is accepted as the simpler evidence when it applies.
    check('pressing play actually plays footage',
      p.currentTimes.some((ct, i) => ct > (before[i] || 0) + 1)
        || (p.readout && p.readout !== beforeReadout),
      'before=' + JSON.stringify(before) + ' after=' + JSON.stringify(p.currentTimes)
        + ' readout ' + beforeReadout + ' -> ' + p.readout);
    check('the clock readout advances with the frames',
      p.readout && p.readout !== beforeReadout, before + ' -> ' + p.readout);

    // --- speed --------------------------------------------------------------------
    await evalJs(`(() => {
      const btn = [...document.querySelectorAll('.tl-rate')].find(b => b.textContent.trim().startsWith('4'));
      if (btn) btn.click();
      return 1;
    })()`);
    await sleep(3000);
    const rates = await evalJs(`(() => {
      const vids = [...document.querySelectorAll('video.tl-video')];
      return JSON.stringify({
        rates: vids.map(v => v.playbackRate),
        on: [...document.querySelectorAll('.tl-rate.is-on')].map(e=>e.textContent.trim()),
      });
    })()`);
    const rr = JSON.parse(rates);
    check('the speed control reaches the video elements',
      rr.rates.every(r => r === 4), JSON.stringify(rr));

    // --- the gap, on screen ---------------------------------------------------------
    // Click where the gappy camera has no footage and read what the tile SAYS. A tile that
    // silently shows a later moment and a tile that says it skipped forward look the same
    // in a screenshot and mean completely different things.
    if (ctx.hole) {
      // PIN THE WINDOW FIRST. The default window ends at "now", so it slides while the
      // bench runs and the drawn gap drifts out from under a click computed a moment
      // earlier. A run that misses the hole reports "no note" — which is indistinguishable
      // from a broken note unless the click is made deterministic.
    // Pause first. Leaving playback running means the keep-the-cursor-visible follow is
    // live while the window is being moved, and the check would then be racing the player
    // rather than testing the control.
      await evalJs(`(() => {
        const btn = [...document.querySelectorAll('.tl-transport button')].find(b => b.classList.contains('primary'));
        if (btn && /pause|jeda|暂停|إيقاف/i.test(btn.textContent || '')) btn.click();
        return 1;
      })()`);
      await sleep(1500);
      await evalJs(`(() => {
        const sel = document.querySelector('.tl-window select');
        if (!sel) return '';
        Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(sel, '600');
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        return sel.value;
      })()`);
      await sleep(3000);
      const pinned = await evalJs(`(() => {
        const dt = document.querySelector('.tl-window input[type=datetime-local]');
        if (!dt) return 'NO WINDOW INPUT';
        const end = new Date((${ctx.hole[1]} + 150) * 1000);
        const pad = (n) => String(n).padStart(2, '0');
        const v = end.getFullYear() + '-' + pad(end.getMonth()+1) + '-' + pad(end.getDate())
          + 'T' + pad(end.getHours()) + ':' + pad(end.getMinutes());
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(dt, v);
        dt.dispatchEvent(new Event('input', { bubbles: true }));
        dt.dispatchEvent(new Event('change', { bubbles: true }));
        return v;
      })()`);
      console.log('pinned window to: ' + pinned);
      await sleep(5000);
      // PROVE the pin took. Returning the string we asked for is not evidence the
      // controlled inputs accepted it, and every failure downstream would then be read as
      // a broken bar rather than a scene that was never set.
      const pinState = await evalJs(`(() => {
        const sel = document.querySelector('.tl-window select');
        const dt = document.querySelector('.tl-window input[type=datetime-local]');
        const rows = [...document.querySelectorAll('.tl-row')];
        return JSON.stringify({
          span: sel ? sel.value : null,
          end: dt ? dt.value : null,
          spans: rows.map(r => r.querySelectorAll('.tl-span').length),
        });
      })()`);
      console.log('pin state: ' + pinState);
      const ps = JSON.parse(pinState);
      check('the window pinned around the hole', ps.span === '600' && ps.end === pinned, pinState);

      const gapBox = await evalJs(`(() => {
        const rows = [...document.querySelectorAll('.tl-row')];
        if (!rows.length) return '';
        const tr = rows[rows.length - 1].querySelector('.tl-track');
        const spans = [...tr.querySelectorAll('.tl-span')].map(s => s.getBoundingClientRect());
        if (spans.length < 2) return '';
        spans.sort((a, b) => a.left - b.left);
        let best = null;
        for (let i = 0; i < spans.length - 1; i++) {
          const w = spans[i+1].left - spans[i].right;
          if (!best || w > best.w) best = { x: spans[i].right, w, y: spans[i].top + spans[i].height / 2 };
        }
        return best && best.w > 8 ? JSON.stringify(best) : '';
      })()`);
      // RAISE rather than skip. A helper that returns nothing when it cannot set the
      // scene turns a missed setup into a passing check, which is how a bench comes back
      // green having tested nothing.
      check('the hole is drawn wide enough to aim at', !!gapBox,
        gapBox ? gapBox : 'no measurable gap between the drawn spans');
      if (gapBox) {
        const gb = JSON.parse(gapBox);
        const gx = Math.round(gb.x + gb.w / 2);
        const gy = Math.round(gb.y);
        for (const type of ['mousePressed', 'mouseReleased']) {
          await cdp.send('Input.dispatchMouseEvent', { type, x: gx, y: gy, button: 'left', clickCount: 1 });
        }
        await sleep(6000);
        const gapState = await evalJs(`(() => {
          return JSON.stringify({
            notes: [...document.querySelectorAll('.tl-tile-note')].map(e=>e.textContent.trim()),
            empties: document.querySelectorAll('.tl-tile-empty').length,
            srcs: [...document.querySelectorAll('video.tl-video')].map(v => (v.currentSrc||'').split('/api/')[1] || ''),
          });
        })()`);
        const gs = JSON.parse(gapState);
        console.log('AFTER GAP CLICK: ' + gapState);
        check('a tile that had to skip a hole says so on screen',
          gs.notes.length >= 1 && gs.notes.some(n => n.length > 0), JSON.stringify(gs.notes));
        // THE DEFECT THIS CHECK EXISTS FOR. The note used to round the gap to whole
        // minutes, so any hole under thirty seconds rendered as "skipped 0 minutes" — a
        // statement that nothing was skipped, printed on the one tile whose whole job is
        // to say that something was. Found in the Arabic run, where the same hole read
        // "تم التخطي 0 دقيقة".
        //
        // The emptiness guard is load-bearing: `[].every(...)` is true, so without it a
        // run that never reached the hole passes this having asserted nothing.
        check('the skipped amount is never rounded away to zero',
          gs.notes.length >= 1
            && gs.notes.every(n => !/(^|[^0-9])0\s*(s|min|h|ثانية|دقيقة|ساعة|秒|分钟|小时)/.test(n)),
          JSON.stringify(gs.notes));
      }
    }

    const shot = await cdp.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(OUT, `uicheck-timeline-${LANG}.png`), Buffer.from(shot.data, 'base64'));

    const errors = cdp.events
      .filter((e) => e.method === 'Log.entryAdded' && e.params.entry.level === 'error')
      .map((e) => e.params.entry.text);
    // Assert on the URLs, not on the console text: "404" with no URL names nothing and
    // cannot be acted on, which is how a broken request survives a screen check.
    //
    // /api/system/recovery/gate is EXPECTED to 404 and is not this screen's: the app
    // shell probes it before login to find out whether the appliance booted into
    // recovery mode, and in normal mode the route is deliberately not mounted. It is
    // named rather than filtered by status, so if anything else starts failing — or if
    // that probe ever stops being the only failure — this check says so.
    const SHELL_404 = '/api/system/recovery/gate';
    const failed = cdp.events
      .filter((e) => e.method === 'Network.responseReceived' && e.params.response.status >= 400)
      .map((e) => e.params.response.status + ' ' + e.params.response.url.replace(/^https?:\/\/[^/]+/, ''));
    const unexpected = failed.filter((f) => !f.endsWith(SHELL_404));
    check('every request the timeline screen made succeeded', unexpected.length === 0, JSON.stringify(failed.slice(0, 8)));
    // Only the shell's by-design recovery probe may be noisy in the console.
    const otherErrors = errors.filter((e) => !/404/.test(e));
    check('the screen produced no console errors beyond the shell recovery probe',
      otherErrors.length === 0, JSON.stringify(errors.slice(0, 6)));
    ws.close();
  } finally {
    chrome.kill();
  }

  const ok = CHECKS.filter((c) => c.ok).length;
  console.log(`\n${ok}/${CHECKS.length} screen checks passed`);
  for (const c of CHECKS) if (!c.ok) console.log('  FAILED: ' + c.name + '   ' + c.detail);
  process.exit(ok === CHECKS.length ? 0 : 1);
})().catch((e) => { console.error('FAILED: ' + e.message); process.exit(1); });
