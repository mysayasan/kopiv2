import { useState, useEffect, useCallback } from 'react';
import { Ico } from './icons';
import { FormAlert } from './ui';
import { useT } from '@shared/i18n';
import { apiBase, formatTimestamp } from '../lib/helpers';

// Appearance search: "find more like this" over recorded sightings.
//
// THE HONESTY OF THIS SCREEN IS PART OF THE FEATURE. The ranking comes from a
// general-purpose image embedding, not a person re-identification network: it separates
// coarse appearance well and is markedly weaker at matching the same individual across big
// changes in pose or lighting. So the screen presents a RANKED SHORTLIST TO CONFIRM, shows
// the similarity of every row rather than hiding it behind a verdict, and says in plain
// words that a match is not an identification. A screen that instead said "found: 4
// matches" would be making a claim the model cannot support, and an operator would act on
// it.

// The band comes from how far a sighting STANDS OUT from the others compared, not from the
// raw similarity — and the difference is not cosmetic.
//
// Measured on the real model: two crops of the same subject score 0.9825, and a red figure
// against a blue one scores 0.9498. Every person looks ~95% like every other person to an
// ImageNet embedding, so a percentage would read as a near-certain match for every row on
// the screen. What separates a real match is standing clear of the crowd, in robust
// deviations above the median of everything the node compared.
function standoutBand(hit, calibrated) {
  if (!calibrated) return 'uncalibrated';
  const s = Number(hit?.standout) || 0;
  if (s >= 6) return 'strong';
  if (s >= 3.5) return 'moderate';
  return 'weak';
}

const WINDOW_CHOICES = [
  { id: 3600, key: 'ap.win1h' },
  { id: 6 * 3600, key: 'ap.win6h' },
  { id: 24 * 3600, key: 'ap.win24h' },
  { id: 7 * 86400, key: 'ap.win7d' },
];

function thumbUrl(segmentId, seek, boxStr, label) {
  const params = new URLSearchParams({ seek: String(Math.max(0, Number(seek) || 0)), w: '220' });
  if (boxStr) {
    params.set('box', boxStr);
    if (label) params.set('label', label);
  }
  return `${apiBase()}/api/recording/segments/${segmentId}/frame?${params}`;
}

// HitThumb renders one ranked sighting's frame. It fetches through the API (rather than
// setting src directly) so an authorization failure is visible as a placeholder instead of
// a browser-native broken image.
function HitThumb({ segmentId, seek, authHeader, alt }) {
  const [url, setUrl] = useState(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!Number(segmentId)) return undefined;
    let cancelled = false;
    let obj = null;
    (async () => {
      try {
        const headers = authHeader ? { Authorization: authHeader } : {};
        const r = await fetch(thumbUrl(segmentId, seek), { credentials: 'include', headers });
        if (!r.ok) throw new Error(String(r.status));
        const blob = await r.blob();
        if (cancelled) return;
        obj = URL.createObjectURL(blob);
        setUrl(obj);
      } catch (_) {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => { cancelled = true; if (obj) URL.revokeObjectURL(obj); };
  }, [segmentId, seek, authHeader]);
  if (failed || !Number(segmentId)) {
    return <span className="ap-thumb ap-thumb--none" aria-hidden="true"><Ico n="film" sz={18} /></span>;
  }
  return url
    ? <img className="ap-thumb" src={url} alt={alt || ''} />
    : <span className="ap-thumb ap-thumb--load" aria-hidden="true" />;
}

// AppearanceSearchDialog ranks recorded sightings against the one the operator picked.
export function AppearanceSearchDialog({ observation, authHeader, cameraName, onClose, onPlay }) {
  const t = useT();
  const [windowSeconds, setWindowSeconds] = useState(6 * 3600);
  const [minStandout, setMinStandout] = useState(2);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const obsId = Number(observation?.id) || 0;
  const seenAt = Number(observation?.peakAt) || Number(observation?.startedAt) || 0;

  const run = useCallback(async () => {
    if (!obsId) return;
    setLoading(true);
    setError('');
    try {
      const headers = authHeader ? { Authorization: authHeader } : {};
      // The window is centred on the sighting, not trailing from now: an investigator
      // asks "where else around this time", and both directions matter — where they came
      // from is as much of the answer as where they went.
      const from = Math.max(0, seenAt - windowSeconds);
      const to = seenAt + windowSeconds;
      const url = `${apiBase()}/api/observations/appearance?observationId=${obsId}`
        + `&from=${from}&to=${to}&minStandout=${minStandout}&limit=50`;
      const r = await fetch(url, { credentials: 'include', headers });
      const payload = await r.json().catch(() => null);
      const body = payload?.data?.result ?? payload?.result ?? payload;
      if (!r.ok) {
        // The server's own words. "Appearance search was not enabled on this camera when
        // it was recorded" is a completely different fact from "nothing matched", and an
        // operator shown the latter would conclude the person appeared nowhere else.
        setError(payload?.message || payload?.data?.message || t('ap.failed'));
        setResult(null);
        return;
      }
      setResult(body);
    } catch (_) {
      setError(t('ap.failed'));
      setResult(null);
    } finally {
      setLoading(false);
    }
  }, [obsId, seenAt, windowSeconds, minStandout, authHeader, t]);

  useEffect(() => { run(); }, [run]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose?.(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const hits = result?.hits || [];

  return (
    <div className="video-overlay" onClick={onClose}>
      <div className="video-dialog ap-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t('ap.title')}>
        <div className="video-dialog-header">
          <div className="video-dialog-title-group">
            <strong>{t('ap.title')}</strong>
            <span className="ap-subject">
              {t('ap.subject', {
                label: observation?.label || '',
                camera: cameraName ? cameraName(observation?.cameraId) : observation?.cameraId,
                time: formatTimestamp(seenAt),
              })}
            </span>
          </div>
          <button type="button" className="quiet" onClick={onClose} aria-label={t('common.close')}>
            <Ico n="x" />
          </button>
        </div>

        {/* Stated before any result is read, not as a footnote after it. The ranking is
            evidence to check, not an identification, and the moment a list of faces
            appears under a heading an operator stops reading disclaimers. */}
        <p className="ap-caveat">
          <Ico n="info" sz={15} />
          <span>{t('ap.caveat')}</span>
        </p>

        <div className="ap-controls">
          <label>
            <span>{t('ap.window')}</span>
            <select value={windowSeconds} onChange={(e) => setWindowSeconds(Number(e.target.value))} disabled={loading}>
              {WINDOW_CHOICES.map((w) => (
                <option key={w.id} value={w.id}>{t(w.key)}</option>
              ))}
            </select>
          </label>
          <label>
            <span>{t('ap.minSimilarity')}</span>
            <select value={minStandout} onChange={(e) => setMinStandout(Number(e.target.value))} disabled={loading}>
              <option value={1}>{t('ap.simLoose')}</option>
              <option value={2}>{t('ap.simBalanced')}</option>
              <option value={4}>{t('ap.simStrict')}</option>
            </select>
          </label>
          <button type="button" onClick={run} disabled={loading}>
            <span className="btn-icon"><Ico n="search" /> {loading ? t('common.loading') : t('ap.rerun')}</span>
          </button>
        </div>

        {error ? <FormAlert message={error} /> : null}

        {!error && result ? (
          <>
            {/* "No matches out of 3" and "no matches out of 40,000" are different answers
                and a bare empty list tells them apart for nobody. */}
            <p className="ap-scanned">
              {hits.length > 0
                ? t('ap.foundOf', { n: hits.length, scanned: result.scanned })
                : t('ap.noneOf', { scanned: result.scanned })}
            </p>
            {/* An uncalibrated ranking is still worth showing, but it must not be dressed
                up as a statistical finding. "Stands out from the crowd" describes nothing
                when there was barely a crowd. */}
            {result.scanned > 0 && !result.calibrated ? (
              <p className="ap-uncalibrated">
                <Ico n="info" sz={14} /> <span>{t('ap.uncalibrated', { scanned: result.scanned })}</span>
              </p>
            ) : null}
            <ul className="ap-hits">
              {hits.map((h) => (
                <li key={`${h.observationId}`} className={`ap-hit is-${standoutBand(h, result.calibrated)}`}>
                  <button
                    type="button"
                    className="ap-hit-open"
                    onClick={() => onPlay?.(h)}
                    disabled={!Number(h.segmentId)}
                    title={Number(h.segmentId) ? t('ap.openFootage') : t('ap.footageGone')}
                  >
                    <HitThumb segmentId={h.segmentId} seek={h.seek} authHeader={authHeader} alt="" />
                  </button>
                  <div className="ap-hit-meta">
                    <strong>{cameraName ? cameraName(h.cameraId) : h.cameraId}</strong>
                    <span>{formatTimestamp(h.seenAt)}</span>
                    {!Number(h.segmentId) ? (
                      <em className="ap-hit-gone">{t('ap.footageGone')}</em>
                    ) : null}
                  </div>
                  <div className="ap-hit-score">
                    <span className={`ap-band ap-band--${standoutBand(h, result.calibrated)}`}>
                      {t(`ap.band.${standoutBand(h, result.calibrated)}`)}
                    </span>
                    {/* Deliberately NOT a percentage. The raw cosine is shown only as a
                        secondary figure, in a tooltip, because on this feature space it
                        looks like a confidence and is not one. */}
                    <span
                      className="ap-score"
                      title={t('ap.rawSimilarity', { value: h.similarity.toFixed(4) })}
                    >
                      {result.calibrated ? t('ap.standoutValue', { n: h.standout.toFixed(1) }) : '—'}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </div>
    </div>
  );
}
