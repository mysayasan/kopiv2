import { useEffect, useState } from 'react';
import { Ico } from './icons';
import { FormBusyOverlay } from './ui';
import { api } from '../lib/helpers';

// RbacAdminTab is the superadmin-only management surface for myseliasan's own RBAC:
// users (role assignment, disable, the bootstrap handoff) and roles + their
// per-endpoint permission matrix. Node access is a separate axis (per-node grants).
export function RbacAdminTab({ session, onToast, onSessionChanged }) {
  const [users, setUsers] = useState([]);
  const [roles, setRoles] = useState([]);
  const [busy, setBusy] = useState(false);

  function toast(t) { if (onToast) onToast(t); }

  async function load() {
    const [u, r] = await Promise.all([
      // user management (role assignment, disable, bootstrap handoff) stays myseliasan-specific.
      api('/api/rbac/users', { noRedirect: true }).catch(() => ({ ok: false })),
      // roles + permissions are the shared accessrbac module (same backing store).
      api('/api/access-rbac/roles', { noRedirect: true }).catch(() => ({ ok: false })),
    ]);
    if (u.ok) setUsers(Array.isArray(u.body) ? u.body : []);
    if (r.ok) setRoles(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, []);

  function roleName(id) { const r = roles.find((x) => x.id === id); return r ? r.name : `#${id}`; }
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
    if (!window.confirm(`Make "${label}" the superadmin?\n\nThe stock superadmin will be retired and signed out immediately. This is the one-time bootstrap handoff.`)) return;
    setBusy(true);
    const r = await api(`/api/rbac/users/${user.id}/elevate`, { method: 'POST', noRedirect: true });
    setBusy(false);
    if (r.ok) {
      toast((r.body && r.body.warning) || 'Superadmin elevated; stock account retired.');
      load();
      if (onSessionChanged) onSessionChanged();
    } else toast(r.message || 'Handoff failed.');
  }

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
        {users.length === 0 ? <p className="settings-hint">No users yet.</p> : (
          <table className="event-table">
            <thead><tr><th>User</th><th>Kind</th><th>Role</th><th>Status</th><th /></tr></thead>
            <tbody>
              {users.map((u) => {
                const self = session && session.userId === u.id;
                return (
                  <tr key={u.id}>
                    <td>{u.email || u.username || u.name || `#${u.id}`}{u.isStock ? <span className="status-pill warn" style={{ marginLeft: 6 }}>stock</span> : null}</td>
                    <td>{u.kind}</td>
                    <td>
                      <select value={u.roleId} disabled={u.isStock} onChange={(e) => setUserRole(u, e.target.value)}>
                        {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
                      </select>
                    </td>
                    <td><span className={`status-pill ${u.disabled ? 'offline' : 'online'}`}>{u.disabled ? 'disabled' : 'active'}</span></td>
                    <td className="node-row-actions">
                      {!u.isStock && u.kind === 'federated' && !isSuperRole(u.roleId)
                        ? <button type="button" className="quiet" onClick={() => elevate(u)} title="Bootstrap handoff">Make superadmin</button> : null}
                      {!self && !u.isStock
                        ? <button type="button" className="quiet danger-text" onClick={() => toggleDisabled(u)}>{u.disabled ? 'Enable' : 'Disable'}</button> : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>

      <RolesPanel roles={roles} onChanged={load} onToast={toast} setBusy={setBusy} />
    </section>
  );
}

// RolesPanel manages roles and, for a selected role, its permission matrix.
function RolesPanel({ roles, onChanged, onToast, setBusy }) {
  const [newName, setNewName] = useState('');
  const [selected, setSelected] = useState(null);

  async function createRole() {
    if (!newName.trim()) { onToast('Enter a role name.'); return; }
    setBusy(true);
    const r = await api('/api/access-rbac/roles', { method: 'POST', noRedirect: true, body: JSON.stringify({ name: newName.trim() }) });
    setBusy(false);
    if (r.ok) { setNewName(''); onChanged(); } else onToast(r.message || 'Failed to create role.');
  }
  async function deleteRole(role) {
    if (!window.confirm(`Delete role "${role.name}"?`)) return;
    setBusy(true);
    const r = await api(`/api/access-rbac/roles/${role.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) { if (selected && selected.id === role.id) setSelected(null); onChanged(); }
    else onToast(r.message || 'Failed to delete role.');
  }

  return (
    <section className="settings-panel span-two">
      <header><h2><span className="btn-icon"><Ico n="shield" /> Roles &amp; permissions</span></h2></header>
      <p className="settings-hint">
        Superadmin bypasses all checks. For other roles, grant access per path and verb (longest path prefix wins; no
        rule means denied). This governs the myseliasan module only — node access is per-node.
      </p>
      <div className="node-remote-bar">
        <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="New role name" />
        <button type="button" onClick={createRole}><span className="btn-icon"><Ico n="plus" /> Add role</span></button>
      </div>
      <table className="event-table">
        <thead><tr><th>Role</th><th>Type</th><th /></tr></thead>
        <tbody>
          {roles.map((r) => (
            <tr key={r.id}>
              <td>{r.name}{r.isSuperadmin ? <span className="status-pill online" style={{ marginLeft: 6 }}>superadmin</span> : null}</td>
              <td>{r.builtin ? 'built-in' : 'custom'}</td>
              <td className="node-row-actions">
                {!r.isSuperadmin ? <button type="button" className="quiet" onClick={() => setSelected(selected && selected.id === r.id ? null : r)}>{selected && selected.id === r.id ? 'Close' : 'Permissions'}</button> : null}
                {!r.builtin ? <button type="button" className="quiet danger-text" onClick={() => deleteRole(r)}>Delete</button> : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {selected ? <PermissionEditor role={selected} onToast={onToast} setBusy={setBusy} /> : null}
    </section>
  );
}

const VERBS = [['canGet', 'GET'], ['canPost', 'POST'], ['canPut', 'PUT'], ['canDelete', 'DELETE']];

function PermissionEditor({ role, onToast, setBusy }) {
  const [perms, setPerms] = useState([]);
  const [path, setPath] = useState('/api');

  async function load() {
    const r = await api(`/api/access-rbac/permissions?roleId=${role.id}`, { noRedirect: true }).catch(() => ({ ok: false }));
    if (r.ok) setPerms(Array.isArray(r.body) ? r.body : []);
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [role.id]);

  async function save(p) {
    setBusy(true);
    // Send only the fields the shared API accepts (it rejects unknown fields like id/createdAt).
    const body = { roleId: role.id, path: p.path, canGet: !!p.canGet, canPost: !!p.canPost, canPut: !!p.canPut, canDelete: !!p.canDelete };
    const r = await api('/api/access-rbac/permissions', { method: 'POST', noRedirect: true, body: JSON.stringify(body) });
    setBusy(false);
    if (r.ok) load(); else onToast(r.message || 'Failed to save permission.');
  }
  async function remove(p) {
    setBusy(true);
    const r = await api(`/api/access-rbac/permissions/${p.id}`, { method: 'DELETE', noRedirect: true });
    setBusy(false);
    if (r.ok) load(); else onToast(r.message || 'Failed.');
  }
  function toggle(p, key) { save({ ...p, [key]: !p[key] }); }
  function addPath() {
    if (!path.trim().startsWith('/')) { onToast('Path must start with /'); return; }
    save({ roleId: role.id, path: path.trim(), canGet: true, canPost: false, canPut: false, canDelete: false });
  }

  return (
    <div className="perm-editor">
      <h3>Permissions for <code>{role.name}</code></h3>
      <div className="node-remote-bar">
        <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/nodes" />
        <button type="button" className="quiet" onClick={addPath}><span className="btn-icon"><Ico n="plus" /> Add path</span></button>
      </div>
      {perms.length === 0 ? <p className="settings-hint">No rules — this role is denied everything.</p> : (
        <table className="event-table">
          <thead><tr><th>Path</th>{VERBS.map(([, v]) => <th key={v}>{v}</th>)}<th /></tr></thead>
          <tbody>
            {perms.map((p) => (
              <tr key={p.id}>
                <td><code>{p.path}</code></td>
                {VERBS.map(([key, v]) => (
                  <td key={v}><input type="checkbox" checked={!!p[key]} onChange={() => toggle(p, key)} /></td>
                ))}
                <td><button type="button" className="quiet danger-text" onClick={() => remove(p)}><Ico n="trash" sz={13} /></button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
