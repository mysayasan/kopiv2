package dtos

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/iancoleman/strcase"
)

func Project[T any](src any) (*T, error) {
	if src == nil {
		return nil, nil
	}

	var dst T
	dstVal := reflect.ValueOf(&dst).Elem()
	if dstVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("dto projection target must be a struct")
	}

	fields, err := sourceFields(src)
	if err != nil {
		return nil, err
	}

	dstTyp := dstVal.Type()
	for i := 0; i < dstTyp.NumField(); i++ {
		dstField := dstTyp.Field(i)
		if dstField.PkgPath != "" {
			continue
		}

		dstSlot := dstVal.Field(i)
		if !dstSlot.CanSet() {
			continue
		}

		for _, key := range fieldKeys(dstField) {
			srcVal, ok := fields[key]
			if !ok {
				continue
			}
			if setProjectedValue(dstSlot, srcVal) {
				break
			}
		}
	}

	return &dst, nil
}

func ProjectSlice[T any](src any) ([]*T, error) {
	if src == nil {
		return nil, nil
	}

	srcVal := reflect.ValueOf(src)
	for srcVal.Kind() == reflect.Pointer {
		if srcVal.IsNil() {
			return nil, nil
		}
		srcVal = srcVal.Elem()
	}

	if srcVal.Kind() != reflect.Slice && srcVal.Kind() != reflect.Array {
		return nil, fmt.Errorf("dto projection source must be a slice or array")
	}

	res := make([]*T, 0, srcVal.Len())
	for i := 0; i < srcVal.Len(); i++ {
		dto, err := Project[T](srcVal.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		if dto != nil {
			res = append(res, dto)
		}
	}

	return res, nil
}

func sourceFields(src any) (map[string]reflect.Value, error) {
	srcVal := reflect.ValueOf(src)
	for srcVal.Kind() == reflect.Pointer {
		if srcVal.IsNil() {
			return map[string]reflect.Value{}, nil
		}
		srcVal = srcVal.Elem()
	}

	switch srcVal.Kind() {
	case reflect.Struct:
		return structFields(srcVal), nil
	case reflect.Map:
		return mapFields(srcVal), nil
	default:
		return nil, fmt.Errorf("dto projection source must be a struct or map")
	}
}

func structFields(srcVal reflect.Value) map[string]reflect.Value {
	srcTyp := srcVal.Type()
	fields := make(map[string]reflect.Value)
	for i := 0; i < srcTyp.NumField(); i++ {
		field := srcTyp.Field(i)
		if field.PkgPath != "" {
			continue
		}
		value := srcVal.Field(i)
		for _, key := range fieldKeys(field) {
			if _, exists := fields[key]; !exists {
				fields[key] = value
			}
		}
	}
	return fields
}

func mapFields(srcVal reflect.Value) map[string]reflect.Value {
	fields := make(map[string]reflect.Value)
	for _, key := range srcVal.MapKeys() {
		if key.Kind() != reflect.String {
			continue
		}
		name := strings.TrimSpace(key.String())
		if name == "" {
			continue
		}
		fields[strings.ToLower(name)] = srcVal.MapIndex(key)
	}
	return fields
}

func fieldKeys(field reflect.StructField) []string {
	raw := []string{
		field.Name,
		strcase.ToLowerCamel(field.Name),
		strcase.ToSnake(field.Name),
		tagName(field.Tag.Get("json")),
		tagName(field.Tag.Get("form")),
		tagName(field.Tag.Get("query")),
	}

	keys := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, key := range raw {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || key == "-" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func tagName(tag string) string {
	if i := strings.Index(tag, ","); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

func setProjectedValue(dst reflect.Value, src reflect.Value) bool {
	if !src.IsValid() {
		return false
	}

	if src.Kind() == reflect.Interface {
		if src.IsNil() {
			return false
		}
		src = src.Elem()
	}

	if dst.Kind() == reflect.Pointer {
		return setProjectedPointer(dst, src)
	}

	for src.Kind() == reflect.Pointer {
		if src.IsNil() {
			return false
		}
		src = src.Elem()
	}

	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return true
	}
	if src.Type().ConvertibleTo(dst.Type()) {
		dst.Set(src.Convert(dst.Type()))
		return true
	}
	return false
}

func setProjectedPointer(dst reflect.Value, src reflect.Value) bool {
	if src.Kind() == reflect.Pointer {
		if src.IsNil() {
			return false
		}
		if src.Type().AssignableTo(dst.Type()) {
			dst.Set(src)
			return true
		}
		if src.Type().ConvertibleTo(dst.Type()) {
			dst.Set(src.Convert(dst.Type()))
			return true
		}
		src = src.Elem()
	}

	if src.Type().AssignableTo(dst.Type().Elem()) {
		val := reflect.New(dst.Type().Elem())
		val.Elem().Set(src)
		dst.Set(val)
		return true
	}
	if src.Type().ConvertibleTo(dst.Type().Elem()) {
		val := reflect.New(dst.Type().Elem())
		val.Elem().Set(src.Convert(dst.Type().Elem()))
		dst.Set(val)
		return true
	}
	return false
}
