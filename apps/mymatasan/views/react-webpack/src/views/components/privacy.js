import { useState, useEffect, useCallback } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { apiJson } from '../lib/helpers';
import { ZoneDrawingPreview } from './previews';

// Privacy zones (W3-6): the parts of a camera's view that must not be seen.
//
// THE SCREEN'S WHOLE JOB IS TO NOT OVERSTATE WHAT IS PROTECTED. One drawn region feeds two
// mechanisms that guarantee different things — the camera burning it in (the pixels are
// never recorded) and this recorder redacting exports (the pixels are recorded and do not
// leave the building) — and which one an operator has depends on their hardware. A screen
// that showed a list of zones and said nothing else would imply the stronger claim on every
// camera, including the ones that cannot do it. So the status is not a detail panel: it is
// the first thing on the page, in the server's own words.

// The zone editor is the SAME component the detection rules use, on purpose: privacy zones
// and detection zones are drawn on the same picture with the same gestures, and a second
// editor would mean a region that looks right on one screen and is wrong on the other.

// statusSentence turns the machine-readable status into the operator's own language.
//
// The four states say genuinely different things, and the difference between them is the
// feature: "not recorded at all" is a promise about the recording, "only the exports are
// redacted" is a promise about disclosure, and they must never be worded so alike that
// somebody reads one as the other.
function statusSentence(t, status) {
  const zones = (status.unconfirmedZones || []).join(', ');
  switch (status.masking) {
    case 'confirmed':
      return status.hasZones ? t('privacy.status.confirmed') : t('privacy.status.ready');
    case 'unconfirmed':
      return t('privacy.status.unconfirmed', { zones });
    case 'unsupported':
      return t('privacy.status.unsupported');
    default:
      return t('privacy.status.unreachable');
  }
}

export function PrivacyPanel({ camera, authHeader, streamConfig, busy, onMessage }) {
  const t = useT();
  const cameraId = Number(camera?.id) || 0;
  const [zones, setZones] = useState([]);
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');
  const [working, setWorking] = useState(false);
  const [draft, setDraft] = useState(null);

  const refresh = useCallback(async () => {
    if (!cameraId) return;
    try {
      const data = await apiJson(`/api/cameras/${cameraId}/privacy`, { authHeader });
      setZones(data?.zones || []);
      setStatus(data?.status || null);
      setError('');
    } catch (err) {
      setError(err.message);
    }
  }, [authHeader, cameraId]);

  useEffect(() => { refresh(); }, [refresh]);

  const run = useCallback(async (fn, done) => {
    setWorking(true);
    setError('');
    try {
      await fn();
      await refresh();
      if (done) onMessage?.(done);
    } catch (err) {
      setError(err.message);
    } finally {
      setWorking(false);
    }
  }, [refresh, onMessage]);

  const save = (zone) => run(
    () => apiJson(
      zone.id ? `/api/cameras/${cameraId}/privacy/${zone.id}` : `/api/cameras/${cameraId}/privacy`,
      {
        method: 'POST', authHeader,
        body: { name: zone.name, points: zone.points, style: zone.style, enabled: zone.enabled },
      },
    ),
    t('privacy.saved', { name: zone.name }),
  ).then(() => setDraft(null));

  const remove = (zone) => run(
    () => apiJson(`/api/cameras/${cameraId}/privacy/${zone.id}/delete`, { method: 'POST', authHeader }),
    t('privacy.removed', { name: zone.name }),
  );

  const recheck = () => run(
    () => apiJson(`/api/cameras/${cameraId}/privacy/apply`, { method: 'POST', authHeader }),
    t('privacy.rechecked'),
  );

  const disabled = busy || working;

  return (
    <section className="camera-settings-panel privacy-panel" data-camera-id={cameraId}>
      <div className="toolbar">
        <div>
          <h2 className="section-title">{t('privacy.title')}</h2>
          <p className="settings-hint">{t('privacy.intro')}</p>
        </div>
        <button type="button" className="quiet" disabled={disabled} onClick={recheck}>
          <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('privacy.recheck')}</span>
        </button>
      </div>

      {error ? <FormAlert message={error} /> : null}

      {/* NOT a footnote. Which of the two protections this camera actually gives is the
          single most important thing on the page, so it is the first thing on it, it
          carries the server's own sentence, and it is machine-readable for the screen
          check that has to prove it is being said. */}
      {status ? (
        <p className={`privacy-status privacy-status-${status.masking}`} data-masking={status.masking}>
          <Ico n={status.masking === 'confirmed' ? 'shield' : 'warning'} sz={14} />
          {/* Composed HERE, from the state and the zone names, rather than printing the
              server's `detail`. That field is English — it is for the API and the log —
              and rendering it put the most important sentence on this page in English in
              every non-English installation. An Arabic screen pass caught it; it is the
              same defect W3-4 shipped with its rule-schedule summaries. */}
          <span>{statusSentence(t, status)}</span>
        </p>
      ) : null}

      <ul className="privacy-zone-list">
        {zones.length === 0 ? <li className="settings-hint">{t('privacy.none')}</li> : null}
        {zones.map((zone) => (
          <li key={zone.id} data-zone-id={zone.id} className={zone.enabled ? '' : 'is-off'}>
            <div className="privacy-zone-head">
              <strong>{zone.name}</strong>
              <span className="privacy-zone-meta" data-zone-state={zone.enabled ? 'on' : 'off'}>
                {zone.enabled ? t('privacy.active') : t('privacy.switchedOff')}
                {' · '}
                {t('privacy.style.' + (zone.style || 'color'))}
                {/* Whether the CAMERA took this particular zone, which is not the same
                    question as whether the camera can mask at all. */}
                {zone.maskToken ? ` · ${t('privacy.onCamera')}` : ` · ${t('privacy.exportOnly')}`}
              </span>
            </div>
            <div className="privacy-zone-actions">
              <button type="button" className="quiet" disabled={disabled}
                onClick={() => save({ ...zone, points: zone.points, enabled: !zone.enabled })}>
                {zone.enabled ? t('privacy.turnOff') : t('privacy.turnOn')}
              </button>
              <button type="button" className="quiet" disabled={disabled}
                onClick={() => setDraft({
                  id: zone.id, name: zone.name, style: zone.style || 'color',
                  enabled: zone.enabled, points: zone.points,
                })}>
                {t('common.edit')}
              </button>
              <button type="button" className="quiet danger-text" disabled={disabled}
                onClick={() => remove(zone)}>
                {t('common.delete')}
              </button>
            </div>
          </li>
        ))}
      </ul>

      {draft ? (
        <PrivacyZoneEditor
          camera={camera}
          draft={draft}
          authHeader={authHeader}
          streamConfig={streamConfig}
          disabled={disabled}
          onChange={setDraft}
          onCancel={() => setDraft(null)}
          onSave={save}
        />
      ) : (
        <button type="button" className="quiet" disabled={disabled}
          onClick={() => setDraft({
            id: 0, name: '', style: 'color', enabled: true,
            points: [[0.3, 0.3], [0.7, 0.3], [0.7, 0.7], [0.3, 0.7]],
          })}>
          <span className="btn-icon"><Ico n="plus" sz={14} /> {t('privacy.add')}</span>
        </button>
      )}
    </section>
  );
}

function PrivacyZoneEditor({ camera, draft, authHeader, streamConfig, disabled, onChange, onCancel, onSave }) {
  const t = useT();
  return (
    <div className="privacy-zone-editor">
      <ZoneDrawingPreview
        camera={camera}
        polygonValue={JSON.stringify(draft.points)}
        authHeader={authHeader}
        streamConfig={streamConfig}
        disabled={disabled}
        onPolygon={(value) => {
          try {
            const parsed = JSON.parse(value);
            // The editor can hold several zones; a privacy zone is one region, so the
            // first is taken rather than silently storing a shape the rest of the product
            // cannot represent.
            const points = Array.isArray(parsed[0]) && Array.isArray(parsed[0][0]) ? parsed[0] : parsed;
            onChange({ ...draft, points });
          } catch (_) { /* a half-drawn zone is not an error */ }
        }}
      />
      <div className="privacy-editor-row">
        <label>
          {t('privacy.name')}
          <input value={draft.name} disabled={disabled}
            onChange={(e) => onChange({ ...draft, name: e.target.value })} />
        </label>
        <label>
          {t('privacy.style')}
          <select value={draft.style} disabled={disabled}
            onChange={(e) => onChange({ ...draft, style: e.target.value })}>
            <option value="color">{t('privacy.style.color')}</option>
            <option value="blurred">{t('privacy.style.blurred')}</option>
            <option value="pixelated">{t('privacy.style.pixelated')}</option>
          </select>
        </label>
      </div>
      {/* Said next to the picker, because the choice only applies to one of the two
          protections: an export is always blacked out, whatever is chosen here. */}
      <p className="settings-hint">{t('privacy.styleHint')}</p>
      <div className="modal-actions">
        <button type="button" className="quiet" onClick={onCancel} disabled={disabled}>{t('common.cancel')}</button>
        <button type="button" disabled={disabled || !draft.name.trim() || (draft.points || []).length < 3}
          onClick={() => onSave(draft)}>{t('common.save')}</button>
      </div>
    </div>
  );
}
