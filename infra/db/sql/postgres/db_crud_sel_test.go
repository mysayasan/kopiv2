package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
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

func TestSignedCountToUint64(t *testing.T) {
	if got := signedCountToUint64(25); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
	if got := signedCountToUint64(-1); got != 0 {
		t.Fatalf("expected negative count to normalize to 0, got %d", got)
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

	if !strings.Contains(sqlStr, "WITH cte AS") {
		t.Fatalf("expected offset query to use count CTE, got:\n%s", sqlStr)
	}
	if !strings.Contains(sqlStr, "OFFSET 10") {
		t.Fatalf("expected OFFSET 10, got:\n%s", sqlStr)
	}
	if strings.Contains(sqlStr, "LIMIT 0") {
		t.Fatalf("offset-only query must not emit LIMIT 0, got:\n%s", sqlStr)
	}
}

func TestGenSelSqlStrWithJoinSpecsUsesAliasesAndDbCol(t *testing.T) {
	_, sqlStr := (&dbCrud{}).genSelSqlStrWithJoinSpecs(
		reflect.ValueOf(entities.ApiEndpointRbacListModel{}),
		10,
		0,
		[]sqldataenums.Filter{{FieldName: "EndpointPath", Compare: sqldataenums.Equal, Value: "/api/users"}},
		[]sqldataenums.Sorter{{FieldName: "EndpointPath", Sort: sqldataenums.ASC}},
		"api_endpoint_rbac",
		dbsql.JoinSpec{Source: "api_endpoint", Alias: "table1"},
		dbsql.JoinSpec{Source: "user_role", Alias: "table2"},
	)

	expectedParts := []string{
		"INNER JOIN api_endpoint table1 ON table0.api_endpoint_id = table1.id",
		"INNER JOIN user_role table2 ON table0.user_role_id = table2.id",
		"table1.path",
		"WHERE table1.path = '/api/users'",
		"ORDER BY table1.path ASC",
	}
	for _, expected := range expectedParts {
		if !strings.Contains(sqlStr, expected) {
			t.Fatalf("expected SQL to contain %q, got:\n%s", expected, sqlStr)
		}
	}
}

func TestWriteSqlEscapesStringValues(t *testing.T) {
	endpoint := entities.ApiEndpoint{
		Title:    "John's Portal",
		Metadata: `{"menu":{"label":"John's Portal"}}`,
		AppCode:  "myidsan",
		Host:     "*",
		Path:     "/api/endpoint",
	}
	props := reflect.ValueOf(endpoint)

	insertSQL := (&dbCrud{}).genInsSqlStr(props, "")
	if strings.Contains(insertSQL, "'John's Portal'") || !strings.Contains(insertSQL, "John''s Portal") {
		t.Fatalf("expected insert SQL to escape apostrophes, got:\n%s", insertSQL)
	}

	updateSQL := (&dbCrud{}).genUpdSqlStr(props, "", nil)
	if strings.Contains(updateSQL, "'John's Portal'") || !strings.Contains(updateSQL, "John''s Portal") {
		t.Fatalf("expected update SQL to escape apostrophes, got:\n%s", updateSQL)
	}
}

func TestSmokeSelectApiLogOffsetOnly(t *testing.T) {
	if os.Getenv("KOPIV2_POSTGRES_SMOKE") != "1" {
		t.Skip("set KOPIV2_POSTGRES_SMOKE=1 to run against a local Postgres database")
	}

	crud, err := NewDbCrud(dbsql.DbConfigModel{
		Host:     getenvDefault("KOPIV2_POSTGRES_HOST", "localhost"),
		Port:     getenvIntDefault("KOPIV2_POSTGRES_PORT", 5433),
		User:     getenvDefault("KOPIV2_POSTGRES_USER", "postgres"),
		Password: getenvDefault("KOPIV2_POSTGRES_PASSWORD", "postgres"),
		DbName:   getenvDefault("KOPIV2_POSTGRES_DB", "mymatasandb"),
		SslMode:  getenvDefault("KOPIV2_POSTGRES_SSLMODE", "disable"),
	})
	if err != nil {
		t.Fatalf("NewDbCrud failed: %v", err)
	}

	_, _, err = crud.Select(context.Background(), entities.ApiEndpoint{}, 0, 0, nil, nil, "")
	if err != nil {
		t.Fatalf("Select api_endpoint failed: %v", err)
	}

	_, _, err = crud.Select(context.Background(), entities.ApiLog{}, 0, 10, nil, []sqldataenums.Sorter{
		{FieldName: "CreatedAt", Sort: sqldataenums.DESC},
	}, "")
	if err != nil {
		t.Fatalf("Select api_log offset-only failed: %v", err)
	}
}

func getenvDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvIntDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}
