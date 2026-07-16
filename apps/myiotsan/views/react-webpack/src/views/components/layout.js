import { Ico, SideNav as SharedSideNav, useT, BrandLogo as SharedBrandLogo } from '@shared';
import { roleLabel } from '../lib/helpers';

// The mark (shield + eye + check) is the suite's, shared verbatim with mymatasan,
// myseliasan and myidsan; the sensor hub only supplies its wordmark and its
// --brand-mark hue (app.css) so the family is one geometry with four tints.
export function BrandLogo(props) {
  return <SharedBrandLogo wordmark="myiotsan" {...props} />;
}

// AccountCard is the slim sign-out pill under the brand at the top of the rail: a
// role chip (avatar + role) and a ghost logout icon button. The username/identity is
// deliberately NOT shown (avoids exposing the signed-in account in the UI).
// Standardized with the mymatasan / myseliasan rails.
function AccountCard({ roleLabel, onLogout, busy }) {
  const t = useT();
  return (
    <div className="side-account">
      <span className="side-account-avatar" aria-hidden="true"><Ico n="user" sz={15} /></span>
      <span className="side-account-role">{roleLabel}</span>
      <button
        type="button"
        className="side-account-logout"
        onClick={onLogout}
        disabled={busy}
        title={t('auth.logout')}
        aria-label={t('auth.logout')}
      >
        <Ico n="logout" sz={16} />
      </button>
    </div>
  );
}

// SideNav is the standardized RBAC-app navigation rail (the shared dark side-nav with
// grouped menu entries and tone accents), carrying the sensor hub's own sections.
export function SideNav({ activeTab, busy, onTab, onLogout, session, pinned = true, onTogglePinned }) {
  const t = useT();
  const navItem = (id, label, icon, tone) => ({ id, label, icon, tone, active: id === activeTab, onClick: () => onTab(id) });
  // The rail is the app's shape: the estate (dashboard), the things in it (devices), what
  // watches them (rules → alerts → the unified feed), and how a device TYPE is declared
  // (profiles). Profiles sit under System because they are configuration, not inventory:
  // one profile is inherited by every device of that type.
  //
  // Discovery sits beside them and ONLY for an admin: opening an enrollment window is the
  // one act in the app that lets an unknown thing talk to the broker at all, and an operator
  // does not get to do it. Hiding the entry is courtesy — every /api/discovery route is
  // admin-only server-side, which is where it is actually enforced.
  // Flows (the visual data-flow canvas) is admin-only IN FULL — a flow can run JavaScript and
  // actuate a device — so, unlike scenes/schedules, the whole entry is hidden from non-admins.
  const automationItems = [
    navItem('scenes', t('nav.scenes'), 'play', 'amber'),
    navItem('schedules', t('nav.schedules'), 'clock', 'teal'),
  ];
  if (session?.isAdmin) automationItems.push(navItem('flows', t('nav.flows'), 'git-branch', 'violet'));

  const systemItems = [];
  if (session?.isAdmin) systemItems.push(navItem('discovery', t('nav.discovery'), 'search', 'blue'));
  systemItems.push(navItem('profiles', t('nav.profiles'), 'box', 'indigo'));
  // Settings configures the hub itself (users, delivery, storage, fleet, restart) — admin-only, so
  // the entry is hidden from viewers and operators. Every route behind it is admin-gated server-side.
  if (session?.isAdmin) systemItems.push(navItem('settings', t('nav.settings'), 'sliders', 'steel'));

  const groups = [
    { label: t('group.workspace'), items: [navItem('dashboard', t('nav.dashboard'), 'monitor', 'steel')] },
    { label: t('group.devices'), items: [navItem('devices', t('nav.devices'), 'cpu', 'teal')] },
    {
      label: t('group.monitoring'),
      items: [
        navItem('rules', t('nav.rules'), 'wand', 'violet'),
        navItem('alerts', t('nav.alerts'), 'bell', 'green'),
        navItem('notifications', t('nav.notifications'), 'send', 'steel'),
      ],
    },
    { label: t('group.automation'), items: automationItems },
    { label: t('group.system'), items: systemItems },
  ];

  const brand = (
    <div className="side-brand">
      {/* Pin / auto-hide toggle, standardized with the other rails: pinned keeps the
          260px rail in the grid flow, unpinned collapses it to an icon strip that slides
          out on hover. */}
      {onTogglePinned ? (
        <button
          type="button"
          className="nav-pin-toggle"
          onClick={(e) => { onTogglePinned(); e.currentTarget.blur(); }}
          aria-pressed={pinned}
          title={pinned ? t('nav.autohide') : t('nav.pin')}
          aria-label={pinned ? t('nav.autohide') : t('nav.pin')}
        >
          <Ico n={pinned ? 'pin' : 'pin-off'} sz={16} />
        </button>
      ) : null}
      <BrandLogo />
      <div className="side-brand-sub">{t('brand.sensorHub')}</div>
      <AccountCard
        roleLabel={session?.isAdmin ? t('role.admin') : (session?.roleName ? roleLabel(t, session.roleName) : t('role.member'))}
        onLogout={onLogout}
        busy={busy}
      />
    </div>
  );

  return <SharedSideNav brand={brand} groups={groups} footer={null} />;
}
