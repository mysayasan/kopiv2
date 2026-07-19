import { useState } from 'react';
import PropTypes from 'prop-types';
import { useT, Tabs } from '@shared';
import { FleetMap } from './fleet_map';
import { IndoorMap } from './indoor_map';
import '../styles/fleet-map.css';

// MapPage is the single "Map" module: the standard shared tab bar switches between the
// geographic view (nodes at lat/lon over the offline basemap) and the floor-plan view (nodes
// and cameras placed on uploaded plans). Both share the status vocabulary; they differ only in
// the surface the markers sit on — the world, or a building.
export function MapPage({ nodes, reloadNodes, onToast, onOpenNode }) {
  const t = useT();
  const [mode, setMode] = useState('geo'); // 'geo' | 'indoor'

  return (
    <section className="workspace map-page">
      <Tabs
        tabs={[
          { id: 'geo', label: t('map.viewGeo'), icon: 'map' },
          { id: 'indoor', label: t('map.viewIndoor'), icon: 'building' },
        ]}
        active={mode}
        onChange={setMode}
        ariaLabel={t('map.viewLabel')}
      />
      {mode === 'geo' ? (
        <FleetMap nodes={nodes} reloadNodes={reloadNodes} onToast={onToast} onOpenNode={onOpenNode} />
      ) : (
        <IndoorMap nodes={nodes} reloadNodes={reloadNodes} onToast={onToast} onOpenNode={onOpenNode} />
      )}
    </section>
  );
}

MapPage.propTypes = {
  nodes: PropTypes.array,
  reloadNodes: PropTypes.func,
  onToast: PropTypes.func,
  onOpenNode: PropTypes.func,
};
