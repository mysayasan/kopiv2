import { useEffect, useState } from 'react';
import { Ico, DataTable, useT } from '@shared';
import { FormBusyOverlay } from './ui';
import { api } from '../lib/helpers';

// The myseliasan admin surface, split into the same menu structure myidsan uses —
// Users, Roles, and the RBAC permission matrix — each built on the shared filterable
// /sortable DataTable. myseliasan has no Groups/Endpoints catalog, so those myidsan
// sections do not apply here.

// --- Users ---------------------------------------------------------------------
export function UsersPage({ session, onToast, onSessionChanged }) {
  const t = useT();
  const [users, setUsers] = useState([]);
  const [roles, setRoles] = useState([]);
  const [busy, setBusy] = useState(false);

  function toast(msg, kind = 'info') { if (onToast) onToast(msg, kind); }

  async function load() {
    const [u, r] = await Promise.all([
      api('/api/rbac/users', { noRedirect: true }).catch(() => ({ ok: false })),
      api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    if (u.ok) setUsers(Array.isArray(u.body) ? u.body : []);
    if (r.ok) setRoles(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, []);

  function isSuperRole(id) { const r = roles.find((x) => x.id === id); return !!(r && r.isSuperadmin); }

  async function setUserRole(user, roleId) {
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/role`, { method: 'POST', noRedirect: true, body: JSON.stringify({ roleId: Number(roleId) }) });
    setBusy(false);
    if (r.ok) {
      const id = Number(roleId);
      const roleName = !id ? t('rb.noRolePending') : (roles.find((x) => Number(x.id) === id)?.name || `role ${id}`);
      const who = user.email || user.name || `user ${user.id}`;
      toast(t('rb.roleSetTo', { who, role: roleName }), 'success');
      load();
    } else toast(r.message || t('rb.failedRole'), 'error');
  }
  async function toggleDisabled(user) {
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/disabled`, { method: 'POST', noRedirect: true, body: JSON.stringify({ disabled: !user.disabled }) });
    setBusy(false);
    if (r.ok) load(); else toast(r.message || t('rb.failed'));
  }
  async function elevate(user) {
    const label = user.email || user.name || user.username || `user ${user.id}`;
    if (!window.confirm(t('rb.confirmSuperadmin', { label }))) return;
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/elevate`, { method: 'POST', noRedirect: true });
    setBusy(false);
    if (r.ok) {
      toast((r.body && r.body.warning) || t('rb.superadminGranted'));
      load();
      if (onSessionChanged) onSessionChanged();
    } else toast(r.message || t('rb.handoffFailed'));
  }

  const columns = [
    { key: 'id', label: t('rb.colId') },
    {
      key: 'email',
      label: t('rb.colUser'),
      render: (_v, u) => (
        <>
          {u.email || u.username || u.name || `#${u.id}`}
          {u.isStock ? <span className="status-pill warn" style={{ marginLeft: 6 }}>{t('rb.stock')}</span> : null}
        </>
      ),
    },
    { key: 'kind', label: t('rb.colKind') },
    {
      key: 'roleId',
      label: t('rb.colRole'),
      filterable: false,
      render: (_v, u) => (
        <select value={u.roleId || 0} disabled={u.isStock} onChange={(e) => setUserRole(u, e.target.value)}>
          {/* value 0 = no role yet (pending). Without this option a role-less user would
              render as the first role, making them look like a superadmin. */}
          <option value={0}>{t('rb.noRolePendingOpt')}</option>
          {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      ),
    },
    { key: 'disabled', label: t('rb.colStatus'), render: (v) => <span className={`status-pill ${v ? 'offline' : 'online'}`}>{v ? t('rb.statusDisabled') : t('rb.statusActive')}</span> },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_v, u) => {
        const self = session && session.userId === u.id;
        return (
          <div className="node-row-actions">
            {!u.isStock && u.kind === 'federated' && !isSuperRole(u.roleId)
              ? <button type="button" className="quiet" onClick={() => elevate(u)} title={t('rb.bootstrapHandoff')}>{t('rb.makeSuperadmin')}</button> : null}
            {!self && (!u.isStock || session?.superadminHandoffPending)
              ? <button type="button" className="quiet danger-text" onClick={() => toggleDisabled(u)}>{u.disabled ? t('rb.enable') : t('rb.disable')}</button> : null}
          </div>
        );
      },
    },
  ];

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="user" /> {t('rb.users')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load}><span className="btn-icon"><Ico n="reload" /> {t('rb.refresh')}</span></button>
          </div>
        </header>
        <p className="settings-hint">{t('rb.usersHint')}</p>
        <DataTable rows={users} columns={columns} pageSize={5} emptyText={t('rb.noUsers')} />
      </section>
    </section>
  );
}

// --- Roles & Access (merged roles + permission matrix) -------------------------
const VERBS = [['canGet', 'GET'], ['canPost', 'POST'], ['canPut', 'PUT'], ['canDelete', 'DELETE']];

// Curated, friendly access toggles so an operator grants a role by FEATURE instead of
// typing raw API paths. Each maps to a path + the verb(s) it grants; toggling merges those
// verbs into that path's permission row. The raw path+verb matrix stays available under
// "Advanced" for edge cases. (myseliasan's Users/Roles admin surfaces are superadmin-only,
// so they're intentionally NOT here — only the operational surfaces are delegatable.)
const ACCESS_FEATURES = [
  { key: 'nodesView', labelKey: 'rb.featNodesView', hintKey: 'rb.featNodesViewHint', path: '/api/nodes', verbs: ['canGet'] },
  { key: 'nodesManage', labelKey: 'rb.featNodesManage', hintKey: 'rb.featNodesManageHint', path: '/api/nodes', verbs: ['canPost', 'canPut', 'canDelete'] },
  { key: 'notifications', labelKey: 'rb.featNotifications', hintKey: 'rb.featNotificationsHint', path: '/api/notifications', verbs: ['canGet', 'canPost'] },
];

// "Viewer" default seeded onto every brand-new role: read-only on the operational
// surfaces (the fleet + the notifications feed) plus viewer access on every node that
// exists right now. Deliberately NOT a GET-everything wildcard on /api — that would
// re-expose the superadmin-gated admin surfaces the shared viewer role was hardened
// against (see EnsureViewerDefaults). Node grants are a snapshot; nodes adopted later
// are granted per-role manually in the matrix below.
const VIEWER_DEFAULT_PATHS = ['/api/nodes', '/api/notifications'];

// RolesAccessPage consolidates what used to be two separate admin pages — a Roles list
// and the RBAC permission matrix — into one surface. Adding a role immediately seeds it
// with the viewer default and selects it, so "add role" flows straight into editing its
// permissions instead of leaving a permission-less role behind.
export function RolesAccessPage({ onToast }) {
  const t = useT();
  const [roles, setRoles] = useState([]);
  const [roleId, setRoleId] = useState(0);
  const [perms, setPerms] = useState([]);
  const [path, setPath] = useState('/api');
  const [busy, setBusy] = useState(false);
  const [nodes, setNodes] = useState([]);
  const [nodeGrants, setNodeGrants] = useState([]);
  const [newName, setNewName] = useState('');
  const [showAdvanced, setShowAdvanced] = useState(false);

  function toast(msg, kind = 'info') { if (onToast) onToast(msg, kind); }

  // loadRoles refreshes the list and keeps a sensible selection: prefer an explicitly
  // requested id (e.g. a just-created role), else the current one, else the first
  // non-superadmin role (superadmin has nothing to edit — it bypasses the matrix).
  async function loadRoles(selectId) {
    const r = await api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false }));
    const list = r.ok && Array.isArray(r.body) ? r.body : [];
    setRoles(list);
    setRoleId((prev) => {
      if (selectId && list.find((x) => x.id === selectId)) return selectId;
      if (prev && list.find((x) => x.id === prev)) return prev;
      return list.find((x) => !x.isSuperadmin)?.id ?? list[0]?.id ?? 0;
    });
  }
  async function loadPerms(rid) {
    if (!rid) { setPerms([]); return; }
    const r = await api(`/api/access-rbac/permissions?roleId=${rid}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setPerms(Array.isArray(r.body) ? r.body : []);
  }
  async function loadNodes() {
    const r = await api('/api/nodes', { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setNodes(Array.isArray(r.body) ? r.body : []);
  }
  async function loadNodeGrants(rid) {
    if (!rid) { setNodeGrants([]); return; }
    const r = await api(`/api/nodes/access?roleId=${rid}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setNodeGrants(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { loadRoles(); loadNodes(); /* eslint-disable-next-line */ }, []);
  useEffect(() => { loadPerms(roleId); loadNodeGrants(roleId); /* eslint-disable-next-line */ }, [roleId]);

  // --- role CRUD ---------------------------------------------------------------
  // seedViewerDefaults grants the viewer preset to a freshly-created role: GET on the
  // operational paths + viewer (read-only) access on every currently-adopted node.
  // Best-effort per call (a single failed grant shouldn't abort the rest); the matrix
  // reload afterwards reflects whatever landed.
  async function seedViewerDefaults(rid) {
    for (const p of VIEWER_DEFAULT_PATHS) {
      await api('/api/access-rbac/permissions', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({ roleId: rid, path: p, canGet: true, canPost: false, canPut: false, canDelete: false }),
      }).catch(() => {});
    }
    for (const n of nodes) {
      await api('/api/nodes/access', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({ roleId: rid, nodeId: n.nodeId, canRead: true, canWrite: false }),
      }).catch(() => {});
    }
  }
  async function createRole() {
    if (!newName.trim()) { toast(t('rb.enterRoleName')); return; }
    setBusy(true);
    const r = await api('/api/access-rbac/roles', { method: 'POST', noRedirect: true, body: JSON.stringify({ name: newName.trim() }) });
    if (!r.ok) { setBusy(false); toast(r.message || t('rb.failedCreateRole'), 'error'); return; }
    const created = r.body || {};
    await seedViewerDefaults(created.id);
    setNewName('');
    await loadRoles(created.id);
    setBusy(false);
    toast(t('rb.viewerDefaultsApplied', { name: created.name || newName.trim() }), 'success');
  }
  async function renameRole(role) {
    const name = window.prompt(t('rb.renameRolePrompt', { name: role.name }), role.name);
    if (name === null) return;
    if (!name.trim()) { toast(t('rb.enterRoleName')); return; }
    setBusy(true);
    const r = await api(`/api/access-rbac/roles/${role.id}`, { method: 'PUT', noRedirect: true, body: JSON.stringify({ name: name.trim(), description: role.description || '' }) });
    setBusy(false);
    if (r.ok) { toast(t('rb.roleRenamed'), 'success'); loadRoles(role.id); } else toast(r.message || t('rb.failedRenameRole'), 'error');
  }
  async function deleteRole(role) {
    if (!window.confirm(t('rb.confirmDeleteRole', { name: role.name }))) return;
    setBusy(true);
    const r = await api(`/api/access-rbac/roles/${role.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) { if (Number(roleId) === role.id) setRoleId(0); loadRoles(); } else toast(r.message || t('rb.failedDeleteRole'));
  }
  // copyRole duplicates a role into a new one: it creates the role, then replays the
  // source role's API permission matrix AND its per-node access grants onto the copy.
  // (Owner status isn't copied — it belongs to the role that adopted a node.)
  async function copyRole(role) {
    const name = window.prompt(t('rb.copyRolePrompt', { name: role.name }), t('rb.copyDefaultName', { name: role.name }));
    if (name === null) return;
    if (!name.trim()) { toast(t('rb.enterRoleName')); return; }
    setBusy(true);
    const r = await api('/api/access-rbac/roles', { method: 'POST', noRedirect: true, body: JSON.stringify({ name: name.trim(), description: role.description || '' }) });
    if (!r.ok) { setBusy(false); toast(r.message || t('rb.failedCreateRole'), 'error'); return; }
    const created = r.body || {};
    const pr = await api(`/api/access-rbac/permissions?roleId=${role.id}`, { noRedirect: true }).catch(() => ({ ok: false }));
    for (const p of (pr.ok && Array.isArray(pr.body) ? pr.body : [])) {
      await api('/api/access-rbac/permissions', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({ roleId: created.id, path: p.path, canGet: !!p.canGet, canPost: !!p.canPost, canPut: !!p.canPut, canDelete: !!p.canDelete }),
      }).catch(() => {});
    }
    const gr = await api(`/api/nodes/access?roleId=${role.id}`, { noRedirect: true }).catch(() => ({ ok: false }));
    for (const g of (gr.ok && Array.isArray(gr.body) ? gr.body : [])) {
      await api('/api/nodes/access', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({ roleId: created.id, nodeId: g.nodeId, canRead: !!g.canRead, canWrite: !!g.canWrite }),
      }).catch(() => {});
    }
    await loadRoles(created.id);
    setBusy(false);
    toast(t('rb.roleCopied', { from: role.name, to: created.name || name.trim() }), 'success');
  }

  const roleColumns = [
    { key: 'id', label: t('rb.colId') },
    {
      key: 'name',
      label: t('rb.colRole'),
      render: (v, r) => (
        <>
          {v}
          {r.isSuperadmin ? <span className="status-pill online" style={{ marginLeft: 6 }}>{t('rb.superadminPill')}</span> : null}
          {r.id === Number(roleId) ? <span className="status-pill" style={{ marginLeft: 6 }}>{t('rb.editing')}</span> : null}
        </>
      ),
    },
    { key: 'builtin', label: t('rb.colType'), render: (v) => (v ? t('rb.typeBuiltin') : t('rb.typeCustom')) },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_v, r) => (
        <div className="node-row-actions">
          <button type="button" className="quiet" onClick={() => setRoleId(r.id)}>{t('rb.editAccess')}</button>
          <button type="button" className="quiet" onClick={() => copyRole(r)}>{t('rb.copy')}</button>
          {!r.builtin && !r.isSuperadmin ? <button type="button" className="quiet" onClick={() => renameRole(r)}>{t('rb.rename')}</button> : null}
          {!r.builtin ? <button type="button" className="quiet danger-text" onClick={() => deleteRole(r)}>{t('rb.delete')}</button> : null}
        </div>
      ),
    },
  ];

  const selectedRole = roles.find((x) => x.id === Number(roleId));

  // Resolve the role's current access level on the node DEVICE (over the tunnel): owner
  // (implicit full), admin, operator, viewer, or none. This is separate from the
  // /api/nodes/* path matrix above — that gates myseliasan's OWN endpoints.
  //
  // The three levels are exactly mymatasan's three local roles, because that is what the
  // tunnel asserts: the level names a ROLE, and the node evaluates its own matrix for it.
  function nodeLevel(node) {
    if (node.ownerRoleId && node.ownerRoleId === Number(roleId)) return 'owner';
    const g = nodeGrants.find((x) => x.nodeId === node.nodeId);
    if (!g) return 'none';
    if (g.canWrite) return 'admin';
    if (g.canOperate) return 'operator';
    if (g.canRead) return 'viewer';
    return 'none';
  }
  // Set the role's access level on a node. "none" removes any grant; the rest upsert one.
  //
  // The flags are an escalation ladder (admin implies operator implies viewer) and the
  // server normalises them, so only the top rung of the chosen level has to be sent.
  async function setNodeLevel(node, level) {
    setBusy(true);
    let r;
    if (level === 'none') {
      const g = nodeGrants.find((x) => x.nodeId === node.nodeId);
      r = g ? await api(`/api/nodes/access/${g.id}`, { method: 'DELETE', noRedirect: true }) : { ok: true };
    } else {
      r = await api('/api/nodes/access', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({
          roleId: Number(roleId),
          nodeId: node.nodeId,
          canRead: true,
          canOperate: level === 'operator' || level === 'admin',
          canWrite: level === 'admin',
        }),
      });
    }
    setBusy(false);
    if (r.ok) loadNodeGrants(roleId); else toast(r.message || t('rb.failedNodeAccess'));
  }

  async function save(p) {
    setBusy(true);
    const body = { roleId: Number(roleId), path: p.path, canGet: !!p.canGet, canPost: !!p.canPost, canPut: !!p.canPut, canDelete: !!p.canDelete };
    const r = await api('/api/access-rbac/permissions', { method: 'POST', noRedirect: true, body: JSON.stringify(body) });
    setBusy(false);
    if (r.ok) loadPerms(roleId); else toast(r.message || t('rb.failedSavePerm'));
  }
  async function remove(p) {
    setBusy(true);
    const r = await api(`/api/access-rbac/permissions/${p.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) loadPerms(roleId); else toast(r.message || t('rb.failed'));
  }
  function toggle(p, key) {
    // Optimistic: flip this row's cell immediately (server reload confirms; the stable
    // path-sorted list keeps rows from reshuffling under the click).
    setPerms((cur) => cur.map((row) => (row.id === p.id ? { ...row, [key]: !row[key] } : row)));
    save({ ...p, [key]: !p[key] });
  }
  function addPath() {
    if (!path.trim().startsWith('/')) { toast(t('nm.pathStartSlash')); return; }
    save({ roleId: Number(roleId), path: path.trim(), canGet: true, canPost: false, canPut: false, canDelete: false });
  }
  // featureOn reports whether the role currently has ALL of a feature's verbs on its path.
  function featureOn(feature) {
    const row = perms.find((p) => p.path === feature.path);
    return !!row && feature.verbs.every((v) => row[v]);
  }
  // toggleFeature flips a feature's verbs on its path, merging with whatever else that path
  // already grants (so "View fleet" and "Manage fleet" share the /api/nodes row cleanly).
  // If the row ends up with no verbs at all it is removed rather than left empty.
  function toggleFeature(feature) {
    const existing = perms.find((p) => p.path === feature.path);
    const base = existing || { canGet: false, canPost: false, canPut: false, canDelete: false };
    const on = feature.verbs.every((v) => base[v]);
    const next = { ...base };
    feature.verbs.forEach((v) => { next[v] = !on; });
    if (existing && existing.id && !(next.canGet || next.canPost || next.canPut || next.canDelete)) {
      remove(existing);
      return;
    }
    save({ roleId: Number(roleId), path: feature.path, canGet: !!next.canGet, canPost: !!next.canPost, canPut: !!next.canPut, canDelete: !!next.canDelete });
  }

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header>
          <h2><span className="btn-icon"><Ico n="key" /> {t('rb.roles')}</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={() => loadRoles()}><span className="btn-icon"><Ico n="reload" /> {t('rb.refresh')}</span></button>
          </div>
        </header>
        <p className="settings-hint">{t('rb.rolesAccessHint')}</p>
        <div className="table-toolbar">
          <input value={newName} onChange={(e) => setNewName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') createRole(); }} placeholder={t('rb.newRoleName')} style={{ maxWidth: 260 }} disabled={busy} />
          <button type="button" onClick={createRole} disabled={busy}><span className="btn-icon"><Ico n="plus" /> {t('rb.addRole')}</span></button>
        </div>
        <p className="settings-hint">{t('rb.addRoleDefaultsHint')}</p>
        <DataTable rows={roles} columns={roleColumns} pageSize={5} emptyText={t('rb.noRoles')} />
      </section>

      <section className="settings-panel span-two rbac-permissions">
        <header><h2><span className="btn-icon"><Ico n="lock" /> {t('rb.permissions')}{selectedRole ? ` — ${selectedRole.name}` : ''}</span></h2></header>
        <p className="settings-hint">{t('rb.rbacHint')}</p>
        {!selectedRole ? (
          <p className="settings-hint">{t('rb.selectRoleHint')}</p>
        ) : selectedRole.isSuperadmin ? (
          <p className="settings-hint">{t('rb.superadminBypass')}</p>
        ) : (
          <>
            <div className="menu-access">
              <h3>{t('rb.accessTitle')}</h3>
              <p className="settings-hint">{t('rb.accessHint')}</p>
              <div className="access-feature-list">
                {ACCESS_FEATURES.map((f) => (
                  <label className="access-feature" key={f.key}>
                    <input type="checkbox" checked={featureOn(f)} onChange={() => toggleFeature(f)} disabled={busy} />
                    <span className="access-feature-text">
                      <span className="access-feature-label">{t(f.labelKey)}</span>
                      <span className="access-feature-hint">{t(f.hintKey)}</span>
                    </span>
                  </label>
                ))}
              </div>
            </div>

            {/* Advanced: raw path + verb matrix, for grants the friendly toggles don't cover. */}
            <div className="rbac-advanced">
              <button type="button" className="quiet rbac-advanced-toggle" onClick={() => setShowAdvanced((s) => !s)} aria-expanded={showAdvanced}>
                <Ico n="chev-down" sz={12} style={showAdvanced ? undefined : { transform: 'rotate(-90deg)' }} /> {t('rb.advanced')}
              </button>
              {showAdvanced ? (
                <>
                  <p className="settings-hint">{t('rb.advancedHint')}</p>
                  <div className="permission-add">
                    <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/nodes" disabled={busy} />
                    <button type="button" className="quiet" onClick={addPath} disabled={busy || !roleId}><span className="btn-icon"><Ico n="plus" /> {t('rb.addPath')}</span></button>
                  </div>
                  {perms.length === 0 ? <p className="settings-hint">{t('rb.noRules')}</p> : (
                    <table className="permission-table">
                      <thead><tr><th>{t('rb.colPath')}</th>{VERBS.map(([, v]) => <th key={v}>{v}</th>)}<th /></tr></thead>
                      <tbody>
                        {perms.map((p) => (
                          <tr key={p.id}>
                            <td><code>{p.path}</code></td>
                            {VERBS.map(([key, v]) => (
                              <td key={v} className="permission-cell"><input type="checkbox" checked={!!p[key]} onChange={() => toggle(p, key)} aria-label={`${v} ${p.path}`} /></td>
                            ))}
                            <td><button type="button" className="quiet danger-text" onClick={() => remove(p)} aria-label={`${t('common.remove')} ${p.path}`}><Ico n="trash" sz={13} /></button></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </>
              ) : null}
            </div>
            {nodes.length > 0 ? (
              <div className="node-access-matrix">
                <h3>{t('rb.nodeAccess')}</h3>
                <p className="settings-hint">{t('rb.nodeAccessHint')}</p>
                <table className="permission-table">
                  <thead><tr><th>{t('rb.colNode')}</th><th>{t('rb.colAccess')}</th></tr></thead>
                  <tbody>
                    {nodes.map((n) => {
                      const level = nodeLevel(n);
                      return (
                        <tr key={n.nodeId}>
                          <td>
                            <span className="node-name-cell"><Ico n={n.icon || 'monitor'} sz={15} /> {n.name || n.nodeId}</span>
                          </td>
                          <td>
                            {level === 'owner' ? (
                              <span className="status-pill online" title={t('rb.ownerTitle')}>{t('rb.ownerFull')}</span>
                            ) : (
                              <select value={level} onChange={(e) => setNodeLevel(n, e.target.value)} disabled={busy} aria-label={`${t('rb.colAccess')} ${n.name || n.nodeId}`}>
                                <option value="none">{t('rb.accessNone')}</option>
                                <option value="viewer">{t('rb.accessViewer')}</option>
                                <option value="operator">{t('rb.accessOperator')}</option>
                                <option value="admin">{t('rb.accessAdmin')}</option>
                              </select>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : null}
          </>
        )}
      </section>
    </section>
  );
}
