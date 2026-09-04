package dbsql

import (
	"database/sql"
	"reflect"
)

// scan_value.go answers the question the postgres and mariadb drivers used to answer
// wrongly, in duplicate: what do you hand rows.Scan for a struct field, and what do you do
// when the column comes back NULL?
//
// WHAT THIS FIXES, because the shape of the bug is worth remembering. The auto-migrator is
// additive: a new entity field becomes an ALTER TABLE ADD COLUMN with no default, which
// leaves every EXISTING row NULL. Strings and bools already scanned through sql.NullString
// and sql.NullBool for exactly that reason — but the numeric kinds were handed a raw
// *int64/*float64, and database/sql cannot put a NULL in one. So the first upgrade that
// added a numeric field to a populated table broke the whole SELECT:
//
//	failover: listing nodes: select list failed: sql: Scan error on column index 8,
//	name "version_seen_at": converting NULL to int64 is unsupported
//
// Not the row — the LIST. One un-backfilled column and the fleet has no nodes.
//
// It stayed hidden because the sqlite driver scans into interface{} and maps nil to the
// zero value, so it is immune; every unit test in the suite runs on sqlite. The bug could
// only ever appear on a customer's postgres or mariadb, on an upgrade, and only once there
// were rows to migrate.
//
// A NULL therefore reads as the field's ZERO VALUE, which is what the un-backfilled column
// means: nobody has reported a version yet, nothing has been seen at, the count is none. A
// migration should still backfill the column — this is the seam that keeps a missed
// backfill from taking a screen down.

// ScanDestinationForField returns the pointer to hand rows.Scan for a struct field of this
// type. Every destination that can receive a NULL is a sql.NullXxx; NormalizeScannedValue
// turns it back into a plain pointer of the field's own type.
func ScanDestinationForField(fieldType reflect.Type) interface{} {
	if fieldType == reflect.TypeOf(sql.NullString{}) {
		return new(sql.NullString)
	}
	if fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Uint8 {
		return new([]uint8)
	}

	switch fieldType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return new(sql.NullInt64)
	case reflect.Float32, reflect.Float64:
		return new(sql.NullFloat64)
	case reflect.String:
		return new(sql.NullString)
	case reflect.Bool:
		return new(sql.NullBool)
	default:
		return new(interface{})
	}
}

// NormalizeScannedValue converts what ScanDestinationForField produced back into a pointer
// to the field's own type, mapping a NULL to that type's zero value. Anything it does not
// recognise is passed through untouched.
func NormalizeScannedValue(raw interface{}, fieldType reflect.Type) interface{} {
	switch value := raw.(type) {
	case *sql.NullString:
		if fieldType.Kind() != reflect.String {
			return raw // an entity field declared as sql.NullString keeps the NULL
		}
		normalized := ""
		if value.Valid {
			normalized = value.String
		}
		return &normalized
	case *sql.NullBool:
		if fieldType.Kind() != reflect.Bool {
			return raw
		}
		normalized := value.Valid && value.Bool
		return &normalized
	case *sql.NullInt64:
		ptr := reflect.New(fieldType)
		if value.Valid {
			switch fieldType.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				ptr.Elem().SetInt(value.Int64)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if value.Int64 >= 0 {
					ptr.Elem().SetUint(uint64(value.Int64))
				}
			default:
				return raw
			}
		}
		return ptr.Interface()
	case *sql.NullFloat64:
		if k := fieldType.Kind(); k != reflect.Float32 && k != reflect.Float64 {
			return raw
		}
		ptr := reflect.New(fieldType)
		if value.Valid {
			ptr.Elem().SetFloat(value.Float64)
		}
		return ptr.Interface()
	}

	return raw
}
