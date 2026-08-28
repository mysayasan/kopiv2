// PART C of the myiotsan screen check: does what the SCREEN OFFERS match what the SERVER ALLOWS?
//
// THE REASON THIS EXISTS. myiotsan decides twice. The navigation rail hides Flows and Settings on
// a client-side `session.isAdmin` (components/layout.js), and the server decides independently
// with a deny-by-default permission matrix (services/rbac.go). Two mechanisms, one intent — which
// is the exact root cause the suite already recorded once: "nav uses isAdmin, server uses the
// matrix — two sources of truth."
//
// Both directions are faults, and they fail differently:
//
//   THE SCREEN OFFERS WHAT THE SERVER REFUSES -> a dead menu entry. The operator presses it, gets
//      an empty screen or a bare error, and has no way to tell "I am not allowed" from "this is
//      broken" or "there is no data". That ambiguity is the same shape as the defect #216 found
//      on a different axis, and it is the one users file bugs about.
//   THE SCREEN HIDES WHAT THE SERVER ALLOWS -> capability an operator paid for and cannot reach,
//      invisible to everyone including whoever granted the role.
//
// And myiotsan draws a line no other app in the suite draws: ACTUATION IS ADMIN-ONLY, on top of
// the per-device toggle, "because a bad relay write is physically dangerous in a way a bad PTZ
// move is not". An operator must not be able to move plant from this screen.
module.exports = async function partC(ctx) {
  const { js, api, press, clickAt, shoot, sleep, check, ROLE, SEED, NAV_SEL, navs } = ctx;

  const goto = async (label) => {
    await js(`(() => { const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
      .find(e => (e.textContent||'').trim() === ${JSON.stringify(label)}); if (hit) hit.click(); return 1; })()`);
    await sleep(2400);
  };
  const mainText = () => js(`(() => ((document.querySelector('main')||document.body).innerText||''))()`);
  const jsonBody = (r) => { try { return JSON.parse(r.body); } catch (_) { return null; } };
  // Both envelope shapes — see the note in part B. A one-shape unwrap turns a working list into
  // an empty one, and an empty list makes every permission check below pass for no reason.
  const unwrap = (r) => {
    const b = jsonBody(r);
    if (!b) return null;
    if (b.data && Object.prototype.hasOwnProperty.call(b.data, 'result')) return b.data.result;
    if (Object.prototype.hasOwnProperty.call(b, 'result')) return b.result;
    return b;
  };
  const items = (r) => { const v = unwrap(r); return (v && v.items) || (Array.isArray(v) ? v : []); };

  console.log(`\n--- PART C: what the ${ROLE}'s screen offers vs what the server allows ---`);

  // Each nav entry and the endpoint its screen cannot function without. If the rail offers the
  // entry, the server has to answer that call for this role.
  const SECTION_API = {
    Dashboard: '/api/devices/stats',
    Devices: '/api/devices?limit=5',
    Rules: '/api/rules',
    Alerts: '/api/alerts?limit=5',
    Notifications: '/api/notifications?limit=5',
    Scenes: '/api/scenes',
    Schedules: '/api/schedules',
    Flows: '/api/flows',
    'Device types': '/api/profiles',
    Settings: '/api/settings/users',
  };

  // ---- 1. every entry the rail OFFERS must work -------------------------------------------
  const offered = navs.filter((label) => SECTION_API[label]);
  const dead = [];
  for (const label of offered) {
    const r = await api(SECTION_API[label]);
    if (r.status === 401 || r.status === 403) dead.push(`${label} -> ${SECTION_API[label]} ${r.status}`);
  }
  check(`every section the ${ROLE}'s nav OFFERS is one the server actually allows`,
    dead.length === 0, dead.length ? dead.join(' | ') : offered.join(', '));

  // ---- 2. and the entries it HIDES are ones the server refuses -----------------------------
  const hidden = Object.keys(SECTION_API).filter((label) => !navs.includes(label));
  const wronglyHidden = [];
  for (const label of hidden) {
    const r = await api(SECTION_API[label]);
    if (r.status === 200) wronglyHidden.push(`${label} -> ${SECTION_API[label]} 200`);
  }
  check(`every section the ${ROLE}'s nav HIDES is one the server would refuse anyway`,
    wronglyHidden.length === 0,
    'hidden: ' + (hidden.join(', ') || 'none')
      + (wronglyHidden.length ? '; but the server ALLOWS ' + wronglyHidden.join(' | ') : ''));

  // The hiding has to actually be happening, or both checks above are vacuous. This app hides
  // Flows and Settings from a non-admin; if they were visible, check 1 would have caught it.
  check('the nav really is reduced for a non-admin (the checks above are not vacuous)',
    hidden.length > 0, 'hidden from ' + ROLE + ': ' + JSON.stringify(hidden));
  await shoot('c0-nav');

  // ---- 3. ACTUATION: admin-only, and the screen must not pretend otherwise -----------------
  await goto('Devices');
  const openPos = await js(`(() => {
    const row = [...document.querySelectorAll('tr')].find(r => (r.textContent||'').includes(${JSON.stringify(SEED.deviceName)}));
    if (!row) return null;
    const btn = [...row.querySelectorAll('button')].find(b => /open/i.test(b.textContent||''));
    if (!btn) return null;
    const r = btn.getBoundingClientRect();
    return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) };
  })()`);
  const canOpen = check(`a ${ROLE} can open a device`, !!openPos, JSON.stringify(openPos));

  if (canOpen) {
    await clickAt(openPos.x, openPos.y);
    await sleep(2500);
    const stageTabs = await js(`(() => [...(document.querySelector('main')||document.body).querySelectorAll('button')]
      .map(b => (b.textContent||'').trim()).filter(t => /^(readings|control|settings)$/i.test(t)))()`);
    console.log('    device stage tabs offered: ' + JSON.stringify(stageTabs));

    const offersControl = stageTabs.some((t) => /control/i.test(t));
    // Ask the server the same question, with a command the admin path proved works.
    const visible = items(await api('/api/devices?limit=200'));
    const dev = visible.find((d) => d.deviceKey === SEED.deviceKey);
    // Commands hang off the device; IssueRequest is {name, value}.
    const attempt = dev ? await api(`/api/devices/${dev.id}/commands`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'relay', value: 1 }),
    }) : { status: 0, body: 'no device visible to this role' };

    const serverRefuses = attempt.status === 401 || attempt.status === 403;
    check(`the server refuses actuation by a ${ROLE} (rbac.go makes it admin-only)`,
      serverRefuses, 'POST /api/commands -> ' + attempt.status + ' ' + attempt.body.slice(0, 160));

    if (offersControl) {
      await press('Control', { sel: 'button' });
      await sleep(2500);
      const control = await js(`(() => {
        const main = document.querySelector('main') || document.body;
        const text = main.innerText || '';
        const actuators = [...main.querySelectorAll('button')]
          .filter(b => /^(turn on|turn off|send)$/i.test((b.textContent||'').trim()));
        return {
          enabled: actuators.filter(b => !b.disabled).map(b => (b.textContent||'').trim()),
          disabled: actuators.filter(b => b.disabled).map(b => (b.textContent||'').trim()),
          explains: /permission|not allowed|admin|read-only|cannot|denied/i.test(text),
          text: text.slice(0, 300),
        };
      })()`);
      // THE ONE THAT MATTERS. If the server refuses and the screen still offers a live button,
      // the operator presses a control on a device that moves real plant and nothing happens —
      // with no way to learn whether they lack permission or the hub is broken.
      check(`...and the ${ROLE}'s Control tab does not offer a live actuation the server will refuse`,
        !serverRefuses || control.enabled.length === 0 || control.explains,
        JSON.stringify(control).slice(0, 380));
      await shoot('c1-control');
    } else {
      check(`the ${ROLE}'s device stage hides the Control tab entirely`, true,
        JSON.stringify(stageTabs));
    }

    // ---- 4. telemetry HISTORY: the viewer/operator line --------------------------------
    //
    // rbac.go: a viewer sees "devices and their current readings" but has "No access to the
    // historical record"; an operator may "review telemetry history". So the Readings tab means
    // something different for each — and the question is whether the screen SAYS which.
    await press('Readings', { sel: 'button' });
    await sleep(4000);
    const hist = dev ? await api(`/api/devices/${dev.id}/readings?key=temp&from=0&to=4000000000`) : { status: 0 };
    const latest = dev ? await api(`/api/devices/${dev.id}/latest`) : { status: 0 };
    const screen = await js(`(() => {
      const main = document.querySelector('main') || document.body;
      const text = main.innerText || '';
      return {
        charts: main.querySelectorAll('svg').length,
        saysNoData: /no readings|no data|no series/i.test(text),
        saysNotPermitted: /permission|not allowed|denied|admin only|restricted|cannot/i.test(text),
        hasCurrentValues: /temperature/i.test(text),
      };
    })()`);
    console.log('    history ' + hist.status + ', latest ' + latest.status + ', screen ' + JSON.stringify(screen));

    check(`a ${ROLE} can still see CURRENT values (rbac.go grants that to both roles)`,
      latest.status === 200 && screen.hasCurrentValues,
      'GET /latest -> ' + latest.status + ', ' + JSON.stringify(screen));

    if (hist.status === 401 || hist.status === 403) {
      // A refused history rendered as "no readings in this period" is a screen telling the
      // operator the sensor is silent when the truth is that they are not allowed to look. That
      // is indistinguishable from a dead device — the same class of ambiguity #216 fixed on the
      // truncation axis.
      check(`...and a ${ROLE} refused the HISTORY is told so, not shown an empty chart`,
        screen.saysNotPermitted && !screen.saysNoData,
        'GET /readings -> ' + hist.status + ' but the screen says ' + JSON.stringify(screen));
    } else {
      check(`a ${ROLE} allowed the history actually gets a chart`,
        hist.status === 200 && screen.charts > 0,
        'GET /readings -> ' + hist.status + ', svg elements: ' + screen.charts);
    }
    await shoot('c2-readings');
  }

  // ---- 5. the write paths a non-admin must not have --------------------------------------
  const writes = [
    ['create a device', '/api/devices', 'POST', { name: 'nope', deviceKey: 'nope-01', protocol: 'mqtt' }],
    ['create a rule', '/api/rules', 'POST', { name: 'nope', enabled: true, key: 'temp', condition: 'above', threshold: 1 }],
    ['change telemetry settings', '/api/settings/telemetry', 'PUT', { rawRetentionDays: 1 }],
    ['create a user', '/api/settings/users', 'POST', { username: 'nope', password: 'Nope!12345', roleId: 1 }],
  ];
  const allowed = [];
  for (const [what, path, method, body] of writes) {
    const r = await api(path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (r.status === 200 || r.status === 201) allowed.push(`${what} (${method} ${path}) -> ${r.status}`);
  }
  check(`a ${ROLE} cannot perform admin write actions through the API behind the screen`,
    allowed.length === 0, allowed.length ? allowed.join(' | ') : 'all four refused');
};
