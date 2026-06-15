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

// toEntity maps an infra notification to the persisted entity, serializing the
// arbitrary Data map into the Metadata JSON column.
func toEntity(n infranotif.Notification) entities.Notification {
	metadata := ""
	if len(n.Data) > 0 {
		if raw, err := json.Marshal(n.Data); err == nil {
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
