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
        <DataTable rows={users} columns={columns} emptyText={t('rb.noUsers')} />
      </section>
    </section>
  );
}

// --- Roles ---------------------------------------------------------------------
export function RolesPage({ onToast }) {
  const t = useT();
  const [roles, setRoles] = useState([]);
  const [newName, setNewName] = useState('');
  const [busy, setBusy] = useState(false);

  function toast(msg, kind = 'info') { if (onToast) onToast(msg, kind); }

  async function load() {
    const r = await api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setRoles(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, []);

  async function createRole() {
    if (!newName.trim()) { toast(t('rb.enterRoleName')); return; }
    setBusy(true);
    const r = await api('/api/access-rbac/roles', { method: 'POST', noRedirect: true, body: JSON.stringify({ name: newName.trim() }) });
    setBusy(false);
    if (r.ok) { setNewName(''); load(); } else toast(r.message || t('rb.failedCreateRole'));
  }
  async function deleteRole(role) {
    if (!window.confirm(t('rb.confirmDeleteRole', { name: role.name }))) return;
    setBusy(true);
    const r = await api(`/api/access-rbac/roles/${role.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) load(); else toast(r.message || t('rb.failedDeleteRole'));
  }

  const columns = [
    { key: 'id', label: t('rb.colId') },
    {
      key: 'name',
      label: t('rb.colRole'),
      render: (v, r) => (<>{v}{r.isSuperadmin ? <span className="status-pill online" style={{ marginLeft: 6 }}>{t('rb.superadminPill')}</span> : null}</>),
    },
    { key: 'builtin', label: t('rb.colType'), render: (v) => (v ? t('rb.typeBuiltin') : t('rb.typeCustom')) },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_v, r) => (
        <div className="node-row-actions">
          {!r.builtin ? <button type="button" className="quiet danger-text" onClick={() => deleteRole(r)}>{t('rb.delete')}</button> : null}
        </div>
      ),
    },
  ];

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="shield" /> {t('rb.roles')}</span></h2></header>
        <p className="settings-hint">{t('rb.rolesHint')}</p>
        <div className="table-toolbar">
          <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder={t('rb.newRoleName')} style={{ maxWidth: 260 }} />
          <button type="button" onClick={createRole}><span className="btn-icon"><Ico n="plus" /> {t('rb.addRole')}</span></button>
        </div>
        <DataTable rows={roles} columns={columns} emptyText={t('rb.noRoles')} />
      </section>
    </section>
  );
}

// --- RBAC permission matrix ----------------------------------------------------
const VERBS = [['canGet', 'GET'], ['canPost', 'POST'], ['canPut', 'PUT'], ['canDelete', 'DELETE']];

// Nav sections whose visibility is controlled per-role by the matrix. myseliasan's
// admin pages (Users/Roles/RBAC) are superadmin-only, so the only role-restrictable
// menu is Nodes (GET on /api/nodes). The label is localized via nav.nodes.
const MENU_SECTIONS = [{ labelKey: 'nav.nodes', path: '/api/nodes' }];

export function RbacPage({ onToast }) {
  const t = useT();
  const [roles, setRoles] = useState([]);
  const [roleId, setRoleId] = useState(0);
  const [perms, setPerms] = useState([]);
  const [path, setPath] = useState('/api');
  const [busy, setBusy] = useState(false);
  const [nodes, setNodes] = useState([]);
  const [nodeGrants, setNodeGrants] = useState([]);

  function toast(msg, kind = 'info') { if (onToast) onToast(msg, kind); }

  async function loadRoles() {
    const r = await api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false }));
    const list = r.ok && Array.isArray(r.body) ? r.body : [];
    setRoles(list);
    setRoleId((prev) => prev || (list.find((x) => !x.isSuperadmin)?.id ?? list[0]?.id ?? 0));
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

  const selectedRole = roles.find((x) => x.id === Number(roleId));

  // Resolve the role's current access level on a node: owner (implicit full),
  // superadmin (read+write), viewer (read-only), or none.
  function nodeLevel(node) {
    if (node.ownerRoleId && node.ownerRoleId === Number(roleId)) return 'owner';
    const g = nodeGrants.find((x) => x.nodeId === node.nodeId);
    if (!g) return 'none';
    if (g.canWrite) return 'superadmin';
    if (g.canRead) return 'viewer';
    return 'none';
  }
  // Set the role's access level on a node. "none" removes any grant; viewer/superadmin
  // upsert it. Superadmin = read+write (drives the node as admin); viewer = read-only —
  // mirroring how mymatasan's superadmin/viewer behave.
  async function setNodeLevel(node, level) {
    setBusy(true);
    let r;
    if (level === 'none') {
      const g = nodeGrants.find((x) => x.nodeId === node.nodeId);
      r = g ? await api(`/api/nodes/access/${g.id}`, { method: 'DELETE', noRedirect: true }) : { ok: true };
    } else {
      r = await api('/api/nodes/access', {
        method: 'POST', noRedirect: true,
        body: JSON.stringify({ roleId: Number(roleId), nodeId: node.nodeId, canRead: true, canWrite: level === 'superadmin' }),
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
  // toggleSection flips GET on a section's path (preserving other verbs) — what
  // shows/hides that menu for the role.
  function toggleSection(section) {
    const existing = perms.find((p) => p.path === section.path);
    save({
      roleId: Number(roleId),
      path: section.path,
      canGet: !(existing && existing.canGet),
      canPost: !!(existing && existing.canPost),
      canPut: !!(existing && existing.canPut),
      canDelete: !!(existing && existing.canDelete),
    });
  }

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two rbac-permissions">
        <header><h2><span className="btn-icon"><Ico n="lock" /> {t('rb.rbac')}</span></h2></header>
        <p className="settings-hint">{t('rb.rbacHint')}</p>
        <div className="permission-controls">
          <label className="permission-role-select">
            <span>{t('rb.role')}</span>
            <select value={roleId} onChange={(e) => setRoleId(Number(e.target.value))} disabled={busy}>
              {roles.length === 0 && <option value={0}>{t('rb.noRolesOpt')}</option>}
              {roles.map((r) => <option key={r.id} value={r.id}>{r.name}{r.isSuperadmin ? t('rb.superadminParen') : ''}</option>)}
            </select>
          </label>
        </div>
        {selectedRole?.isSuperadmin ? (
          <p className="settings-hint">{t('rb.superadminBypass')}</p>
        ) : (
          <>
            <div className="menu-access">
              <h3>{t('rb.menuAccess')}</h3>
              <p className="settings-hint">{t('rb.menuAccessHint')}</p>
              <div className="menu-access-grid">
                {MENU_SECTIONS.map((section) => {
                  const on = !!perms.find((p) => p.path === section.path)?.canGet;
                  return (
                    <label className="menu-access-item" key={section.path}>
                      <input type="checkbox" checked={on} onChange={() => toggleSection(section)} disabled={busy} />
                      <span>{t(section.labelKey)}</span>
                    </label>
                  );
                })}
              </div>
            </div>
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
                                <option value="superadmin">{t('rb.accessSuperadmin')}</option>
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
