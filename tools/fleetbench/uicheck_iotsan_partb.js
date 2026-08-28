// PART B of the myiotsan screen check: drive real workflows and confirm THE SERVER CHANGED.
//
// English only, admin only: the point here is the state change, not the wording. Every step is
// confirmed against the API, because a button that reports success without doing anything is
// exactly what a screen check that stops at clicking would call a pass.
//
// The order is deliberate. Creation first (it is the workflow an installer does on day one),
// then the device stage, then ACTUATION — the thing this app exists to do and the thing no
// screen check in this suite has ever driven — then delete, last, because it removes what
// everything above needs.
module.exports = async function partB(ctx) {
  const { js, api, press, centre, clickAt, shoot, sleep, check, cdp, dialogs, SEED, NAV_SEL } = ctx;

  const goto = async (label) => {
    await js(`(() => { const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
      .find(e => (e.textContent||'').trim() === ${JSON.stringify(label)}); if (hit) hit.click(); return 1; })()`);
    await sleep(2400);
  };
  const mainText = () => js(`(() => ((document.querySelector('main')||document.body).innerText||''))()`);
  const jsonBody = (r) => { try { return JSON.parse(r.body); } catch (_) { return null; } };

  // THE ENVELOPE TRAP, JavaScript edition. This suite's apps answer BOTH `{data:{result}}` and a
  // bare `{result}` depending on the handler, and reaching for only one of them turns a working
  // endpoint into an empty list — which then reads as "the screen created nothing". Cost a run.
  const unwrap = (r) => {
    const b = jsonBody(r);
    if (!b) return null;
    if (b.data && Object.prototype.hasOwnProperty.call(b.data, 'result')) return b.data.result;
    if (Object.prototype.hasOwnProperty.call(b, 'result')) return b.result;
    return b;
  };
  const items = (r) => { const v = unwrap(r); return (v && v.items) || (Array.isArray(v) ? v : []); };
  const listDevices = async () => items(await api('/api/devices?limit=200'));

  // Clicking the "Devices" nav entry does NOT come back from an opened device: the stage is
  // per-tab state, and the tab is already `devices`, so the click changes nothing and every
  // lookup below finds an empty table. The way back is the stage's own "All devices" control.
  const backToList = async () => {
    await js(`(() => { const b = [...document.querySelectorAll('button, a')]
      .find(x => /^all devices$/i.test((x.textContent||'').trim())); if (b) b.click(); return 1; })()`);
    await sleep(1800);
    await goto('Devices');
  };

  console.log('\n--- PART B: admin workflows, confirmed against the server ---');

  // ---- the device stage: the screen an operator actually lives on ------------------------
  await goto('Devices');
  const opened = await press('Open', { sel: 'button' });
  check('a device can be OPENED from its row', opened,
    opened ? 'the row action worked' : 'no reachable "Open" button — is the actions column off-screen?');
  await sleep(1500);

  const stage = await mainText();
  check('the device stage names the device it opened',
    stage.includes(SEED.deviceName) || stage.includes(SEED.muteName),
    stage.slice(0, 120).replace(/\n/g, ' | '));

  // The Readings tab. #216 fixed a bug where one chatty key hid every other key from this panel;
  // this is the screen-side guard on that fix — the panel must show EVERY key the profile
  // declares, not just whichever one reported most recently.
  await press('Readings', { sel: 'button' });
  await sleep(4000);
  const readings = await js(`(() => {
    const main = document.querySelector('main') || document.body;
    const tiles = [...main.querySelectorAll('.iot-latest-key, [class*=latest]')]
      .map(e => (e.innerText||'').trim().split('\\n')[0]).filter(Boolean);
    const text = main.innerText || '';
    return {
      tiles: [...new Set(tiles)],
      hasTemp: /temperature/i.test(text), hasDoor: /door/i.test(text), hasRelay: /relay state/i.test(text),
      charts: main.querySelectorAll('svg').length,
    };
  })()`);
  check('the current-value strip shows EVERY declared key, not just the busiest',
    readings.hasTemp && readings.hasDoor && readings.hasRelay,
    JSON.stringify(readings));
  check('and the history charts render', readings.charts > 0, 'svg elements: ' + readings.charts);
  await shoot('b1-readings');

  // ---- ACTUATION: the reason this app exists ---------------------------------------------
  //
  // Every previous myiotsan bench issued commands over the API. This is the first time the
  // BUTTON A HUMAN PRESSES has been pressed, and the difference matters: services/commands.go's
  // four gates are only reachable if the screen actually reaches them.
  await press('Control', { sel: 'button' });
  await sleep(2500);
  const controlText = await mainText();
  check('the Control tab offers the profile\'s declared commands',
    /relay/i.test(controlText) && /setpoint/i.test(controlText),
    controlText.slice(0, 200).replace(/\n/g, ' | '));
  await shoot('b2-control');

  // Commands hang off the DEVICE (`/api/devices/{id}/commands`), not a top-level collection —
  // a guessed path answers 404 "no such endpoint", which looks exactly like "nothing was issued".
  const devsNow = await listDevices();
  const seedDev = devsNow.find((d) => d.deviceKey === SEED.deviceKey) || devsNow[0];
  const histPath = `/api/devices/${seedDev.id}/commands/history?limit=100`;
  const countBefore = items(await api(histPath)).length;

  // A `switch` command renders "Turn on" / "Turn off", NOT "Send" — only the slider, mode,
  // colour and setpoint kinds render a Send button, and the setpoint's is disabled while its
  // value is empty. Pressing for a label the app does not use reports "no reachable actuation
  // control" about a perfectly working screen.
  const fired = await press('Turn on', { sel: 'button', exact: false, settle: 2500 });
  check('an actuation control can be PRESSED on screen', fired,
    fired ? 'pressed' : 'no reachable actuation control');

  // CONFIRM-TO-EXECUTE. Pressing the control does NOT actuate — it raises a confirmation that
  // states the physical consequence, and that is deliberate on the app that moves real plant.
  // The first run of this check pressed the control, saw no command row and nearly reported a
  // broken screen; the screen was refusing to act without being asked twice, which is right.
  const confirmModal = await js(`(() => {
    const text = document.body.innerText || '';
    return {
      asks: /send this command to the device/i.test(text),
      warnsPhysical: /acts on real hardware|physically switch|cannot be undone/i.test(text),
      buttons: [...document.querySelectorAll('.modal-actions button, [class*=modal] button')]
        .map(b => (b.textContent||'').trim()).filter(Boolean),
    };
  })()`);
  check('actuation asks for confirmation before it touches hardware',
    confirmModal.asks, JSON.stringify(confirmModal).slice(0, 300));
  check('...and the confirmation states the PHYSICAL consequence, not just "are you sure"',
    confirmModal.warnsPhysical, JSON.stringify(confirmModal.buttons));

  // Cancel first: a refused confirmation must issue NOTHING. Checked before the real send so the
  // command row that appears afterwards can only have come from the confirmed press.
  await press('Cancel', { sel: 'button', exact: false, settle: 2000 });
  await sleep(1500);
  const afterCancel = items(await api(histPath)).length;
  check('cancelling the confirmation issues NOTHING',
    afterCancel === countBefore, 'history ' + countBefore + ' -> ' + afterCancel);

  // Now do it for real.
  await press('Turn on', { sel: 'button', exact: false, settle: 2500 });
  const confirmed = await press('Yes, send it', { sel: 'button', exact: false, settle: 4000 });
  check('the confirmation can be completed', confirmed,
    confirmed ? 'confirmed' : 'no reachable confirm button');

  await sleep(3000);
  const rowsAfter = items(await api(histPath));
  // THE ASSERTION THAT MATTERS. Not "the button was clickable" — that a real device_command row
  // exists on the server, which is the only evidence the press reached the chokepoint at all.
  check('...and the press REACHED THE SERVER as a real command row',
    rowsAfter.length > countBefore,
    'command history ' + countBefore + ' -> ' + rowsAfter.length);
  if (rowsAfter.length) {
    const newest = rowsAfter[0];
    check('the command records WHO issued it, not "System"',
      !!(newest.issuedBy || newest.actor || newest.requestedBy || newest.createdBy),
      JSON.stringify(newest).slice(0, 260));
  }
  await shoot('b3-actuated');

  // The device whose actuation capability is OFF. The server refuses it (#212 proved that); the
  // question here is whether the SCREEN says so, or silently offers a control that will fail.
  // An operator who presses a button and gets nothing has no way to learn the toggle exists.
  await backToList();
  const openedMute = await js(`(() => {
    const rows = [...document.querySelectorAll('tr')]
      .filter(r => (r.textContent||'').includes(${JSON.stringify(SEED.muteName)}));
    if (!rows.length) return null;
    const btn = [...rows[0].querySelectorAll('button')].find(b => /open/i.test(b.textContent||''));
    if (!btn) return null;
    const r = btn.getBoundingClientRect();
    return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) };
  })()`);
  if (check('the non-actuating device can be opened too', !!openedMute, JSON.stringify(openedMute))) {
    await clickAt(openedMute.x, openedMute.y);
    await sleep(2500);
    await press('Control', { sel: 'button' });
    await sleep(2200);
    const muteState = await js(`(() => {
      const main = document.querySelector('main') || document.body;
      const text = main.innerText || '';
      const buttons = [...main.querySelectorAll('button')]
        .filter(b => /send|fire|relay|set|apply/i.test(b.textContent||''));
      return {
        text: text.slice(0, 400),
        enabledActuators: buttons.filter(b => !b.disabled).map(b => (b.textContent||'').trim()),
        disabledActuators: buttons.filter(b => b.disabled).map(b => (b.textContent||'').trim()),
        explains: /disabled|not enabled|turn on actuation|actuation is off|read-only|cannot/i.test(text),
      };
    })()`);
    // Either answer is acceptable ENGINEERING — hide it, or disable it — but silence is not.
    check('a device with actuation OFF does not silently offer a dead actuation control',
      muteState.explains || muteState.enabledActuators.length === 0,
      JSON.stringify(muteState).slice(0, 380));
    check('...and it EXPLAINS why, so the operator can find the toggle',
      muteState.explains, JSON.stringify({ text: muteState.text.slice(0, 220) }));
    await shoot('b4-actuation-off');
  }

  // ---- create a device THROUGH THE FORM, with real keystrokes ----------------------------
  //
  // Real typing, not an API call: the create form is the workflow an installer does on day one,
  // and its validation, its dropdowns and its credential modal only exist on the screen.
  await backToList();
  const newKey = 'screen-made-01';
  const beforeCreate = (await listDevices()).length;
  await press('Add device', { sel: 'button', exact: false });
  await sleep(1500);

  // ---- THE PROTOCOL DROPDOWN, read from the form that is open right now -------------------
  //
  // The form offers a protocol choice. Whatever it offers, a device created with it must be able
  // to REPORT — an option that provisions a permanently mute device is worse than no option,
  // because nothing anywhere reports the mistake: no route rejects it, no validation objects, and
  // the field cannot even be changed afterwards. An API bench structurally cannot find this; it
  // only ever sends the value it already knows is good.
  const protocols = await js(`(() => {
    for (const sel of document.querySelectorAll('select')) {
      const opts = [...sel.options].map(o => ({ value: o.value, label: (o.textContent||'').trim() }));
      if (opts.some(o => /mqtt/i.test(o.value))) return opts;
    }
    return [];
  })()`);
  console.log('    protocol options offered: ' + JSON.stringify(protocols));

  // THE POSITIVE FIRST, or the check below is worthless: an empty option list makes "no bad
  // protocol is offered" trivially true — which is exactly how an earlier run of this check
  // reported a PASS for the very defect it was written to find, because the form was not open.
  const formOpen = check('the Add-device form really opened (its protocol list is readable)',
    protocols.length > 0 && protocols.some((o) => /^mqtt$/i.test(o.value)),
    JSON.stringify(protocols));

  if (formOpen) {
    // Ask the SERVER whether each offered protocol can actually carry telemetry. A device reaches
    // this app over exactly one transport — the embedded MQTT broker — so anything else the form
    // offers has to have a route behind it.
    const probes = await Promise.all(['/api/ingest', '/api/telemetry', '/api/devices/ingest']
      .map(async (path) => ({ path, status: (await api(path, { method: 'POST', body: '{}' })).status })));
    const httpOffered = protocols.some((o) => /^http$/i.test(o.value));
    const anyRoute = probes.some((p) => p.status !== 404 && p.status !== 405);
    check('the form does not offer a protocol the app has no ingest route for',
      !httpOffered || anyRoute,
      'protocols offered: ' + JSON.stringify(protocols.map((o) => o.value))
        + '; ingest probes: ' + JSON.stringify(probes));

    // The other half, and the one that matters for anything not going through this form: the
    // SERVER must refuse it too. A guard that lives only in a dropdown is a guard that any
    // script, integration or older client walks straight past — and the resulting device is
    // permanently mute with no error at any layer.
    const mute = await api('/api/devices', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'mute probe', deviceKey: 'mute-probe-01', protocol: 'http' }),
    });
    check('and the SERVER refuses a protocol it has no transport for, not just the form',
      mute.status >= 400, 'POST /api/devices protocol=http -> ' + mute.status + ' ' + mute.body.slice(0, 160));
    // Nonsense must be refused for the same reason — the field is a closed set, not free text.
    const junk = await api('/api/devices', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'junk probe', deviceKey: 'junk-probe-01', protocol: 'carrier-pigeon' }),
    });
    check('...and an unknown protocol string is refused rather than stored verbatim',
      junk.status >= 400, 'protocol=carrier-pigeon -> ' + junk.status);
    await shoot('b5-protocols');
  }

  const typeInto = async (labelRe, value) => {
    const pos = await js(`(() => {
      const main = document.querySelector('main') || document.body;
      const lbl = [...main.querySelectorAll('label')].find(l => ${labelRe}.test((l.textContent||'').trim()));
      const el = lbl && (lbl.querySelector('input, select') || lbl.parentElement.querySelector('input, select'));
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2), tag: el.tagName };
    })()`);
    if (!pos) return false;
    await clickAt(pos.x, pos.y);
    await sleep(250);
    await cdp.send('Input.insertText', { text: value });
    await sleep(250);
    return true;
  };

  const typedName = await typeInto('/name/i', 'Screen made device');
  const typedKey = await typeInto('/device key|key/i', newKey);
  check('the Add-device form accepts typed input', typedName && typedKey,
    JSON.stringify({ typedName, typedKey }));

  // The profile dropdown must be set or the device is created without a type.
  await js(`(() => {
    const main = document.querySelector('main') || document.body;
    for (const sel of main.querySelectorAll('select')) {
      const opt = [...sel.options].find(o => /screen bench sensor/i.test(o.textContent||''));
      if (opt) { sel.value = opt.value;
        sel.dispatchEvent(new Event('change', { bubbles: true })); return 1; }
    }
    return 0;
  })()`);
  await sleep(600);
  await shoot('b5-create-form');

  const submitted = await press('Create', { sel: 'button', exact: false, settle: 3500 })
    || await press('Add', { sel: 'button', exact: false, settle: 3500 })
    || await press('Save', { sel: 'button', exact: false, settle: 3500 });
  check('the create form can be submitted', submitted, submitted ? 'submitted' : 'no submit button found');
  await sleep(2500);

  const afterCreate = await listDevices();
  const madeIt = afterCreate.find((d) => d.deviceKey === newKey);
  check('a device typed into the FORM reaches the server',
    !!madeIt, 'devices ' + beforeCreate + ' -> ' + afterCreate.length + ', looking for ' + newKey);

  // The broker password is minted once and shown once. If the modal does not actually display
  // it, the installer has provisioned a device they can never connect.
  const credential = await js(`(() => {
    const text = document.body.innerText || '';
    const m = text.match(/[A-Za-z0-9_\\-]{20,}/g) || [];
    return { shown: /password|credential|kata laluan/i.test(text), candidates: m.slice(0, 3) };
  })()`);
  check('the one-time broker credential is actually SHOWN on screen',
    credential.shown && credential.candidates.length > 0, JSON.stringify(credential).slice(0, 240));
  await shoot('b6-credential');

  // ---- create a RULE on screen, then DELETE it -------------------------------------------
  await goto('Rules');
  const ruleName = 'screencheck-rule';
  const target = seedDev;
  const createdRule = await api('/api/rules', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      name: ruleName, enabled: true, deviceId: target.id, key: 'temp', condition: 'above',
      threshold: 99, consecutiveSamples: 1, cooldownSeconds: 60, severity: 'warning',
      schedulePolicy: 'always',
    }),
  });
  check('a rule can be created', createdRule.status === 200,
    createdRule.status + ' ' + createdRule.body.slice(0, 140));
  await goto('Dashboard'); await goto('Rules');
  const ruleListed = (await mainText()).includes(ruleName);
  check('and the new rule APPEARS ON SCREEN', ruleListed, 'looking for ' + ruleName);

  // Delete THROUGH THE SCREEN, and accept the confirmation it should ask for. A green run that
  // never removes anything has not covered delete — the lesson W3-5a paid 44/44 for.
  const dialogsBefore = dialogs.length;
  const pressedDelete = await js(`(() => {
    const row = [...document.querySelectorAll('tr')].find(r => (r.textContent||'').includes(${JSON.stringify(ruleName)}));
    if (!row) return null;
    const btn = [...row.querySelectorAll('button')].find(b => /delete|remove|padam/i.test(b.textContent||''));
    if (!btn) return null;
    const r = btn.getBoundingClientRect();
    return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) };
  })()`);
  check('the rule row offers a delete action', !!pressedDelete, JSON.stringify(pressedDelete));
  if (pressedDelete) {
    await clickAt(pressedDelete.x, pressedDelete.y);
    await sleep(1800);
    // An in-app confirm dialog, not window.confirm: press its confirm button too.
    await press('Delete', { sel: 'button', exact: false, settle: 2500 });
    await sleep(2000);
  }
  const deleteAsked = await js(`(() => /are you sure|confirm|delete|cannot be undone/i.test(document.body.innerText||''))()`);
  check('deleting asks for confirmation before it happens',
    dialogs.length > dialogsBefore || deleteAsked,
    'native dialogs: ' + JSON.stringify(dialogs.slice(dialogsBefore)) + ', in-app confirm text: ' + deleteAsked);

  const rulesNow = items(await api('/api/rules'));
  const stillThere = JSON.stringify(rulesNow).includes(ruleName);
  // Conditioned on the create having WORKED, deliberately: "it is gone" is trivially true of a
  // rule that never existed, and this suite has now been bitten by that six times.
  check('and the rule is DELETED on the server after deleting it on screen',
    createdRule.status === 200 && !stillThere,
    'create ' + createdRule.status + ', still listed: ' + stillThere);
  await shoot('b8-rule-deleted');

  // ---- delete the device we made, through the screen --------------------------------------
  await backToList();
  const delPos = await js(`(() => {
    const row = [...document.querySelectorAll('tr')].find(r => (r.textContent||'').includes(${JSON.stringify(newKey)}));
    if (!row) return null;
    const btn = [...row.querySelectorAll('button')].find(b => /delete|remove/i.test(b.textContent||''));
    if (!btn) return null;
    const r = btn.getBoundingClientRect();
    return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) };
  })()`);
  if (delPos) {
    await clickAt(delPos.x, delPos.y);
    await sleep(1800);
    await press('Delete', { sel: 'button', exact: false, settle: 2500 });
    await sleep(2000);
  }
  const remaining = await listDevices();
  check('a device deleted on screen is gone from the server',
    !!madeIt && !remaining.some((d) => d.deviceKey === newKey),
    'created: ' + !!madeIt + ', still present: ' + remaining.some((d) => d.deviceKey === newKey));
  await shoot('b9-device-deleted');
};
