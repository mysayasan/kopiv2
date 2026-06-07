package apis

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func TestParseListQueryOptionsNormalizesFiltersAndSorters(t *testing.T) {
	target := "/api/log?limit=25&offset=50"
	target += "&filters=" + url.QueryEscape(`[{"fieldName":"statsCode","compare":1,"value":200},{"fieldName":"createdAt","compare":5,"value":"1700000000"}]`)
	target += "&sorters=" + url.QueryEscape(`[{"fieldName":"createdAt","sort":2}]`)
	req := httptest.NewRequest("GET", target, nil)

	opts, err := parseListQueryOptions[entities.ApiLog](req)
	if err != nil {
		t.Fatalf("parseListQueryOptions failed: %v", err)
	}

	if opts.Limit != 25 || opts.Offset != 50 {
		t.Fatalf("unexpected paging: limit=%d offset=%d", opts.Limit, opts.Offset)
	}
	if len(opts.Filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(opts.Filters))
	}
	if opts.Filters[0].FieldName != "StatsCode" || opts.Filters[0].Compare != sqldataenums.Equal || opts.Filters[0].Value != int64(200) {
		t.Fatalf("unexpected first filter: %#v", opts.Filters[0])
	}
	if opts.Filters[1].FieldName != "CreatedAt" || opts.Filters[1].Compare != sqldataenums.GreaterThanOrEqualTo || opts.Filters[1].Value != int64(1700000000) {
		t.Fatalf("unexpected second filter: %#v", opts.Filters[1])
	}
	if len(opts.Sorters) != 1 || opts.Sorters[0].FieldName != "CreatedAt" || opts.Sorters[0].Sort != sqldataenums.DESC {
		t.Fatalf("unexpected sorters: %#v", opts.Sorters)
	}
}

func TestParseListQueryOptionsRejectsUnknownField(t *testing.T) {
	target := "/api/log?filters=" + url.QueryEscape(`[{"fieldName":"doesNotExist","compare":1,"value":"x"}]`)
	req := httptest.NewRequest("GET", target, nil)

	if _, err := parseListQueryOptions[entities.ApiLog](req); err == nil {
		t.Fatalf("expected unknown field error")
	}
}
