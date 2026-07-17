import { useCallback, useEffect, useRef, useState } from 'react';
import { DataTable, Ico, useT } from '@shared';
import { api, errorMessage } from '../lib/helpers';
import { ConfirmModal, Field, Modal, Panel } from './ui';

// The Flows page: a Node-RED-style visual, executable data-flow canvas. A flow is a graph of
// nodes (telemetry inputs, transforms — including sandboxed JavaScript — logic, and outputs) joined
// by wires; the runtime compiles it and runs it against the live telemetry stream. Admin-only (the
// nav entry is hidden from non-admins and every /api/flows route is admin-gated server-side).
//
// This module has two halves: FlowsPage (the list) and FlowEditor (the canvas). The canvas reuses
// the pointer-capture drag technique the mymatasan zone editor uses — an SVG world under a
// pan/zoom transform, screen→world conversion via getBoundingClientRect, element-level port
// handlers for wiring, and a whole-graph snapshot undo/redo.

// --- node palette metadata ------------------------------------------------------------------
//
// One entry per node type: how it draws (icon), which ports it has, and which config fields it
// exposes. The backend is the source of truth for behaviour (services/flows.go); this is only the
// authoring surface, so it mirrors that node set.

const NODE_W = 176;
const NODE_H = 58;

// field specs: {key, kind, labelKey, options?}
const NODE_DEFS = {
  device_telemetry: {
    group: 'input', icon: 'cpu', labelKey: 'flows.node.deviceTelemetry', in: false, out: true,
    fields: [
      { key: 'deviceKey', kind: 'device', labelKey: 'flows.f.deviceKey' },
      { key: 'key', kind: 'text', labelKey: 'flows.f.telemetryKey' },
    ],
    summary: (c) => `${c.deviceKey || '?'} · ${c.key || '?'}`,
  },
  scale: {
    group: 'transform', icon: 'sliders', labelKey: 'flows.node.scale', in: true, out: true,
    fields: [
      { key: 'factor', kind: 'number', labelKey: 'flows.f.factor' },
      { key: 'offset', kind: 'number', labelKey: 'flows.f.offset' },
    ],
    summary: (c) => `×${c.factor ?? 1} ${Number(c.offset) ? (Number(c.offset) > 0 ? '+' : '') + c.offset : ''}`.trim(),
  },
  expression: {
    group: 'transform', icon: 'wand', labelKey: 'flows.node.expression', in: true, out: true,
    fields: [{ key: 'expr', kind: 'text', labelKey: 'flows.f.expr' }],
    summary: (c) => c.expr || 'msg.payload',
  },
  function: {
    group: 'transform', icon: 'git-branch', labelKey: 'flows.node.function', in: true, out: true,
    fields: [{ key: 'code', kind: 'code', labelKey: 'flows.f.code' }],
    summary: () => 'JavaScript',
  },
  threshold: {
    group: 'logic', icon: 'activity', labelKey: 'flows.node.threshold', in: true, out: true,
    fields: [
      { key: 'op', kind: 'select', labelKey: 'flows.f.op', options: ['>', '>=', '<', '<=', '==', '!='] },
      { key: 'value', kind: 'number', labelKey: 'flows.f.value' },
    ],
    summary: (c) => `payload ${c.op || '>'} ${c.value ?? 0}`,
  },
  switch: {
    group: 'logic', icon: 'git-branch', labelKey: 'flows.node.switch', in: true, out: true,
    fields: [{ key: 'predicate', kind: 'text', labelKey: 'flows.f.predicate' }],
    summary: (c) => c.predicate || 'true',
  },
  deadband: {
    group: 'logic', icon: 'activity', labelKey: 'flows.node.deadband', in: true, out: true,
    fields: [{ key: 'delta', kind: 'number', labelKey: 'flows.f.delta' }],
    summary: (c) => `Δ ≥ ${c.delta ?? 0}`,
  },
  throttle: {
    group: 'logic', icon: 'pause', labelKey: 'flows.node.throttle', in: true, out: true,
    fields: [{ key: 'seconds', kind: 'number', labelKey: 'flows.f.seconds' }],
    summary: (c) => `≤ 1 / ${c.seconds ?? 0}s`,
  },
  debug: {
    group: 'output', icon: 'search', labelKey: 'flows.node.debug', in: true, out: false,
    fields: [{ key: 'label', kind: 'text', labelKey: 'flows.f.label' }],
    summary: (c) => c.label || 'debug',
  },
  notify: {
    group: 'output', icon: 'bell', labelKey: 'flows.node.notify', in: true, out: false,
    fields: [
      { key: 'title', kind: 'text', labelKey: 'flows.f.title' },
      { key: 'body', kind: 'text', labelKey: 'flows.f.body' },
      { key: 'severity', kind: 'select', labelKey: 'flows.f.severity', options: ['info', 'warning', 'critical'] },
      { key: 'category', kind: 'select', labelKey: 'flows.f.category', options: ['device.alert', 'system'] },
    ],
    summary: (c) => c.title || 'notify',
  },
  command: {
    group: 'output', icon: 'zap', labelKey: 'flows.node.command', in: true, out: false,
    fields: [
      { key: 'deviceKey', kind: 'device', labelKey: 'flows.f.deviceKey' },
      { key: 'command', kind: 'text', labelKey: 'flows.f.command' },
      { key: 'value', kind: 'number', labelKey: 'flows.f.valueOpt' },
    ],
    summary: (c) => `${c.deviceKey || '?'} · ${c.command || '?'}`,
  },
  derived_metric: {
    group: 'output', icon: 'server', labelKey: 'flows.node.derived', in: true, out: false,
    fields: [
      { key: 'deviceKey', kind: 'device', labelKey: 'flows.f.deviceKey' },
      { key: 'key', kind: 'text', labelKey: 'flows.f.derivedKey' },
    ],
    summary: (c) => `${c.deviceKey || '?'} · ${c.key || '?'}`,
  },
  mqtt_out: {
    group: 'output', icon: 'send', labelKey: 'flows.node.mqttOut', in: true, out: false,
    fields: [
      { key: 'topic', kind: 'text', labelKey: 'flows.f.topic' },
      { key: 'qos', kind: 'select', labelKey: 'flows.f.qos', options: ['0', '1', '2'] },
      { key: 'retain', kind: 'bool', labelKey: 'flows.f.retain' },
    ],
    summary: (c) => c.topic || 'mqtt',
  },
};

const PALETTE_GROUPS = [
  { group: 'input', types: ['device_telemetry'] },
  { group: 'transform', types: ['scale', 'expression', 'function'] },
  { group: 'logic', types: ['threshold', 'switch', 'deadband', 'throttle'] },
  { group: 'output', types: ['debug', 'notify', 'command', 'derived_metric', 'mqtt_out'] },
];

// A device reference of the form "$name" is a SLOT — a placeholder that makes the flow a reusable
// template, bound to a real device at instantiation. This mirrors services/flows.go slotName.
const SLOT_RE = /^\$([A-Za-z0-9_.-]+)$/;
function graphSlots(graphStr) {
  const g = parseGraph(graphStr);
  const seen = new Set();
  const out = [];
  for (const n of g.nodes || []) {
    const m = SLOT_RE.exec(String((n.config && n.config.deviceKey) || '').trim());
    if (m && !seen.has(m[1])) { seen.add(m[1]); out.push(m[1]); }
  }
  return out;
}

let nodeSeq = 0;
function newNodeId(type) { nodeSeq += 1; return `${type}_${Date.now().toString(36)}${nodeSeq}`; }

// nodeWarnings returns a map of nodeId -> human warning for anything an author is likely to have
// left half-done: a node with no wire where it needs one, an input/output missing its target, an
// empty code node. These are non-blocking hints shown on the canvas; the server still hard-validates
// on save (unknown types, cycles).
function nodeWarnings(graph, t) {
  const warn = {};
  const hasIn = {}, hasOut = {};
  for (const w of graph.wires || []) { hasOut[w.from.node] = true; hasIn[w.to.node] = true; }
  for (const n of graph.nodes || []) {
    const def = NODE_DEFS[n.type] || {};
    const c = n.config || {};
    const msgs = [];
    if (def.in && !hasIn[n.id]) msgs.push(t('flows.warn.noInput'));
    if (def.out && !hasOut[n.id]) msgs.push(t('flows.warn.noOutput'));
    if (n.type === 'device_telemetry' && (!c.deviceKey || !c.key)) msgs.push(t('flows.warn.setDeviceKey'));
    if ((n.type === 'function' || n.type === 'expression' || n.type === 'switch') && !String(c.code || c.expr || c.predicate || '').trim()) msgs.push(t('flows.warn.emptyCode'));
    if (n.type === 'command' && (!c.deviceKey || !c.command)) msgs.push(t('flows.warn.setDeviceCommand'));
    if (n.type === 'derived_metric' && (!c.deviceKey || !c.key)) msgs.push(t('flows.warn.setDeviceKey'));
    if (n.type === 'mqtt_out' && !c.topic) msgs.push(t('flows.warn.setTopic'));
    if (msgs.length) warn[n.id] = msgs.join(', ');
  }
  return warn;
}

function emptyGraph() { return { nodes: [], wires: [] }; }
function parseGraph(raw) {
  try { const g = JSON.parse(raw || '{}'); return { nodes: g.nodes || [], wires: g.wires || [] }; }
  catch { return emptyGraph(); }
}

// --- FlowsPage (the list) -------------------------------------------------------------------

export function FlowsPage({ onToast, session }) {
  const t = useT();
  const [flows, setFlows] = useState([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // flow id, 0 for new, null for list
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [deleting, setDeleting] = useState(false);
  const [instantiating, setInstantiating] = useState(null); // a template flow to stamp out
  const importRef = useRef(null);

  const load = useCallback(async () => {
    setBusy(true);
    const r = await api('/api/flows');
    if (r.ok) { setFlows(r.body?.items || []); setError(''); }
    else setError(errorMessage(r, t('flows.loadFailed')));
    setBusy(false);
  }, [t]);
  useEffect(() => { load(); }, [load]);

  async function toggleEnabled(flow) {
    const r = await api(`/api/flows/${flow.id}`, {
      method: 'PUT',
      body: JSON.stringify({ name: flow.name, slug: flow.slug, description: flow.description, category: flow.category, enabled: !flow.enabled, graph: flow.graph }),
    });
    if (!r.ok) { onToast(errorMessage(r, t('flows.saveFailed')), 'error'); return; }
    onToast(!flow.enabled ? t('flows.enabled', { name: flow.name }) : t('flows.disabled', { name: flow.name }), 'success');
    load();
  }

  async function remove(flow) {
    setDeleting(true);
    const r = await api(`/api/flows/${flow.id}`, { method: 'DELETE' });
    setDeleting(false);
    setConfirmDelete(null);
    if (!r.ok) { onToast(errorMessage(r, t('flows.deleteFailed')), 'error'); return; }
    onToast(t('flows.deleted', { name: flow.name }), 'success');
    load();
  }

  async function exportFlow(flow) {
    const r = await api(`/api/flows/${flow.id}/export`);
    if (!r.ok) { onToast(errorMessage(r, t('flows.exportFailed')), 'error'); return; }
    const blob = new Blob([JSON.stringify(r.body, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `${flow.slug || 'flow'}.iotflow`;
    document.body.appendChild(a); a.click(); a.remove();
    URL.revokeObjectURL(url);
  }

  async function importFile(file) {
    const text = await file.text();
    const r = await api('/api/flows/import', { method: 'POST', body: text });
    if (!r.ok) { onToast(errorMessage(r, t('flows.importFailed')), 'error'); return; }
    onToast(t('flows.imported'), 'success');
    load();
  }

  if (editing !== null) {
    return <FlowEditor flowId={editing} onBack={() => setEditing(null)} onSaved={() => { setEditing(null); load(); }} onToast={onToast} />;
  }

  const columns = [
    { key: 'name', label: t('flows.name'), render: (v, row) => {
      const isTemplate = graphSlots(row.graph).length > 0;
      return (
        <button type="button" className="quiet iot-link-cell" onClick={() => setEditing(row.id)}>
          {v}
          {row.builtin ? <span className="flow-builtin-tag">{t('flows.builtin')}</span> : null}
          {isTemplate ? <span className="flow-builtin-tag is-template">{t('flows.template')}</span> : null}
        </button>
      );
    } },
    { key: 'category', label: t('flows.category') },
    { key: 'enabled', label: t('flows.status'), filterType: 'boolean', render: (v) => (
      v ? <span className="status-pill resolved">{t('flows.on')}</span> : <span className="status-pill">{t('flows.off')}</span>
    ) },
    { key: 'actions', label: '', filterable: false, render: (_v, row) => {
      const isTemplate = graphSlots(row.graph).length > 0;
      return (
        <div className="table-actions">
          {isTemplate ? (
            <button type="button" onClick={() => setInstantiating(row)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('flows.instantiate')}</span>
            </button>
          ) : (
            <button type="button" className="quiet" onClick={() => toggleEnabled(row)}>
              <span className="btn-icon"><Ico n={row.enabled ? 'pause' : 'play'} sz={14} /> {row.enabled ? t('flows.disable') : t('flows.enable')}</span>
            </button>
          )}
          <button type="button" className="quiet" onClick={() => setEditing(row.id)}>{t('common.edit')}</button>
          <button type="button" className="quiet" onClick={() => exportFlow(row)}><Ico n="download" sz={14} /></button>
          {!row.builtin ? <button type="button" className="quiet danger-text" onClick={() => setConfirmDelete(row)}><Ico n="trash" sz={14} /></button> : null}
        </div>
      );
    } },
  ];

  return (
    <section className="workspace">
      <Panel
        icon="git-branch"
        title={t('page.flows')}
        hint={t('page.flowsHint')}
        actions={(
          <>
            <button type="button" className="quiet" onClick={load} disabled={busy}>
              <span className="btn-icon"><Ico n="refresh" sz={14} /> {t('common.refresh')}</span>
            </button>
            <button type="button" className="quiet" onClick={() => importRef.current?.click()}>
              <span className="btn-icon"><Ico n="folder" sz={14} /> {t('flows.import')}</span>
            </button>
            <input ref={importRef} type="file" accept=".iotflow,.json,application/json" style={{ display: 'none' }}
              onChange={(e) => { const f = e.target.files?.[0]; if (f) importFile(f); e.target.value = ''; }} />
            <button type="button" onClick={() => setEditing(0)}>
              <span className="btn-icon"><Ico n="plus" sz={14} /> {t('flows.add')}</span>
            </button>
          </>
        )}
      >
        {error ? <div className="status-line">{error}</div> : null}
        <DataTable rows={flows} columns={columns} busy={busy} pageSize={10} emptyText={t('flows.empty')} />
      </Panel>

      {confirmDelete ? (
        <ConfirmModal
          title={t('flows.deleteTitle')} body={t('flows.deleteBody', { name: confirmDelete.name })}
          confirmLabel={t('common.delete')} busy={deleting}
          onCancel={() => setConfirmDelete(null)} onConfirm={() => remove(confirmDelete)}
        />
      ) : null}

      {instantiating ? (
        <InstantiateModal
          flow={instantiating}
          onClose={() => setInstantiating(null)}
          onDone={() => { setInstantiating(null); load(); }}
          onToast={onToast}
        />
      ) : null}
    </section>
  );
}

// InstantiateModal stamps a concrete flow out of a template: for each device-role slot the template
// declares, the admin binds a real device, and a new (disabled) flow is created with the slots
// resolved. This is the "design once, deploy many" payoff.
function InstantiateModal({ flow, onClose, onDone, onToast }) {
  const t = useT();
  const slots = graphSlots(flow.graph);
  const [devices, setDevices] = useState([]);
  const [name, setName] = useState(`${flow.name} — `);
  const [bindings, setBindings] = useState({});
  const [busy, setBusy] = useState(false);

  useEffect(() => { api('/api/devices').then((r) => { if (r.ok) setDevices(r.body?.items || []); }); }, []);

  async function submit(e) {
    e.preventDefault();
    const missing = slots.filter((s) => !bindings[s]);
    if (missing.length) { onToast(t('flows.bindAll'), 'error'); return; }
    setBusy(true);
    const r = await api(`/api/flows/${flow.id}/instantiate`, { method: 'POST', body: JSON.stringify({ name: name.trim(), bindings }) });
    setBusy(false);
    if (!r.ok) { onToast(errorMessage(r, t('flows.instantiateFailed')), 'error'); return; }
    onToast(t('flows.instantiated'), 'success');
    onDone();
  }

  return (
    <Modal title={t('flows.instantiateTitle', { name: flow.name })} onClose={busy ? undefined : onClose}>
      <form onSubmit={submit} className="flow-config-body">
        <p className="settings-hint">{t('flows.instantiateHint')}</p>
        <Field label={t('flows.name')} span><input value={name} onChange={(e) => setName(e.target.value)} /></Field>
        {slots.map((s) => (
          <Field key={s} label={`$${s}`} span>
            <select value={bindings[s] || ''} onChange={(e) => setBindings((b) => ({ ...b, [s]: e.target.value }))}>
              <option value="">{t('flows.pickDevice')}</option>
              {devices.map((d) => <option key={d.id} value={d.deviceKey}>{d.name} ({d.deviceKey})</option>)}
            </select>
          </Field>
        ))}
        <div className="modal-actions">
          <button type="button" className="quiet" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
          <button type="submit" disabled={busy}>{busy ? t('common.saving') : t('flows.create')}</button>
        </div>
      </form>
    </Modal>
  );
}

// --- FlowEditor (the canvas) ----------------------------------------------------------------

const EMPTY_FLOW = { name: '', slug: '', description: '', category: '', enabled: false, graph: '' };

export function FlowEditor({ flowId, onBack, onSaved, onToast }) {
  const t = useT();
  const [meta, setMeta] = useState(EMPTY_FLOW);
  const [graph, setGraph] = useState(emptyGraph());
  const [devices, setDevices] = useState([]);
  const [busy, setBusy] = useState(true);
  const [saving, setSaving] = useState(false);
  const [selected, setSelected] = useState(null);      // { kind:'node'|'wire', id }
  const [view, setView] = useState({ x: 40, y: 20, zoom: 1 });
  const [pending, setPending] = useState(null);        // live wire being drawn: {from, x, y}
  const [debug, setDebug] = useState({});              // nodeId -> {payload, key, seq}
  const svgRef = useRef(null);
  const dragRef = useRef(null);
  const history = useRef({ past: [], future: [] });

  // load flow + devices
  useEffect(() => {
    let live = true;
    (async () => {
      setBusy(true);
      const dv = await api('/api/devices');
      if (live && dv.ok) setDevices(dv.body?.items || []);
      if (flowId) {
        const r = await api(`/api/flows/${flowId}`);
        if (live && r.ok) { setMeta({ ...EMPTY_FLOW, ...r.body }); setGraph(parseGraph(r.body?.graph)); }
      } else {
        setMeta(EMPTY_FLOW); setGraph(emptyGraph());
      }
      if (live) setBusy(false);
    })();
    return () => { live = false; };
  }, [flowId]);

  // Wheel-zoom via a NATIVE non-passive listener. React attaches onWheel as passive, so a React
  // handler's preventDefault is ignored and the page would scroll while zooming. Re-runs when the
  // canvas mounts (busy flips false). Zoom keeps the point under the cursor stationary.
  useEffect(() => {
    const el = svgRef.current;
    if (!el) return undefined;
    const onWheel = (e) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const mx = e.clientX - rect.left, my = e.clientY - rect.top;
      setView((v) => {
        const zoom = Math.min(2.2, Math.max(0.4, v.zoom * (e.deltaY < 0 ? 1.1 : 1 / 1.1)));
        return { zoom, x: mx - (mx - v.x) * (zoom / v.zoom), y: my - (my - v.y) * (zoom / v.zoom) };
      });
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, [busy]);

  // live debug polling while an existing, enabled flow is open
  useEffect(() => {
    if (!flowId || !meta.enabled) { setDebug({}); return undefined; }
    let live = true;
    const tick = async () => {
      const r = await api(`/api/flows/${flowId}/debug`);
      if (live && r.ok) setDebug(r.body?.nodes || {});
    };
    tick();
    const h = setInterval(tick, 1500);
    return () => { live = false; clearInterval(h); };
  }, [flowId, meta.enabled]);

  // --- history ---
  const snapshot = useCallback(() => {
    history.current.past.push(JSON.stringify(graph));
    if (history.current.past.length > 100) history.current.past.shift();
    history.current.future = [];
  }, [graph]);
  const undo = () => {
    const h = history.current;
    if (!h.past.length) return;
    h.future.push(JSON.stringify(graph));
    setGraph(JSON.parse(h.past.pop()));
    setSelected(null);
  };
  const redo = () => {
    const h = history.current;
    if (!h.future.length) return;
    h.past.push(JSON.stringify(graph));
    setGraph(JSON.parse(h.future.pop()));
    setSelected(null);
  };

  // --- graph mutations ---
  const updateNode = (id, patch) => setGraph((g) => ({ ...g, nodes: g.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)) }));
  const updateNodeConfig = (id, cfgPatch) => { snapshot(); setGraph((g) => ({ ...g, nodes: g.nodes.map((n) => (n.id === id ? { ...n, config: { ...n.config, ...cfgPatch } } : n)) })); };

  function addNode(type) {
    snapshot();
    const center = clientToWorld({ clientX: (svgRef.current?.clientWidth || 600) / 2 + (svgRef.current?.getBoundingClientRect().left || 0), clientY: 150 }, svgRef.current, view);
    // Cascade each new node so they don't stack on the same spot.
    const off = (graph.nodes.length % 8) * 28;
    const node = { id: newNodeId(type), type, x: Math.round(center.x - NODE_W / 2 + off), y: Math.round(center.y + off), config: {} };
    setGraph((g) => ({ ...g, nodes: [...g.nodes, node] }));
    setSelected({ kind: 'node', id: node.id });
  }
  function deleteSelected() {
    if (!selected) return;
    snapshot();
    if (selected.kind === 'node') {
      setGraph((g) => ({ nodes: g.nodes.filter((n) => n.id !== selected.id), wires: g.wires.filter((w) => w.from.node !== selected.id && w.to.node !== selected.id) }));
    } else {
      setGraph((g) => ({ ...g, wires: g.wires.filter((_, i) => `w${i}` !== selected.id) }));
    }
    setSelected(null);
  }
  // Delete / Backspace removes the selected node or wire (unless a text field is focused). A ref
  // keeps the always-current deleteSelected without re-subscribing the listener every render.
  const deleteRef = useRef();
  deleteRef.current = deleteSelected;
  useEffect(() => {
    function onKey(e) {
      if (e.key !== 'Delete' && e.key !== 'Backspace') return;
      const el = e.target;
      const tag = (el.tagName || '').toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable) return;
      e.preventDefault();
      deleteRef.current && deleteRef.current();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  function addWire(fromNode, toNode) {
    if (fromNode === toNode) return;
    setGraph((g) => {
      if (g.wires.some((w) => w.from.node === fromNode && w.to.node === toNode)) return g;
      snapshot();
      return { ...g, wires: [...g.wires, { from: { node: fromNode }, to: { node: toNode } }] };
    });
  }

  // --- pointer geometry ---
  function clientToWorld(e, svgEl, v) {
    const rect = (svgEl || svgRef.current).getBoundingClientRect();
    return { x: (e.clientX - rect.left - v.x) / v.zoom, y: (e.clientY - rect.top - v.y) / v.zoom };
  }

  function onPointerDownNode(e, node) {
    if (e.button !== 0) return;
    e.stopPropagation();
    svgRef.current.setPointerCapture(e.pointerId);
    const w = clientToWorld(e, svgRef.current, view);
    dragRef.current = { kind: 'node', id: node.id, dx: w.x - node.x, dy: w.y - node.y, moved: false };
    setSelected({ kind: 'node', id: node.id });
  }
  function onPointerDownOutPort(e, node) {
    if (e.button !== 0) return;
    e.stopPropagation();
    // NOTE: deliberately NO setPointerCapture here. Capturing on the SVG would redirect the
    // pointerup to it, so the target input port's onPointerUp (which completes the wire) would
    // never fire. Without capture, releasing over an input port triggers that port's handler.
    const w = clientToWorld(e, svgRef.current, view);
    dragRef.current = { kind: 'wire', from: node.id };
    setPending({ from: node.id, x: w.x, y: w.y });
  }
  function onPointerUpInPort(e, node) {
    e.stopPropagation();
    const d = dragRef.current;
    if (d && d.kind === 'wire') addWire(d.from, node.id);
    dragRef.current = null;
    setPending(null);
  }
  function onPointerDownCanvas(e) {
    if (e.button !== 0) return;
    svgRef.current.setPointerCapture(e.pointerId);
    dragRef.current = { kind: 'pan', sx: e.clientX, sy: e.clientY, ox: view.x, oy: view.y };
    setSelected(null);
  }
  function onPointerMove(e) {
    const d = dragRef.current;
    if (!d) return;
    if (d.kind === 'node') {
      const w = clientToWorld(e, svgRef.current, view);
      if (!d.moved) { snapshot(); d.moved = true; }
      updateNode(d.id, { x: Math.round(w.x - d.dx), y: Math.round(w.y - d.dy) });
    } else if (d.kind === 'pan') {
      setView((v) => ({ ...v, x: d.ox + (e.clientX - d.sx), y: d.oy + (e.clientY - d.sy) }));
    } else if (d.kind === 'wire') {
      const w = clientToWorld(e, svgRef.current, view);
      setPending((p) => (p ? { ...p, x: w.x, y: w.y } : p));
    }
  }
  function onPointerUp(e) {
    if (dragRef.current?.kind === 'wire') setPending(null);
    dragRef.current = null;
    try { svgRef.current.releasePointerCapture(e.pointerId); } catch { /* noop */ }
  }

  // --- save / test ---
  async function save() {
    if (!meta.name.trim()) { onToast(t('flows.needName'), 'error'); return; }
    setSaving(true);
    const body = { name: meta.name.trim(), slug: meta.slug, description: meta.description, category: meta.category, enabled: !!meta.enabled, graph: JSON.stringify(graph) };
    const r = flowId
      ? await api(`/api/flows/${flowId}`, { method: 'PUT', body: JSON.stringify(body) })
      : await api('/api/flows', { method: 'POST', body: JSON.stringify(body) });
    setSaving(false);
    if (!r.ok) { onToast(errorMessage(r, t('flows.saveFailed')), 'error'); return; }
    onToast(t('flows.saved', { name: body.name }), 'success');
    onSaved();
  }

  async function testFire() {
    if (!flowId) { onToast(t('flows.saveFirst'), 'error'); return; }
    const r = await api(`/api/flows/${flowId}/run`, { method: 'POST', body: JSON.stringify({ seed: 1 }) });
    if (!r.ok) { onToast(errorMessage(r, t('flows.runFailed')), 'error'); return; }
    setDebug(r.body?.nodes || {});
    onToast(t('flows.tested'), 'success');
  }

  const selectedNode = selected?.kind === 'node' ? graph.nodes.find((n) => n.id === selected.id) : null;
  const warnings = nodeWarnings(graph, t);
  const warnCount = Object.keys(warnings).length;

  if (busy) return <section className="workspace"><Panel icon="git-branch" title={t('flows.editTitle')}><p className="settings-hint">{t('common.loading')}</p></Panel></section>;

  return (
    <section className="workspace flow-editor">
      <div className="flow-toolbar">
        <button type="button" className="quiet" onClick={onBack}><span className="btn-icon"><Ico n="arr-left" sz={14} /> {t('common.back')}</span></button>
        <input className="flow-name-input" value={meta.name} placeholder={t('flows.namePlaceholder')} onChange={(e) => setMeta((m) => ({ ...m, name: e.target.value }))} />
        <input className="flow-cat-input" value={meta.category || ''} placeholder={t('flows.category')} onChange={(e) => setMeta((m) => ({ ...m, category: e.target.value }))} />
        <label className="check-row flow-enable"><input type="checkbox" checked={!!meta.enabled} onChange={(e) => setMeta((m) => ({ ...m, enabled: e.target.checked }))} /> {t('flows.enable')}</label>
        <div className="flow-toolbar-spacer" />
        {warnCount ? <span className="flow-warn-count" title={t('flows.warnHint')}><Ico n="warning" sz={13} /> {warnCount}</span> : null}
        <button type="button" className="quiet" onClick={undo} title={t('flows.undo')}><Ico n="undo" sz={15} /></button>
        <button type="button" className="quiet" onClick={redo} title={t('flows.redo')}><Ico n="redo" sz={15} /></button>
        <button type="button" className="quiet" onClick={testFire} disabled={!flowId}><span className="btn-icon"><Ico n="play" sz={14} /> {t('flows.test')}</span></button>
        <button type="button" onClick={save} disabled={saving}><span className="btn-icon"><Ico n="save" sz={14} /> {saving ? t('common.saving') : t('common.save')}</span></button>
      </div>

      <div className="flow-body">
        <div className="flow-palette">
          {PALETTE_GROUPS.map((pg) => (
            <div key={pg.group} className="flow-palette-group">
              <div className="flow-palette-label">{t(`flows.group.${pg.group}`)}</div>
              {pg.types.map((type) => (
                <button key={type} type="button" className={`flow-palette-item is-${pg.group}`} onClick={() => addNode(type)}>
                  <Ico n={NODE_DEFS[type].icon} sz={14} /> {t(NODE_DEFS[type].labelKey)}
                </button>
              ))}
            </div>
          ))}
        </div>

        <div className="flow-canvas-wrap">
          <svg
            ref={svgRef} className="flow-canvas"
            onPointerDown={onPointerDownCanvas} onPointerMove={onPointerMove} onPointerUp={onPointerUp}
          >
            <g transform={`translate(${view.x},${view.y}) scale(${view.zoom})`}>
              {graph.wires.map((w, i) => {
                const from = graph.nodes.find((n) => n.id === w.from.node);
                const to = graph.nodes.find((n) => n.id === w.to.node);
                if (!from || !to) return null;
                const id = `w${i}`;
                return (
                  <path key={id} className={`flow-wire${selected?.kind === 'wire' && selected.id === id ? ' is-selected' : ''}`}
                    d={wirePath(from.x + NODE_W, from.y + NODE_H / 2, to.x, to.y + NODE_H / 2)}
                    onPointerDown={(e) => { e.stopPropagation(); setSelected({ kind: 'wire', id }); }} />
                );
              })}
              {pending ? (() => {
                const from = graph.nodes.find((n) => n.id === pending.from);
                if (!from) return null;
                return <path className="flow-wire is-pending" d={wirePath(from.x + NODE_W, from.y + NODE_H / 2, pending.x, pending.y)} />;
              })() : null}

              {graph.nodes.map((node) => {
                const def = NODE_DEFS[node.type] || {};
                const isSel = selected?.kind === 'node' && selected.id === node.id;
                const dbg = debug[node.id];
                const warn = warnings[node.id];
                return (
                  <g key={node.id} transform={`translate(${node.x},${node.y})`} className={`flow-node is-${def.group}${isSel ? ' is-selected' : ''}${warn ? ' has-warn' : ''}`}>
                    <rect className="flow-node-box" width={NODE_W} height={NODE_H} rx="9"
                      onPointerDown={(e) => onPointerDownNode(e, node)}>
                      {warn ? <title>{warn}</title> : null}
                    </rect>
                    <g className="flow-node-content" pointerEvents="none">
                      <text className="flow-node-title" x="34" y="22">{t(def.labelKey || 'flows.node.unknown')}</text>
                      <text className="flow-node-sub" x="34" y="40">{clip(def.summary ? def.summary(node.config || {}) : '', 22)}</text>
                    </g>
                    {def.in ? <circle className="flow-port flow-port-in" cx="0" cy={NODE_H / 2} r="6"
                      onPointerUp={(e) => onPointerUpInPort(e, node)} onPointerDown={(e) => e.stopPropagation()} /> : null}
                    {def.out ? <circle className="flow-port flow-port-out" cx={NODE_W} cy={NODE_H / 2} r="6"
                      onPointerDown={(e) => onPointerDownOutPort(e, node)} /> : null}
                    {warn ? <circle className="flow-node-warn" cx={NODE_W - 12} cy="12" r="4" /> : null}
                    {dbg !== undefined ? <text className="flow-node-dbg" x={NODE_W - 22} y="22">{fmtDbg(dbg.payload)}</text> : null}
                  </g>
                );
              })}
            </g>
          </svg>

          {graph.nodes.length === 0 ? <div className="flow-canvas-empty">{t('flows.canvasEmpty')}</div> : null}
          <div className="flow-zoom">
            <button type="button" className="quiet" onClick={() => setView((v) => ({ ...v, zoom: Math.min(2.2, v.zoom * 1.1) }))}>+</button>
            <span>{Math.round(view.zoom * 100)}%</span>
            <button type="button" className="quiet" onClick={() => setView((v) => ({ ...v, zoom: Math.max(0.4, v.zoom / 1.1) }))}>−</button>
            <button type="button" className="quiet" title={t('flows.resetView')} onClick={() => setView({ x: 40, y: 20, zoom: 1 })}><Ico n="maximize" sz={13} /></button>
          </div>
        </div>

        <div className="flow-config">
          {selectedNode ? (
            <NodeConfig node={selectedNode} devices={devices} warning={warnings[selectedNode.id]} onChange={(patch) => updateNodeConfig(selectedNode.id, patch)} onDelete={deleteSelected} />
          ) : selected?.kind === 'wire' ? (
            <div className="flow-config-empty">
              <p>{t('flows.wireSelected')}</p>
              <button type="button" className="quiet danger-text" onClick={deleteSelected}><span className="btn-icon"><Ico n="trash" sz={14} /> {t('flows.deleteWire')}</span></button>
            </div>
          ) : (
            <div className="flow-config-empty"><p>{t('flows.selectHint')}</p></div>
          )}
        </div>
      </div>
    </section>
  );
}

// NodeConfig renders the fields for the selected node, driven by its type's field spec.
function NodeConfig({ node, devices, warning, onChange, onDelete }) {
  const t = useT();
  const def = NODE_DEFS[node.type] || { fields: [] };
  const cfg = node.config || {};
  return (
    <div className="flow-config-body">
      <div className="flow-config-head">
        <span className="btn-icon"><Ico n={def.icon} sz={15} /> {t(def.labelKey)}</span>
        <button type="button" className="quiet danger-text" onClick={onDelete} title={t('flows.deleteNode')}><Ico n="trash" sz={14} /></button>
      </div>
      {warning ? <p className="flow-config-warn"><Ico n="warning" sz={13} /> {warning}</p> : null}
      {def.fields.map((f) => (f.kind === 'bool' ? (
        <label key={f.key} className="check-row"><input type="checkbox" checked={!!cfg[f.key]} onChange={(e) => onChange({ [f.key]: e.target.checked })} /> {t(f.labelKey)}</label>
      ) : (
        <Field key={f.key} label={t(f.labelKey)} span hint={f.kind === 'device' ? t('flows.slotHint') : undefined}>
          {f.kind === 'code' ? (
            <textarea className="flow-code" rows={8} value={cfg[f.key] || ''} spellCheck={false}
              placeholder={'msg.payload = msg.payload * 2;\nreturn msg;'} onChange={(e) => onChange({ [f.key]: e.target.value })} />
          ) : f.kind === 'select' ? (
            <select value={cfg[f.key] ?? f.options[0]} onChange={(e) => onChange({ [f.key]: e.target.value })}>
              {f.options.map((o) => <option key={o} value={o}>{o}</option>)}
            </select>
          ) : f.kind === 'number' ? (
            <input type="number" step="any" value={cfg[f.key] ?? ''} onChange={(e) => onChange({ [f.key]: e.target.value === '' ? '' : Number(e.target.value) })} />
          ) : f.kind === 'device' ? (
            <input list={`flow-dev-${node.id}`} value={cfg[f.key] || ''} onChange={(e) => onChange({ [f.key]: e.target.value })} />
          ) : (
            <input value={cfg[f.key] || ''} onChange={(e) => onChange({ [f.key]: e.target.value })} />
          )}
        </Field>
      )))}
      <datalist id={`flow-dev-${node.id}`}>
        {devices.map((d) => <option key={d.id} value={d.deviceKey}>{d.name}</option>)}
      </datalist>
    </div>
  );
}

// wirePath draws a horizontal cubic bezier between two points, the Node-RED wire look.
function wirePath(x1, y1, x2, y2) {
  const dx = Math.max(40, Math.abs(x2 - x1) * 0.5);
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}
function clip(s, n) { s = String(s || ''); return s.length > n ? s.slice(0, n - 1) + '…' : s; }
function fmtDbg(v) {
  if (v === undefined || v === null) return '';
  if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(2);
  return clip(String(v), 8);
}
