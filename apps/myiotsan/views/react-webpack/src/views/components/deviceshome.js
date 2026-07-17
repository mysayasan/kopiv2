import { Tabs, useT } from '@shared';
import { DevicesPage } from './devices';
import { DiscoveryPage } from './discovery';

// DevicesHome is the one place a device is added. It puts the two ways to get a device into the
// inventory side by side under the Devices tab:
//
//   - Inventory: the device list, and "Add device" for one you already know the details of.
//   - Discover:  active network scan + the MQTT enrollment window + the candidate/adopt flow —
//     the whole onboarding surface, which is admin-only and security-sensitive (a scan sweeps the
//     LAN; the window opens a hole in the broker), so the tab is hidden for non-admins.
//
// This replaces the separate "Discovery" nav entry: onboarding was hard to find as its own page,
// and "add a device" is where an operator looks for it. Every /api/discovery route stays admin-only
// server-side, so hiding the tab is UX on top of the real enforcement, not the enforcement itself.
// sub/onSub are controlled by App so another surface (the first-run wizard) can jump straight to the
// Discover sub-tab.
export function DevicesHome({ onToast, session, sub, onSub }) {
  const t = useT();
  const isAdmin = !!session?.isAdmin;
  // A non-admin can never sit on the Discover tab (it is not offered), but guard anyway.
  const active = !isAdmin && sub === 'discover' ? 'inventory' : sub || 'inventory';

  const tabs = [{ id: 'inventory', label: t('devices.tabInventory'), icon: 'cpu' }];
  if (isAdmin) tabs.push({ id: 'discover', label: t('devices.tabDiscover'), icon: 'search' });

  return (
    <div className="devices-home">
      <Tabs tabs={tabs} active={active} onChange={onSub} ariaLabel={t('nav.devices')} />
      {active === 'discover' && isAdmin ? (
        <DiscoveryPage onToast={onToast} />
      ) : (
        <DevicesPage onToast={onToast} session={session} />
      )}
    </div>
  );
}
