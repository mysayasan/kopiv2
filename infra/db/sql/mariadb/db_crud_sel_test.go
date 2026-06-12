package mariadb

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
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

func TestScanDestinationForFieldSupportsDefinedIntEnum(t *testing.T) {
	dest := scanDestinationForField(reflect.TypeOf(apiaccessenums.AuthOnly))
	if _, ok := dest.(*int32); !ok {
		t.Fatalf("expected *int32 destination for defined int32 enum, got %T", dest)
	}
}

func TestGenSelSqlStrAppliesOffsetWithoutLimit(t *testing.T) {
	_, sqlStr := (&dbCrud{}).genSelSqlStr(reflect.ValueOf(entities.ApiLog{}), 0, 10, nil, nil, "")

	if !strings.Contains(sqlStr, "LIMIT 18446744073709551615 OFFSET 10") {
		t.Fatalf("expected MariaDB offset-only query to include unbounded limit, got:\n%s", sqlStr)
	}
	if strings.Contains(sqlStr, "LIMIT 0") {
		t.Fatalf("offset-only query must not emit LIMIT 0, got:\n%s", sqlStr)
	}
}
