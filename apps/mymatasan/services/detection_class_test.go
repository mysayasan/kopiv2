package services

import (
	"reflect"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

func classRow(name, kind string, members ...string) *entities.DetectionClass {
	encoded := "[]"
	if len(members) > 0 {
		encoded = `["` + joinQuoted(members) + `"]`
	}
	return &entities.DetectionClass{Name: name, Kind: kind, Members: encoded}
}

func joinQuoted(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += `","`
		}
		out += item
	}
	return out
}

func findCatalog(entries []LabelCatalogEntry, label string) (LabelCatalogEntry, bool) {
	for _, e := range entries {
		if e.Label == label {
			return e, true
		}
	}
	return LabelCatalogEntry{}, false
}

func TestBuildLabelCatalogMergesStockAndRegistry(t *testing.T) {
	rows := []*entities.DetectionClass{
		{Name: "vehicle", DisplayName: "Vehicle", Kind: ClassKindObject, Source: ClassSourceBuiltin, Members: `["car","truck"]`},
		{Name: "papa", DisplayName: "Papa", Kind: ClassKindObject, Source: ClassSourceTrained, Members: `["papa"]`},
		{Name: "delivery", DisplayName: "Delivery", Kind: ClassKindGroup, Source: ClassSourceGroup, Members: `["vehicle"]`},
	}
	got := buildLabelCatalog(rows)

	// Stock-only label (not in the registry) still appears, grouped.
	if e, ok := findCatalog(got, "knife"); !ok || e.Group != "Possible Weapons" || e.Source != "stock" {
		t.Fatalf("knife = %#v, ok=%v — want stock/Possible Weapons", e, ok)
	}
	// A registry mapping wins the group for a stock label.
	if e, ok := findCatalog(got, "car"); !ok || e.Group != "Vehicle" || e.Display != "Car" {
		t.Fatalf("car = %#v, ok=%v — want Vehicle/Car", e, ok)
	}
	// Trained label is tagged trained and grouped under its category.
	if e, ok := findCatalog(got, "papa"); !ok || e.Source != "trained" || e.Group != "Papa" {
		t.Fatalf("papa = %#v, ok=%v — want trained/Papa", e, ok)
	}
	// Multi-word stock label keeps its spaced form and a titleized display.
	if e, ok := findCatalog(got, "cell phone"); !ok || e.Display != "Cell Phone" {
		t.Fatalf("cell phone = %#v, ok=%v", e, ok)
	}
	// Group rows never leak into the catalog as labels.
	if _, ok := findCatalog(got, "delivery"); ok {
		t.Fatalf("group 'delivery' should not appear as a label")
	}
}

func TestBuildLabelCatalogTrainedUpgradesSource(t *testing.T) {
	// "dog" is a stock label; once a trained class also maps it, the entry should be
	// tagged "trained" so the UI can mark user-trained labels.
	rows := []*entities.DetectionClass{
		{Name: "rex", DisplayName: "Rex", Kind: ClassKindObject, Source: ClassSourceTrained, Members: `["dog"]`},
	}
	got := buildLabelCatalog(rows)
	if e, ok := findCatalog(got, "dog"); !ok || e.Source != "trained" || e.Group != "Rex" {
		t.Fatalf("dog = %#v, ok=%v — want trained/Rex", e, ok)
	}
}

func TestResolveLabelsExpandsGroupsAndObjects(t *testing.T) {
	index := indexClasses([]*entities.DetectionClass{
		classRow("vehicle", ClassKindObject, "car", "truck"),
		classRow("courier", ClassKindObject, "courier"),
		classRow("delivery", ClassKindGroup, "courier", "vehicle"),
	})

	got := resolveLabels(index, []string{"delivery"})
	want := []string{"courier", "car", "truck"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveLabels(delivery) = %v, want %v", got, want)
	}
}

func TestResolveLabelsTreatsUnknownSlugAsRawLabel(t *testing.T) {
	index := indexClasses(nil)
	got := resolveLabels(index, []string{"person", "*"})
	want := []string{"person", "*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveLabels passthrough = %v, want %v", got, want)
	}
}

func TestResolveLabelsGuardsCycles(t *testing.T) {
	// g1 -> g2 -> g1 must terminate and yield no labels (groups with no objects).
	index := indexClasses([]*entities.DetectionClass{
		classRow("g1", ClassKindGroup, "g2"),
		classRow("g2", ClassKindGroup, "g1"),
	})
	got := resolveLabels(index, []string{"g1"})
	if len(got) != 0 {
		t.Fatalf("expected cycle to resolve to no labels, got %v", got)
	}
}

func TestResolveLabelsDedupes(t *testing.T) {
	index := indexClasses([]*entities.DetectionClass{
		classRow("vehicle", ClassKindObject, "car", "truck"),
		classRow("cars", ClassKindObject, "car"),
	})
	got := resolveLabels(index, []string{"vehicle", "cars"})
	want := []string{"car", "truck"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveLabels dedupe = %v, want %v", got, want)
	}
}
