import { useCallback, useEffect, useRef, useState } from 'react';
import { Ico, useT } from '@shared';
import { api } from '../lib/helpers';

// NodeCameraTree is a dropdown whose menu is a two-level tree — adopted Nodes, each
// expanding into its cameras (lazily fetched over the proxy). Selecting a camera reports
// { nodeId, nodeName, cameraId, cameraName } up. It's the fleet "Where" picker (which
// camera to act on) used by the Teach page in place of a flat camera <select>.
export function NodeCameraTree({ nodes = [], value, onChange, disabled, placeholder }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [camsByNode, setCamsByNode] = useState({}); // nodeId -> { loading, cams, error }
  const [expanded, setExpanded] = useState({}); // nodeId -> bool
  const wrapRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => { if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false); };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);

  const loadCams = useCallback(async (nodeId) => {
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: true, cams: m[nodeId]?.cams || [] } }));
    const r = await api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy/api/cameras?limit=200`, { noRedirect: true }).catch(() => ({ ok: false }));
    const cams = r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : [];
    setCamsByNode((m) => ({ ...m, [nodeId]: { loading: false, cams, error: !r.ok } }));
  }, []);

  const toggleNode = (nodeId) => {
    setExpanded((e) => {
      const next = !e[nodeId];
      if (next && !camsByNode[nodeId]) loadCams(nodeId);
      return { ...e, [nodeId]: next };
    });
  };

  function pick(node, cam) {
    onChange({ nodeId: node.nodeId, nodeName: node.name || node.nodeId, cameraId: cam.id, cameraName: cam.name || t('nodes.cameraN', { id: cam.id }) });
    setOpen(false);
  }

  const summary = value && value.cameraId
    ? `${value.nodeName} · ${value.cameraName}`
    : (placeholder || t('teach.pickCameraTree'));

  return (
    <div className="node-cam-tree" ref={wrapRef}>
      <button type="button" className="node-cam-tree-toggle" onClick={() => setOpen((o) => !o)} disabled={disabled} aria-haspopup="tree" aria-expanded={open}>
        <span className="node-cam-tree-summary">{summary}</span>
        <Ico n="chev-down" sz={12} />
      </button>
      {open ? (
        <div className="node-cam-tree-menu" role="tree">
          {nodes.length === 0 ? <div className="node-cam-tree-empty">{t('teach.noNodes')}</div> : nodes.map((node) => {
            const entry = camsByNode[node.nodeId];
            const isOpen = !!expanded[node.nodeId];
            const status = (node.status || '').toLowerCase() === 'online' ? 'online' : 'offline';
            return (
              <div className="node-cam-tree-node" key={node.nodeId}>
                <button type="button" className="node-cam-tree-noderow" onClick={() => toggleNode(node.nodeId)} aria-expanded={isOpen}>
                  <Ico n="chev-down" sz={11} style={isOpen ? undefined : { transform: 'rotate(-90deg)' }} />
                  <span className={`node-cam-tree-dot node-cam-tree-dot--${status}`} aria-hidden="true" />
                  <Ico n={node.icon || 'monitor'} sz={14} />
                  <span className="node-cam-tree-label">{node.name || node.nodeId}</span>
                </button>
                {isOpen ? (
                  <div className="node-cam-tree-cams">
                    {entry?.loading ? <div className="node-cam-tree-msg">{t('teach.loadingCams')}</div>
                      : entry?.error ? <div className="node-cam-tree-msg">{t('teach.camsFailed')}</div>
                      : (entry?.cams || []).length === 0 ? <div className="node-cam-tree-msg">{t('teach.noCams')}</div>
                      : entry.cams.map((cam) => {
                        const sel = value && value.nodeId === node.nodeId && String(value.cameraId) === String(cam.id);
                        const cstatus = (cam.healthStatus || '').toLowerCase() === 'online' ? 'online' : (cam.healthStatus || '').toLowerCase() === 'offline' ? 'offline' : 'unknown';
                        return (
                          <button type="button" key={cam.id} className={`node-cam-tree-camrow${sel ? ' selected' : ''}`} onClick={() => pick(node, cam)} role="treeitem">
                            <span className={`node-cam-tree-dot node-cam-tree-dot--${cstatus}`} aria-hidden="true" />
                            <Ico n="camera" sz={13} />
                            <span className="node-cam-tree-label">{cam.name || t('nodes.cameraN', { id: cam.id })}</span>
                          </button>
                        );
                      })}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
