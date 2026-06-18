package services

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Detection class kinds and sources.
const (
	ClassKindObject = "object"
	ClassKindHazard = "hazard"
	ClassKindGroup  = "group"

	ClassSourceBuiltin = "builtin"
	ClassSourceTrained = "trained"
	ClassSourceGroup   = "group"
)

// DetectionClassRequest is the create/update shape for a registry class.
type DetectionClassRequest struct {
	Id          int64    `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Icon        string   `json:"icon"`
	Kind        string   `json:"kind"`
	Members     []string `json:"members"`
	Source      string   `json:"source"`
	IsEnabled   *bool    `json:"isEnabled"`
}

// DetectionClassView is the display-ready shape returned to the UI: the stored
// row plus the resolved raw labels the class expands to.
type DetectionClassView struct {
	entities.DetectionClass
	MemberList     []string `json:"memberList"`
	ResolvedLabels []string `json:"resolvedLabels"`
}

// LabelCatalogEntry is one raw model label the detector can emit, tagged with its
// source and the category (group) it belongs to. The Object Classes label picker
// uses this to group + search labels so it scales to thousands of entries, instead
// of inferring structure from a flat list of strings.
type LabelCatalogEntry struct {
	Label   string `json:"label"`   // canonical slug the model emits (e.g. "traffic_light")
	Display string `json:"display"` // human-friendly name (e.g. "Traffic Light")
	Source  string `json:"source"`  // "stock" (built-in) | "trained"
	Group   string `json:"group"`   // category the label is filed under, for grouped browse
}

// ClassResolver resolves rule target class slugs to concrete model labels. The
// vision monitor depends on this (not the concrete service) so detection rules
// can reference registry slugs/groups that expand live.
type ClassResolver interface {
	ResolveLabels(ctx context.Context, slugs []string) []string
}

// IDetectionClassService manages the detection class registry.
type IDetectionClassService interface {
	ClassResolver
	List(ctx context.Context) ([]*DetectionClassView, error)
	LabelCatalog(ctx context.Context) ([]LabelCatalogEntry, error)
	Save(ctx context.Context, req DetectionClassRequest, userId int64) (*entities.DetectionClass, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	EnsureBuiltins(ctx context.Context, classMap map[string][]string) error
	UpsertTrained(ctx context.Context, labels []string, userId int64) error
}

type detectionClassService struct {
	repo dbsql.IGenericRepo[entities.DetectionClass]
	now  func() time.Time
}

// NewDetectionClassService creates the class registry service.
func NewDetectionClassService(repo dbsql.IGenericRepo[entities.DetectionClass]) IDetectionClassService {
	return &detectionClassService{repo: repo, now: time.Now}
}

// builtinSeed defines the classes seeded on first run. classMap members are
// merged in (config is the source of truth for which labels each maps to); these
// defaults cover the kind/display/icon and a fallback member when the config has
// no entry.
var builtinSeed = []struct {
	Name    string
	Display string
	Icon    string
	Kind    string
}{
	{"person", "Person", "user", ClassKindObject},
	{"vehicle", "Vehicle", "car", ClassKindObject},
	{"animal", "Animal", "paw", ClassKindObject},
	{"fire", "Fire", "flame", ClassKindHazard},
	{"smoke", "Smoke", "cloud", ClassKindHazard},
}

func (s *detectionClassService) List(ctx context.Context) ([]*DetectionClassView, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		return nil, err
	}
	index := indexClasses(rows)
	views := make([]*DetectionClassView, 0, len(rows))
	for _, row := range rows {
		views = append(views, &DetectionClassView{
			DetectionClass: *row,
			MemberList:     decodeMembers(row.Members),
			ResolvedLabels: resolveLabels(index, []string{row.Name}),
		})
	}
	return views, nil
}

// LabelCatalog returns the de-duplicated set of raw labels known to the system,
// each tagged with its source and the category (group) it belongs to. The Object
// Classes label picker uses it to group + search labels at scale.
//
// Today the catalog is derived entirely from the class registry — the source of
// truth for which labels exist and how they're grouped. To surface a large stock
// vocabulary whose labels aren't all mapped to a category yet (e.g. when swapping
// in a bigger stock model), add a stock-label provider that yields {label, group}
// from the model's class list and merge it into buildLabelCatalog; registry
// mappings should win for grouping a label a user has explicitly filed.
func (s *detectionClassService) LabelCatalog(ctx context.Context) ([]LabelCatalogEntry, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		return nil, err
	}
	return buildLabelCatalog(rows), nil
}

// stockLabelGroups is the curated set of surveillance-relevant labels the stock
// YOLO (COCO) model emits, mapped to a browse group. Labels are in the canonical
// form the detector produces (lowercase, spaces preserved — e.g. "cell phone"),
// so a category built from them matches detections exactly. Group names match the
// built-in category displays (Person/Vehicle/Animal) so a mapped and an unmapped
// label of the same kind don't split into two near-identical groups.
//
// To grow the stock vocabulary, add labels here (or, when swapping in a different
// stock model, source this from the model's class list). COCO/OpenImages ship
// supercategories you can reuse as groups.
var stockLabelGroups = map[string]string{
	// People
	"person": "Person",
	// Vehicles
	"bicycle": "Vehicle", "car": "Vehicle", "motorcycle": "Vehicle", "bus": "Vehicle",
	"train": "Vehicle", "truck": "Vehicle", "boat": "Vehicle", "airplane": "Vehicle",
	// Bags / abandoned-object surveillance
	"backpack": "Bags & Belongings", "handbag": "Bags & Belongings",
	"suitcase": "Bags & Belongings", "umbrella": "Bags & Belongings",
	// Possible weapons / threat proxies
	"knife": "Possible Weapons", "scissors": "Possible Weapons", "baseball bat": "Possible Weapons",
	// High-value items (theft)
	"cell phone": "Electronics", "laptop": "Electronics",
	// Animals
	"bird": "Animal", "cat": "Animal", "dog": "Animal", "horse": "Animal", "sheep": "Animal",
	"cow": "Animal", "elephant": "Animal", "bear": "Animal", "zebra": "Animal", "giraffe": "Animal",
	// Street furniture / context
	"traffic light": "Street & Outdoor", "fire hydrant": "Street & Outdoor",
	"stop sign": "Street & Outdoor", "parking meter": "Street & Outdoor", "bench": "Street & Outdoor",
}

// buildLabelCatalog merges the stock model vocabulary with the class registry into
// per-label catalog entries. The stock vocabulary seeds every detectable label
// (so labels show even before they're mapped to a category); the registry then
// overrides a label's group with the category a user filed it under, and upgrades
// its source to "trained" for user-trained classes.
func buildLabelCatalog(rows []*entities.DetectionClass) []LabelCatalogEntry {
	type meta struct{ group, source string }

	// Registry mappings: the first non-group class a label appears under wins its
	// group; a trained mapping upgrades the source.
	registry := map[string]meta{}
	for _, row := range rows {
		if row == nil || row.Kind == ClassKindGroup {
			continue
		}
		source := "stock"
		if row.Source == ClassSourceTrained {
			source = "trained"
		}
		group := strings.TrimSpace(row.DisplayName)
		if group == "" {
			group = titleizeSlug(row.Name)
		}
		for _, label := range decodeMembers(row.Members) {
			if existing, seen := registry[label]; seen {
				if existing.source != "trained" && source == "trained" {
					registry[label] = meta{group: group, source: source}
				}
				continue
			}
			registry[label] = meta{group: group, source: source}
		}
	}

	// Final set: stock vocabulary, overridden by registry mappings where present.
	final := map[string]meta{}
	for label, group := range stockLabelGroups {
		final[label] = meta{group: group, source: "stock"}
	}
	for label, m := range registry {
		final[label] = m
	}

	labels := make([]string, 0, len(final))
	for label := range final {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	out := make([]LabelCatalogEntry, 0, len(labels))
	for _, label := range labels {
		m := final[label]
		out = append(out, LabelCatalogEntry{
			Label:   label,
			Display: titleizeSlug(label),
			Source:  m.source,
			Group:   m.group,
		})
	}
	return out
}

func (s *detectionClassService) Save(ctx context.Context, req DetectionClassRequest, userId int64) (*entities.DetectionClass, error) {
	name := normalizeSlug(req.Name)
	if name == "" {
		return nil, errors.New("class name is required")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case ClassKindObject, ClassKindHazard, ClassKindGroup:
	case "":
		kind = ClassKindObject
	default:
		return nil, errors.New("kind must be object, hazard, or group")
	}
	members := normalizeMembers(req.Members, kind == ClassKindGroup)
	if kind == ClassKindGroup && len(members) == 0 {
		return nil, errors.New("a group must reference at least one class")
	}
	membersJSON, _ := json.Marshal(members)

	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		if kind == ClassKindGroup {
			source = ClassSourceGroup
		} else {
			source = ClassSourceTrained
		}
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	row := entities.DetectionClass{
		Id:          req.Id,
		Name:        name,
		DisplayName: strings.TrimSpace(req.DisplayName),
		Icon:        strings.TrimSpace(req.Icon),
		Kind:        kind,
		Members:     string(membersJSON),
		Source:      source,
		IsEnabled:   isEnabled,
	}
	if row.DisplayName == "" {
		row.DisplayName = titleizeSlug(name)
	}

	now := s.now().UTC().Unix()
	row.UpdatedBy = userId
	row.UpdatedAt = now
	if row.Id > 0 {
		existing, err := s.repo.GetById(ctx, "", uint64(row.Id))
		if err != nil {
			return nil, err
		}
		// Built-in rows keep their identity (name/kind/source) but allow display
		// tweaks and member edits.
		if existing.Source == ClassSourceBuiltin {
			row.Name = existing.Name
			row.Kind = existing.Kind
			row.Source = existing.Source
		}
		row.CreatedAt = existing.CreatedAt
		row.CreatedBy = existing.CreatedBy
		if _, err := s.repo.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
		return &row, nil
	}
	row.CreatedBy = userId
	row.CreatedAt = now
	id, err := s.repo.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *detectionClassService) Delete(ctx context.Context, id uint64) (uint64, error) {
	existing, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	if existing.Source == ClassSourceBuiltin {
		return 0, errors.New("built-in classes cannot be deleted")
	}
	return s.repo.DeleteById(ctx, "", id)
}

// EnsureBuiltins seeds the built-in object/hazard classes once. It only inserts
// rows whose name does not already exist, so it is safe to run on every startup
// and never clobbers user edits.
func (s *detectionClassService) EnsureBuiltins(ctx context.Context, classMap map[string][]string) error {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, row := range rows {
		existing[row.Name] = true
	}
	now := s.now().UTC().Unix()
	for _, seed := range builtinSeed {
		if existing[seed.Name] {
			continue
		}
		members := normalizeMembers(classMap[seed.Name], false)
		if len(members) == 0 {
			members = []string{seed.Name}
		}
		membersJSON, _ := json.Marshal(members)
		row := entities.DetectionClass{
			Name:        seed.Name,
			DisplayName: seed.Display,
			Icon:        seed.Icon,
			Kind:        seed.Kind,
			Members:     string(membersJSON),
			Source:      ClassSourceBuiltin,
			IsEnabled:   true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := s.repo.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

// UpsertTrained registers each trained model label as a class (source "trained")
// when it does not already exist, so newly trained classes appear in the rule
// target picker immediately.
func (s *detectionClassService) UpsertTrained(ctx context.Context, labels []string, userId int64) error {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, row := range rows {
		existing[row.Name] = true
	}
	now := s.now().UTC().Unix()
	for _, label := range normalizeMembers(labels, false) {
		if existing[label] {
			continue
		}
		membersJSON, _ := json.Marshal([]string{label})
		row := entities.DetectionClass{
			Name:        label,
			DisplayName: titleizeSlug(label),
			Kind:        ClassKindObject,
			Members:     string(membersJSON),
			Source:      ClassSourceTrained,
			IsEnabled:   true,
			CreatedBy:   userId,
			CreatedAt:   now,
			UpdatedBy:   userId,
			UpdatedAt:   now,
		}
		if _, err := s.repo.Create(ctx, "", row); err != nil {
			return err
		}
		existing[label] = true
	}
	return nil
}

func (s *detectionClassService) ResolveLabels(ctx context.Context, slugs []string) []string {
	if len(slugs) == 0 {
		return nil
	}
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		// On error, fall back to treating slugs as raw labels so detection still works.
		return normalizeMembers(slugs, false)
	}
	return resolveLabels(indexClasses(rows), slugs)
}

func indexClasses(rows []*entities.DetectionClass) map[string]*entities.DetectionClass {
	index := make(map[string]*entities.DetectionClass, len(rows))
	for _, row := range rows {
		index[row.Name] = row
	}
	return index
}

// resolveLabels flattens slugs into concrete raw model labels: group rows expand
// into their child class slugs (recursively), object/hazard rows expand into
// their member labels, and a slug with no registry entry is treated as a raw
// label. "*" passes through. Cycles are guarded; output is order-preserving and
// de-duplicated.
func resolveLabels(index map[string]*entities.DetectionClass, slugs []string) []string {
	out := make([]string, 0, len(slugs))
	seen := map[string]bool{}
	visiting := map[string]bool{}

	var add func(label string)
	add = func(label string) {
		if label != "" && !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}

	var walk func(slug string)
	walk = func(slug string) {
		slug = strings.ToLower(strings.TrimSpace(slug))
		if slug == "" {
			return
		}
		if slug == "*" {
			add("*")
			return
		}
		row, ok := index[slug]
		if !ok {
			add(slug) // raw label
			return
		}
		if visiting[slug] {
			return // cycle guard
		}
		visiting[slug] = true
		defer delete(visiting, slug)
		members := decodeMembers(row.Members)
		if len(members) == 0 {
			add(slug)
			return
		}
		if row.Kind == ClassKindGroup {
			for _, child := range members {
				walk(child)
			}
			return
		}
		for _, label := range members {
			add(label)
		}
	}

	for _, slug := range slugs {
		walk(slug)
	}
	return out
}

func decodeMembers(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var members []string
	if err := json.Unmarshal([]byte(value), &members); err != nil {
		return nil
	}
	return normalizeMembers(members, false)
}

// normalizeMembers lowercases, trims, and de-duplicates a member list. When
// allowStar is false a literal "*" is dropped (only behavior class lists allow it).
func normalizeMembers(members []string, allowStar bool) []string {
	out := make([]string, 0, len(members))
	seen := map[string]bool{}
	for _, m := range members {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || seen[m] {
			continue
		}
		if m == "*" && !allowStar {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func titleizeSlug(slug string) string {
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
