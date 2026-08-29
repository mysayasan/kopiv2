// PART C of the mypintusan screen check: does what the SCREEN OFFERS match what the SERVER ALLOWS?
//
// THE REASON THIS EXISTS. This app decides authorization twice. `views/App.js` filters the
// navigation rail on a client-side `user.isAdmin`; the server decides independently with the
// deny-by-default matrix in `services/rbac.go`. Two mechanisms, one intent — the exact root cause
// this suite already recorded once as "nav uses isAdmin, server uses the matrix — two sources of
// truth". Nothing had ever checked they agree, and rbac.go's header spends thirty lines reasoning
// about a viewer and an operator whose powers no test had ever exercised.
//
// Both directions are faults, and they fail differently:
//
//   THE SCREEN OFFERS WHAT THE SERVER REFUSES -> a dead control. The operator presses it, gets a
//      bare error, and cannot tell "I am not allowed" from "this is broken". On a door screen
//      that ambiguity is expensive: the person is standing at the door.
//   THE SCREEN HIDES WHAT THE SERVER ALLOWS -> capability somebody was deliberately granted and
//      cannot reach, invisible to everyone including whoever granted the role.
//
// And this app draws a line no other app in the suite draws. rbac.go: handing someone a badge is
// "a daily, reversible, fully logged act, and a receptionist needs it", while editing a GRANT
// "silently changes who may enter every door in that group, at every hour, until somebody
// notices". Credentials are operator-level; grants are admin-only. LOCKDOWN is admin-only because
// it is the one control that stops a building working. Those three claims are what PART C tests.
module.exports = async function partC(ctx) {
  const { js, api, press, clickAt, shoot, sleep, check, ROLE, SEED, NAV_SEL, navs } = ctx;

  const goto = async (label) => {
    await js(`(() => { const hit = [...document.querySelectorAll(${JSON.stringify(NAV_SEL)})]
      .filter(e => !e.closest('.side-nav-foot') && !e.closest('.nav-footer'))
      .find(e => (e.textContent||'').trim() === ${JSON.stringify(label)}); if (hit) hit.click(); return 1; })()`);
    await sleep(2400);
  };
  const mainText = () => js(`(() => ((document.querySelector('main')||document.body).innerText||''))()`);
  const jsonBody = (r) => { try { return JSON.parse(r.body); } catch (_) { return null; } };
  // Both envelope shapes — a one-shape unwrap turns a working list into an empty one, and an
  // empty list makes every permission check below pass for no reason at all.
  const unwrap = (r) => {
    const b = jsonBody(r);
    if (!b) return null;
    if (b.data && Object.prototype.hasOwnProperty.call(b.data, 'result')) return b.data.result;
    if (Object.prototype.hasOwnProperty.call(b, 'result')) return b.result;
    return b;
  };
  const items = (r) => { const v = unwrap(r); return (v && v.items) || (Array.isArray(v) ? v : []); };
  const refused = (r) => r.status === 401 || r.status === 403;

  console.log(`\n--- PART C: what the ${ROLE}'s screen offers vs what the server allows ---`);

  // Each nav entry and the endpoint its screen cannot function without. If the rail offers the
  // entry, the server has to answer that call for this role — otherwise the entry opens an error.
  const SECTION_API = {
    Doors: '/api/doors',
    People: '/api/holders',
    Activity: '/api/events?limit=5',
    Readers: '/api/readers',
    'Access rules': '/api/grants',
    // The administrative trail is admin-only in services/rbac.go, so for these two roles this
    // entry belongs in the HIDDEN half below — and check 2 proves the hiding is real by asking
    // the server, not by trusting the rail.
    'Admin trail': '/api/audit',
    Settings: '/api/settings/access',
  };

  // ---- 1. every entry the rail OFFERS must work -------------------------------------------
  const offered = navs.filter((label) => SECTION_API[label]);
  const dead = [];
  for (const label of offered) {
    const r = await api(SECTION_API[label]);
    if (refused(r)) dead.push(`${label} -> ${SECTION_API[label]} ${r.status}`);
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

  // The hiding has to actually be happening, or both checks above are vacuous. App.js hides
  // Access rules and Settings from a non-admin.
  check('the nav really is reduced for a non-admin (the checks above are not vacuous)',
    hidden.length > 0, 'hidden from ' + ROLE + ': ' + JSON.stringify(hidden));
  await shoot('c0-nav');

  // ---- 3. THE DOOR. The one power the operator role exists for -----------------------------
  //
  // rbac.go: "Opening a door remotely is an operator power: it is what a receptionist does all
  // day, it is instantaneous, and every use of it lands in the same log as a badge." A viewer
  // must not have it. Both halves are asked of the SCREEN and of the SERVER, and disagreement in
  // either direction is a defect.
  await goto('Doors');
  const doorsText = await mainText();
  check(`a ${ROLE} can see the doors`, doorsText.includes(SEED.doorName),
    doorsText.slice(0, 160).replace(/\n/g, ' | '));

  const unlockOffer = await js(`(() => {
    const btns = [...document.querySelectorAll('.door-card button')];
    return btns.map(b => ({ text: (b.textContent||'').trim(), disabled: !!b.disabled,
      hittable: (() => { const r = b.getBoundingClientRect();
        const x = Math.round(r.left+r.width/2), y = Math.round(r.top+r.height/2);
        if (x<0||y<0||x>innerWidth||y>innerHeight) return false;
        const at = document.elementFromPoint(x,y); return !!at && (at===b||b.contains(at)||at.contains(b)); })() }));
  })()`);
  const screenOffersUnlock = unlockOffer.some((b) => /unlock/i.test(b.text) && !b.disabled && b.hittable);

  const doors = items(await api('/api/doors'));
  const door = doors.find((d) => d.name === SEED.doorName) || doors[0];
  check(`the bench can see a door to try the ${ROLE}'s unlock against`, !!door,
    JSON.stringify(doors.map((d) => d.name)));

  let serverAllowsUnlock = false;
  if (door) {
    const r = await api(`/api/doors/${door.id}/unlock`, { method: 'POST' });
    // PERMISSION, NOT HARDWARE. This probe really opens a door, so it can also come back 400 with
    // "osdp: PD did not reply" when the simulator has gone away — which says nothing at all about
    // whether the operator is allowed. Anchoring on "not refused" keeps this check about the thing
    // PART C exists to compare; the admin pass already proved an unlock reaches the strike.
    serverAllowsUnlock = !refused(r);
    if (ROLE === 'operator') {
      check('the server lets an OPERATOR open a door remotely — the whole reason the role exists',
        serverAllowsUnlock, `POST /api/doors/${door.id}/unlock -> ` + r.status + ' ' + r.body.slice(0, 200));
    } else {
      check('the server refuses a VIEWER a remote unlock',
        refused(r), `POST /api/doors/${door.id}/unlock -> ` + r.status + ' ' + r.body.slice(0, 200));
    }
  }

  check(`the Unlock button the ${ROLE}'s screen offers matches what the server will do`,
    screenOffersUnlock === serverAllowsUnlock,
    'screen offers a working Unlock: ' + screenOffersUnlock
      + '; server allows it: ' + serverAllowsUnlock
      + '; buttons: ' + JSON.stringify(unlockOffer));
  await shoot('c1-doors');

  // ---- 4. LOCKDOWN is admin-only, and the screen must not pretend otherwise ----------------
  const lockdownOffered = await js(`(() => [...document.querySelectorAll('main button')]
    .some(b => /lockdown/i.test(b.textContent||'')))()`);
  const ldWrite = await api('/api/lockdown', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ lockdown: true }),
  });
  check(`the server refuses a ${ROLE} the power to seal the site`, refused(ldWrite),
    'POST /api/lockdown -> ' + ldWrite.status + ' ' + ldWrite.body.slice(0, 160));
  check(`and the ${ROLE}'s screen does not offer the lockdown control`, !lockdownOffered,
    'a lockdown button is on screen: ' + lockdownOffered);
  // A 200 here would mean the bench just sealed the building. Undo it rather than leaving the
  // rest of the pass running against a site in lockdown, where every unlock is refused for a
  // reason that has nothing to do with permissions.
  if (ldWrite.status === 200) {
    await api('/api/lockdown', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lockdown: false }),
    });
  }

  // ---- 5. PEOPLE AND BADGES: operator-level, viewer read-only ------------------------------
  //
  // The sharper of the two lines this app draws. An operator issues and revokes badges all day;
  // a viewer sees who holds what and changes nothing.
  await goto('People');
  await sleep(1500);
  const peopleText = await mainText();
  check(`a ${ROLE} can see the people list`, peopleText.includes(SEED.holderName),
    peopleText.slice(0, 200).replace(/\n/g, ' | '));

  const holdersRead = await api('/api/holders');
  check(`the server lets a ${ROLE} read the people list the screen just rendered`,
    holdersRead.status === 200,
    'GET /api/holders -> ' + holdersRead.status + ' ' + holdersRead.body.slice(0, 160));

  const addOffered = await js(`(() => [...document.querySelectorAll('main button')]
    .some(b => /add person/i.test(b.textContent||'') && !b.disabled))()`);
  const holderWrite = await api('/api/holders', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: 'Part C probe ' + ROLE, ref: 'PC-' + ROLE, kind: 'visitor' }),
  });
  const serverAllowsHolderWrite = holderWrite.status === 200;
  if (ROLE === 'operator') {
    check('the server lets an OPERATOR enrol a person — the routine, reversible half of access control',
      serverAllowsHolderWrite, 'POST /api/holders -> ' + holderWrite.status + ' ' + holderWrite.body.slice(0, 160));
  } else {
    check('the server refuses a VIEWER the power to enrol a person', refused(holderWrite),
      'POST /api/holders -> ' + holderWrite.status + ' ' + holderWrite.body.slice(0, 160));
  }
  check(`the "Add person" button the ${ROLE}'s screen offers matches what the server will do`,
    addOffered === serverAllowsHolderWrite,
    'screen offers it: ' + addOffered + '; server allows it: ' + serverAllowsHolderWrite);

  // The badge drawer, and the revoke inside it — the operator's other daily act.
  const openedBadges = await press('Badges', { sel: 'button' });
  await sleep(1500);
  const issueOffered = await js(`(() => [...document.querySelectorAll('.modal button')]
    .some(b => /issue a badge/i.test(b.textContent||'') && !b.disabled))()`);
  const holders = items(await api('/api/holders'));
  const holder = holders.find((h) => h.ref === SEED.holderRef) || holders[0];
  let credWrite = { status: 0, body: 'no holder visible to this role' };
  if (holder) {
    credWrite = await api(`/api/holders/${holder.id}/credentials`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'card', format: 'wiegand26', facilityCode: 2, cardNumber: '00770077' }),
    });
  }
  const serverAllowsIssue = credWrite.status === 200;
  if (ROLE === 'operator') {
    check('the server lets an OPERATOR issue a badge', serverAllowsIssue,
      'POST /api/holders/*/credentials -> ' + credWrite.status + ' ' + String(credWrite.body).slice(0, 160));
  } else {
    check('the server refuses a VIEWER the power to issue a badge', refused(credWrite),
      'POST /api/holders/*/credentials -> ' + credWrite.status + ' ' + String(credWrite.body).slice(0, 160));
  }
  check(`the badge drawer the ${ROLE} sees offers issuing only if the server would allow it`,
    !openedBadges || issueOffered === serverAllowsIssue,
    'drawer opened: ' + openedBadges + '; offers issue: ' + issueOffered
      + '; server allows: ' + serverAllowsIssue);
  await shoot('c2-people');

  // ---- 6. THE RULES: admin-only in both directions -----------------------------------------
  //
  // An operator may READ the rules they have to work within — rbac.go grants them GET on groups,
  // grants and schedules — and may not change one. A screen that hides the reading is hiding
  // something the server deliberately allowed.
  for (const [what, path] of [['a grant', '/api/grants'], ['a schedule', '/api/schedules'],
    ['a holiday', '/api/schedules/holidays'], ['a group', '/api/groups']]) {
    const r = await api(path, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Part C probe', groupId: 1, doorId: 1, scheduleId: 1, date: '2026-01-01' }),
    });
    check(`the server refuses a ${ROLE} the power to create ${what}`, refused(r),
      'POST ' + path + ' -> ' + r.status + ' ' + r.body.slice(0, 140));
  }
  if (ROLE === 'operator') {
    const readable = [];
    for (const path of ['/api/groups', '/api/grants', '/api/schedules']) {
      const r = await api(path);
      if (r.status !== 200) readable.push(path + ' -> ' + r.status);
    }
    check('an OPERATOR can READ the rules they have to work within, as the catalog says',
      readable.length === 0, readable.join(' | ') || 'groups, grants and schedules all readable');

    // ...and the screen has to show them WITHOUT offering an edit. This is the harder half: it is
    // easy to hide a whole section and easy to show it entire, and both are wrong here. The
    // catalog grants read and refuses write on the same paths, so the screen must do both at once.
    await goto('Access rules');
    await sleep(2400);
    const rulesText = await mainText();
    check('the operator can see the access rules on screen, not just over the API',
      rulesText.includes(SEED.groupName) && rulesText.includes(SEED.scheduleName),
      rulesText.slice(0, 220).replace(/\n/g, ' | '));
    const editOffers = await js(`(() => [...(document.querySelector('main')||document.body).querySelectorAll('button')]
      .map(b => (b.textContent||'').trim())
      .filter(x => /^(grant access|new group|new schedule|add a holiday|revoke|delete|remove)$/i.test(x)))()`);
    check('and the screen offers the operator no way to CHANGE one',
      editOffers.length === 0, 'edit controls on screen: ' + JSON.stringify(editOffers));
    check('and it says why, rather than looking like an empty or broken screen',
      /cannot change|not change them/i.test(rulesText),
      rulesText.slice(0, 200).replace(/\n/g, ' | '));
    await shoot('c5-access');
  }

  // ---- 7. THE READERS SCREEN MUST NOT LIE --------------------------------------------------
  //
  // #220's lesson, on the screen it was found on: an alarm woke somebody and the list they opened
  // said "ok, last seen never". The security column here is computed from /api/settings/access,
  // which is ADMIN-ONLY — and Readers.js swallows the refusal with `.catch(() => null)`, after
  // which `securityFor` returns null and every reader renders "Not encrypted".
  //
  // The reader under test holds the site key and is running an encrypted session right now. So a
  // non-admin being shown "Not encrypted" is not a permission boundary, it is the screen stating
  // the opposite of the truth about the security of a door — the one thing this screen exists to
  // answer. Saying nothing would be fine. Saying the wrong thing is not.
  await goto('Readers');
  await sleep(2000);
  const readersText = await mainText();
  const readerRows = items(await api('/api/readers'));
  check(`a ${ROLE} can see the readers`, readerRows.length > 0,
    JSON.stringify(readerRows.map((r) => r.name)));
  const settingsProbe = await api('/api/settings/access');
  const canSeePosture = settingsProbe.status === 200;
  const claimsPlain = /Not encrypted/i.test(readersText);
  const claimsEncrypted = /(^|[^t])Encrypted/i.test(readersText.replace(/Not encrypted/gi, ''));
  check(`the ${ROLE}'s Readers screen does not claim an ENCRYPTED reader is in the clear`,
    !claimsPlain,
    'can read the security posture: ' + canSeePosture
      + '; screen says "Not encrypted": ' + claimsPlain
      + '; screen says "Encrypted": ' + claimsEncrypted
      + '; row: ' + (readersText.split('\n').slice(-4).join(' | ')));
  await shoot('c3-readers');

  // ---- 8. the access log, which is evidentiary and readable by both roles -------------------
  await goto('Activity');
  await sleep(2000);
  const activityText = await mainText();
  const eventsRead = await api('/api/events?limit=5');
  check(`a ${ROLE} can read the access log`, eventsRead.status === 200,
    'GET /api/events -> ' + eventsRead.status);
  check('and the Activity screen actually renders rows rather than an error',
    activityText.length > 120 && !/Request failed/i.test(activityText),
    activityText.slice(0, 200).replace(/\n/g, ' | '));
  await shoot('c4-activity');
};
