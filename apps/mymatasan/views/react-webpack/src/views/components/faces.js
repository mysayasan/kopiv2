import { useCallback, useEffect, useRef, useState } from 'react';
import { apiBase } from '../lib/helpers';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';

// FacesTab is the face-recognition enrollment surface: a GLOBAL roster of people the system should
// recognize, plus a per-camera switch to turn recognition on. Enrolling a person is instant (no
// training) — a photo becomes a faceprint stored in the encrypted gallery, and any camera with face
// recognition enabled then names them when they appear.
//
// FACE TEMPLATES ARE BIOMETRIC DATA. The feature is off until someone is enrolled AND a camera is
// switched on, and the page leads with a consent notice — enroll only people who have agreed.

const CONSENT_KEY = 'mymatasan_face_consent';

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

export function FacesTab({ authHeader, cameras = [], onMessage }) {
  const t = useT();
  const [people, setPeople] = useState([]);
  const [rules, setRules] = useState([]);
  const [busy, setBusy] = useState(true);
  const [newName, setNewName] = useState('');
  const [consented, setConsented] = useState(() => localStorage.getItem(CONSENT_KEY) === '1');
  const [expanded, setExpanded] = useState(0);
  const fileRef = useRef(null);

  const api = useCallback(async (path, options = {}) => {
    const headers = { ...(options.headers || {}) };
    if (authHeader) headers.Authorization = authHeader;
    if (options.body) headers['Content-Type'] = 'application/json';
    const resp = await fetch(`${apiBase()}${path}`, { credentials: 'include', ...options, headers });
    const text = await resp.text();
    let payload = null;
    if (text) { try { payload = JSON.parse(text); } catch (_) { payload = { message: text }; } }
    if (!resp.ok) throw new Error(payload?.message || payload?.data?.message || `Request failed (${resp.status})`);
    return payload?.data?.result ?? payload?.result ?? payload;
  }, [authHeader]);

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

  async function addPerson(e) {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    try {
      const p = await api('/faces', { method: 'POST', body: JSON.stringify({ name }) });
      setNewName('');
      setExpanded(p?.id || 0);
      onMessage?.(t('faces.added', { name }), 'success');
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function removePerson(id, name) {
    if (!window.confirm(t('faces.deleteConfirm', { name }))) return;
    try { await api(`/faces/${id}`, { method: 'DELETE' }); onMessage?.(t('faces.deleted', { name }), 'success'); load(); }
    catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function togglePerson(p) {
    try {
      await api(`/faces/${p.id}`, { method: 'PUT', body: JSON.stringify({ name: p.name, notes: p.notes || '', enabled: !p.enabled }) });
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  async function enrollFiles(personId, files) {
    let ok = 0; let fail = 0;
    for (const file of files) {
      try {
        const image = await fileToBase64(file);
        await api(`/faces/${personId}/enroll`, { method: 'POST', body: JSON.stringify({ image, source: 'upload' }) });
        ok += 1;
      } catch (err) { fail += 1; onMessage?.(t('faces.enrollFileError', { file: file.name, error: err.message }), 'error'); }
    }
    if (ok) onMessage?.(t('faces.enrolled', { n: ok }), 'success');
    load();
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
            ruleConfig: JSON.stringify({ matchMode: 'known' }), isEnabled: true, minFrames: 1, cooldownSeconds: 60,
          }),
        });
      }
      load();
    } catch (err) { onMessage?.(err.message, 'error'); }
  }

  if (!consented) {
    return (
      <div className="page faces-page">
        <div className="faces-consent">
          {/* The consent gate is precisely where somebody should be able to read what they are
              agreeing to, so the help link lives here as well as on the roster behind it. */}
          <h2>
            {t('faces.consentTitle')}
            <HelpButton slug="people" anchor="consent" />
          </h2>
          <p>{withBold(t('faces.consentP1'))}</p>
          <p>{t('faces.consentP2')}</p>
          <button type="button" onClick={() => { localStorage.setItem(CONSENT_KEY, '1'); setConsented(true); }}>
            {t('faces.consentAccept')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="page faces-page">
      <div className="page-head">
        <h1>
          {t('faces.title')}
          {/* Points at the consent section, not the top of the article: enrolling somebody
              stores biometric data, and that is the part an operator needs to have read. */}
          <HelpButton slug="people" anchor="consent" />
        </h1>
        <p className="page-sub">{t('faces.subtitle')}</p>
      </div>

      <form className="faces-add" onSubmit={addPerson}>
        <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder={t('faces.namePlaceholder')} />
        <button type="submit">{t('faces.addPerson')}</button>
      </form>

      {busy ? <p className="settings-hint">{t('faces.loading')}</p> : null}

      <ul className="faces-list">
        {people.map((p) => (
          <li key={p.id} className={`faces-card${expanded === p.id ? ' open' : ''}`}>
            <div className="faces-card-head">
              {p.thumbnail
                ? <img className="faces-thumb" alt={p.name} src={`data:image/jpeg;base64,${p.thumbnail}`} />
                : <div className="faces-thumb placeholder">{(p.name || '?').slice(0, 1).toUpperCase()}</div>}
              <button type="button" className="faces-name" onClick={() => setExpanded(expanded === p.id ? 0 : p.id)}>
                {p.name}
              </button>
              <label className="faces-enabled">
                <input type="checkbox" checked={!!p.enabled} onChange={() => togglePerson(p)} /> {t('faces.enabled')}
              </label>
              <button type="button" className="quiet danger-text" onClick={() => removePerson(p.id, p.name)}>{t('faces.delete')}</button>
            </div>
            {expanded === p.id ? (
              <div className="faces-card-body">
                <p className="settings-hint">{t('faces.photoHint')}</p>
                <input
                  ref={fileRef}
                  type="file"
                  accept="image/*"
                  multiple
                  onChange={(e) => { if (e.target.files?.length) enrollFiles(p.id, Array.from(e.target.files)); e.target.value = ''; }}
                />
              </div>
            ) : null}
          </li>
        ))}
        {!busy && people.length === 0 ? <li className="settings-hint">{t('faces.emptyList')}</li> : null}
      </ul>

      <div className="faces-cameras">
        <h2>{t('faces.recognizeOn')}</h2>
        <p className="settings-hint">{t('faces.recognizeHint')}</p>
        <ul>
          {cameras.map((c) => (
            <li key={c.id}>
              <label>
                <input type="checkbox" checked={rules.some((r) => r.cameraId === c.id)} onChange={() => toggleCamera(c)} />
                {c.name || t('notif.cameraN', { id: c.id })}
              </label>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
