import { useCallback, useEffect, useRef, useState } from 'react';
import { Ico, Tabs, useT } from '@shared';
import { api, apiBase, csrfToken } from '../lib/helpers';
import { NodeCameraTree } from './node_camera_tree';
import { NodeEmbed } from './node_embed';
import { TeachTab } from './nodecam/teach';
import { setNodeProxyBase, installProxyCsrf } from './nodecam/lib/helpers';

// The copied teach wizard issues its own raw fetch() writes; teach window.fetch to attach
// the CSRF token on proxy writes so they pass myseliasan's auth gate. Idempotent.
installProxyCsrf(csrfToken);

// TeachPage is the control plane's fleet Teach surface. Because teaching (capture →
// train → activate) runs entirely ON a node, the flow is "Where-first": the operator
// picks the target camera via a Node→Camera TREE dropdown, and the real mymatasan Teach
// wizard (copied under nodecam/) then runs against that camera's node over the tunnel.
export function TeachPage({ nodes = [], onToast }) {
  const t = useT();
  const [tab, setTab] = useState('teach');
  const [sel, setSel] = useState(null); // { nodeId, nodeName, cameraId, cameraName }

  return (
    <section className="workspace teach-page">
      <Tabs
        ariaLabel={t('teach.tabsAria')}
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'teach', label: t('teach.tabTeach'), icon: 'wand' },
          { id: 'share', label: t('teach.tabShare'), icon: 'download' },
        ]}
      />
      {tab === 'teach' ? (
        <>
          <section className="settings-panel span-two">
            <header><h2><span className="btn-icon"><Ico n="wand" /> {t('teach.title')}</span></h2></header>
            <p className="settings-hint">{t('teach.wherePickHint')}</p>
            <label className="teach-where">
              <span>{t('teach.stepWhere')}</span>
              <NodeCameraTree nodes={nodes} value={sel} onChange={setSel} />
            </label>
          </section>
          {sel ? (
            <NodeTeach key={`${sel.nodeId}:${sel.cameraId}`} nodeId={sel.nodeId} cameraId={sel.cameraId} onToast={onToast} />
          ) : (
            <p className="settings-hint">{t('teach.pickToStart')}</p>
          )}
        </>
      ) : (
        <SkillPush nodes={nodes} onToast={onToast} />
      )}
    </section>
  );
}

// SkillPush shares a trained skill across the fleet: it exports a skill from a source node
// (as an encrypted .mmskill package, over the proxy) and imports it into every selected
// target node. Because both the export payload and each import travel through the control
// tunnel (frame/body caps ~8–16 MB), it defaults to model-only (no images) — the practical
// way to fan a finished skill out to many nodes.
function SkillPush({ nodes = [], onToast }) {
  const t = useT();
  const nodeList = nodes;
  const [srcNode, setSrcNode] = useState(nodeList[0]?.nodeId || '');
  const [skills, setSkills] = useState([]);
  const [skillId, setSkillId] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [includeImages, setIncludeImages] = useState(false);
  const [targets, setTargets] = useState([]);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState(null);

  const px = useCallback(
    (nid, path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nid)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [],
  );
  const nodeName = useCallback((nid) => nodeList.find((n) => n.nodeId === nid)?.name || nid, [nodeList]);

  useEffect(() => {
    if (!srcNode) { setSkills([]); return undefined; }
    let cancelled = false;
    (async () => {
      const r = await px(srcNode, '/api/teach/skills');
      if (cancelled) return;
      setSkills(r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);
      setSkillId('');
    })();
    return () => { cancelled = true; };
  }, [srcNode, px]);

  const toggleTarget = (nid) => setTargets((ts) => (ts.includes(nid) ? ts.filter((x) => x !== nid) : [...ts, nid]));

  async function push() {
    if (!srcNode || !skillId) { if (onToast) onToast(t('teach.pushPickSkill')); return; }
    if (passphrase.trim().length < 8) { if (onToast) onToast(t('teach.pkgPassTooShort')); return; }
    if (targets.length === 0) { if (onToast) onToast(t('teach.pushPickTargets')); return; }
    setBusy(true);
    setResults(null);
    const ex = await px(srcNode, `/api/teach/skills/${skillId}/export`, { method: 'POST', body: JSON.stringify({ passphrase: passphrase.trim(), includeImages }) });
    if (!ex.ok || !ex.body?.dataBase64) { setBusy(false); if (onToast) onToast(ex.message || t('teach.pushExportFailed')); return; }
    const dataBase64 = ex.body.dataBase64;
    const res = [];
    for (const nid of targets) {
      const im = await px(nid, '/api/teach/import', { method: 'POST', body: JSON.stringify({ dataBase64, passphrase: passphrase.trim() }) });
      res.push({ nodeId: nid, ok: im.ok, message: im.message });
    }
    setResults(res);
    setBusy(false);
    if (onToast) onToast(t('teach.pushDone', { ok: res.filter((r) => r.ok).length, total: res.length }));
  }

  return (
    <section className="settings-panel span-two teach-push">
      <header>
        <h2><span className="btn-icon"><Ico n="download" /> {t('teach.pushTitle')}</span></h2>
      </header>
      <>
          <p className="settings-hint">{t('teach.pushHint')}</p>
          <div className="settings-field-grid">
            <label>{t('teach.pushSource')}
              <select value={srcNode} onChange={(e) => setSrcNode(e.target.value)} disabled={busy}>
                {nodeList.map((n) => <option key={n.nodeId} value={n.nodeId}>{n.name || n.nodeId}</option>)}
              </select>
            </label>
            <label>{t('teach.pushSkill')}
              <select value={skillId} onChange={(e) => setSkillId(e.target.value)} disabled={busy}>
                <option value="">{t('teach.pushPickSkillOpt')}</option>
                {skills.map((s) => <option key={s.id} value={s.id}>{s.name || `#${s.id}`}</option>)}
              </select>
            </label>
            <label>{t('teach.pkgPassphrase')}<input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} placeholder={t('teach.pkgPassphrasePh')} disabled={busy} /></label>
            <label className="check-row"><input type="checkbox" checked={includeImages} onChange={(e) => setIncludeImages(e.target.checked)} disabled={busy} /> {t('teach.pkgIncludeImages')}</label>
          </div>
          <div className="teach-push-targets">
            <span className="settings-hint">{t('teach.pushTargets')}</span>
            <div className="teach-push-target-list">
              {nodeList.filter((n) => n.nodeId !== srcNode).map((n) => (
                <label key={n.nodeId} className="check-row"><input type="checkbox" checked={targets.includes(n.nodeId)} onChange={() => toggleTarget(n.nodeId)} disabled={busy} /> {n.name || n.nodeId}</label>
              ))}
            </div>
          </div>
          <div className="settings-actions">
            <button type="button" onClick={push} disabled={busy}><span className="btn-icon"><Ico n="download" /> {busy ? t('teach.pushing') : t('teach.pushBtn')}</span></button>
          </div>
          {results ? (
            <ul className="teach-push-results">
              {results.map((r) => <li key={r.nodeId} className={r.ok ? 'ok' : 'err'}>{nodeName(r.nodeId)}: {r.ok ? t('teach.pushOk') : (r.message || t('teach.pushFail'))}</li>)}
            </ul>
          ) : null}
      </>
    </section>
  );
}

// NodeTeach renders the copied mymatasan Teach wizard against ONE node over the proxy. It
// points the copied component's apiBase() at the node's command tunnel (setNodeProxyBase),
// loads the node's cameras + label catalog, and scopes the camera list to the tree-selected
// camera so the wizard's own "Where" step is pre-narrowed. Writes go through the CSRF-safe
// proxy; reads use cookie auth (authHeader undefined).
function NodeTeach({ nodeId, cameraId, onToast }) {
  const t = useT();
  // Point the copied components' apiBase() at this node's proxy before they render.
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);

  const [cameras, setCameras] = useState([]);
  const [labelCatalog, setLabelCatalog] = useState([]);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );
  const listOf = (r) => (r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const [c, l] = await Promise.all([px('/api/cameras?limit=200'), px('/api/vision/labels')]);
      if (cancelled) return;
      setCameras(listOf(c));
      setLabelCatalog(listOf(l));
    })();
    return () => { cancelled = true; };
  }, [px]);

  const onToastRef = useRef(onToast);
  useEffect(() => { onToastRef.current = onToast; }, [onToast]);
  const onMessage = useCallback((m) => { if (onToastRef.current) onToastRef.current(m); }, []);

  // Scope the wizard's "Where" list to the tree-selected camera (fall back to all node
  // cameras while the list is still loading).
  const scoped = cameras.filter((c) => String(c.id) === String(cameraId));
  const wizardCameras = scoped.length ? scoped : cameras;

  return (
    <NodeEmbed className="node-teach-embed">
      <TeachTab
        authHeader={undefined}
        onMessage={onMessage}
        cameras={wizardCameras}
        streamConfig={null}
        labelCatalog={labelCatalog}
        onOpenModels={() => { if (onToastRef.current) onToastRef.current(t('teach.modelsOnNode')); }}
      />
    </NodeEmbed>
  );
}
