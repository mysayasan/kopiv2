import { apiBase } from '../../lib/helpers';

// Same-origin control-plane URLs for a node event's annotated snapshot and its recorded clip.
// Both route through myseliasan's node proxy / recording-stream, so the browser never contacts the
// node directly (the same rule the Notifications page follows) - which is what lets an air-gapped
// control plane show footage from an appliance the browser has no route to.
//
// All that is left of map/popups.js, which existed for the floating cards the inspector replaced.

export const eventSnapshotSrc = (nodeId, alertId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/vision/alerts/${alertId}/snapshot?annotated=1`;

export const recordingStreamSrc = (nodeId, segId) =>
  `${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/recording-stream/${segId}`;
