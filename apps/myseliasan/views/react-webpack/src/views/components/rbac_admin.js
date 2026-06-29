import { useEffect, useState } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay } from './ui';
import { DataTable } from './data_table';
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

  function toast(t) { if (onToast) onToast(t); }

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
    if (r.ok) { toast('Role updated.'); load(); } else toast(r.message || 'Failed to update role.');
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
        <select value={u.roleId} disabled={u.isStock} onChange={(e) => setUserRole(u, e.target.value)}>
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

  function toast(t) { if (onToast) onToast(t); }

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

  function toast(t) { if (onToast) onToast(t); }

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
  useEffect(() => { loadRoles(); /* eslint-disable-next-line */ }, []);
  useEffect(() => { loadPerms(roleId); /* eslint-disable-next-line */ }, [roleId]);

  const selectedRole = roles.find((x) => x.id === Number(roleId));

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
  function toggle(p, key) { save({ ...p, [key]: !p[key] }); }
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
          </>
        )}
      </section>
    </section>
  );
}
