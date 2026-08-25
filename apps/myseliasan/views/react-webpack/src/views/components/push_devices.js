import { useCallback, useEffect, useState } from 'react';
import { Ico, useT } from '@shared';
import { HelpButton } from '@shared/Manual';
import { api, formatTimestamp } from '../lib/helpers';
import {
  browserCapability, currentSubscription, enrol, forget,
  PUSH_BLOCKED, PUSH_ENABLED, PUSH_INSECURE, PUSH_READY, PUSH_UNSUPPORTED,
} from '../lib/push';

// "Alerts on this device" — the mobile-push panel on the Notifications page (W3-9).
//
// The screen's whole job is to never let somebody walk away believing they will be woken when
// they will not. So it says THREE separate things, and they are separate because they fail
// separately:
//
//   1. what this BROWSER can do        (a phone with notifications denied)
//   2. what this INSTALL can do        (an intranet appliance with no route to a push service)
//   3. what each DEVICE last did       (the evidence, per row, from a real delivery)
//
// A single "notifications: on" line covering all three is the shape of failure this hardening
// programme keeps finding: a green state that was never checked against anything.

export function PushDevicesPanel({ session, onToast }) {
  const t = useT();
  const [status, setStatus] = useState(null);
  const [devices, setDevices] = useState([]);
  const [browserState, setBrowserState] = useState(PUSH_UNSUPPORTED);
  // WHICH ROW IS THE BROWSER YOU ARE LOOKING AT. It cannot be derived from the list: the
  // endpoint identifies the device and the API deliberately never returns it. So the id is
  // remembered from the enrolment this page performed — the only moment the two are known
  // together. Without it, somebody with a phone and a laptop sees "this device" on both, and
  // Remove becomes a guess about which one they are about to silence.
  const [thisDeviceId, setThisDeviceId] = useState(0);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(async () => {
    const [s, d] = await Promise.all([
      api('/api/push/status', { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/push/devices', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    if (s.ok) setStatus(s.body || null);
    if (d.ok) setDevices(Array.isArray(d.body?.items) ? d.body.items : []);
    setLoaded(true);
    return s.ok ? s.body : null;
  }, []);

  // Read the browser's state and, if this browser is already subscribed, RE-POST the
  // subscription. That re-post is not redundant: browsers rotate subscriptions on their own,
  // and the server upserts by endpoint, so opening the page is what heals a rotation. The old
  // endpoint is swept separately, when the push service answers 410 for it.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const cap = browserCapability();
      if (cancelled) return;
      if (cap !== PUSH_READY) { setBrowserState(cap); await load(); return; }
      const sub = await currentSubscription().catch(() => null);
      if (cancelled) return;
      setBrowserState(sub ? PUSH_ENABLED : PUSH_READY);
      const st = await load();
      if (cancelled || !sub || !st?.publicKey) return;
      // enrol() reuses the existing subscription when its application server key still
      // matches, and hands back the full payload (endpoint AND keys) that the upsert needs.
      // Permission is already granted at this point, so nothing here can prompt.
      const again = await enrol(st.publicKey).catch(() => ({ ok: false }));
      if (cancelled || !again.ok) return;
      const posted = await api('/api/push/devices', {
        method: 'POST',
        body: JSON.stringify({ ...again.payload, label: deviceLabel(), minSeverity: '' }),
        noRedirect: true,
      }).catch(() => ({ ok: false }));
      if (cancelled) return;
      if (posted.ok && posted.body?.id) setThisDeviceId(posted.body.id);
      await load();
    })();
    return () => { cancelled = true; };
  }, [load]);

  const turnOn = useCallback(async () => {
    setBusy(true);
    const st = status || (await load());
    const res = await enrol(st?.publicKey).catch(() => ({ ok: false, reason: 'failed' }));
    if (!res.ok) {
      setBusy(false);
      setBrowserState(res.reason === PUSH_BLOCKED ? PUSH_BLOCKED : browserCapability());
      onToast && onToast(t(`push.refused.${res.reason || 'failed'}`), 'error');
      return;
    }
    const r = await api('/api/push/devices', {
      method: 'POST',
      body: JSON.stringify({ ...res.payload, label: deviceLabel(), minSeverity: '' }),
      noRedirect: true,
    }).catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) {
      onToast && onToast(r.message || t('push.enrolFailed'), 'error');
      return;
    }
    setBrowserState(PUSH_ENABLED);
    if (r.body?.id) setThisDeviceId(r.body.id);
    await load();
    // The toast says what the PROOF found, not that a row was written. "Registered" would be
    // true and useless on an install that cannot reach a push service.
    const outcome = r.body?.lastOutcome || '';
    onToast && onToast(
      outcome === 'delivered' ? t('push.enrolProved') : t('push.enrolUnproved'),
      outcome === 'delivered' ? 'success' : 'error',
    );
  }, [status, load, onToast, t]);

  const testDevice = useCallback(async (id) => {
    setBusy(true);
    const r = await api(`/api/push/devices/${id}/test`, { method: 'POST', noRedirect: true })
      .catch(() => ({ ok: false }));
    setBusy(false);
    if (!r.ok) { onToast && onToast(r.message || t('push.testFailed'), 'error'); return; }
    await load();
    const outcome = r.body?.lastOutcome || '';
    onToast && onToast(
      outcome === 'delivered' ? t('push.testAccepted') : t(`push.outcome.${outcome || 'unreachable'}`),
      outcome === 'delivered' ? 'success' : 'error',
    );
  }, [load, onToast, t]);

  const removeDevice = useCallback(async (device) => {
    if (!window.confirm(t('push.removeConfirm', { label: device.label }))) return;
    setBusy(true);
    const r = await api(`/api/push/devices/${device.id}`, { method: 'DELETE', noRedirect: true })
      .catch(() => ({ ok: false }));
    if (r.ok && device.id === thisDeviceId) {
      // Tear the browser's own subscription down too, so "off" means off rather than
      // "the server has forgotten you but your browser is still holding a live endpoint".
      await forget();
      setThisDeviceId(0);
      setBrowserState(browserCapability());
    }
    setBusy(false);
    if (!r.ok) { onToast && onToast(r.message || t('push.removeFailed'), 'error'); return; }
    await load();
    onToast && onToast(t('push.removed'), 'success');
  }, [load, onToast, t, thisDeviceId]);

  if (!loaded) return null;

  const delivery = status?.delivery || 'untested';
  const vendors = Array.isArray(status?.vendors) ? status.vendors : [];
  const canEnrol = browserState === PUSH_READY;

  return (
    <section className="push-panel" data-push="panel" data-push-state={browserState} data-push-delivery={delivery}>
      <div className="push-head">
        <span className="push-head-ico"><Ico n="bell" sz={18} /></span>
        <div className="push-head-text">
          {/* The contextual "?" goes on the sentence somebody actually needs help with: not
              "what is push", but why an install can be switched on and still reach nobody. */}
          <h3>
            {t('push.title')}
            <HelpButton slug="notifications" anchor="push-airgap" />
          </h3>
          <p>{t('push.lead')}</p>
        </div>
        {canEnrol ? (
          <button type="button" className="push-btn push-btn-primary" data-push="enable" onClick={turnOn} disabled={busy}>
            <Ico n="bell" sz={15} /><span>{t('push.turnOn')}</span>
          </button>
        ) : null}
      </div>

      {/* What stands in this BROWSER's way, when something does. Each reason gets its own
          sentence because each has a different remedy. */}
      {browserState !== PUSH_READY && browserState !== PUSH_ENABLED ? (
        <p className={`push-note push-note--stop${browserState === PUSH_INSECURE ? ' push-note--insecure' : ''}`} data-push="browser-note">
          <Ico n="warning" sz={15} />
          <span>{t(`push.browser.${browserState}`)}</span>
        </p>
      ) : null}

      {/* What this INSTALL can do, from real attempts. This is the line an air-gapped
          operator needs, and the one that must never say "working" on the strength of a
          setting being switched on. */}
      <p className={`push-verdict push-verdict--${delivery}`} data-push="verdict">
        <Ico n={delivery === 'confirmed' ? 'check-ok' : (delivery === 'no-devices' || delivery === 'untested') ? 'info' : 'warning'} sz={15} />
        <span>{t(`push.delivery.${delivery}`)}</span>
      </p>
      {delivery === 'unreachable' && vendors.length ? (
        <p className="push-note" data-push="vendors">{t('push.vendorsHint', { hosts: vendors.join(', ') })}</p>
      ) : null}

      {devices.length ? (
        <ul className="push-devices" data-push="devices">
          {devices.map((d) => (
            <li key={d.id} className="push-device" data-push="device" data-push-device={d.id} data-push-outcome={d.lastOutcome || 'untested'}>
              <div className="push-device-main">
                <span className="push-device-label">
                  {d.label}
                  {/* Two different facts, and they must not share a badge: the browser you
                      are looking at right now, and a device that happens to be yours. */}
                  {d.id === thisDeviceId
                    ? <span className="push-device-mine">{t('push.thisDevice')}</span>
                    : d.mine ? <span className="push-device-mine push-device-mine--other">{t('push.yours')}</span> : null}
                </span>
                <span className="push-device-meta">
                  {t('push.vendorLine', { vendor: d.vendor || '—' })}
                  {' · '}
                  {t(`push.floor.${d.minSeverity || 'warning'}`)}
                </span>
                <span className={`push-device-outcome push-device-outcome--${d.lastOutcome || 'untested'}`}>
                  {t(`push.outcome.${d.lastOutcome || 'untested'}`)}
                  {d.lastAttemptAt ? ` · ${formatTimestamp(d.lastAttemptAt)}` : ''}
                </span>
              </div>
              <div className="push-device-actions">
                <button type="button" className="push-btn" data-push="test" onClick={() => testDevice(d.id)} disabled={busy}>
                  <Ico n="send" sz={14} /><span>{t('push.test')}</span>
                </button>
                <button type="button" className="push-btn push-btn-danger" data-push="remove" onClick={() => removeDevice(d)} disabled={busy}>
                  <Ico n="trash" sz={14} /><span>{t('push.remove')}</span>
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className="push-note" data-push="empty">{t(session?.isSuperadmin ? 'push.emptyAdmin' : 'push.empty')}</p>
      )}

      {/* Stated on the screen, not only in the manual. Turning this on means this appliance
          starts talking to a company outside the building, and the person pressing the button
          is entitled to know that before they press it rather than after. */}
      <p className="push-note push-note--privacy" data-push="privacy">{t('push.privacy')}</p>
    </section>
  );
}

// deviceLabel names the device from what the browser will tell us. It is a convenience, not
// an identity: the operator can see it and the row is theirs either way.
function deviceLabel() {
  const ua = (navigator.userAgent || '').toLowerCase();
  const platform = /android/.test(ua) ? 'Android'
    : /iphone|ipad|ipod/.test(ua) ? 'iOS'
      : /windows/.test(ua) ? 'Windows'
        : /mac os/.test(ua) ? 'macOS'
          : /linux/.test(ua) ? 'Linux' : '';
  const browser = /edg\//.test(ua) ? 'Edge'
    : /chrome\//.test(ua) ? 'Chrome'
      : /firefox\//.test(ua) ? 'Firefox'
        : /safari\//.test(ua) ? 'Safari' : '';
  return [platform, browser].filter(Boolean).join(' ') || 'This device';
}
