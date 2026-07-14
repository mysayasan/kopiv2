import { useEffect, useState } from 'react';
import { Ico, SideNav as SharedSideNav, useT, BrandLogo as SharedBrandLogo } from '@shared';
import { api, sessionCanGet } from '../lib/helpers';

// The mark (shield + eye + check) is the suite's, shared verbatim with mymatasan and
// myidsan; the control plane only supplies its wordmark and its --brand-mark hue
// (app.css) so the family is one geometry with three tints.
export function BrandLogo(props) {
  return <SharedBrandLogo wordmark="myseliasan" {...props} />;
}

// Past this many nodes the branch gets a filter box and becomes a height-capped
// scroll area, so the rail stays usable with dozens–hundreds of nodes.
const NODE_FILTER_THRESHOLD = 8;
const NODE_FALLBACK_ICON = 'monitor';
// A sensor hub (myiotsan) is not a camera NVR, and the rail should not pretend otherwise.
const SENSOR_FALLBACK_ICON = 'cpu';
const CAMERA_ICON = 'video';

// isSensorNode: the node's kind is "camera" or "iot"; an EMPTY kind is a camera node, because
// every node adopted before the field existed is a mymatasan NVR.
export const isSensorNode = (node) => String(node?.kind || '').toLowerCase() === 'iot';
export const nodeFallbackIcon = (node) => (isSensorNode(node) ? SENSOR_FALLBACK_ICON : NODE_FALLBACK_ICON);

// NodeTreeChild is one adopted node in the nav tree, now itself expandable into its
// cameras. The node row navigates to that node's manage surface (all cameras); its
// caret reveals a lazily-loaded camera sub-branch (fetched over the node's command
// tunnel the first time it's opened). Each camera leaf jumps straight to that single
// camera's live view. The branch auto-opens when a camera on this node is focused.
function NodeTreeChild({ node, onNodes, managingNodeId, managingCameraId, onSelectNode }) {
  const t = useT();
  const isManaged = onNodes && managingNodeId === node.nodeId;
  const nodeActive = isManaged && !managingCameraId;
  const status = node.status === 'online' ? 'online' : 'offline';
  // A sensor hub has no cameras, so it gets no camera sub-branch (and no caret that would
  // fetch /api/cameras from a node that does not serve it).
  const sensor = isSensorNode(node);
  const [open, setOpen] = useState(false);
  const [cams, setCams] = useState(null); // null = never loaded; [] = loaded, empty
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(''); // set when the node couldn't be reached / listed

  async function loadCams() {
    setLoading(true);
    setError('');
    const r = await api(`/api/nodes/${encodeURIComponent(node.nodeId)}/proxy/api/cameras?limit=100`, { noRedirect: true })
      .catch(() => ({ ok: false }));
    setLoading(false);
    if (r.ok) {
      setCams(Array.isArray(r.body) ? r.body : (r.body?.items || []));
      return;
    }
    // Distinguish "reached the node, it has no cameras" from "couldn't reach it" — the
    // proxy returns 404 when the node is offline and 403 when the role lacks access.
    setCams([]);
    if (r.status === 403) setError(t('nodes.camsNoAccess'));
    else if (r.status === 404) setError(t('nodes.nodeOffline'));
    else setError(r.message || t('nodes.camsFailed'));
  }

  function toggle() {
    const next = !open;
    setOpen(next);
    if (next && cams === null && !loading) loadCams();
  }

  // expand opens (never collapses) the camera sub-branch and lazy-loads it the first time.
  function expand() {
    setOpen(true);
    if (cams === null && !loading) loadCams();
  }

  // A single click navigates to the node AND expands its cameras — consistent with the
  // Nodes root row. Collapse via the caret (or a double-click, which toggles).
  function selectAndExpand() {
    onSelectNode(node.nodeId);
    expand();
  }

  // Reflect a camera opened from elsewhere (e.g. deep-link / page) by expanding this
  // node and loading its cameras so the active leaf is visible.
  useEffect(() => {
    if (isManaged && managingCameraId && !open) {
      setOpen(true);
      if (cams === null && !loading) loadCams();
    }
    // eslint-disable-next-line
  }, [isManaged, managingCameraId]);

  return (
    <div className="nav-subtree">
      <div className="nav-tree-rootrow nav-subtree-row">
        {sensor ? (
          <span className="nav-tree-caret nav-subtree-caret" aria-hidden="true" />
        ) : (
          <button
            type="button"
            className="nav-tree-caret nav-subtree-caret"
            onClick={toggle}
            aria-label={open ? t('nodes.collapseCams') : t('nodes.expandCams')}
            aria-expanded={open}
          >
            <Ico n="chev-down" sz={12} style={open ? undefined : { transform: 'rotate(-90deg)' }} />
          </button>
        )}
        <button
          type="button"
          className={`nav-item tone-blue nav-tree-child nav-tree-main${nodeActive ? ' active' : ''}`}
          onClick={sensor ? () => onSelectNode(node.nodeId) : selectAndExpand}
          onDoubleClick={sensor ? undefined : toggle}
          title={node.description ? undefined : `${node.name || node.nodeId} — ${sensor ? t('node.kindIot') : t('node.kindCamera')}`}
        >
          <span className="nav-tree-ico" data-status={status}>
            <Ico n={node.icon || nodeFallbackIcon(node)} sz={16} />
          </span>
          <span className="nav-label">{node.name || node.nodeId}</span>
          {node.description ? (
            <span className="nav-tip" role="tooltip">
              <span className="nav-tip-title">{node.name || node.nodeId}</span>
              <span className="nav-tip-body">{node.description}</span>
            </span>
          ) : null}
        </button>
      </div>
      {open && !sensor ? (
        <div className="nav-tree-children nav-cam-children">
          {loading ? (
            <div className="nav-tree-empty">{t('nodes.loadingCams')}</div>
          ) : error ? (
            <button type="button" className="nav-tree-empty nav-cam-retry" onClick={loadCams}>
              <Ico n="reload" sz={12} /> {error}
            </button>
          ) : cams === null ? null
            : cams.length === 0 ? (
              <div className="nav-tree-empty">{t('nodes.noCams')}</div>
            ) : (
              cams.map((c) => {
                const camActive = isManaged && String(managingCameraId) === String(c.id);
                // Per-camera liveness dot (green online / red offline / grey unknown),
                // mirroring mymatasan's camera nav — driven by the node-reported health.
                const camStatus = (c.healthStatus || '').toLowerCase() === 'online'
                  ? 'online'
                  : (c.healthStatus || '').toLowerCase() === 'offline'
                    ? 'offline'
                    : 'unknown';
                return (
                  <button
                    key={c.id}
                    type="button"
                    className={`nav-item tone-blue nav-tree-child nav-tree-grandchild${camActive ? ' active' : ''}`}
                    onClick={() => onSelectNode(node.nodeId, c.id)}
                    title={c.name || t('nodes.cameraN', { id: c.id })}
                  >
                    <span className="nav-tree-ico nav-cam-ico" data-status={camStatus}><Ico n={CAMERA_ICON} sz={14} /></span>
                    <span className="nav-label">{c.name || t('nodes.cameraN', { id: c.id })}</span>
                  </button>
                );
              })
            )}
        </div>
      ) : null}
    </div>
  );
}

// NodesNavItem renders the Nodes entry as an expandable tree: the root row navigates
// to the fleet management page (discovery/adoption/list), while each adopted node is a
// child branch that jumps straight to that node's manage surface. A caret toggles the
// branch without leaving the current view. Large fleets get a search filter and a
// scrollable branch.
function NodesNavItem({ nodes, activeTab, managingNodeId, managingCameraId, onSelectNode }) {
  const t = useT();
  const onNodes = activeTab === 'nodes';
  const [open, setOpen] = useState(onNodes);
  const [query, setQuery] = useState('');
  const rootActive = onNodes && !managingNodeId;
  const list = Array.isArray(nodes) ? nodes : [];

  // Auto-expand the tree when you land on the Nodes tab, so the fleet is visible.
  useEffect(() => { if (onNodes) setOpen(true); }, [onNodes]);

  const big = list.length > NODE_FILTER_THRESHOLD;
  const q = query.trim().toLowerCase();
  const shown = q
    ? list.filter((n) => `${n.name || ''} ${n.description || ''} ${n.nodeId || ''}`.toLowerCase().includes(q))
    : list;

  return (
    <div className="nav-tree">
      <div className="nav-tree-rootrow">
        <button
          type="button"
          className="nav-tree-caret"
          onClick={() => setOpen((o) => !o)}
          aria-label={open ? t('nodes.collapse') : t('nodes.expand')}
          aria-expanded={open}
        >
          <Ico n="chev-down" sz={14} style={open ? undefined : { transform: 'rotate(-90deg)' }} />
        </button>
        <button
          type="button"
          className={`nav-item tone-blue nav-tree-main${rootActive ? ' active' : ''}`}
          onClick={() => { onSelectNode(null); setOpen(true); }}
          onDoubleClick={() => setOpen((o) => !o)}
        >
          <span className="nav-ico"><Ico n="shield" sz={17} /></span>
          <span className="nav-label">{t('nav.nodes')}</span>
          {list.length > 0 ? <span className="nav-tree-count">{list.length}</span> : null}
        </button>
      </div>
      {open ? (
        <div className="nav-tree-branch">
          {big ? (
            <div className="nav-tree-search">
              <Ico n="search" sz={13} />
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t('nodes.filter', { n: list.length })}
                aria-label={t('nodes.filterAria')}
              />
            </div>
          ) : null}
          <div className={`nav-tree-children${big ? ' scrolling' : ''}`}>
            {list.length === 0 ? (
              <div className="nav-tree-empty">{t('nodes.none')}</div>
            ) : shown.length === 0 ? (
              <div className="nav-tree-empty">{t('nodes.noMatches')}</div>
            ) : (
              shown.map((n) => (
                <NodeTreeChild
                  key={n.nodeId}
                  node={n}
                  onNodes={onNodes}
                  managingNodeId={managingNodeId}
                  managingCameraId={managingCameraId}
                  onSelectNode={onSelectNode}
                />
              ))
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

// SideNav is the standardized RBAC-app navigation rail: the myidsan dark side-nav
// (grouped menu, code tiles, tone accents) reused as the shared shell. Menu entries
// follow the same permission matrix that gates their APIs — a role needs GET on
// /api/nodes to see Nodes; Users & Roles is superadmin-only.
// AccountCard is the slim sign-out pill under the brand at the top of the rail: a
// role chip (avatar + role) and a ghost logout icon button. The email/identity is
// deliberately NOT shown (avoids exposing the signed-in account in the UI).
// Standardized with mymatasan's rail.
function AccountCard({ roleLabel, onLogout, busy }) {
  const t = useT();
  return (
    <div className="side-account">
      <span className="side-account-avatar" aria-hidden="true"><Ico n="user" sz={15} /></span>
      <span className="side-account-role">{roleLabel}</span>
      <button
        type="button"
        className="side-account-logout"
        onClick={onLogout}
        disabled={busy}
        title={t('auth.logout')}
        aria-label={t('auth.logout')}
      >
        <Ico n="logout" sz={16} />
      </button>
    </div>
  );
}

export function SideNav({ activeTab, busy, onTab, onLogout, session, nodes, managingNodeId, managingCameraId, onSelectNode, notifUnread = 0, pinned = true, onTogglePinned }) {
  const t = useT();
  const navItem = (id, label, icon, tone) => ({ id, label, icon, tone, active: id === activeTab, onClick: () => onTab(id) });
  // The consolidated Notifications entry carries an unread badge (control-plane + every
  // node's pushed events), mirroring mymatasan's own Notifications nav item.
  const notificationsItem = {
    id: 'notifications',
    render: () => (
      <button
        type="button"
        className={`nav-item nav-item-badged tone-green${activeTab === 'notifications' ? ' active' : ''}`}
        onClick={() => onTab('notifications')}
        aria-label={notifUnread > 0 ? t('notif.navAriaUnread', { n: notifUnread }) : t('notif.navAria')}
      >
        <span className="nav-ico"><Ico n="bell" sz={17} /></span>
        <span className="nav-label">{t('tab.notifications')}</span>
        {notifUnread > 0 ? <span className="nav-item-badge">{notifUnread > 99 ? '99+' : notifUnread}</span> : null}
      </button>
    ),
  };
  const groups = [
    { label: t('group.workspace'), items: [navItem('dashboard', t('nav.dashboard'), 'monitor', 'steel')] },
    {
      label: t('group.fleet'),
      // Live Views sits above the Nodes tree; the Nodes entry is a bespoke tree injected via
      // the shell's render hook. Fleet rules sits below it, because it is the only entry that
      // is about the fleet AS A WHOLE — a rule that spans a camera node and a sensor hub — and
      // it has its own permission gate (reading rules is matrix-driven, writing is superadmin).
      items: [
        ...(sessionCanGet(session, '/api/nodes')
          ? [
              navItem('liveviews', t('nav.liveViews'), 'video', 'steel'),
              navItem('objects', t('nav.objects'), 'eye', 'teal'),
              navItem('teach', t('nav.teach'), 'wand', 'amber'),
              { id: 'nodes', render: () => (
                <NodesNavItem nodes={nodes} activeTab={activeTab} managingNodeId={managingNodeId} managingCameraId={managingCameraId} onSelectNode={onSelectNode} />
              ) },
            ]
          : []),
        ...(sessionCanGet(session, '/api/fleet-rules')
          ? [navItem('fleetrules', t('nav.fleetRules'), 'grid4', 'violet')]
          : []),
      ],
    },
    {
      label: t('group.administration'),
      items: session?.isSuperadmin
        ? [
            navItem('users', t('nav.users'), 'user', 'blue'),
            navItem('roles', t('nav.roles'), 'lock', 'violet'),
            navItem('audit', t('nav.audit'), 'list', 'steel'),
          ]
        : [],
    },
    // System: the consolidated notification feed, available to any signed-in operator.
    { label: t('group.system'), items: [notificationsItem] },
  ];

  const brand = (
    <div className="side-brand">
      {/* Pin / auto-hide toggle, standardized with mymatasan's rail: pinned keeps the
          260px rail in the grid flow, unpinned collapses it to an icon strip that slides
          out on hover. */}
      {onTogglePinned ? (
        <button
          type="button"
          className="nav-pin-toggle"
          onClick={(e) => { onTogglePinned(); e.currentTarget.blur(); }}
          aria-pressed={pinned}
          title={pinned ? t('nav.autohide') : t('nav.pin')}
          aria-label={pinned ? t('nav.autohide') : t('nav.pin')}
        >
          <Ico n={pinned ? 'pin' : 'pin-off'} sz={16} />
        </button>
      ) : null}
      <BrandLogo />
      <div className="side-brand-sub">{t('brand.controlPlane')}</div>
      <AccountCard
        roleLabel={session?.isSuperadmin ? t('role.superadmin') : (session?.roleName || t('role.viewer'))}
        onLogout={onLogout}
        busy={busy}
      />
    </div>
  );

  return <SharedSideNav brand={brand} groups={groups} footer={null} />;
}
