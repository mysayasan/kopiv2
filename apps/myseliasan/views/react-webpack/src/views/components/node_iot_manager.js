import { useState } from 'react';
import { Ico, Tabs, useT } from '@shared';
import { NodeIotEmbed } from './node_embed';
import { setNodeProxyBase } from './nodeiot/lib/helpers';
import { DashboardPage } from './nodeiot/dashboard';
import { DevicesPage } from './nodeiot/devices';
import { RulesPage } from './nodeiot/rules';
import { AlertsPage } from './nodeiot/alerts';
import { NotificationsPage } from './nodeiot/notifications';
import { ProfilesPage } from './nodeiot/profiles';
import { DiscoveryPage } from './nodeiot/discovery';
import { apiBase } from '../lib/helpers';

// NodeIotManager mirrors a SENSOR HUB's own app inside the control plane, the way NodeManager
// mirrors a camera node's. The pages under nodeiot/ are myiotsan's real pages; what makes them
// work here is the one line below — every `${apiBase()}/api/...` inside them is pointed at the
// node's command tunnel, so the hub's inventory, telemetry, rules, alerts and relays are read
// and written over the channel the NODE dialled out to us. The browser never touches the hub.
//
// The tabs are myseliasan's chrome (the shell's own Tabs component, outside the embed); the page
// under them is the hub's, styled by the hub's own scoped stylesheet.
const TABS = [
  { id: 'overview', labelKey: 'niot.tabOverview', icon: 'monitor' },
  { id: 'devices', labelKey: 'niot.tabDevices', icon: 'cpu' },
  { id: 'rules', labelKey: 'niot.tabRules', icon: 'wand' },
  { id: 'alerts', labelKey: 'niot.tabAlerts', icon: 'bell' },
  { id: 'notifications', labelKey: 'niot.tabNotifications', icon: 'send' },
  { id: 'profiles', labelKey: 'niot.tabProfiles', icon: 'box' },
  { id: 'provisioning', labelKey: 'niot.tabProvisioning', icon: 'search' },
];

export function NodeIotManager({ node, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  const [tab, setTab] = useState('overview');

  // Point the copied hub pages at the commander proxy BEFORE they render, so their very first
  // fetch already goes down the tunnel rather than at myseliasan's own API.
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);

  const offline = !!node.status && node.status !== 'online';
  // The hub's own address, for the one place that needs it: the provisioning page tells an
  // installer where to point a device, and the device must reach the HUB's broker — not this
  // control plane, which is a different machine on a different network.
  const host = node.ip || '';

  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2>
            <span className="btn-icon">
              <Ico n={node.icon || 'cpu'} /> {node.name || nodeId}
            </span>
          </h2>
          <div className="settings-header-actions">
            <span className="node-kind-pill node-kind-pill--iot">{t('node.kindIot')}</span>
            <span className={`status-pill ${offline ? 'offline' : 'online'}`}>
              {offline ? t('nodes.nodeOffline') : t('node.online')}
            </span>
          </div>
        </header>
        <p className="settings-hint">{node.description || t('niot.hubHint')}</p>
        {/* An offline hub is not an empty one. Say which, so nobody reads a stalled tunnel as a
            site with no sensors in it. */}
        {offline ? <p className="settings-hint danger-text">{t('niot.offlineNote')}</p> : null}
        <Tabs
          active={tab}
          onChange={setTab}
          tabs={TABS.map((tb) => ({ id: tb.id, label: t(tb.labelKey), icon: tb.icon }))}
          ariaLabel={t('niot.tabsAria')}
        />
      </section>

      <NodeIotEmbed className="nodeiot-manage">
        {tab === 'overview' ? <DashboardPage onNavigate={setTab} /> : null}
        {tab === 'devices' ? <DevicesPage onToast={onToast} /> : null}
        {tab === 'rules' ? <RulesPage onToast={onToast} /> : null}
        {tab === 'alerts' ? <AlertsPage onToast={onToast} /> : null}
        {tab === 'notifications' ? <NotificationsPage onToast={onToast} /> : null}
        {tab === 'profiles' ? <ProfilesPage onToast={onToast} /> : null}
        {tab === 'provisioning' ? <DiscoveryPage onToast={onToast} host={host} /> : null}
      </NodeIotEmbed>
    </section>
  );
}
