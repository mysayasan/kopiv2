import { useState } from 'react';
import { Ico, Tabs, useT } from '@shared';
import { NodeDoorEmbed } from './node_embed';
import { setNodeProxyBase, setTranslator } from './nodedoor/lib/access';
import { DoorsPage } from './nodedoor/doors';
import { PeoplePage } from './nodedoor/people';
import { ActivityPage } from './nodedoor/activity';
import { ReadersPage } from './nodedoor/readers';
import { SettingsPage } from './nodedoor/settings';
import { apiBase } from '../lib/helpers';

// NodeDoorManager mirrors a DOOR CONTROLLER's own app inside the control plane, the way
// NodeIotManager mirrors a sensor hub's. The pages under nodedoor/ are mypintusan's real pages;
// what makes them work here is the one line below — every call inside them is pointed at the
// node's command tunnel, so the building's doors, people, badges and access log are read and
// written over the channel the NODE dialled out to us. The browser never touches the node, and a
// remote unlock issued here lands in the node's OWN access log attributed to this operator by
// name ("cp:alice") — the same audit row a badge writes.
//
// The tabs are myseliasan's chrome (the shell's own Tabs component, outside the embed); the page
// under them is the door controller's, styled by its own scoped stylesheet.
const TABS = [
  { id: 'doors', labelKey: 'ndoor.tabDoors', icon: 'door' },
  { id: 'people', labelKey: 'ndoor.tabPeople', icon: 'user' },
  { id: 'activity', labelKey: 'ndoor.tabActivity', icon: 'list' },
  { id: 'readers', labelKey: 'ndoor.tabReaders', icon: 'cpu' },
  { id: 'settings', labelKey: 'ndoor.tabSettings', icon: 'sliders' },
];

export function NodeDoorManager({ node, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;
  const [tab, setTab] = useState('doors');

  // Point the copied door-controller pages at the commander proxy BEFORE they render, so their
  // very first fetch already goes down the tunnel rather than at myseliasan's own API.
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);
  setTranslator(t);

  const offline = !!node.status && node.status !== 'online';

  return (
    <section className="workspace">
      <section className="settings-panel span-two">
        <header>
          <h2>
            <span className="btn-icon">
              <Ico n={node.icon || 'door'} /> {node.name || nodeId}
            </span>
          </h2>
          <div className="settings-header-actions">
            <span className="node-kind-pill node-kind-pill--door">{t('node.kindDoor')}</span>
            <span className={`status-pill ${offline ? 'offline' : 'online'}`}>
              {offline ? t('nodes.nodeOffline') : t('node.online')}
            </span>
          </div>
        </header>
        <p className="settings-hint">{node.description || t('ndoor.doorHint')}</p>
        {/* An offline controller is not a building with no doors. Say which, so nobody reads a
            stalled tunnel as an empty site. */}
        {offline ? <p className="settings-hint danger-text">{t('ndoor.offlineNote')}</p> : null}
        <Tabs
          active={tab}
          onChange={setTab}
          tabs={TABS.map((tb) => ({ id: tb.id, label: t(tb.labelKey), icon: tb.icon }))}
          ariaLabel={t('ndoor.tabsAria')}
        />
      </section>

      <NodeDoorEmbed className="nodedoor-manage">
        {tab === 'doors' ? <DoorsPage onToast={onToast} /> : null}
        {tab === 'people' ? <PeoplePage onToast={onToast} /> : null}
        {tab === 'activity' ? <ActivityPage /> : null}
        {tab === 'readers' ? <ReadersPage /> : null}
        {tab === 'settings' ? <SettingsPage onToast={onToast} /> : null}
      </NodeDoorEmbed>
    </section>
  );
}
