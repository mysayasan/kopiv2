import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { apiBase, parseFaceRuleConfig, faceRuleConfigText, formatTimestamp } from '../lib/helpers';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';
import { Ico } from './icons';
import { FormAlert, Toggle } from './ui';

// FacesTab is the face-recognition enrollment surface: a GLOBAL roster of people the system should
// recognize, plus a per-camera switch to turn recognition on. Enrolling a person is instant (no
// training) — a photo becomes a faceprint stored in the encrypted gallery, and any camera with face
// recognition enabled then names them when they appear.
//
// FACE TEMPLATES ARE BIOMETRIC DATA. The feature is off until someone is enrolled AND a camera is
// switched on, and the page leads with a consent notice — enroll only people who have agreed.
//
// THE SHAPE OF THIS SCREEN FOLLOWS ONE FACT: a person with no faceprints is not recognized by
// anything. Creating them writes a name and nothing else — the worker's gallery skips a person with
// zero embeddings entirely. So naming somebody is not the task, it is the first half of the task,
// and the screen has to carry the operator into the second half instead of leaving a row that looks
// finished. Adding a person therefore opens the enrolment dialog on that person straight away, the
// roster states each person's photo count in plain words, and a person at zero is marked as not yet
// recognized rather than silently sitting there looking enrolled.

const CONSENT_KEY = 'mymatasan_face_consent';

// The point past which a person's photo count reads as settled rather than thin. It matches the
// "10-30 is good" guidance in faces.photoHint on purpose — a card that turns green at 3 while the
// hint asks for 10 is the screen contradicting itself.
const GOOD_PHOTO_COUNT = 10;

// A roster of six people and a roster of a hundred are different screens. Past this many, the panel
// grows the controls that make a directory findable — search, state filters, a sort, and a compact
// row view. Below it they would be four controls sitting above six cards: furniture, not help.
const CONTROLS_FROM = 12;

// How many people are drawn before "Show more". A card carries a 46px avatar and two lines of its
// own chrome; a row is a quarter of that, so rows page in bigger batches. The cap exists because a
// hundred cards is a hundred base64 avatars decoded into one layout — the screen an operator sees
// should be the part they can read.
const PAGE = { cards: 24, list: 60 };

const VIEW_KEY = 'mymatasan_faces_view';
const SORT_KEY = 'mymatasan_faces_sort';

// The states an operator hunts for in a long roster — and every one of them is a state the person's
// own card already reports out loud. Nothing here filters by something invisible.
const FILTERS = [
  { key: 'all', label: 'faces.filterAll', match: () => true },
  { key: 'nophotos', label: 'faces.filterNeedsPhotos', match: (p) => (p.photos || 0) === 0 },
  { key: 'thin', label: 'faces.filterThin', match: (p) => (p.photos || 0) > 0 && (p.photos || 0) < GOOD_PHOTO_COUNT },
  { key: 'off', label: 'faces.filterOff', match: (p) => !p.enabled },
];

const SORTS = ['name', 'seen', 'photos'];

// Names are typed from memory, not copied: "zaid" has to find "Zaïd", and a trailing space from a
// paste has to find anybody at all. Case and combining marks are dropped on both sides.
function foldName(value) {
  return String(value || '').toLowerCase().normalize('NFD').replace(/[\u0300-\u036f]/g, '');
}

// The letter a name files under in the A-Z dividers. Deliberately NOT restricted to Latin: an
// Arabic or Chinese roster gets its own initials rather than one giant "#" bucket.
function letterOf(name) {
  const c = String(name || '').trim().normalize('NFD').replace(/[\u0300-\u036f]/g, '').charAt(0).toUpperCase();
  return c || '#';
}

// The photo count is the whole truth about a person: at zero they are not in the gallery the
// matcher reads, so they are not recognized by anything.
function photoClass(photos) {
  return photos === 0 ? ' is-warn' : photos < GOOD_PHOTO_COUNT ? ' is-thin' : ' is-ok';
}

async function fileToBase64(file) {
  const buf = await file.arrayBuffer();
  let binary = '';
  const bytes = new Uint8Array(buf);
  for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]);
  return btoa(binary);
}

// Renders **bold** markdown-style segments as <strong> so translated consent copy can carry
// emphasis without embedding JSX in the dictionary.
function withBold(text) {
  const parts = String(text).split(/\*\*(.+?)\*\*/g);
  return parts.map((part, i) => (i % 2 === 1 ? <strong key={i}>{part}</strong> : part));
}

// relativeWhen turns an epoch into "4 minutes ago" / "yesterday 18:12". A face sighting is read
// as "is this working, and how recently" — an absolute timestamp makes the reader do subtraction
// to answer either.
function relativeWhen(t, at) {
  const secs = Math.max(0, Math.floor(Date.now() / 1000) - Number(at || 0));
  if (!at) return '';
  if (secs < 60) return t('faces.justNow');
  if (secs < 3600) return t('faces.minsAgo', { n: Math.floor(secs / 60) });
  if (secs < 86400) return t('faces.hoursAgo', { n: Math.floor(secs / 3600) });
  return formatTimestamp(at);
}

function initial(name) {
  return (String(name || '?').trim().slice(0, 1) || '?').toUpperCase();
}

// Avatar shows the person's representative face crop, or their initial. It is the one place the
// roster can be scanned by eye rather than read, so it is the largest thing on the card.
function Avatar({ person, size = 'md' }) {
  const cls = `faces-avatar faces-avatar--${size}`;
  if (person?.thumbnail) {
    return <img className={cls} alt="" src={`data:image/jpeg;base64,${person.thumbnail}`} />;
  }
  return <span className={`${cls} is-placeholder`} aria-hidden="true">{initial(person?.name)}</span>;
}

// One person as a CARD: avatar-first, for a roster short enough to recognize by face.
function PersonCard({ p, seen, seenText, onOpen, onToggle, onRemove }) {
  const t = useT();
  const photos = p.photos || 0;
  return (
    <li data-person-id={p.id} className={`faces-card${p.enabled ? '' : ' is-off'}`}>
      <button type="button" className="faces-card-main" onClick={onOpen} aria-label={t('faces.openEnroll', { name: p.name })}>
        <Avatar person={p} />
        <span className="faces-card-text">
          {/* A person's name is data, not translated copy: in an RTL page a Latin name (or one
              with punctuation) reorders unless it is isolated. */}
          <span className="faces-card-name"><bdi>{p.name}</bdi></span>
          <span className={`faces-pill${photoClass(photos)}`} data-photos={photos}>
            {photos === 0 ? t('faces.noPhotosYet') : t('faces.photoCount', { n: photos })}
          </span>
          {/* The proof the feature is doing anything, on the card of the person it happened to. A
              recognition is already an alert, a snapshot, a clip and a notification — all of them
              on other screens. */}
          <span className="faces-seen" data-seen={seen ? seen.at : 0}>{seenText}</span>
        </span>
        <span className="faces-card-go" aria-hidden="true"><Ico n="chev-right" sz={16} /></span>
      </button>
      <div className="faces-card-foot">
        <Toggle
          checked={!!p.enabled}
          onChange={onToggle}
          label={t('faces.recognizeThem')}
          ariaLabel={t('faces.recognizeThemAria', { name: p.name })}
        />
        <button
          type="button"
          className="faces-icon-btn danger-text"
          onClick={onRemove}
          title={t('faces.delete')}
          aria-label={t('faces.deleteAria', { name: p.name })}
        >
          <Ico n="trash" sz={14} />
        </button>
      </div>
    </li>
  );
}

// The same person as a ROW: identical facts, one line, ~44px instead of ~150px. This is the view a
// hundred-person roster opens in, because a directory that long is read by NAME — you already know
// who you are looking for — and a wall of face cards is the slowest possible way to answer that.
function PersonRow({ p, seen, seenText, onOpen, onToggle, onRemove }) {
  const t = useT();
  const photos = p.photos || 0;
  return (
    <li data-person-id={p.id} className={`faces-row${p.enabled ? '' : ' is-off'}`}>
      <button type="button" className="faces-row-main" onClick={onOpen} aria-label={t('faces.openEnroll', { name: p.name })}>
        <Avatar person={p} size="sm" />
        <span className="faces-row-name"><bdi>{p.name}</bdi></span>
        <span className={`faces-pill${photoClass(photos)}`} data-photos={photos}>
          {photos === 0 ? t('faces.noPhotosYet') : t('faces.photoCount', { n: photos })}
        </span>
        <span className="faces-seen" data-seen={seen ? seen.at : 0}>{seenText}</span>
      </button>
      <div className="faces-row-actions">
        <Toggle
          checked={!!p.enabled}
          onChange={onToggle}
          ariaLabel={t('faces.recognizeThemAria', { name: p.name })}
        />
        <button
          type="button"
          className="faces-icon-btn danger-text"
          onClick={onRemove}
          title={t('faces.delete')}
          aria-label={t('faces.deleteAria', { name: p.name })}
        >
          <Ico n="trash" sz={14} />
        </button>
      </div>
    </li>
  );
}

export function FacesTab({ authHeader, cameras = [], onMessage }) {
  const t = useT();
  const [people, setPeople] = useState([]);
  const [rules, setRules] = useState([]);
  // What the host still needs before a face can be enrolled at all. Loaded here rather than only
  // in the dialog so the answer is on screen BEFORE somebody picks a photo and is told no.
  const [setup, setSetup] = useState(undefined);
  // The most recent recognition per person, from the alert log. Without this the roster could not
  // tell an operator whether ANY of this has ever worked — the alerts, clips and notifications a
  // match produces all land on other screens.
  const [sightings, setSightings] = useState({ items: [], unknownAt: 0, unknownCount: 0 });
  // The camera whose watchlist is open but NOT yet saved. A watchlist rule with nobody on it
  // alerts on nobody, so the server refuses one (validateFaceRule) — which means "Only chosen
  // people" cannot be saved at the moment it is selected, before anyone has been chosen. Rather
  // than save something else and pretend, the row opens the list and waits: the mode commits with
  // the first person ticked. Setting the select and having it snap back with a rejection toast is
  // the version of this that wastes somebody's afternoon.
  const [pendingInclude, setPendingInclude] = useState(0);
  const [busy, setBusy] = useState(true);
  const [newName, setNewName] = useState('');
  const [adding, setAdding] = useState(false);
  const [consented, setConsented] = useState(() => localStorage.getItem(CONSENT_KEY) === '1');
  // The person whose enrolment dialog is open. Set the moment a person is created, so "add" flows
  // straight into "give them a face" instead of ending on a row nobody notices.
  const [enrolling, setEnrolling] = useState(null);
  // How the roster is being narrowed. None of it is persisted except the two SHAPE choices (view
  // and sort): a search or a filter left over from last week would hide people from somebody who
  // does not remember typing it.
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState('all');
  const [sort, setSort] = useState(() => {
    const saved = localStorage.getItem(SORT_KEY);
    return SORTS.includes(saved) ? saved : 'name';
  });
  // '' means "let the size of the roster decide" — an explicit choice sticks.
  const [view, setView] = useState(() => {
    const saved = localStorage.getItem(VIEW_KEY);
    return saved === 'cards' || saved === 'list' ? saved : '';
  });
  const [shown, setShown] = useState(PAGE.cards);
  const nameRef = useRef(null);

  // NOTE THE /api PREFIX. Without it every request on this page went to `${origin}/faces`, which
  // the SPA's catch-all answers with index.html and a 200 — so the roster read an HTML page as its
  // person list (silently empty), and "Add person" reported success while creating nobody. The
  // screen looked like it worked and did nothing. Every other screen builds `${apiBase()}/api/...`;
  // this one had drifted.
  const api = useCallback(async (path, options = {}) => {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}/api${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    let parsed = false;
    if (text) { try { payload = JSON.parse(text); parsed = true; } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    // A 200 that is not JSON is the catch-all page, not an answer. Say so instead of quietly
    // reading `undefined.items` and rendering an empty, contented-looking screen.
    if (text && !parsed) throw new Error(t('faces.apiNotJson', { path }));
    return payload?.data?.result ?? payload?.result ?? payload;
  }, [authHeader, t]);

  const loadSightings = useCallback(async () => {
    try {
      const res = await api('/faces/sightings');
      setSightings({
        items: res?.items || [],
        unknownAt: res?.unknownAt || 0,
        unknownCount: res?.unknownCount || 0,
      });
    } catch (_) { /* the roster still works without it; it just cannot say "last seen" */ }
  }, [api]);

  const loadSetup = useCallback(async () => {
    try { setSetup(await api('/faces/models')); }
    catch (_) { setSetup(null); } // the panel says "could not check" rather than claiming ready
  }, [api]);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const [pp, rr] = await Promise.all([api('/faces'), api('/vision/rules')]);
      setPeople(pp?.items || []);
      setRules((rr?.items || rr || []).filter((r) => (r.detectionType || '').toLowerCase() === 'face'));
    } catch (e) {
      onMessage?.(e.message, 'error');
    } finally {
      setBusy(false);
    }
  }, [api, onMessage]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { loadSetup(); }, [loadSetup]);
  useEffect(() => { loadSightings(); }, [loadSightings]);

  // Keep the open dialog's person in step with the roster: enrolling a photo changes the count and
  // the thumbnail, and the dialog header says both.
  const openPerson = useMemo(
    () => (enrolling ? people.find((p) => p.id === enrolling.id) || enrolling : null),
    [enrolling, people],
  );

  const enrolledCount = people.filter((p) => (p.photos || 0) > 0 && p.enabled).length;
  const camerasOn = cameras.filter((c) => rules.some((r) => r.cameraId === c.id)).length;
  const sightingByPerson = useMemo(() => {
    const map = new Map();
    (sightings.items || []).forEach((s) => map.set(Number(s.personId), s));
    return map;
  }, [sightings]);
  const cameraNameById = useMemo(() => {
    const map = new Map();
    cameras.forEach((c) => map.set(Number(c.id), c.name || t('notif.cameraN', { id: c.id })));
    return map;
  }, [cameras, t]);
  // Names offered to a watchlist rule. The rule matches by NAME (that is what the worker emits and
  // what infra/vision/face.go compares), so a renamed person silently drops out of their own
  // watchlist — said out loud in the hint rather than left to be discovered.
  const enrolledNames = people.filter((p) => (p.photos || 0) > 0).map((p) => p.name);

  // "Last seen" as one sentence, resolved here because it needs the camera names and the sightings
  // index; both list shapes then render the same string.
  const seenTextFor = useCallback((p) => {
    const seen = sightingByPerson.get(Number(p.id));
    if (seen) {
      return t('faces.seenAt', {
        when: relativeWhen(t, seen.at),
        camera: cameraNameById.get(Number(seen.cameraId)) || t('notif.cameraN', { id: seen.cameraId }),
      });
    }
    return (p.photos || 0) > 0 ? t('faces.notSeenYet') : '';
  }, [cameraNameById, sightingByPerson, t]);

  // Every chip carries its own count, so the roster answers "how many of my people still have no
  // photos?" without anybody clicking anything — which is the question this screen exists for.
  const filterCounts = useMemo(() => {
    const counts = {};
    FILTERS.forEach((f) => { counts[f.key] = people.filter(f.match).length; });
    return counts;
  }, [people]);

  // Cards below the threshold, rows above it, unless somebody has said otherwise. The default is
  // the one that stays usable as the roster grows past the point where faces can be scanned.
  const effView = view || (people.length > PAGE.cards ? 'list' : 'cards');
  const pageSize = PAGE[effView];
  const showControls = people.length >= CONTROLS_FROM;

  const visible = useMemo(() => {
    const q = foldName(query.trim());
    const f = FILTERS.find((x) => x.key === filter) || FILTERS[0];
    const seenAt = (p) => Number(sightingByPerson.get(Number(p.id))?.at || 0);
    const byName = (a, b) => String(a.name || '').localeCompare(String(b.name || ''));
    const rows = people.filter((p) => f.match(p) && (!q || foldName(p.name).includes(q)));
    if (sort === 'seen') return rows.sort((a, b) => seenAt(b) - seenAt(a) || byName(a, b));
    if (sort === 'photos') return rows.sort((a, b) => (a.photos || 0) - (b.photos || 0) || byName(a, b));
    return rows.sort(byName);
  }, [people, query, filter, sort, sightingByPerson]);

  // Narrowing the list resets the page. Without this, searching inside an expanded roster shows a
  // "Show more" that is already exhausted, and clearing the search leaves 300 rows mounted.
  useEffect(() => { setShown(pageSize); }, [query, filter, sort, pageSize]);

  const slice = visible.slice(0, shown);
  // A-Z dividers only when the order actually IS A-Z; over any other sort they would be lying
  // about where the next name begins.
  const showLetters = effView === 'list' && sort === 'name' && showControls;

  function chooseView(next) {
    setView(next);
    try { localStorage.setItem(VIEW_KEY, next); } catch (_) { /* private mode: the choice is just not remembered */ }
  }

  function chooseSort(next) {
    setSort(next);
    try { localStorage.setItem(SORT_KEY, next); } catch (_) { /* as above */ }
  }

  function clearFilters() {
    setQuery('');
    setFilter('all');
  }

  // saveRuleConfig writes a face rule's policy back.
  //
  // An update is POST /vision/rules WITH an id — there is no PUT — and the handler decodes with
  // DisallowUnknownFields, so the body must be exactly DetectionRuleRequest. Spreading the rule
  // row from the list API sends createdAt/updatedAt too and the request is refused; every field is
  // therefore named explicitly, and every one the face policy does not own is carried through
  // unchanged so an edit here cannot quietly reset a zone, a schedule or a cooldown.
  async function saveRuleConfig(rule, nextConfig) {
    try {
      await api('/vision/rules', {
        method: 'POST',
        body: JSON.stringify({
          id: rule.id,
          cameraId: rule.cameraId,
          name: rule.name,
          detectionType: rule.detectionType,
          zonePolygon: rule.zonePolygon || '',
          ruleConfig: nextConfig,
          schedulePolicy: rule.schedulePolicy || '',
          threshold: rule.threshold || 0,
          minFrames: rule.minFrames || 0,
          cooldownSeconds: rule.cooldownSeconds || 0,
          soundEnabled: !!rule.soundEnabled,
          archiveClip: !!rule.archiveClip,
          isEnabled: rule.isEnabled !== false,
        }),
      });
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function addPerson(e) {
    e.preventDefault();
    const name = newName.trim();
    if (!name || adding) return;
    setAdding(true);
    try {
      const p = await api('/faces', { method: 'POST', body: JSON.stringify({ name }) });
      setNewName('');
      onMessage?.(t('faces.added', { name }), 'success');
      await load();
      // Straight into enrolment: a named person with no photos is recognized by nothing, and this
      // is the only moment we can be sure the operator is thinking about that person.
      if (p?.id) setEnrolling({ id: p.id, name, photos: 0 });
      else nameRef.current?.focus();
    } catch (err) { onMessage?.(err.message, 'error'); }
    finally { setAdding(false); }
  }

  async function removePerson(id, name) {
    if (!window.confirm(t('faces.deleteConfirm', { name }))) return;
    try {
      await api(`/faces/${id}`, { method: 'DELETE' });
      onMessage?.(t('faces.deleted', { name }), 'success');
      if (enrolling?.id === id) setEnrolling(null);
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function togglePerson(p) {
    try {
      await api(`/faces/${p.id}`, { method: 'PUT', body: JSON.stringify({ name: p.name, notes: p.notes || '', enabled: !p.enabled }) });
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function toggleCamera(camera) {
    const existing = rules.find((r) => r.cameraId === camera.id);
    try {
      if (existing) {
        await api(`/vision/rules/${existing.id}`, { method: 'DELETE' });
      } else {
        await api('/vision/rules', {
          method: 'POST',
          body: JSON.stringify({
            cameraId: camera.id, name: 'Face recognition', detectionType: 'face',
            ruleConfig: faceRuleConfigText({ matchMode: 'known' }), isEnabled: true, minFrames: 1, cooldownSeconds: 60,
          }),
        });
      }
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  if (!consented) {
    return (
      <section className="workspace faces-page">
        <div className="settings-panel faces-consent">
          {/* The consent gate is precisely where somebody should be able to read what they are
              agreeing to, so the help link lives here as well as on the roster behind it. */}
          <div className="faces-consent-head">
            <span className="faces-consent-mark" aria-hidden="true"><Ico n="shield" sz={22} /></span>
            <h2>
              {t('faces.consentTitle')}
              <HelpButton slug="people" anchor="consent" />
            </h2>
          </div>
          <p>{withBold(t('faces.consentP1'))}</p>
          <p>{t('faces.consentP2')}</p>
          <div className="modal-actions">
            <button type="button" onClick={() => { localStorage.setItem(CONSENT_KEY, '1'); setConsented(true); }}>
              {t('faces.consentAccept')}
            </button>
          </div>
        </div>
      </section>
    );
  }

  return (
    <section className="workspace faces-page">
      <div className="toolbar">
        <div>
          <h2 className="section-title">
            {t('faces.title')}
            {/* Points at the consent section, not the top of the article: enrolling somebody
                stores biometric data, and that is the part an operator needs to have read. */}
            <HelpButton slug="people" anchor="consent" />
          </h2>
          <p className="section-subtitle">{t('faces.subtitle')}</p>
        </div>
        <form className="faces-add" onSubmit={addPerson}>
          <input
            ref={nameRef}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder={t('faces.namePlaceholder')}
            aria-label={t('faces.nameLabel')}
            disabled={adding}
          />
          <button type="submit" disabled={adding || !newName.trim()}>
            <span className="btn-icon"><Ico n="user-plus" sz={14} /> {t('faces.addPerson')}</span>
          </button>
        </form>
      </div>

      {/* The one-time host setup, when it is outstanding. It sits above everything because no
          amount of enrolling works until it is done, and because the old behaviour — let them add
          people, then fail on the photo with a message naming a PowerShell script — is exactly the
          kind of dead end this screen should never hand anybody. */}
      <FaceModelsSetup
        authHeader={authHeader}
        status={setup}
        onRefresh={loadSetup}
        onMessage={onMessage}
      />

      {/* What the system is actually doing right now, in one line. Face recognition needs BOTH an
          enrolled person and a switched-on camera, and an operator who has done only one of the two
          has every reason to believe it is running.
          SUPPRESSED while the host setup is outstanding: with no models nothing is recognized no
          matter how many people and cameras are switched on, and a green "Recognizing 1 person on 2
          cameras" under a banner saying the models are missing is the screen contradicting itself.
          The banner is the whole truth in that state. */}
      {setup && !setup.ready ? null : (
      <p className={`faces-state${enrolledCount && camerasOn ? ' is-live' : ''}`}>
        <Ico n={enrolledCount && camerasOn ? 'check-ok' : 'info'} sz={14} />
        {enrolledCount && camerasOn
          ? t('faces.stateLive', { people: enrolledCount, cameras: camerasOn })
          : t('faces.stateIdle', { people: enrolledCount, cameras: camerasOn })}
      </p>
      )}

      <div className="settings-panel faces-roster">
        <header>
          <h2>{t('faces.rosterTitle')}</h2>
          {/* The count is also the feedback that a filter did something, so it says "12 of 128"
              the moment the two numbers differ, and announces the change to a screen reader. */}
          <span className="settings-hint" aria-live="polite">
            {visible.length === people.length
              ? t('faces.rosterCount', { n: people.length })
              : t('faces.showingOf', { n: visible.length, total: people.length })}
          </span>
        </header>

        {/* Search / filter / sort / view. Hidden under CONTROLS_FROM people, where the whole
            roster already fits on one screen and every one of these controls would be a thing to
            read past. */}
        {showControls ? (
          <div className="faces-controls">
            <div className="faces-search">
              <Ico n="search" sz={14} />
              <input
                type="search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t('faces.searchPlaceholder', { n: people.length })}
                aria-label={t('faces.searchLabel')}
              />
              {query ? (
                <button
                  type="button"
                  className="faces-search-clear"
                  onClick={() => setQuery('')}
                  aria-label={t('faces.searchClear')}
                  title={t('faces.searchClear')}
                >
                  <Ico n="x" sz={12} />
                </button>
              ) : null}
            </div>

            <div className="faces-chips" role="group" aria-label={t('faces.filterAria')}>
              {FILTERS.map((f) => (
                <button
                  key={f.key}
                  type="button"
                  className={`faces-chip${filter === f.key ? ' active' : ''}`}
                  aria-pressed={filter === f.key}
                  onClick={() => setFilter(f.key)}
                  data-filter={f.key}
                >
                  {t(f.label)}
                  <span className="faces-chip-n">{filterCounts[f.key] || 0}</span>
                </button>
              ))}
            </div>

            <div className="faces-controls-right">
              <select
                className="faces-sort"
                value={sort}
                onChange={(e) => chooseSort(e.target.value)}
                aria-label={t('faces.sortLabel')}
              >
                <option value="name">{t('faces.sortName')}</option>
                <option value="seen">{t('faces.sortSeen')}</option>
                <option value="photos">{t('faces.sortPhotos')}</option>
              </select>

              <div className="seg-toggle faces-view" role="group" aria-label={t('faces.viewAria')}>
                <button
                  type="button"
                  className={effView === 'list' ? 'active' : ''}
                  aria-pressed={effView === 'list'}
                  onClick={() => chooseView('list')}
                  title={t('faces.viewList')}
                  aria-label={t('faces.viewList')}
                >
                  <Ico n="list" sz={14} />
                </button>
                <button
                  type="button"
                  className={effView === 'cards' ? 'active' : ''}
                  aria-pressed={effView === 'cards'}
                  onClick={() => chooseView('cards')}
                  title={t('faces.viewCards')}
                  aria-label={t('faces.viewCards')}
                >
                  <Ico n="grid4" sz={14} />
                </button>
              </div>
            </div>
          </div>
        ) : null}

        {busy && people.length === 0 ? <p className="settings-hint">{t('faces.loading')}</p> : null}

        {!busy && people.length === 0 ? (
          <div className="faces-empty">
            <span className="faces-empty-mark" aria-hidden="true"><Ico n="user-plus" sz={26} /></span>
            <p className="faces-empty-title">{t('faces.emptyTitle')}</p>
            <p className="settings-hint">{t('faces.emptyList')}</p>
            <button type="button" className="quiet" onClick={() => nameRef.current?.focus()}>
              {t('faces.emptyAction')}
            </button>
          </div>
        ) : null}

        {/* A roster with people in it that shows none of them has to say why, and offer the way
            back — otherwise a stale filter reads as "everybody is gone". */}
        {people.length > 0 && visible.length === 0 ? (
          <div className="faces-empty">
            <span className="faces-empty-mark" aria-hidden="true"><Ico n="search" sz={26} /></span>
            <p className="faces-empty-title">{query ? t('faces.noMatch') : t('faces.noMatchFilter')}</p>
            <button type="button" className="quiet" onClick={clearFilters}>{t('faces.clearFilters')}</button>
          </div>
        ) : null}

        {slice.length > 0 && effView === 'cards' ? (
          <ul className="faces-grid">
            {slice.map((p) => (
              <PersonCard
                key={p.id}
                p={p}
                seen={sightingByPerson.get(Number(p.id))}
                seenText={seenTextFor(p)}
                onOpen={() => setEnrolling(p)}
                onToggle={() => togglePerson(p)}
                onRemove={() => removePerson(p.id, p.name)}
              />
            ))}
          </ul>
        ) : null}

        {slice.length > 0 && effView === 'list' ? (
          <ul className="faces-rows">
            {slice.map((p, i) => {
              const letter = letterOf(p.name);
              const heads = showLetters && (i === 0 || letterOf(slice[i - 1].name) !== letter);
              return (
                <Fragment key={p.id}>
                  {heads ? <li className="faces-row-letter" aria-hidden="true">{letter}</li> : null}
                  <PersonRow
                    p={p}
                    seen={sightingByPerson.get(Number(p.id))}
                    seenText={seenTextFor(p)}
                    onOpen={() => setEnrolling(p)}
                    onToggle={() => togglePerson(p)}
                    onRemove={() => removePerson(p.id, p.name)}
                  />
                </Fragment>
              );
            })}
          </ul>
        ) : null}

        {/* Paging, not virtualization: the roster is a few hundred people at the outside, and a
            button that says how many are still hidden is honest in a way an infinite scroll that
            silently stops fetching is not. */}
        {visible.length > slice.length ? (
          <div className="faces-more">
            <span className="settings-hint">{t('faces.showingOf', { n: slice.length, total: visible.length })}</span>
            <button type="button" className="quiet" onClick={() => setShown(shown + pageSize)}>
              {t('faces.showMore', { n: Math.min(pageSize, visible.length - slice.length) })}
            </button>
            <button type="button" className="faces-more-all" onClick={() => setShown(visible.length)}>
              {t('faces.showAll', { n: visible.length })}
            </button>
          </div>
        ) : null}
      </div>

      <div className="settings-panel">
        <header>
          <h2>{t('faces.recognizeOn')}</h2>
          <span className="settings-hint">{t('faces.camerasOn', { n: camerasOn, total: cameras.length })}</span>
        </header>
        <p className="settings-hint">{t('faces.recognizeHint')}</p>
        {cameras.length === 0 ? <p className="settings-hint">{t('faces.noCameras')}</p> : null}
        <ul className="faces-camera-list">
          {cameras.map((c) => {
            const rule = rules.find((r) => r.cameraId === c.id);
            const on = !!rule;
            const name = c.name || t('notif.cameraN', { id: c.id });
            const cfg = on ? parseFaceRuleConfig(rule.ruleConfig) : null;
            // What the row SHOWS: the saved policy, or "include" while its watchlist is being
            // filled in for the first time.
            const mode = on ? (pendingInclude === c.id ? 'include' : cfg.matchMode) : '';
            return (
              <li key={c.id} data-camera-id={c.id} className={on ? 'is-on' : ''}>
                <div className="faces-camera-row">
                  <span className="faces-camera-mark" aria-hidden="true"><Ico n="camera" sz={15} /></span>
                  <span className="faces-camera-name">{name}</span>
                  {/* WHICH faces are worth an alert. All three modes have worked in the detector
                      since face rules shipped; this screen used to hardcode "known" and offer no
                      way to ask for the other two, so stranger detection existed and could not be
                      switched on. */}
                  {on ? (
                    <select
                      className="faces-camera-mode"
                      value={mode}
                      aria-label={t('faces.modeForCamera', { name })}
                      onChange={(e) => {
                        const matchMode = e.target.value;
                        if (matchMode === 'include' && cfg.people.length === 0) {
                          setPendingInclude(c.id); // open the list; the first tick commits it
                          return;
                        }
                        setPendingInclude(0);
                        saveRuleConfig(rule, faceRuleConfigText({ ...cfg, matchMode }));
                      }}
                    >
                      <option value="known">{t('faces.alertKnown')}</option>
                      <option value="include">{t('faces.alertInclude')}</option>
                      <option value="unknown">{t('faces.alertUnknown')}</option>
                    </select>
                  ) : null}
                  <Toggle checked={on} onChange={() => toggleCamera(c)} ariaLabel={t('faces.cameraToggleAria', { name })} />
                </div>
                {on && mode === 'include' ? (
                  <div className="faces-watchlist">
                    <p className="settings-hint">{t('faces.watchlistHint')}</p>
                    {enrolledNames.length === 0 ? <p className="settings-hint">{t('faces.watchlistEmpty')}</p> : null}
                    <div className="faces-watchlist-names">
                      {enrolledNames.map((personName) => {
                        const picked = cfg.people.some((p) => p.toLowerCase() === personName.toLowerCase());
                        return (
                          <label key={personName} className="faces-watchlist-name">
                            <input
                              type="checkbox"
                              checked={picked}
                              onChange={(e) => {
                                const next = e.target.checked
                                  ? [...cfg.people, personName]
                                  : cfg.people.filter((p) => p.toLowerCase() !== personName.toLowerCase());
                                if (next.length === 0) {
                                  // The server refuses a watchlist of nobody, and it is right to:
                                  // the rule would be switched on and match no one. Say that
                                  // instead of sending a request that comes back as an error.
                                  onMessage?.(t('faces.watchlistNeedsOne'), 'error');
                                  return;
                                }
                                setPendingInclude(0);
                                saveRuleConfig(rule, faceRuleConfigText({ ...cfg, matchMode: 'include', people: next }));
                              }}
                            />
                            <bdi>{personName}</bdi>
                          </label>
                        );
                      })}
                    </div>
                    {cfg.people.length === 0 ? (
                      <p className="faces-watchlist-warn">
                        <Ico n="warning" sz={13} /> {t('faces.watchlistPickOne', { mode: t(`faces.alert${cfg.matchMode === 'unknown' ? 'Unknown' : 'Known'}`) })}
                      </p>
                    ) : null}
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
        {/* What a rule DOES on a match, said once, where somebody has just switched one on. It is
            not obvious that any of this happens, because none of it happens on this screen. */}
        {camerasOn > 0 ? <p className="settings-hint faces-actions-note"><Ico n="info" sz={13} /> {t('faces.whatHappens')}</p> : null}
        {sightings.unknownCount > 0 ? (
          <p className="settings-hint faces-unknown-note">
            <Ico n="user" sz={13} /> {t('faces.unknownSeen', { n: sightings.unknownCount, when: relativeWhen(t, sightings.unknownAt) })}
          </p>
        ) : null}
      </div>

      {openPerson ? (
        <EnrollDialog
          api={api}
          authHeader={authHeader}
          person={openPerson}
          setup={setup}
          onSetupRefresh={loadSetup}
          onClose={() => { setEnrolling(null); load(); }}
          onChanged={load}
          onMessage={onMessage}
        />
      ) : null}
    </section>
  );
}

// EnrollDialog is where a name becomes something the system can recognize. It shows what is already
// enrolled (so a bad photo can be found and removed — one bad faceprint quietly poisons every future
// match) and offers the two ways to add one: a file from this computer, or a shot from this
// computer's own camera. Both post to the SAME endpoint; only the recorded `source` differs.
function EnrollDialog({ api, authHeader, person, setup, onSetupRefresh, onClose, onChanged, onMessage }) {
  const t = useT();
  const [mode, setMode] = useState('upload');
  const [shots, setShots] = useState([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  // What the dialog is busy WITH, so the notice can name it: enrolling a photo and removing a
  // faceprint freeze the same controls, and they must not both say "uploading".
  const [busyKind, setBusyKind] = useState('');
  // A batch of photos is several round trips, not one. `progress` is what turns "busy" into "photo
  // 2 of 5", and it stays set across the GAPS between those posts — otherwise the notice blinks out
  // between files and the dialog looks briefly idle, and briefly closable, mid-upload.
  const [progress, setProgress] = useState(null);
  const [error, setError] = useState('');
  const [dragging, setDragging] = useState(false);
  const fileRef = useRef(null);

  // Every way out of this dialog is gated on `working`, never on `busy`: closing between two files
  // of a batch abandons the rest, with nothing on screen saying which ones made it.
  const working = busy || progress !== null;

  const loadShots = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api(`/faces/${person.id}/embeddings`);
      setShots(res?.items || []);
    } catch (err) { setError(err.message); }
    finally { setLoading(false); }
  }, [api, person.id]);

  useEffect(() => { loadShots(); }, [loadShots]);

  // Esc closes, like every other dialog in the app — except while photos are going up.
  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape' && !working) onClose(); }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose, working]);

  // The dialog is not the only way to walk out on an upload; so is closing the tab. Ask first.
  useEffect(() => {
    if (!working) return undefined;
    function onLeave(e) { e.preventDefault(); e.returnValue = ''; return ''; }
    window.addEventListener('beforeunload', onLeave);
    return () => window.removeEventListener('beforeunload', onLeave);
  }, [working]);

  // enroll posts one base64 JPEG and reports the outcome IN THE DIALOG. The server's rejections
  // ("no face found", "more than one face", "too small") are the operator's next instruction, so
  // they belong beside the picker they came from, not only in a toast that scrolls away.
  const enroll = useCallback(async (image, source, label) => {
    setBusy(true);
    setBusyKind('enroll');
    setError('');
    try {
      await api(`/faces/${person.id}/enroll`, { method: 'POST', body: JSON.stringify({ image, source }) });
      onMessage?.(t('faces.enrolled', { n: 1 }), 'success');
      await loadShots();
      onChanged?.();
      return true;
    } catch (err) {
      setError(label ? t('faces.enrollFileError', { file: label, error: err.message }) : err.message);
      // A rejected enrolment is the moment to re-ask what the host is missing: "face models are not
      // installed" is a host problem with a button, not a problem with the photo, and the panel
      // below turns the message into that button.
      onSetupRefresh?.();
      return false;
    } finally { setBusy(false); setBusyKind(''); }
  }, [api, loadShots, onChanged, onMessage, onSetupRefresh, person.id, t]);

  async function enrollFiles(files) {
    // A second drop landing on top of a running batch would interleave two counters and two sets of
    // errors in one dialog. One batch at a time.
    if (working) return;
    const list = Array.from(files).filter((f) => f.type.startsWith('image/'));
    if (list.length === 0) {
      setError(t('faces.notAnImage'));
      return;
    }
    let ok = 0;
    setProgress({ done: 0, total: list.length });
    try {
      for (let i = 0; i < list.length; i += 1) {
        const file = list[i];
        setProgress({ done: i, total: list.length });
        /* eslint-disable no-await-in-loop */
        try {
          const image = await fileToBase64(file);
          if (await enroll(image, 'upload', file.name)) ok += 1;
        } catch (err) {
          // An unreadable file is that file's problem, not the batch's: name it and keep going.
          // Stopping here would silently skip photos the operator watched themselves pick.
          setError(t('faces.enrollFileError', { file: file.name, error: err.message }));
        }
        /* eslint-enable no-await-in-loop */
      }
    } finally {
      // Whatever happened, the dialog has to become closable again — a notice that never clears
      // locks the operator in with no way out but the tab.
      setProgress(null);
    }
    if (ok > 1) onMessage?.(t('faces.enrolled', { n: ok }), 'success');
  }

  async function removeShot(shot) {
    setBusy(true);
    setBusyKind('delete');
    try {
      await api(`/faces/embeddings/${shot.id}`, { method: 'DELETE' });
      await loadShots();
      onChanged?.();
    } catch (err) { setError(err.message); }
    finally { setBusy(false); setBusyKind(''); }
  }

  return (
    <div className="modal-backdrop" onClick={working ? undefined : onClose}>
      <div className="modal-card faces-enroll" role="dialog" aria-modal="true" data-person-id={person.id} onClick={(e) => e.stopPropagation()}>
        <div className="faces-enroll-head">
          <Avatar person={person} size="sm" />
          <div className="faces-enroll-title">
            <h2>{t('faces.enrollTitle', { name: person.name })}</h2>
            <p className="settings-hint">
              {shots.length === 0 ? t('faces.enrollNone') : t('faces.enrollHave', { n: shots.length })}
            </p>
          </div>
          <button
            type="button"
            className="faces-icon-btn"
            onClick={onClose}
            disabled={working}
            aria-label={t('common.close')}
            title={working ? t('faces.uploadingKeepOpen') : t('common.close')}
          >
            <Ico n="x" sz={16} />
          </button>
        </div>

        <p className="settings-hint">{t('faces.photoHint')}</p>

        {/* The dialog has to stay open while a batch uploads — it is the thing doing the counting —
            so every exit is disabled underneath this notice. A control that refuses to work without
            saying why reads as broken, so this says why, and says how far along it is. */}
        {working ? (
          <div className="faces-uploading" role="status" aria-live="polite">
            <span className="form-busy-spinner faces-uploading-spin" aria-hidden="true" />
            <span className="faces-uploading-text">
              <strong>
                {busyKind === 'delete'
                  ? t('faces.removingShot')
                  : progress && progress.total > 1
                    ? t('faces.uploadingN', { n: progress.done + 1, total: progress.total })
                    : t('faces.uploadingOne')}
              </strong>
              <span className="settings-hint">{t('faces.uploadingKeepOpen')}</span>
            </span>
          </div>
        ) : null}

        {error ? <FormAlert message={error} /> : null}

        <FaceModelsSetup
          authHeader={authHeader}
          status={setup}
          onRefresh={onSetupRefresh}
          onMessage={onMessage}
        />

        <div className="seg-toggle faces-enroll-modes" role="group" aria-label={t('faces.modeAria')}>
          <button type="button" className={mode === 'upload' ? 'active' : ''} disabled={working} onClick={() => setMode('upload')}>
            <span className="btn-icon"><Ico n="upload" sz={14} /> {t('faces.modeUpload')}</span>
          </button>
          <button type="button" className={mode === 'camera' ? 'active' : ''} disabled={working} onClick={() => setMode('camera')}>
            <span className="btn-icon"><Ico n="camera" sz={14} /> {t('faces.modeCamera')}</span>
          </button>
        </div>

        {mode === 'upload' ? (
          <div
            className={`faces-drop${dragging ? ' is-dragging' : ''}${working ? ' is-busy' : ''}`}
            onDragOver={(e) => { if (working) return; e.preventDefault(); setDragging(true); }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragging(false);
              if (working) return;
              if (e.dataTransfer?.files?.length) enrollFiles(e.dataTransfer.files);
            }}
          >
            <span className="faces-drop-mark" aria-hidden="true"><Ico n="upload" sz={22} /></span>
            <p className="faces-drop-title">{t('faces.dropTitle')}</p>
            <p className="settings-hint">{t('faces.dropHint')}</p>
            <button type="button" className="quiet" disabled={working} onClick={() => fileRef.current?.click()}>
              {working ? t('faces.uploadingBtn') : t('faces.choosePhotos')}
            </button>
            {/* The native file input is the thing that actually opens the picker, but it is not the
                thing anybody should have to look at. */}
            <input
              ref={fileRef}
              className="faces-file-input"
              type="file"
              accept="image/*"
              multiple
              disabled={working}
              onChange={(e) => { if (e.target.files?.length) enrollFiles(e.target.files); e.target.value = ''; }}
            />
          </div>
        ) : (
          <WebcamCapture busy={working} onCapture={(image) => enroll(image, 'camera')} />
        )}

        <div className="faces-shots">
          <h3>{t('faces.shotsTitle')}</h3>
          {loading ? <p className="settings-hint">{t('faces.loading')}</p> : null}
          {!loading && shots.length === 0 ? <p className="settings-hint">{t('faces.shotsEmpty')}</p> : null}
          <ul>
            {shots.map((s) => (
              <li key={s.id} data-embedding-id={s.id}>
                {s.thumbnail
                  ? <img alt="" src={`data:image/jpeg;base64,${s.thumbnail}`} />
                  : <span className="faces-shot-none" aria-hidden="true"><Ico n="user" sz={18} /></span>}
                <span className="faces-shot-meta">
                  <Ico n={s.source === 'camera' ? 'camera' : 'upload'} sz={12} />
                  {t('faces.shotQuality', { pct: Math.round((s.quality || 0) * 100) })}
                </span>
                <button
                  type="button"
                  className="faces-shot-del"
                  disabled={working}
                  onClick={() => removeShot(s)}
                  aria-label={t('faces.deleteShot')}
                  title={t('faces.deleteShot')}
                >
                  <Ico n="trash" sz={12} />
                </button>
              </li>
            ))}
          </ul>
        </div>

        <div className="modal-actions">
          <button
            type="button"
            className="quiet"
            onClick={onClose}
            disabled={working}
            title={working ? t('faces.uploadingKeepOpen') : undefined}
          >
            {working ? t('faces.uploadingBtn') : t('common.close')}
          </button>
        </div>
      </div>
    </div>
  );
}

// WebcamCapture takes the shot from THIS computer's camera (not from an NVR camera — those are
// pointed at doorways, and enrolment wants a face at arm's length).
//
// Browsers only hand over a camera in a secure context, so on a plain-http LAN address
// navigator.mediaDevices is simply undefined. That is not a failure to report as "camera error":
// it is a fact about the address the operator typed, and it is said as such — otherwise somebody
// spends the afternoon looking for a broken webcam.
function WebcamCapture({ busy, onCapture }) {
  const t = useT();
  const videoRef = useRef(null);
  const streamRef = useRef(null);
  const [live, setLive] = useState(false);
  const [starting, setStarting] = useState(false);
  const [problem, setProblem] = useState('');

  const stop = useCallback(() => {
    const stream = streamRef.current;
    if (stream) stream.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
    if (videoRef.current) videoRef.current.srcObject = null;
    setLive(false);
  }, []);

  // The camera light must go out when this pane goes away — closing the dialog, switching back to
  // upload, or leaving the page.
  useEffect(() => stop, [stop]);

  async function start() {
    setProblem('');
    if (!navigator.mediaDevices?.getUserMedia) {
      setProblem(window.isSecureContext ? t('faces.camUnsupported') : t('faces.camInsecure'));
      return;
    }
    setStarting(true);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { width: { ideal: 1280 }, height: { ideal: 720 }, facingMode: 'user' },
        audio: false,
      });
      streamRef.current = stream;
      if (videoRef.current) {
        videoRef.current.srcObject = stream;
        await videoRef.current.play().catch(() => {});
      }
      setLive(true);
    } catch (err) {
      const name = err?.name || '';
      if (name === 'NotAllowedError' || name === 'SecurityError') setProblem(t('faces.camDenied'));
      else if (name === 'NotFoundError' || name === 'OverconstrainedError') setProblem(t('faces.camNone'));
      else if (name === 'NotReadableError') setProblem(t('faces.camBusy'));
      else setProblem(err?.message || t('faces.camNone'));
    } finally { setStarting(false); }
  }

  function capture() {
    const video = videoRef.current;
    if (!video || !video.videoWidth) return;
    const canvas = document.createElement('canvas');
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height);
    // JPEG, because that is what the enrolment endpoint and the embedder both expect.
    const dataUrl = canvas.toDataURL('image/jpeg', 0.92);
    onCapture(dataUrl.slice(dataUrl.indexOf(',') + 1));
  }

  return (
    <div className="faces-webcam">
      <div className={`faces-webcam-stage${live ? ' is-live' : ''}`}>
        <video ref={videoRef} playsInline muted autoPlay aria-label={t('faces.camPreview')} />
        {!live ? (
          <div className="faces-webcam-idle">
            <span aria-hidden="true"><Ico n="camera" sz={26} /></span>
            <p className="settings-hint">{t('faces.camIdle')}</p>
          </div>
        ) : null}
      </div>
      {problem ? <FormAlert message={problem} /> : null}
      <div className="faces-webcam-actions">
        {live ? (
          <>
            <button type="button" disabled={busy} onClick={capture}>
              <span className="btn-icon"><Ico n="camera" sz={14} /> {t('faces.capture')}</span>
            </button>
            <button type="button" className="quiet" onClick={stop}>{t('faces.camStop')}</button>
          </>
        ) : (
          <button type="button" className="quiet" disabled={starting} onClick={start}>
            <span className="btn-icon"><Ico n="camera" sz={14} /> {starting ? t('faces.camStarting') : t('faces.camStart')}</span>
          </button>
        )}
      </div>
      <p className="settings-hint">{t('faces.camHint')}</p>
    </div>
  );
}


// FaceModelsSetup is the answer to "face models are not installed — run the face-recognition
// setup". That message was true and useless: the setup it named was a PowerShell script in the
// source tree, which is not a thing to tell somebody who is standing in a browser trying to enrol a
// face. AN ERROR WITHOUT A ROUTE TO THE FIX IS A HALF-BUILT FEATURE, so the route is a button.
//
// It renders NOTHING when the host is ready (unless `always`, for the Settings page, where the
// point is to show the state whatever it is), so the People screen is clean on a working install.
//
// The work runs server-side as a background job — pip for opencv-python if the detector's
// interpreter lacks it, then the two .onnx models — and the live log streams into the box, because
// a 37 MB download behind a spinner with no output is indistinguishable from a hang.
export function FaceModelsSetup({ authHeader, status, onRefresh, onMessage, always = false }) {
  const t = useT();
  const [installing, setInstalling] = useState(false);
  const [log, setLog] = useState('');
  const [failed, setFailed] = useState(false);

  const api = useCallback(async (path, options = {}) => {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    const resp = await fetch(`${apiBase()}/api${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }, [authHeader]);

  async function install() {
    setInstalling(true);
    setFailed(false);
    setLog('');
    onMessage?.(t('faces.setupStarted'), 'info');
    try {
      await api('/faces/models/install', { method: 'POST' });
      // The recognizer model is ~37 MB and pip may be fetching a wheel, so allow a long window —
      // but poll often enough that the log looks alive.
      const deadline = Date.now() + 20 * 60 * 1000;
      let state = null;
      for (;;) {
        state = await api('/faces/models/install/status');
        setLog(state?.log || '');
        if (state?.status === 'done' || state?.status === 'failed') break;
        if (Date.now() > deadline) { state = { status: 'failed', log: `${state?.log || ''}\nTimed out.` }; break; }
        await new Promise((r) => setTimeout(r, 1500));
      }
      if (state?.status === 'done') {
        onMessage?.(t('faces.setupDone'), 'success');
      } else {
        setFailed(true);
        onMessage?.(t('faces.setupFailed'), 'error');
      }
    } catch (err) {
      setFailed(true);
      setLog((prev) => `${prev}\n${err.message}`);
      onMessage?.(err.message, 'error');
    } finally {
      setInstalling(false);
      onRefresh?.();
    }
  }

  if (status === undefined) {
    return always ? <p className="settings-hint">{t('faces.setupChecking')}</p> : null;
  }
  if (status && status.ready && !always) {
    return null;
  }

  if (status && status.ready) {
    return (
      <div className="faces-setup is-ready">
        <span className="faces-setup-mark ok" aria-hidden="true"><Ico n="check-ok" sz={18} /></span>
        <div className="faces-setup-body">
          <p className="faces-setup-title">{t('faces.setupReady')}</p>
          <p className="settings-hint">{t('faces.setupWhere', { dir: status.dir })}</p>
        </div>
      </div>
    );
  }

  // status === null: the check itself failed (not signed in as an admin, server down). Say that
  // rather than inventing a diagnosis.
  const missing = status ? (status.models || []).filter((m) => !m.present) : [];
  const totalMB = missing.reduce((sum, m) => sum + (m.sizeMb || 0), 0);
  const noPython = !!status && !status.python?.found;

  return (
    <div className={`faces-setup${failed ? ' is-failed' : ''}`}>
      <span className="faces-setup-mark" aria-hidden="true"><Ico n={status ? 'download' : 'warning'} sz={18} /></span>
      <div className="faces-setup-body">
        <p className="faces-setup-title">{status ? t('faces.setupTitle') : t('faces.setupUnknown')}</p>
        {status ? <p className="settings-hint">{t('faces.setupWhy')}</p> : null}

        {status ? (
          <ul className="faces-setup-list">
            {status.needsOpenCV ? <li><Ico n="cpu" sz={13} /> {t('faces.setupNeedOpenCV')}</li> : null}
            {missing.map((m) => (
              <li key={m.file}>
                <Ico n="download" sz={13} />
                {t(m.role === 'detector' ? 'faces.roleDetector' : 'faces.roleRecognizer')}
                <span className="faces-setup-size">{t('faces.setupSize', { mb: m.sizeMb })}</span>
              </li>
            ))}
            {!status.worker ? <li><Ico n="warning" sz={13} /> {t('faces.setupNoWorker')}</li> : null}
          </ul>
        ) : null}

        {noPython ? <FormAlert message={t('faces.setupNoPython')} /> : null}

        <div className="faces-setup-actions">
          {status && !noPython ? (
            <button type="button" onClick={install} disabled={installing}>
              <span className="btn-icon">
                <Ico n="download" sz={14} />
                {installing ? t('faces.setupInstalling') : t('faces.setupInstall', { mb: totalMB || 38 })}
              </span>
            </button>
          ) : null}
          <button type="button" className="quiet" onClick={() => onRefresh?.()} disabled={installing}>
            <span className="btn-icon"><Ico n="reload" sz={14} /> {t('faces.setupCheck')}</span>
          </button>
        </div>

        {status ? <p className="settings-hint">{t('faces.setupWhere', { dir: status.dir })}</p> : null}
        {log ? <pre className="install-output">{log}</pre> : null}
      </div>
    </div>
  );
}
