// Package notification wires the app-agnostic notification engine
// (infra/notification) to a database-backed store and a set of default delivery
// channels, so any app can adopt one unified notification feed in a few lines.
package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	infranotif "github.com/mysayasan/kopiv2/infra/notification"
)

// repoStore persists notifications using a generic repository. It implements
// infra/notification.Store.
type repoStore struct {
	repo dbsql.IGenericRepo[entities.Notification]
}

// NewStore returns an infra/notification.Store backed by a generic repository.
func NewStore(repo dbsql.IGenericRepo[entities.Notification]) infranotif.Store {
	return &repoStore{repo: repo}
}

func (s *repoStore) Save(ctx context.Context, n infranotif.Notification) (int64, error) {
	entity := toEntity(n)
	now := time.Now().UTC().Unix()
	entity.CreatedAt = now
	entity.UpdatedAt = now
	id, err := s.repo.Create(ctx, "", entity)
	if err != nil {
		return 0, err
	}
	return int64(id), nil
}

// OriginIDKey is the reserved Data/Metadata key under which a notification's engine id
// (infra/notification.Notification.ID) is persisted. The numeric DB primary key differs
// between stores, but this id travels with the notification on the live control-channel
// push too — so a fleet control plane replaying a node's missed notifications can dedup a
// pulled row against a live-pushed one by a single stable key on both paths.
const OriginIDKey = "__oid"

// toEntity maps an infra notification to the persisted entity, serializing the
// arbitrary Data map into the Metadata JSON column. The engine id is folded into the
// serialized Data under OriginIDKey so it survives persistence (see OriginIDKey).
func toEntity(n infranotif.Notification) entities.Notification {
	metadata := ""
	data := n.Data
	if n.ID != "" {
		merged := make(map[string]any, len(data)+1)
		for k, v := range data {
			merged[k] = v
		}
		merged[OriginIDKey] = n.ID
		data = merged
	}
	if len(data) > 0 {
		if raw, err := json.Marshal(data); err == nil {
			metadata = string(raw)
		}
	}
	return entities.Notification{
		Category: n.Category,
		Severity: string(n.Severity),
		Title:    n.Title,
		Body:     n.Body,
		Source:   n.Source,
		CameraId: n.CameraId,
		RefType:  n.RefType,
		RefId:    n.RefId,
		Link:     n.Link,
		Metadata: metadata,
	}
}

// listSorters returns the default newest-first ordering for notification lists.
func listSorters() []sqldataenums.Sorter {
	return []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}}
}
