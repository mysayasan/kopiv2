package services

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// NodeAccess is the resolved per-(role, node) capability — an escalation ladder, where each
// rung implies the ones below it.
type NodeAccess struct {
	CanRead    bool
	CanOperate bool
	CanWrite   bool
}

// Role names the role the tunnel asserts at the node.
//
// The node does NOT take this as permission — it takes it as an identity claim, resolves the
// name against its OWN roles, and evaluates its OWN matrix. That is the whole security
// posture of the fleet: the control plane says "on behalf of an operator", and the node
// decides what an operator may do to its data. A compromised control plane cannot assert
// capabilities the node never granted.
//
// "admin" is the wire word for the node's superadmin, kept for the fleet already in the
// field. "operator" is the rung that was missing: without it a control-plane user was either
// a viewer or an admin, and the node's role model collapsed to a binary the moment a command
// crossed the tunnel.
func (a NodeAccess) Role() string {
	switch {
	case a.CanWrite:
		return "admin"
	case a.CanOperate:
		return "operator"
	case a.CanRead:
		return "viewer"
	default:
		return ""
	}
}

// INodeAccessService resolves and manages per-node access for myseliasan roles.
type INodeAccessService interface {
	// Resolve returns the effective access a role has on a node: full if the role
	// owns the node (adopted it), otherwise the explicit grant, otherwise none.
	Resolve(ctx context.Context, roleId int64, nodeId string) (NodeAccess, error)
	// OwnsNode reports whether roleId is the node's owning (adopting) role.
	OwnsNode(ctx context.Context, roleId int64, nodeId string) (bool, error)
	// ListForNode returns the explicit grants configured for a node.
	ListForNode(ctx context.Context, nodeId string) ([]*entities.NodeAccessGrant, error)
	// ListForRole returns the explicit grants a role holds across all nodes (used by
	// the central RBAC node-access matrix).
	ListForRole(ctx context.Context, roleId int64) ([]*entities.NodeAccessGrant, error)
	// Set creates or updates the grant for (roleId, nodeId). write implies read.
	Set(ctx context.Context, grant entities.NodeAccessGrant) (*entities.NodeAccessGrant, error)
	// GrantById returns a grant by id, or nil when not found (for ownership checks
	// before deletion).
	GrantById(ctx context.Context, id int64) (*entities.NodeAccessGrant, error)
	// Delete removes a grant by id.
	Delete(ctx context.Context, id int64) error
}

type nodeAccessService struct {
	grants dbsql.IGenericRepo[entities.NodeAccessGrant]
	nodes  dbsql.IGenericRepo[entities.ManagedNode]
	roles  sharedservices.IAccessRoleService
}

// NewNodeAccessService builds the access service over the control-plane database. The
// roles service lets it grant superadmin roles implicit full access to every node.
func NewNodeAccessService(db dbsql.IDbCrud, roles sharedservices.IAccessRoleService) INodeAccessService {
	return newNodeAccessService(
		dbsql.NewGenericRepo[entities.NodeAccessGrant](db),
		dbsql.NewGenericRepo[entities.ManagedNode](db),
		roles,
	)
}

func newNodeAccessService(
	grants dbsql.IGenericRepo[entities.NodeAccessGrant],
	nodes dbsql.IGenericRepo[entities.ManagedNode],
	roles sharedservices.IAccessRoleService,
) *nodeAccessService {
	return &nodeAccessService{grants: grants, nodes: nodes, roles: roles}
}

// normalizeAccess enforces the escalation ladder: each rung implies the ones below it.
func normalizeAccess(a NodeAccess) NodeAccess {
	if a.CanWrite {
		a.CanOperate = true
	}
	if a.CanOperate {
		a.CanRead = true
	}
	return a
}

func (s *nodeAccessService) Resolve(ctx context.Context, roleId int64, nodeId string) (NodeAccess, error) {
	// A superadmin role has full access to every node (mirrors mymatasan's superadmin),
	// without needing ownership or an explicit grant.
	if s.isSuperadminRole(ctx, roleId) {
		return normalizeAccess(NodeAccess{CanWrite: true}), nil
	}
	owns, err := s.OwnsNode(ctx, roleId, nodeId)
	if err != nil {
		return NodeAccess{}, err
	}
	if owns {
		return normalizeAccess(NodeAccess{CanWrite: true}), nil
	}
	grant, err := s.getGrant(ctx, roleId, nodeId)
	if err != nil {
		return NodeAccess{}, err
	}
	if grant == nil {
		return NodeAccess{}, nil
	}
	// Normalise the ladder on read too, so a stale or hand-edited row cannot express
	// something incoherent (write without read).
	return normalizeAccess(NodeAccess{
		CanRead:    grant.CanRead,
		CanOperate: grant.CanOperate,
		CanWrite:   grant.CanWrite,
	}), nil
}

// isSuperadminRole reports whether roleId is a superadmin role (nil-safe — returns
// false when no roles service is wired, e.g. in unit tests).
func (s *nodeAccessService) isSuperadminRole(ctx context.Context, roleId int64) bool {
	if s.roles == nil || roleId == 0 {
		return false
	}
	role, err := s.roles.GetById(ctx, roleId)
	return err == nil && role != nil && role.IsSuperadmin
}

func (s *nodeAccessService) OwnsNode(ctx context.Context, roleId int64, nodeId string) (bool, error) {
	if roleId == 0 {
		return false, nil
	}
	node, err := s.nodes.GetByUnique(ctx, "", "node_id", nodeId)
	if err != nil {
		if isNoResultFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return node != nil && node.OwnerRoleId == roleId, nil
}

func (s *nodeAccessService) ListForNode(ctx context.Context, nodeId string) ([]*entities.NodeAccessGrant, error) {
	rows, _, err := s.grants.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeId}}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.NodeAccessGrant{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *nodeAccessService) ListForRole(ctx context.Context, roleId int64) ([]*entities.NodeAccessGrant, error) {
	rows, _, err := s.grants.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: roleId}}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.NodeAccessGrant{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *nodeAccessService) Set(ctx context.Context, grant entities.NodeAccessGrant) (*entities.NodeAccessGrant, error) {
	// Enforce the escalation ladder: admin implies operator implies viewer. A grant that
	// says "may delete footage but may not watch it" is not a policy anybody means.
	ladder := normalizeAccess(NodeAccess{CanRead: grant.CanRead, CanOperate: grant.CanOperate, CanWrite: grant.CanWrite})
	grant.CanRead, grant.CanOperate, grant.CanWrite = ladder.CanRead, ladder.CanOperate, ladder.CanWrite

	now := time.Now().Unix()
	existing, err := s.getGrant(ctx, grant.RoleId, grant.NodeId)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.CanRead = grant.CanRead
		existing.CanOperate = grant.CanOperate
		existing.CanWrite = grant.CanWrite
		existing.UpdatedBy = grant.UpdatedBy
		existing.UpdatedAt = now
		if _, err := s.grants.UpdateById(ctx, "", *existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	grant.CreatedAt = now
	grant.UpdatedAt = now
	id, err := s.grants.Create(ctx, "", grant)
	if err != nil {
		return nil, err
	}
	grant.Id = int64(id)
	return &grant, nil
}

func (s *nodeAccessService) GrantById(ctx context.Context, id int64) (*entities.NodeAccessGrant, error) {
	rows, _, err := s.grants.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "Id", Compare: sqldataenums.Equal, Value: id}}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *nodeAccessService) Delete(ctx context.Context, id int64) error {
	_, err := s.grants.DeleteById(ctx, "", uint64(id))
	return err
}

// getGrant returns the grant for (roleId, nodeId), or nil when none exists.
func (s *nodeAccessService) getGrant(ctx context.Context, roleId int64, nodeId string) (*entities.NodeAccessGrant, error) {
	rows, _, err := s.grants.Get(ctx, "", 1, 0, []sqldataenums.Filter{
		{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: roleId},
		{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeId},
	}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}
