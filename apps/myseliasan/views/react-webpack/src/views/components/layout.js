import { useState } from 'react';
import { Ico, SideNav as SharedSideNav } from '@shared';
import { ThemeDropdown } from './ui';
import { sessionCanGet } from '../lib/helpers';

// BrandLogo mirrors the mymatasan mark (line-art shield + eye + check) with the
// rounded lowercase wordmark, so the control plane shares the product's identity.
export function BrandLogo({ size = 40, className = '' }) {
  return (
    <div className={`brand-logo${className ? ` ${className}` : ''}`} aria-label="myseliasan">
      <svg className="brand-mark" viewBox="0 0 64 64" width={size} height={size} role="img" aria-hidden="true">
        <g fill="none" stroke="#4e9d6e" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round">
          <path d="M19 13.5 H45 Q50 13.5 50 18 V33 Q50 45 32 54 Q14 45 14 33 V18 Q14 13.5 19 13.5 Z" />
          <path d="M20 30 Q31 21 42 30 Q31 39 20 30 Z" />
          <circle cx="31" cy="30" r="6" />
          <path d="M24 41 L31 48 L46 30" strokeWidth="3.4" />
        </g>
        <circle cx="31" cy="30" r="3" fill="#4e9d6e" />
        <circle cx="29.5" cy="28.6" r="1" fill="#eaf6ef" />
      </svg>
      <span className="brand-wordmark">myseliasan</span>
    </div>
  );
}

// Past this many nodes the branch gets a filter box and becomes a height-capped
// scroll area, so the rail stays usable with dozens–hundreds of nodes.
const NODE_FILTER_THRESHOLD = 8;
const NODE_FALLBACK_ICON = 'monitor';

// NodesNavItem renders the Nodes entry as an expandable tree: the root row navigates
// to the fleet management page (discovery/adoption/list), while each adopted node is a
// child branch that jumps straight to that node's manage surface. A caret toggles the
// branch without leaving the current view. Large fleets get a search filter and a
// scrollable branch.
function NodesNavItem({ nodes, activeTab, managingNodeId, onSelectNode }) {
  const onNodes = activeTab === 'nodes';
  const [open, setOpen] = useState(onNodes);
  const [query, setQuery] = useState('');
  const rootActive = onNodes && !managingNodeId;
  const list = Array.isArray(nodes) ? nodes : [];

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
          aria-label={open ? 'Collapse nodes' : 'Expand nodes'}
          aria-expanded={open}
        >
          <Ico n="chev-down" sz={14} style={open ? undefined : { transform: 'rotate(-90deg)' }} />
        </button>
        <button
          type="button"
          className={`nav-item tone-blue nav-tree-main${rootActive ? ' active' : ''}`}
          onClick={() => { onSelectNode(null); setOpen(true); }}
        >
          <span className="nav-ico"><Ico n="shield" sz={17} /></span>
          <span className="nav-label">Nodes</span>
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
                placeholder={`Filter ${list.length} nodes…`}
                aria-label="Filter nodes"
              />
            </div>
          ) : null}
          <div className={`nav-tree-children${big ? ' scrolling' : ''}`}>
            {list.length === 0 ? (
              <div className="nav-tree-empty">No nodes yet</div>
            ) : shown.length === 0 ? (
              <div className="nav-tree-empty">No matches</div>
            ) : (
              shown.map((n) => {
                const active = onNodes && managingNodeId === n.nodeId;
                const status = n.status === 'online' ? 'online' : 'offline';
                return (
                  <button
                    key={n.nodeId}
                    type="button"
                    className={`nav-item tone-blue nav-tree-child${active ? ' active' : ''}`}
                    onClick={() => onSelectNode(n.nodeId)}
                    title={n.description ? undefined : (n.name || n.nodeId)}
                  >
                    <span className="nav-tree-ico" data-status={status}>
                      <Ico n={n.icon || NODE_FALLBACK_ICON} sz={16} />
                    </span>
                    <span className="nav-label">{n.name || n.nodeId}</span>
                    {n.description ? (
                      <span className="nav-tip" role="tooltip">
                        <span className="nav-tip-title">{n.name || n.nodeId}</span>
                        <span className="nav-tip-body">{n.description}</span>
                      </span>
                    ) : null}
                  </button>
                );
              })
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
export function SideNav({ activeTab, busy, onTab, onLogout, theme, onThemeChange, session, nodes, managingNodeId, onSelectNode }) {
  const navItem = (id, label, icon, tone) => ({ id, label, icon, tone, active: id === activeTab, onClick: () => onTab(id) });
  const groups = [
    { label: 'Workspace', items: [navItem('dashboard', 'Dashboard', 'monitor', 'steel')] },
    {
      label: 'Fleet',
      items: sessionCanGet(session, '/api/nodes')
        // The Nodes entry is a bespoke tree — injected via the shell's render hook.
        ? [{ id: 'nodes', render: () => (
            <NodesNavItem nodes={nodes} activeTab={activeTab} managingNodeId={managingNodeId} onSelectNode={onSelectNode} />
          ) }]
        : [],
    },
    {
      label: 'Administration',
      items: session?.isSuperadmin
        ? [navItem('users', 'Users', 'user', 'blue'), navItem('roles', 'Roles', 'key', 'violet'), navItem('rbac', 'RBAC', 'lock', 'green')]
        : [],
    },
  ];

  const brand = (
    <div className="side-brand">
      <BrandLogo />
      <div className="side-brand-sub">Control plane</div>
    </div>
  );
  const footer = (
    <>
      {session?.email ? <div className="side-brand-sub" title={session.email}>{session.email}</div> : null}
      <ThemeDropdown theme={theme} onThemeChange={onThemeChange} />
      <button type="button" className="logout-button" onClick={onLogout} disabled={busy}>
        Log out
      </button>
    </>
  );

  return <SharedSideNav brand={brand} groups={groups} footer={footer} />;
}
