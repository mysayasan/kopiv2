package postgres

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestNormalizeScannedValueConvertsNullStringToEmptyStringPointer(t *testing.T) {
	value := normalizeScannedValue(&sql.NullString{}, reflect.TypeOf(""))

	str, ok := value.(*string)
	if !ok {
		t.Fatalf("expected *string, got %T", value)
	}
	if *str != "" {
		t.Fatalf("expected empty string, got %q", *str)
	}
}

func TestNormalizeScannedValueConvertsValidNullStringToStringPointer(t *testing.T) {
	value := normalizeScannedValue(&sql.NullString{String: "avatar", Valid: true}, reflect.TypeOf(""))

	str, ok := value.(*string)
	if !ok {
		t.Fatalf("expected *string, got %T", value)
	}
	if *str != "avatar" {
		t.Fatalf("expected avatar, got %q", *str)
	}
}
