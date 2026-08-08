import { useCallback, useEffect, useState } from 'react';
import { useT, Ico } from '@shared';
import * as api from './lib/access';

// mypintusan's access-settings screen, ported into the control-plane embed: timezone, timers and
// the reader-cable layout, editable from the control plane without a site visit. Two deliberate
// differences from the node's own page:
//
//   - No fleet-pairing panel. Unpairing a node from INSIDE the tunnel that pairing provides
//     would saw off the branch being sat on; joining/leaving the fleet is a decision taken at
//     the node (or by releasing it from the Nodes screen), not from within the embed.
//   - Security keys still never cross the wire: the node redacts SCBKs and carries stored keys
//     forward on save, so this page cannot leak a site key and cannot destroy one.
export function SettingsPage({ onToast }) {
  const t = useT();
  const [form, setForm] = useState(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const cfg = await api.getSettings();
      setForm(cfg);
      setError('');
    } catch (e) {
      setError(e && e.message ? e.message : t('ndoor.error'));
    }
  }, [t]);

  useEffect(() => { load(); }, [load]);

  const save = async (e) => {
    e.preventDefault();
    setSaving(true);
    try {
      const saved = await api.saveSettings(form);
      setForm(saved);
      onToast(t('ndoor.settings.saved'), 'success');
    } catch (err) {
      // The node's validation messages are written for this reader — "two readers at address 1"
      // — so they are shown verbatim rather than replaced with a generic failure.
      onToast(err && err.message ? err.message : t('ndoor.settings.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    if (!window.confirm(t('ndoor.settings.resetConfirm'))) return;
    try {
      setForm(await api.resetSettings());
      onToast(t('ndoor.settings.saved'), 'success');
    } catch (err) {
      onToast(err && err.message ? err.message : t('ndoor.error'), 'error');
    }
  };

  if (error) return <div className="screen"><div className="notice notice-error"><p>{error}</p></div></div>;
  if (!form) return <div className="screen"><p>{t('ndoor.loading')}</p></div>;

  const setBus = (i, patch) => {
    const buses = form.buses.map((b, idx) => (idx === i ? { ...b, ...patch } : b));
    setForm({ ...form, buses });
  };
  const setReader = (bi, ri, patch) => {
    const buses = form.buses.map((b, idx) => (idx !== bi ? b : {
      ...b, readers: b.readers.map((r, j) => (j === ri ? { ...r, ...patch } : r)),
    }));
    setForm({ ...form, buses });
  };

  return (
    <div className="screen">
      <header className="screen-head">
        <div>
          <h1>{t('ndoor.settings.title')}</h1>
          <p className="muted">{t('ndoor.settings.subtitle')}</p>
        </div>
        <button type="button" className="btn btn-quiet" onClick={reset}>{t('ndoor.settings.reset')}</button>
      </header>

      <form onSubmit={save} className="form">
        <label>
          <span>{t('ndoor.settings.timezone')}</span>
          <input value={form.timezone || ''} onChange={(e) => setForm({ ...form, timezone: e.target.value })} />
          <small className="muted">{t('ndoor.settings.timezoneHint')}</small>
        </label>

        <div className="form-row">
          <label>
            <span>{t('ndoor.settings.tick')}</span>
            <input
              type="number" min="1" max="60"
              value={form.tickSeconds || 1}
              onChange={(e) => setForm({ ...form, tickSeconds: Number(e.target.value) })}
            />
            <small className="muted">{t('ndoor.settings.tickHint')}</small>
          </label>
          <label>
            <span>{t('ndoor.settings.pinWindow')}</span>
            <input
              type="number" min="1"
              value={form.pinWindowSeconds || 15}
              onChange={(e) => setForm({ ...form, pinWindowSeconds: Number(e.target.value) })}
            />
            <small className="muted">{t('ndoor.settings.pinWindowHint')}</small>
          </label>
        </div>

        <h2 className="section-head">{t('ndoor.settings.buses')}</h2>
        <p className="muted small">{t('ndoor.settings.restartNote')}</p>

        {(form.buses || []).map((bus, bi) => (
          <fieldset className="bus-box" key={bi}>
            <legend>
              <input
                className="bus-port"
                value={bus.port || ''}
                placeholder="tcp://host:port"
                onChange={(e) => setBus(bi, { port: e.target.value })}
              />
            </legend>

            {(bus.readers || []).map((rd, ri) => (
              <div className="reader-row" key={ri}>
                <label>
                  <span>{t('ndoor.settings.readerLabel')}</span>
                  <input value={rd.label || ''} onChange={(e) => setReader(bi, ri, { label: e.target.value })} />
                </label>
                <label>
                  <span>{t('ndoor.settings.readerAddress')}</span>
                  <input
                    type="number" min="0" max="127"
                    value={rd.address ?? 0}
                    onChange={(e) => setReader(bi, ri, { address: Number(e.target.value) })}
                  />
                </label>
                <label className="check">
                  <input
                    type="checkbox"
                    checked={!!rd.requireSecureChannel}
                    onChange={(e) => setReader(bi, ri, { requireSecureChannel: e.target.checked })}
                  />
                  <span>{t('ndoor.settings.requireEncryption')}</span>
                </label>
                <span className="key-state">
                  {rd.usingDefaultKey ? (
                    <span className="pill pill-danger" title={t('ndoor.readers.defaultKeyWarn')}>
                      <Ico n="shield" sz={12} /> {t('ndoor.readers.defaultKey')}
                    </span>
                  ) : rd.hasScbk ? (
                    <span className="pill pill-ok">{t('ndoor.settings.keyInstalled')}</span>
                  ) : (
                    <span className="pill pill-warn">{t('ndoor.settings.noKey')}</span>
                  )}
                </span>
              </div>
            ))}

            {/* The address-0 collision is the commonest onboarding mistake — readers ship set to 0,
                so a second one silently never answers. Said here, before it happens. */}
            <p className="muted small">{t('ndoor.settings.addressHint')}</p>

            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => setBus(bi, {
                readers: [...(bus.readers || []), { address: 0, requireSecureChannel: false, label: '' }],
              })}
            >
              <Ico n="plus" sz={14} /> {t('ndoor.settings.addReader')}
            </button>
          </fieldset>
        ))}

        <button
          type="button"
          className="btn btn-quiet"
          onClick={() => setForm({
            ...form,
            buses: [...(form.buses || []), { port: '', slotMillis: 50, replyTimeoutMillis: 200, readers: [] }],
          })}
        >
          <Ico n="plus" sz={14} /> {t('ndoor.settings.addBus')}
        </button>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={saving}>{t('common.save')}</button>
        </div>
      </form>
    </div>
  );
}
