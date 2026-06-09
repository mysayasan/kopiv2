package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/stream"
)

const runtimeSettingsKey = "runtime"

type runtimeSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults RuntimeSettings
}

// NewRuntimeSettingsService creates a runtime settings service seeded by app config defaults.
func NewRuntimeSettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting], defaults RuntimeSettings) IRuntimeSettingsService {
	return &runtimeSettingsService{repo: repo, defaults: normalizeRuntimeSettings(defaults)}
}

func (s *runtimeSettingsService) Get(ctx context.Context) (RuntimeSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", runtimeSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.createDefaults(ctx)
		}
		return RuntimeSettings{}, err
	}

	settings := RuntimeSettings{}
	if strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return RuntimeSettings{}, fmt.Errorf("parse runtime settings failed: %w", err)
		}
	}
	settings = normalizeRuntimeSettings(settings)
	return settings, nil
}

func (s *runtimeSettingsService) Save(ctx context.Context, settings RuntimeSettings) (RuntimeSettings, error) {
	settings = normalizeRuntimeSettings(settings)
	if err := validateRuntimeSettings(settings); err != nil {
		return RuntimeSettings{}, err
	}

	payload, err := json.Marshal(settings)
	if err != nil {
		return RuntimeSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", runtimeSettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return RuntimeSettings{}, err
		}
		return settings, nil
	}
	if err != nil && !isNoResultFoundErr(err) {
		return RuntimeSettings{}, err
	}

	if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       runtimeSettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return RuntimeSettings{}, err
	}
	return settings, nil
}

func (s *runtimeSettingsService) Reset(ctx context.Context) (RuntimeSettings, error) {
	return s.Save(ctx, s.defaults)
}

func (s *runtimeSettingsService) Stream(ctx context.Context) (StreamSettings, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return StreamSettings{}, err
	}
	return settings.Stream, nil
}

func (s *runtimeSettingsService) Decoder(ctx context.Context) (DecoderSettings, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return DecoderSettings{}, err
	}
	return settings.Decoder, nil
}

func (s *runtimeSettingsService) createDefaults(ctx context.Context) (RuntimeSettings, error) {
	return s.Save(ctx, s.defaults)
}

func normalizeRuntimeSettings(settings RuntimeSettings) RuntimeSettings {
	if settings.Stream.WebRTC.ICEServers == nil {
		settings.Stream.WebRTC.ICEServers = []stream.ICEServer{}
	}
	return settings
}

func validateRuntimeSettings(settings RuntimeSettings) error {
	for idx, server := range settings.Stream.WebRTC.ICEServers {
		if len(server.URLs) == 0 {
			return fmt.Errorf("stream.webrtc.iceServers[%d].urls is required", idx)
		}
		for urlIdx, rawURL := range server.URLs {
			if strings.TrimSpace(rawURL) == "" {
				return fmt.Errorf("stream.webrtc.iceServers[%d].urls[%d] is required", idx, urlIdx)
			}
		}
	}
	return nil
}
