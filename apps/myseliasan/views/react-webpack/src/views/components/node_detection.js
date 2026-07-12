import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@shared';
import { api, apiBase } from '../lib/helpers';
import { NodeFeatureBanner } from './node_embed';
import { CameraAiPanel } from './nodecam/vision';
import {
  setNodeProxyBase,
  defaultVisionRuleDraft,
  defaultZonePolygon,
  isLineDetectionType,
  defaultLineRuleConfig,
  lineRuleConfigText,
} from './nodecam/lib/helpers';
import { defaultVisionThreshold, defaultVisionMinFrames } from './nodecam/lib/constants';

// NodeDetectionTab hosts the real mymatasan per-camera AI panel (CameraAiPanel) against
// a managed node. The panel and its zone/line editors were written for the node's own
// same-origin API; this wrapper (a) points their internal GETs (snapshots/frames) at the
// commander proxy via setNodeProxyBase, and (b) supplies the data + mutation handlers the
// panel expects — replicating the node App's vision orchestration, but routed over the
// tunnel with myseliasan's CSRF-aware api() so writes are accepted.
export function NodeDetectionTab({ node, camera, onToast }) {
  const t = useT();
  const nodeId = node.nodeId;

  // Point the copied components' apiBase() at this node's proxy before they render, so
  // `${apiBase()}/api/...` resolves to `${origin}/api/nodes/{id}/proxy/api/...`.
  setNodeProxyBase(`${apiBase()}/api/nodes/${encodeURIComponent(nodeId)}/proxy`);

  const [rules, setRules] = useState([]);
  const [alerts, setAlerts] = useState([]);
  const [classes, setClasses] = useState([]);
  const [activeModelClasses, setActiveModelClasses] = useState([]);
  const [taughtByRuleId, setTaughtByRuleId] = useState({});
  const [destinations, setDestinations] = useState([]);
  const [ruleDraft, setRuleDraft] = useState(() => defaultVisionRuleDraft(camera?.id));
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);

  const onToastRef = useRef(onToast);
  useEffect(() => { onToastRef.current = onToast; }, [onToast]);
  const toast = useCallback((m) => { if (onToastRef.current) onToastRef.current(m); }, []);

  const px = useCallback(
    (path, opts = {}) => api(`/api/nodes/${encodeURIComponent(nodeId)}/proxy${path}`, { noRedirect: true, ...opts }).catch(() => ({ ok: false })),
    [nodeId],
  );
  const listOf = (r) => (r.ok ? (Array.isArray(r.body) ? r.body : (r.body?.items || [])) : []);

  const load = useCallback(async () => {
    const [r, a, c] = await Promise.all([
      px('/api/vision/rules?limit=100&offset=0'),
      px('/api/vision/alerts?status=detections&limit=100&offset=0'),
      px('/api/vision/classes'),
    ]);
    // A 404 on the core endpoint means the node predates this feature — surface it.
    setUnavailable(r.status === 404);
    setRules(listOf(r));
    setAlerts(listOf(a));
    setClasses(listOf(c));
    // Supplementary, best-effort (mirror the node app): active model's class labels,
    // taught-skill rule map, and notification destinations.
    px('/api/training/models').then((m) => {
      if (!m.ok) return;
      const items = Array.isArray(m.body) ? m.body : (m.body?.items || []);
      const active = items.find((x) => x.isActive);
      let cls = [];
      if (active) { try { cls = JSON.parse(active.classes || '[]'); } catch (_) { cls = []; } }
      setActiveModelClasses(cls.map((x) => String(x).toLowerCase()));
    });
    px('/api/teach/skills').then((s) => {
      if (!s.ok) return;
      const items = Array.isArray(s.body) ? s.body : (s.body?.items || []);
      const map = {};
      items.forEach((sk) => {
        let cfg = {};
        try { cfg = JSON.parse(sk.config || '{}'); } catch (_) { cfg = {}; }
        if (cfg.ruleId) map[cfg.ruleId] = { name: sk.name, skillType: sk.skillType };
      });
      setTaughtByRuleId(map);
    });
    px('/api/settings/notification/destination').then((d) => {
      if (!d.ok) return;
      const dd = d.body?.destinations || d.body?.settings?.destinations || [];
      setDestinations(Array.isArray(dd) ? dd : []);
    });
  }, [px]);
  useEffect(() => { load(); }, [load]);

  // visionRuleToDraft mirrors the node App: maps a listed rule into the typed, complete
  // draft the upsert endpoint (POST /api/vision/rules) expects — used by both edit and
  // the in-list enable/disable toggle so each posts a valid body.
  const visionRuleToDraft = (rule) => ({
    id: rule.id,
    cameraId: rule.cameraId || '',
    name: rule.name || '',
    detectionType: rule.detectionType || 'presence',
    zonePolygon: rule.zonePolygon || defaultZonePolygon,
    ruleConfig: rule.ruleConfig || (isLineDetectionType(rule.detectionType) ? lineRuleConfigText(defaultLineRuleConfig(rule.detectionType), rule.detectionType) : ''),
    schedulePolicy: rule.schedulePolicy || '',
    threshold: rule.threshold || defaultVisionThreshold,
    minFrames: rule.minFrames || defaultVisionMinFrames,
    cooldownSeconds: rule.cooldownSeconds || 30,
    soundEnabled: Boolean(rule.soundEnabled),
    isEnabled: Boolean(rule.isEnabled),
  });

  async function onSaveRule(event) {
    if (event && event.preventDefault) event.preventDefault();
    setBusy(true);
    const r = await px('/api/vision/rules', { method: 'POST', body: JSON.stringify(ruleDraft) });
    setBusy(false);
    if (r.ok) { setRuleDraft(defaultVisionRuleDraft(ruleDraft.cameraId || camera?.id)); load(); toast(t('app.ruleSaved')); }
    else toast(t('app.ruleSaveFailed'));
  }

  function onEditRule(rule) { setRuleDraft(visionRuleToDraft(rule)); }

  async function onDeleteRule(id) {
    setBusy(true);
    const r = await px(`/api/vision/rules/${id}`, { method: 'DELETE' });
    setBusy(false);
    if (r.ok) { load(); toast(t('app.ruleDeleted')); }
  }

  async function onToggleRule(rule) {
    setBusy(true);
    const r = await px('/api/vision/rules', { method: 'POST', body: JSON.stringify({ ...visionRuleToDraft(rule), isEnabled: !rule.isEnabled }) });
    setBusy(false);
    if (r.ok) load();
  }

  async function onTriggerTestAlert(rule) {
    setBusy(true);
    const r = await px('/api/vision/alerts', {
      method: 'POST',
      body: JSON.stringify({
        ruleId: rule.id,
        cameraId: rule.cameraId,
        detectionType: rule.detectionType,
        label: `Test ${rule.detectionType}`,
        confidence: Math.max(0.01, Math.min(1, rule.threshold || defaultVisionThreshold)),
        zonePolygon: rule.zonePolygon,
        metadata: JSON.stringify({ source: 'manual-test' }),
      }),
    });
    setBusy(false);
    if (r.ok && r.body) { setAlerts((cur) => [r.body, ...cur]); toast(t('app.testAlertCreated')); }
  }

  async function onAcknowledgeAlert(id) {
    setAlerts((cur) => cur.map((a) => (Number(a.id) === Number(id) ? { ...a, isAcknowledged: true } : a)));
    setBusy(true);
    const r = await px(`/api/vision/alerts/${id}/ack`, { method: 'POST' });
    setBusy(false);
    if (r.ok) load(); else load();
  }

  if (unavailable) return <NodeFeatureBanner version={node.version} />;

  // The camera page wraps all panels in one scoped-CSS embed, so this renders the raw
  // mymatasan panel (styled by mymatasan's stylesheet under `.nodecam-embed`).
  return (
    <CameraAiPanel
      camera={camera}
      rules={rules}
      alerts={alerts}
      classes={classes}
      activeModelClasses={activeModelClasses}
      taughtByRuleId={taughtByRuleId}
      destinations={destinations}
      ruleDraft={ruleDraft}
      busy={busy}
      authHeader={undefined}
      streamConfig={null}
      onRuleDraft={setRuleDraft}
      onSaveRule={onSaveRule}
      onEditRule={onEditRule}
      onDeleteRule={onDeleteRule}
      onToggleRule={onToggleRule}
      onTriggerTestAlert={onTriggerTestAlert}
      onAcknowledgeAlert={onAcknowledgeAlert}
      onPrepareCamera={() => Promise.resolve()}
      onReload={load}
    />
  );
}
