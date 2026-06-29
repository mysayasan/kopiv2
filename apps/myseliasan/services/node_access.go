package services

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// NodeAccess is the resolved per-(role, node) capability. CanWrite implies CanRead.
type NodeAccess struct {
	CanRead  bool
	CanWrite bool
}

// Role maps the capability to the tunnel's asserted role: admin when writable,
// viewer when read-only, "" when no access.
func (a NodeAccess) Role() string {
	switch {
	case a.CanWrite:
		return "admin"
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
}

// NewNodeAccessService builds the access service over the control-plane database.
func NewNodeAccessService(db dbsql.IDbCrud) INodeAccessService {
	return newNodeAccessService(
		dbsql.NewGenericRepo[entities.NodeAccessGrant](db),
		dbsql.NewGenericRepo[entities.ManagedNode](db),
	)
}

func newNodeAccessService(
	grants dbsql.IGenericRepo[entities.NodeAccessGrant],
	nodes dbsql.IGenericRepo[entities.ManagedNode],
) *nodeAccessService {
	return &nodeAccessService{grants: grants, nodes: nodes}
}

func (s *nodeAccessService) Resolve(ctx context.Context, roleId int64, nodeId string) (NodeAccess, error) {
	owns, err := s.OwnsNode(ctx, roleId, nodeId)
	if err != nil {
		return NodeAccess{}, err
	}
	if owns {
		return NodeAccess{CanRead: true, CanWrite: true}, nil
	}
	grant, err := s.getGrant(ctx, roleId, nodeId)
	if err != nil {
		return NodeAccess{}, err
	}
	if grant == nil {
		return NodeAccess{}, nil
	}
	// write implies read, even if a stale row says otherwise.
	return NodeAccess{CanRead: grant.CanRead || grant.CanWrite, CanWrite: grant.CanWrite}, nil
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

func (s *nodeAccessService) Set(ctx context.Context, grant entities.NodeAccessGrant) (*entities.NodeAccessGrant, error) {
	// write implies read — a writable grant is always readable.
	if grant.CanWrite {
		grant.CanRead = true
	}
	now := time.Now().Unix()
	existing, err := s.getGrant(ctx, grant.RoleId, grant.NodeId)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.CanRead = grant.CanRead
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
