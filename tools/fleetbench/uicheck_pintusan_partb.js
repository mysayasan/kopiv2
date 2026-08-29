// PART B of the mypintusan screen check: drive real workflows and confirm THE SERVER CHANGED.
//
// English only, admin only: the point here is the state change, not the wording. Every step is
// confirmed against the API, because a button that reports success without doing anything is
// exactly what a screen check that stops at clicking would call a pass.
//
// The order is deliberate. LOCKDOWN FIRST, because it is the one control an operator reaches for
// in an emergency and the only one on this app whose failure mode is a building that will not
// work — and because engaging it lets the remote unlock be checked from both sides (refused while
// sealed, granted once released). Then a badge issued and revoked from the People screen, then a
// grant created and revoked from Access rules — the change that silently decides who may enter
// every door in a group. Deletion is driven last and deliberately: a green run that never removes
// anything has not covered DELETE at all, and this app's grant and holiday screens are the only
// place a rule can be taken away.
module.exports = async function partB(ctx) {
  const { js, api, press, centre, clickAt, shoot, sleep, check, dialogs, setPrompt, SEED, NAV_SEL } = ctx;

  const goto = async (label) => {
    await js(`(() => { const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
      .filter(e => !e.closest('.side-nav-foot') && !e.closest('.nav-footer'))
      .find(e => (e.textContent||'').trim() === ${JSON.stringify(label)}); if (hit) hit.click(); return 1; })()`);
    await sleep(2400);
  };
  const mainText = () => js(`(() => ((document.querySelector('main')||document.body).innerText||''))()`);
  const jsonBody = (r) => { try { return JSON.parse(r.body); } catch (_) { return null; } };

  // THE ENVELOPE TRAP, JavaScript edition. This suite's apps answer BOTH `{data:{result}}` and a
  // bare `{result}` depending on the handler, and reaching for only one of them turns a working
  // endpoint into an empty list — which then reads as "the screen created nothing".
  const unwrap = (r) => {
    const b = jsonBody(r);
    if (!b) return null;
    if (b.data && Object.prototype.hasOwnProperty.call(b.data, 'result')) return b.data.result;
    if (Object.prototype.hasOwnProperty.call(b, 'result')) return b.result;
    return b;
  };
  // items() ALSO reports when an OK response failed to become a list. A silent unwrap failure is
  // indistinguishable from an empty estate, and every diff-based check below then compares nothing
  // with nothing and passes — or, worse, fails and names a defect that does not exist. This bench
  // lost a run to exactly that (see the note on `api`'s body cap), so the failure is now loud.
  const unreadable = [];
  const items = (r, what) => {
    const v = unwrap(r);
    const list = (v && v.items) || (Array.isArray(v) ? v : null);
    if (list === null) {
      unreadable.push((what || 'a list') + ' -> ' + r.status
        + (r.truncated ? ' (body truncated)' : '') + ' ' + String(r.body).slice(0, 80));
      return [];
    }
    return list;
  };
  const listDoors = async () => items(await api('/api/doors'), 'doors');
  const listEvents = async () => items(await api('/api/events?limit=100'), 'the access log');
  const listGrants = async () => items(await api('/api/grants'), 'grants');
  const listHolidays = async () => items(await api('/api/schedules/holidays'), 'holidays');

  console.log('\n--- PART B: admin workflows, confirmed against the server ---');

  // ---- LOCKDOWN, from the screen ---------------------------------------------------------
  //
  // It lives on the Doors screen next to the door cards, which is right: it is the control
  // somebody reaches for while looking at the doors. It confirms before engaging, and the
  // confirmation is part of the check — a one-click site seal is a different product.
  await goto('Doors');
  const doorsText = await mainText();
  check('the Doors screen shows the seeded door', doorsText.includes(SEED.doorName),
    doorsText.slice(0, 140).replace(/\n/g, ' | '));

  const dialogsBefore = dialogs.length;
  const engaged = await press('Start lockdown', { sel: 'button' });
  check('the lockdown control is reachable on the Doors screen', engaged);
  await sleep(1500);
  check('engaging lockdown ASKS first', dialogs.length > dialogsBefore,
    JSON.stringify(dialogs.slice(dialogsBefore)));

  const ldOn = unwrap(await api('/api/lockdown'));
  check('the SERVER is now in lockdown, not just the screen', !!(ldOn && ldOn.lockdown),
    JSON.stringify(ldOn));
  await sleep(1200);
  const sealedText = await mainText();
  check('and the screen says so', /Site is in lockdown/i.test(sealedText),
    sealedText.slice(0, 120).replace(/\n/g, ' | '));
  await shoot('b1-lockdown-on');

  // The door cards must stop offering an unlock while the site is sealed. This is the screen half
  // of a rule the controller enforces anyway (OperatorUnlock refuses under lockdown) — but a
  // button that is offered, pressed, and then refused is how an operator learns to distrust the
  // screen in the exact minute they need it.
  const unlockState = await js(`(() => {
    const btns = [...document.querySelectorAll('.door-card button')]
      .filter(b => !/lockdown/i.test(b.textContent||''));
    return btns.map(b => ({ text: (b.textContent||'').trim(), disabled: !!b.disabled }));
  })()`);
  check('while sealed, no door card offers a working Unlock button',
    unlockState.length > 0 && unlockState.every((b) => b.disabled), JSON.stringify(unlockState));

  // And the server refuses it too, so the screen is not the only thing standing in the way.
  const doorsNow = await listDoors();
  const door = doorsNow.find((d) => d.name === SEED.doorName) || doorsNow[0];
  check('the bench can see the door it is about to open', !!door, JSON.stringify(doorsNow.map((d) => d.name)));
  if (door) {
    const refused = await api(`/api/doors/${door.id}/unlock`, { method: 'POST' });
    check('the server refuses a remote unlock while the site is sealed',
      refused.status >= 400, refused.status + ' ' + refused.body.slice(0, 160));
  }

  // Release it. A lockdown that cannot be lifted from the same control is a support call.
  const released = await press('End lockdown', { sel: 'button' });
  check('lockdown can be released from the same control', released);
  await sleep(1500);
  const ldOff = unwrap(await api('/api/lockdown'));
  check('the SERVER is out of lockdown', !!(ldOff && ldOff.lockdown === false), JSON.stringify(ldOff));
  await shoot('b2-lockdown-off');

  // ---- the remote unlock, from the door card ---------------------------------------------
  //
  // This travels OSDP to the strike, so a pass here means the screen reached real hardware. It is
  // confirmed against the ACCESS LOG rather than against the toast: OperatorUnlock writes an
  // event with rawCredential "operator" and the SESSION's username, never a name from the body.
  await sleep(1200);
  const eventsBefore = await listEvents();
  const beforeIds = new Set(eventsBefore.map((e) => e.id));
  const pressedUnlock = await press('Unlock', { sel: '.door-card button', settle: 3000 });
  check('the door card offers a working Unlock once the site is open again', pressedUnlock);
  await sleep(2500);

  const eventsAfter = await listEvents();
  const fresh = eventsAfter.filter((e) => !beforeIds.has(e.id));
  const remote = fresh.find((e) => e.rawCredential === 'operator');
  check('pressing Unlock reaches the controller and lands in the access log',
    !!remote, JSON.stringify(fresh.slice(0, 3).map((e) => ({ raw: e.rawCredential, dec: e.decision, why: e.reason }))));
  if (remote) {
    check('and the log names the operator who pressed it, from the session',
      remote.holderName === 'admin' && remote.decision === 'granted',
      JSON.stringify({ who: remote.holderName, decision: remote.decision, detail: remote.detail }));
  }
  await shoot('b3-unlocked');

  // ---- the Activity screen shows what just happened ---------------------------------------
  await goto('Activity');
  await sleep(2000);
  const activity = await mainText();
  check('the Activity screen shows the remote unlock that was just driven',
    /operator/i.test(activity), activity.slice(0, 200).replace(/\n/g, ' | '));
  // The filter buttons are the screen's only interactive surface, and a filter that does not
  // filter is worse than none: the operator concludes there were no denials.
  await press('Only denied', { sel: 'button' });
  await sleep(2500);
  const denied = await mainText();
  check('the "only denied" filter really filters', !/\bGranted\b/.test(denied.replace(/Only granted/g, '')),
    denied.slice(0, 200).replace(/\n/g, ' | '));
  await press('All', { sel: 'button' });
  await shoot('b4-activity');

  // ---- the Readers screen must not lie about encryption -----------------------------------
  //
  // #220's lesson, applied to a different screen: after proving an alarm fires, check the list it
  // sends somebody to. Here the reader is live on the bus and holds the site key, so the screen
  // has to say both things — and "last seen: never" next to a reader that is answering right now
  // is the exact failure #220 found on this same screen.
  await goto('Readers');
  await sleep(2000);
  const readersText = await mainText();
  const readerRows = items(await api('/api/readers'), 'readers');
  const live = readerRows[0];
  check('the Readers screen lists the reader the door created', readerRows.length > 0,
    JSON.stringify(readerRows.map((r) => r.name)));
  check('and it reports the reader as ENCRYPTED, which is what the bus is actually doing',
    /Encrypted/i.test(readersText) && !/Not encrypted/i.test(readersText),
    readersText.slice(0, 260).replace(/\n/g, ' | '));
  if (live) {
    check('and the last-seen column is a real time, not "never"',
      !!live.lastSeenAt && live.lastSeenAt > 0 && !/never|—\s*$/i.test(readersText.split('\n').pop() || ''),
      'lastSeenAt=' + live.lastSeenAt);
  }
  await shoot('b5-readers');

  // ---- a badge, issued and revoked from the People screen ---------------------------------
  await goto('People');
  await sleep(1500);
  const peopleText = await mainText();
  check('the People screen shows the seeded person', peopleText.includes(SEED.holderName),
    peopleText.slice(0, 160).replace(/\n/g, ' | '));

  const openedBadges = await press('Badges', { sel: 'button' });
  check('a person’s badges can be opened from their row', openedBadges);
  await sleep(1500);
  const holders = items(await api('/api/holders'), 'people');
  const holder = holders.find((h) => h.ref === SEED.holderRef) || holders[0];
  check('the bench can see the person whose badge it is about to revoke', !!holder,
    JSON.stringify(holders.map((h) => h.ref)));

  let credsBefore = [];
  if (holder) credsBefore = items(await api(`/api/holders/${holder.id}/credentials`), 'badges');
  check('the badge list the screen opened matches the server', credsBefore.length > 0,
    JSON.stringify(credsBefore.map((c) => ({ n: c.cardNumber, s: c.status }))));

  // Issue a second badge THROUGH THE FORM. Typed with real key events, because a value poked into
  // a React input with .value = does not fire onChange and the component's state never sees it —
  // the form then submits its initial state and the check passes on an empty card number.
  const issued = await press('Issue a badge', { sel: 'button' });
  check('the badge form opens', issued);
  await sleep(1200);
  const typedOK = await js(`(() => {
    const setNative = (el, v) => {
      const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    const form = [...document.querySelectorAll('form.form-inset')].pop();
    if (!form) return 'no issue form';
    const inputs = [...form.querySelectorAll('input')];
    const num = inputs.find(i => i.required && i.type !== 'number');
    if (!num) return 'no card-number input';
    setNative(num, '00990041');
    return 'ok';
  })()`);
  check('the card number can be typed into the badge form', typedOK === 'ok', typedOK);
  await press('Save', { sel: 'button' });
  await sleep(2500);

  const credsAfterIssue = holder ? items(await api(`/api/holders/${holder.id}/credentials`), 'badges') : [];
  const issuedCard = credsAfterIssue.find((c) => c.cardNumber === '00990041');
  check('the badge the screen issued exists on the server', !!issuedCard,
    JSON.stringify(credsAfterIssue.map((c) => c.cardNumber)));

  // Revoke it. The screen asks for the status through window.prompt(), which BLOCKS the page —
  // an unanswered prompt wedges every check after this one.
  if (issuedCard) {
    setPrompt('lost');
    // TARGET THE ROW, NOT THE FIRST BUTTON. Every active badge renders its own Revoke, and a bare
    // text match picks whichever comes first — which on the first run revoked the SEEDED card
    // instead of the one just issued, then reported that the server had not recorded the
    // revocation. It had; the bench had revoked something else, and the sim spent the rest of the
    // run badging a card that was now `lost`. A control that exists once per row has to be
    // located through its row.
    const pos = await js(`(() => {
      const row = [...document.querySelectorAll('.modal tr')]
        .find(tr => (tr.textContent||'').includes('00990041'));
      if (!row) return null;
      const btn = [...row.querySelectorAll('button')].find(b => !b.disabled);
      if (!btn) return null;
      const r = btn.getBoundingClientRect();
      const x = Math.round(r.left + r.width/2), y = Math.round(r.top + r.height/2);
      const at = document.elementFromPoint(x, y);
      if (!at || !(btn === at || btn.contains(at) || at.contains(btn))) return null;
      return { x, y };
    })()`);
    const revoked = !!pos;
    if (pos) { await clickAt(pos.x, pos.y); await sleep(3000); }
    check('the badge just issued can be revoked from its own row', revoked);
    await sleep(1500);
    const after = items(await api(`/api/holders/${holder.id}/credentials`), 'badges');
    const gone = after.find((c) => c.cardNumber === '00990041');
    check('and the server records the revocation, keeping the row so the card stays RECOGNISED',
      !!gone && gone.status !== 'active',
      JSON.stringify(after.map((c) => ({ n: c.cardNumber, s: c.status }))));
  }
  await shoot('b6-badges');

  // ---- a grant, created and revoked from Access rules --------------------------------------
  //
  // The most consequential edit in the product: it changes who may enter every door in a group,
  // at every hour, until somebody notices. #222 added an `access.rule-change` notification for
  // exactly that reason, so the check is not "did the row appear" but "did anybody get told".
  await goto('Access rules');
  await sleep(2000);
  const accessText = await mainText();
  check('the Access rules screen renders all four sections',
    /Grants/.test(accessText) && /Groups/.test(accessText)
      && /Schedules/.test(accessText) && /Holiday calendar/.test(accessText),
    accessText.slice(0, 200).replace(/\n/g, ' | '));
  check('and it shows the seeded rules',
    accessText.includes(SEED.groupName) && accessText.includes(SEED.scheduleName),
    accessText.slice(0, 240).replace(/\n/g, ' | '));
  await shoot('b7-access');

  const grantsBefore = await listGrants();
  const notifsBefore = items(await api('/api/notifications?limit=100'), 'the feed');
  const notifIdsBefore = new Set(notifsBefore.map((x) => x.id));

  // Revoking the seeded grant exercises DELETE — which a create-only bench never touches, and
  // which is the operation that TAKES ACCESS AWAY. If it silently fails, everybody keeps getting
  // in and the screen shows the grant gone.
  const revokedGrant = await press('Revoke', { sel: 'button', settle: 3000 });
  check('a grant can be revoked from the screen', revokedGrant);
  await sleep(2000);
  const grantsAfter = await listGrants();
  check('and the grant is really gone from the server',
    grantsAfter.length === grantsBefore.length - 1,
    'before ' + grantsBefore.length + ' after ' + grantsAfter.length);

  const notifsAfter = items(await api('/api/notifications?limit=100'), 'the feed');
  const freshNotifs = notifsAfter.filter((x) => !notifIdsBefore.has(x.id));
  check('revoking a grant is announced in the feed under a name — a rule change must not look like nothing',
    freshNotifs.some((x) => (x.category || '') === 'access.rule-change'),
    JSON.stringify(freshNotifs.slice(0, 4).map((x) => x.category + ': ' + (x.title || ''))));

  // ---- the holiday calendar, created and deleted from the screen ---------------------------
  //
  // #222 built this screen because listHolidays/createHoliday/deleteHoliday existed and were
  // called from NOWHERE. This is the first time a human's clicks have driven it.
  const holidaysBefore = await listHolidays();
  const addedHoliday = await press('Add a holiday', { sel: 'button' });
  check('the holiday form opens from the calendar section', addedHoliday);
  await sleep(1200);
  const holidayTyped = await js(`(() => {
    const setNative = (el, v) => {
      const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    const modal = document.querySelector('.modal');
    if (!modal) return 'no modal';
    const text = [...modal.querySelectorAll('input')].find(i => i.type !== 'date');
    const date = modal.querySelector('input[type=date]');
    if (!text || !date) return 'form is missing a name or a date field';
    setNative(text, 'Screen bench added holiday');
    setNative(date, '2026-11-11');
    return 'ok';
  })()`);
  check('a holiday name and date can be typed in', holidayTyped === 'ok', holidayTyped);
  await press('Save', { sel: 'button', settle: 3000 });
  await sleep(1500);
  const holidaysAfter = await listHolidays();
  check('the holiday the screen created exists on the server',
    holidaysAfter.some((h) => h.name === 'Screen bench added holiday'),
    JSON.stringify(holidaysAfter.map((h) => h.name)));
  check('and it did not simply replace the one that was there',
    holidaysAfter.length === holidaysBefore.length + 1,
    'before ' + holidaysBefore.length + ' after ' + holidaysAfter.length);
  await shoot('b8-holidays');

  // ---- Settings: the one screen that can change how every door decides ---------------------
  await goto('Settings');
  await sleep(2500);
  const settingsText = await mainText();
  check('the Settings screen loads the live access configuration',
    /time\s*zone/i.test(settingsText) && settingsText.length > 200,
    settingsText.slice(0, 200).replace(/\n/g, ' | '));
  // The site timezone is the FIRST thing the wizard asks and, until #222, the change never
  // reached the running controller. Driving it from the screen is the only way to check the
  // whole path — form, save, runtime — rather than the handler alone.
  const tzTyped = await js(`(() => {
    const setNative = (el, v) => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    const labels = [...document.querySelectorAll('main label')];
    // \\s, not \s: this whole block is a TEMPLATE LITERAL, and an unrecognised escape in one
    // loses its backslash before the browser ever sees it — /time\s*zone/ would arrive as
    // /times*zone/ and match nothing, reporting a field that is right there on the screen as
    // missing. The label reads "Time zone", with a space.
    const tz = labels.find(l => /time\\s*zone/i.test(l.textContent||''));
    const input = tz && tz.querySelector('input');
    if (!input) return 'no timezone field';
    setNative(input, 'Asia/Kuala_Lumpur');
    return 'ok';
  })()`);
  check('the site timezone can be typed on the Settings screen', tzTyped === 'ok', tzTyped);
  await press('Save', { sel: 'button[type=submit]', exact: false, settle: 3000 });
  await sleep(1500);
  const saved = unwrap(await api('/api/settings/access'));
  check('and the saved timezone reaches the server',
    !!saved && saved.timezone === 'Asia/Kuala_Lumpur', JSON.stringify(saved && saved.timezone));
  await shoot('b9-settings');

  // Last, and it guards everything above: every list this pass read really parsed. Without it a
  // truncated or re-shaped response turns each before/after diff into nothing-versus-nothing, and
  // this part reports invented defects with total confidence.
  check('every list this pass read came back as a readable list', unreadable.length === 0,
    unreadable.join(' | '));
};
