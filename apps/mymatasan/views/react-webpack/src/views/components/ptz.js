import { useState, useEffect, useCallback } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { apiJson } from '../lib/helpers';

// PTZ presets and guard tours (W3-5).
//
// The PTZ ring is a camera that needs somebody holding a button. This panel is the rest of
// it: the named places a camera can be sent to, the patrol that visits them on its own, and
// the home it goes back to.
//
// It lives beside the ring, in LIVE VIEW, rather than on the Cameras page — because that is
// where the operator who moves cameras already is, and because the role model grants moving
// a camera as a rung of the Live Views page. Putting the tour editor behind Cameras would
// have put it behind a page the operators who run the tours do not hold.

export function loadPresets(authHeader, cameraId) {
  return apiJson(`/api/cameras/${cameraId}/ptz/presets`, { authHeader })
    .then((data) => data?.presets || []);
}

function loadTours(authHeader, cameraId) {
  return apiJson(`/api/cameras/${cameraId}/ptz/tours`, { authHeader })
    .then((data) => data?.tours || []);
}

// PTZPanel is the whole surface for one camera, shown as a dialog over the wall.
export function PTZPanel({ authHeader, cameraId, cameraName, onClose, onMessage }) {
  const t = useT();
  const [presets, setPresets] = useState([]);
  const [tours, setTours] = useState([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [newPreset, setNewPreset] = useState('');
  const [editing, setEditing] = useState(null);

  const refresh = useCallback(async () => {
    try {
      const [rows, tourRows] = await Promise.all([
        loadPresets(authHeader, cameraId),
        loadTours(authHeader, cameraId),
      ]);
      setPresets(rows);
      setTours(tourRows);
      setError('');
    } catch (err) {
      setError(err.message);
    }
  }, [authHeader, cameraId]);

  useEffect(() => { refresh(); }, [refresh]);

  const run = useCallback(async (fn, done) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      await refresh();
      if (done) onMessage?.(done);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [refresh, onMessage]);

  const gotoPreset = (token) => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/presets/${encodeURIComponent(token)}/goto`, { method: 'POST', authHeader }),
  );

  const savePreset = () => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/presets`, { method: 'POST', authHeader, body: { name: newPreset } }),
    t('ptz.presetSaved', { name: newPreset }),
  ).then(() => setNewPreset(''));

  const replacePreset = (preset) => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/presets`, {
      method: 'POST', authHeader, body: { name: preset.name, token: preset.token },
    }),
    t('ptz.presetReplaced', { name: preset.name }),
  );

  const deletePreset = (preset) => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/presets/${encodeURIComponent(preset.token)}/delete`, { method: 'POST', authHeader }),
    t('ptz.presetDeleted', { name: preset.name }),
  );

  const goHome = () => run(() => apiJson(`/api/cameras/${cameraId}/ptz/home`, { method: 'POST', authHeader }));
  const setHome = () => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/home/set`, { method: 'POST', authHeader }),
    t('ptz.homeSaved'),
  );

  const setRunning = (tour, running) => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/tours/${tour.id}/${running ? 'start' : 'stop'}`, { method: 'POST', authHeader }),
    running ? t('ptz.tourStarted', { name: tour.name }) : t('ptz.tourStopped', { name: tour.name }),
  );

  const deleteTour = (tour) => run(
    () => apiJson(`/api/cameras/${cameraId}/ptz/tours/${tour.id}/delete`, { method: 'POST', authHeader }),
    t('ptz.tourDeleted', { name: tour.name }),
  );

  const saveTour = (draft) => run(
    () => apiJson(
      draft.id ? `/api/cameras/${cameraId}/ptz/tours/${draft.id}` : `/api/cameras/${cameraId}/ptz/tours`,
      { method: 'POST', authHeader, body: { name: draft.name, dwellSeconds: draft.dwellSeconds, stops: draft.stops } },
    ),
    t('ptz.tourSaved', { name: draft.name }),
  ).then(() => setEditing(null));

  const anyRunning = tours.some((tour) => tour.isRunning);

  return (
    <div className="modal-backdrop" onClick={busy ? undefined : onClose}>
      <div className="modal-card ptz-panel" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" data-camera-id={cameraId}>
        <h2 className="ptz-panel-title">{t('ptz.title', { name: cameraName })}</h2>
        {error ? <FormAlert message={error} /> : null}

        {/* The trade this feature makes, said where the person making it can read it.
            A patrolling camera cannot be judged re-aimed or covered, because it has no
            normal view to be judged against. Discovering that from the absence of an
            alert is not acceptable for a security feature. */}
        {anyRunning ? (
          <p className="settings-hint ptz-tamper-note">
            <Ico n="warning" sz={14} /> {t('ptz.tamperNote')}
          </p>
        ) : null}

        <section className="ptz-section">
          <h3>{t('ptz.presets')}</h3>
          {presets.length === 0 ? (
            <p className="settings-hint">{t('ptz.noPresets')}</p>
          ) : (
            <ul className="ptz-preset-list">
              {presets.map((preset) => (
                <li key={preset.token} data-preset-token={preset.token}>
                  <button type="button" className="ptz-preset-go" disabled={busy}
                    onClick={() => gotoPreset(preset.token)} title={t('ptz.goHint')}>
                    <Ico n="map-pin" sz={14} /> <span className="ptz-preset-name">{preset.name}</span>
                  </button>
                  <button type="button" className="quiet" disabled={busy} onClick={() => replacePreset(preset)}>
                    {t('ptz.replace')}
                  </button>
                  <button type="button" className="quiet danger-text" disabled={busy} onClick={() => deletePreset(preset)}
                    aria-label={t('ptz.deletePreset', { name: preset.name })}>
                    <Ico n="trash" sz={14} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="ptz-save-row">
            <input
              value={newPreset}
              placeholder={t('ptz.newPresetName')}
              aria-label={t('ptz.newPresetName')}
              disabled={busy}
              onChange={(e) => setNewPreset(e.target.value)}
            />
            <button type="button" disabled={busy || !newPreset.trim()} onClick={savePreset}>
              {t('ptz.savePreset')}
            </button>
          </div>
          {/* Store-where-it-is-now is the only gesture an operator can perform accurately:
              they drive the camera until the picture is right and then say "here". */}
          <p className="settings-hint">{t('ptz.savePresetHint')}</p>
        </section>

        <section className="ptz-section">
          <h3>{t('ptz.home')}</h3>
          <div className="ptz-home-row">
            <button type="button" className="quiet" disabled={busy} onClick={goHome}>
              <span className="btn-icon"><Ico n="home" sz={14} /> {t('ptz.goHome')}</span>
            </button>
            <button type="button" className="quiet" disabled={busy} onClick={setHome}>{t('ptz.setHome')}</button>
          </div>
        </section>

        <section className="ptz-section">
          <h3>{t('ptz.tours')}</h3>
          {tours.length === 0 ? <p className="settings-hint">{t('ptz.noTours')}</p> : null}
          <ul className="ptz-tour-list">
            {tours.map((tour) => (
              <li key={tour.id} data-tour-id={tour.id} className={tour.isRunning ? 'running' : ''}>
                <div className="ptz-tour-head">
                  <strong>{tour.name}</strong>
                  <span className="ptz-tour-meta" data-tour-state={tour.isRunning ? 'running' : 'stopped'}>
                    {tour.isRunning ? t('ptz.running') : t('ptz.stopped')}
                    {' · '}
                    {t('ptz.stopCount', { n: (tour.stopList || []).length, dwell: tour.dwellSeconds })}
                  </span>
                </div>
                {/* A tour whose presets have been deleted from the camera says so. Visiting
                    five of six places quietly tells an operator the route is intact. */}
                {(tour.stopList || []).some((stop) => stop.missing) ? (
                  <p className="ptz-tour-missing"><Ico n="warning" sz={14} /> {t('ptz.tourMissing')}</p>
                ) : null}
                {tour.presetsUnavailable ? (
                  <p className="settings-hint">{t('ptz.presetsUnavailable')}</p>
                ) : null}
                {tour.lastError ? <p className="ptz-tour-missing">{tour.lastError}</p> : null}
                <div className="ptz-tour-actions">
                  <button type="button" disabled={busy} onClick={() => setRunning(tour, !tour.isRunning)}>
                    {tour.isRunning ? t('ptz.stop') : t('ptz.start')}
                  </button>
                  <button type="button" className="quiet" disabled={busy}
                    onClick={() => setEditing({
                      id: tour.id, name: tour.name, dwellSeconds: tour.dwellSeconds,
                      stops: (tour.stopList || []).map((stop) => ({ preset: stop.preset, dwellSeconds: stop.dwellSeconds })),
                    })}>
                    {t('common.edit')}
                  </button>
                  <button type="button" className="quiet danger-text" disabled={busy} onClick={() => deleteTour(tour)}>
                    {t('common.delete')}
                  </button>
                </div>
              </li>
            ))}
          </ul>
          {editing ? (
            <TourEditor
              draft={editing}
              presets={presets}
              busy={busy}
              onChange={setEditing}
              onCancel={() => setEditing(null)}
              onSave={saveTour}
            />
          ) : (
            <button type="button" className="quiet" disabled={busy || presets.length < 2}
              onClick={() => setEditing({ id: 0, name: '', dwellSeconds: 15, stops: [] })}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('ptz.newTour')}</span>
            </button>
          )}
          {/* Two presets is the floor, and saying why beats a disabled button with no
              explanation: one stop is a preset recall, not a patrol. */}
          {presets.length < 2 && !editing ? <p className="settings-hint">{t('ptz.needTwoPresets')}</p> : null}
        </section>

        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onClose}>{t('common.close')}</button>
        </div>
      </div>
    </div>
  );
}

function TourEditor({ draft, presets, busy, onChange, onCancel, onSave }) {
  const t = useT();
  const byToken = new Map(presets.map((preset) => [preset.token, preset]));

  const addStop = (token) => {
    if (!token) return;
    onChange({ ...draft, stops: [...draft.stops, { preset: token, dwellSeconds: 0 }] });
  };
  const removeStop = (index) => {
    onChange({ ...draft, stops: draft.stops.filter((_, i) => i !== index) });
  };
  const moveStop = (index, delta) => {
    const next = [...draft.stops];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    onChange({ ...draft, stops: next });
  };

  return (
    <div className="ptz-tour-editor">
      <div className="ptz-save-row">
        <input value={draft.name} placeholder={t('ptz.tourName')} aria-label={t('ptz.tourName')} disabled={busy}
          onChange={(e) => onChange({ ...draft, name: e.target.value })} />
        <label className="ptz-dwell">
          {t('ptz.dwell')}
          <input type="number" min={5} max={3600} value={draft.dwellSeconds} disabled={busy}
            onChange={(e) => onChange({ ...draft, dwellSeconds: Number(e.target.value) })} />
        </label>
      </div>
      <ol className="ptz-stop-list">
        {draft.stops.map((stop, index) => (
          <li key={`${stop.preset}-${index}`} data-stop-preset={stop.preset}>
            <span>{byToken.get(stop.preset)?.name || stop.preset}</span>
            <button type="button" className="quiet" disabled={busy || index === 0}
              onClick={() => moveStop(index, -1)} aria-label={t('ptz.moveUp')}>↑</button>
            <button type="button" className="quiet" disabled={busy || index === draft.stops.length - 1}
              onClick={() => moveStop(index, 1)} aria-label={t('ptz.moveDown')}>↓</button>
            <button type="button" className="quiet danger-text" disabled={busy}
              onClick={() => removeStop(index)} aria-label={t('ptz.removeStop')}>×</button>
          </li>
        ))}
      </ol>
      <div className="ptz-save-row">
        <select disabled={busy} value="" aria-label={t('ptz.addStop')} onChange={(e) => addStop(e.target.value)}>
          <option value="">{t('ptz.addStop')}</option>
          {presets.map((preset) => (
            <option key={preset.token} value={preset.token}>{preset.name}</option>
          ))}
        </select>
      </div>
      <div className="modal-actions">
        <button type="button" className="quiet" onClick={onCancel} disabled={busy}>{t('common.cancel')}</button>
        <button type="button" disabled={busy || !draft.name.trim() || draft.stops.length < 2}
          onClick={() => onSave(draft)}>{t('common.save')}</button>
      </div>
    </div>
  );
}

// PTZRecallField is the detection rule's "point this camera at it" control.
//
// It reads the camera's presets LIVE rather than offering a free-text token, because a rule
// that names a preset the camera does not have is a rule that fails silently at 3am — the
// alert fires, the camera does not move, and nothing says why. The picker can only offer
// places that exist.
export function PTZRecallField({ authHeader, cameraId, recall, onChange }) {
  const t = useT();
  const [presets, setPresets] = useState([]);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    if (!cameraId) return undefined;
    loadPresets(authHeader, cameraId)
      .then((rows) => { if (!cancelled) { setPresets(rows); setError(''); } })
      .catch((err) => { if (!cancelled) setError(err.message); });
    return () => { cancelled = true; };
  }, [authHeader, cameraId]);

  const selected = recall?.preset || '';
  const hold = recall?.holdSeconds || 0;

  return (
    <section className="schedule-panel ptz-recall">
      <header>
        <h3>{t('ptz.recall')}</h3>
        <span className="status-pill" data-ptz-recall={selected ? 'on' : 'off'}>
          {selected ? t('ptz.recallOn') : t('ptz.recallOff')}
        </span>
      </header>
      {error ? <p className="field-hint">{t('ptz.recallUnavailable')}</p> : null}
      {!error && presets.length === 0 ? <p className="field-hint">{t('ptz.recallNoPresets')}</p> : null}
      {presets.length > 0 ? (
        <div className="metadata-row">
          <label>
            {t('ptz.recallPreset')}
            <select
              value={selected}
              onChange={(e) => onChange(e.target.value ? { cameraId: 0, preset: e.target.value, holdSeconds: hold } : null)}
            >
              <option value="">{t('ptz.recallNone')}</option>
              {presets.map((preset) => (
                <option key={preset.token} value={preset.token}>{preset.name}</option>
              ))}
            </select>
          </label>
          {selected ? (
            <label>
              {t('ptz.recallHold')}
              <input
                type="number" min={0} max={3600} value={hold}
                onChange={(e) => onChange({ cameraId: 0, preset: selected, holdSeconds: Number(e.target.value) })}
              />
            </label>
          ) : null}
        </div>
      ) : null}
      {/* Said plainly because it is the behaviour people are most surprised by: an alarm
          does not take a camera away from the person driving it. */}
      {selected ? <p className="field-hint">{t('ptz.recallHint')}</p> : null}
    </section>
  );
}

// --- relay outputs (W3-5b) -----------------------------------------------------------
//
// A SEPARATE PANEL FROM THE PTZ ONE, and not a section inside it, because they are separate
// capabilities: the role model grants pointing a camera and switching the building's outputs
// on different rungs, and a camera can have relays without having PTZ at all. One dialog
// would have meant one button, behind whichever grant happened to gate it.

export function loadRelays(authHeader, cameraId) {
  return apiJson(`/api/cameras/${cameraId}/relays`, { authHeader })
    .then((data) => data?.relays || []);
}

export function RelayPanel({ authHeader, cameraId, cameraName, onClose, onMessage }) {
  const t = useT();
  const [relays, setRelays] = useState([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [pulse, setPulse] = useState(5);

  const refresh = useCallback(async () => {
    try {
      setRelays(await loadRelays(authHeader, cameraId));
      setError('');
    } catch (err) {
      setError(err.message);
    } finally {
      setLoaded(true);
    }
  }, [authHeader, cameraId]);

  useEffect(() => { refresh(); }, [refresh]);

  const fire = useCallback(async (relay, action) => {
    setBusy(true);
    setError('');
    try {
      await apiJson(`/api/cameras/${cameraId}/relays/${encodeURIComponent(relay.token)}/fire`, {
        method: 'POST', authHeader,
        body: { action, pulseSeconds: Number(pulse) || 0, reason: t('relay.operatorReason') },
      });
      onMessage?.(t('relay.switched', { token: relay.token }));
      await refresh();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }, [authHeader, cameraId, pulse, refresh, onMessage, t]);

  return (
    <div className="modal-backdrop" onClick={busy ? undefined : onClose}>
      <div className="modal-card ptz-panel relay-panel" onClick={(e) => e.stopPropagation()}
        role="dialog" aria-modal="true" data-camera-id={cameraId}>
        <h2 className="ptz-panel-title">{t('relay.title', { name: cameraName })}</h2>
        {error ? <FormAlert message={error} /> : null}

        {loaded && relays.length === 0 && !error ? (
          <p className="settings-hint">{t('relay.none')}</p>
        ) : null}

        <ul className="relay-list">
          {relays.map((relay) => (
            <li key={relay.token} data-relay-token={relay.token} className={relay.heldByUs ? 'held' : ''}>
              <div className="relay-head">
                <strong>{relay.token}</strong>
                <span className="relay-meta" data-relay-mode={relay.bistable ? 'bistable' : 'monostable'}>
                  {relay.bistable ? t('relay.stays') : t('relay.selfReleases', { n: relay.delaySeconds || 0 })}
                </span>
              </div>
              {/* The one state where a restart of the appliance leaves the output
                  energised. Said out loud, because nothing else would say it. */}
              {relay.heldByUs ? (
                <p className="relay-held"><Ico n="warning" sz={14} /> {t('relay.heldByUs')}</p>
              ) : null}
              <div className="relay-actions">
                <button type="button" disabled={busy} onClick={() => fire(relay, 'pulse')}>
                  {t('relay.pulse')}
                </button>
                {relay.bistable ? (
                  <button type="button" className="quiet" disabled={busy} onClick={() => fire(relay, 'on')}>
                    {t('relay.on')}
                  </button>
                ) : null}
                {/* OFF is never disabled, not even while another request is in flight.
                    A button that stops a siren must not be greyed out by the appliance's
                    own busy state — which is exactly what it would be during the request
                    that started the siren. */}
                <button type="button" className="quiet danger-text" onClick={() => fire(relay, 'off')}>
                  {t('relay.off')}
                </button>
              </div>
            </li>
          ))}
        </ul>

        {relays.length > 0 ? (
          <label className="ptz-dwell relay-pulse">
            {t('relay.pulseSeconds')}
            <input type="number" min={1} max={300} value={pulse} disabled={busy}
              onChange={(e) => setPulse(Number(e.target.value))} />
          </label>
        ) : null}

        <p className="settings-hint">{t('relay.auditNote')}</p>

        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onClose}>{t('common.close')}</button>
        </div>
      </div>
    </div>
  );
}

// RelayRuleField is the detection rule's "switch something on when this fires" control.
//
// It offers only a PULSE, with no latch option, because the service refuses a latching rule
// anyway — a rule that could leave a siren sounding is, by construction, the one firing at
// 4am with nobody watching. Offering a control the server will refuse is worse than not
// offering it.
export function RelayRuleField({ authHeader, cameraId, relay, onChange }) {
  const t = useT();
  const [relays, setRelays] = useState([]);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    if (!cameraId) return undefined;
    loadRelays(authHeader, cameraId)
      .then((rows) => { if (!cancelled) { setRelays(rows); setError(''); } })
      .catch((err) => { if (!cancelled) setError(err.message); });
    return () => { cancelled = true; };
  }, [authHeader, cameraId]);

  if (error || relays.length === 0) {
    return null;
  }
  const selected = relay?.token || '';
  const seconds = relay?.pulseSeconds || 0;

  return (
    <section className="schedule-panel relay-rule">
      <header>
        <h3>{t('relay.ruleTitle')}</h3>
        <span className="status-pill" data-relay-rule={selected ? 'on' : 'off'}>
          {selected ? t('relay.ruleOn') : t('relay.ruleOff')}
        </span>
      </header>
      <div className="metadata-row">
        <label>
          {t('relay.output')}
          <select value={selected}
            onChange={(e) => onChange(e.target.value ? { cameraId: 0, token: e.target.value, pulseSeconds: seconds } : null)}>
            <option value="">{t('relay.ruleNone')}</option>
            {relays.map((r) => (<option key={r.token} value={r.token}>{r.token}</option>))}
          </select>
        </label>
        {selected ? (
          <label>
            {t('relay.pulseSeconds')}
            <input type="number" min={1} max={300} value={seconds || 5}
              onChange={(e) => onChange({ cameraId: 0, token: selected, pulseSeconds: Number(e.target.value) })} />
          </label>
        ) : null}
      </div>
      {selected ? <p className="field-hint">{t('relay.ruleHint')}</p> : null}
    </section>
  );
}
