package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Flow import/export: a flow is a small declarative document (its graph is already JSON), so it
// should be portable — the same reasoning as a device profile (profile_transfer.go) or a
// mymatasan .mmskill. Designing a flow is real work; an integrator who builds a good "Solar
// system" flow once should be able to carry it to the next site, not redraw it.
//
// The format is plain JSON with a version field. An importer that meets a version it does not know
// refuses rather than guessing.

const flowExportVersion = 1

// FlowExport is the portable form of a flow.
type FlowExport struct {
	Version     int    `json:"version"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// Graph is the node/wire document verbatim. Its device references are natural keys, so the
	// import lands unbound on a new site until its devices exist under those keys — data, not
	// dangling foreign keys.
	Graph string `json:"graph"`
}

// Export renders a flow as a portable document.
func (s *FlowService) Export(ctx context.Context, id int64) (*FlowExport, error) {
	flow, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	return &FlowExport{
		Version:     flowExportVersion,
		Slug:        flow.Slug,
		Name:        flow.Name,
		Description: flow.Description,
		Category:    flow.Category,
		Graph:       flow.Graph,
	}, nil
}

// Import creates a flow from a document.
//
// An imported flow is NEVER builtin, whatever the document claims, and is created DISABLED — a flow
// carried from another site references devices by key that may not exist here yet, and it should
// not start acting until an admin has looked at it and switched it on. A slug collision is reported,
// never silently overwritten.
func (s *FlowService) Import(ctx context.Context, raw []byte, actor int64) (*flowImportResult, error) {
	var doc FlowExport
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("not a flow document: %w", err)
	}
	if doc.Version != flowExportVersion {
		return nil, fmt.Errorf("unsupported flow format version %d (this build understands %d)", doc.Version, flowExportVersion)
	}
	name := strings.TrimSpace(doc.Name)
	if name == "" {
		return nil, fmt.Errorf("the flow has no name")
	}
	if _, err := parseGraph(doc.Graph); err != nil {
		return nil, fmt.Errorf("the flow's graph is invalid: %w", err)
	}
	slug := strings.TrimSpace(doc.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	existing, err := s.getBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("a flow with the slug %q already exists — rename the import, or edit the existing one", slug)
	}

	flow, err := s.Create(ctx, SaveFlowRequest{
		Slug:        slug,
		Name:        name,
		Description: doc.Description,
		Category:    doc.Category,
		Enabled:     false, // imported flows arrive OFF; an admin enables after reviewing
		Graph:       doc.Graph,
	}, actor)
	if err != nil {
		return nil, err
	}
	return &flowImportResult{Flow: flow}, nil
}

type flowImportResult struct {
	Flow any `json:"flow"`
}
