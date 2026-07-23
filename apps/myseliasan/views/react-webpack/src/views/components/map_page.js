import PropTypes from 'prop-types';
import { FleetMap } from './fleet_map';
import '../styles/fleet-map.css';

// MapPage is the single "Map" module: the geographic fleet map IS the map. Buildings are created,
// positioned and authored from it — the wizard collects name/areas, the map takes the drop point,
// and the building editor opens over it to draw the plan and pin cameras. There is no separate
// floor-plan tab: a floor plan is something a building HAS, not a parallel place to navigate to.
export function MapPage({ nodes, reloadNodes, onToast, onOpenNode }) {
  return (
    <section className="workspace map-page">
      <FleetMap nodes={nodes} reloadNodes={reloadNodes} onToast={onToast} onOpenNode={onOpenNode} />
    </section>
  );
}

MapPage.propTypes = {
  nodes: PropTypes.array,
  reloadNodes: PropTypes.func,
  onToast: PropTypes.func,
  onOpenNode: PropTypes.func,
};
