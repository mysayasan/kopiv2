package mariadb

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

func TestGenSelSqlStrAppliesOffsetWithoutLimit(t *testing.T) {
	_, sqlStr := (&dbCrud{}).genSelSqlStr(reflect.ValueOf(entities.ApiLog{}), 0, 10, nil, nil, "")

	if !strings.Contains(sqlStr, "LIMIT 18446744073709551615 OFFSET 10") {
		t.Fatalf("expected MariaDB offset-only query to include unbounded limit, got:\n%s", sqlStr)
	}
	if strings.Contains(sqlStr, "LIMIT 0") {
		t.Fatalf("offset-only query must not emit LIMIT 0, got:\n%s", sqlStr)
	}
}
