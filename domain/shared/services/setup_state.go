package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// SetupStateKey is the RuntimeSetting key every app stores its first-run flag
// under. It is exported because restore paths write the same row directly to
// stop the wizard reappearing after a restore.
const SetupStateKey = "setup.state"

// SetupState tracks whether the first-run setup wizard has been completed. It is
// persisted as a single key-value row so it survives restarts and is shared
// across browsers rather than living in one browser's localStorage.
type SetupState struct {
	Completed   bool  `json:"completed"`
	CompletedAt int64 `json:"completedAt"`
}

// ISetupStateService reads and records first-run setup completion.
type ISetupStateService interface {
	Get(ctx context.Context) (SetupState, error)
	Complete(ctx context.Context) (SetupState, error)
}

type setupStateService struct {
	repo dbsql.IGenericRepo[entities.RuntimeSetting]
}

// NewSetupStateService stores the setup flag in the shared runtime-setting table
// under a dedicated key.
func NewSetupStateService(repo dbsql.IGenericRepo[entities.RuntimeSetting]) ISetupStateService {
	return &setupStateService{repo: repo}
}

func (s *setupStateService) Get(ctx context.Context) (SetupState, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", SetupStateKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return SetupState{}, nil
		}
		return SetupState{}, err
	}
	if row == nil || row.Value == "" {
		return SetupState{}, nil
	}
	var state SetupState
	if err := json.Unmarshal([]byte(row.Value), &state); err != nil {
		// A corrupt value is treated as "not completed" so setup re-runs rather
		// than wedging on a parse error.
		return SetupState{}, nil
	}
	return state, nil
}

func (s *setupStateService) Complete(ctx context.Context) (SetupState, error) {
	state := SetupState{Completed: true, CompletedAt: time.Now().UTC().Unix()}
	payload, err := json.Marshal(state)
	if err != nil {
		return SetupState{}, err
	}
	row, err := s.repo.GetByUnique(ctx, "", "key", SetupStateKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return state, s.insert(ctx, string(payload), state.CompletedAt)
		}
		return SetupState{}, err
	}
	// A repo that signals "missing" by returning a zero row rather than an error
	// would otherwise UPDATE id 0 and silently persist nothing.
	if row == nil || row.Id == 0 {
		return state, s.insert(ctx, string(payload), state.CompletedAt)
	}
	row.Value = string(payload)
	row.UpdatedAt = state.CompletedAt
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
		return SetupState{}, err
	}
	return state, nil
}

func (s *setupStateService) insert(ctx context.Context, payload string, ts int64) error {
	_, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       SetupStateKey,
		Value:     payload,
		CreatedAt: ts,
		UpdatedAt: ts,
	})
	return err
}
