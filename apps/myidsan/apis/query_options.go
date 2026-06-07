package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type ListQueryOptions struct {
	Limit   uint64
	Offset  uint64
	Filters []sqldataenums.Filter
	Sorters []sqldataenums.Sorter
}

type queryField struct {
	Name string
	Type reflect.Type
}

func ParseListQueryOptions[T any](r *http.Request) (ListQueryOptions, error) {
	query := r.URL.Query()
	limit, _ := strconv.ParseUint(query.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(query.Get("offset"), 10, 64)

	fields, err := queryFieldsFor[T]()
	if err != nil {
		return ListQueryOptions{}, err
	}

	filters, err := parseQueryFilters(query["filters"], fields)
	if err != nil {
		return ListQueryOptions{}, err
	}
	if repeated, err := parseQueryFilters(query["filter"], fields); err != nil {
		return ListQueryOptions{}, err
	} else {
		filters = append(filters, repeated...)
	}

	sorters, err := parseQuerySorters(query["sorters"], fields)
	if err != nil {
		return ListQueryOptions{}, err
	}
	if repeated, err := parseQuerySorters(query["sorter"], fields); err != nil {
		return ListQueryOptions{}, err
	} else {
		sorters = append(sorters, repeated...)
	}

	return ListQueryOptions{
		Limit:   limit,
		Offset:  offset,
		Filters: filters,
		Sorters: sorters,
	}, nil
}

func parseListQueryOptions[T any](r *http.Request) (ListQueryOptions, error) {
	return ParseListQueryOptions[T](r)
}

func queryFieldsFor[T any]() (map[string]queryField, error) {
	var model T
	typ := reflect.TypeOf(model)
	if typ == nil {
		return nil, fmt.Errorf("query model type is invalid")
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("query model must be a struct")
	}

	fields := make(map[string]queryField)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		qf := queryField{Name: field.Name, Type: field.Type}
		addQueryField(fields, field.Name, qf)
		addQueryField(fields, strcase.ToLowerCamel(field.Name), qf)
		addQueryField(fields, strcase.ToSnake(field.Name), qf)
		addTaggedQueryField(fields, field.Tag.Get("json"), qf)
		addTaggedQueryField(fields, field.Tag.Get("query"), qf)
		addTaggedQueryField(fields, field.Tag.Get("form"), qf)
	}

	return fields, nil
}

func addTaggedQueryField(fields map[string]queryField, tag string, field queryField) {
	if tag == "" {
		return
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return
	}
	addQueryField(fields, name, field)
}

func addQueryField(fields map[string]queryField, name string, field queryField) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	fields[strings.ToLower(name)] = field
}

type rawQueryFilter struct {
	FieldName string               `json:"fieldName"`
	Compare   sqldataenums.Compare `json:"compare"`
	Value     any                  `json:"value"`
}

func parseQueryFilters(values []string, fields map[string]queryField) ([]sqldataenums.Filter, error) {
	var filters []sqldataenums.Filter
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var batch []rawQueryFilter
		if err := decodeQueryJSON(raw, &batch); err != nil {
			var single rawQueryFilter
			if err := decodeQueryJSON(raw, &single); err != nil {
				return nil, fmt.Errorf("filters must be JSON filter object or array")
			}
			batch = []rawQueryFilter{single}
		}

		for _, filter := range batch {
			field, ok := fields[strings.ToLower(strings.TrimSpace(filter.FieldName))]
			if !ok {
				return nil, fmt.Errorf("unknown filter field %q", filter.FieldName)
			}
			if filter.Compare < sqldataenums.Equal || filter.Compare > sqldataenums.LessThanOrEqualTo {
				return nil, fmt.Errorf("unsupported filter compare %d", filter.Compare)
			}

			value, err := normalizeQueryValue(filter.Value, field.Type)
			if err != nil {
				return nil, fmt.Errorf("invalid filter value for %s: %w", filter.FieldName, err)
			}

			filters = append(filters, sqldataenums.Filter{
				FieldName: field.Name,
				Compare:   filter.Compare,
				Value:     value,
			})
		}
	}

	return filters, nil
}

type rawQuerySorter struct {
	FieldName string            `json:"fieldName"`
	Sort      sqldataenums.Sort `json:"sort"`
}

func parseQuerySorters(values []string, fields map[string]queryField) ([]sqldataenums.Sorter, error) {
	var sorters []sqldataenums.Sorter
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var batch []rawQuerySorter
		if err := decodeQueryJSON(raw, &batch); err != nil {
			var single rawQuerySorter
			if err := decodeQueryJSON(raw, &single); err != nil {
				return nil, fmt.Errorf("sorters must be JSON sorter object or array")
			}
			batch = []rawQuerySorter{single}
		}

		for _, sorter := range batch {
			field, ok := fields[strings.ToLower(strings.TrimSpace(sorter.FieldName))]
			if !ok {
				return nil, fmt.Errorf("unknown sorter field %q", sorter.FieldName)
			}
			if sorter.Sort != sqldataenums.ASC && sorter.Sort != sqldataenums.DESC {
				return nil, fmt.Errorf("unsupported sorter direction %d", sorter.Sort)
			}

			sorters = append(sorters, sqldataenums.Sorter{
				FieldName: field.Name,
				Sort:      sorter.Sort,
			})
		}
	}

	return sorters, nil
}

func decodeQueryJSON(raw string, target any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(target)
}

func normalizeQueryValue(value any, typ reflect.Type) (any, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if typ.PkgPath() == "database/sql" && typ.Name() == "NullString" {
		return queryStringValue(value)
	}

	switch typ.Kind() {
	case reflect.Bool:
		return queryBoolValue(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return queryIntValue(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return queryUintValue(value)
	case reflect.String:
		return queryStringValue(value)
	default:
		return queryStringValue(value)
	}
}

func queryStringValue(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case bool:
		return strconv.FormatBool(v), nil
	case []any, map[string]any:
		return "", fmt.Errorf("expected scalar string value")
	default:
		return fmt.Sprint(v), nil
	}
}

func queryBoolValue(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(v))
	default:
		return false, fmt.Errorf("expected boolean value")
	}
}

func queryIntValue(value any) (int64, error) {
	switch v := value.(type) {
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("expected integer value")
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected integer value")
	}
}

func queryUintValue(value any) (uint64, error) {
	switch v := value.(type) {
	case json.Number:
		return strconv.ParseUint(v.String(), 10, 64)
	case string:
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	case float64:
		if v != float64(uint64(v)) {
			return 0, fmt.Errorf("expected unsigned integer value")
		}
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("expected unsigned integer value")
	}
}
