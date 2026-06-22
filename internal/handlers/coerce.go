package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// bindJSONCoerced binds the request JSON body onto dst, first coercing any
// string-encoded scalar values (e.g. {"moisture":"11.5"}) into the numeric /
// boolean types expected by the destination struct. Lab tech UIs submit form
// values as strings, which would otherwise cause gin's ShouldBindJSON to fail
// with "json: cannot unmarshal string into Go struct field ... of type float64".
//
// It preserves gin's existing `binding` validation by restoring the coerced
// body onto the request and delegating to ShouldBindJSON.
func bindJSONCoerced(c *gin.Context, dst any) error {
	raw, err := c.GetRawData()
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(coerceJSONScalars(raw, dst)))
	return c.ShouldBindJSON(dst)
}

// coerceJSONScalars rewrites raw JSON so that string-encoded scalar fields are
// converted to the numeric/boolean kinds expected by dst's type. It walks
// nested structs and slices of structs recursively. On any parse error it
// returns raw unchanged so behaviour degrades to the original binding.
func coerceJSONScalars(raw []byte, dst any) []byte {
	t := reflect.TypeOf(dst)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return raw
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		var rows []map[string]any
		if json.Unmarshal(raw, &rows) != nil {
			return raw
		}
		et := t.Elem()
		for _, m := range rows {
			coerceScalarStrings(et, m)
		}
		if out, err := json.Marshal(rows); err == nil {
			return out
		}
	case reflect.Struct:
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return raw
		}
		coerceScalarStrings(t, m)
		if out, err := json.Marshal(m); err == nil {
			return out
		}
	}
	return raw
}

func coerceScalarStrings(t reflect.Type, m map[string]any) {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		val, present := m[name]
		if !present {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if s, ok := val.(string); ok && s == "" {
			switch ft.Kind() {
			case reflect.Float32, reflect.Float64,
				reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.Bool:
				// Blank numeric/bool form value clears the field: JSON null decodes
				// to zero (value) or nil (pointer); "" would fail the unmarshal.
				m[name] = nil
				continue
			}
		}
		switch ft.Kind() {
		case reflect.Struct:
			if nested, ok := val.(map[string]any); ok {
				coerceScalarStrings(ft, nested)
			}
		case reflect.Slice, reflect.Array:
			et := ft.Elem()
			for et.Kind() == reflect.Ptr {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				if arr, ok := val.([]any); ok {
					for _, el := range arr {
						if nested, ok := el.(map[string]any); ok {
							coerceScalarStrings(et, nested)
						}
					}
				}
			}
		case reflect.Float32, reflect.Float64:
			if s, ok := val.(string); ok && s != "" {
				if v, err := strconv.ParseFloat(s, 64); err == nil {
					m[name] = v
				}
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if s, ok := val.(string); ok && s != "" {
				if v, err := strconv.ParseInt(s, 10, 64); err == nil {
					m[name] = v
				}
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if s, ok := val.(string); ok && s != "" {
				if v, err := strconv.ParseUint(s, 10, 64); err == nil {
					m[name] = v
				}
			}
		case reflect.Bool:
			if s, ok := val.(string); ok && s != "" {
				if v, err := strconv.ParseBool(s); err == nil {
					m[name] = v
				}
			}
		}
	}
}
