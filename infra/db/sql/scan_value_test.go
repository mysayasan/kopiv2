package dbsql

import (
	"database/sql"
	"reflect"
	"testing"
)

type accessTier int32

// A NULL in a numeric column is the case that used to take a whole SELECT down on postgres
// and mariadb: the auto-migrator adds a column without a default, every existing row is
// NULL, and a raw *int64 destination cannot hold one.
func TestScanDestinationsAreNullTolerant(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field interface{}
	}{
		{"int64", int64(0)},
		{"int", int(0)},
		{"int32 enum", accessTier(0)},
		{"float64", float64(0)},
		{"float32", float32(0)},
		{"uint64", uint64(0)},
		{"string", ""},
		{"bool", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fieldType := reflect.TypeOf(tc.field)
			dest := ScanDestinationForField(fieldType)
			scanner, ok := dest.(sql.Scanner)
			if !ok {
				t.Fatalf("destination %T cannot receive a NULL", dest)
			}
			if err := scanner.Scan(nil); err != nil {
				t.Fatalf("scanning NULL: %v", err)
			}
			got := reflect.ValueOf(NormalizeScannedValue(dest, fieldType)).Elem()
			if got.Type() != fieldType {
				t.Fatalf("normalized to %s, want %s", got.Type(), fieldType)
			}
			if !got.IsZero() {
				t.Fatalf("NULL normalized to %v, want the zero value", got.Interface())
			}
		})
	}
}

func TestNormalizeScannedValueKeepsRealValues(t *testing.T) {
	if got := *NormalizeScannedValue(&sql.NullInt64{Int64: 1788227533, Valid: true}, reflect.TypeOf(int64(0))).(*int64); got != 1788227533 {
		t.Fatalf("int64 = %d", got)
	}
	if got := *NormalizeScannedValue(&sql.NullInt64{Int64: 2, Valid: true}, reflect.TypeOf(accessTier(0))).(*accessTier); got != 2 {
		t.Fatalf("enum = %d", got)
	}
	if got := *NormalizeScannedValue(&sql.NullInt64{Int64: 7, Valid: true}, reflect.TypeOf(uint32(0))).(*uint32); got != 7 {
		t.Fatalf("uint32 = %d", got)
	}
	if got := *NormalizeScannedValue(&sql.NullFloat64{Float64: 3.5, Valid: true}, reflect.TypeOf(float64(0))).(*float64); got != 3.5 {
		t.Fatalf("float64 = %v", got)
	}
	if got := *NormalizeScannedValue(&sql.NullString{String: "avatar", Valid: true}, reflect.TypeOf("")).(*string); got != "avatar" {
		t.Fatalf("string = %q", got)
	}
	if got := *NormalizeScannedValue(&sql.NullBool{Bool: true, Valid: true}, reflect.TypeOf(false)).(*bool); !got {
		t.Fatal("bool = false")
	}
}

// An entity field DECLARED as sql.NullString means the caller wants to know about the NULL,
// so it must come back as the NullString itself rather than being flattened to "".
func TestNormalizeScannedValueKeepsDeclaredNullString(t *testing.T) {
	fieldType := reflect.TypeOf(sql.NullString{})
	dest := ScanDestinationForField(fieldType)
	if _, ok := NormalizeScannedValue(dest, fieldType).(*sql.NullString); !ok {
		t.Fatalf("declared sql.NullString field normalized away")
	}
}

func TestScanDestinationForByteSlice(t *testing.T) {
	if _, ok := ScanDestinationForField(reflect.TypeOf([]byte(nil))).(*[]uint8); !ok {
		t.Fatal("[]byte field must scan into *[]uint8")
	}
}
