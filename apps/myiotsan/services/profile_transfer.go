package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Profile import/export: a device type is a small declarative document, so it should be
// portable.
//
// The point is not backup. It is that tuning a profile — getting a deadband right for a
// particular sensor in a particular building — is real work, and an integrator who does it once
// should be able to carry it to the next site rather than rediscovering it. It is the same
// reasoning as mymatasan's .mmskill export.
//
// The format is deliberately plain JSON with a version field. A config format nobody can read
// in a text editor is a config format nobody will trust.

// profileExportVersion guards the format. An importer that meets a version it does not know
// refuses rather than guessing — a half-understood profile silently mis-decodes every reading
// the devices using it will ever send.
const profileExportVersion = 1

// ProfileExport is the portable form of a device type.
type ProfileExport struct {
	Version       int                `json:"version"`
	Slug          string             `json:"slug"`
	Name          string             `json:"name"`
	Vendor        string             `json:"vendor"`
	Description   string             `json:"description"`
	TopicTemplate string             `json:"topicTemplate"`
	PayloadFormat string             `json:"payloadFormat"`
	Keys          []SaveTelemetryKey `json:"keys"`
}

// Export renders a profile as a portable document.
func (s *ProfileService) Export(ctx context.Context, id int64) (*ProfileExport, error) {
	detail, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &ProfileExport{
		Version:       profileExportVersion,
		Slug:          detail.Profile.Slug,
		Name:          detail.Profile.Name,
		Vendor:        detail.Profile.Vendor,
		Description:   detail.Profile.Description,
		TopicTemplate: detail.Profile.TopicTemplate,
		PayloadFormat: detail.Profile.PayloadFormat,
		Keys:          make([]SaveTelemetryKey, 0, len(detail.Keys)),
	}
	for _, k := range detail.Keys {
		out.Keys = append(out.Keys, SaveTelemetryKey{
			Key:              k.Key,
			Label:            k.Label,
			Unit:             k.Unit,
			DataType:         k.DataType,
			JsonPath:         k.JsonPath,
			Deadband:         k.Deadband,
			HeartbeatSeconds: k.HeartbeatSeconds,
			Min:              k.Min,
			Max:              k.Max,
		})
	}
	return out, nil
}

// Import creates a profile from a document.
//
// An imported profile is NEVER builtin, whatever the document claims — "builtin" means "shipped
// by us and therefore undeletable", and a file off the internet does not get to assert that
// about itself.
//
// A slug collision is reported rather than silently overwritten. Quietly replacing a profile
// would re-point every device using it at different decoding rules, which is a data-corruption
// bug that looks like an import succeeding.
func (s *ProfileService) Import(ctx context.Context, raw []byte, actor int64) (*ProfileDetail, error) {
	var doc ProfileExport
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("not a profile document: %w", err)
	}
	if doc.Version != profileExportVersion {
		return nil, fmt.Errorf("unsupported profile format version %d (this build understands %d)", doc.Version, profileExportVersion)
	}
	if strings.TrimSpace(doc.Slug) == "" {
		return nil, fmt.Errorf("the profile has no slug")
	}
	if len(doc.Keys) == 0 {
		return nil, fmt.Errorf("the profile declares no telemetry keys, so nothing a device sends could be decoded")
	}

	existing, err := s.profiles.GetByUnique(ctx, "", "slug", doc.Slug)
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("a profile with the slug %q already exists — rename the import, or edit the existing one", doc.Slug)
	}

	return s.Create(ctx, SaveProfileRequest{
		Slug:          doc.Slug,
		Name:          doc.Name,
		Vendor:        doc.Vendor,
		Description:   doc.Description,
		TopicTemplate: doc.TopicTemplate,
		PayloadFormat: doc.PayloadFormat,
		Keys:          doc.Keys,
	}, actor)
}
