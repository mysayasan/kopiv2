import { useEffect, useState } from 'react';
import { Ico, DataTable } from '@shared';
import { FormBusyOverlay } from './ui';
import { api } from '../lib/helpers';

// The myseliasan admin surface, split into the same menu structure myidsan uses —
// Users, Roles, and the RBAC permission matrix — each built on the shared filterable
// /sortable DataTable. myseliasan has no Groups/Endpoints catalog, so those myidsan
// sections do not apply here.

// --- Users ---------------------------------------------------------------------
export function UsersPage({ session, onToast, onSessionChanged }) {
  const [users, setUsers] = useState([]);
  const [roles, setRoles] = useState([]);
  const [busy, setBusy] = useState(false);

  function toast(t, kind = 'info') { if (onToast) onToast(t, kind); }

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
      const roleName = !id ? 'No role (pending)' : (roles.find((x) => Number(x.id) === id)?.name || `role ${id}`);
      const who = user.email || user.name || `user ${user.id}`;
      toast(`Role for ${who} set to ${roleName}.`, 'success');
      load();
    } else toast(r.message || 'Failed to update role.', 'error');
  }
  async function toggleDisabled(user) {
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/disabled`, { method: 'POST', noRedirect: true, body: JSON.stringify({ disabled: !user.disabled }) });
    setBusy(false);
    if (r.ok) load(); else toast(r.message || 'Failed.');
  }
  async function elevate(user) {
    const label = user.email || user.name || user.username || `user ${user.id}`;
    if (!window.confirm(`Make "${label}" a superadmin?\n\nThe stock superadmin stays active — disable it from this list once you've confirmed the new account works.`)) return;
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/elevate`, { method: 'POST', noRedirect: true });
    setBusy(false);
    if (r.ok) {
      toast((r.body && r.body.warning) || 'Superadmin granted.');
      load();
      if (onSessionChanged) onSessionChanged();
    } else toast(r.message || 'Handoff failed.');
  }

  const columns = [
    { key: 'id', label: 'ID' },
    {
      key: 'email',
      label: 'User',
      render: (_v, u) => (
        <>
          {u.email || u.username || u.name || `#${u.id}`}
          {u.isStock ? <span className="status-pill warn" style={{ marginLeft: 6 }}>stock</span> : null}
        </>
      ),
    },
    { key: 'kind', label: 'Kind' },
    {
      key: 'roleId',
      label: 'Role',
      filterable: false,
      render: (_v, u) => (
        <select value={u.roleId || 0} disabled={u.isStock} onChange={(e) => setUserRole(u, e.target.value)}>
          {/* value 0 = no role yet (pending). Without this option a role-less user would
              render as the first role, making them look like a superadmin. */}
          <option value={0}>— No role (pending) —</option>
          {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      ),
    },
    { key: 'disabled', label: 'Status', render: (v) => <span className={`status-pill ${v ? 'offline' : 'online'}`}>{v ? 'disabled' : 'active'}</span> },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_v, u) => {
        const self = session && session.userId === u.id;
        return (
          <div className="node-row-actions">
            {!u.isStock && u.kind === 'federated' && !isSuperRole(u.roleId)
              ? <button type="button" className="quiet" onClick={() => elevate(u)} title="Bootstrap handoff">Make superadmin</button> : null}
            {!self && (!u.isStock || session?.superadminHandoffPending)
              ? <button type="button" className="quiet danger-text" onClick={() => toggleDisabled(u)}>{u.disabled ? 'Enable' : 'Disable'}</button> : null}
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
          <h2><span className="btn-icon"><Ico n="user" /> Users</span></h2>
          <div className="settings-header-actions">
            <button type="button" className="quiet" onClick={load}><span className="btn-icon"><Ico n="reload" /> Refresh</span></button>
          </div>
        </header>
        <p className="settings-hint">
          Every myidsan sign-in is provisioned as a viewer. Assign a role, disable access, or run the one-time
          bootstrap handoff to retire the stock superadmin.
        </p>
        <DataTable rows={users} columns={columns} emptyText="No users yet." />
      </section>
    </section>
  );
}

// --- Roles ---------------------------------------------------------------------
export function RolesPage({ onToast }) {
  const [roles, setRoles] = useState([]);
  const [newName, setNewName] = useState('');
  const [busy, setBusy] = useState(false);

  function toast(t, kind = 'info') { if (onToast) onToast(t, kind); }

  async function load() {
    const r = await api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setRoles(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, []);

  async function createRole() {
    if (!newName.trim()) { toast('Enter a role name.'); return; }
    setBusy(true);
    const r = await api('/api/access-rbac/roles', { method: 'POST', noRedirect: true, body: JSON.stringify({ name: newName.trim() }) });
    setBusy(false);
    if (r.ok) { setNewName(''); load(); } else toast(r.message || 'Failed to create role.');
  }
  async function deleteRole(role) {
    if (!window.confirm(`Delete role "${role.name}"?`)) return;
    setBusy(true);
    const r = await api(`/api/access-rbac/roles/${role.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) load(); else toast(r.message || 'Failed to delete role.');
  }

  const columns = [
    { key: 'id', label: 'ID' },
    {
      key: 'name',
      label: 'Role',
      render: (v, r) => (<>{v}{r.isSuperadmin ? <span className="status-pill online" style={{ marginLeft: 6 }}>superadmin</span> : null}</>),
    },
    { key: 'builtin', label: 'Type', render: (v) => (v ? 'built-in' : 'custom') },
    {
      key: 'actions',
      label: '',
      filterable: false,
      render: (_v, r) => (
        <div className="node-row-actions">
          {!r.builtin ? <button type="button" className="quiet danger-text" onClick={() => deleteRole(r)}>Delete</button> : null}
        </div>
      ),
    },
  ];

  return (
    <section className="workspace">
      <FormBusyOverlay busy={busy} />
      <section className="settings-panel span-two">
        <header><h2><span className="btn-icon"><Ico n="shield" /> Roles</span></h2></header>
        <p className="settings-hint">
          Roles are the shared accessrbac roles. Superadmin bypasses all checks; built-in roles cannot be deleted.
          Grant per-path access on the RBAC page.
        </p>
        <div className="table-toolbar">
          <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="New role name" style={{ maxWidth: 260 }} />
          <button type="button" onClick={createRole}><span className="btn-icon"><Ico n="plus" /> Add role</span></button>
        </div>
        <DataTable rows={roles} columns={columns} emptyText="No roles yet." />
      </section>
    </section>
  );
}

// --- RBAC permission matrix ----------------------------------------------------
const VERBS = [['canGet', 'GET'], ['canPost', 'POST'], ['canPut', 'PUT'], ['canDelete', 'DELETE']];

// Nav sections whose visibility is controlled per-role by the matrix. myseliasan's
// admin pages (Users/Roles/RBAC) are superadmin-only, so the only role-restrictable
// menu is Nodes (GET on /api/nodes). The toggles below grant/revoke that GET.
const MENU_SECTIONS = [{ label: 'Nodes', path: '/api/nodes' }];

export function RbacPage({ onToast }) {
  const [roles, setRoles] = useState([]);
  const [roleId, setRoleId] = useState(0);
  const [perms, setPerms] = useState([]);
  const [path, setPath] = useState('/api');
  const [busy, setBusy] = useState(false);
  const [nodes, setNodes] = useState([]);
  const [nodeGrants, setNodeGrants] = useState([]);

  function toast(t, kind = 'info') { if (onToast) onToast(t, kind); }

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
    if (r.ok) loadNodeGrants(roleId); else toast(r.message || 'Failed to update node access.');
  }

  async function save(p) {
    setBusy(true);
    const body = { roleId: Number(roleId), path: p.path, canGet: !!p.canGet, canPost: !!p.canPost, canPut: !!p.canPut, canDelete: !!p.canDelete };
    const r = await api('/api/access-rbac/permissions', { method: 'POST', noRedirect: true, body: JSON.stringify(body) });
    setBusy(false);
    if (r.ok) loadPerms(roleId); else toast(r.message || 'Failed to save permission.');
  }
  async function remove(p) {
    setBusy(true);
    const r = await api(`/api/access-rbac/permissions/${p.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) loadPerms(roleId); else toast(r.message || 'Failed.');
  }
  function toggle(p, key) {
    // Optimistic: flip this row's cell immediately (server reload confirms; the stable
    // path-sorted list keeps rows from reshuffling under the click).
    setPerms((cur) => cur.map((row) => (row.id === p.id ? { ...row, [key]: !row[key] } : row)));
    save({ ...p, [key]: !p[key] });
  }
  function addPath() {
    if (!path.trim().startsWith('/')) { toast('Path must start with /'); return; }
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
        <header><h2><span className="btn-icon"><Ico n="lock" /> RBAC</span></h2></header>
        <p className="settings-hint">
          Grant a role access per path prefix and verb (longest matching prefix wins; no rule means denied). This
          governs both API access and which menus the role sees. Superadmin bypasses all checks.
        </p>
        <div className="permission-controls">
          <label className="permission-role-select">
            <span>Role</span>
            <select value={roleId} onChange={(e) => setRoleId(Number(e.target.value))} disabled={busy}>
              {roles.length === 0 && <option value={0}>No roles</option>}
              {roles.map((r) => <option key={r.id} value={r.id}>{r.name}{r.isSuperadmin ? ' (superadmin)' : ''}</option>)}
            </select>
          </label>
        </div>
        {selectedRole?.isSuperadmin ? (
          <p className="settings-hint">Superadmin bypasses all checks — no rules needed.</p>
        ) : (
          <>
            <div className="menu-access">
              <h3>Menu access</h3>
              <p className="settings-hint">Toggle which navigation sections this role can see and open. Each switch grants or revokes GET on the section&apos;s API path.</p>
              <div className="menu-access-grid">
                {MENU_SECTIONS.map((section) => {
                  const on = !!perms.find((p) => p.path === section.path)?.canGet;
                  return (
                    <label className="menu-access-item" key={section.path}>
                      <input type="checkbox" checked={on} onChange={() => toggleSection(section)} disabled={busy} />
                      <span>{section.label}</span>
                    </label>
                  );
                })}
              </div>
            </div>
            <div className="permission-add">
              <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/nodes" disabled={busy} />
              <button type="button" className="quiet" onClick={addPath} disabled={busy || !roleId}><span className="btn-icon"><Ico n="plus" /> Add path</span></button>
            </div>
            {perms.length === 0 ? <p className="settings-hint">No rules — this role is denied everything (and sees no menus).</p> : (
              <table className="permission-table">
                <thead><tr><th>Path</th>{VERBS.map(([, v]) => <th key={v}>{v}</th>)}<th /></tr></thead>
                <tbody>
                  {perms.map((p) => (
                    <tr key={p.id}>
                      <td><code>{p.path}</code></td>
                      {VERBS.map(([key, v]) => (
                        <td key={v} className="permission-cell"><input type="checkbox" checked={!!p[key]} onChange={() => toggle(p, key)} aria-label={`${v} ${p.path}`} /></td>
                      ))}
                      <td><button type="button" className="quiet danger-text" onClick={() => remove(p)} aria-label={`Remove ${p.path}`}><Ico n="trash" sz={13} /></button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {nodes.length > 0 ? (
              <div className="node-access-matrix">
                <h3>Node access</h3>
                <p className="settings-hint">
                  Grant this role access to managed nodes. <strong>Superadmin</strong> drives the node with full
                  read + write; <strong>Viewer</strong> is read-only — mirroring how mymatasan&apos;s superadmin and
                  viewer roles behave. The node&apos;s owning role always has full access.
                </p>
                <table className="permission-table">
                  <thead><tr><th>Node</th><th>Access</th></tr></thead>
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
                              <span className="status-pill online" title="This role adopted the node">Owner — full access</span>
                            ) : (
                              <select value={level} onChange={(e) => setNodeLevel(n, e.target.value)} disabled={busy} aria-label={`Access for ${n.name || n.nodeId}`}>
                                <option value="none">No access</option>
                                <option value="viewer">Viewer (read-only)</option>
                                <option value="superadmin">Superadmin (read + write)</option>
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
